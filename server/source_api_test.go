package server

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/sergklm98/kopia-lib/lib/repo"
	"github.com/sergklm98/kopia-lib/lib/repo/snapshot"
)

func TestSourceCreateAndListAPI(t *testing.T) {
	repository := openNotificationAPITestRepository(t)
	direct := repository.(repo.DirectRepository)
	handler := newHandler("", repository, direct.ConfigFilename(), "", "")
	sourcePath := t.TempDir()
	response := performJSONRequest(t, handler, http.MethodPost, "/api/v1/sources", createSourceRequest{
		Path:   sourcePath,
		Policy: &policyJSON{Retention: policyRetentionJSON{KeepDaily: intPointer(9)}},
	})
	if response.Code != http.StatusOK {
		t.Fatalf("unexpected source create status: %s: %s", response.Result().Status, response.Body.String())
	}
	var created createSourceResponse
	if err := json.NewDecoder(response.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.SnapshotStarted {
		t.Fatal("source creation without a snapshot unexpectedly started one")
	}

	response = performJSONRequest(t, handler, http.MethodGet, "/api/v1/sources", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("unexpected source list status: %s", response.Result().Status)
	}
	var listed sourcesResponse
	if err := json.NewDecoder(response.Body).Decode(&listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Sources) != 1 || listed.Sources[0].Source.Path != sourcePath ||
		listed.Sources[0].Source.Host != repository.ClientOptions().Hostname ||
		listed.Sources[0].Source.UserName != repository.ClientOptions().Username ||
		listed.Sources[0].Status != "IDLE" || listed.Sources[0].LastSnapshot != nil {
		t.Fatalf("unexpected source list response: %#v", listed)
	}
	if !listed.MultiUser || listed.LocalHost != repository.ClientOptions().Hostname ||
		listed.LocalUsername != repository.ClientOptions().Username {
		t.Fatalf("unexpected source-list repository metadata: %#v", listed)
	}

	response = performJSONRequest(t, handler, http.MethodPost, "/api/v1/sources", createSourceRequest{
		Path:           sourcePath,
		CreateSnapshot: true,
		Policy:         &policyJSON{},
	})
	if response.Code != http.StatusNotImplemented {
		t.Fatalf("unexpected scheduled-create status: %s", response.Result().Status)
	}
	for _, actionPath := range []string{"/api/v1/sources/upload", "/api/v1/sources/cancel"} {
		response = performJSONRequest(t, handler, http.MethodPost, actionPath, nil)
		if response.Code != http.StatusNotImplemented {
			t.Fatalf("unexpected %s status: %s", actionPath, response.Result().Status)
		}
	}

	response = performJSONRequest(t, handler, http.MethodPost, "/api/v1/sources", createSourceRequest{
		Path:   filepath.Join(t.TempDir(), "missing"),
		Policy: &policyJSON{},
	})
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unexpected missing-path status: %s", response.Result().Status)
	}

	storedPolicy, err := snapshot.GetPolicy(t.Context(), repository, snapshot.SourceInfo{
		Host:     repository.ClientOptions().Hostname,
		UserName: repository.ClientOptions().Username,
		Path:     sourcePath,
	})
	if err != nil || storedPolicy.Retention.KeepDaily == nil || *storedPolicy.Retention.KeepDaily != 9 {
		t.Fatalf("source policy was not stored: policy=%#v, err=%v", storedPolicy, err)
	}
}

func intPointer(value int) *int {
	return &value
}
