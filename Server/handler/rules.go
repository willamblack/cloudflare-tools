package handler

import (
	"bytes"
	"cloudflare-tools/server/models"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
)

type BatchCopyRulesRequest struct {
	AccountID     string   `json:"accountId"`
	SourceDomain  string   `json:"sourceDomain"`
	TargetDomains []string `json:"targetDomains"`
	RuleTypes     []string `json:"ruleTypes"`
}

type CopyRulesResult struct {
	Domain  string `json:"domain"`
	Success bool   `json:"success"`
	Message string `json:"message"`
	Count   int    `json:"count"`
}

func BatchCopyRules(c *gin.Context) {
	var req BatchCopyRulesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}
	if !validateBatch(c, len(req.TargetDomains)) {
		return
	}

	acc, ok := findAccount(req.AccountID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "Account not found"})
		return
	}

	sourceZoneID, err := getZoneIDByDomain(acc, req.SourceDomain)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Source domain not found"})
		return
	}

	results := make([]CopyRulesResult, len(req.TargetDomains))
	var wg sync.WaitGroup

	for i, domain := range req.TargetDomains {
		wg.Add(1)
		go func(idx int, dom string) {
			defer wg.Done()
			acquireBatchSlot()
			defer releaseBatchSlot()
			success, msg, count := copyRulesToDomain(acc, sourceZoneID, req.SourceDomain, dom, req.RuleTypes)
			results[idx] = CopyRulesResult{
				Domain:  dom,
				Success: success,
				Message: msg,
				Count:   count,
			}
		}(i, domain)
	}

	wg.Wait()
	c.JSON(http.StatusOK, results)
}

func copyRulesToDomain(acc *models.Account, sourceZoneID, sourceDomain, targetDomain string, ruleTypes []string) (bool, string, int) {
	targetZoneID, err := getZoneIDByDomain(acc, targetDomain)
	if err != nil {
		return false, "Target zone not found", 0
	}

	totalCopied := 0

	for _, ruleType := range ruleTypes {
		switch ruleType {
		case "page_rules":
			count := copyPageRules(acc, sourceZoneID, targetZoneID, sourceDomain, targetDomain)
			totalCopied += count
		case "firewall_rules":
			count := copyFirewallRules(acc, sourceZoneID, targetZoneID, sourceDomain, targetDomain)
			totalCopied += count
		case "rate_limiting":
			count := copyRateLimitRules(acc, sourceZoneID, targetZoneID, sourceDomain, targetDomain)
			totalCopied += count
		}
	}

	if totalCopied > 0 {
		return true, fmt.Sprintf("Copied %d rules", totalCopied), totalCopied
	}

	return false, "No rules copied", 0
}

func getZoneIDByDomain(acc *models.Account, domain string) (string, error) {
	return getZoneID(acc, domain)
}

func copyPageRules(acc *models.Account, sourceZoneID, targetZoneID, sourceDomain, targetDomain string) int {
	return copyRuleCollection(acc, "pagerules", sourceZoneID, targetZoneID, sourceDomain, targetDomain)
}

func copyFirewallRules(acc *models.Account, sourceZoneID, targetZoneID, sourceDomain, targetDomain string) int {
	return copyRuleCollection(acc, "firewall/rules", sourceZoneID, targetZoneID, sourceDomain, targetDomain)
}

func copyRateLimitRules(acc *models.Account, sourceZoneID, targetZoneID, sourceDomain, targetDomain string) int {
	return copyRuleCollection(acc, "rate_limits", sourceZoneID, targetZoneID, sourceDomain, targetDomain)
}

func copyRuleCollection(acc *models.Account, resource, sourceZoneID, targetZoneID, sourceDomain, targetDomain string) int {
	rules, err := listRuleMaps(acc, sourceZoneID, resource)
	if err != nil {
		return 0
	}
	count := 0
	for _, rule := range rules {
		rewriteRuleValue(rule, sourceDomain, targetDomain)
		ruleBody, err := json.Marshal(rule)
		if err != nil {
			continue
		}
		endpoint := fmt.Sprintf("https://api.cloudflare.com/client/v4/zones/%s/%s", targetZoneID, resource)
		postReq, _ := http.NewRequest("POST", endpoint, bytes.NewBuffer(ruleBody))
		postReq.Header.Add("X-Auth-Email", acc.Email)
		postReq.Header.Add("X-Auth-Key", acc.Key)
		postReq.Header.Add("Content-Type", "application/json")
		postResp, err := cloudflareClient.Do(postReq)
		if err == nil && (postResp.StatusCode == http.StatusOK || postResp.StatusCode == http.StatusCreated) {
			count++
		}
		if postResp != nil {
			postResp.Body.Close()
		}
	}
	return count
}

