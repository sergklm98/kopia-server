package server

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"
)

const (
	defaultMaxFinishedTasks   = 50
	defaultMaxTaskLogMessages = 1000
)

type TaskStatus string

const (
	TaskRunning   TaskStatus = "RUNNING"
	TaskCanceling TaskStatus = "CANCELING"
	TaskCanceled  TaskStatus = "CANCELED"
	TaskSuccess   TaskStatus = "SUCCESS"
	TaskFailed    TaskStatus = "FAILED"
)

type TaskCounter struct {
	Value int64  `json:"value"`
	Units string `json:"units,omitempty"`
	Level string `json:"level"`
}

type TaskInfo struct {
	ID           string                 `json:"id"`
	StartTime    time.Time              `json:"startTime"`
	EndTime      *time.Time             `json:"endTime,omitempty"`
	Kind         string                 `json:"kind"`
	Description  string                 `json:"description"`
	Status       TaskStatus             `json:"status"`
	ProgressInfo string                 `json:"progressInfo"`
	ErrorMessage string                 `json:"errorMessage,omitempty"`
	Counters     map[string]TaskCounter `json:"counters"`
	LogLines     []json.RawMessage      `json:"-"`

	sequenceNumber int
}

type TaskController interface {
	CurrentTaskID() string
	OnCancel(context.CancelFunc)
	ReportCounters(map[string]TaskCounter)
	ReportProgressInfo(string)
	ReportLog(module string, level int, message string, fields map[string]any)
}

type TaskFunc func(context.Context, TaskController) error

type TaskManager struct {
	mu                    sync.Mutex
	nextTaskID            int
	running               map[string]*managedTask
	finished              map[string]*TaskInfo
	MaxFinishedTasks      int
	MaxLogMessagesPerTask int
}

type managedTask struct {
	mu sync.Mutex
	TaskInfo
	cancelContext context.CancelFunc
	cancelActions []context.CancelFunc
	maxLogs       int
	done          chan struct{}
}

func NewTaskManager() *TaskManager {
	return &TaskManager{
		running:               map[string]*managedTask{},
		finished:              map[string]*TaskInfo{},
		MaxFinishedTasks:      defaultMaxFinishedTasks,
		MaxLogMessagesPerTask: defaultMaxTaskLogMessages,
	}
}

func (m *TaskManager) StartTask(ctx context.Context, kind, description string, task TaskFunc) string {
	taskContext, cancel := context.WithCancel(ctx)
	managed := &managedTask{
		TaskInfo: TaskInfo{
			Kind:        kind,
			Description: description,
			Status:      TaskRunning,
			Counters:    map[string]TaskCounter{},
		},
		cancelContext: cancel,
		maxLogs:       m.MaxLogMessagesPerTask,
		done:          make(chan struct{}),
	}

	m.mu.Lock()
	m.nextTaskID++
	managed.ID = fmt.Sprintf("%x", m.nextTaskID)
	managed.StartTime = time.Now()
	managed.sequenceNumber = m.nextTaskID
	m.running[managed.ID] = managed
	m.mu.Unlock()

	go func() {
		err := task(taskContext, managed)
		cancel()
		m.completeTask(managed, err)
	}()

	return managed.ID
}

func (m *TaskManager) ListTasks() []TaskInfo {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := make([]TaskInfo, 0, len(m.running)+len(m.finished))
	for _, task := range m.running {
		result = append(result, task.info())
	}
	for _, task := range m.finished {
		result = append(result, cloneTaskInfo(*task))
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].sequenceNumber > result[j].sequenceNumber
	})
	return result
}

func (m *TaskManager) TaskSummary() map[TaskStatus]int {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := map[TaskStatus]int{TaskRunning: len(m.running)}
	for _, task := range m.finished {
		result[task.Status]++
	}
	return result
}

func (m *TaskManager) GetTask(taskID string) (TaskInfo, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if task := m.running[taskID]; task != nil {
		return task.info(), true
	}
	if task := m.finished[taskID]; task != nil {
		return cloneTaskInfo(*task), true
	}
	return TaskInfo{}, false
}

func (m *TaskManager) WaitForTask(ctx context.Context, taskID string) (TaskInfo, bool) {
	m.mu.Lock()
	task := m.running[taskID]
	if task == nil {
		if finished := m.finished[taskID]; finished != nil {
			info := cloneTaskInfo(*finished)
			m.mu.Unlock()
			return info, true
		}
		m.mu.Unlock()
		return TaskInfo{}, false
	}
	done := task.done
	m.mu.Unlock()

	select {
	case <-done:
		info, ok := m.GetTask(taskID)
		return info, ok
	case <-ctx.Done():
		return TaskInfo{}, false
	}
}

func (m *TaskManager) TaskLog(taskID string) []json.RawMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	if task := m.running[taskID]; task != nil {
		return task.logs()
	}
	if task := m.finished[taskID]; task != nil {
		return cloneTaskLogs(task.LogLines)
	}
	return nil
}

