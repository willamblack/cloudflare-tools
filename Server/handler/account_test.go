package handler

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestAccountConnectionChecksCloudflareSuccessField(t *testing.T) {
	previous := cloudflareClient
	defer func() { cloudflareClient = previous }()
	responseBody := `{"success":false,"errors":[{"message":"invalid key"}]}`
	cloudflareClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("X-Auth-Key") != "test-secret" {
			t.Error("API key was not sent to Cloudflare")
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(responseBody)), Header: make(http.Header)}, nil
	})}
	router := gin.New()
	router.POST("/test", TestAccount)

	check := func(wantSuccess bool) {
		t.Helper()
		request := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(`{"email":"test@example.com","key":"test-secret"}`))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusOK || strings.Contains(response.Body.String(), `"success":true`) != wantSuccess {
			t.Fatalf("unexpected Cloudflare result: %d %s", response.Code, response.Body.String())
		}
	}
	check(false)
	responseBody = `{"success":true,"result":[]}`
	check(true)
}
