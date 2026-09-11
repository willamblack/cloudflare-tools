package handler

import (
	"bytes"
	"cloudflare-tools/server/models"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
)

type BatchParseDNSRequest struct {
	AccountID   string   `json:"accountId"`
	Records     []string `json:"records"`
	TTL         int      `json:"ttl"`
	Proxied     bool     `json:"proxied"`
	DeleteOld   bool     `json:"deleteOld"`
	OfflineMode bool     `json:"offlineMode"`
}

type BatchDeleteDNSRequest struct {
	AccountID  string   `json:"accountId"`
	Domains    []string `json:"domains"`
	RecordType string   `json:"recordType"`
	HostRecord string   `json:"hostRecord"`
	DeleteAll  bool     `json:"deleteAll"`
}

type BatchProxyToggleRequest struct {
	AccountID   string   `json:"accountId"`
	Domains     []string `json:"domains"`
	RecordType  string   `json:"recordType"`
	HostRecord  string   `json:"hostRecord"`
	ProxyStatus bool     `json:"proxyStatus"`
}

type DNSRecord struct {
	Domain   string `json:"domain"`
	Host     string `json:"host"`
	Type     string `json:"type"`
	Value    string `json:"value"`
	Priority *int   `json:"priority,omitempty"`
}

type DNSResult struct {
	Domain  string `json:"domain"`
	Host    string `json:"host"`
	Type    string `json:"type"`
	Value   string `json:"value"`
	Success bool   `json:"success"`
	Message string `json:"message"`
}

type DeleteResult struct {
	Domain  string `json:"domain"`
	Success bool   `json:"success"`
	Message string `json:"message"`
	Count   int    `json:"count"`
}

type ProxyToggleResult struct {
	Domain  string `json:"domain"`
	Success bool   `json:"success"`
	Message string `json:"message"`
	Count   int    `json:"count"`
}

type cloudflareDNSRecord struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Proxied bool   `json:"proxied"`
}

type cloudflareDNSListResponse struct {
	Result     []cloudflareDNSRecord `json:"result"`
	ResultInfo struct {
		Page       int `json:"page"`
		TotalPages int `json:"total_pages"`
	} `json:"result_info"`
}

func BatchParseDNS(c *gin.Context) {
	var req BatchParseDNSRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}
	if !validateBatch(c, len(req.Records)) {
		return
	}

	records := parseRecords(req.Records)
	if len(records) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No valid records"})
		return
	}
	if req.OfflineMode {
		results := make([]DNSResult, len(records))
		for i, rec := range records {
			results[i] = DNSResult{
				Domain: rec.Domain, Host: rec.Host, Type: rec.Type, Value: rec.Value,
				Success: true, Message: "离线检查通过，未向 Cloudflare 写入任何记录",
			}
		}
		c.JSON(http.StatusOK, results)
		return
	}

	acc, ok := findAccount(req.AccountID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "Account not found"})
		return
	}

	results := make([]DNSResult, len(records))
	var wg sync.WaitGroup

	for i, record := range records {
		wg.Add(1)
		go func(idx int, rec DNSRecord) {
			defer wg.Done()
			acquireBatchSlot()
			defer releaseBatchSlot()
			success, msg := addDNSRecord(acc, rec, req.TTL, req.Proxied, req.DeleteOld)
			results[idx] = DNSResult{
				Domain:  rec.Domain,
				Host:    rec.Host,
				Type:    rec.Type,
				Value:   rec.Value,
				Success: success,
				Message: msg,
			}
		}(i, record)
	}

	wg.Wait()
	c.JSON(http.StatusOK, results)
}

func parseRecords(lines []string) []DNSRecord {
	var records []DNSRecord
	for _, line := range lines {
		parts := strings.Split(line, "|")
		if len(parts) < 4 {
			continue
		}
		rec := DNSRecord{
			Domain: strings.TrimSpace(parts[0]),
			Host:   strings.TrimSpace(parts[1]),
			Type:   strings.TrimSpace(parts[2]),
			Value:  strings.TrimSpace(parts[3]),
		}
		if len(parts) >= 5 {
			var p int
			_, err := fmt.Sscanf(strings.TrimSpace(parts[4]), "%d", &p)
			if err == nil {
				rec.Priority = &p
			}
		} else if strings.ToUpper(rec.Type) == "MX" {
			default_priority := 10
			rec.Priority = &default_priority
		}
		records = append(records, rec)
	}
	return records
}

