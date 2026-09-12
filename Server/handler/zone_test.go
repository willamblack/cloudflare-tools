package handler

import (
	"cloudflare-tools/server/models"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestFetchAllZonesRejectsCloudflareErrors(t *testing.T) {
	previous := cloudflareClient
	defer func() { cloudflareClient = previous }()
	acc := &models.Account{Email: "test@example.com", Key: "test-key"}
	tests := []struct {
		name   string
		status int
		body   string
	}{
		{"HTTP error", http.StatusForbidden, `{"success":false,"errors":[{"message":"forbidden"}]}`},
		{"API error", http.StatusOK, `{"success":false,"result":[]}`},
		{"malformed JSON", http.StatusOK, `{"success":`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cloudflareClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: test.status, Body: io.NopCloser(strings.NewReader(test.body)), Header: make(http.Header)}, nil
			})}
			if zones, err := fetchAllZones(acc); err == nil || zones != nil {
				t.Fatalf("expected error and no zones, got zones=%v err=%v", zones, err)
			}
		})
	}
}

func TestFetchAllZonesReadsAllPages(t *testing.T) {
	previous := cloudflareClient
	defer func() { cloudflareClient = previous }()
	pageCount := 0
	cloudflareClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		pageCount++
		body := `{"success":true,"result":[{"name":"first.example"}],"result_info":{"page":1,"total_pages":2}}`
		if req.URL.Query().Get("page") == "2" {
			body = `{"success":true,"result":[{"name":"second.example"}],"result_info":{"page":2,"total_pages":2}}`
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
	zones, err := fetchAllZones(&models.Account{Email: "test@example.com", Key: "test-key"})
	if err != nil || pageCount != 2 || len(zones) != 2 || zones[0].Domain != "first.example" || zones[1].Domain != "second.example" {
		t.Fatalf("unexpected export: zones=%v pageCount=%d err=%v", zones, pageCount, err)
	}
}
