package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"

	"github.com/sergklm98/kopia-lib/lib/repo"
	"github.com/sergklm98/kopia-lib/lib/repo/blob"
	"github.com/sergklm98/kopia-lib/lib/repo/blob/throttling"
	"github.com/sergklm98/kopia-lib/lib/repo/compression"
	"github.com/sergklm98/kopia-lib/lib/repo/ecc"
	"github.com/sergklm98/kopia-lib/lib/repo/encryption"
	"github.com/sergklm98/kopia-lib/lib/repo/format"
	"github.com/sergklm98/kopia-lib/lib/repo/hashing"
	"github.com/sergklm98/kopia-lib/lib/repo/splitter"
)

func registerRepositoryRoutes(mux *http.ServeMux, repositories *repositoryState, configPath, password string) {
	mux.Handle("/api/v1/repo/status", repositories.readHandler(statusHandler(repositories)))
	mux.HandleFunc("/api/v1/repo/algorithms", supportedAlgorithmsHandler)
	mux.HandleFunc("/api/v1/repo/exists", repositoryExistsHandler)
	mux.Handle("/api/v1/repo/description", repositories.readHandler(repositoryDescriptionHandler(repositories, configPath)))
	mux.Handle("/api/v1/repo/throttle", repositories.readHandler(repositoryThrottleHandler(repositories)))
	mux.HandleFunc("/api/v1/repo/connect", repositoryConnectHandler(repositories, configPath))
	mux.HandleFunc("/api/v1/repo/create", repositoryCreateHandler(repositories, configPath))
	mux.HandleFunc("/api/v1/repo/disconnect", repositoryDisconnectHandler(repositories, configPath))
}

type checkRepositoryExistsRequest struct {
	Storage blob.ConnectionInfo `json:"storage"`
}

type repositoryAPIError struct {
	Code  string `json:"code"`
	Error string `json:"error"`
}

type repositoryProbeBuffer struct {
	bytes.Buffer
}

func (b *repositoryProbeBuffer) Length() int {
	return b.Len()
}

func repositoryExistsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeRepositoryAPIError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
		return
	}

	var request checkRepositoryExistsRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeRepositoryError(w, http.StatusBadRequest, "MALFORMED_REQUEST", "unable to decode request: "+err.Error())
		return
	}

	storage, err := blob.NewStorage(r.Context(), request.Storage, false)
	if err != nil {
		writeRepositoryError(w, http.StatusInternalServerError, "INTERNAL", fmt.Sprintf("internal server error: %v", err))
		return
	}
	defer storage.Close(r.Context()) //nolint:errcheck

	var probe repositoryProbeBuffer
	if err := storage.GetBlob(r.Context(), format.KopiaRepositoryBlobID, 0, -1, &probe); err != nil {
		if errors.Is(err, blob.ErrBlobNotFound) {
			writeRepositoryError(w, http.StatusBadRequest, "NOT_INITIALIZED", "repository not initialized")
			return
		}
		writeRepositoryError(w, http.StatusInternalServerError, "INTERNAL", fmt.Sprintf("internal server error: %v", err))
		return
	}

	writeRepositoryJSON(w, http.StatusOK, struct{}{})
}

func writeRepositoryError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(repositoryAPIError{Code: code, Error: message})
}

func repositoryDescriptionHandler(repositories *repositoryState, configPath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			writeRepositoryAPIError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
			return
		}
		repository := repositories.repository
		if repository == nil {
			writeRepositoryAPIError(w, http.StatusServiceUnavailable, errors.New("repository is not connected"))
			return
		}
		var request repo.ClientOptions
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			writeRepositoryAPIError(w, http.StatusBadRequest, fmt.Errorf("malformed request body: %w", err))
			return
		}
		options := repository.ClientOptions()
		options.Description = request.Description
		if err := repo.SetClientOptions(r.Context(), configPath, options); err != nil {
			writeRepositoryAPIError(w, http.StatusInternalServerError, err)
			return
		}
		repository.UpdateDescription(request.Description)
		writeRepositoryStatus(w, repository)
	}
}

func repositoryThrottleHandler(repositories *repositoryState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		repository := repositories.repository
		direct, ok := repository.(repo.DirectRepository)
		if !ok {
			writeRepositoryAPIError(w, http.StatusServiceUnavailable, errors.New("no direct storage connection"))
			return
		}
		switch r.Method {
		case http.MethodGet:
			writeRepositoryJSON(w, http.StatusOK, direct.Throttler().Limits())
		case http.MethodPut:
			var limits throttling.Limits
			if err := json.NewDecoder(r.Body).Decode(&limits); err != nil {
				writeRepositoryAPIError(w, http.StatusBadRequest, fmt.Errorf("malformed request body: %w", err))
				return
			}
			if err := direct.Throttler().SetLimits(limits); err != nil {
				writeRepositoryAPIError(w, http.StatusBadRequest, fmt.Errorf("unable to set limits: %w", err))
				return
			}
			writeRepositoryJSON(w, http.StatusOK, struct{}{})
		default:
			w.Header().Set("Allow", "GET, PUT")
			writeRepositoryAPIError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
		}
	}
}

func writeRepositoryJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeRepositoryAPIError(w http.ResponseWriter, status int, err error) {
	writeRepositoryJSON(w, status, map[string]string{"error": err.Error()})
}

type algorithmInfo struct {
	ID         string `json:"id"`
	Deprecated bool   `json:"deprecated"`
}

type supportedAlgorithmsResponse struct {
	DefaultHashAlgorithm           string          `json:"defaultHash"`
	DefaultEncryptionAlgorithm     string          `json:"defaultEncryption"`
	DefaultECCAlgorithm            string          `json:"defaultEcc"`
	DefaultSplitterAlgorithm       string          `json:"defaultSplitter"`
	SupportedHashAlgorithms        []algorithmInfo `json:"hash"`
	SupportedEncryptionAlgorithms  []algorithmInfo `json:"encryption"`
	SupportedECCAlgorithms         []algorithmInfo `json:"ecc"`
	SupportedSplitterAlgorithms    []algorithmInfo `json:"splitter"`
	SupportedCompressionAlgorithms []algorithmInfo `json:"compression"`
}

func supportedAlgorithmsHandler(w http.ResponseWriter, _ *http.Request) {
	result := supportedAlgorithmsResponse{
		DefaultHashAlgorithm:          hashing.DefaultAlgorithm,
		SupportedHashAlgorithms:       algorithmInfos(hashing.SupportedAlgorithms()),
		DefaultEncryptionAlgorithm:    encryption.DefaultAlgorithm,
		SupportedEncryptionAlgorithms: algorithmInfos(encryption.SupportedAlgorithms()),
		DefaultECCAlgorithm:           ecc.DefaultAlgorithm,
		SupportedECCAlgorithms:        algorithmInfos(ecc.SupportedAlgorithms()),
		DefaultSplitterAlgorithm:      splitter.DefaultAlgorithm,
		SupportedSplitterAlgorithms:   algorithmInfos(splitter.SupportedAlgorithms()),
	}
	for name := range compression.ByName {
		result.SupportedCompressionAlgorithms = append(result.SupportedCompressionAlgorithms, algorithmInfo{
			ID:         string(name),
			Deprecated: compression.IsDeprecated[name],
		})
	}
	sortAlgorithmInfos(result.SupportedCompressionAlgorithms)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func algorithmInfos(names []string) []algorithmInfo {
	result := make([]algorithmInfo, 0, len(names))
	for _, name := range names {
		result = append(result, algorithmInfo{ID: name})
	}
	sortAlgorithmInfos(result)
	return result
}

func sortAlgorithmInfos(algorithms []algorithmInfo) {
	sort.Slice(algorithms, func(i, j int) bool {
		if algorithms[i].Deprecated != algorithms[j].Deprecated {
			return !algorithms[i].Deprecated
		}
		return algorithms[i].ID < algorithms[j].ID
	})
}

func statusHandler(repositories *repositoryState) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		writeRepositoryStatus(w, repositories.repository)
	}
}

func writeRepositoryStatus(w http.ResponseWriter, repository repo.Repository) {
	w.Header().Set("Content-Type", "application/json")
	result := map[string]any{
		"apiVersion": "v1",
		"connected":  repository != nil,
	}
	if repository != nil {
		result["clientOptions"] = repository.ClientOptions()
		if direct, ok := repository.(repo.DirectRepository); ok {
			contentFormat := direct.ContentReader().ContentFormat()
			mutableParameters := contentFormat.GetCachedMutableParameters()
			result["configFile"] = direct.ConfigFilename()
			result["formatVersion"] = mutableParameters.Version
			result["hash"] = contentFormat.GetHashFunction()
			result["encryption"] = contentFormat.GetEncryptionAlgorithm()
			result["ecc"] = contentFormat.GetECCAlgorithm()
			result["eccOverheadPercent"] = contentFormat.GetECCOverheadPercent()
			result["maxPackSize"] = mutableParameters.MaxPackSize
			result["splitter"] = direct.ObjectFormat().Splitter
			result["storage"] = direct.BlobReader().ConnectionInfo().Type
			result["supportsContentCompression"] = direct.ContentReader().SupportsContentCompression()
		}
	}
	_ = json.NewEncoder(w).Encode(result)
}
