package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"devjam-backend/internal/skill"
)

// LiveGuideHandler upgrades /api/live_guide to a WebSocket so the rider's
// camera can stream continuously while their journey is in progress — this
// is what "the Live API is actually watching, not being told the answer"
// requires: a one-shot HTTP request/response can't hold a camera feed open,
// and the model needs many frames in the same session to notice a bus
// arriving or a door opening, not one frame per independent call.
//
// Wire protocol (binary WebSocket messages client -> server):
//   0x00 + JPEG bytes: one camera frame, sent roughly every second
//   0x01 + PCM bytes: microphone audio (16kHz mono s16le), streamed continuously
//   0x02 + UTF-8 JSON {"lat":..,"lng":..}: a GPS fix, so the model's
//          station_status tool can answer "what's near me" without the
//          model ever inventing a coordinate
//   server -> client: UTF-8 text, one sentence to speak — only sent when
//          the model judged a turn worth a reply; most judgment ticks
//          produce no server message at all. The client speaks it with its
//          own device TTS.
type LiveGuideHandler struct {
	guide *skill.LiveGuide
	// stationStatus is the background agent pipeline the judgment ticker
	// runs so the model gets an ETA snapshot in its prompt context instead
	// of spending a tool round trip per data question. nil disables the
	// injection (plain judgment prompts only).
	stationStatus *StationStatusSkill
	upgrader      websocket.Upgrader
}

func NewLiveGuideHandler(guide *skill.LiveGuide, stationStatus *StationStatusSkill) *LiveGuideHandler {
	// 白名單跨源 WebSocket 檢查：Live session 每幀都燒 Gemini 額度，
	// 惡意網站若能連上這個 WS 就可以塞假畫面/假音訊洗掉使用者的
	// 語音額度（cross-site WebSocket hijacking）。允許的 host 用
	// LIVE_GUIDE_ALLOWED_ORIGINS 逗號分隔覆寫，預設是正式網域加
	// 本機開發用的 localhost。
	allowedHosts := map[string]bool{
		"devjam.justin0711.com": true,
		"localhost":             true,
		"127.0.0.1":             true,
	}
	if extra := os.Getenv("LIVE_GUIDE_ALLOWED_ORIGINS"); extra != "" {
		for _, o := range strings.Split(extra, ",") {
			o = strings.TrimSpace(o)
			if u, err := url.Parse(o); err == nil && u.Hostname() != "" {
				allowedHosts[u.Hostname()] = true
			}
		}
	}
	return &LiveGuideHandler{
		guide:         guide,
		stationStatus: stationStatus,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				// 同源連線可能不帶 Origin（部分客戶端）→ 放行；有帶的
				// 只放行白名單 host，其餘拒絕（gorilla 回 403）。
				origin := r.Header.Get("Origin")
				if origin == "" {
					return true
				}
				u, err := url.Parse(origin)
				return err == nil && allowedHosts[u.Hostname()]
			},
		},
	}
}

const liveGuideFrameDeadline = 15 * time.Second

// liveGuideJudgmentInterval is how often the server asks Gemini Live for a
// verdict on the frames it's been receiving (see skill.LiveGuideSession
// doc comment: video frames alone never complete a Live API turn, so
// something has to solicit one on a schedule independent of frame
// arrival). Slower than the ~1s frame cadence so each judgment call has
// more than one fresh frame of context to work from. Voice questions don't
// wait on this ticker at all — the session's receive loop answers them the
// moment the model responds.
const liveGuideJudgmentInterval = 4 * time.Second

