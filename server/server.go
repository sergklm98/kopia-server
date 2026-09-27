package server

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/sergklm98/kopia-lib/repo"
)

const DefaultListenAddress = "127.0.0.1:51515"

type Config struct {
	ListenAddress        string
	FrontendDir          string
	RepositoryConfigPath string
	RepositoryPassword   string
	AuthUsername         string
	AuthPassword         string
}

type Server struct {
	config       Config
	http         *http.Server
	listen       net.Listener
	serve        chan error
	repositories *repositoryState
}

func New(config Config) (*Server, error) {
	if config.ListenAddress == "" {
		config.ListenAddress = DefaultListenAddress
	}
	if (config.AuthUsername == "") != (config.AuthPassword == "") {
		return nil, errors.New("server auth username and password must be provided together")
	}

	frontendDir, err := resolveFrontendDir(config.FrontendDir)
	if err != nil {
		return nil, err
	}
	config.FrontendDir = frontendDir

	repositories := &repositoryState{}
	return &Server{
		config: config,
		http: &http.Server{
			Handler: newHandlerWithState(config.FrontendDir, repositories, config.RepositoryConfigPath, config.RepositoryPassword, config.AuthUsername, config.AuthPassword),
		},
		serve:        make(chan error, 1),
		repositories: repositories,
	}, nil
}

func (s *Server) Start() error {
	if s.listen != nil {
		return errors.New("server already started")
	}

	listener, err := net.Listen("tcp", s.config.ListenAddress)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", s.config.ListenAddress, err)
	}

	s.listen = listener
	go func() {
		err := s.http.Serve(listener)
		if !errors.Is(err, http.ErrServerClosed) {
			s.serve <- err
		}
		close(s.serve)
	}()
	return nil
}

func (s *Server) OpenRepository(ctx context.Context) error {
	if s.config.RepositoryConfigPath == "" {
		return nil
	}

	r, err := repo.Open(ctx, s.config.RepositoryConfigPath, s.config.RepositoryPassword, nil)
	if err != nil {
		return fmt.Errorf("open repository: %w", err)
	}
	s.repositories.mu.Lock()
	s.repositories.repository = r
	s.repositories.mu.Unlock()
	return nil
}

func (s *Server) CloseRepository(ctx context.Context) error {
	s.repositories.mu.Lock()
	defer s.repositories.mu.Unlock()
	if s.repositories.repository == nil {
		return nil
	}
	err := s.repositories.repository.Close(ctx)
	s.repositories.repository = nil
	return err
}

func (s *Server) Addr() net.Addr {
	if s.listen == nil {
		return nil
	}
	return s.listen.Addr()
}

func (s *Server) Shutdown(ctx context.Context) error {
	if s.listen == nil {
		return nil
	}
	return s.http.Shutdown(ctx)
}

func (s *Server) Wait() error {
	return <-s.serve
}

func newHandler(frontendDir string, repository repo.Repository, configPath, authUsername, authPassword string) http.Handler {
	state := &repositoryState{repository: repository}
	return newHandlerWithState(frontendDir, state, configPath, "", authUsername, authPassword)
}

func newHandlerWithState(frontendDir string, repositories *repositoryState, configPath, repositoryPassword, authUsername, authPassword string) http.Handler {
	mux := http.NewServeMux()
	registerRepositoryRoutes(mux, repositories, configPath, repositoryPassword)
	registerPolicyRoutes(mux, repositories)
	registerSnapshotRoutes(mux, repositories)
	registerSourceRoutes(mux, repositories)
	registerPathRoutes(mux)
	registerObjectRoutes(mux, repositories)
	registerDataRoutes(mux)
	registerSystemRoutes(mux, configPath)
	registerNotificationRoutes(mux, repositories)
	var handler http.Handler = mux
	if frontendDir == "" {
		return basicAuthHandler(handler, authUsername, authPassword)
	}

	fileServer := http.FileServer(http.Dir(frontendDir))
	mux.Handle("/", frontendHandler(frontendDir, fileServer))
	return basicAuthHandler(handler, authUsername, authPassword)
}

func basicAuthHandler(next http.Handler, expectedUsername, expectedPassword string) http.Handler {
	if expectedUsername == "" {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok || subtle.ConstantTimeCompare([]byte(username), []byte(expectedUsername)) != 1 ||
			subtle.ConstantTimeCompare([]byte(password), []byte(expectedPassword)) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="Kopia"`)
			http.Error(w, "Access denied.\n", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func frontendHandler(frontendDir string, fileServer http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := filepath.Join(frontendDir, filepath.FromSlash(strings.TrimPrefix(r.URL.Path, "/")))
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			fileServer.ServeHTTP(w, r)
			return
		}

		indexPath := filepath.Join(frontendDir, "index.html")
		if _, err := os.Stat(indexPath); err != nil {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, indexPath)
	})
}

func resolveFrontendDir(frontendDir string) (string, error) {
	if frontendDir == "" {
		return "", nil
	}

	path, err := filepath.Abs(frontendDir)
	if err != nil {
		return "", fmt.Errorf("resolve frontend directory: %w", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("frontend directory: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("frontend path is not a directory: %s", path)
	}
	return path, nil
}
