package main

import (
	"cloudflare-tools/server/config"
	"cloudflare-tools/server/handler"
	"cloudflare-tools/server/models"
	"embed"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

//go:embed dist/*
var content embed.FS

func main() {
	if err := config.LoadConfig(); err != nil {
		log.Fatalf("Configuration error: %v", err)
	}
	if err := models.LoadAccounts(); err != nil {
		log.Fatalf("Account data error: %v", err)
	}
	stableJWTSecret, err := handler.InitializeJWTSecret()
	if err != nil {
		log.Fatalf("Authentication configuration error: %v", err)
	}
	if !stableJWTSecret {
		log.Print("Warning: JWT_SECRET is not set; login sessions will be invalidated on restart")
	}

	r := newRouter()
	server := &http.Server{
		Addr:              ":8080",
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Minute,
		IdleTimeout:       60 * time.Second,
	}
	log.Println("Server starting on :8080")
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func newRouter() *gin.Engine {
	r := gin.Default()
	if err := r.SetTrustedProxies(nil); err != nil {
		log.Printf("Unable to disable trusted proxies: %v", err)
	}
	r.Use(func(c *gin.Context) {
		c.Writer.Header().Set("X-Content-Type-Options", "nosniff")
		c.Writer.Header().Set("X-Frame-Options", "DENY")
		c.Writer.Header().Set("Referrer-Policy", "no-referrer")
		if strings.HasPrefix(c.Request.URL.Path, "/api/") && c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 2<<20)
		}
		c.Next()
	})

	r.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	r.POST("/api/login", handler.Login)

	api := r.Group("/api")
	api.Use(handler.AuthMiddleware())
	{
		api.GET("/accounts", handler.ListAccounts)
		api.POST("/accounts", handler.AddAccount)
		api.PUT("/accounts/:id", handler.UpdateAccount)
		api.POST("/accounts/test", handler.TestAccount)
		api.DELETE("/accounts/:id", handler.DeleteAccount)
		api.POST("/zones/batch-add", handler.BatchAddZones)
		api.POST("/zones/batch-delete", handler.BatchDeleteZones)
		api.POST("/zones/export", handler.ExportZones)
		api.POST("/dns/batch-parse", handler.BatchParseDNS)
		api.POST("/dns/batch-delete", handler.BatchDeleteDNS)
		api.POST("/dns/proxy-toggle", handler.BatchProxyToggle)
		api.POST("/ssl/batch-settings", handler.BatchSSLSettings)
		api.POST("/certs/batch-apply", handler.BatchApplyCert)
		api.GET("/certs/list", handler.ListCerts)
		api.GET("/certs/download/:filename", handler.DownloadCert)
		api.POST("/rules/batch-copy", handler.BatchCopyRules)
		api.POST("/rules/batch-delete", handler.BatchDeleteRules)
		api.POST("/cache/batch-settings", handler.BatchCacheSettings)
		api.POST("/optimization/batch-settings", handler.BatchOptimization)
		api.POST("/bulk-settings/batch-apply", handler.BatchBulkSettings)
		api.POST("/email/batch-routing", handler.BatchEmailRouting)
		api.POST("/email/batch-delete", handler.BatchDeleteEmailRouting)
	}

	dist, err := fs.Sub(content, "dist")
	if err != nil {
		log.Fatal(err)
	}
	frontend := http.FileServer(http.FS(dist))
	r.NoRoute(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.JSON(http.StatusNotFound, gin.H{"error": "API route not found"})
			return
		}
		frontend.ServeHTTP(c.Writer, c.Request)
	})
	return r
}
