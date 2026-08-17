package pda

import (
	"context"
	"fmt"
	"html"
	"regexp"
	"strconv"
	"unicode/utf8"

	"golang.org/x/text/encoding/traditionalchinese"
	"golang.org/x/text/transform"
)

// decodePage converts a pda5284 HTML response into clean UTF-8 text.
//
// Every dynamically inserted value (stop names, route names, destinations)
// is HTML numeric-entity escaped ASCII, decoded the same way regardless of
// page encoding. Static template text (labels like "去程"/"返程"), however,
// is inconsistent ACROSS routes: some route.jsp responses were observed
// serving it as raw Big5, others as raw UTF-8 (e.g. rid=10417 vs
// rid=16111, both fetched 2026-08-17) — a quirk of whatever legacy system
// generates these pages, not something this project controls. Raw Big5
// byte sequences essentially never happen to also be valid UTF-8, so
// utf8.Valid reliably tells the two cases apart; decoding valid UTF-8 as
// Big5 anyway corrupts exactly the destination labels riders would hear
// read aloud.
func decodePage(raw []byte) string {
	if utf8.Valid(raw) {
		return html.UnescapeString(string(raw))
	}
	utf8Bytes, _, err := transform.Bytes(traditionalchinese.Big5.NewDecoder(), raw)
	if err != nil {
		utf8Bytes = raw // best-effort fallback
	}
	return html.UnescapeString(string(utf8Bytes))
}

var routeOptionRE = regexp.MustCompile(`<option value="([^"]*)">([^<]*)</option>`)

// RouteList fetches every route pda5284 knows about. The autocomplete box
// on routelist.jsp embeds the full list up front as <option> elements and
// filters them client-side in JS, so one request returns everything.
func (c *Client) RouteList(ctx context.Context) ([]Route, error) {
	body, err := c.get(ctx, "routelist.jsp")
	if err != nil {
		return nil, err
	}
	text := decodePage(body)

	var routes []Route
	for _, m := range routeOptionRE.FindAllStringSubmatch(text, -1) {
		id, name := m[1], m[2]
		if id == "" || name == "" {
			continue
		}
		routes = append(routes, Route{ID: id, Name: name})
	}
	return routes, nil
}

var (
	routeTitleRE = regexp.MustCompile(`routeinfo\.jsp\?rid=[^"]*">([^<]*)</a>`)
	goTitleRE    = regexp.MustCompile(`class="ttegotitle">[^(]*\(([^)]*)\)`)
	backTitleRE  = regexp.MustCompile(`class="ttebacktitle">[^(]*\(([^)]*)\)`)
	stopRowRE    = regexp.MustCompile(`<tr class="tte(go|back)[12]"><td><a href="stop\.jsp\?sid=(\d+)">([^<]*)</a>`)
)

// RouteDetail fetches and parses route.jsp for one route: its display
// title, each direction's destination label, and the ordered stop list per
// direction.
func (c *Client) RouteDetail(ctx context.Context, routeID string) (*RouteDetail, error) {
	body, err := c.get(ctx, "route.jsp?rid="+routeID)
	if err != nil {
		return nil, err
	}
	text := decodePage(body)

	detail := &RouteDetail{RouteID: routeID}
	if m := routeTitleRE.FindStringSubmatch(text); m != nil {
		detail.Title = m[1]
	}
	if m := goTitleRE.FindStringSubmatch(text); m != nil {
		detail.GoLabel = m[1]
	}
	if m := backTitleRE.FindStringSubmatch(text); m != nil {
		detail.BackLabel = m[1]
	}

	seq := map[string]int{}
	for _, m := range stopRowRE.FindAllStringSubmatch(text, -1) {
		dir := m[1] // "go" or "back"
		sid, err := strconv.Atoi(m[2])
		if err != nil {
			continue
		}
		seq[dir]++
		label := detail.GoLabel
		if dir == "back" {
			label = detail.BackLabel
		}
		detail.Stops = append(detail.Stops, StopRef{
			SID:            sid,
			RouteID:        routeID,
			Direction:      dir,
			DirectionLabel: label,
			Name:           m[3],
			Seq:            seq[dir],
		})
	}
	if len(detail.Stops) == 0 {
		return nil, fmt.Errorf("pda: route %s: no stops parsed (page layout may have changed)", routeID)
	}
	return detail, nil
}

var slidRE = regexp.MustCompile(`stoplocationid=(\d+)`)

// ResolveStopLocation follows the redirect stop.jsp?sid=N issues to find
// the aggregated station id (slid) a physical stop pole belongs to. The
// redirect target page (stoplocation.jsp) embeds its own slid in an inline
// script call, which is what the regex below recovers.
func (c *Client) ResolveStopLocation(ctx context.Context, sid int) (int, error) {
	body, err := c.get(ctx, fmt.Sprintf("stop.jsp?sid=%d", sid))
	if err != nil {
		return 0, err
	}
	m := slidRE.FindStringSubmatch(string(body))
	if m == nil {
		return 0, fmt.Errorf("pda: stop %d: could not resolve stoplocationid", sid)
	}
	slid, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, err
	}
	return slid, nil
}
