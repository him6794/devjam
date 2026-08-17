package agent

import (
	"context"
	"log"
	"sync"
	"time"
)

// Orchestrator runs waves of agents. Each wave is a set of agents that are
// safe to execute concurrently because none of them depends on another's
// output; the caller decides wave boundaries (e.g. Transit must wait for
// Geo to pick a station, so it runs in its own, later wave).
type Orchestrator struct {
	agentTimeout time.Duration
}

func NewOrchestrator() *Orchestrator {
	// 8s covers VisionAgent's Gemini call comfortably; Geo/Profile/Transit
	// finish in low milliseconds regardless, so a larger ceiling doesn't
	// slow down requests that never hit it.
	return &Orchestrator{agentTimeout: 8 * time.Second}
}

// RunWave runs every agent concurrently against a snapshot of bb, waits
// for all of them, then applies their contributions in order.
//
// A single agent failing does not cancel its siblings: e.g. if the transit
// lookup fails, the profile lookup (already running concurrently) should
// still be allowed to finish so the response can degrade gracefully
// instead of losing the whole request. This is the opposite of the
// fail-fast default an errgroup gives you, and is intentional here — a
// tool/agent error is normal input to this layer, not a reason to abort.
func (o *Orchestrator) RunWave(ctx context.Context, bb *Blackboard, agents ...Agent) []Contribution {
	contribs := make([]Contribution, len(agents))
	var wg sync.WaitGroup
	snapshot := *bb
	for i, a := range agents {
		wg.Add(1)
		go func(i int, a Agent) {
			defer wg.Done()
			actx, cancel := context.WithTimeout(ctx, o.agentTimeout)
			defer cancel()
			c := a.Run(actx, snapshot)
			if c.Err != nil {
				log.Printf("agent %s degraded: %v", a.Name(), c.Err)
			}
			contribs[i] = c
		}(i, a)
	}
	wg.Wait()

	for _, c := range contribs {
		if c.Apply != nil {
			c.Apply(bb)
		}
	}
	return contribs
}
