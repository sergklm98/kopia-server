package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sergklm98/kopia-lib/lib/notification"
	"github.com/sergklm98/kopia-lib/lib/repo"
	"github.com/sergklm98/kopia-lib/lib/repo/blob/filesystem"
)

func openNotificationAPITestRepository(t *testing.T) repo.Repository {
	t.Helper()
	ctx := context.Background()
	storage, err := filesystem.New(ctx, &filesystem.Options{Path: t.TempDir()}, true)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, storage.Close(ctx)) })
	configPath := filepath.Join(t.TempDir(), "repository.config")
	require.NoError(t, repo.Initialize(ctx, storage, nil, "test-password"))
	require.NoError(t, repo.Connect(ctx, configPath, storage, "test-password", nil))
	repository, err := repo.Open(ctx, configPath, "test-password", nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, repository.Close(ctx)) })
	return repository
}

func TestNotificationProfileAPICRUD(t *testing.T) {
	repository := openNotificationAPITestRepository(t)
	server := httptest.NewServer(newHandler("", repository, "", "", ""))
	defer server.Close()

	method, err := notification.NewMethodConfig(notification.MethodWebhook, notification.WebhookOptions{Endpoint: "https://example.test/hook"})
	require.NoError(t, err)
	profile := notification.ProfileConfig{ProfileName: "ops", MethodConfig: method, MinSeverity: int32(notification.SeverityWarning)}
	data, err := json.Marshal(profile)
	require.NoError(t, err)
	response, err := http.Post(server.URL+"/api/v1/notificationProfiles", "application/json", strings.NewReader(string(data)))
	require.NoError(t, err)
	response.Body.Close()
	require.Equal(t, http.StatusOK, response.StatusCode)

	response, err = http.Get(server.URL + "/api/v1/notificationProfiles")
	require.NoError(t, err)
	var profiles []notification.ProfileConfig
	require.NoError(t, json.NewDecoder(response.Body).Decode(&profiles))
	response.Body.Close()
	require.Equal(t, []notification.ProfileConfig{profile}, profiles)

	response, err = http.Get(server.URL + "/api/v1/notificationProfiles/ops")
	require.NoError(t, err)
	var fetched notification.ProfileConfig
	require.NoError(t, json.NewDecoder(response.Body).Decode(&fetched))
	response.Body.Close()
	require.Equal(t, profile, fetched)

	request, err := http.NewRequest(http.MethodDelete, server.URL+"/api/v1/notificationProfiles/ops", nil)
	require.NoError(t, err)
	response, err = http.DefaultClient.Do(request)
	require.NoError(t, err)
	response.Body.Close()
	require.Equal(t, http.StatusOK, response.StatusCode)

	response, err = http.Get(server.URL + "/api/v1/notificationProfiles")
	require.NoError(t, err)
	profiles = nil
	require.NoError(t, json.NewDecoder(response.Body).Decode(&profiles))
	response.Body.Close()
	require.Empty(t, profiles)

	response, err = http.Get(server.URL + "/api/v1/notificationProfiles/ops")
	require.NoError(t, err)
	var apiError map[string]string
	require.NoError(t, json.NewDecoder(response.Body).Decode(&apiError))
	response.Body.Close()
	require.Equal(t, http.StatusInternalServerError, response.StatusCode)
	require.NotEmpty(t, apiError["error"])
}

func TestNotificationProfileTestSendsFromServer(t *testing.T) {
	requestBody := make(chan string, 1)
	webhook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		requestBody <- string(body)
		w.WriteHeader(http.StatusOK)
	}))
	defer webhook.Close()

	repository := openNotificationAPITestRepository(t)
	server := httptest.NewServer(newHandler("", repository, "", "", ""))
	defer server.Close()
	method, err := notification.NewMethodConfig(notification.MethodWebhook, notification.WebhookOptions{Endpoint: webhook.URL})
	require.NoError(t, err)
	profile := notification.ProfileConfig{ProfileName: "browser-test", MethodConfig: method}
	data, err := json.Marshal(profile)
	require.NoError(t, err)
	response, err := http.Post(server.URL+"/api/v1/testNotificationProfile", "application/json", strings.NewReader(string(data)))
	require.NoError(t, err)
	response.Body.Close()
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.Contains(t, <-requestBody, "This is a test notification from Kopia.")
}
