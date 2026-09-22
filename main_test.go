package main

import "testing"

func TestServerConfigFlagsOverrideEnvironment(t *testing.T) {
	getenv := func(name string) string {
		values := map[string]string{
			"KOPIA_SERVER_ADDRESS":  "http://127.0.0.1:6000",
			"KOPIA_CONFIG":          "environment.json",
			"KOPIA_PASSWORD":        "environment-password",
			"KOPIA_FRONTEND_DIR":    "environment-ui",
			"KOPIA_SERVER_USERNAME": "environment-user",
			"KOPIA_SERVER_PASSWORD": "environment-server-password",
		}
		return values[name]
	}

	config, err := serverConfigFromFlags([]string{
		"--address", "127.0.0.1:7000",
		"--config", "command.json",
		"--password", "command-password",
		"--frontend-dir", "command-ui",
		"--server-username", "command-user",
		"--server-password", "command-server-password",
	}, getenv)
	if err != nil {
		t.Fatal(err)
	}

	if config.ListenAddress != "127.0.0.1:7000" ||
		config.RepositoryConfigPath != "command.json" ||
		config.RepositoryPassword != "command-password" ||
		config.FrontendDir != "command-ui" ||
		config.AuthUsername != "command-user" ||
		config.AuthPassword != "command-server-password" {
		t.Fatalf("unexpected config: %#v", config)
	}
}

func TestServerConfigUsesEnvironmentDefaults(t *testing.T) {
	getenv := func(name string) string {
		values := map[string]string{
			"KOPIA_SERVER_ADDRESS": "http://127.0.0.1:6000",
			"KOPIA_CONFIG":         "environment.json",
		}
		return values[name]
	}

	config, err := serverConfigFromFlags(nil, getenv)
	if err != nil {
		t.Fatal(err)
	}
	if config.ListenAddress != "127.0.0.1:6000" || config.RepositoryConfigPath != "environment.json" {
		t.Fatalf("unexpected config: %#v", config)
	}
}
