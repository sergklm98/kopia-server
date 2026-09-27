package server

import (
	"net/http"
	"testing"
)

func TestUnimplementedDataAPIRoutes(t *testing.T) {
	handler := newHandler("", nil, "", "", "")
	for _, test := range []struct {
		method string
		path   string
	}{
		{method: http.MethodPost, path: "/api/v1/restore"},
		{method: http.MethodPost, path: "/api/v1/estimate"},
		{method: http.MethodGet, path: "/api/v1/mounts"},
		{method: http.MethodPost, path: "/api/v1/mounts"},
		{method: http.MethodGet, path: "/api/v1/mounts/aa"},
		{method: http.MethodDelete, path: "/api/v1/mounts/aa"},
	} {
		response := performJSONRequest(t, handler, test.method, test.path, nil)
		if response.Code != http.StatusNotImplemented {
			t.Errorf("%s %s returned %s, want 501", test.method, test.path, response.Result().Status)
		}
	}

	for _, test := range []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: "/api/v1/restore"},
		{method: http.MethodPost, path: "/api/v1/mounts/aa"},
		{method: http.MethodGet, path: "/api/v1/mounts/aa/child"},
	} {
		response := performJSONRequest(t, handler, test.method, test.path, nil)
		if response.Code != http.StatusMethodNotAllowed && response.Code != http.StatusNotFound {
			t.Errorf("%s %s returned %s, want 405 or 404", test.method, test.path, response.Result().Status)
		}
	}
}
