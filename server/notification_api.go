package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/sergklm98/kopia-lib/notification"
	"github.com/sergklm98/kopia-lib/repo"
)

func registerNotificationRoutes(mux *http.ServeMux, repositories *repositoryState) {
	mux.Handle("/api/v1/notificationProfiles", repositories.readHandler(notificationProfileCollectionHandler(repositories)))
	mux.Handle("/api/v1/notificationProfiles/", repositories.readHandler(notificationProfileItemHandler(repositories)))
	mux.Handle("/api/v1/testNotificationProfile", repositories.readHandler(notificationProfileTestHandler(repositories)))
}

func notificationProfileCollectionHandler(repositories *repositoryState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		repository := repositories.repository
		if repository == nil {
			writeNotificationAPIError(w, http.StatusServiceUnavailable, errors.New("repository is not connected"))
			return
		}
		switch r.Method {
		case http.MethodGet:
			profiles, err := notification.ListProfiles(r.Context(), repository)
			if err != nil {
				writeNotificationAPIError(w, http.StatusInternalServerError, err)
				return
			}
			writeNotificationJSON(w, http.StatusOK, profiles)
		case http.MethodPost:
			var profile notification.ProfileConfig
			if err := json.NewDecoder(r.Body).Decode(&profile); err != nil {
				writeNotificationAPIError(w, http.StatusBadRequest, fmt.Errorf("malformed request body: %w", err))
				return
			}
			if err := writeNotificationProfile(r.Context(), repository, "NotificationProfileCreate", func(ctx context.Context, writer repo.RepositoryWriter) error {
				return notification.SaveProfile(ctx, writer, profile)
			}); err != nil {
				writeNotificationAPIError(w, http.StatusInternalServerError, err)
				return
			}
			writeNotificationJSON(w, http.StatusOK, struct{}{})
		default:
			w.Header().Set("Allow", "GET, POST")
			writeNotificationAPIError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
		}
	}
}

func notificationProfileItemHandler(repositories *repositoryState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		repository := repositories.repository
		if repository == nil {
			writeNotificationAPIError(w, http.StatusServiceUnavailable, errors.New("repository is not connected"))
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/api/v1/notificationProfiles/")
		if name == "" || strings.Contains(name, "/") {
			http.NotFound(w, r)
			return
		}
		switch r.Method {
		case http.MethodGet:
			profile, err := notification.GetProfile(r.Context(), repository, name)
			if err != nil {
				writeNotificationAPIError(w, http.StatusInternalServerError, err)
				return
			}
			writeNotificationJSON(w, http.StatusOK, profile)
		case http.MethodDelete:
			if err := writeNotificationProfile(r.Context(), repository, "NotificationProfileDelete", func(ctx context.Context, writer repo.RepositoryWriter) error {
				return notification.DeleteProfile(ctx, writer, name)
			}); err != nil {
				writeNotificationAPIError(w, http.StatusInternalServerError, err)
				return
			}
			writeNotificationJSON(w, http.StatusOK, struct{}{})
		default:
			w.Header().Set("Allow", "GET, DELETE")
			writeNotificationAPIError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
		}
	}
}

func notificationProfileTestHandler(repositories *repositoryState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		repository := repositories.repository
		if repository == nil {
			writeNotificationAPIError(w, http.StatusServiceUnavailable, errors.New("repository is not connected"))
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			writeNotificationAPIError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
			return
		}
		var profile notification.ProfileConfig
		if err := json.NewDecoder(r.Body).Decode(&profile); err != nil {
			writeNotificationAPIError(w, http.StatusBadRequest, fmt.Errorf("malformed request body: %w", err))
			return
		}
		sender, err := notification.GetSender(r.Context(), profile.ProfileName, profile.MethodConfig)
		if err != nil {
			writeNotificationAPIError(w, http.StatusBadRequest, fmt.Errorf("unable to construct sender: %w", err))
			return
		}
		if err := notification.SendTestNotification(r.Context(), repository, sender); err != nil {
			writeNotificationAPIError(w, http.StatusBadRequest, fmt.Errorf("unable to send notification: %w", err))
			return
		}
		writeNotificationJSON(w, http.StatusOK, struct{}{})
	}
}

func writeNotificationProfile(ctx context.Context, repository repo.Repository, purpose string, action func(context.Context, repo.RepositoryWriter) error) error {
	writerCtx, writer, err := repository.NewWriter(ctx, repo.WriteSessionOptions{Purpose: purpose})
	if err != nil {
		return fmt.Errorf("create repository write session: %w", err)
	}
	defer writer.Close(writerCtx) //nolint:errcheck
	if err := action(writerCtx, writer); err != nil {
		return err
	}
	return writer.Flush(writerCtx)
}

func writeNotificationJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeNotificationAPIError(w http.ResponseWriter, status int, err error) {
	writeNotificationJSON(w, status, map[string]string{"error": err.Error()})
}
