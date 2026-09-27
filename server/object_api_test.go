package server

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sergklm98/kopia-lib/repo"
	"github.com/sergklm98/kopia-lib/repo/object"
)

func TestObjectGetAPI(t *testing.T) {
	repository := openNotificationAPITestRepository(t)
	writerRepository, ok := repository.(repo.RepositoryWriter)
	if !ok {
		t.Fatal("test repository is not writable")
	}
	ctx := context.Background()
	regularID := writeTestObject(t, ctx, writerRepository, object.WriterOptions{}, []byte("0123456789"))
	directoryID := writeTestObject(t, ctx, writerRepository, object.WriterOptions{Prefix: "k"}, []byte(`{"entries":[]}`))
	handler := newHandler("", repository, "", "", "")

	request := httptest.NewRequest(http.MethodGet, "/api/v1/objects/"+regularID.String()+"?fname=report.txt&mtime=2026-09-27T10:00:00Z", nil)
	request.Header.Set("Range", "bytes=2-5")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusPartialContent || response.Body.String() != "2345" ||
		response.Header().Get("Accept-Ranges") != "bytes" || !strings.Contains(response.Header().Get("Content-Disposition"), "report.txt") {
		t.Fatalf("unexpected ranged object response: status=%s headers=%v body=%q", response.Result().Status, response.Header(), response.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/api/v1/objects/"+directoryID.String(), nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "application/json" || response.Body.String() != `{"entries":[]}` {
		t.Fatalf("unexpected directory object response: status=%s content-type=%q body=%q", response.Result().Status, response.Header().Get("Content-Type"), response.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/api/v1/objects/not-an-object-id", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "invalid object id") {
		t.Fatalf("unexpected invalid object ID response: %s: %q", response.Result().Status, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/api/v1/objects/aa", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), "object not found") {
		t.Fatalf("unexpected missing object response: %s: %q", response.Result().Status, response.Body.String())
	}
}

func writeTestObject(t *testing.T, ctx context.Context, repository repo.RepositoryWriter, options object.WriterOptions, data []byte) object.ID {
	t.Helper()
	writer := repository.NewObjectWriter(ctx, options)
	if _, err := io.Copy(writer, bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
	id, err := writer.Result()
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return id
}
