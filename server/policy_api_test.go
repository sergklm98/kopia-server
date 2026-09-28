package server

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/sergklm98/kopia-lib/lib/repo"
	"github.com/sergklm98/kopia-lib/lib/repo/manifest"
	"github.com/sergklm98/kopia-lib/lib/repo/snapshot"
)

func TestPolicyStoredAPIs(t *testing.T) {
	repository := openNotificationAPITestRepository(t)
	direct := repository.(repo.DirectRepository)
	handler := newHandler("", repository, direct.ConfigFilename(), "", "")
	targetQuery := "?host=policy-host&userName=policy-user&path=%2Fdata%2Fpolicy"
	requestPolicy := map[string]any{
		"retention":   map[string]any{"keepDaily": 12, "keepLatest": 4, "ignoreIdenticalSnapshots": true},
		"compression": map[string]any{"compressorName": "zstd-fastest"},
	}

	response := performJSONRequest(t, handler, http.MethodPut, "/api/v1/policy"+targetQuery, requestPolicy)
	if response.Code != http.StatusOK {
		t.Fatalf("unexpected policy PUT status: %s: %s", response.Result().Status, response.Body.String())
	}

	response = performJSONRequest(t, handler, http.MethodGet, "/api/v1/policy"+targetQuery, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("unexpected policy GET status: %s: %s", response.Result().Status, response.Body.String())
	}
	var stored map[string]json.RawMessage
	if err := json.NewDecoder(response.Body).Decode(&stored); err != nil {
		t.Fatal(err)
	}
	var retention map[string]json.RawMessage
	if err := json.Unmarshal(stored["retention"], &retention); err != nil {
		t.Fatal(err)
	}
	var keepDaily, keepLatest int
	var ignoreIdentical bool
	if err := json.Unmarshal(retention["keepDaily"], &keepDaily); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(retention["keepLatest"], &keepLatest); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(retention["ignoreIdenticalSnapshots"], &ignoreIdentical); err != nil {
		t.Fatal(err)
	}
	if keepDaily != 12 || keepLatest != 4 || !ignoreIdentical {
		t.Fatalf("unexpected retention response: %#v", retention)
	}
	if _, hasUppercaseField := retention["KeepDaily"]; hasUppercaseField {
		t.Fatalf("retention response does not match the lower-camel API contract: %#v", retention)
	}

	entries, err := direct.FindManifests(context.Background(), map[string]string{manifest.TypeLabelKey: snapshot.PolicyManifestType})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Labels["policyType"] != "path" {
		t.Fatalf("unexpected persisted policy labels: %#v", entries)
	}

	response = performJSONRequest(t, handler, http.MethodGet, "/api/v1/policies?host=policy-host", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("unexpected policies list status: %s", response.Result().Status)
	}
	var listed policyListResponse
	if err := json.NewDecoder(response.Body).Decode(&listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Policies) != 1 || listed.Policies[0].ID == "" ||
		listed.Policies[0].Target.Host != "policy-host" || listed.Policies[0].Target.UserName != "policy-user" ||
		listed.Policies[0].Target.Path != "/data/policy" || listed.Policies[0].Policy.Retention.KeepDaily == nil ||
		*listed.Policies[0].Policy.Retention.KeepDaily != 12 ||
		listed.Policies[0].Policy.Retention.IgnoreIdenticalSnapshots == nil ||
		!*listed.Policies[0].Policy.Retention.IgnoreIdenticalSnapshots {
		t.Fatalf("unexpected policies list response: %#v", listed)
	}

	response = performJSONRequest(t, handler, http.MethodGet, "/api/v1/policies?host=other-host", nil)
	if err := json.NewDecoder(response.Body).Decode(&listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Policies) != 0 {
		t.Fatalf("unexpected filtered policy list: %#v", listed)
	}

	response = performJSONRequest(t, handler, http.MethodDelete, "/api/v1/policy"+targetQuery, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("unexpected policy DELETE status: %s: %s", response.Result().Status, response.Body.String())
	}
	response = performJSONRequest(t, handler, http.MethodGet, "/api/v1/policy"+targetQuery, nil)
	if response.Code != http.StatusNotFound {
		t.Fatalf("unexpected missing policy status: %s", response.Result().Status)
	}

	response = performJSONRequest(t, handler, http.MethodPost, "/api/v1/policy/resolve"+targetQuery, map[string]any{})
	if response.Code != http.StatusNotImplemented {
		t.Fatalf("unexpected policy resolve status: %s", response.Result().Status)
	}
}
