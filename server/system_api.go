package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/natefinch/atomic"

	"github.com/sergklm98/kopia-lib/lib/repo"
)

func registerSystemRoutes(mux *http.ServeMux, configPath string) {
	mux.HandleFunc("/api/v1/cli", cliInfoHandler(configPath))
	mux.HandleFunc("/api/v1/current-user", currentUserHandler)
	preferencesPath := ""
	if configPath != "" {
		preferencesPath = filepath.Join(filepath.Dir(configPath), "ui-preferences.json")
	}
	mux.HandleFunc("/api/v1/ui-preferences", uiPreferencesHandler(preferencesPath))
}

type uiPreferences struct {
	BytesStringBase2       bool   `json:"bytesStringBase2"`
	DefaultSnapshotViewAll bool   `json:"defaultSnapshotViewAll"`
	Theme                  string `json:"theme"`
	FontSize               string `json:"fontSize"`
	PageSize               int    `json:"pageSize"`
	Language               string `json:"language"`
	Locale                 string `json:"locale"`
}

func uiPreferencesHandler(path string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			preferences, err := readUIPreferences(path)
			if err != nil {
				writeSystemAPIError(w, http.StatusInternalServerError, err)
				return
			}
			writeSystemJSON(w, http.StatusOK, preferences)
		case http.MethodPut:
			body, err := io.ReadAll(r.Body)
			if err != nil {
				writeSystemAPIError(w, http.StatusBadRequest, errors.New("malformed request"))
				return
			}
			var preferences uiPreferences
			if err := json.Unmarshal(body, &preferences); err != nil {
				writeSystemAPIError(w, http.StatusBadRequest, errors.New("malformed request"))
				return
			}
			if err := atomic.WriteFile(path, bytes.NewReader(body)); err != nil {
				writeSystemAPIError(w, http.StatusInternalServerError, err)
				return
			}
			writeSystemJSON(w, http.StatusOK, struct{}{})
		default:
			w.Header().Set("Allow", "GET, PUT")
			writeSystemAPIError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
		}
	}
}

func readUIPreferences(path string) (uiPreferences, error) {
	var preferences uiPreferences
	if path == "" {
		return preferences, nil
	}

	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return preferences, nil
	}
	if err != nil {
		return preferences, fmt.Errorf("unable to open UI preferences file: %w", err)
	}
	defer file.Close() //nolint:errcheck

	if err := json.NewDecoder(file).Decode(&preferences); err != nil {
		return preferences, fmt.Errorf("invalid UI preferences file: %w", err)
	}
	return preferences, nil
}

func writeSystemJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeSystemAPIError(w http.ResponseWriter, status int, err error) {
	writeSystemJSON(w, status, map[string]string{"error": err.Error()})
}

func currentUserHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"username": repo.GetDefaultUserName(ctx),
		"hostname": repo.GetDefaultHostName(ctx),
	})
}

func cliInfoHandler(configPath string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		executable, err := os.Executable()
		if err != nil {
			executable = "kopia"
		}
		if strings.Contains(executable, " ") {
			executable = `"` + executable + `"`
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"executable": executable + " --config-file=" + quoteIfNeeded(configPath),
		})
	}
}

func quoteIfNeeded(value string) string {
	if strings.Contains(value, " ") {
		return `"` + value + `"`
	}
	return value
}
