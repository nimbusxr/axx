package core

import (
	"context"
	"encoding/json"
)

// Tool is something a pack lets coding agents do, as a tool of `axx mcp`.
// An agent's session with the server has one scenario that stays open: the
// steps the agent tries run in it (the steps_try tool), and the packs' tools
// look at what they did, such as the page a step opened.
type Tool struct {
	// Name is the tool's name, the pack's first: "web_page".
	Name string `json:"name"`
	// Description says what the tool does and when to use it, for agents.
	Description string `json:"description"`
	// Input is the JSON Schema of the tool's input, an object.
	Input json.RawMessage `json:"input"`
	// ReadOnly says the tool changes nothing, in the session or elsewhere.
	ReadOnly bool                                      `json:"readOnly,omitempty"`
	Run      func(call *ToolCall) (*ToolResult, error) `json:"-"`
}

// ToolCall is a call of a pack's tool.
type ToolCall struct {
	Context context.Context
	// Scenario is the session's scenario.
	Scenario *Scenario
	// Input is the tool's input, valid against its schema.
	Input json.RawMessage
}

// ToolResult is what a tool gives the agent.
type ToolResult struct {
	// Data is the result, as JSON.
	Data any
	// Images go with it, such as a screenshot.
	Images []Image
}

// Image is an image a tool gives the agent.
type Image struct {
	MediaType string
	Data      []byte
}
