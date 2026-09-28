package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sort"

	"github.com/sergklm98/kopia-lib/lib/repo"
	"github.com/sergklm98/kopia-lib/lib/repo/manifest"
	"github.com/sergklm98/kopia-lib/lib/repo/snapshot"
)

func registerPolicyRoutes(mux *http.ServeMux, repositories *repositoryState) {
	mux.Handle("/api/v1/policy", repositories.readHandler(policyHandler(repositories)))
	mux.Handle("/api/v1/policy/resolve", repositories.readHandler(http.HandlerFunc(policyResolveHandler)))
	mux.Handle("/api/v1/policies", repositories.readHandler(policyListHandler(repositories)))
}

type policyRetentionJSON struct {
	KeepLatest               *int  `json:"keepLatest,omitempty"`
	KeepHourly               *int  `json:"keepHourly,omitempty"`
	KeepDaily                *int  `json:"keepDaily,omitempty"`
	KeepWeekly               *int  `json:"keepWeekly,omitempty"`
	KeepMonthly              *int  `json:"keepMonthly,omitempty"`
	KeepAnnual               *int  `json:"keepAnnual,omitempty"`
	IgnoreIdenticalSnapshots *bool `json:"ignoreIdenticalSnapshots,omitempty"`
}

type policyJSON struct {
	Retention           policyRetentionJSON                `json:"retention,omitempty"`
	Files               snapshot.FilesPolicy               `json:"files,omitempty"`
	ErrorHandling       snapshot.ErrorHandlingPolicy       `json:"errorHandling,omitempty"`
	Scheduling          snapshot.SchedulingPolicy          `json:"scheduling,omitempty"`
	Compression         snapshot.CompressionPolicy         `json:"compression,omitempty"`
	MetadataCompression snapshot.MetadataCompressionPolicy `json:"metadataCompression,omitempty"`
	Splitter            snapshot.SplitterPolicy            `json:"splitter,omitempty"`
	Actions             snapshot.ActionsPolicy             `json:"actions,omitempty"`
	OSSnapshot          snapshot.OSSnapshotPolicy          `json:"osSnapshots,omitempty"`
	Logging             snapshot.LoggingPolicy             `json:"logging,omitempty"`
	Upload              snapshot.UploadPolicy              `json:"upload,omitempty"`
	NoParent            bool                               `json:"noParent,omitempty"`
}

type policyListEntry struct {
	ID     string              `json:"id"`
	Target snapshot.SourceInfo `json:"target"`
	Policy policyJSON          `json:"policy"`
}

type policyListResponse struct {
	Policies []policyListEntry `json:"policies"`
}

func policyHandler(repositories *repositoryState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		repository := repositories.repository
		if repository == nil {
			writeRepositoryError(w, http.StatusBadRequest, "NOT_CONNECTED", "not connected")
			return
		}
		source := policySourceFromURL(r.URL)
		switch r.Method {
		case http.MethodGet:
			entries, err := findPolicyManifests(r.Context(), repository, source)
			if err != nil {
				writeRepositoryError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
				return
			}
			if len(entries) == 0 {
				writeRepositoryError(w, http.StatusNotFound, "NOT_FOUND", "policy not found")
				return
			}
			var policy policyJSON
			if _, err := repository.GetManifest(r.Context(), manifest.PickLatestID(entries), &policy); err != nil {
				writeRepositoryError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
				return
			}
			writeRepositoryJSON(w, http.StatusOK, policy)
		case http.MethodPut:
			var request policyJSON
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				writeRepositoryError(w, http.StatusBadRequest, "MALFORMED_REQUEST", "malformed request body")
				return
			}
			if _, ok := repository.(repo.RepositoryWriter); !ok {
				writeRepositoryError(w, http.StatusInternalServerError, "INTERNAL", "repository is not writable")
				return
			}
			if err := savePolicy(r.Context(), repository, source, request); err != nil {
				writeRepositoryError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
				return
			}
			writeRepositoryJSON(w, http.StatusOK, struct{}{})
		case http.MethodDelete:
			if _, ok := repository.(repo.RepositoryWriter); !ok {
				writeRepositoryError(w, http.StatusInternalServerError, "INTERNAL", "repository is not writable")
				return
			}
			if err := deletePolicy(r.Context(), repository, source); err != nil {
				writeRepositoryError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
				return
			}
			writeRepositoryJSON(w, http.StatusOK, struct{}{})
		default:
			w.Header().Set("Allow", "GET, PUT, DELETE")
			writeRepositoryAPIError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
		}
	}
}

func policyListHandler(repositories *repositoryState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		repository := repositories.repository
		if repository == nil {
			writeRepositoryError(w, http.StatusBadRequest, "NOT_CONNECTED", "not connected")
			return
		}
		entries, err := repository.FindManifests(r.Context(), map[string]string{manifest.TypeLabelKey: snapshot.PolicyManifestType})
		if err != nil {
			writeRepositoryError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
			return
		}

		latestBySource := map[snapshot.SourceInfo]*manifest.EntryMetadata{}
		for _, entry := range entries {
			target := policySourceFromLabels(entry.Labels)
			if !policySourceMatchesFilter(target, r.URL.Query()) {
				continue
			}
			current := latestBySource[target]
			if current == nil || entry.ModTime.After(current.ModTime) ||
				entry.ModTime.Equal(current.ModTime) && entry.ID > current.ID {
				latestBySource[target] = entry
			}
		}

		targets := make([]snapshot.SourceInfo, 0, len(latestBySource))
		for target := range latestBySource {
			targets = append(targets, target)
		}
		sort.Slice(targets, func(i, j int) bool { return targets[i].String() < targets[j].String() })

		response := policyListResponse{Policies: make([]policyListEntry, 0, len(targets))}
		for _, target := range targets {
			entry := latestBySource[target]
			var policy policyJSON
			if _, err := repository.GetManifest(r.Context(), entry.ID, &policy); err != nil {
				writeRepositoryError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
				return
			}
			response.Policies = append(response.Policies, policyListEntry{
				ID:     string(entry.ID),
				Target: target,
				Policy: policy,
			})
		}
		writeRepositoryJSON(w, http.StatusOK, response)
	}
}

