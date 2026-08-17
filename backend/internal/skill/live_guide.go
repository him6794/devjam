package skill

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"google.golang.org/genai"
)

// liveGuideModel must be a model that supports the Live (BidiGenerateContent)
// API — the regular gemini-2.5-flash used by VisionReadSign/RouteExtract
// does not accept a Live.Connect call. Verified against this project's
// Vertex AI model catalog directly (GET .../publishers/google/models/...):
// gemini-live-2.5-flash is GA there, unlike several -live-001/-preview
// candidate names that returned 404 for this project/region. Also verified
// live (not just doc-read) that this project's Vertex AI Live endpoint only
// accepts connections at location=global — us-central1/us-east4/
// europe-west4 all reject the websocket handshake with "model not found"
// for this exact publisher model, matching the region restriction
// cmd/server/main.go already documents for Speech-to-Text v2.
const liveGuideModel = "gemini-live-2.5-flash"

// liveGuideSystemPrompt frames the whole session up front. Critical
// architectural note (found by actually running this against Vertex AI,
// not from documentation alone): SendRealtimeInput video frames do NOT by
// themselves complete a Live API turn — the Live protocol's turn-taking is
// built around voice activity detection, and pure video input has no
// "stream ended, judge now" signal. A first implementation that called
// SendRealtimeInput(video) and then blocked on Receive() hung forever on
// the very first frame (confirmed via a blocking integration test whose
// goroutine dump showed Receive stuck in conn.ReadMessage indefinitely).
// So frames are sent as ambient context only (see PushFrame — it does not
// wait for a reply), and a separate periodic text nudge (RequestJudgment)
// is what actually solicits a turn: text client content with
// TurnComplete=true reliably completes a turn, and by the time it arrives
// the model already has the recent frames as context to judge against.
const liveGuideSystemPrompt = `你是視障公車乘客的即時語音助理。你會持續收到使用者手機鏡頭拍到的畫面，並可以聽到使用者的語音。
你可以呼叫工具查詢真實的公車資訊：
1. station_status：查詢使用者目前位置附近站牌的公車到站時間。直接呼叫、不需要參數。看到公車站牌、或使用者問「現在有什麼公車／還要等多久」時使用。
2. stop_eta：查詢特定站牌所有路線的即時到站時間，需要帶上 slid 參數（來自 station_status 回傳的 candidates）。

你的任務：
1. 回答使用者的語音問題：根據畫面、工具查詢結果與常識自然回答，語氣親切、口語化、簡短，只講繁體中文。
2. 主動安全與資訊提醒：即使使用者沒有講話，每隔幾秒你會收到「請判斷目前畫面」，請根據最新畫面判斷：
  - 危險：路緣落差、對向來車、障礙物等 → 立刻、簡短、急促地提醒。
  - 公車進站：畫面中出現公車靠站 → 說出是幾號公車進站。
  - 車門位置：公車停妥後車門打開的方向 → 明確說出方向。
  - 發現公車站牌：如果提示裡的背景資料已涵蓋該站牌的到站資訊，直接引用背景資料口述（例如「這裡是XX站，307路還有3分鐘進站」）；背景資料沒涵蓋時才呼叫 station_status 工具。同一個站牌不要重複查詢，除非使用者移動了位置或明確要求再查。

規則：
- 收到「請判斷目前畫面」時，若沒有危險、沒公車、沒站牌、使用者也沒問問題，只回覆一個英文字元「.」。
- 提示裡的「背景資料」是系統自動查好的即時到站資訊，僅供你回答時引用：使用者沒問就不要主動重複，只有情況明顯改變（公車進站、時間大幅縮短）或使用者詢問時才提到。
- 使用者問到站時間時，優先直接引用最新的背景資料回答、不要呼叫工具（背景資料每幾秒就會更新一次，是最快的路徑）。只有背景資料沒涵蓋的問題（問別的站牌、別的路線、特定方向）才呼叫工具查詢。
- 主動提醒時只講一句話，口語化。
- 工具查詢失敗時，用一句話說明無法查詢，不要唸出錯誤訊息或 JSON。`

// liveGuideJudgmentPrompt is the periodic text nudge that completes a turn
// (see the architectural note above). Deliberately short — it's not new
// information, just the trigger to make the model commit to a judgment
// about the frames it has already been receiving as realtime input.
const liveGuideJudgmentPrompt = "請判斷目前畫面"

