package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"sort"
	"time"

	"github.com/sergklm98/kopia-lib/repo"
	"github.com/sergklm98/kopia-lib/repo/manifest"
	"github.com/sergklm98/kopia-lib/repo/snapshot"
)

func registerSourceRoutes(mux *http.ServeMux, repositories *repositoryState) {
	mux.Handle("/api/v1/sources", repositories.readHandler(sourceHandler(repositories)))
	mux.Handle("/api/v1/sources/upload", repositories.readHandler(sourceActionNotImplementedHandler(repositories, "source upload requires policy-aware source manager execution")))
	mux.Handle("/api/v1/sources/cancel", repositories.readHandler(sourceActionNotImplementedHandler(repositories, "source cancellation requires an active upload task manager")))
}

func sourceActionNotImplementedHandler(repositories *repositoryState, message string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			writeRepositoryAPIError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
			return
		}
		if repositories.repository == nil {
			writeRepositoryError(w, http.StatusBadRequest, "NOT_CONNECTED", "not connected")
			return
		}
		writeRepositoryError(w, http.StatusNotImplemented, "INTERNAL", message)
	})
}

type sourceStatusResponse struct {
	Source           snapshot.SourceInfo       `json:"source"`
	Status           string                    `json:"status"`
	SchedulingPolicy snapshot.SchedulingPolicy `json:"schedule"`
	LastSnapshot     *snapshot.Manifest        `json:"lastSnapshot,omitempty"`
	NextSnapshotTime *time.Time                `json:"nextSnapshotTime,omitempty"`
	CurrentTask      string                    `json:"currentTask,omitempty"`
}

type sourcesResponse struct {
	LocalUsername string                 `json:"localUsername"`
	LocalHost     string                 `json:"localHost"`
	MultiUser     bool                   `json:"multiUser"`
	Sources       []sourceStatusResponse `json:"sources"`
}

type createSourceRequest struct {
	Path           string      `json:"path"`
	CreateSnapshot bool        `json:"createSnapshot"`
	Policy         *policyJSON `json:"policy"`
}

type createSourceResponse struct {
	SnapshotStarted bool `json:"snapshotted"`
}

func sourceHandler(repositories *repositoryState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		repository := repositories.repository
		if repository == nil {
			writeRepositoryError(w, http.StatusBadRequest, "NOT_CONNECTED", "not connected")
			return
		}
		switch r.Method {
		case http.MethodGet:
			listSources(w, r, repository)
		case http.MethodPost:
			createSource(w, r, repository)
		default:
			w.Header().Set("Allow", "GET, POST")
			writeRepositoryAPIError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
		}
	}
}

func listSources(w http.ResponseWriter, r *http.Request, repository repo.Repository) {
	sourceSet := map[snapshot.SourceInfo]struct{}{}
	sources, err := snapshot.ListSources(r.Context(), repository)
	if err != nil {
		writeRepositoryError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	for _, source := range sources {
		if source.Path != "" && policySourceMatchesFilter(source, r.URL.Query()) {
			sourceSet[source] = struct{}{}
		}
	}
	policyEntries, err := repository.FindManifests(r.Context(), map[string]string{manifest.TypeLabelKey: snapshot.PolicyManifestType})
	if err != nil {
		writeRepositoryError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	for _, entry := range policyEntries {
		source := policySourceFromLabels(entry.Labels)
		if source.Path != "" && policySourceMatchesFilter(source, r.URL.Query()) {
			sourceSet[source] = struct{}{}
		}
	}

	orderedSources := make([]snapshot.SourceInfo, 0, len(sourceSet))
	for source := range sourceSet {
		orderedSources = append(orderedSources, source)
	}
	sort.Slice(orderedSources, func(i, j int) bool { return orderedSources[i].String() < orderedSources[j].String() })

	clientOptions := repository.ClientOptions()
	_, multiUser := repository.(repo.DirectRepository)
	response := sourcesResponse{
		LocalUsername: clientOptions.Username,
		LocalHost:     clientOptions.Hostname,
		MultiUser:     multiUser,
		Sources:       make([]sourceStatusResponse, 0, len(orderedSources)),
	}
	for _, source := range orderedSources {
		status := sourceStatusResponse{Source: source, Status: "IDLE"}
		effective, err := snapshot.ResolvePersistedPolicy(r.Context(), repository, source)
		if err != nil {
			writeRepositoryError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
			return
		}
		status.SchedulingPolicy = effective.Scheduling
		manifests, err := snapshot.ListSnapshots(r.Context(), repository, source)
		if err != nil {
			writeRepositoryError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
			return
		}
		manifests = snapshot.SortByTime(manifests, true)
		if len(manifests) > 0 {
			status.LastSnapshot = manifests[0]
		}
		response.Sources = append(response.Sources, status)
	}
	writeRepositoryJSON(w, http.StatusOK, response)
}

func createSource(w http.ResponseWriter, r *http.Request, repository repo.Repository) {
	var request createSourceRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeRepositoryError(w, http.StatusBadRequest, "MALFORMED_REQUEST", "malformed request body")
		return
	}
	if request.Path == "" {
		writeRepositoryError(w, http.StatusBadRequest, "MALFORMED_REQUEST", "missing path")
		return
	}
	if request.Policy == nil {
		writeRepositoryError(w, http.StatusBadRequest, "MALFORMED_REQUEST", "missing policy")
		return
	}
	if request.CreateSnapshot {
		writeRepositoryError(w, http.StatusNotImplemented, "INTERNAL", "source snapshot scheduling is not available")
		return
	}

	path, err := os.UserHomeDir()
	if err != nil {
		path = ""
	}
	resolvedPath := resolveUserFriendlyPath(request.Path, path)
	if _, err := os.Stat(resolvedPath); os.IsNotExist(err) {
		writeRepositoryError(w, http.StatusBadRequest, "PATH_NOT_FOUND", "path does not exist")
		return
	} else if err != nil {
		writeRepositoryError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}

	options := repository.ClientOptions()
	source := snapshot.SourceInfo{Host: options.Hostname, UserName: options.Username, Path: resolvedPath}
	if err := savePolicy(r.Context(), repository, source, *request.Policy); err != nil {
		writeRepositoryError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	writeRepositoryJSON(w, http.StatusOK, createSourceResponse{})
}
