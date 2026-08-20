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












const liveGuideModel = "gemini-live-2.5-flash"















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





const liveGuideJudgmentPrompt = "請判斷目前畫面"





const liveGuideSilence = "."




type GPSCoord struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}





type LiveToolset interface {
	
	
	Functions() []*genai.FunctionDeclaration
	
	
	
	Invoke(ctx context.Context, name string, args map[string]any, gps *GPSCoord) (any, error)
}





type LiveCallbacks struct {
	
	
	OnText func(text string)
	
	
	OnDone func(err error)
}


















type LiveGuide struct {
	client *genai.Client
	tools  LiveToolset 
}

func NewLiveGuide(client *genai.Client, tools LiveToolset) *LiveGuide {
	return &LiveGuide{client: client, tools: tools}
}

func (s *LiveGuide) Name() string { return "live_guide" }

func (s *LiveGuide) Description() string {
	return "Open a real-time Gemini Live session that watches a camera feed and proactively speaks up for danger, bus arrival, or door-side guidance."
}








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











type LiveGuideSession struct {
	mu      sync.Mutex 
	session *genai.Session
	tools   LiveToolset

	gpsMu sync.Mutex
	gps   *GPSCoord
}




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
