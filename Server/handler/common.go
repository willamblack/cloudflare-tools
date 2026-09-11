package handler

import (
	"cloudflare-tools/server/models"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	maxBatchItems              = 100
	maxCloudflareResponseBytes = 4 << 20
)

var cloudflareClient = &http.Client{
	Timeout: 30 * time.Second,
}

var batchSemaphore = make(chan struct{}, 8)

var domainPattern = regexp.MustCompile(`(?i)^(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

func findAccount(id string) (*models.Account, bool) {
	account, ok := models.FindAccount(strings.TrimSpace(id))
	if !ok {
		return nil, false
	}
	return &account, true
}

func validBatchSize(size int) bool {
	return size > 0 && size <= maxBatchItems
}

func validateBatch(c *gin.Context, size int) bool {
	if !validBatchSize(size) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Batch must contain between 1 and 100 items"})
		return false
	}
	return true
}

func acquireBatchSlot() {
	batchSemaphore <- struct{}{}
}

func releaseBatchSlot() {
	<-batchSemaphore
}

func validDomain(domain string) bool {
	domain = strings.TrimSuffix(strings.TrimSpace(domain), ".")
	return len(domain) <= 253 && domainPattern.MatchString(domain)
}
