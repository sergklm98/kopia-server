package server

import (
	"encoding/json"
	"errors"
	"net/http"
)

func registerTaskAndControlRoutes(mux *http.ServeMux, tasks *TaskManager) {
	mux.Handle("/api/v1/repo/sync", notImplementedMethodHandler(http.MethodPost, "repository sync requires server refresh and source orchestration"))
	mux.Handle("/api/v1/refresh", notImplementedMethodHandler(http.MethodPost, "refresh requires server source and maintenance orchestration"))

	mux.Handle("/api/v1/tasks-summary", taskSummaryHandler(tasks))
	mux.Handle("/api/v1/tasks", taskListHandler(tasks))
	mux.Handle("/api/v1/tasks/{taskID}", taskInfoHandler(tasks))
	mux.Handle("/api/v1/tasks/{taskID}/logs", taskLogsHandler(tasks))
	mux.Handle("/api/v1/tasks/{taskID}/cancel", taskCancelHandler(tasks))

	mux.Handle("/api/v1/control/sources", notImplementedMethodHandler(http.MethodGet, "control API requires server-control authentication and source managers"))
	mux.Handle("/api/v1/control/status", notImplementedMethodHandler(http.MethodGet, "control API requires server-control authentication"))
	mux.Handle("/api/v1/control/flush", notImplementedMethodHandler(http.MethodPost, "control API requires server-control authentication and a flush manager"))
	mux.Handle("/api/v1/control/refresh", notImplementedMethodHandler(http.MethodPost, "control API requires server-control authentication and refresh orchestration"))
	mux.Handle("/api/v1/control/shutdown", notImplementedMethodHandler(http.MethodPost, "control API requires server-control authentication and server lifecycle access"))
	mux.Handle("/api/v1/control/trigger-snapshot", notImplementedMethodHandler(http.MethodPost, "control API requires server-control authentication and source managers"))
	mux.Handle("/api/v1/control/cancel-snapshot", notImplementedMethodHandler(http.MethodPost, "control API requires server-control authentication and task cancellation"))
	mux.Handle("/api/v1/control/pause-source", notImplementedMethodHandler(http.MethodPost, "control API requires server-control authentication and source managers"))
	mux.Handle("/api/v1/control/resume-source", notImplementedMethodHandler(http.MethodPost, "control API requires server-control authentication and source managers"))
	mux.Handle("/api/v1/control/throttle", notImplementedMethodHandler(http.MethodGet+", "+http.MethodPut, "control API requires server-control authentication"))
}

func taskListHandler(tasks *TaskManager) http.Handler {
	return taskMethodHandler(http.MethodGet, func(w http.ResponseWriter, _ *http.Request) {
		items := tasks.ListTasks()
		if items == nil {
			items = []TaskInfo{}
		}
		writeRepositoryJSON(w, http.StatusOK, struct {
			Tasks []TaskInfo `json:"tasks"`
		}{Tasks: items})
	})
}

func taskSummaryHandler(tasks *TaskManager) http.Handler {
	return taskMethodHandler(http.MethodGet, func(w http.ResponseWriter, _ *http.Request) {
		writeRepositoryJSON(w, http.StatusOK, tasks.TaskSummary())
	})
}

func taskInfoHandler(tasks *TaskManager) http.Handler {
	return taskMethodHandler(http.MethodGet, func(w http.ResponseWriter, r *http.Request) {
		info, ok := tasks.GetTask(r.PathValue("taskID"))
		if !ok {
			writeRepositoryError(w, http.StatusNotFound, "NOT_FOUND", "task not found")
			return
		}
		writeRepositoryJSON(w, http.StatusOK, info)
	})
}

func taskLogsHandler(tasks *TaskManager) http.Handler {
	return taskMethodHandler(http.MethodGet, func(w http.ResponseWriter, r *http.Request) {
		logs := tasks.TaskLog(r.PathValue("taskID"))
		writeRepositoryJSON(w, http.StatusOK, struct {
			Logs []json.RawMessage `json:"logs"`
		}{Logs: logs})
	})
}

func taskCancelHandler(tasks *TaskManager) http.Handler {
	return taskMethodHandler(http.MethodPost, func(w http.ResponseWriter, r *http.Request) {
		tasks.CancelTask(r.PathValue("taskID"))
		writeRepositoryJSON(w, http.StatusOK, struct{}{})
	})
}

func taskMethodHandler(method string, next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method {
			w.Header().Set("Allow", method)
			writeRepositoryAPIError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
			return
		}
		next.ServeHTTP(w, r)
	})
}
