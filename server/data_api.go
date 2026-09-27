package server

import (
	"errors"
	"net/http"
	"strings"
)

func registerDataRoutes(mux *http.ServeMux) {
	mux.Handle("/api/v1/restore", notImplementedMethodHandler(http.MethodPost, "restore requires a host filesystem adapter and task manager"))
	mux.Handle("/api/v1/estimate", notImplementedMethodHandler(http.MethodPost, "estimate requires a host filesystem adapter and task manager"))
	mux.Handle("/api/v1/mounts", notImplementedMethodHandler(http.MethodGet+", "+http.MethodPost, "mounts require a platform mount manager"))
	mux.Handle("/api/v1/mounts/", mountItemNotImplementedHandler())
}

func notImplementedMethodHandler(methods, reason string) http.Handler {
	allowedMethods := strings.Split(methods, ", ")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !containsMethod(allowedMethods, r.Method) {
			w.Header().Set("Allow", strings.Join(allowedMethods, ", "))
			writeRepositoryAPIError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
			return
		}
		writeRepositoryError(w, http.StatusNotImplemented, "INTERNAL", reason)
	})
}

func mountItemNotImplementedHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		itemPath := strings.TrimPrefix(r.URL.Path, "/api/v1/mounts/")
		if itemPath == "" || strings.Contains(itemPath, "/") {
			http.NotFound(w, r)
			return
		}
		if !containsMethod([]string{http.MethodGet, http.MethodDelete}, r.Method) {
			w.Header().Set("Allow", "GET, DELETE")
			writeRepositoryAPIError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
			return
		}
		writeRepositoryError(w, http.StatusNotImplemented, "INTERNAL", "mounts require a platform mount manager")
	})
}

func containsMethod(methods []string, method string) bool {
	for _, allowed := range methods {
		if method == allowed {
			return true
		}
	}
	return false
}
