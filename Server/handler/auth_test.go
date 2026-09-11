package handler

import (
	"bytes"
	"cloudflare-tools/server/config"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

func TestLoginAndAuthMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	config.GlobalConfig.Admin.Username = "admin"
	config.GlobalConfig.Admin.Password = "correct-horse-battery-staple"
	t.Setenv("JWT_SECRET", "a-test-secret-that-is-longer-than-thirty-two-characters")
	if _, err := InitializeJWTSecret(); err != nil {
		t.Fatal(err)
	}
	attemptsMutex.Lock()
	loginAttempts = make(map[string]LoginAttempt)
	attemptsMutex.Unlock()

	router := gin.New()
	router.POST("/login", Login)
	router.GET("/protected", AuthMiddleware(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	loginBody := bytes.NewBufferString(`{"username":"admin","password":"correct-horse-battery-staple"}`)
	loginRequest := httptest.NewRequest(http.MethodPost, "/login", loginBody)
	loginRequest.Header.Set("Content-Type", "application/json")
	loginRecorder := httptest.NewRecorder()
	router.ServeHTTP(loginRecorder, loginRequest)
	if loginRecorder.Code != http.StatusOK {
		t.Fatalf("login returned %d: %s", loginRecorder.Code, loginRecorder.Body.String())
	}
	var loginResponse struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(loginRecorder.Body.Bytes(), &loginResponse); err != nil || loginResponse.Token == "" {
		t.Fatalf("invalid login response: %v %q", err, loginRecorder.Body.String())
	}

	protectedRequest := httptest.NewRequest(http.MethodGet, "/protected", nil)
	protectedRequest.Header.Set("Authorization", "Bearer "+loginResponse.Token)
	protectedRecorder := httptest.NewRecorder()
	router.ServeHTTP(protectedRecorder, protectedRequest)
	if protectedRecorder.Code != http.StatusOK {
		t.Fatalf("protected route returned %d: %s", protectedRecorder.Code, protectedRecorder.Body.String())
	}

	forged := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Issuer: jwtIssuer, Subject: "admin", Audience: jwt.ClaimStrings{jwtAudience},
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	})
	forgedToken, err := forged.SignedString([]byte("cf-tools-secret-change-in-production"))
	if err != nil {
		t.Fatal(err)
	}
	forgedRequest := httptest.NewRequest(http.MethodGet, "/protected", nil)
	forgedRequest.Header.Set("Authorization", forgedToken)
	forgedRecorder := httptest.NewRecorder()
	router.ServeHTTP(forgedRecorder, forgedRequest)
	if forgedRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("forged token returned %d, want 401", forgedRecorder.Code)
	}
}

func TestOfflineDNSDoesNotRequireAccountOrNetwork(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/dns", BatchParseDNS)
	body := bytes.NewBufferString(`{"accountId":"missing","records":["example.com|www|A|192.0.2.1"],"offlineMode":true}`)
	request := httptest.NewRequest(http.MethodPost, "/dns", body)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("offline DNS returned %d: %s", recorder.Code, recorder.Body.String())
	}
	if !bytes.Contains(recorder.Body.Bytes(), []byte(`"success":true`)) {
		t.Fatalf("offline DNS did not report success: %s", recorder.Body.String())
	}
}

func TestNormalizeRecordName(t *testing.T) {
	tests := map[string]string{
		"@":                "example.com",
		"www":              "www.example.com",
		"WWW.EXAMPLE.COM.": "www.example.com",
		"":                 "",
	}
	for host, expected := range tests {
		if actual := normalizeRecordName("Example.COM.", host); actual != expected {
			t.Errorf("normalizeRecordName(%q)=%q, want %q", host, actual, expected)
		}
	}
}