// liveGuideSilence is what the model is instructed to answer with when a
// judgment check-in needs no rider-facing reply (see liveGuideSystemPrompt).
// The receive loop treats this — after trimming whitespace — as "nothing to
// say" and never forwards it to the client.
const liveGuideSilence = "."

// GPSCoord is the rider's latest known position, fed to the live session by
// the client so the station_status tool can answer "what's near me" without
// the model ever needing to know a latitude/longitude number.
type GPSCoord struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

// LiveToolset is the set of functions a Live session offers the model.
// Implemented by the httpapi package (not here) so it can wire both the
// skill registry and the orchestrator-backed station lookup, neither of
// which skill may import (agent depends on skill, not the reverse).
type LiveToolset interface {
	// Functions returns the function declarations to register when the
	// Live connection is opened.
	Functions() []*genai.FunctionDeclaration
	// Invoke runs one tool call. gps is the session's latest known rider
	// position (nil when the client has not reported one yet); tools whose
	// arguments omit lat/lon are expected to fall back to it.
	Invoke(ctx context.Context, name string, args map[string]any, gps *GPSCoord) (any, error)
}

// LiveCallbacks is how one live session reports model output back to its
// caller. All callbacks are invoked from the session's single receive
// goroutine, so implementations must return quickly and do their own
// locking/queueing for anything slow.
type LiveCallbacks struct {
	// OnText fires once per completed turn that said something rider-facing
	// (silence turns never fire it).
	OnText func(text string)
	// OnDone fires exactly once when the receive loop exits (session closed
	// or connection dropped). err is the receive error that ended it.
	OnDone func(err error)
}

// LiveGuide opens real-time Gemini Live sessions that watch a continuous
// camera feed and decide for themselves when to speak — danger warnings,
// "your bus just arrived", "the door is on your left" — rather than being
// told the answer by another system and asked to phrase it. That framing
// (model judges the frames, not a caller who already computed the verdict)
// is the point of using the Live API here at all instead of one-shot
// GenerateContent calls: it holds an open session with visual context
// across many frames, so it doesn't need the situation re-explained on
// every call.
//
// Response modality is text, spoken by the client's own TTS. Native audio
// output would be preferable, but verified live that gemini-live-2.5-flash
// on Vertex AI cannot produce intelligible Mandarin audio: every prebuilt
// voice was tested against this project and none speaks Chinese
// comprehensibly (the API also rejects cmn-TW/zh-TW as voice-output
// language codes). Text + device TTS is the reliable path until the model
// gains a Chinese voice.
type LiveGuide struct {
	client *genai.Client
	tools  LiveToolset // nil = sessions open without function calling
}

func NewLiveGuide(client *genai.Client, tools LiveToolset) *LiveGuide {
	return &LiveGuide{client: client, tools: tools}
}

func (s *LiveGuide) Name() string { return "live_guide" }

func (s *LiveGuide) Description() string {
	return "Open a real-time Gemini Live session that watches a camera feed and proactively speaks up for danger, bus arrival, or door-side guidance."
}

// Open starts one Live session and begins its receive loop. The caller owns
// its lifecycle: push frames with PushFrame, push microphone audio with
// PushAudio, periodically solicit a judgment with RequestJudgment, and
// Close it when the rider's journey ends (arrival, or they cancel). A nil
// client (no Vertex AI credentials) fails fast here rather than on the
// first call, matching this codebase's other skills' pattern of surfacing
// missing-config errors as early as possible.
func (s *LiveGuide) Open(ctx context.Context, cbs LiveCallbacks) (*LiveGuideSession, error) {
	if s.client == nil {
		return nil, errors.New("live_guide: client not configured")
	}
	cfg := &genai.LiveConnectConfig{
		ResponseModalities: []genai.Modality{genai.ModalityText},
		SystemInstruction: &genai.Content{
			Role:  "user",
			Parts: []*genai.Part{{Text: liveGuideSystemPrompt}},
		},
	}
	if s.tools != nil {
		cfg.Tools = []*genai.Tool{{FunctionDeclarations: s.tools.Functions()}}
	}
	session, err := s.client.Live.Connect(ctx, liveGuideModel, cfg)
	if err != nil {
		return nil, fmt.Errorf("live_guide: connect: %w", err)
	}
	sess := &LiveGuideSession{session: session, tools: s.tools}
	go sess.receiveLoop(cbs)
	return sess, nil
}

