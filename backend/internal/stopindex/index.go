// Package stopindex answers "which bus station is the user standing at".
//
// pda5284 never publishes stop coordinates, so this package's File is built
// offline by cmd/indexer (which infers coordinates from live bus GPS, see
// builder.go) and loaded once at server startup. Station lookup itself is a
// pure, static computation deliberately kept off the request's network
// path — see plan.md §2.
package stopindex

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"time"
)

// Station is one aggregated bus stop location (a "slid" in pda5284 terms)
// with a coordinate inferred offline.
type Station struct {
	SlID int     `json:"slid"`
	Name string  `json:"name"`
	Lat  float64 `json:"lat"`
	Lon  float64 `json:"lon"`
}

// Stop struct adds RouteName to prefer the short, rider-facing code
// (e.g. "307") over the internal RouteID (e.g. "10443") when available.
type Stop struct {
	SID            int    `json:"sid"`
	SlID           int    `json:"slid"`
	RouteID        string `json:"route_id"`       // fallback
	RouteName      string `json:"route_name"`     // preferred (short code, e.g. "307")
	Direction      string `json:"direction"`       // "go" | "back"
	DirectionLabel string `json:"direction_label"` // e.g. "往台北車站"
	StopName       string `json:"stop_name"`
}

// File is the on-disk shape cmd/indexer writes and Load reads.
type File struct {
	GeneratedAt time.Time `json:"generated_at"`
	Stations    []Station `json:"stations"`
	Stops       []Stop    `json:"stops"`
}

// Index serves station-proximity and stop-metadata lookups from memory.
// City-scale stop counts stay in the low thousands, so a linear scan per
// request is simpler than a spatial index and still comfortably
// sub-millisecond.
type Index struct {
	stations         []Station
	metaBySID        map[int]Stop
	directionsBySlID map[int][]string // deduped DirectionLabel values serving each station
	routesBySlID     map[int][]Stop   // every Stop record (route+direction) serving each station, for RoutesAt
}

func Load(path string) (*Index, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("stopindex: read %s: %w", path, err)
	}
	var f File
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("stopindex: parse %s: %w", path, err)
	}
	return newIndex(f), nil
}

func newIndex(f File) *Index {
	idx := &Index{
		stations:         f.Stations,
		metaBySID:        make(map[int]Stop, len(f.Stops)),
		directionsBySlID: make(map[int][]string),
		routesBySlID:     make(map[int][]Stop),
	}
	seen := make(map[int]map[string]bool)
	seenRoute := make(map[int]map[string]bool) // slid -> routeName+direction, so a multi-stop route isn't listed twice
	for _, s := range f.Stops {
		idx.metaBySID[s.SID] = s
		if seenRoute[s.SlID] == nil {
			seenRoute[s.SlID] = map[string]bool{}
		}
		routeKey := s.RouteName + "|" + s.Direction
		if !seenRoute[s.SlID][routeKey] {
			seenRoute[s.SlID][routeKey] = true
			idx.routesBySlID[s.SlID] = append(idx.routesBySlID[s.SlID], s)
		}
		if s.DirectionLabel == "" {
			continue
		}
		if seen[s.SlID] == nil {
			seen[s.SlID] = map[string]bool{}
		}
		if !seen[s.SlID][s.DirectionLabel] {
			seen[s.SlID][s.DirectionLabel] = true
			idx.directionsBySlID[s.SlID] = append(idx.directionsBySlID[s.SlID], s.DirectionLabel)
		}
	}
	return idx
}

// Match is one candidate returned by Nearest, ordered nearest-first.
type Match struct {
	Station         Station  `json:"station"`
	Distance        float64  `json:"distance_m"`
	DirectionLabels []string `json:"direction_labels,omitempty"`
}

// Nearest returns every known station within radiusM of (lat, lon),
// nearest first. DirectionLabels lets a caller disambiguate two
// same-named, opposite-direction stops (a real case this project's own
// index turned up, e.g. two "華中橋" stops 18m apart) without a second
// lookup: pda5284 gives each direction its own destination label (e.g.
// "往台北車站" vs "往板橋"), scraped alongside the stop and stored per
// station in cmd/indexer.
func (idx *Index) Nearest(lat, lon, radiusM float64) []Match {
	var matches []Match
	for _, st := range idx.stations {
		d := haversineMeters(lat, lon, st.Lat, st.Lon)
		if d <= radiusM {
			matches = append(matches, Match{Station: st, Distance: d, DirectionLabels: idx.directionsBySlID[st.SlID]})
		}
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].Distance < matches[j].Distance })
	return matches
}

// LookupStop returns the route/direction metadata for one physical stop
// pole (sid), used to enrich a live StopLocationDyna row.
func (idx *Index) LookupStop(sid int) (Stop, bool) {
	s, ok := idx.metaBySID[sid]
	return s, ok
}

// RoutesAt returns every route+direction known to serve one station (slid),
// deduped. Used by the navigate skill to find a route connecting two
// stations without a live pda5284 call — this is static index data, same
// justification as Nearest (see package doc).
func (idx *Index) RoutesAt(slid int) []Stop {
	return idx.routesBySlID[slid]
}

// StationByID returns a station's static record (name + coordinate) by its
// slid, used by the navigate skill to resolve a destination station name
// back to a station it can compute a distance to.
func (idx *Index) StationByID(slid int) (Station, bool) {
	for _, st := range idx.stations {
		if st.SlID == slid {
			return st, true
		}
	}
	return Station{}, false
}

// FindStationByName does a substring search over station names (both
// directions, matching stationNameMatches' semantics in httpapi) — used to
// resolve a rider- or Gemini-stated destination name to a known station
// when the caller doesn't have a slid yet.
func (idx *Index) FindStationByName(name string) (Station, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Station{}, false
	}
	for _, st := range idx.stations {
		if strings.Contains(st.Name, name) || strings.Contains(name, st.Name) {
			return st, true
		}
	}
	return Station{}, false
}

func (idx *Index) Len() int { return len(idx.stations) }

// StationNames returns every known station's name, for the navigate skill
// to give Gemini a closed candidate list when resolving a free-text
// destination (grounding it against real stations instead of letting it
// invent a plausible-sounding name that isn't actually indexed).
func (idx *Index) StationNames() []string {
	names := make([]string, len(idx.stations))
	for i, st := range idx.stations {
		names[i] = st.Name
	}
	return names
}

const earthRadiusM = 6371000

func haversineMeters(lat1, lon1, lat2, lon2 float64) float64 {
	toRad := func(deg float64) float64 { return deg * math.Pi / 180 }
	dLat := toRad(lat2 - lat1)
	dLon := toRad(lon2 - lon1)
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(toRad(lat1))*math.Cos(toRad(lat2))*math.Sin(dLon/2)*math.Sin(dLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return earthRadiusM * c
}
