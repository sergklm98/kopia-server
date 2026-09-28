package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"

	"github.com/sergklm98/kopia-lib/lib/repo"
	"github.com/sergklm98/kopia-lib/lib/repo/manifest"
	"github.com/sergklm98/kopia-lib/lib/repo/snapshot"
)

func registerSnapshotRoutes(mux *http.ServeMux, repositories *repositoryState) {
	mux.Handle("/api/v1/snapshots", repositories.readHandler(snapshotListHandler(repositories)))
	mux.Handle("/api/v1/snapshots/edit", repositories.readHandler(snapshotEditHandler(repositories)))
	mux.Handle("/api/v1/snapshots/delete", repositories.readHandler(snapshotDeleteHandler(repositories)))
}

type snapshotResponse struct {
	ID               manifest.ID                `json:"id"`
	Description      string                     `json:"description"`
	StartTime        snapshot.UTCTimestamp      `json:"startTime"`
	EndTime          snapshot.UTCTimestamp      `json:"endTime"`
	IncompleteReason string                     `json:"incomplete,omitempty"`
	Summary          *snapshot.DirectorySummary `json:"summary"`
	RootID           string                     `json:"rootID"`
	Retention        []string                   `json:"retention"`
	Pins             []string                   `json:"pins"`
}

type snapshotsResponse struct {
	Snapshots       []*snapshotResponse `json:"snapshots"`
	UnfilteredCount int                 `json:"unfilteredCount"`
	UniqueCount     int                 `json:"uniqueCount"`
}

type snapshotEditRequest struct {
	Snapshots   []manifest.ID `json:"snapshots"`
	Description *string       `json:"description"`
	AddPins     []string      `json:"addPins"`
	RemovePins  []string      `json:"removePins"`
}

type snapshotDeleteRequest struct {
	SourceInfo            snapshot.SourceInfo `json:"source"`
	SnapshotManifestIDs   []manifest.ID       `json:"snapshotManifestIds"`
	DeleteSourceAndPolicy bool                `json:"deleteSourceAndPolicy"`
}

func snapshotListHandler(repositories *repositoryState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		repository := repositories.repository
		if repository == nil {
			writeRepositoryError(w, http.StatusBadRequest, "NOT_CONNECTED", "not connected")
			return
		}

		source := policySourceFromURL(r.URL)
		manifestIDs, err := snapshot.ListSnapshotManifests(r.Context(), repository, &source, nil)
		if err != nil {
			writeRepositoryError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
			return
		}
		manifests, err := snapshot.LoadSnapshots(r.Context(), repository, manifestIDs)
		if err != nil {
			writeRepositoryError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
			return
		}
		manifests = snapshot.SortByTime(manifests, false)

		retentionByID := map[manifest.ID][]string{}
		if effective, err := snapshot.ResolvePersistedPolicy(r.Context(), repository, source); err == nil {
			for _, decision := range snapshot.PlanRetention(effective.Retention, manifests) {
				retentionByID[decision.ManifestID] = decision.RetentionReasons
			}
		}

		all := make([]*snapshotResponse, 0, len(manifests))
		for _, item := range manifests {
			all = append(all, convertSnapshotResponse(item, retentionByID[item.ID]))
		}
		response := snapshotsResponse{
			Snapshots:       all,
			UnfilteredCount: len(all),
			UniqueCount:     len(uniqueSnapshotResponses(all)),
		}
		if r.URL.Query().Get("all") == "" {
			response.Snapshots = uniqueSnapshotResponses(all)
		}
		writeRepositoryJSON(w, http.StatusOK, response)
	}
}

