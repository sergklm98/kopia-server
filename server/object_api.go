package server

import (
	"errors"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/sergklm98/kopia-lib/repo/object"
)

func registerObjectRoutes(mux *http.ServeMux, repositories *repositoryState) {
	mux.Handle("/api/v1/objects/", repositories.readHandler(objectGetHandler(repositories)))
}

func objectGetHandler(repositories *repositoryState) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		repository := repositories.repository
		if repository == nil {
			http.Error(w, "not connected", http.StatusBadRequest)
			return
		}
		objectIDString := strings.TrimPrefix(r.URL.Path, "/api/v1/objects/")
		if objectIDString == "" || strings.Contains(objectIDString, "/") {
			http.NotFound(w, r)
			return
		}
		objectID, err := object.ParseID(objectIDString)
		if err != nil {
			http.Error(w, "invalid object id", http.StatusBadRequest)
			return
		}

		objectReader, err := repository.OpenObject(r.Context(), objectID)
		if errors.Is(err, object.ErrObjectNotFound) {
			http.Error(w, "object not found", http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer objectReader.Close() //nolint:errcheck

		if isDirectoryObjectID(objectID) {
			w.Header().Set("Content-Type", "application/json")
		}
		filename := objectID.String()
		if requestedFilename := r.URL.Query().Get("fname"); requestedFilename != "" {
			filename = requestedFilename
			if disposition := mime.FormatMediaType("attachment", map[string]string{"filename": requestedFilename}); disposition != "" {
				w.Header().Set("Content-Disposition", disposition)
			}
		}

		modifiedAt := time.Now()
		if value := r.URL.Query().Get("mtime"); value != "" {
			if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
				modifiedAt = parsed
			}
		}
		http.ServeContent(w, r, filename, modifiedAt, objectReader)
	})
}

func isDirectoryObjectID(id object.ID) bool {
	if underlyingID, ok := id.IndexObjectID(); ok {
		return isDirectoryObjectID(underlyingID)
	}
	contentID, _, ok := id.ContentID()
	return ok && contentID.Prefix() == "k"
}
