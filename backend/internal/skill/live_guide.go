package skill

import (
	"context"
	"errors"
	"fmt"
	"sync"

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
const liveGuideSystemPrompt = `你是視障公車乘客的即時視覺協助員。你會持續收到使用者手機鏡頭拍到的畫面，並可以聽到使用者的語音。
你的任務有兩個：
1. 回應使用者的問題：當使用者用語音問你問題時，根據畫面與常識自然地回答他們，語氣要親切、口語化、簡短。
2. 主動安全與資訊提醒：即使使用者沒有講話，每隔幾秒你會收到「請判斷目前畫面」的提示字眼，請根據最新畫面判斷：
  - 危險：路緣落差、對向來車、障礙物等 → 立刻、簡短、急促地提醒。
  - 公車進站：畫面中出現公車靠站 → 說出是幾號公車進站。
  - 車門位置：公車停妥後車門打開的方向 → 明確說出方向。
  - 發現公車站牌：如果你在畫面中清楚看見公車站牌，請在回覆的最後加上特定字串 "[FIND_STATION]"，這樣系統就會自動幫使用者查詢該站牌的到站時間。

規則：
- 如果是收到「請判斷目前畫面」，且沒有危險、沒公車、沒站牌、使用者也沒問問題，只回覆一個字元的英文句點「.」。
- 主動提醒時，只講一句話，繁體中文，口語化。
- 如果你加上了 [FIND_STATION]，你可以說類似「看到站牌了，我幫您查詢」然後加上 [FIND_STATION]。同一個站牌不要重複查詢。`

// liveGuideJudgmentPrompt is the periodic text nudge that completes a turn
// (see the architectural note above). Deliberately short — it's not new
// information, just the trigger to make the model commit to a judgment
// about the frames it has already been receiving as realtime input.
const liveGuideJudgmentPrompt = "請判斷目前畫面"

// liveGuideSilence is what the model is instructed to answer with when a
// judgment check-in needs no rider-facing reply (see liveGuideSystemPrompt).
// RequestJudgment treats this — after trimming whitespace — as "nothing to
// say" and returns an empty string rather than passing "." through to
// speech synthesis.
const liveGuideSilence = "."

// LiveGuideSession wraps one open genai Live connection for one rider's
// journey. PushFrame and RequestJudgment are the only two operations a
// caller needs (see httpapi.LiveGuideHandler): push frames as fast as the
// camera produces them, and separately ask for a judgment on a slower
// timer. Both serialize through mu so the underlying genai.Session (a
// single WebSocket to Vertex AI) never has two goroutines writing to or
// reading from it concurrently.
type LiveGuideSession struct {
	mu      sync.Mutex
	session *genai.Session
}

// PushFrame sends one camera frame (JPEG bytes) as realtime video input and
// returns as soon as it's sent — it does NOT wait for or expect a reply.
// The frame becomes context the model has available the next time
// RequestJudgment completes a turn; see this file's architectural note for
// why frames can't drive turns themselves. Because this never blocks on
// Receive(), a slow or stuck Gemini turn (see RequestJudgment) never stalls
// frame ingestion — at worst a frame waits briefly for the mutex held by an
// in-flight judgment call.
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

// RequestJudgment sends the periodic text nudge that completes a Live API
// turn (see liveGuideJudgmentPrompt) and returns either a short rider-facing
// sentence or "" when the model judged the recent frames needed no reply.
// This is the only call in this session that blocks on Receive() — callers
// should invoke it on a timer independent of frame arrival, not per-frame
// (see httpapi.LiveGuideHandler's judgment ticker).
func (sess *LiveGuideSession) RequestJudgment(ctx context.Context) (string, error) {
	sess.mu.Lock()
	defer sess.mu.Unlock()

	err := sess.session.SendClientContent(genai.LiveClientContentInput{
		Turns: []*genai.Content{{
			Role:  "user",
			Parts: []*genai.Part{{Text: liveGuideJudgmentPrompt}},
		}},
		TurnComplete: genai.Ptr(true),
	})
	if err != nil {
		return "", fmt.Errorf("live_guide: send judgment prompt: %w", err)
	}

	reply, err := sess.readTurn()
	if err != nil {
		return "", err
	}
	if reply == liveGuideSilence || reply == "" {
		return "", nil
	}
	return reply, nil
}

func (sess *LiveGuideSession) readTurn() (string, error) {
	var reply string
	for {
		msg, err := sess.session.Receive()
		if err != nil {
			return "", fmt.Errorf("live_guide: receive: %w", err)
		}
		if msg.ServerContent == nil {
			continue
		}
		if msg.ServerContent.ModelTurn != nil {
			for _, part := range msg.ServerContent.ModelTurn.Parts {
				if part.Text != "" {
					reply += part.Text
				}
			}
		}
		if msg.ServerContent.TurnComplete {
			break
		}
	}
	return trimSilence(reply), nil
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

func (sess *LiveGuideSession) Close() error {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	return sess.session.Close()
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
type LiveGuide struct {
	client *genai.Client
}

func NewLiveGuide(client *genai.Client) *LiveGuide {
	return &LiveGuide{client: client}
}

func (s *LiveGuide) Name() string { return "live_guide" }

func (s *LiveGuide) Description() string {
	return "Open a real-time Gemini Live session that watches a camera feed and proactively speaks up for danger, bus arrival, or door-side guidance."
}

// Open starts one Live session. The caller owns its lifecycle: push frames
// with PushFrame, periodically solicit a verdict with RequestJudgment, and
// Close it when the rider's journey ends (arrival, or they cancel). A nil
// client (no Vertex AI credentials) fails fast here rather than on the
// first call, matching this codebase's other skills' pattern of surfacing
// missing-config errors as early as possible.
func (s *LiveGuide) Open(ctx context.Context) (*LiveGuideSession, error) {
	if s.client == nil {
		return nil, errors.New("live_guide: client not configured")
	}
	session, err := s.client.Live.Connect(ctx, liveGuideModel, &genai.LiveConnectConfig{
		ResponseModalities: []genai.Modality{genai.ModalityText},
		SystemInstruction: &genai.Content{
			Role:  "user",
			Parts: []*genai.Part{{Text: liveGuideSystemPrompt}},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("live_guide: connect: %w", err)
	}
	return &LiveGuideSession{session: session}, nil
}
