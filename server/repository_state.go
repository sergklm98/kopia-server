package server

import (
	"net/http"
	"sync"

	"github.com/sergklm98/kopia-lib/repo"
)

type repositoryState struct {
	mu         sync.RWMutex
	repository repo.Repository
}

func (s *repositoryState) readHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.RLock()
		defer s.mu.RUnlock()
		next.ServeHTTP(w, r)
	})
}
