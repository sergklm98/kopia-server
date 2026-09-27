package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sergklm98/kopia-lib/repo"
	"github.com/sergklm98/kopia-lib/repo/blob"
	"github.com/sergklm98/kopia-lib/repo/blob/filesystem"
	"github.com/sergklm98/kopia-lib/repo/blob/throttling"
	"github.com/sergklm98/kopia-lib/repo/compression"
	"github.com/sergklm98/kopia-lib/repo/ecc"
	"github.com/sergklm98/kopia-lib/repo/encryption"
	"github.com/sergklm98/kopia-lib/repo/hashing"
	"github.com/sergklm98/kopia-lib/repo/maintenance"
	"github.com/sergklm98/kopia-lib/repo/snapshot"
	"github.com/sergklm98/kopia-lib/repo/splitter"
)

func TestSupportedAlgorithmsAPI(t *testing.T) {
	handler := newHandler("", nil, "", "", "")
	request := httptest.NewRequest(http.MethodGet, "/api/v1/repo/algorithms", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("unexpected status: %s", response.Result().Status)
	}

	var result supportedAlgorithmsResponse
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.DefaultHashAlgorithm != hashing.DefaultAlgorithm ||
		result.DefaultEncryptionAlgorithm != encryption.DefaultAlgorithm ||
		result.DefaultECCAlgorithm != ecc.DefaultAlgorithm ||
		result.DefaultSplitterAlgorithm != splitter.DefaultAlgorithm {
		t.Fatalf("unexpected algorithm defaults: %#v", result)
	}
	if !containsAlgorithm(result.SupportedHashAlgorithms, hashing.DefaultAlgorithm) ||
		!containsAlgorithm(result.SupportedEncryptionAlgorithms, encryption.DefaultAlgorithm) ||
		!containsAlgorithm(result.SupportedECCAlgorithms, ecc.DefaultAlgorithm) ||
		!containsAlgorithm(result.SupportedSplitterAlgorithms, splitter.DefaultAlgorithm) ||
		len(result.SupportedCompressionAlgorithms) != len(compression.ByName) {
		t.Fatalf("supported algorithm lists are incomplete: %#v", result)
	}
	for _, algorithms := range [][]algorithmInfo{
		result.SupportedHashAlgorithms,
		result.SupportedEncryptionAlgorithms,
		result.SupportedECCAlgorithms,
		result.SupportedSplitterAlgorithms,
		result.SupportedCompressionAlgorithms,
	} {
		for index := 1; index < len(algorithms); index++ {
			previous, current := algorithms[index-1], algorithms[index]
			if previous.Deprecated && !current.Deprecated ||
				previous.Deprecated == current.Deprecated && previous.ID > current.ID {
				t.Fatalf("algorithm list is not sorted: %#v", algorithms)
			}
		}
	}
}

func containsAlgorithm(algorithms []algorithmInfo, target string) bool {
	for _, algorithm := range algorithms {
		if algorithm.ID == target {
			return true
		}
	}
	return false
}

func TestRepositoryDescriptionAPI(t *testing.T) {
	repository := openNotificationAPITestRepository(t)
	direct := repository.(repo.DirectRepository)
	handler := newHandler("", repository, direct.ConfigFilename(), "", "")
	request := httptest.NewRequest(http.MethodPost, "/api/v1/repo/description", strings.NewReader(`{"description":"production"}`))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("unexpected description status: %s: %s", response.Result().Status, response.Body.String())
	}
	if repository.ClientOptions().Description != "production" {
		t.Fatalf("description was not applied to repository: %#v", repository.ClientOptions())
	}
	config, err := repo.LoadConfigFromFile(direct.ConfigFilename())
	if err != nil {
		t.Fatal(err)
	}
	if config.Description != "production" {
		t.Fatalf("description was not persisted: %#v", config.ClientOptions)
	}
}

