// Package agent runs multiple independent workers concurrently against a
// shared Blackboard and merges their results — the "multiple agents do
// different things in parallel" layer plan.md §3 describes. Deterministic
// agents (Geo, Profile, Transit) and LLM-backed agents (Vision, and
// future ones like Journey/Narrator) implement the same interface, so the
// orchestrator can run either kind side by side in a wave.
package agent

import (
	"context"

	"devjam-backend/internal/profile"
)

// Blackboard is the shared state every agent in one /api/analyze request
// reads from and contributes to. Agents in wave N+1 only ever see wave N's
// already-merged output; there is no direct agent-to-agent call.
type Blackboard struct {
	Lat, Lon  float64
	ImageData []byte
	ImageMIME string

	// Candidates is GeoAgent's wave-1 output: every known station within
	// radius, nearest first. Deciding which one to lock onto happens
	// after wave 1 (see httpapi.pickStation), since that decision needs
	// both this and VisionAgent's output — neither agent depends on the
	// other, so both can run in the same wave.
	Candidates []Candidate

	VisionFound           bool
	VisionStopName        string
	VisionDestinationText string

	NearestFound bool
	NearestSlID  int
	NearestName  string
	NearestDistM float64

	Profile ProfileView

	Buses []BusReport
}

// Candidate is one station GeoAgent found within search radius.
type Candidate struct {
	SlID            int
	Name            string
	Distance        float64
	DirectionLabels []string // e.g. ["往台北車站"] — see stopindex.Index.Nearest
}

// ProfileView is the subset of a user's accessibility profile the agent
// layer needs, populated by ProfileAgent.
type ProfileView struct {
	Exists            bool
	ImpairmentType    string
	ReactionBufferMin int
	FontScale         float64
	SafeZone          profile.SafeZone
}

// BusReport is one route's live status at the locked station, already
// merged with route/direction metadata.
type BusReport struct {
	Route      string
	Direction  string
	ETAMinutes int
	HasETA     bool
}

// Contribution is what one agent hands back after a wave. Apply is nil
// when the agent had nothing to merge (e.g. Transit before a station is
// locked); Degraded/Err record a failure the orchestrator should log but
// not let cancel the rest of the wave.
type Contribution struct {
	Agent    string
	Apply    func(*Blackboard)
	Degraded bool
	Err      error
}

// Agent is one node in the analyze DAG.
type Agent interface {
	Name() string
	Run(ctx context.Context, bb Blackboard) Contribution
}
