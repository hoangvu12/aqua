// Command aqua is the PC app. It polls VALORANT (or a simulation with -sim),
// derives the wire `state`, and bridges it over the relay to a paired phone,
// which can drive select/lock and arm the auto-lock pre-pick.
//
// By default it presents a console UI (internal/ui): the pairing QR + code, live
// status, and single-key actions (r toggle remote, u unpair, q quit). Because the
// UI owns the terminal, logs are routed to %APPDATA%\Aqua\aqua.log. Run with
// -headless to drop the UI and log to stderr instead (prints PAIRCODE=… for the
// integration tests / scripted pairing).
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"aqua/internal/config"
	"aqua/internal/picker"
	"aqua/internal/relay"
	"aqua/internal/ui"
	"aqua/internal/updater"
	"aqua/internal/version"
)

// connSink emits picker frames to whichever relay connection is currently live,
// and mirrors the game state into the console UI.
type connSink struct {
	cur *atomic.Pointer[relay.Conn]
	ui  *ui.UI // may be nil in headless mode
}

func (s *connSink) send(f relay.Frame) {
	if c := s.cur.Load(); c != nil {
		c.Send(f)
	}
}
func (s *connSink) SendState(st picker.State) {
	if s.ui != nil {
		s.ui.SetGameState(st.State)
	}
	s.send(relay.MakeFrame("state", "", st))
}
func (s *connSink) SendResult(reqID string, ok bool, message string) {
	s.send(relay.MakeFrame("result", reqID, map[string]any{"ok": ok, "message": message}))
}
func (s *connSink) SendAuthStatus(ok bool, message string) {
	s.send(relay.MakeFrame("auth_status", "", map[string]any{"ok": ok, "message": message}))
}

