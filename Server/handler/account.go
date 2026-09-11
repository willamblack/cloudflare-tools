package handler

import (
	"cloudflare-tools/server/models"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func ListAccounts(c *gin.Context) {
	accounts := models.ListAccounts()
	if accounts == nil {
		accounts = []models.Account{}
	}
	for i := range accounts {
		accounts[i].Key = ""
	}
	c.JSON(http.StatusOK, accounts)
}

func AddAccount(c *gin.Context) {
	var acc models.Account
	if err := c.ShouldBindJSON(&acc); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	acc.Email = strings.TrimSpace(acc.Email)
	acc.Name = strings.TrimSpace(acc.Name)
	if acc.Email == "" || acc.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Name and email are required"})
		return
	}

	if acc.ID != "" {
		existing, found := models.FindAccount(acc.ID)
		if !found {
			c.JSON(http.StatusNotFound, gin.H{"error": "Account not found"})
			return
		}
		if acc.Key == "" {
			acc.Key = existing.Key
		}
	} else {
		if acc.Key == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "API key is required"})
			return
		}
		acc.ID = uuid.New().String()
	}

	if err := models.UpsertAccount(acc); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	acc.Key = ""
	c.JSON(http.StatusOK, acc)
}

func DeleteAccount(c *gin.Context) {
	id := c.Param("id")
	deleted, err := models.DeleteAccount(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if !deleted {
		c.JSON(http.StatusNotFound, gin.H{"error": "Account not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}

func TestAccount(c *gin.Context) {
	var acc models.Account
	if err := c.ShouldBindJSON(&acc); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid data"})
		return
	}
	if acc.ID != "" {
		stored, ok := models.FindAccount(acc.ID)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "Account not found"})
			return
		}
		acc = stored
	}
	if acc.Email == "" || acc.Key == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Email and API key are required"})
		return
	}

	req, err := http.NewRequestWithContext(c.Request.Context(), "GET", "https://api.cloudflare.com/client/v4/zones?per_page=1", nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "Unable to create request"})
		return
	}
	req.Header.Add("X-Auth-Email", acc.Email)
	req.Header.Add("X-Auth-Key", acc.Key)
	req.Header.Add("Content-Type", "application/json")

	resp, err := cloudflareClient.Do(req)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "无法连接到 Cloudflare API"})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		c.JSON(http.StatusOK, gin.H{"success": true})
	} else {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxCloudflareResponseBytes))
		var apiError struct {
			Errors []struct {
				Message string `json:"message"`
			} `json:"errors"`
		}
		message := fmt.Sprintf("校验失败 (HTTP %d)", resp.StatusCode)
		if json.Unmarshal(body, &apiError) == nil && len(apiError.Errors) > 0 {
			message += ": " + apiError.Errors[0].Message
		}
		c.JSON(http.StatusOK, gin.H{"success": false, "message": message})
	}
}
