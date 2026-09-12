package handler

import (
	"cloudflare-tools/server/models"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestDeleteDNSRecordsReportsPartialFailure(t *testing.T) {
	previous := cloudflareClient
	defer func() { cloudflareClient = previous }()
	for _, failureStatus := range []int{http.StatusForbidden, http.StatusOK} {
		t.Run(http.StatusText(failureStatus), func(t *testing.T) {
			cloudflareClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				status := http.StatusOK
				body := `{"success":true,"result":[{"id":"zone-id"}]}`
				switch {
				case req.Method == http.MethodGet && strings.Contains(req.URL.Path, "/dns_records"):
					body = `{"success":true,"result":[{"id":"record-1"},{"id":"record-2"}],"result_info":{"total_pages":1}}`
				case req.Method == http.MethodDelete && strings.HasSuffix(req.URL.Path, "/record-2"):
					status = failureStatus
					body = `{"success":false}`
				case req.Method == http.MethodDelete:
					body = `{"success":true}`
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			})}
			success, message, count := deleteDNSRecords(&models.Account{Email: "test@example.com", Key: "test-key"}, "example.com", "A", "@", false)
			if success || count != 1 || !strings.Contains(message, "1 of 2") {
				t.Fatalf("partial deletion was not reported: success=%v message=%q count=%d", success, message, count)
			}
		})
	}
}

func TestDNSLookupRejectsCloudflareSuccessFalse(t *testing.T) {
	previous := cloudflareClient
	defer func() { cloudflareClient = previous }()
	acc := &models.Account{Email: "test@example.com", Key: "test-key"}
	cloudflareClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"success":false,"result":[{"id":"wrong"}]}`)), Header: make(http.Header)}, nil
	})}
	if zoneID, err := getZoneID(acc, "example.com"); err == nil || zoneID != "" {
		t.Fatalf("zone lookup accepted unsuccessful response: zoneID=%q err=%v", zoneID, err)
	}
	if records, err := listDNSRecords(acc, "zone-id", "A", "example.com"); err == nil || records != nil {
		t.Fatalf("DNS list accepted unsuccessful response: records=%v err=%v", records, err)
	}
}
