package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sergklm98/kopia-lib/repo"
)

func TestPathResolveAPI(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	handler := newHandler("", nil, "", "", "")

	for _, test := range []struct {
		path string
		want string
	}{
		{path: "~/work/../data", want: filepath.Join(home, "data")},
		{path: "relative/path", want: filepath.Join(home, "relative", "path")},
		{path: filepath.Join(home, "absolute", "..", "target"), want: filepath.Join(home, "target")},
	} {
		response := performJSONRequest(t, handler, http.MethodPost, "/api/v1/paths/resolve", resolvePathRequest{Path: test.path})
		if response.Code != http.StatusOK {
			t.Fatalf("unexpected path resolve status for %q: %s", test.path, response.Result().Status)
		}
		var result resolvePathResponse
		if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
			t.Fatal(err)
		}
		if result.Source.Path != test.want || result.Source.Host != repo.GetDefaultHostName(t.Context()) ||
			result.Source.UserName != repo.GetDefaultUserName(t.Context()) {
			t.Fatalf("unexpected resolved source for %q: %#v", test.path, result.Source)
		}
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/paths/resolve", strings.NewReader("{"))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unexpected malformed request status: %s", response.Result().Status)
	}
}
