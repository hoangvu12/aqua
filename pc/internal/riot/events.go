package riot

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
)

// Event is one local Riot Client API event: the resource at URI changed. The
// local client runs the chat/session/party machinery itself and rebroadcasts
// every internal API change over a websocket, so we can react to game state
// without polling the GLZ/PD endpoints on a timer.
//
// Patch-fragile like the rest of riot: the wire shape is the undocumented
// LCU-style [opcode, topic, payload] protocol. Parse defensively and degrade to
// the reconcile poll on anything unexpected.
type Event struct {
	URI  string          // resource path, e.g. /chat/v6/presences
	Type string          // Create | Update | Delete
	Data json.RawMessage // resource payload (raw; callers parse the subset they need)
}

// IsMatchRelevant reports whether an event URI should trigger a state refresh.
// Two resources matter to the picker, and their paths drift across client
// versions, so match the resource name rather than a fixed version (verified
// against a live client via the /help endpoint — see TestLiveHelp):
//
//   - presence: loop state / party / queue / map. Lived at /chat/v4/presences in
//     2022; the current client emits it at /social/v1/presences. Match /presences.
//   - the messaging service "message" resource, which relays server pushes for
//     party/pregame/coregame transitions (/riot-messaging-service/v1/message).
//
// Everything else (chat messages, typing, voice/session housekeeping, telemetry)
// is ignored so the picker only re-polls on changes it actually renders.
func IsMatchRelevant(uri string) bool {
	if strings.Contains(uri, "/presences") {
		return true
	}
	return strings.Contains(uri, "/riot-messaging-service/") && strings.Contains(uri, "message")
}

// EventStream is a reconnecting client to the local Riot Client websocket
// (wss://127.0.0.1:<lockfile-port>). It reads the lockfile itself so it survives
// the Riot Client restarting with a new port/password, mirroring how the HTTP
// source re-authenticates. OnEvent is invoked for every subscribed event; keep
// it fast and non-blocking — it runs on the read loop.
type EventStream struct {
	OnEvent func(Event)
}

// Run dials the local websocket and delivers events until ctx is cancelled,
// reconnecting on drop (and while the game is down) with a capped backoff. It
// never surfaces an error: the local socket coming and going is normal (the
// player launches/closes VALORANT), and the reconcile poll covers any gap.
func (s *EventStream) Run(ctx context.Context) {
	const (
		baseBackoff = time.Second
		maxBackoff  = 15 * time.Second
		healthy     = 30 * time.Second
	)
	backoff := baseBackoff
	for ctx.Err() == nil {
		start := time.Now()
		if err := s.session(ctx); err != nil && ctx.Err() == nil {
			log.Printf("riot/events: %v", err)
		}
		if ctx.Err() != nil {
			return
		}
		if time.Since(start) >= healthy {
			backoff = baseBackoff // it was a real connection; don't punish the retry
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff *= 2; backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

// session reads the lockfile, dials, subscribes, and pumps events until the
// connection drops or ctx ends.
func (s *EventStream) session(ctx context.Context) error {
	lf, err := ReadLockfile()
	if err != nil {
		return err // game not running → ErrLockfileNotFound; Run backs off and retries
	}
	basic := "Basic " + base64.StdEncoding.EncodeToString([]byte("riot:"+lf.Password))

	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	// No subprotocol: the VALORANT client's websocket (unlike the League LCU) does
	// not use "wamp"; requesting it risks the handshake. Auth is the same Basic
	// riot:<password> the local HTTP API uses (the reference logger puts it in the
	// URL userinfo, which is just this header on the wire).
	conn, _, err := websocket.Dial(dialCtx, "wss://127.0.0.1:"+lf.Port, &websocket.DialOptions{
		HTTPClient: &http.Client{
			Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
		},
		HTTPHeader: http.Header{"Authorization": {basic}},
	})
	if err != nil {
		return err
	}
	defer conn.CloseNow()
	conn.SetReadLimit(1 << 20) // presence/event payloads exceed the 32 KiB default

	// Subscribe to every JSON API event; we filter by URI downstream. Protocol:
	// send [5, "OnJsonApiEvent"]; events arrive as
	// [8, "OnJsonApiEvent", {data, eventType, uri}].
	if err := conn.Write(ctx, websocket.MessageText, []byte(`[5, "OnJsonApiEvent"]`)); err != nil {
		return err
	}
	log.Printf("riot/events: subscribed on 127.0.0.1:%s", lf.Port)

	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return err
		}
		if ev, ok := parseEvent(data); ok && s.OnEvent != nil {
			s.OnEvent(ev)
		}
	}
}

// parseEvent decodes a [8, "OnJsonApiEvent", {uri, eventType, data}] frame. The
// client also sends subscribe acks and occasional empty payloads; those return
// ok=false and are skipped.
func parseEvent(raw []byte) (Event, bool) {
	var msg []json.RawMessage
	if err := json.Unmarshal(raw, &msg); err != nil || len(msg) != 3 {
		return Event{}, false
	}
	var opcode int
	if err := json.Unmarshal(msg[0], &opcode); err != nil || opcode != 8 {
		return Event{}, false // 8 = event; ignore acks and other opcodes
	}
	var payload struct {
		URI       string          `json:"uri"`
		EventType string          `json:"eventType"`
		Data      json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(msg[2], &payload); err != nil {
		return Event{}, false
	}
	return Event{URI: payload.URI, Type: payload.EventType, Data: payload.Data}, true
}
