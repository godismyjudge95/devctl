package dumps

import (
	"encoding/json"
	"fmt"
)

// Dump is the decoded PHP dump payload.
type Dump struct {
	Timestamp float64    `json:"timestamp"`
	Source    DumpSource `json:"source"`
	Host      string     `json:"host"`
	Nodes     []Node     `json:"nodes"`
}

// DumpSource is the file/line context for a dump.
type DumpSource struct {
	File string `json:"file"`
	Line int    `json:"line"`
	Name string `json:"name"`
}

// Node is a recursive dump node (used for JSON serialisation only in this file).
// The actual schema is open so we use json.RawMessage internally.
type Node = json.RawMessage

// marshalNodes serialises a slice of nodes to a JSON string.
func marshalNodes(nodes []Node) (string, error) {
	b, err := json.Marshal(nodes)
	if err != nil {
		return "", fmt.Errorf("marshal nodes: %w", err)
	}
	return string(b), nil
}
