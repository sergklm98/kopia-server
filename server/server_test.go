package server

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestServerServesAPIAndFrontendFallback(t *testing.T) {
	frontendDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(frontendDir, "index.html"), []byte("app"), 0o600); err != nil {
		t.Fatal(err)
	}

	s, err := New(Config{ListenAddress: "127.0.0.1:0", FrontendDir: frontendDir})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	defer s.Shutdown(context.Background()) //nolint:errcheck

	response, err := http.Get("http://" + s.Addr().String() + "/api/v1/repo/status")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: %s", response.Status)
	}
	var status map[string]any
	if err := json.NewDecoder(response.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	if status["apiVersion"] != "v1" || status["connected"] != false {
		t.Fatalf("unexpected response: %#v", status)
	}

	response, err = http.Get("http://" + s.Addr().String() + "/settings/sources")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("unexpected frontend status: %s", response.Status)
	}

	response, err = http.Get("http://" + s.Addr().String() + "/api/v1/cli")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("unexpected cli status: %s", response.Status)
	}
	var cliInfo map[string]string
	if err := json.NewDecoder(response.Body).Decode(&cliInfo); err != nil {
		t.Fatal(err)
	}
	if cliInfo["executable"] == "" {
		t.Fatalf("missing executable in response: %#v", cliInfo)
	}

	response, err = http.Get("http://" + s.Addr().String() + "/api/v1/current-user")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("unexpected current-user status: %s", response.Status)
	}
	var currentUser map[string]string
	if err := json.NewDecoder(response.Body).Decode(&currentUser); err != nil {
		t.Fatal(err)
	}
	if currentUser["username"] == "" || currentUser["hostname"] == "" {
		t.Fatalf("incomplete current-user response: %#v", currentUser)
	}
}

func TestServerRequiresConfiguredBasicAuth(t *testing.T) {
	s, err := New(Config{
		ListenAddress: "127.0.0.1:0",
		AuthUsername:  "admin",
		AuthPassword:  "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	defer s.Shutdown(context.Background()) //nolint:errcheck

	client := &http.Client{}
	request, err := http.NewRequest(http.MethodGet, "http://"+s.Addr().String()+"/api/v1/repo/status", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized || response.Header.Get("WWW-Authenticate") == "" {
		t.Fatalf("unexpected unauthenticated response: %s", response.Status)
	}

	request, err = http.NewRequest(http.MethodGet, "http://"+s.Addr().String()+"/api/v1/repo/status", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.SetBasicAuth("admin", "secret")
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("unexpected authenticated response: %s", response.Status)
	}
}
