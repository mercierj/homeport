package deploy

type EventType string

const (
	EventPhase    EventType = "phase"
	EventProgress EventType = "progress"
	EventLog      EventType = "log"
	EventComplete EventType = "complete"
	EventError    EventType = "error"
)

type PhaseEvent struct {
	Phase string `json:"phase"`
	Index int    `json:"index"`
	Total int    `json:"total"`
}

type ProgressEvent struct {
	Percent int `json:"percent"`
}

type LogEvent struct {
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"`
	Message   string `json:"message"`
}

type CompleteEvent struct {
	Services []ServiceStatus `json:"services"`
}

type ServiceStatus struct {
	Name    string   `json:"name"`
	Healthy bool     `json:"healthy"`
	Ports   []string `json:"ports"`
}

type ErrorEvent struct {
	Message     string `json:"message"`
	Phase       string `json:"phase"`
	Recoverable bool   `json:"recoverable"`
}

type Event struct {
	Type EventType   `json:"type"`
	Data interface{} `json:"data"`
}
