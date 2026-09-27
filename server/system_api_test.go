package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestUIPreferencesAPI(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "repository.config")
	handler := newHandler("", nil, configPath, "", "")

	request := httptest.NewRequest(http.MethodGet, "/api/v1/ui-preferences", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("unexpected GET status: %s", response.Result().Status)
	}
	var preferences uiPreferences
	if err := json.NewDecoder(response.Body).Decode(&preferences); err != nil {
		t.Fatal(err)
	}
	if preferences.Theme != "" || preferences.PageSize != 0 {
		t.Fatalf("expected empty preferences, got %#v", preferences)
	}

	request = httptest.NewRequest(http.MethodPut, "/api/v1/ui-preferences", strings.NewReader(`{"theme":"dark","pageSize":20,"bytesStringBase2":true}`))
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("unexpected PUT status: %s: %s", response.Result().Status, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/api/v1/ui-preferences", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if err := json.NewDecoder(response.Body).Decode(&preferences); err != nil {
		t.Fatal(err)
	}
	if preferences.Theme != "dark" || preferences.PageSize != 20 || !preferences.BytesStringBase2 {
		t.Fatalf("unexpected persisted preferences: %#v", preferences)
	}

	request = httptest.NewRequest(http.MethodPut, "/api/v1/ui-preferences", strings.NewReader("{"))
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unexpected malformed PUT status: %s", response.Result().Status)
	}
}