func rewriteRuleValue(value interface{}, sourceDomain, targetDomain string) {
	switch typed := value.(type) {
	case map[string]interface{}:
		for key, child := range typed {
			switch key {
			case "id", "created_on", "modified_on":
				delete(typed, key)
				continue
			}
			if text, ok := child.(string); ok {
				typed[key] = strings.ReplaceAll(text, sourceDomain, targetDomain)
				continue
			}
			rewriteRuleValue(child, sourceDomain, targetDomain)
		}
	case []interface{}:
		for _, child := range typed {
			rewriteRuleValue(child, sourceDomain, targetDomain)
		}
	}
}

func listRuleMaps(acc *models.Account, zoneID, resource string) ([]map[string]interface{}, error) {
	var all []map[string]interface{}
	for page := 1; ; page++ {
		endpoint := fmt.Sprintf("https://api.cloudflare.com/client/v4/zones/%s/%s?page=%d&per_page=50", zoneID, resource, page)
		req, _ := http.NewRequest("GET", endpoint, nil)
		req.Header.Add("X-Auth-Email", acc.Email)
		req.Header.Add("X-Auth-Key", acc.Key)
		resp, err := cloudflareClient.Do(req)
		if err != nil {
			return nil, err
		}
		var result struct {
			Result     []map[string]interface{} `json:"result"`
			ResultInfo struct {
				TotalPages int `json:"total_pages"`
			} `json:"result_info"`
		}
		decodeErr := json.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || decodeErr != nil {
			return nil, fmt.Errorf("Cloudflare returned an invalid rule response")
		}
		all = append(all, result.Result...)
		if len(result.Result) < 50 || (result.ResultInfo.TotalPages > 0 && page >= result.ResultInfo.TotalPages) {
			return all, nil
		}
	}
}

type BatchDeleteRulesRequest struct {
	AccountID string   `json:"accountId"`
	Domains   []string `json:"domains"`
	RuleTypes []string `json:"ruleTypes"`
}

type DeleteRulesResult struct {
	Domain  string `json:"domain"`
	Success bool   `json:"success"`
	Message string `json:"message"`
	Count   int    `json:"count"`
}

func BatchDeleteRules(c *gin.Context) {
	var req BatchDeleteRulesRequest
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

	results := make([]DeleteRulesResult, len(req.Domains))
	var wg sync.WaitGroup

	for i, domain := range req.Domains {
		wg.Add(1)
		go func(idx int, dom string) {
			defer wg.Done()
			acquireBatchSlot()
			defer releaseBatchSlot()
			success, msg, count := deleteRulesFromDomain(acc, dom, req.RuleTypes)
			results[idx] = DeleteRulesResult{
				Domain:  dom,
				Success: success,
				Message: msg,
				Count:   count,
			}
		}(i, domain)
	}

	wg.Wait()
	c.JSON(http.StatusOK, results)
}

func deleteRulesFromDomain(acc *models.Account, domain string, ruleTypes []string) (bool, string, int) {
	zoneID, err := getZoneIDByDomain(acc, domain)
	if err != nil {
		return false, "Zone not found", 0
	}

	totalDeleted := 0

	for _, ruleType := range ruleTypes {
		switch ruleType {
		case "page_rules":
			count := deletePageRules(acc, zoneID)
			totalDeleted += count
		case "firewall_rules":
			count := deleteFirewallRules(acc, zoneID)
			totalDeleted += count
		case "rate_limiting":
			count := deleteRateLimitRules(acc, zoneID)
			totalDeleted += count
		}
	}

	if totalDeleted > 0 {
		return true, fmt.Sprintf("Deleted %d rules", totalDeleted), totalDeleted
	}

	return false, "No rules found", 0
}

func deletePageRules(acc *models.Account, zoneID string) int {
	return deleteRuleCollection(acc, zoneID, "pagerules")
}

func deleteFirewallRules(acc *models.Account, zoneID string) int {
	return deleteRuleCollection(acc, zoneID, "firewall/rules")
}

func deleteRateLimitRules(acc *models.Account, zoneID string) int {
	return deleteRuleCollection(acc, zoneID, "rate_limits")
}

func deleteRuleCollection(acc *models.Account, zoneID, resource string) int {
	rules, err := listRuleMaps(acc, zoneID, resource)
	if err != nil {
		return 0
	}
	count := 0
	for _, rule := range rules {
		id, ok := rule["id"].(string)
		if !ok || id == "" {
			continue
		}
		endpoint := fmt.Sprintf("https://api.cloudflare.com/client/v4/zones/%s/%s/%s", zoneID, resource, id)
		delReq, _ := http.NewRequest("DELETE", endpoint, nil)
		delReq.Header.Add("X-Auth-Email", acc.Email)
		delReq.Header.Add("X-Auth-Key", acc.Key)
		delResp, err := cloudflareClient.Do(delReq)
		if err == nil && delResp.StatusCode == http.StatusOK {
			count++
		}
		if delResp != nil {
			delResp.Body.Close()
		}
	}
	return count
}