// LiveGuideSession wraps one open genai Live connection for one rider's
// journey.
//
// Concurrency model: gorilla/websocket (which genai.Session wraps
// directly, with no internal locking) permits exactly one concurrent
// reader and one concurrent writer. This session dedicates its receive
// loop goroutine to being the only reader, so the model's replies — spoken
// answers to voice questions and tool calls alike — are picked up the
// moment they arrive instead of only on a polling timer. All writes
// (frames, audio, judgment nudges, tool responses) serialize through mu.
type LiveGuideSession struct {
	mu      sync.Mutex // guards every send on the underlying WebSocket
	session *genai.Session
	tools   LiveToolset

	gpsMu sync.Mutex
	gps   *GPSCoord
}

// SetGPS records the rider's latest position for the station_status tool.
// Zero/NaN coordinates are ignored rather than stored: a broken fix must
// not silently become "the rider is at 0,0".
func (sess *LiveGuideSession) SetGPS(lat, lon float64) {
	if lat == 0 && lon == 0 {
		return
	}
	sess.gpsMu.Lock()
	defer sess.gpsMu.Unlock()
	sess.gps = &GPSCoord{Lat: lat, Lon: lon}
}

func (sess *LiveGuideSession) GPS() *GPSCoord {
	sess.gpsMu.Lock()
	defer sess.gpsMu.Unlock()
	if sess.gps == nil {
		return nil
	}
	return &GPSCoord{Lat: sess.gps.Lat, Lon: sess.gps.Lon}
}

// PushFrame sends one camera frame (JPEG bytes) as realtime video input and
// returns as soon as it's sent — it does NOT wait for or expect a reply.
// The frame becomes context the model has available for its next turn; see
// this file's architectural note for why frames can't drive turns
// themselves. Because this never blocks on a model turn, a slow or stuck
// generation never stalls frame ingestion — at worst a frame waits briefly
// for the mutex held by an in-flight send.
func (sess *LiveGuideSession) PushFrame(ctx context.Context, jpegData []byte) error {
	sess.mu.Lock()
	defer sess.mu.Unlock()

	if err := sess.session.SendRealtimeInput(genai.LiveRealtimeInput{
		Video: &genai.Blob{Data: jpegData, MIMEType: "image/jpeg"},
	}); err != nil {
		return fmt.Errorf("live_guide: send frame: %w", err)
	}
	return nil
}

// PushAudio sends one audio frame (PCM 16kHz bytes) as realtime audio input.
// Voice questions the model answers on its own VAD are picked up by the
// receive loop immediately — no judgment tick involved.
func (sess *LiveGuideSession) PushAudio(ctx context.Context, pcmData []byte) error {
	sess.mu.Lock()
	defer sess.mu.Unlock()

	if err := sess.session.SendRealtimeInput(genai.LiveRealtimeInput{
		Audio: &genai.Blob{Data: pcmData, MIMEType: "audio/pcm;rate=16000"},
	}); err != nil {
		return fmt.Errorf("live_guide: send audio: %w", err)
	}
	return nil
}

// RequestJudgment sends the periodic text nudge that completes a turn (see
// liveGuideJudgmentPrompt). extraContext, when non-empty, is appended as a
// parenthetical "背景資料" block — the server-side agent pipeline's latest
// ETA snapshot — so the model can answer data questions straight from
// context instead of spending a tool round trip (see
// httpapi.LiveGuideHandler.backgroundStationInfo). Unlike the
// pre-receive-loop design it no longer blocks on the model's reply — the
// receive loop delivers that through the callbacks — so callers should
// invoke it on a timer independent of frame arrival (see
// httpapi.LiveGuideHandler's judgment ticker).
func (sess *LiveGuideSession) RequestJudgment(ctx context.Context, extraContext string) error {
	sess.mu.Lock()
	defer sess.mu.Unlock()

	prompt := liveGuideJudgmentPrompt
	if extraContext != "" {
		prompt = fmt.Sprintf("%s（背景資料：%s）", liveGuideJudgmentPrompt, extraContext)
	}
	err := sess.session.SendClientContent(genai.LiveClientContentInput{
		Turns: []*genai.Content{{
			Role:  "user",
			Parts: []*genai.Part{{Text: prompt}},
		}},
		TurnComplete: genai.Ptr(true),
	})
	if err != nil {
		return fmt.Errorf("live_guide: send judgment prompt: %w", err)
	}
	return nil
}