func (m *TaskManager) CancelTask(taskID string) {
	m.mu.Lock()
	task := m.running[taskID]
	m.mu.Unlock()
	if task == nil {
		return
	}
	task.cancel()
}

func (m *TaskManager) CancelAll() {
	m.mu.Lock()
	running := make([]*managedTask, 0, len(m.running))
	for _, task := range m.running {
		running = append(running, task)
	}
	m.mu.Unlock()
	for _, task := range running {
		task.cancel()
	}
}

func (m *TaskManager) WaitForIdle(ctx context.Context) error {
	for {
		m.mu.Lock()
		var done <-chan struct{}
		for _, task := range m.running {
			done = task.done
			break
		}
		m.mu.Unlock()
		if done == nil {
			return nil
		}

		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (m *TaskManager) completeTask(task *managedTask, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	task.mu.Lock()
	defer task.mu.Unlock()
	if err != nil {
		task.ErrorMessage = err.Error()
	}
	if task.Status == TaskCanceling {
		task.Status = TaskCanceled
	} else if err != nil {
		task.Status = TaskFailed
	} else {
		task.Status = TaskSuccess
	}
	task.ProgressInfo = ""
	now := time.Now()
	task.EndTime = &now

	delete(m.running, task.ID)
	finished := cloneTaskInfo(task.TaskInfo)
	m.finished[task.ID] = &finished
	close(task.done)
	for len(m.finished) > m.MaxFinishedTasks {
		oldestID := ""
		oldestSequence := int(^uint(0) >> 1)
		for id, candidate := range m.finished {
			if candidate.sequenceNumber < oldestSequence {
				oldestID = id
				oldestSequence = candidate.sequenceNumber
			}
		}
		delete(m.finished, oldestID)
	}
}

func (t *managedTask) CurrentTaskID() string {
	return t.ID
}

func (t *managedTask) OnCancel(cancel context.CancelFunc) {
	t.mu.Lock()
	if t.Status != TaskCanceling {
		t.cancelActions = append(t.cancelActions, cancel)
		t.mu.Unlock()
		return
	}
	t.mu.Unlock()
	go cancel()
}

func (t *managedTask) cancel() {
	t.mu.Lock()
	if t.Status != TaskRunning {
		t.mu.Unlock()
		return
	}
	t.Status = TaskCanceling
	actions := append([]context.CancelFunc(nil), t.cancelActions...)
	t.cancelActions = nil
	cancelContext := t.cancelContext
	t.mu.Unlock()

	cancelContext()
	for _, cancel := range actions {
		go cancel()
	}
}

func (t *managedTask) ReportCounters(counters map[string]TaskCounter) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.Counters = cloneTaskCounters(counters)
}

func (t *managedTask) ReportProgressInfo(progress string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.ProgressInfo = progress
}

func (t *managedTask) ReportLog(module string, level int, message string, fields map[string]any) {
	entry := make(map[string]any, len(fields)+4)
	for name, value := range fields {
		entry[name] = value
	}
	entry["ts"] = float64(time.Now().UnixNano()) / float64(time.Second)
	entry["mod"] = module
	entry["msg"] = message
	entry["level"] = level
	encoded, err := json.Marshal(entry)
	if err != nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if TaskStatus(t.Status).IsFinished() {
		return
	}
	t.LogLines = append(t.LogLines, encoded)
	if len(t.LogLines) > t.maxLogs {
		t.LogLines = append([]json.RawMessage(nil), t.LogLines[len(t.LogLines)-t.maxLogs:]...)
	}
}

func (t *managedTask) info() TaskInfo {
	t.mu.Lock()
	defer t.mu.Unlock()
	return cloneTaskInfo(t.TaskInfo)
}

func (t *managedTask) logs() []json.RawMessage {
	t.mu.Lock()
	defer t.mu.Unlock()
	return cloneTaskLogs(t.LogLines)
}

func cloneTaskCounters(source map[string]TaskCounter) map[string]TaskCounter {
	copyOfCounters := make(map[string]TaskCounter, len(source))
	for name, counter := range source {
		copyOfCounters[name] = counter
	}
	return copyOfCounters
}

func cloneTaskLogs(source []json.RawMessage) []json.RawMessage {
	copyOfLogs := make([]json.RawMessage, len(source))
	for index, line := range source {
		copyOfLogs[index] = append(json.RawMessage(nil), line...)
	}
	return copyOfLogs
}

func cloneTaskInfo(source TaskInfo) TaskInfo {
	result := source
	result.Counters = cloneTaskCounters(source.Counters)
	result.LogLines = cloneTaskLogs(source.LogLines)
	if source.EndTime != nil {
		endTime := *source.EndTime
		result.EndTime = &endTime
	}
	return result
}

func (s TaskStatus) IsFinished() bool {
	return s == TaskCanceled || s == TaskSuccess || s == TaskFailed
}
