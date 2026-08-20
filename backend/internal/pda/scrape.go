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














func decodePage(raw []byte) string {
	if utf8.Valid(raw) {
		return html.UnescapeString(string(raw))
	}
	utf8Bytes, _, err := transform.Bytes(traditionalchinese.Big5.NewDecoder(), raw)
	if err != nil {
		utf8Bytes = raw 
	}
	return html.UnescapeString(string(utf8Bytes))
}

var routeOptionRE = regexp.MustCompile(`<option value="([^"]*)">([^<]*)</option>`)




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
		dir := m[1] 
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