// receiveLoop is the session's only reader, for the session's whole life.
// It owns turn state: text accumulates per turn and is reported at
// TurnComplete (silence turns excluded), and tool calls are executed inline
// and answered with SendToolResponse so the model can finish the turn it
// started.
func (sess *LiveGuideSession) receiveLoop(cbs LiveCallbacks) {
	var turnText strings.Builder
	for {
		msg, err := sess.session.Receive()
		if err != nil {
			log.Printf("live_guide: receive loop exited: %v", err)
			if cbs.OnDone != nil {
				cbs.OnDone(err)
			}
			return
		}
		if msg.ToolCall != nil {
			if err := sess.handleToolCall(msg.ToolCall.FunctionCalls); err != nil {
				log.Printf("live_guide: tool call failed: %v", err)
				if cbs.OnDone != nil {
					cbs.OnDone(err)
				}
				return
			}
			continue
		}
		if msg.ToolCallCancellation != nil {
			continue
		}
		sc := msg.ServerContent
		if sc == nil {
			continue
		}
		if sc.Interrupted {
			// The model interrupted its own turn (usually because the rider
			// started speaking). Whatever was accumulated for the old turn
			// is obsolete.
			turnText.Reset()
		}
		if sc.ModelTurn != nil {
			for _, part := range sc.ModelTurn.Parts {
				if part.Thought {
					continue
				}
				if part.Text != "" {
					turnText.WriteString(part.Text)
				}
			}
		}
		if sc.TurnComplete {
			text := trimSilence(turnText.String())
			if text != "" && text != liveGuideSilence {
				if cbs.OnText != nil {
					cbs.OnText(text)
				}
			}
			turnText.Reset()
		}
	}
}

// handleToolCall executes every function call the model issued in one
// message and answers with one SendToolResponse, so the model can continue
// (and finish) the turn it was in the middle of. A failing tool is
// reported as an error result, not a session-ending error: like the
// orchestrator layer, this treats a bad tool outcome as normal input for
// the model to phrase around, not a reason to drop the rider's connection.
func (sess *LiveGuideSession) handleToolCall(calls []*genai.FunctionCall) error {
	if sess.tools == nil {
		return errors.New("live_guide: model issued a tool call but no tools are configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	responses := make([]*genai.FunctionResponse, 0, len(calls))
	for _, call := range calls {
		resp := &genai.FunctionResponse{Name: call.Name, ID: call.ID}
		out, err := sess.tools.Invoke(ctx, call.Name, call.Args, sess.GPS())
		if err != nil {
			log.Printf("live_guide: tool %s failed: %v", call.Name, err)
			resp.Response = map[string]any{"error": err.Error()}
		} else {
			log.Printf("live_guide: tool %s ok", call.Name)
			resp.Response = map[string]any{"output": out}
		}
		responses = append(responses, resp)
	}

	sess.mu.Lock()
	defer sess.mu.Unlock()
	if err := sess.session.SendToolResponse(genai.LiveSendToolResponseParameters{FunctionResponses: responses}); err != nil {
		return fmt.Errorf("live_guide: send tool response: %w", err)
	}
	return nil
}

// trimSilence strips whitespace so a reply of " . \n" (the model sometimes
// pads the sentinel) still compares equal to liveGuideSilence.
func trimSilence(s string) string {
	start, end := 0, len(s)
	for start < end && isSpace(s[start]) {
		start++
	}
	for end > start && isSpace(s[end-1]) {
		end--
	}
	return s[start:end]
}

func isSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

// SchemaFromMap converts a JSON-Schema-style map (the shape skill
// InputSchema methods already return) into the typed *genai.Schema that
// genai.FunctionDeclaration.Parameters expects. Only the schema subset this
// project's skills actually use is handled; the skills are the only
// producers.
func SchemaFromMap(m map[string]any) *genai.Schema {
	s := &genai.Schema{}
	if t, ok := m["type"].(string); ok {
		s.Type = genai.Type(t)
	}
	if d, ok := m["description"].(string); ok {
		s.Description = d
	}
	if props, ok := m["properties"].(map[string]any); ok {
		s.Properties = make(map[string]*genai.Schema, len(props))
		for name, raw := range props {
			if pm, ok := raw.(map[string]any); ok {
				s.Properties[name] = SchemaFromMap(pm)
			}
		}
	}
	switch req := m["required"].(type) {
	case []string:
		s.Required = req
	case []any:
		for _, r := range req {
			if name, ok := r.(string); ok {
				s.Required = append(s.Required, name)
			}
		}
	}
	return s
}

func (sess *LiveGuideSession) Close() error {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	return sess.session.Close()
}
