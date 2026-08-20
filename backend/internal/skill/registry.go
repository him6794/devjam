package skill

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)




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



type Call struct {
	Name string
	Args json.RawMessage
}





type Result struct {
	Name  string
	Value any
	Err   error
}



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