func addDNSRecord(acc *models.Account, record DNSRecord, ttl int, proxied bool, deleteOld bool) (bool, string) {
	zoneID, err := getZoneID(acc, record.Domain)
	if err != nil {
		return false, err.Error()
	}

	if deleteOld {
		if err := deleteExistingRecords(acc, zoneID, record.Host, record.Type); err != nil {
			return false, "Delete old records failed: " + err.Error()
		}
	}

	if ttl == 0 {
		ttl = 1
	}

	payload := map[string]interface{}{
		"type":    record.Type,
		"name":    record.Host,
		"content": record.Value,
		"ttl":     ttl,
		"proxied": proxied,
	}
	if strings.ToUpper(record.Type) == "MX" {
		payload["proxied"] = false
	}
	if record.Priority != nil {
		payload["priority"] = *record.Priority
	}
	body, _ := json.Marshal(payload)

	req, _ := http.NewRequest("POST", fmt.Sprintf("https://api.cloudflare.com/client/v4/zones/%s/dns_records", zoneID), bytes.NewBuffer(body))
	req.Header.Add("X-Auth-Email", acc.Email)
	req.Header.Add("X-Auth-Key", acc.Key)
	req.Header.Add("Content-Type", "application/json")

	client := cloudflareClient
	resp, err := client.Do(req)
	if err != nil {
		return false, "Request failed"
	}
	defer resp.Body.Close()

	respBody, _ := ioutil.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusOK {
		return true, "Success"
	}

	var errorRes struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	json.Unmarshal(respBody, &errorRes)

	msg := "Unknown error"
	if len(errorRes.Errors) > 0 {
		msg = errorRes.Errors[0].Message
	} else {
		msg = fmt.Sprintf("HTTP %d", resp.StatusCode)
	}

	return false, msg
}

func getZoneID(acc *models.Account, domain string) (string, error) {
	req, _ := http.NewRequest("GET", fmt.Sprintf("https://api.cloudflare.com/client/v4/zones?name=%s", url.QueryEscape(domain)), nil)
	req.Header.Add("X-Auth-Email", acc.Email)
	req.Header.Add("X-Auth-Key", acc.Key)

	client := cloudflareClient
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("Request failed")
	}
	defer resp.Body.Close()

	var result struct {
		Result []struct {
			ID string `json:"id"`
		} `json:"result"`
	}

	body, _ := ioutil.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Cloudflare returned HTTP %d", resp.StatusCode)
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("Invalid Cloudflare response")
	}

	if len(result.Result) == 0 {
		return "", fmt.Errorf("Zone not found")
	}

	return result.Result[0].ID, nil
}

func deleteExistingRecords(acc *models.Account, zoneID string, name string, recordType string) error {
	records, err := listDNSRecords(acc, zoneID, recordType, name)
	if err != nil {
		return err
	}
	for _, record := range records {
		if err := deleteCloudflareDNSRecord(acc, zoneID, record.ID); err != nil {
			return err
		}
	}
	return nil
}

