package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/sergklm98/kopia-lib/lib/repo"
	"github.com/sergklm98/kopia-lib/lib/repo/blob"
	"github.com/sergklm98/kopia-lib/lib/repo/maintenance"
	"github.com/sergklm98/kopia-lib/lib/repo/snapshot"
)

type repositoryConnectRequest struct {
	Storage       blob.ConnectionInfo `json:"storage"`
	Password      string              `json:"password"`
	Token         string              `json:"token"`
	APIServer     *repo.APIServerInfo `json:"apiServer"`
	ClientOptions repo.ClientOptions  `json:"clientOptions"`
}

type repositoryCreateRequest struct {
	Storage       blob.ConnectionInfo       `json:"storage"`
	Password      string                    `json:"password"`
	Token         string                    `json:"token"`
	APIServer     *repo.APIServerInfo       `json:"apiServer"`
	ClientOptions repo.ClientOptions        `json:"clientOptions"`
	Options       repo.NewRepositoryOptions `json:"options"`
}

func repositoryConnectHandler(repositories *repositoryState, configPath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			writeRepositoryAPIError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
			return
		}
		repositories.mu.Lock()
		defer repositories.mu.Unlock()
		if repositories.repository != nil {
			writeRepositoryError(w, http.StatusBadRequest, "ALREADY_CONNECTED", "already connected")
			return
		}
		if configPath == "" {
			writeRepositoryError(w, http.StatusInternalServerError, "INTERNAL", "repository config path is not configured")
			return
		}

		var request repositoryConnectRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			writeRepositoryError(w, http.StatusBadRequest, "MALFORMED_REQUEST", "unable to decode request: "+err.Error())
			return
		}
		if request.APIServer != nil {
			writeRepositoryError(w, http.StatusNotImplemented, "INTERNAL", "remote repository connections are not supported by kopia-lib")
			return
		}
		if !decodeRepositoryToken(&request.Storage, &request.Password, request.Token, w) {
			return
		}

		connected, err := connectRepository(r.Context(), configPath, request.Storage, request.Password, request.ClientOptions)
		if err != nil {
			writeRepositoryConnectError(w, err)
			return
		}
		repositories.repository = connected
		writeRepositoryStatus(w, connected)
	}
}

func repositoryCreateHandler(repositories *repositoryState, configPath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			writeRepositoryAPIError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
			return
		}
		repositories.mu.Lock()
		defer repositories.mu.Unlock()
		if repositories.repository != nil {
			writeRepositoryError(w, http.StatusBadRequest, "ALREADY_CONNECTED", "already connected")
			return
		}
		if configPath == "" {
			writeRepositoryError(w, http.StatusInternalServerError, "INTERNAL", "repository config path is not configured")
			return
		}

		var request repositoryCreateRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			writeRepositoryError(w, http.StatusBadRequest, "MALFORMED_REQUEST", "unable to decode request: "+err.Error())
			return
		}
		if request.APIServer != nil {
			writeRepositoryError(w, http.StatusNotImplemented, "INTERNAL", "remote repository creation is not supported")
			return
		}
		if !decodeRepositoryToken(&request.Storage, &request.Password, request.Token, w) {
			return
		}

		storage, err := blob.NewStorage(r.Context(), request.Storage, true)
		if err != nil {
			writeRepositoryError(w, http.StatusBadRequest, "STORAGE_CONNECTION", "unable to connect to storage: "+err.Error())
			return
		}
		defer storage.Close(r.Context()) //nolint:errcheck

		if err := repo.Initialize(r.Context(), storage, &request.Options, request.Password); err != nil {
			writeRepositoryConnectError(w, err)
			return
		}
		connected, err := connectRepositoryWithStorage(r.Context(), configPath, storage, request.Password, request.ClientOptions)
		if err != nil {
			writeRepositoryConnectError(w, err)
			return
		}
		repositories.repository = connected
		if err := initializeNewRepository(r.Context(), connected); err != nil {
			writeRepositoryError(w, http.StatusInternalServerError, "INTERNAL", fmt.Sprintf("internal server error: %v", err))
			return
		}
		writeRepositoryStatus(w, connected)
	}
}

func repositoryDisconnectHandler(repositories *repositoryState, configPath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			writeRepositoryAPIError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
			return
		}
		repositories.mu.Lock()
		defer repositories.mu.Unlock()
		if repositories.repository == nil {
			writeRepositoryError(w, http.StatusBadRequest, "NOT_CONNECTED", "not connected")
			return
		}
		if err := repositories.repository.Close(r.Context()); err != nil {
			writeRepositoryError(w, http.StatusInternalServerError, "INTERNAL", fmt.Sprintf("internal server error: %v", err))
			return
		}
		repositories.repository = nil
		if err := repo.Disconnect(r.Context(), configPath); err != nil {
			writeRepositoryError(w, http.StatusInternalServerError, "INTERNAL", fmt.Sprintf("internal server error: %v", err))
			return
		}
		writeRepositoryJSON(w, http.StatusOK, struct{}{})
	}
}

func connectRepository(ctx context.Context, configPath string, connection blob.ConnectionInfo, password string, clientOptions repo.ClientOptions) (repo.Repository, error) {
	storage, err := blob.NewStorage(ctx, connection, false)
	if err != nil {
		return nil, fmt.Errorf("can't open storage: %w", err)
	}
	defer storage.Close(ctx) //nolint:errcheck

	return connectRepositoryWithStorage(ctx, configPath, storage, password, clientOptions)
}