func (h *LiveGuideHandler) Handle(c *gin.Context) {
	conn, err := h.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("live_guide: websocket upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	writeMu := &sync.Mutex{}
	writeText := func(text string) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return conn.WriteMessage(websocket.TextMessage, []byte(text))
	}

	// done is closed (once) by stop; the two loops and the session's
	// OnDone callback all funnel into stop, and the handler's final
	// <-done waits for whichever of them goes first. Closing conn inside
	// stop unblocks whichever loop (if any) is still inside a blocking
	// network call — this is what makes "rider navigates away" actually
	// stop the session promptly instead of waiting out an in-flight
	// model turn.
	done := make(chan struct{})
	var stopOnce sync.Once
	stop := func() { stopOnce.Do(func() { close(done); conn.Close() }) }

	session, err := h.guide.Open(c.Request.Context(), skill.LiveCallbacks{
		OnText: func(text string) {
			if err := writeText(text); err != nil {
				stop()
			}
		},
		OnDone: func(err error) {
			log.Printf("live_guide: session ended: %v", err)
			stop()
		},
	})
	if err != nil {
		log.Printf("live_guide: open session failed: %v", err)
		_ = conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseTryAgainLater, "live_guide unavailable"))
		return
	}
	defer session.Close()

	// Three independent loops sharing one WebSocket connection and one
	// Live session:
	//   - readLoop: consumes client frames/audio/GPS as fast as they
	//     arrive, calls PushFrame/PushAudio (which return immediately —
	//     see live_guide.go), never blocks on a model reply.
	//   - judgmentLoop: on its own slower ticker, sends the judgment
	//     nudge that completes a turn; the model's reply comes back
	//     through the session's receive loop via the callbacks above.
	//   - stop closes conn when any of the loops or the session ends,
	//     which unblocks whichever one (if any) is still inside a
	//     blocking network call.
	go func() {
		defer stop()
		for {
			_ = conn.SetReadDeadline(time.Now().Add(liveGuideFrameDeadline))
			msgType, data, err := conn.ReadMessage()
			if err != nil {
				return // client closed the tab, network drop, or deadline — normal end of journey
			}
			if msgType != websocket.BinaryMessage || len(data) == 0 {
				continue
			}
			switch data[0] {
			case 0x00:
				if err := session.PushFrame(c.Request.Context(), data[1:]); err != nil {
					log.Printf("live_guide: push frame failed: %v", err)
					return
				}
			case 0x01:
				if err := session.PushAudio(c.Request.Context(), data[1:]); err != nil {
					log.Printf("live_guide: push audio failed: %v", err)
					return
				}
			case 0x02:
				var fix struct {
					Lat float64 `json:"lat"`
					Lng float64 `json:"lng"`
				}
				if err := json.Unmarshal(data[1:], &fix); err != nil {
					log.Printf("live_guide: bad GPS payload: %v", err)
					continue
				}
				session.SetGPS(fix.Lat, fix.Lng)
			default:
				// Old client fallback just in case
				if err := session.PushFrame(c.Request.Context(), data); err != nil {
					log.Printf("live_guide: push frame failed: %v", err)
					return
				}
			}
		}
	}()

	go func() {
		defer stop()
		ticker := time.NewTicker(liveGuideJudgmentInterval)
		defer ticker.Stop()
		// Tolerate transient Gemini errors (rate limits, timeouts) instead
		// of killing the whole session on the first failure. Only give up
		// after several consecutive errors — a single successful send
		// resets the counter.
		const maxConsecutiveErrors = 3
		consecutiveErrors := 0
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				info := h.backgroundStationInfo(c.Request.Context(), session)
				if err := session.RequestJudgment(c.Request.Context(), info); err != nil {
					consecutiveErrors++
					log.Printf("live_guide: request judgment failed (%d/%d): %v", consecutiveErrors, maxConsecutiveErrors, err)
					if consecutiveErrors >= maxConsecutiveErrors {
						log.Printf("live_guide: too many consecutive judgment errors, closing session")
						return
					}
					continue
				}
				consecutiveErrors = 0 // success resets the counter
			}
		}
	}()

	<-done
}

// backgroundStationInfo runs the same orchestrator station lookup the
// model's station_status tool uses, but on the server's own clock, and
// formats a one-line ETA snapshot for injection into the judgment prompt.
// That way a "還有幾分鐘" question is answered straight from model
// context — one generation pass, zero tool round trips — while the tools
// stay available for drill-downs (other stations, specific routes).
//
// Degrades to "" (plain prompt) rather than blocking or failing the tick:
// no GPS yet, station not found, or the query exceeding its 3s budget all
// just mean this tick carries no data, and the next tick retries.
func (h *LiveGuideHandler) backgroundStationInfo(ctx context.Context, sess *skill.LiveGuideSession) string {
	if h.stationStatus == nil {
		return ""
	}
	gps := sess.GPS()
	if gps == nil {
		return ""
	}
	qctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	out, err := h.stationStatus.Do(qctx, StationStatusIn{Lat: gps.Lat, Lon: gps.Lon})
	if err != nil || !out.Found {
		return ""
	}

	parts := []string{fmt.Sprintf("最近的站牌「%s」", out.Name)}
	for i, b := range out.Buses {
		if i >= 3 {
			break
		}
		bus := fmt.Sprintf("%s路", b.Route)
		if b.Direction != "" {
			bus = fmt.Sprintf("%s路（%s）", b.Route, b.Direction)
		}
		if b.HasETA {
			parts = append(parts, fmt.Sprintf("%s還有%d分鐘", bus, b.ETAMinutes))
		} else {
			parts = append(parts, fmt.Sprintf("%s無到站時間", bus))
		}
	}
	return strings.Join(parts, "，")
}