func BatchDeleteDNS(c *gin.Context) {
	var req BatchDeleteDNSRequest
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

	if len(req.Domains) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No domains provided"})
		return
	}

	results := make([]DeleteResult, len(req.Domains))
	var wg sync.WaitGroup

	for i, domain := range req.Domains {
		wg.Add(1)
		go func(idx int, dom string) {
			defer wg.Done()
			acquireBatchSlot()
			defer releaseBatchSlot()
			success, msg, count := deleteDNSRecords(acc, dom, req.RecordType, req.HostRecord, req.DeleteAll)
			results[idx] = DeleteResult{
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

func deleteDNSRecords(acc *models.Account, domain string, recordType string, hostRecord string, deleteAll bool) (bool, string, int) {
	zoneID, err := getZoneID(acc, domain)
	if err != nil {
		return false, err.Error(), 0
	}

	if deleteAll {
		recordType, hostRecord = "", ""
	}
	records, err := listDNSRecords(acc, zoneID, recordType, hostRecord)
	if err != nil {
		return false, err.Error(), 0
	}
	if len(records) == 0 {
		return false, "No records found", 0
	}

	count := 0
	for _, record := range records {
		if err := deleteCloudflareDNSRecord(acc, zoneID, record.ID); err == nil {
			count++
		}
	}

	if count > 0 {
		return true, fmt.Sprintf("Deleted %d records", count), count
	}
	return false, "Failed to delete records", 0
}

func BatchProxyToggle(c *gin.Context) {
	var req BatchProxyToggleRequest
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

	if len(req.Domains) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No domains provided"})
		return
	}

	results := make([]ProxyToggleResult, len(req.Domains))
	var wg sync.WaitGroup

	for i, domain := range req.Domains {
		wg.Add(1)
		go func(idx int, dom string) {
			defer wg.Done()
			acquireBatchSlot()
			defer releaseBatchSlot()
			success, msg, count := toggleProxyStatus(acc, dom, req.RecordType, req.HostRecord, req.ProxyStatus)
			results[idx] = ProxyToggleResult{
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

func toggleProxyStatus(acc *models.Account, domain string, recordType string, hostRecord string, proxyStatus bool) (bool, string, int) {
	zoneID, err := getZoneID(acc, domain)
	if err != nil {
		return false, err.Error(), 0
	}

	records, err := listDNSRecords(acc, zoneID, recordType, hostRecord)
	if err != nil {
		return false, err.Error(), 0
	}
	if len(records) == 0 {
		return false, "No records found", 0
	}

	count := 0
	for _, record := range records {
		if record.Type == "A" || record.Type == "AAAA" || record.Type == "CNAME" {
			payload := map[string]interface{}{
				"proxied": proxyStatus,
			}
			patchBody, _ := json.Marshal(payload)

			patchReq, _ := http.NewRequest("PATCH", fmt.Sprintf("https://api.cloudflare.com/client/v4/zones/%s/dns_records/%s", zoneID, record.ID), bytes.NewBuffer(patchBody))
			patchReq.Header.Add("X-Auth-Email", acc.Email)
			patchReq.Header.Add("X-Auth-Key", acc.Key)
			patchReq.Header.Add("Content-Type", "application/json")

			patchResp, err := cloudflareClient.Do(patchReq)
			if err == nil && patchResp.StatusCode == http.StatusOK {
				count++
			}
			if patchResp != nil {
				patchResp.Body.Close()
			}
		}
	}

	if count > 0 {
		status := "开启"
		if !proxyStatus {
			status = "关闭"
		}
		return true, fmt.Sprintf("%s代理 %d 条记录", status, count), count
	}
	return false, "No proxiable records found", 0
}

func listDNSRecords(acc *models.Account, zoneID, recordType, name string) ([]cloudflareDNSRecord, error) {
	var records []cloudflareDNSRecord
	for page := 1; ; page++ {
		params := url.Values{"page": {fmt.Sprint(page)}, "per_page": {"100"}}
		if recordType != "" {
			params.Set("type", recordType)
		}
		if name != "" {
			params.Set("name", name)
		}
		endpoint := fmt.Sprintf("https://api.cloudflare.com/client/v4/zones/%s/dns_records?%s", zoneID, params.Encode())
		req, _ := http.NewRequest("GET", endpoint, nil)
		req.Header.Add("X-Auth-Email", acc.Email)
		req.Header.Add("X-Auth-Key", acc.Key)
		resp, err := cloudflareClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("request failed")
		}
		var result cloudflareDNSListResponse
		decodeErr := json.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("Cloudflare returned HTTP %d", resp.StatusCode)
		}
		if decodeErr != nil {
			return nil, fmt.Errorf("invalid Cloudflare response")
		}
		records = append(records, result.Result...)
		if len(result.Result) < 100 || (result.ResultInfo.TotalPages > 0 && page >= result.ResultInfo.TotalPages) {
			return records, nil
		}
	}
}

func deleteCloudflareDNSRecord(acc *models.Account, zoneID, recordID string) error {
	req, _ := http.NewRequest("DELETE", fmt.Sprintf("https://api.cloudflare.com/client/v4/zones/%s/dns_records/%s", zoneID, recordID), nil)
	req.Header.Add("X-Auth-Email", acc.Email)
	req.Header.Add("X-Auth-Key", acc.Key)
	resp, err := cloudflareClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Cloudflare returned HTTP %d", resp.StatusCode)
	}
	return nil
}
