package subagent

import "sync"

// taskSet manages a set of task IDs for a session
type taskSet struct {
	mu      sync.Mutex
	taskIDs []string
}

func (ts *taskSet) add(taskID string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.taskIDs = append(ts.taskIDs, taskID)
}

func (ts *taskSet) snapshot() []string {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	// Copy to avoid holding lock during iteration
	snapshot := make([]string, len(ts.taskIDs))
	copy(snapshot, ts.taskIDs)
	return snapshot
}

func (ts *taskSet) remove(taskID string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	for i, id := range ts.taskIDs {
		if id == taskID {
			ts.taskIDs = append(ts.taskIDs[:i], ts.taskIDs[i+1:]...)
			return
		}
	}
}
