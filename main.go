package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/sergklm98/kopia-server/server"
)

func main() {
	config, err := serverConfigFromFlags(os.Args[1:], os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	s, err := server.New(config)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := s.OpenRepository(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer s.CloseRepository(context.Background()) //nolint:errcheck
	if err := s.Start(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	<-ctx.Done()
	if err := s.Shutdown(context.Background()); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func serverConfigFromFlags(args []string, getenv func(string) string) (server.Config, error) {
	defaults := server.Config{
		ListenAddress:        getenvOr(getenv, "KOPIA_SERVER_ADDRESS", server.DefaultListenAddress),
		FrontendDir:          getenv("KOPIA_FRONTEND_DIR"),
		RepositoryConfigPath: getenv("KOPIA_CONFIG"),
		RepositoryPassword:   getenv("KOPIA_PASSWORD"),
		AuthUsername:         getenv("KOPIA_SERVER_USERNAME"),
		AuthPassword:         getenv("KOPIA_SERVER_PASSWORD"),
	}

	flags := flag.NewFlagSet("kopia-server", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	address := flags.String("address", defaults.ListenAddress, "server address")
	frontendDir := flags.String("frontend-dir", defaults.FrontendDir, "external frontend directory")
	configPath := flags.String("config", defaults.RepositoryConfigPath, "repository configuration file")
	password := flags.String("password", defaults.RepositoryPassword, "repository password")
	authUsername := flags.String("server-username", defaults.AuthUsername, "server authentication username")
	authPassword := flags.String("server-password", defaults.AuthPassword, "server authentication password")
	if err := flags.Parse(args); err != nil {
		return server.Config{}, err
	}

	return server.Config{
		ListenAddress:        normalizeListenAddress(*address),
		FrontendDir:          *frontendDir,
		RepositoryConfigPath: *configPath,
		RepositoryPassword:   *password,
		AuthUsername:         *authUsername,
		AuthPassword:         *authPassword,
	}, nil
}

func getenvOr(getenv func(string) string, name, fallback string) string {
	if value := getenv(name); value != "" {
		return value
	}
	return fallback
}

func normalizeListenAddress(address string) string {
	return strings.TrimPrefix(strings.TrimPrefix(address, "http://"), "https://")
}
