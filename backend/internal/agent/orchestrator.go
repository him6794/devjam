package agent

import (
	"context"
	"log"
	"sync"
	"time"
)





type Orchestrator struct {
	agentTimeout time.Duration
}

func NewOrchestrator() *Orchestrator {
	
	
	
	return &Orchestrator{agentTimeout: 8 * time.Second}
}










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
