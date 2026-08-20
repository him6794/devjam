














package main

import (
	"context"
	"flag"
	"log"
	"strings"
	"time"

	"devjam-backend/internal/pda"
	"devjam-backend/internal/stopindex"
)






const defaultRoutes = "16111,15111,10841,11811,10873,10417" 

func main() {
	routesFlag := flag.String("routes", defaultRoutes, "comma-separated pda5284 route ids to index")
	out := flag.String("out", "data/stops.json", "output path for the station/stop index")
	pollWindow := flag.Duration("poll-window", 90*time.Second, "how long to poll live positions for stationary-bus coordinate samples")
	pollInterval := flag.Duration("poll-interval", 20*time.Second, "delay between position polls within the window")
	flag.Parse()

	ids := splitNonEmpty(*routesFlag)
	client := pda.NewClient()
	builder := stopindex.NewBuilder()
	ctx := context.Background()

	log.Printf("indexer: fetching route list for display names")
	allRoutes, err := client.RouteList(ctx)
	if err != nil {
		log.Fatalf("indexer: routelist.jsp: %v", err)
	}
	nameByRID := make(map[string]string, len(allRoutes))
	for _, r := range allRoutes {
		nameByRID[r.ID] = r.Name
	}

	sidToSlid := map[int]int{}

	for _, rid := range ids {
		log.Printf("indexer: route %s (%s): fetching stop list", rid, nameByRID[rid])
		detail, err := client.RouteDetail(ctx, rid)
		if err != nil {
			log.Printf("indexer: route %s: %v (skipped)", rid, err)
			continue
		}

		routeName := nameByRID[rid]
		if routeName == "" {
			routeName = detail.Title
		}

		resolved := 0
		for _, ref := range detail.Stops {
			slid, ok := sidToSlid[ref.SID]
			if !ok {
				slid, err = client.ResolveStopLocation(ctx, ref.SID)
				if err != nil {
					log.Printf("indexer: sid %d: %v (skipped)", ref.SID, err)
					continue
				}
				sidToSlid[ref.SID] = slid
			}
			builder.AddStop(stopindex.Stop{
				SID:            ref.SID,
				SlID:           slid,
				RouteID:        rid,
				RouteName:      routeName,
				Direction:      ref.Direction,
				DirectionLabel: ref.DirectionLabel,
				StopName:       ref.Name,
			})
			resolved++
		}
		log.Printf("indexer: route %s: %d/%d stops resolved to stations", rid, resolved, len(detail.Stops))
	}

	log.Printf("indexer: sampling live bus positions for %v to infer station coordinates", *pollWindow)
	deadline := time.Now().Add(*pollWindow)
	for round := 1; ; round++ {
		for _, rid := range ids {
			dyna, err := client.RouteDyna(ctx, rid)
			if err != nil {
				log.Printf("indexer: RouteDyna %s: %v", rid, err)
				continue
			}
			samples := 0
			for _, bus := range dyna.Buses {
				if bus.SpeedKmh >= 5 || bus.CurrentStopID == 0 {
					continue 
				}
				if slid, ok := sidToSlid[bus.CurrentStopID]; ok {
					builder.AddStationSample(slid, bus.Lat, bus.Lon)
					samples++
				}
			}
			log.Printf("indexer: round %d route %s: %d stationary samples", round, rid, samples)
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(*pollInterval)
	}

	file := builder.Build()
	if err := stopindex.SaveFile(*out, file); err != nil {
		log.Fatalf("indexer: save %s: %v", *out, err)
	}
	log.Printf("indexer: wrote %s — %d stations with coordinates, %d stop records", *out, len(file.Stations), len(file.Stops))
}

func splitNonEmpty(csv string) []string {
	var out []string
	for _, s := range strings.Split(csv, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}
