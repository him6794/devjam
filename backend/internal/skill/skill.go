// Package skill wraps every externally-callable capability this backend
// has (pda5284 queries, station lookups, ...) behind one interface so it
// can be invoked two ways from the same implementation:
//
//   - directly and type-safely by Go code, via the concrete Do method each
//     skill exposes alongside this interface (used by internal/agent's
//     deterministic agents), and
//   - dynamically via JSON arguments, the way an LLM function-calling loop
//     would invoke it, via Invoke.
//
// Keeping both paths backed by one implementation means a deterministic
// agent and a future LLM-backed agent can never observe different data for
// the same call.
package skill

import (
	"context"
	"encoding/json"
)

type Skill interface {
	Name() string
	Description() string
	InputSchema() map[string]any
	Invoke(ctx context.Context, args json.RawMessage) (any, error)
}
