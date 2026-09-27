package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/sergklm98/kopia-lib/repo"
	"github.com/sergklm98/kopia-lib/repo/snapshot"
)

func registerPathRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/paths/resolve", pathResolveHandler)
}

type resolvePathRequest struct {
	Path string `json:"path"`
}

type resolvePathResponse struct {
	Source snapshot.SourceInfo `json:"source"`
}

func pathResolveHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeRepositoryAPIError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
		return
	}
	var request resolvePathRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeRepositoryError(w, http.StatusBadRequest, "MALFORMED_REQUEST", "malformed request body")
		return
	}

	home, _ := os.UserHomeDir()
	path := resolveUserFriendlyPath(request.Path, home)
	writeRepositoryJSON(w, http.StatusOK, resolvePathResponse{Source: snapshot.SourceInfo{
		Host:     repo.GetDefaultHostName(r.Context()),
		UserName: repo.GetDefaultUserName(r.Context()),
		Path:     path,
	}})
}

func resolveUserFriendlyPath(path, home string) string {
	if home != "" && strings.HasPrefix(path, "~") {
		return filepath.Clean(home + path[1:])
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Clean(filepath.Join(home, path))
}
