package runner

// Codex JSONL event types.
// codex exec --json emits newline-delimited JSON events to stdout.

// EventType identifies the kind of JSONL event from codex.
type EventType string

const (
	EventThreadStarted EventType = "thread.started"
	EventItemStarted   EventType = "item.started"
	EventItemCompleted EventType = "item.completed"
	EventTurnCompleted EventType = "turn.completed"
	EventTurnFailed    EventType = "turn.failed"
)

// Event is the top-level JSONL structure emitted by codex exec --json.
type Event struct {
	Type EventType `json:"type"`
	Item *Item     `json:"item,omitempty"`
}

// Item represents a completed item within a codex turn.
type Item struct {
	Type    string `json:"type"` // "reasoning", "command_execution", "agent_message"
	Content string `json:"content,omitempty"`
	Status  string `json:"status,omitempty"` // for command_execution: "success", "failure"
	Command string `json:"command,omitempty"`
	Output  string `json:"output,omitempty"`
}
