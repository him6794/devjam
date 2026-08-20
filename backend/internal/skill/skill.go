












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