func snapshotEditHandler(repositories *repositoryState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			writeRepositoryAPIError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
			return
		}
		repository := repositories.repository
		if repository == nil {
			writeRepositoryError(w, http.StatusBadRequest, "NOT_CONNECTED", "not connected")
			return
		}
		var request snapshotEditRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			writeRepositoryError(w, http.StatusBadRequest, "MALFORMED_REQUEST", "malformed request")
			return
		}

		var updated []*snapshotResponse
		err := repo.WriteSession(r.Context(), repository, repo.WriteSessionOptions{Purpose: "EditSnapshots"}, func(ctx context.Context, writer repo.RepositoryWriter) error {
			for _, id := range request.Snapshots {
				item, err := snapshot.LoadSnapshot(ctx, writer, id)
				if err != nil {
					return err
				}
				changed := item.UpdatePins(request.AddPins, request.RemovePins)
				if request.Description != nil {
					item.Description = *request.Description
					changed = true
				}
				if changed {
					if err := snapshot.UpdateSnapshot(ctx, writer, item); err != nil {
						return err
					}
				}
				updated = append(updated, convertSnapshotResponse(item, nil))
			}
			return nil
		})
		if err != nil {
			writeRepositoryError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
			return
		}
		writeRepositoryJSON(w, http.StatusOK, updated)
	}
}

func snapshotDeleteHandler(repositories *repositoryState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			writeRepositoryAPIError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
			return
		}
		repository := repositories.repository
		if repository == nil {
			writeRepositoryError(w, http.StatusBadRequest, "NOT_CONNECTED", "not connected")
			return
		}
		var request snapshotDeleteRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			writeRepositoryError(w, http.StatusBadRequest, "MALFORMED_REQUEST", "malformed request")
			return
		}

		err := repo.WriteSession(r.Context(), repository, repo.WriteSessionOptions{Purpose: "DeleteSnapshots"}, func(ctx context.Context, writer repo.RepositoryWriter) error {
			ids := request.SnapshotManifestIDs
			if request.DeleteSourceAndPolicy {
				var err error
				ids, err = snapshot.ListSnapshotManifests(ctx, writer, &request.SourceInfo, nil)
				if err != nil {
					return err
				}
				if len(ids) == 0 {
					return errUnknownSnapshotSource
				}
			} else {
				items, err := snapshot.LoadSnapshots(ctx, writer, ids)
				if err != nil {
					return err
				}
				for _, item := range items {
					if item.Source != request.SourceInfo {
						return errors.New("source info does not match snapshot source")
					}
				}
			}
			for _, id := range ids {
				if err := writer.DeleteManifest(ctx, id); err != nil {
					return err
				}
			}
			if request.DeleteSourceAndPolicy {
				if err := snapshot.RemovePolicy(ctx, writer, request.SourceInfo); err != nil {
					return err
				}
			}
			return nil
		})
		if errors.Is(err, errUnknownSnapshotSource) {
			writeRepositoryError(w, http.StatusNotFound, "NOT_FOUND", "unknown source")
			return
		}
		if err != nil {
			writeRepositoryError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
			return
		}
		writeRepositoryJSON(w, http.StatusOK, struct{}{})
	}
}

var errUnknownSnapshotSource = errors.New("unknown source")

func convertSnapshotResponse(item *snapshot.Manifest, retentionReasons []string) *snapshotResponse {
	var summary *snapshot.DirectorySummary
	if item.RootEntry != nil {
		summary = item.RootEntry.DirSummary
	}
	return &snapshotResponse{
		ID:               item.ID,
		Description:      item.Description,
		StartTime:        item.StartTime,
		EndTime:          item.EndTime,
		IncompleteReason: item.IncompleteReason,
		Summary:          summary,
		RootID:           item.RootObjectID().String(),
		Retention:        append([]string{}, retentionReasons...),
		Pins:             append([]string{}, item.Pins...),
	}
}

func uniqueSnapshotResponses(snapshots []*snapshotResponse) []*snapshotResponse {
	result := make([]*snapshotResponse, 0, len(snapshots))
	byRoot := map[string]*snapshotResponse{}
	for _, item := range snapshots {
		previous := byRoot[item.RootID]
		if previous == nil {
			byRoot[item.RootID] = item
			result = append(result, item)
			continue
		}
		previous.Retention = sortedUnion(previous.Retention, item.Retention)
		previous.Pins = sortedUnion(previous.Pins, item.Pins)
	}
	return result
}

func sortedUnion(first, second []string) []string {
	values := map[string]struct{}{}
	for _, value := range first {
		values[value] = struct{}{}
	}
	for _, value := range second {
		values[value] = struct{}{}
	}
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
