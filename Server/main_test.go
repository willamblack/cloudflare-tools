package main

import (
	"bytes"
	"cloudflare-tools/server/config"
	"cloudflare-tools/server/handler"
	"cloudflare-tools/server/models"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAuthenticatedAccountEdit(t *testing.T) {
	t.Setenv("DATA_DIR", t.TempDir())
	t.Setenv("ADMIN_USERNAME", "editor")
	t.Setenv("ADMIN_PASSWORD", "test-password-with-enough-length")
	t.Setenv("JWT_SECRET", "test-jwt-secret-with-more-than-32-characters")
	if err := config.LoadConfig(); err != nil {
		t.Fatal(err)
	}
	if err := models.LoadAccounts(); err != nil {
		t.Fatal(err)
	}
	if _, err := handler.InitializeJWTSecret(); err != nil {
		t.Fatal(err)
	}
	router := newRouter()

	request := func(method, path, body, token string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		return response
	}

	login := request(http.MethodPost, "/api/login", `{"username":"editor","password":"test-password-with-enough-length"}`, "")
	if login.Code != http.StatusOK {
		t.Fatalf("login failed: %d %s", login.Code, login.Body.String())
	}
	var loginResult struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(login.Body.Bytes(), &loginResult); err != nil || loginResult.Token == "" {
		t.Fatalf("login token missing: %v", err)
	}

	if got := request(http.MethodPost, "/api/accounts", `{"id":"spoofed","name":"x","email":"x@example.com","key":"x"}`, loginResult.Token); got.Code != http.StatusBadRequest {
		t.Fatalf("create with caller-supplied ID returned %d", got.Code)
	}
	created := request(http.MethodPost, "/api/accounts", `{"name":"original","email":"old@example.com","key":"original-secret"}`, loginResult.Token)
	if created.Code != http.StatusOK || strings.Contains(created.Body.String(), "original-secret") {
		t.Fatalf("create failed or leaked key: %d %s", created.Code, created.Body.String())
	}
	var account models.Account
	if err := json.Unmarshal(created.Body.Bytes(), &account); err != nil || account.ID == "" {
		t.Fatalf("account ID missing: %v", err)
	}
	path := "/api/accounts/" + account.ID
	if got := request(http.MethodPut, path, `{"name":"edited","email":"new@example.com","key":""}`, ""); got.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated edit returned %d", got.Code)
	}
	updated := request(http.MethodPut, path, `{"name":"edited","email":"new@example.com","key":""}`, loginResult.Token)
	if updated.Code != http.StatusOK || strings.Contains(updated.Body.String(), "original-secret") {
		t.Fatalf("edit failed or leaked key: %d %s", updated.Code, updated.Body.String())
	}
	stored, ok := models.FindAccount(account.ID)
	if !ok || stored.Name != "edited" || stored.Email != "new@example.com" || stored.Key != "original-secret" {
		t.Fatalf("blank key was not preserved: %#v", stored)
	}
	updated = request(http.MethodPut, path, `{"name":"edited","email":"new@example.com","key":"replacement-secret"}`, loginResult.Token)
	if updated.Code != http.StatusOK || strings.Contains(updated.Body.String(), "replacement-secret") {
		t.Fatalf("key replacement failed or leaked key: %d %s", updated.Code, updated.Body.String())
	}
	if got := request(http.MethodPut, "/api/accounts/missing", `{"name":"x","email":"x@example.com"}`, loginResult.Token); got.Code != http.StatusNotFound {
		t.Fatalf("missing account edit returned %d", got.Code)
	}
	if err := models.LoadAccounts(); err != nil {
		t.Fatal(err)
	}
	stored, ok = models.FindAccount(account.ID)
	if !ok || stored.Key != "replacement-secret" {
		t.Fatalf("replacement key did not persist: %#v", stored)
	}
}
