package handler

import (
	"cloudflare-tools/server/models"
	"fmt"
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
)

type BatchBulkSettingsRequest struct {
	AccountID          string   `json:"accountId"`
	Domains            []string `json:"domains"`
	SecurityLevel      string   `json:"securityLevel"`
	ChallengePassage   string   `json:"challengePassage"`
	BrowserIntegrity   string   `json:"browserIntegrity"`
	HotlinkProtection  string   `json:"hotlinkProtection"`
	EmailObfuscation   string   `json:"emailObfuscation"`
	ServerSideExcludes string   `json:"serverSideExcludes"`
	WAF                string   `json:"waf"`
	PrivacyPass        string   `json:"privacyPass"`
	AutomaticPlatform  string   `json:"automaticPlatform"`
	OrangeToOrange     string   `json:"orangeToOrange"`
	ProxyReadTimeout   string   `json:"proxyReadTimeout"`
	PrefetchPreload    string   `json:"prefetchPreload"`
	ResponseBuffering  string   `json:"responseBuffering"`
	SortQueryString    string   `json:"sortQueryString"`
	TrueClientIP       string   `json:"trueClientIp"`
	CrawlerHints       string   `json:"crawlerHints"`
}

type BulkSettingsResult struct {
	Domain  string `json:"domain"`
	Success bool   `json:"success"`
	Message string `json:"message"`
}

func BatchBulkSettings(c *gin.Context) {
	var req BatchBulkSettingsRequest
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

	results := make([]BulkSettingsResult, len(req.Domains))
	var wg sync.WaitGroup

	for i, domain := range req.Domains {
		wg.Add(1)
		go func(idx int, dom string) {
			defer wg.Done()
			acquireBatchSlot()
			defer releaseBatchSlot()
			success, msg := applyBulkSettings(acc, dom, &req)
			results[idx] = BulkSettingsResult{
				Domain:  dom,
				Success: success,
				Message: msg,
			}
		}(i, domain)
	}

	wg.Wait()
	c.JSON(http.StatusOK, results)
}

func applyBulkSettings(acc *models.Account, domain string, settings *BatchBulkSettingsRequest) (bool, string) {
	zoneID, err := getZoneID(acc, domain)
	if err != nil {
		return false, err.Error()
	}
	successCount := 0
	totalOperations := 0

	if settings.SecurityLevel != "" {
		totalOperations++
		if updateZoneSetting(acc, zoneID, "security_level", settings.SecurityLevel) {
			successCount++
		}
	}

	if settings.ChallengePassage != "" {
		totalOperations++
		if updateZoneSetting(acc, zoneID, "challenge_ttl", settings.ChallengePassage) {
			successCount++
		}
	}

	if settings.BrowserIntegrity != "" {
		totalOperations++
		if updateZoneSetting(acc, zoneID, "browser_check", settings.BrowserIntegrity) {
			successCount++
		}
	}

	if settings.HotlinkProtection != "" {
		totalOperations++
		if updateZoneSetting(acc, zoneID, "hotlink_protection", settings.HotlinkProtection) {
			successCount++
		}
	}

	if settings.EmailObfuscation != "" {
		totalOperations++
		if updateZoneSetting(acc, zoneID, "email_obfuscation", settings.EmailObfuscation) {
			successCount++
		}
	}

	if settings.ServerSideExcludes != "" {
		totalOperations++
		if updateZoneSetting(acc, zoneID, "server_side_exclude", settings.ServerSideExcludes) {
			successCount++
		}
	}

	if settings.WAF != "" {
		totalOperations++
		if updateZoneSetting(acc, zoneID, "waf", settings.WAF) {
			successCount++
		}
	}

	if settings.PrivacyPass != "" {
		totalOperations++
		if updateZoneSetting(acc, zoneID, "privacy_pass", settings.PrivacyPass) {
			successCount++
		}
	}

	if settings.AutomaticPlatform != "" {
		totalOperations++
		if updateZoneSetting(acc, zoneID, "automatic_platform_optimization", settings.AutomaticPlatform) {
			successCount++
		}
	}

	if settings.OrangeToOrange != "" {
		totalOperations++
		if updateZoneSetting(acc, zoneID, "orange_to_orange", settings.OrangeToOrange) {
			successCount++
		}
	}

	if settings.ProxyReadTimeout != "" {
		totalOperations++
		if updateZoneSetting(acc, zoneID, "proxy_read_timeout", settings.ProxyReadTimeout) {
			successCount++
		}
	}

	if settings.PrefetchPreload != "" {
		totalOperations++
		if updateZoneSetting(acc, zoneID, "prefetch_preload", settings.PrefetchPreload) {
			successCount++
		}
	}

	if settings.ResponseBuffering != "" {
		totalOperations++
		if updateZoneSetting(acc, zoneID, "response_buffering", settings.ResponseBuffering) {
			successCount++
		}
	}

	if settings.SortQueryString != "" {
		totalOperations++
		if updateZoneSetting(acc, zoneID, "sort_query_string_for_cache", settings.SortQueryString) {
			successCount++
		}
	}

	if settings.TrueClientIP != "" {
		totalOperations++
		if updateZoneSetting(acc, zoneID, "true_client_ip_header", settings.TrueClientIP) {
			successCount++
		}
	}

	if settings.CrawlerHints != "" {
		totalOperations++
		if updateZoneSetting(acc, zoneID, "crawler_hints", settings.CrawlerHints) {
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
