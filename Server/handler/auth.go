package handler

import (
	"cloudflare-tools/server/config"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

const (
	jwtIssuer   = "cloudflare-tools"
	jwtAudience = "cloudflare-tools-web"
)

var (
	jwtSecret     []byte
	loginAttempts = make(map[string]LoginAttempt)
	attemptsMutex sync.Mutex
)

type LoginAttempt struct {
	Count       int
	LastAttempt time.Time
	LockedUntil time.Time
}

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type authClaims struct {
	jwt.RegisteredClaims
}

// InitializeJWTSecret loads a stable signing secret from JWT_SECRET. If none is
// configured, it generates an ephemeral secret so a restart invalidates tokens.
func InitializeJWTSecret() (bool, error) {
	configured := os.Getenv("JWT_SECRET")
	if configured != "" {
		if len(configured) < 32 {
			return false, fmt.Errorf("JWT_SECRET must contain at least 32 characters")
		}
		jwtSecret = []byte(configured)
		return true, nil
	}

	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return false, fmt.Errorf("generate JWT signing secret: %w", err)
	}
	jwtSecret = secret
	return false, nil
}

func Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	if req.Username == "" || req.Password == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "用户名和密码不能为空"})
		return
	}

	clientIP := c.ClientIP()
	if locked, remaining := loginLocked(clientIP); locked {
		c.JSON(http.StatusTooManyRequests, gin.H{
			"error":          "登录失败次数过多，请稍后再试",
			"locked_minutes": int(remaining.Minutes()) + 1,
		})
		return
	}

	if !secureEqual(req.Username, config.GlobalConfig.Admin.Username) ||
		!secureEqual(req.Password, config.GlobalConfig.Admin.Password) {
		remaining, locked := recordFailedLogin(clientIP)
		if locked {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "登录失败次数过多，账户已锁定15分钟"})
			return
		}
		c.JSON(http.StatusUnauthorized, gin.H{
			"error":              "用户名或密码错误",
			"remaining_attempts": remaining,
		})
		return
	}

	attemptsMutex.Lock()
	delete(loginAttempts, clientIP)
	attemptsMutex.Unlock()

	if len(jwtSecret) == 0 {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "认证服务尚未初始化"})
		return
	}
	now := time.Now()
	claims := authClaims{RegisteredClaims: jwt.RegisteredClaims{
		Issuer:    jwtIssuer,
		Subject:   config.GlobalConfig.Admin.Username,
		Audience:  jwt.ClaimStrings{jwtAudience},
		IssuedAt:  jwt.NewNumericDate(now),
		NotBefore: jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(24 * time.Hour)),
	}}
	tokenString, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(jwtSecret)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "生成令牌失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"token": tokenString, "expires_in": 86400})
}

func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		tokenString := strings.TrimSpace(c.GetHeader("Authorization"))
		if strings.HasPrefix(strings.ToLower(tokenString), "bearer ") {
			tokenString = strings.TrimSpace(tokenString[7:])
		}
		if tokenString == "" || len(jwtSecret) == 0 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "未提供认证令牌"})
			return
		}

		claims := &authClaims{}
		token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
			return jwtSecret, nil
		}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithIssuer(jwtIssuer),
			jwt.WithAudience(jwtAudience), jwt.WithExpirationRequired())
		if err != nil || !token.Valid || claims.Subject != config.GlobalConfig.Admin.Username {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "令牌无效或已过期"})
			return
		}

		c.Set("user", claims.Subject)
		c.Next()
	}
}

func secureEqual(provided, expected string) bool {
	providedHash := sha256.Sum256([]byte(provided))
	expectedHash := sha256.Sum256([]byte(expected))
	return subtle.ConstantTimeCompare(providedHash[:], expectedHash[:]) == 1
}

func loginLocked(clientIP string) (bool, time.Duration) {
	attemptsMutex.Lock()
	defer attemptsMutex.Unlock()
	now := time.Now()
	attempt, exists := loginAttempts[clientIP]
	if !exists {
		return false, 0
	}
	if !attempt.LockedUntil.IsZero() && now.Before(attempt.LockedUntil) {
		return true, time.Until(attempt.LockedUntil)
	}
	if now.Sub(attempt.LastAttempt) > 15*time.Minute {
		delete(loginAttempts, clientIP)
	}
	return false, 0
}

func recordFailedLogin(clientIP string) (int, bool) {
	attemptsMutex.Lock()
	defer attemptsMutex.Unlock()
	now := time.Now()
	attempt := loginAttempts[clientIP]
	if now.Sub(attempt.LastAttempt) > 15*time.Minute {
		attempt.Count = 0
	}
	attempt.Count++
	attempt.LastAttempt = now
	if attempt.Count >= 5 {
		attempt.LockedUntil = now.Add(15 * time.Minute)
		loginAttempts[clientIP] = attempt
		return 0, true
	}
	loginAttempts[clientIP] = attempt
	return 5 - attempt.Count, false
}