func TestRepositoryThrottleAPI(t *testing.T) {
	repository := openNotificationAPITestRepository(t)
	direct := repository.(repo.DirectRepository)
	handler := newHandler("", repository, direct.ConfigFilename(), "", "")
	want := throttling.Limits{
		ReadsPerSecond:         10,
		WritesPerSecond:        11,
		ListsPerSecond:         12,
		UploadBytesPerSecond:   1024,
		DownloadBytesPerSecond: 2048,
		ConcurrentReads:        3,
		ConcurrentWrites:       4,
	}
	body, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPut, "/api/v1/repo/throttle", strings.NewReader(string(body)))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("unexpected throttle PUT status: %s: %s", response.Result().Status, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/api/v1/repo/throttle", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var got throttling.Limits
	if err := json.NewDecoder(response.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("unexpected throttle limits: got %#v, want %#v", got, want)
	}
}

func TestRepositoryExistsAPI(t *testing.T) {
	repository := openNotificationAPITestRepository(t)
	direct := repository.(repo.DirectRepository)
	storageInfo := direct.BlobReader().ConnectionInfo()
	body, err := json.Marshal(map[string]any{"storage": storageInfo})
	if err != nil {
		t.Fatal(err)
	}
	handler := newHandler("", nil, "", "", "")
	request := httptest.NewRequest(http.MethodPost, "/api/v1/repo/exists", strings.NewReader(string(body)))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("unexpected initialized repository status: %s: %s", response.Result().Status, response.Body.String())
	}

	storageInfo.Config = map[string]any{"path": t.TempDir()}
	body, err = json.Marshal(map[string]any{"storage": storageInfo})
	if err != nil {
		t.Fatal(err)
	}
	request = httptest.NewRequest(http.MethodPost, "/api/v1/repo/exists", strings.NewReader(string(body)))
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unexpected uninitialized repository status: %s", response.Result().Status)
	}
	var apiError repositoryAPIError
	if err := json.NewDecoder(response.Body).Decode(&apiError); err != nil {
		t.Fatal(err)
	}
	if apiError.Code != "NOT_INITIALIZED" || apiError.Error != "repository not initialized" {
		t.Fatalf("unexpected API error: %#v", apiError)
	}

	request = httptest.NewRequest(http.MethodPost, "/api/v1/repo/exists", strings.NewReader("{"))
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unexpected malformed request status: %s", response.Result().Status)
	}
}

func newFilesystemConnection(t *testing.T) (blob.ConnectionInfo, string) {
	t.Helper()
	storagePath := filepath.Join(t.TempDir(), "storage")
	return blob.ConnectionInfo{
		Type:   "filesystem",
		Config: &filesystem.Options{Path: storagePath},
	}, storagePath
}

func newRepositoryTestServer(t *testing.T) *Server {
	t.Helper()
	server, err := New(Config{
		RepositoryConfigPath: filepath.Join(t.TempDir(), "repository.config"),
		RepositoryPassword:   "repository-password",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := server.CloseRepository(context.Background()); err != nil {
			t.Errorf("close repository: %v", err)
		}
	})
	return server
}

