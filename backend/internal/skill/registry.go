package skill

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// Registry is the lookup table an LLM tool-calling loop (or, today, the
// GET /api/skills introspection endpoint) uses to discover and invoke
// skills by name.
type Registry struct {
	mu     sync.RWMutex
	skills map[string]Skill
}

func NewRegistry() *Registry {
	return &Registry{skills: map[string]Skill{}}
}

func (r *Registry) Register(s Skill) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.skills[s.Name()] = s
}

func (r *Registry) Get(name string) (Skill, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.skills[name]
	return s, ok
}

// Schemas returns every registered skill's function-calling schema, ready
// to hand to an LLM tool-use API.
func (r *Registry) Schemas() []map[string]any {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]map[string]any, 0, len(r.skills))
	for _, s := range r.skills {
		out = append(out, map[string]any{
			"name":        s.Name(),
			"description": s.Description(),
			"parameters":  s.InputSchema(),
		})
	}
	return out
}

// Call is one skill invocation request, e.g. as decoded from an LLM's
// function-calling response.
type Call struct {
	Name string
	Args json.RawMessage
}

// Result pairs a Call's outcome back up by name. Err is populated instead
// of aborting the batch: a failed tool call is normal input for an agent
// loop (the caller can inspect the error and retry with different
// arguments), not a reason to cancel every other in-flight call.
type Result struct {
	Name  string
	Value any
	Err   error
}

// InvokeParallel runs every call concurrently, each under its own timeout,
// and returns once all have finished or ctx is cancelled.
func (r *Registry) InvokeParallel(ctx context.Context, calls []Call, timeout time.Duration) []Result {
	results := make([]Result, len(calls))
	var wg sync.WaitGroup
	for i, c := range calls {
		wg.Add(1)
		go func(i int, c Call) {
			defer wg.Done()
			s, ok := r.Get(c.Name)
			if !ok {
				results[i] = Result{Name: c.Name, Err: fmt.Errorf("skill: unknown skill %q", c.Name)}
				return
			}
			cctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			v, err := s.Invoke(cctx, c.Args)
			results[i] = Result{Name: c.Name, Value: v, Err: err}
		}(i, c)
	}
	wg.Wait()
	return results
}
