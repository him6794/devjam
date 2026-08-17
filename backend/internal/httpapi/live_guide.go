package httpapi

import (
	"log"
	"net/http"
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
// Wire protocol (binary WebSocket messages both ways):
//   client -> server: raw JPEG bytes, one frame, sent roughly every 1-2s
//   server -> client: UTF-8 text, one sentence to speak — only sent when
//                      the model judged a recent frame worth a reply; most
//                      judgment ticks produce no server message at all.
// No JSON envelope: a bus rider's client only ever needs "here's a frame" /
// "here's what to say", and skipping envelope parsing keeps the hot path
// (one round trip roughly every 1-2s for the whole journey) cheap on both
// ends.
type LiveGuideHandler struct {
	guide    *skill.LiveGuide
	upgrader websocket.Upgrader
}

func NewLiveGuideHandler(guide *skill.LiveGuide) *LiveGuideHandler {
	return &LiveGuideHandler{
		guide: guide,
		upgrader: websocket.Upgrader{
			// The passenger page is same-origin (served by this project's
			// own frontend container behind nginx, see docker-compose.yml),
			// so there's no cross-site WebSocket use case to guard against
			// here the way a public API would need to.
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}
}

const liveGuideFrameDeadline = 15 * time.Second

// liveGuideJudgmentInterval is how often the server asks Gemini Live for a
// verdict on the frames it's been receiving (see skill.LiveGuideSession
// doc comment: video frames alone never complete a Live API turn, so
// something has to solicit one on a schedule independent of frame
// arrival). Slower than the ~2s frame cadence so each judgment call has
// more than one fresh frame of context to work from.
const liveGuideJudgmentInterval = 4 * time.Second

func (h *LiveGuideHandler) Handle(c *gin.Context) {
	conn, err := h.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("live_guide: websocket upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	session, err := h.guide.Open(c.Request.Context())
	if err != nil {
		log.Printf("live_guide: open session failed: %v", err)
		_ = conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseTryAgainLater, "live_guide unavailable"))
		return
	}
	defer session.Close()

	// Three independent loops sharing one WebSocket connection and one
	// Live session:
	//   - readLoop: consumes client frames as fast as they arrive, calls
	//     PushFrame (which returns immediately — see live_guide.go), never
	//     blocks on a model reply.
	//   - judgmentLoop: on its own slower ticker, calls RequestJudgment
	//     (the only call that blocks on a model turn) and forwards any
	//     verdict to the client.
	//   - the goroutine below watches for either loop's exit and closes
	//     conn, which unblocks whichever loop (if any) is still inside a
	//     blocking network call — this is what makes "rider navigates
	//     away" actually stop the session promptly instead of waiting out
	//     an in-flight judgment call.
	done := make(chan struct{})
	var stopOnce sync.Once
	stop := func() { stopOnce.Do(func() { close(done); conn.Close() }) }

	writeMu := &sync.Mutex{}
	writeText := func(text string) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return conn.WriteMessage(websocket.TextMessage, []byte(text))
	}

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
			if data[0] == 0x00 {
				if err := session.PushFrame(c.Request.Context(), data[1:]); err != nil {
					log.Printf("live_guide: push frame failed: %v", err)
					return
				}
			} else if data[0] == 0x01 {
				if err := session.PushAudio(c.Request.Context(), data[1:]); err != nil {
					log.Printf("live_guide: push audio failed: %v", err)
					return
				}
			} else {
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
		// after several consecutive errors — a single successful judgment
		// resets the counter.
		const maxConsecutiveErrors = 3
		consecutiveErrors := 0
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				reply, err := session.RequestJudgment(c.Request.Context())
				if err != nil {
					consecutiveErrors++
					log.Printf("live_guide: request judgment failed (%d/%d): %v", consecutiveErrors, maxConsecutiveErrors, err)
					if consecutiveErrors >= maxConsecutiveErrors {
						log.Printf("live_guide: too many consecutive judgment errors, closing session")
						return
					}
					continue
				}
				consecutiveErrors = 0 // success resets the counter
				if reply == "" {
					continue // model judged nothing worth saying — most ticks land here
				}
				if err := writeText(reply); err != nil {
					return
				}
			}
		}
	}()

	<-done
}
