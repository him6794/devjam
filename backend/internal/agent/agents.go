package agent

import (
	"context"

	"devjam-backend/internal/profile"
	"devjam-backend/internal/skill"
)

const defaultSearchRadiusM = 150

// GeoAgent turns a raw GPS fix into every known station within radius. It
// is pure, in-memory computation (via the nearest_stops skill), so it
// costs nothing to run on every request. It deliberately does not pick a
// single "the" station — see Blackboard.Candidates — because with an
// opposite-direction stop pair, distance alone can't tell them apart;
// that needs VisionAgent's output too, and the two must stay independent
// to run in the same wave.
type GeoAgent struct {
	nearestStops *skill.NearestStops
}

func NewGeoAgent(s *skill.NearestStops) *GeoAgent { return &GeoAgent{nearestStops: s} }

func (a *GeoAgent) Name() string { return "geo" }

func (a *GeoAgent) Run(ctx context.Context, bb Blackboard) Contribution {
	out, err := a.nearestStops.Do(ctx, skill.NearestStopsIn{Lat: bb.Lat, Lon: bb.Lon, RadiusM: defaultSearchRadiusM})
	if err != nil {
		return Contribution{Agent: a.Name(), Degraded: true, Err: err}
	}
	candidates := make([]Candidate, len(out.Matches))
	for i, m := range out.Matches {
		candidates[i] = Candidate{SlID: m.Station.SlID, Name: m.Station.Name, Distance: m.Distance, DirectionLabels: m.DirectionLabels}
	}
	return Contribution{
		Agent: a.Name(),
		Apply: func(b *Blackboard) { b.Candidates = candidates },
	}
}

// VisionAgent reads a bus-stop sign photo, when one was submitted, via
// Gemini. Its output is only ever used to pick among GeoAgent's
// candidates (see httpapi.pickStation) — it never introduces a station
// GeoAgent didn't already find within radius, so a misread photo can
// change *which* nearby stop gets picked but can't make the system report
// a stop that isn't actually there.
type VisionAgent struct {
	visionSign *skill.VisionReadSign
}

func NewVisionAgent(s *skill.VisionReadSign) *VisionAgent { return &VisionAgent{visionSign: s} }

func (a *VisionAgent) Name() string { return "vision" }

func (a *VisionAgent) Run(ctx context.Context, bb Blackboard) Contribution {
	if len(bb.ImageData) == 0 {
		return Contribution{Agent: a.Name()}
	}
	out, err := a.visionSign.Do(ctx, skill.VisionReadSignIn{ImageData: bb.ImageData, MIMEType: bb.ImageMIME})
	if err != nil {
		return Contribution{Agent: a.Name(), Degraded: true, Err: err}
	}
	return Contribution{
		Agent: a.Name(),
		Apply: func(b *Blackboard) {
			b.VisionFound = out.Found
			b.VisionStopName = out.StopName
			b.VisionDestinationText = out.DestinationText
		},
	}
}

// ProfileAgent loads the caller's accessibility preferences. It runs
// concurrently with GeoAgent: the two are fully independent (one reads the
// profile store, the other does GPS math), so there is no reason to make
// the request wait on them sequentially.
type ProfileAgent struct {
	store  *profile.Store
	userID string
}

func NewProfileAgent(store *profile.Store, userID string) *ProfileAgent {
	return &ProfileAgent{store: store, userID: userID}
}

func (a *ProfileAgent) Name() string { return "profile" }

func (a *ProfileAgent) Run(ctx context.Context, bb Blackboard) Contribution {
	p, exists := a.store.Get(a.userID)
	if !exists {
		p.FontScale = 1.0
	}
	return Contribution{
		Agent: a.Name(),
		Apply: func(b *Blackboard) {
			b.Profile = ProfileView{
				Exists:            exists,
				ImpairmentType:    p.ImpairmentType,
				ReactionBufferMin: p.ReactionBufferMinutes(),
				FontScale:         p.FontScale,
				SafeZone:          p.SafeZone,
			}
		},
	}
}

// TransitAgent fetches live ETAs for the station Geo locked onto. It only
// does anything once NearestFound is true, so it belongs in the wave
// after Geo, not alongside it.
type TransitAgent struct {
	stopETA *skill.StopETA
}

func NewTransitAgent(s *skill.StopETA) *TransitAgent { return &TransitAgent{stopETA: s} }

func (a *TransitAgent) Name() string { return "transit" }

func (a *TransitAgent) Run(ctx context.Context, bb Blackboard) Contribution {
	if !bb.NearestFound {
		return Contribution{Agent: a.Name()}
	}
	out, err := a.stopETA.Do(ctx, skill.StopETAIn{SlID: bb.NearestSlID})
	if err != nil {
		return Contribution{Agent: a.Name(), Degraded: true, Err: err}
	}
	buses := make([]BusReport, len(out.Buses))
	for i, b := range out.Buses {
		buses[i] = BusReport{Route: b.Route, Direction: b.Direction, ETAMinutes: b.ETAMinutes, HasETA: b.HasETA}
	}
	return Contribution{
		Agent: a.Name(),
		Apply: func(bb *Blackboard) { bb.Buses = buses },
	}
}
