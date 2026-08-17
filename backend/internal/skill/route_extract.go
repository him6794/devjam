package skill

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"

	"google.golang.org/genai"
)

type RouteExtractIn struct {
	Text string `json:"text"`
}

type RouteExtractOut struct {
	Found bool   `json:"found"`
	Route string `json:"route"`
}

// routeExtractPrompt asks Gemini for the one route code a rider said.
// It must be told to strip filler words ("路", "號", "公車") because the
// transcripts it gets are raw STT output like "我要搭307路" or "幫我查三零七",
// and the wanted-route match against stopindex route names is exact.
const routeExtractPrompt = `使用者正在說出想搭乘的公車路線編號，這是語音辨識轉錄的文字。
請提取路線編號本身：
1. route：路線編號，例如「307」或「棕7」，只保留編號，去掉「路」「號」「公車」等字；
   若語音提到的是中文數字（如「三零七」），請轉成阿拉伯數字「307」
2. found：若文字中沒有任何路線編號，設為 false，route 留空字串`

var routeExtractSchema = &genai.Schema{
	Type: genai.TypeObject,
	Properties: map[string]*genai.Schema{
		"found": {Type: genai.TypeBoolean},
		"route": {Type: genai.TypeString},
	},
	Required: []string{"found", "route"},
}

// routeDigitsRe is the deterministic fallback when Gemini isn't configured
// (or its call fails): the first run of digits in a transcript like
// "我要搭307" or "307" is the route number for the numeric routes v1
// targets.
var routeDigitsRe = regexp.MustCompile(`\d+`)

// RouteExtract resolves what route a rider wants from a speech transcript —
// the second half of Journey Agent v1 (STT turns audio into text; this
// turns text into a route code). Gemini handles spoken Chinese numerals
// ("三零七" → "307") and alphanumeric routes; the regex fallback keeps the
// numeric-route path working with no Vertex AI credentials at all.
type RouteExtract struct {
	client *genai.Client
	model  string
}

func NewRouteExtract(client *genai.Client) *RouteExtract {
	return &RouteExtract{client: client, model: "gemini-2.5-flash"}
}

func (s *RouteExtract) Name() string { return "route_extract" }

func (s *RouteExtract) Description() string {
	return "Extract the bus route number a rider wants from a speech transcript."
}

func (s *RouteExtract) InputSchema() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{"text": map[string]any{"type": "string"}},
		"required":   []string{"text"},
	}
}

func (s *RouteExtract) Do(ctx context.Context, in RouteExtractIn) (RouteExtractOut, error) {
	if in.Text == "" {
		return RouteExtractOut{}, errors.New("route_extract: empty text")
	}

	if s.client != nil {
		resp, err := s.client.Models.GenerateContent(ctx, s.model,
			[]*genai.Content{{
				Role: "user",
				Parts: []*genai.Part{
					{Text: routeExtractPrompt + "\n\n語音轉錄：「" + in.Text + "」"},
				},
			}},
			&genai.GenerateContentConfig{
				ResponseMIMEType: "application/json",
				ResponseSchema:   routeExtractSchema,
			},
		)
		if err == nil {
			var out RouteExtractOut
			if unmarshalErr := json.Unmarshal([]byte(resp.Text()), &out); unmarshalErr == nil && out.Found && out.Route != "" {
				return out, nil
			}
			// Any Gemini hiccup (network, bad schema, found=false on a
			// digit transcript) falls through to the regex — the caller
			// should still get a route when one is plainly in the text.
		}
	}

	if m := routeDigitsRe.FindString(in.Text); m != "" {
		return RouteExtractOut{Found: true, Route: m}, nil
	}
	return RouteExtractOut{}, nil
}

func (s *RouteExtract) Invoke(ctx context.Context, raw json.RawMessage) (any, error) {
	var in RouteExtractIn
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, fmt.Errorf("skill %s: bad args: %w", s.Name(), err)
	}
	return s.Do(ctx, in)
}