func policyResolveHandler(w http.ResponseWriter, _ *http.Request) {
	writeRepositoryError(w, http.StatusNotImplemented, "INTERNAL", "policy provenance and upcoming schedule calculation are not available in kopia-lib")
}

func policySourceFromURL(u *url.URL) snapshot.SourceInfo {
	return snapshot.SourceInfo{
		Host:     u.Query().Get("host"),
		UserName: u.Query().Get("userName"),
		Path:     u.Query().Get("path"),
	}
}

func policySourceFromLabels(labels map[string]string) snapshot.SourceInfo {
	return snapshot.SourceInfo{
		Host:     labels[snapshot.HostnameLabel],
		UserName: labels[snapshot.UsernameLabel],
		Path:     labels[snapshot.PathLabel],
	}
}

func policySourceMatchesFilter(source snapshot.SourceInfo, query url.Values) bool {
	return (query.Get("host") == "" || query.Get("host") == source.Host) &&
		(query.Get("userName") == "" || query.Get("userName") == source.UserName) &&
		(query.Get("path") == "" || query.Get("path") == source.Path)
}

func snapshotPolicyFromJSON(policy policyJSON, source snapshot.SourceInfo) snapshot.SnapshotPolicy {
	return snapshot.SnapshotPolicy{
		Source: source,
		Retention: snapshot.RetentionPolicyOverrides{
			KeepLatest:  policy.Retention.KeepLatest,
			KeepHourly:  policy.Retention.KeepHourly,
			KeepDaily:   policy.Retention.KeepDaily,
			KeepWeekly:  policy.Retention.KeepWeekly,
			KeepMonthly: policy.Retention.KeepMonthly,
			KeepAnnual:  policy.Retention.KeepAnnual,
		},
		Files:               policy.Files,
		ErrorHandling:       policy.ErrorHandling,
		Scheduling:          policy.Scheduling,
		Compression:         policy.Compression,
		MetadataCompression: policy.MetadataCompression,
		Splitter:            policy.Splitter,
		Actions:             policy.Actions,
		OSSnapshot:          policy.OSSnapshot,
		Logging:             policy.Logging,
		Upload:              policy.Upload,
		NoParent:            policy.NoParent,
	}
}

func findPolicyManifests(ctx context.Context, repository repo.Repository, source snapshot.SourceInfo) ([]*manifest.EntryMetadata, error) {
	entries, err := repository.FindManifests(ctx, map[string]string{manifest.TypeLabelKey: snapshot.PolicyManifestType})
	if err != nil {
		return nil, err
	}
	var matching []*manifest.EntryMetadata
	for _, entry := range entries {
		if policySourceFromLabels(entry.Labels) == source {
			matching = append(matching, entry)
		}
	}
	return matching, nil
}

func savePolicy(ctx context.Context, repository repo.Repository, source snapshot.SourceInfo, value policyJSON) error {
	policy := snapshotPolicyFromJSON(value, source)
	if _, err := snapshot.ResolvePolicy(source, []snapshot.SnapshotPolicy{policy}); err != nil {
		return err
	}

	return repo.WriteSession(ctx, repository, repo.WriteSessionOptions{Purpose: "PolicyPut"}, func(ctx context.Context, writer repo.RepositoryWriter) error {
		entries, err := writer.FindManifests(ctx, map[string]string{manifest.TypeLabelKey: snapshot.PolicyManifestType})
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if policySourceFromLabels(entry.Labels) == source {
				if err := writer.DeleteManifest(ctx, entry.ID); err != nil {
					return err
				}
			}
		}
		_, err = writer.PutManifest(ctx, policyLabelsForSource(source), value)
		return err
	})
}

func deletePolicy(ctx context.Context, repository repo.Repository, source snapshot.SourceInfo) error {
	return repo.WriteSession(ctx, repository, repo.WriteSessionOptions{Purpose: "PolicyDelete"}, func(ctx context.Context, writer repo.RepositoryWriter) error {
		entries, err := writer.FindManifests(ctx, map[string]string{manifest.TypeLabelKey: snapshot.PolicyManifestType})
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if policySourceFromLabels(entry.Labels) == source {
				if err := writer.DeleteManifest(ctx, entry.ID); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func policyLabelsForSource(source snapshot.SourceInfo) map[string]string {
	policyType := "global"
	switch {
	case source.Path != "":
		policyType = "path"
	case source.UserName != "":
		policyType = "user"
	case source.Host != "":
		policyType = "host"
	}

	labels := map[string]string{
		manifest.TypeLabelKey: "policy",
		"policyType":          policyType,
	}
	if source.Path != "" {
		labels[snapshot.UsernameLabel] = source.UserName
		labels[snapshot.HostnameLabel] = source.Host
		labels[snapshot.PathLabel] = source.Path
	} else if source.UserName != "" {
		labels[snapshot.UsernameLabel] = source.UserName
		labels[snapshot.HostnameLabel] = source.Host
	} else if source.Host != "" {
		labels[snapshot.HostnameLabel] = source.Host
	}
	return labels
}
