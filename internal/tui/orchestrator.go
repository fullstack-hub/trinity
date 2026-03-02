package tui

import (
	"fmt"
	"strings"

	"github.com/fullstack-hub/trinity/internal/client"
)

// TaskState represents the state of an orchestration sub-task.
type TaskState int

const (
	TaskPending TaskState = iota
	TaskRunning
	TaskDone
	TaskError
)

// SubTask is one unit of work in an orchestration plan.
type SubTask struct {
	ID          string    `json:"id"`
	Agent       string    `json:"agent"`      // "claude", "gemini", "copilot"
	Model       string    `json:"model"`
	Description string    `json:"description"`
	Prompt      string    `json:"prompt"`
	DependsOn   []string  `json:"depends_on"`
	State       TaskState `json:"-"`
	Result      string    `json:"-"`
	Thinking    string    `json:"-"`
	ActualModel string    `json:"-"` // model name returned by server
}

// SynthesisConfig describes how to combine sub-task results.
type SynthesisConfig struct {
	Agent  string `json:"agent"`
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

// TaskPlan is the top-level orchestration plan returned by the Agent.
type TaskPlan struct {
	Tasks     []*SubTask      `json:"tasks"`
	Synthesis SynthesisConfig `json:"synthesis"`
}

// orchPhase tracks the current orchestration state.
type orchPhase int

const (
	orchIdle orchPhase = iota
	orchRunning
	orchSynthesizing
)

// orchEventMsg carries an SSE event from an orchestration sub-task goroutine.
type orchEventMsg struct {
	taskID string
	event  client.SSEEvent
}

// ReadyTasks returns sub-tasks whose dependencies are all done.
func ReadyTasks(plan *TaskPlan) []*SubTask {
	var ready []*SubTask
	for _, t := range plan.Tasks {
		if t.State != TaskPending {
			continue
		}
		allDone := true
		for _, dep := range t.DependsOn {
			for _, d := range plan.Tasks {
				if d.ID == dep && d.State != TaskDone {
					allDone = false
				}
			}
		}
		if allDone {
			ready = append(ready, t)
		}
	}
	return ready
}

// AllDone returns true if every sub-task is done (or errored).
func AllDone(plan *TaskPlan) bool {
	for _, t := range plan.Tasks {
		if t.State != TaskDone && t.State != TaskError {
			return false
		}
	}
	return true
}

// countDone returns the number of completed tasks.
func countDone(plan *TaskPlan) int {
	n := 0
	for _, t := range plan.Tasks {
		if t.State == TaskDone {
			n++
		}
	}
	return n
}

// findTask looks up a sub-task by ID.
func findTask(plan *TaskPlan, id string) *SubTask {
	for _, t := range plan.Tasks {
		if t.ID == id {
			return t
		}
	}
	return nil
}

// BuildSynthesisPrompt combines all sub-task results into a synthesis prompt.
func BuildSynthesisPrompt(plan *TaskPlan, originalMsg string) string {
	var sb strings.Builder
	sb.WriteString("Here are the results from parallel analysis:\n\n")
	for _, t := range plan.Tasks {
		sb.WriteString(fmt.Sprintf("## %s (%s)\n%s\n\n", t.Description, t.Model, t.Result))
	}
	sb.WriteString("Based on these findings, provide a comprehensive answer to:\n")
	sb.WriteString(originalMsg)
	if plan.Synthesis.Prompt != "" {
		sb.WriteString("\n\nAdditional instructions: " + plan.Synthesis.Prompt)
	}
	return sb.String()
}
