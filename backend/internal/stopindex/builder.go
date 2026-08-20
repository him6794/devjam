package stopindex

import (
	"encoding/json"
	"os"
	"sort"
	"time"
)

type latlon struct{ lat, lon float64 }









type Builder struct {
	stops          map[int]Stop 
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



func (b *Builder) AddStop(s Stop) {
	b.stops[s.SID] = s
	if s.StopName != "" {
		if _, exists := b.stationNames[s.SlID]; !exists {
			b.stationNames[s.SlID] = s.StopName
		}
	}
}



func (b *Builder) AddStationSample(slid int, lat, lon float64) {
	b.stationSamples[slid] = append(b.stationSamples[slid], latlon{lat, lon})
}





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
