package server

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/sergklm98/kopia-lib/repo"
	"github.com/sergklm98/kopia-lib/repo/manifest"
	"github.com/sergklm98/kopia-lib/repo/object"
	"github.com/sergklm98/kopia-lib/repo/snapshot"
)

func TestSnapshotListEditDeleteAPI(t *testing.T) {
	ctx := context.Background()
	repository := openNotificationAPITestRepository(t)
	direct := repository.(repo.DirectRepository)
	handler := newHandler("", repository, direct.ConfigFilename(), "", "")
	source := snapshot.SourceInfo{Host: "snapshot-host", UserName: "snapshot-user", Path: "/data/source"}
	rootID, err := object.ParseID("aa")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)
	var snapshotIDs []manifest.ID
	if err := repo.WriteSession(ctx, repository, repo.WriteSessionOptions{Purpose: "snapshot-api-test"}, func(ctx context.Context, writer repo.RepositoryWriter) error {
		for index, description := range []string{"older", "newer"} {
			item := &snapshot.Manifest{
				Source:      source,
				Description: description,
				StartTime:   snapshot.UTCTimestamp(start.Add(time.Duration(index) * time.Hour).UnixNano()),
				EndTime:     snapshot.UTCTimestamp(start.Add(time.Duration(index)*time.Hour + time.Minute).UnixNano()),
				RootEntry:   &snapshot.DirEntry{Type: snapshot.EntryTypeDirectory, ObjectID: rootID},
			}
			id, err := snapshot.SaveSnapshot(ctx, writer, item)
			if err != nil {
				return err
			}
			snapshotIDs = append(snapshotIDs, id)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	baseURL := "/api/v1/snapshots?host=snapshot-host&userName=snapshot-user&path=%2Fdata%2Fsource"
	response := performJSONRequest(t, handler, http.MethodGet, baseURL, nil)
	var listed snapshotsResponse
	if err := json.NewDecoder(response.Body).Decode(&listed); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || len(listed.Snapshots) != 1 || listed.UnfilteredCount != 2 || listed.UniqueCount != 1 {
		t.Fatalf("unexpected unique snapshot response (%s): %#v", response.Result().Status, listed)
	}
	if listed.Snapshots[0].RootID != rootID.String() {
		t.Fatalf("unexpected root ID: %#v", listed.Snapshots[0])
	}

	response = performJSONRequest(t, handler, http.MethodGet, baseURL+"&all=true", nil)
	listed = snapshotsResponse{}
	if err := json.NewDecoder(response.Body).Decode(&listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Snapshots) != 2 || listed.UnfilteredCount != 2 || listed.UniqueCount != 1 {
		t.Fatalf("unexpected all-snapshots response: %#v", listed)
	}

	description := "edited description"
	response = performJSONRequest(t, handler, http.MethodPost, "/api/v1/snapshots/edit", snapshotEditRequest{
		Snapshots:   []manifest.ID{snapshotIDs[0]},
		Description: &description,
		AddPins:     []string{"keep"},
	})
	if response.Code != http.StatusOK {
		t.Fatalf("unexpected snapshot edit status: %s: %s", response.Result().Status, response.Body.String())
	}
	var edited []*snapshotResponse
	if err := json.NewDecoder(response.Body).Decode(&edited); err != nil {
		t.Fatal(err)
	}
	if len(edited) != 1 || edited[0].Description != description || len(edited[0].Pins) != 1 || edited[0].Pins[0] != "keep" {
		t.Fatalf("unexpected edited snapshot: %#v", edited)
	}

	response = performJSONRequest(t, handler, http.MethodPost, "/api/v1/snapshots/delete", snapshotDeleteRequest{
		SourceInfo:          source,
		SnapshotManifestIDs: []manifest.ID{snapshotIDs[0]},
	})
	if response.Code != http.StatusOK {
		t.Fatalf("unexpected snapshot delete status: %s: %s", response.Result().Status, response.Body.String())
	}

	response = performJSONRequest(t, handler, http.MethodPost, "/api/v1/snapshots/delete", snapshotDeleteRequest{
		SourceInfo:            source,
		DeleteSourceAndPolicy: true,
	})
	if response.Code != http.StatusOK {
		t.Fatalf("unexpected source delete status: %s: %s", response.Result().Status, response.Body.String())
	}

	response = performJSONRequest(t, handler, http.MethodGet, baseURL+"&all=true", nil)
	listed = snapshotsResponse{}
	if err := json.NewDecoder(response.Body).Decode(&listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Snapshots) != 0 || listed.UnfilteredCount != 0 {
		t.Fatalf("snapshots remain after deleting source: %#v", listed)
	}
}