func main() {
	var (
		workerFlag  = flag.String("worker", "", "override relay base URL (ws/wss); default from config")
		sim         = flag.Bool("sim", false, "use the simulated game source (no live VALORANT)")
		mint        = flag.Bool("mint", true, "mint a pair code after relay auth")
		ttl         = flag.Int("ttl", 300, "pair-code TTL in seconds (1..600)")
		headless    = flag.Bool("headless", false, "no console UI; log to stderr (for tests/scripting)")
		showVersion = flag.Bool("version", false, "print the Aqua version and exit")
		doUpdate    = flag.Bool("update", false, "check for and install a newer aqua.exe, then exit")
	)
	flag.Parse()

	if *showVersion {
		fmt.Println("Aqua", version.Version)
		return
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	workerURL := cfg.WorkerURL
	if *workerFlag != "" {
		workerURL = *workerFlag
	}

	// -update runs as its own short-lived mode (no UI / relay), then exits.
	if *doUpdate {
		os.Exit(runUpdate())
	}

	// Sweep any <exe>.old left by a previous in-place update (Windows couldn't
	// delete it while it was the running image).
	updater.CleanupOldBinary()

	// In UI mode the UI owns the terminal, so logs go to a file. In headless
	// mode keep the original stderr logging (PAIRCODE=… is parseable there).
	if !*headless {
		if f := openLogFile(); f != nil {
			log.SetOutput(f)
			defer f.Close()
		} else {
			log.SetOutput(io.Discard)
		}
	}

	agentURL := fmt.Sprintf("%s/agent?role=pc&device=%s", workerURL, cfg.DeviceID)

	var src picker.Source
	if *sim {
		src = picker.NewSimSource()
		log.Printf("aqua pc client (SIM); device=%s worker=%s", cfg.DeviceID, workerURL)
	} else {
		src = picker.NewRiotSource()
		log.Printf("aqua pc client; device=%s worker=%s", cfg.DeviceID, workerURL)
	}

	baseCtx, stopSignal := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stopSignal()
	ctx, cancel := context.WithCancel(baseCtx)
	defer cancel()

	var cur atomic.Pointer[relay.Conn]

	// Console UI (nil in headless mode).
	var u *ui.UI
	if !*headless {
		u = ui.New(cfg.DeviceID, ui.Actions{}) // actions wired below once cmds exists
	}

	sink := &connSink{cur: &cur, ui: u}
	pk := picker.New(cfg, src, sink)

	// announcePair surfaces a freshly minted pair code (UI or stdout).
	announcePair := func(code string, expires time.Time) {
		url := fmt.Sprintf("%s/?code=%s&device=%s", httpBase(workerURL), code, cfg.DeviceID)
		if u != nil {
			u.SetPairCode(code, url, expires)
		} else {
			log.Printf("PAIRCODE=%s URL=%s", code, url)
			log.Printf("pair this phone: open %s", url)
		}
	}

	minted := false
	client := &relay.Client{
		URL: agentURL,
		OnConnect: func(c *relay.Conn) {
			cur.Store(c)
			minted = false
			if u != nil {
				u.SetRelay("connected")
			}
			c.Send(relay.MakeFrame("pc_auth", "", map[string]string{"secret": cfg.DeviceSecret}))
		},
		OnDisconnect: func() {
			cur.Store(nil)
			if u != nil {
				// If remote was just toggled off, the supervisor cancelled us —
				// stay "off". Otherwise the supervisor will reconnect.
				if cfg.RemoteEnabled {
					u.SetRelay("connecting")
				} else {
					u.SetRelay("off")
				}
				u.ClearPairCode()
			}
		},
		OnFrame: func(c *relay.Conn, f relay.Frame) {
			switch f.Type {
			case "auth_status":
				// Relay-level auth result (from the DO), not Riot.
				var d struct {
					OK      bool   `json:"ok"`
					Message string `json:"message"`
				}
				_ = json.Unmarshal(f.Data, &d)
				log.Printf("relay auth: ok=%v %s", d.OK, d.Message)
				if d.OK {
					if u != nil {
						u.SetRelay("authenticated")
					}
					pk.Republish() // push current state so late phones get a snapshot
					if *mint && !minted {
						minted = true
						c.Send(relay.MakeFrame("mint_pair_code", "mint1", map[string]int{"ttl_seconds": *ttl}))
					}
				} else if u != nil {
					u.SetRelay("error")
				}
			case "result":
				// Either a mint_pair_code reply (carries code) or an unpair_all
				// ack (carries message).
				var d struct {
					Code      string `json:"code"`
					ExpiresAt int64  `json:"expires_at"`
					Message   string `json:"message"`
				}
				_ = json.Unmarshal(f.Data, &d)
				switch {
				case d.Code != "":
					announcePair(d.Code, time.UnixMilli(d.ExpiresAt))
				case d.Message != "":
					log.Printf("relay: %s", d.Message)
					if u != nil {
						u.Notify("%s", d.Message)
					}
				}
			default:
				// Phone command: get_state | select | lock | set_config | test_auth.
				// Run off the read loop — Riot calls may block.
				go pk.HandlePhoneFrame(ctx, f.Type, f.ReqID, f.Data)
			}
		},
	}

	// Remote supervisor: starts/stops the relay client so the UI's `r` key can
	// toggle remote control without tearing down the picker.
	cmds := make(chan bool, 8)
	go superviseRemote(ctx, client, cmds, cfg.RemoteEnabled)

	if u != nil {
		u.SetRemoteEnabled(cfg.RemoteEnabled)
		if cfg.RemoteEnabled {
			u.SetRelay("connecting")
		}
		u.SetActions(ui.Actions{
			ToggleRemote: func() {
				cfg.RemoteEnabled = !cfg.RemoteEnabled
				if err := cfg.Save(); err != nil {
					log.Printf("save config: %v", err)
				}
				u.SetRemoteEnabled(cfg.RemoteEnabled)
				if cfg.RemoteEnabled {
					u.SetRelay("connecting")
				} else {
					u.SetRelay("off")
					u.ClearPairCode()
				}
				cmds <- cfg.RemoteEnabled
			},
			UnpairAll: func() {
				if c := cur.Load(); c != nil {
					c.Send(relay.MakeFrame("unpair_all", "unpair1", nil))
					u.Notify("unpairing all phones…")
				} else {
					u.Notify("not connected — enable remote first")
				}
			},
			Quit: cancel,
		})
	}

	go pk.Run(ctx)

	// Throttled, non-blocking check that lights up the UI's update banner.
	startUpdateCheck(ctx, u)

	if u != nil {
		u.Run(ctx) // owns the terminal; returns when ctx is cancelled / user quits
	} else {
		<-ctx.Done()
	}
	log.Printf("aqua: shut down")
}

// superviseRemote runs client.Run under a cancelable sub-context, started or
// stopped by booleans on cmds. Stopping cancels the sub-context (the relay loop
// returns and stops reconnecting); starting spins a fresh loop.
func superviseRemote(ctx context.Context, client *relay.Client, cmds <-chan bool, initial bool) {
	var cancel context.CancelFunc
	start := func() {
		if cancel != nil {
			return
		}
		var sub context.Context
		sub, cancel = context.WithCancel(ctx)
		go client.Run(sub)
	}
	stop := func() {
		if cancel == nil {
			return
		}
		cancel()
		cancel = nil
	}
	if initial {
		start()
	}
	for {
		select {
		case <-ctx.Done():
			stop()
			return
		case on := <-cmds:
			if on {
				start()
			} else {
				stop()
			}
		}
	}
}

// openLogFile opens %APPDATA%\Aqua\aqua.log for append, or returns nil.
func openLogFile() *os.File {
	dir, err := config.Dir()
	if err != nil {
		return nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil
	}
	f, err := os.OpenFile(filepath.Join(dir, "aqua.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil
	}
	return f
}

// httpBase converts a ws/wss relay base into its http/https origin for the
// pairing URL the phone opens.
func httpBase(workerURL string) string {
	switch {
	case strings.HasPrefix(workerURL, "wss://"):
		return "https://" + strings.TrimPrefix(workerURL, "wss://")
	case strings.HasPrefix(workerURL, "ws://"):
		return "http://" + strings.TrimPrefix(workerURL, "ws://")
	default:
		return workerURL
	}
}
