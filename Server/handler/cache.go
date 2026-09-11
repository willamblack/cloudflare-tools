package handler

import (
	"bytes"
	"cloudflare-tools/server/models"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
)

type BatchCacheRequest struct {
	AccountID       string   `json:"accountId"`
	Domains         []string `json:"domains"`
	PurgeCache      bool     `json:"purgeCache"`
	CacheLevel      string   `json:"cacheLevel"`
	BrowserTTL      string   `json:"browserTtl"`
	AlwaysOnline    string   `json:"alwaysOnline"`
	DevelopmentMode string   `json:"developmentMode"`
}

type CacheResult struct {
	Domain  string `json:"domain"`
	Success bool   `json:"success"`
	Message string `json:"message"`
}

func BatchCacheSettings(c *gin.Context) {
	var req BatchCacheRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}
	if !validateBatch(c, len(req.Domains)) {
		return
	}

	acc, ok := findAccount(req.AccountID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "Account not found"})
		return
	}

	results := make([]CacheResult, len(req.Domains))
	var wg sync.WaitGroup

	for i, domain := range req.Domains {
		wg.Add(1)
		go func(idx int, dom string) {
			defer wg.Done()
			acquireBatchSlot()
			defer releaseBatchSlot()
			success, msg := applyCacheSettings(acc, dom, &req)
			results[idx] = CacheResult{
				Domain:  dom,
				Success: success,
				Message: msg,
			}
		}(i, domain)
	}

	wg.Wait()
	c.JSON(http.StatusOK, results)
}

func applyCacheSettings(acc *models.Account, domain string, settings *BatchCacheRequest) (bool, string) {
	zoneID, err := getZoneID(acc, domain)
	if err != nil {
		return false, err.Error()
	}
	successCount := 0
	totalOperations := 0

	if settings.PurgeCache {
		totalOperations++
		if purgeAllCache(acc, zoneID) {
			successCount++
		}
	}

	if settings.CacheLevel != "" {
		totalOperations++
		if updateZoneSetting(acc, zoneID, "cache_level", settings.CacheLevel) {
			successCount++
		}
	}

	if settings.BrowserTTL != "" {
		totalOperations++
		ttlValue := settings.BrowserTTL
		if updateZoneSetting(acc, zoneID, "browser_cache_ttl", ttlValue) {
			successCount++
		}
	}

	if settings.AlwaysOnline != "" {
		totalOperations++
		if updateZoneSetting(acc, zoneID, "always_online", settings.AlwaysOnline) {
			successCount++
		}
	}

	if settings.DevelopmentMode != "" {
		totalOperations++
		if updateZoneSetting(acc, zoneID, "development_mode", settings.DevelopmentMode) {
			successCount++
		}
	}

	if successCount == totalOperations && totalOperations > 0 {
		return true, fmt.Sprintf("Success (%d/%d)", successCount, totalOperations)
	} else if successCount > 0 {
		return true, fmt.Sprintf("Partial success (%d/%d)", successCount, totalOperations)
	}

	return false, "No operations performed"
}

func purgeAllCache(acc *models.Account, zoneID string) bool {
	payload := map[string]interface{}{
		"purge_everything": true,
	}
	body, _ := json.Marshal(payload)

	req, _ := http.NewRequest("POST", fmt.Sprintf("https://api.cloudflare.com/client/v4/zones/%s/purge_cache", zoneID), bytes.NewBuffer(body))
	req.Header.Add("X-Auth-Email", acc.Email)
	req.Header.Add("X-Auth-Key", acc.Key)
	req.Header.Add("Content-Type", "application/json")

	client := cloudflareClient
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}