func performJSONRequest(t *testing.T, handler http.Handler, method, path string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	var body strings.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		body = *strings.NewReader(string(data))
	}
	request := httptest.NewRequest(method, path, &body)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestRepositoryConnectAndDisconnectAPI(t *testing.T) {
	ctx := context.Background()
	connection, _ := newFilesystemConnection(t)
	storage, err := blob.NewStorage(ctx, connection, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Initialize(ctx, storage, nil, "repository-password"); err != nil {
		t.Fatal(err)
	}
	if err := storage.Close(ctx); err != nil {
		t.Fatal(err)
	}

	server := newRepositoryTestServer(t)
	response := performJSONRequest(t, server.http.Handler, http.MethodPost, "/api/v1/repo/connect", repositoryConnectRequest{
		Storage:  connection,
		Password: "repository-password",
	})
	if response.Code != http.StatusOK {
		t.Fatalf("unexpected connect status: %s: %s", response.Result().Status, response.Body.String())
	}
	var status map[string]any
	if err := json.NewDecoder(response.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	if status["connected"] != true {
		t.Fatalf("connect response did not show connected repository: %#v", status)
	}

	response = performJSONRequest(t, server.http.Handler, http.MethodGet, "/api/v1/notificationProfiles", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("notification route did not see newly connected repository: %s", response.Result().Status)
	}

	response = performJSONRequest(t, server.http.Handler, http.MethodPost, "/api/v1/repo/disconnect", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("unexpected disconnect status: %s: %s", response.Result().Status, response.Body.String())
	}
	response = performJSONRequest(t, server.http.Handler, http.MethodGet, "/api/v1/repo/status", nil)
	if err := json.NewDecoder(response.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	if status["connected"] != false {
		t.Fatalf("disconnect did not clear repository state: %#v", status)
	}
	if _, err := os.Stat(server.config.RepositoryConfigPath); !os.IsNotExist(err) {
		t.Fatalf("repository config still exists after disconnect: %v", err)
	}
}

func TestRepositoryCreateAPIInitializesDefaults(t *testing.T) {
	ctx := context.Background()
	connection, _ := newFilesystemConnection(t)
	server := newRepositoryTestServer(t)
	response := performJSONRequest(t, server.http.Handler, http.MethodPost, "/api/v1/repo/create", repositoryCreateRequest{
		Storage:  connection,
		Password: "repository-password",
	})
	if response.Code != http.StatusOK {
		t.Fatalf("unexpected create status: %s: %s", response.Result().Status, response.Body.String())
	}

	effectivePolicy, err := snapshot.ResolvePersistedPolicy(ctx, server.repositories.repository, snapshot.SourceInfo{})
	if err != nil {
		t.Fatalf("global default policy was not initialized: %v", err)
	}
	if effectivePolicy.Retention != snapshot.DefaultRetentionPolicy() ||
		effectivePolicy.Compression.CompressorName != "none" ||
		effectivePolicy.MetadataCompression.CompressorName != "zstd-fastest" ||
		len(effectivePolicy.Files.DotIgnoreFiles) != 1 || effectivePolicy.Files.DotIgnoreFiles[0] != ".kopiaignore" ||
		effectivePolicy.ErrorHandling.IgnoreFileErrors == nil || *effectivePolicy.ErrorHandling.IgnoreFileErrors ||
		effectivePolicy.ErrorHandling.IgnoreDirectoryErrors == nil || *effectivePolicy.ErrorHandling.IgnoreDirectoryErrors ||
		effectivePolicy.ErrorHandling.IgnoreUnknownTypes == nil || !*effectivePolicy.ErrorHandling.IgnoreUnknownTypes ||
		effectivePolicy.Scheduling.RunMissed == nil || !*effectivePolicy.Scheduling.RunMissed ||
		effectivePolicy.Logging.Directories.Snapshotted == nil || *effectivePolicy.Logging.Directories.Snapshotted != snapshot.LogDetailNormal ||
		effectivePolicy.Logging.Entries.Snapshotted == nil || *effectivePolicy.Logging.Entries.Snapshotted != snapshot.LogDetailNone ||
		effectivePolicy.OSSnapshot.VolumeShadowCopy.Enable == nil || *effectivePolicy.OSSnapshot.VolumeShadowCopy.Enable != snapshot.OSSnapshotNever ||
		effectivePolicy.Upload.MaxParallelSnapshots == nil || *effectivePolicy.Upload.MaxParallelSnapshots != 1 ||
		effectivePolicy.Upload.ParallelUploadAboveSize == nil || *effectivePolicy.Upload.ParallelUploadAboveSize != 2<<30 {
		t.Fatalf("unexpected effective default policy: %#v", effectivePolicy)
	}
	params, err := maintenance.GetParams(ctx, server.repositories.repository)
	if err != nil {
		t.Fatal(err)
	}
	if params.Owner != server.repositories.repository.ClientOptions().UsernameAtHost() {
		t.Fatalf("unexpected maintenance owner %q", params.Owner)
	}
}