func connectRepositoryWithStorage(ctx context.Context, configPath string, storage blob.Storage, password string, clientOptions repo.ClientOptions) (repo.Repository, error) {
	if err := repo.Connect(ctx, configPath, storage, password, &repo.ConnectOptions{ClientOptions: clientOptions}); err != nil {
		return nil, fmt.Errorf("error connecting: %w", err)
	}
	connected, err := repo.Open(ctx, configPath, password, nil)
	if err != nil {
		return nil, fmt.Errorf("error opening repository: %w", err)
	}
	return connected, nil
}

func initializeNewRepository(ctx context.Context, repository repo.Repository) error {
	return repo.WriteSession(ctx, repository, repo.WriteSessionOptions{Purpose: "handleRepoCreate"}, func(ctx context.Context, writer repo.RepositoryWriter) error {
		if err := snapshot.SetPolicy(ctx, writer, snapshot.SourceInfo{}, defaultRepositoryPolicy()); err != nil {
			return fmt.Errorf("set global policy: %w", err)
		}
		params := maintenance.DefaultParams()
		params.Owner = writer.ClientOptions().UsernameAtHost()
		if err := maintenance.SetParams(ctx, writer, &params); err != nil {
			return fmt.Errorf("unable to set maintenance params: %w", err)
		}
		return nil
	})
}

func defaultRepositoryPolicy() snapshot.SnapshotPolicy {
	retention := snapshot.DefaultRetentionPolicy()
	logDirectory := snapshot.LogDetailNormal
	logEntryNormal := snapshot.LogDetailNormal
	logEntryNone := snapshot.LogDetailNone
	osSnapshotNever := snapshot.OSSnapshotNever
	ignoreFileErrors := false
	ignoreDirectoryErrors := false
	ignoreUnknownTypes := true
	runMissed := true
	maxParallelSnapshots := 1
	parallelUploadAboveSize := int64(2 << 30)

	return snapshot.SnapshotPolicy{
		Retention: snapshot.RetentionPolicyOverrides{
			KeepLatest:  &retention.KeepLatest,
			KeepHourly:  &retention.KeepHourly,
			KeepDaily:   &retention.KeepDaily,
			KeepWeekly:  &retention.KeepWeekly,
			KeepMonthly: &retention.KeepMonthly,
			KeepAnnual:  &retention.KeepAnnual,
		},
		Files: snapshot.FilesPolicy{
			DotIgnoreFiles: []string{".kopiaignore"},
		},
		ErrorHandling: snapshot.ErrorHandlingPolicy{
			IgnoreFileErrors:      &ignoreFileErrors,
			IgnoreDirectoryErrors: &ignoreDirectoryErrors,
			IgnoreUnknownTypes:    &ignoreUnknownTypes,
		},
		Scheduling: snapshot.SchedulingPolicy{
			RunMissed: &runMissed,
		},
		Compression: snapshot.CompressionPolicy{
			CompressorName: "none",
		},
		MetadataCompression: snapshot.MetadataCompressionPolicy{
			CompressorName: "zstd-fastest",
		},
		OSSnapshot: snapshot.OSSnapshotPolicy{
			VolumeShadowCopy: snapshot.VolumeShadowCopyPolicy{Enable: &osSnapshotNever},
		},
		Logging: snapshot.LoggingPolicy{
			Directories: snapshot.DirLoggingPolicy{
				Snapshotted: &logDirectory,
				Ignored:     &logDirectory,
			},
			Entries: snapshot.EntryLoggingPolicy{
				Snapshotted: &logEntryNone,
				Ignored:     &logEntryNormal,
				CacheHit:    &logEntryNone,
				CacheMiss:   &logEntryNone,
			},
		},
		Upload: snapshot.UploadPolicy{
			MaxParallelSnapshots:    &maxParallelSnapshots,
			ParallelUploadAboveSize: &parallelUploadAboveSize,
		},
	}
}

func decodeRepositoryToken(connection *blob.ConnectionInfo, password *string, token string, w http.ResponseWriter) bool {
	if token == "" {
		return true
	}
	decodedConnection, decodedPassword, err := repo.DecodeToken(token)
	if err != nil {
		writeRepositoryError(w, http.StatusBadRequest, "INVALID_TOKEN", "invalid token: "+err.Error())
		return false
	}
	*connection = decodedConnection
	if decodedPassword != "" {
		*password = decodedPassword
	}
	return true
}

func writeRepositoryConnectError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, repo.ErrRepositoryNotInitialized):
		writeRepositoryError(w, http.StatusBadRequest, "NOT_INITIALIZED", "repository not initialized")
	case errors.Is(err, repo.ErrInvalidPassword):
		writeRepositoryError(w, http.StatusBadRequest, "INVALID_PASSWORD", "invalid password")
	case errors.Is(err, repo.ErrAlreadyInitialized):
		writeRepositoryError(w, http.StatusBadRequest, "ALREADY_INITIALIZED", "repository already initialized")
	default:
		writeRepositoryError(w, http.StatusInternalServerError, "INTERNAL", fmt.Sprintf("internal server error: connect error: %v", err))
	}
}
