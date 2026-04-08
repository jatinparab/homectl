package common

import "encoding/json"

type LinkMessage struct {
	Type      string          `json:"type"`
	ID        string          `json:"id,omitempty"`
	Source    string          `json:"source,omitempty"`
	AgentName string          `json:"agent_name,omitempty"`
	AgentIP   string          `json:"agent_ip,omitempty"`
	Method    string          `json:"method,omitempty"`
	Path      string          `json:"path,omitempty"`
	Query     string          `json:"query,omitempty"`
	Status    int             `json:"status,omitempty"`
	Body      []byte          `json:"body,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	Error     string          `json:"error,omitempty"`
	Done      bool            `json:"done,omitempty"`
	Stream    bool            `json:"stream,omitempty"`
}

const (
	LinkTypeHello    = "hello"
	LinkTypeRequest  = "request"
	LinkTypeResponse = "response"
)
