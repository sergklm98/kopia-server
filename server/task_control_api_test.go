package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestTaskAndControlRoutesAreExplicitlyRegistered(t *testing.T) {
	handler := newHandler("", nil, "", "", "")
	for _, test := range []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/v1/repo/sync"},
		{http.MethodPost, "/api/v1/refresh"},
		{http.MethodGet, "/api/v1/control/sources"},
		{http.MethodGet, "/api/v1/control/status"},
		{http.MethodPost, "/api/v1/control/flush"},
		{http.MethodPost, "/api/v1/control/refresh"},
		{http.MethodPost, "/api/v1/control/shutdown"},
		{http.MethodPost, "/api/v1/control/trigger-snapshot"},
		{http.MethodPost, "/api/v1/control/cancel-snapshot"},
		{http.MethodPost, "/api/v1/control/pause-source"},
		{http.MethodPost, "/api/v1/control/resume-source"},
		{http.MethodGet, "/api/v1/control/throttle"},
		{http.MethodPut, "/api/v1/control/throttle"},
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
		{http.MethodGet, "/api/v1/repo/sync"},
		{http.MethodPost, "/api/v1/tasks"},
		{http.MethodPost, "/api/v1/control/status"},
	} {
		response := performJSONRequest(t, handler, test.method, test.path, nil)
		if response.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s %s returned %s, want 405", test.method, test.path, response.Result().Status)
		}
	}
}

func TestTaskAPIsExposeLifecycleProgressCountersAndLogs(t *testing.T) {
	server, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	finish := make(chan struct{})
	taskID := server.tasks.StartTask(context.Background(), "Snapshot", "source path", func(_ context.Context, controller TaskController) error {
		controller.ReportProgressInfo("reading files")
		controller.ReportCounters(map[string]TaskCounter{"files": {Value: 3}})
		controller.ReportLog("test", 1, "upload started", nil)
		close(started)
		<-finish
		return nil
	})
	<-started
	handler := server.http.Handler

	response := performJSONRequest(t, handler, http.MethodGet, "/api/v1/tasks", nil)
	var listed struct {
		Tasks []TaskInfo `json:"tasks"`
	}
	if err := json.NewDecoder(response.Body).Decode(&listed); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || len(listed.Tasks) != 1 || listed.Tasks[0].ID != taskID ||
		listed.Tasks[0].Status != TaskRunning || listed.Tasks[0].ProgressInfo != "reading files" || listed.Tasks[0].Counters["files"].Value != 3 {
		t.Fatalf("unexpected task list response (%s): %#v", response.Result().Status, listed)
	}

	response = performJSONRequest(t, handler, http.MethodGet, "/api/v1/tasks-summary", nil)
	var summary map[string]int
	if err := json.NewDecoder(response.Body).Decode(&summary); err != nil {
		t.Fatal(err)
	}
	if summary[string(TaskRunning)] != 1 {
		t.Fatalf("unexpected running task summary: %#v", summary)
	}

	response = performJSONRequest(t, handler, http.MethodGet, "/api/v1/tasks/"+taskID, nil)
	var detail TaskInfo
	if err := json.NewDecoder(response.Body).Decode(&detail); err != nil {
		t.Fatal(err)
	}
	if detail.ID != taskID || detail.Description != "source path" {
		t.Fatalf("unexpected task detail: %#v", detail)
	}

	response = performJSONRequest(t, handler, http.MethodGet, "/api/v1/tasks/"+taskID+"/logs", nil)
	var logResponse struct {
		Logs []json.RawMessage `json:"logs"`
	}
	if err := json.NewDecoder(response.Body).Decode(&logResponse); err != nil {
		t.Fatal(err)
	}
	if len(logResponse.Logs) != 1 || !strings.Contains(string(logResponse.Logs[0]), `"msg":"upload started"`) {
		t.Fatalf("unexpected task log: %#v", logResponse)
	}

	close(finish)
	waitCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	completed, ok := server.tasks.WaitForTask(waitCtx, taskID)
	if !ok || completed.Status != TaskSuccess || completed.EndTime == nil || completed.ProgressInfo != "" {
		t.Fatalf("unexpected completed task: %#v (found=%v)", completed, ok)
	}
}

