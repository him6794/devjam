package stopindex

import (
	"encoding/json"
	"os"
	"sort"
	"time"
)

type latlon struct{ lat, lon float64 }

// Builder accumulates route/stop metadata and coordinate samples gathered
// during one cmd/indexer run, then reduces them into a File.
//
// Coordinate samples are reduced with a median rather than kept raw, so a
// single stray GPS fix cannot skew a station's inferred position. Builder
// does not persist samples between runs — see cmd/indexer's doc comment
// for why that is an acceptable simplification for a hackathon-scale
// index.
type Builder struct {
	stops          map[int]Stop // keyed by sid, last write wins (stable across route re-scrapes)
	stationNames   map[int]string
	stationSamples map[int][]latlon
}

func NewBuilder() *Builder {
	return &Builder{
		stops:          map[int]Stop{},
		stationNames:   map[int]string{},
		stationSamples: map[int][]latlon{},
	}
}

// AddStop records one route's stop-pole metadata, discovered while
// scraping route.jsp.
func (b *Builder) AddStop(s Stop) {
	b.stops[s.SID] = s
	if s.StopName != "" {
		if _, exists := b.stationNames[s.SlID]; !exists {
			b.stationNames[s.SlID] = s.StopName
		}
	}
}

// AddStationSample records one inferred coordinate for a station (slid),
// e.g. from a bus observed stationary near that stop.
func (b *Builder) AddStationSample(slid int, lat, lon float64) {
	b.stationSamples[slid] = append(b.stationSamples[slid], latlon{lat, lon})
}

// Build reduces accumulated data into a File. Stations that never received
// a coordinate sample are omitted rather than written with a bogus (0,0)
// location — such stops remain resolvable via LookupStop once a future run
// samples them, they just won't be reachable by GPS proximity yet.
func (b *Builder) Build() File {
	f := File{GeneratedAt: time.Now().UTC()}
	for _, s := range b.stops {
		f.Stops = append(f.Stops, s)
	}
	for slid, pts := range b.stationSamples {
		if len(pts) == 0 {
			continue
		}
		f.Stations = append(f.Stations, Station{
			SlID: slid,
			Name: b.stationNames[slid],
			Lat:  medianOf(pts, func(p latlon) float64 { return p.lat }),
			Lon:  medianOf(pts, func(p latlon) float64 { return p.lon }),
		})
	}
	return f
}

func medianOf(pts []latlon, get func(latlon) float64) float64 {
	vals := make([]float64, len(pts))
	for i, p := range pts {
		vals[i] = get(p)
	}
	sort.Float64s(vals)
	mid := len(vals) / 2
	if len(vals)%2 == 1 {
		return vals[mid]
	}
	return (vals[mid-1] + vals[mid]) / 2
}

func SaveFile(path string, f File) error {
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}
