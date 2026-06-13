// Package relay is the outbound WebSocket client that connects aqua.exe to the
// Cloudflare relay. The PC always dials out, so nothing is exposed at home.
//
// It speaks the JSON envelope protocol ({type, reqId?, data}) and exposes two
// hooks: OnConnect (fired once per established connection — used to send the
// pc_auth handshake) and OnFrame (fired per inbound frame). Writes are funneled
// through a single goroutine via Conn.Send, since the underlying socket is not
// safe for concurrent writes. Auto-reconnects with capped backoff and keepalive.
package relay

import (
	"context"
	"encoding/json"
	"log"
	"math/rand"
	"time"

	"github.com/coder/websocket"
)

const (
	baseBackoff = time.Second
	maxBackoff  = 30 * time.Second
	// A session that stays up at least this long counts as "healthy"; when it
	// later drops we reconnect from the base delay rather than the grown one.
	healthyAfter = 30 * time.Second
)

// Frame is the protocol envelope. Data is left raw so callers decode it against
// the type they expect.
type Frame struct {
	Type  string          `json:"type"`
	ReqID string          `json:"reqId,omitempty"`
	Data  json.RawMessage `json:"data,omitempty"`
}

// MakeFrame builds a Frame, JSON-encoding data into Data (nil data → no field).
func MakeFrame(typ, reqID string, data any) Frame {
	f := Frame{Type: typ, ReqID: reqID}
	if data != nil {
		raw, _ := json.Marshal(data)
		f.Data = raw
	}
	return f
}

// Conn is the per-connection send handle handed to the hooks.
type Conn struct {
	out chan Frame
}

// Send enqueues a frame for the writer goroutine. Safe for concurrent use.
func (c *Conn) Send(f Frame) {
	select {
	case c.out <- f:
	default:
		log.Printf("relay: send buffer full, dropping %s frame", f.Type)
	}
}

// Client is a reconnecting WebSocket client to the relay.
type Client struct {
	URL          string
	OnConnect    func(c *Conn)          // fired once per connection (send pc_auth here)
	OnDisconnect func()                 // fired when a connection ends
	OnFrame      func(c *Conn, f Frame) // fired per inbound frame
}

// Run connects and serves until ctx is cancelled, reconnecting on drop with
// capped exponential backoff (≤30s) plus jitter. A connection that stays healthy
// resets the backoff, so a brief blip after hours up still retries promptly.
func (c *Client) Run(ctx context.Context) {
	backoff := baseBackoff
	for ctx.Err() == nil {
		start := time.Now()
		err := c.session(ctx)
		if c.OnDisconnect != nil {
			c.OnDisconnect()
		}
		if ctx.Err() != nil {
			return
		}
		if time.Since(start) >= healthyAfter {
			backoff = baseBackoff // the connection was real; don't punish the next retry
		}
		wait := jitter(backoff)
		if err != nil {
			log.Printf("relay: disconnected: %v; retrying in %s", err, wait.Round(time.Millisecond))
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		if backoff *= 2; backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

// jitter returns a randomized delay in [d/2, d] to avoid a tight, perfectly
// periodic reconnect loop (full-jitter's lower half).
func jitter(d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	return d/2 + time.Duration(rand.Int63n(int64(d/2)+1))
}

func (c *Client) session(ctx context.Context) error {
	dialCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(dialCtx, c.URL, nil)
	if err != nil {
		return err
	}
	defer conn.CloseNow()
	log.Printf("relay: connected to %s", c.URL)

	sessCtx, sessCancel := context.WithCancel(ctx)
	defer sessCancel()

	handle := &Conn{out: make(chan Frame, 32)}

	// Writer goroutine: the only place that writes to the socket.
	go func() {
		for {
			select {
			case <-sessCtx.Done():
				return
			case f := <-handle.out:
				raw, err := json.Marshal(f)
				if err != nil {
					log.Printf("relay: marshal %s: %v", f.Type, err)
					continue
				}
				if err := conn.Write(sessCtx, websocket.MessageText, raw); err != nil {
					sessCancel()
					return
				}
				log.Printf("relay: sent %s", raw)
			}
		}
	}()

	// Keepalive ping (~45s) so idle NAT/proxies don't drop us.
	go func() {
		t := time.NewTicker(45 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-sessCtx.Done():
				return
			case <-t.C:
				pingCtx, c2 := context.WithTimeout(sessCtx, 10*time.Second)
				_ = conn.Ping(pingCtx)
				c2()
			}
		}
	}()

	if c.OnConnect != nil {
		c.OnConnect(handle)
	}

	for {
		_, data, err := conn.Read(sessCtx)
		if err != nil {
			return err
		}
		log.Printf("relay: recv %s", data)
		var f Frame
		if err := json.Unmarshal(data, &f); err != nil {
			log.Printf("relay: bad frame: %v", err)
			continue
		}
		if c.OnFrame != nil {
			c.OnFrame(handle, f)
		}
	}
}