func TestTaskCancelAPI(t *testing.T) {
	server, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	cancelCallback := make(chan struct{})
	taskID := server.tasks.StartTask(context.Background(), "Snapshot", "cancel me", func(ctx context.Context, controller TaskController) error {
		controller.OnCancel(func() { close(cancelCallback) })
		close(started)
		<-ctx.Done()
		return ctx.Err()
	})
	<-started

	response := performJSONRequest(t, server.http.Handler, http.MethodPost, "/api/v1/tasks/"+taskID+"/cancel", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("unexpected cancel status: %s", response.Result().Status)
	}
	select {
	case <-cancelCallback:
	case <-time.After(time.Second):
		t.Fatal("cancel callback was not invoked")
	}
	waitCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	completed, ok := server.tasks.WaitForTask(waitCtx, taskID)
	if !ok || completed.Status != TaskCanceled {
		t.Fatalf("unexpected canceled task: %#v (found=%v)", completed, ok)
	}

	response = performJSONRequest(t, server.http.Handler, http.MethodGet, "/api/v1/tasks/missing", nil)
	if response.Code != http.StatusNotFound {
		t.Fatalf("unexpected missing-task status: %s", response.Result().Status)
	}
}

func TestTaskManagerRetainsOnlyConfiguredFinishedTasks(t *testing.T) {
	manager := NewTaskManager()
	manager.MaxFinishedTasks = 1
	for index := range 2 {
		id := manager.StartTask(context.Background(), "Test", "finished", func(_ context.Context, _ TaskController) error {
			return nil
		})
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		_, ok := manager.WaitForTask(ctx, id)
		cancel()
		if !ok {
			t.Fatalf("task %d did not finish", index)
		}
	}
	items := manager.ListTasks()
	if len(items) != 1 || items[0].Description != "finished" {
		t.Fatalf("unexpected retained task history: %#v", items)
	}
}

func TestTaskManagerBoundsTaskLogs(t *testing.T) {
	manager := NewTaskManager()
	manager.MaxLogMessagesPerTask = 2
	taskID := manager.StartTask(context.Background(), "Test", "logs", func(_ context.Context, controller TaskController) error {
		controller.ReportLog("test", 1, "first", nil)
		controller.ReportLog("test", 1, "second", nil)
		controller.ReportLog("test", 1, "third", nil)
		return nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, ok := manager.WaitForTask(ctx, taskID); !ok {
		t.Fatal("task did not finish")
	}
	logs := manager.TaskLog(taskID)
	if len(logs) != 2 || !strings.Contains(string(logs[0]), `"msg":"second"`) ||
		!strings.Contains(string(logs[1]), `"msg":"third"`) {
		t.Fatalf("unexpected bounded task logs: %s", logs)
	}
}

func TestTaskManagerMarksErrorsFailed(t *testing.T) {
	manager := NewTaskManager()
	wantError := errors.New("task failed")
	taskID := manager.StartTask(context.Background(), "Test", "failure", func(_ context.Context, _ TaskController) error {
		return wantError
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	info, ok := manager.WaitForTask(ctx, taskID)
	if !ok || info.Status != TaskFailed || info.ErrorMessage != wantError.Error() {
		t.Fatalf("unexpected failed task: %#v (found=%v)", info, ok)
	}
}

func TestServerShutdownCancelsAndWaitsForTasks(t *testing.T) {
	server, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	taskID := server.tasks.StartTask(context.Background(), "Test", "shutdown", func(ctx context.Context, _ TaskController) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	})
	<-started

	shutdownContext, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownContext); err != nil {
		t.Fatal(err)
	}
	task, ok := server.tasks.GetTask(taskID)
	if !ok || task.Status != TaskCanceled {
		t.Fatalf("server shutdown did not finish its task as canceled: %#v (found=%v)", task, ok)
	}
}
