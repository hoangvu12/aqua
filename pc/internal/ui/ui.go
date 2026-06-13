// Package ui is Aqua's console UI (Phase 6). It owns the terminal: it renders
// the pairing QR (terminal half-blocks), the pair URL + 8-char code, a live
// status panel (remote on/off, relay connection, game state), and handles
// single-key actions (r = toggle remote, u = unpair all phones, q = quit).
//
// It is deliberately a plain console UI, not a GUI: that keeps Aqua a single
// static Aqua.exe with no CGO / C toolchain / GL libraries (Fyne was dropped for
// exactly that reason). Rendering is a full-screen redraw on every change via
// ANSI escapes; key input uses raw mode (golang.org/x/term). Because the UI owns
// stdout, the rest of the app routes its logs to a file — see cmd/aqua/main.go.
package ui

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/mdp/qrterminal/v3"
	"golang.org/x/term"
)

// Actions are the callbacks the key handler invokes. Any may be nil.
type Actions struct {
	ToggleRemote func() // r
	UnpairAll    func() // u
	Quit         func() // q / Ctrl-C
}

// Model is everything the UI renders. Mutated only through UI's setters.
type Model struct {
	DeviceID string

	RemoteEnabled bool
	Relay         string // off | connecting | connected | authenticated | error
	GameState     string // offline | menus | … | ingame (from the picker)

	PairCode    string
	PairURL     string
	PairExpires time.Time

	Notice    string // transient toast (e.g. "all phones unpaired")
	noticeGen int

	UpdateVersion   string // non-empty when a newer Aqua.exe is available
	UpdateMandatory bool   // running build is below the manifest's min_version
}

// UI renders Model to the terminal and dispatches keypresses to Actions.
type UI struct {
	mu      sync.Mutex
	m       Model
	qr      string // cached QR block for the current PairURL
	dirty   chan struct{}
	actions Actions
	out     io.Writer // where frames are written (os.Stdout; a buffer in tests)
}

// New builds a UI for a device. baseURL is informational only; the pair URL is
// supplied later via SetPairCode once the relay mints a code.
func New(deviceID string, actions Actions) *UI {
	return &UI{
		m:       Model{DeviceID: deviceID, Relay: "off", GameState: "offline"},
		dirty:   make(chan struct{}, 1),
		actions: actions,
		out:     os.Stdout,
	}
}

// SetActions replaces the key-action callbacks. Used when the callbacks can only
// be built after the UI exists (they close over it). Safe for concurrent use.
func (u *UI) SetActions(a Actions) {
	u.mu.Lock()
	u.actions = a
	u.mu.Unlock()
}

func (u *UI) act() Actions {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.actions
}

// ---- setters (safe for concurrent use) -----------------------------------

func (u *UI) SetRemoteEnabled(b bool) { u.set(func(m *Model) { m.RemoteEnabled = b }) }
func (u *UI) SetRelay(status string)  { u.set(func(m *Model) { m.Relay = status }) }
func (u *UI) SetGameState(s string)   { u.set(func(m *Model) { m.GameState = s }) }

// SetUpdateAvailable surfaces a persistent banner advertising a newer Aqua.exe.
// Pass an empty version to clear it.
func (u *UI) SetUpdateAvailable(version string, mandatory bool) {
	u.set(func(m *Model) { m.UpdateVersion, m.UpdateMandatory = version, mandatory })
}

// SetPairCode records a freshly minted code + the URL the phone opens, and
// pre-renders its QR. Clears when code is empty.
func (u *UI) SetPairCode(code, url string, expires time.Time) {
	qr := ""
	if url != "" {
		qr = renderQR(url)
	}
	u.set(func(m *Model) {
		m.PairCode, m.PairURL, m.PairExpires = code, url, expires
		u.qr = qr
	})
}

// ClearPairCode drops the current pairing (e.g. on disconnect or unpair).
func (u *UI) ClearPairCode() {
	u.set(func(m *Model) {
		m.PairCode, m.PairURL, m.PairExpires = "", "", time.Time{}
		u.qr = ""
	})
}

// Notify shows a transient message line that auto-clears after a few seconds.
func (u *UI) Notify(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	var gen int
	u.set(func(m *Model) {
		m.noticeGen++
		gen = m.noticeGen
		m.Notice = msg
	})
	go func() {
		time.Sleep(4 * time.Second)
		u.set(func(m *Model) {
			if m.noticeGen == gen { // not superseded
				m.Notice = ""
			}
		})
	}()
}

func (u *UI) set(mut func(*Model)) {
	u.mu.Lock()
	mut(&u.m)
	u.mu.Unlock()
	select {
	case u.dirty <- struct{}{}:
	default:
	}
}

// ---- run loop ------------------------------------------------------------

// Run takes over the terminal until ctx is cancelled or the user quits. It
// renders on every model change and reads keys in raw mode. Returns once the
// terminal is restored.
func (u *UI) Run(ctx context.Context) {
	enableVT() // Windows: turn on ANSI processing for stdout
	fd := int(os.Stdin.Fd())

	var restore func()
	if term.IsTerminal(fd) {
		if old, err := term.MakeRaw(fd); err == nil {
			restore = func() { _ = term.Restore(fd, old) }
		}
	}
	// Hide cursor while we own the screen; restore on exit.
	fmt.Print("\x1b[?25l")
	defer func() {
		fmt.Print("\x1b[?25h\x1b[0m\r\n")
		if restore != nil {
			restore()
		}
	}()

	go u.readKeys(ctx)

	u.render()
	for {
		select {
		case <-ctx.Done():
			return
		case <-u.dirty:
			u.render()
		}
	}
}

// readKeys reads single keypresses in raw mode and dispatches actions. In raw
// mode Ctrl-C arrives as byte 0x03 (not a signal), so we handle it as quit.
func (u *UI) readKeys(ctx context.Context) {
	buf := make([]byte, 8)
	for ctx.Err() == nil {
		n, err := os.Stdin.Read(buf)
		if err != nil {
			return
		}
		a := u.act()
		for _, b := range buf[:n] {
			switch b {
			case 'r', 'R':
				if a.ToggleRemote != nil {
					a.ToggleRemote()
				}
			case 'u', 'U':
				if a.UnpairAll != nil {
					a.UnpairAll()
				}
			case 'q', 'Q', 0x03, 0x04: // q, Ctrl-C, Ctrl-D
				if a.Quit != nil {
					a.Quit()
				}
				return
			}
		}
	}
}

// ---- rendering -----------------------------------------------------------

const (
	cReset  = "\x1b[0m"
	cDim    = "\x1b[90m"
	cBold   = "\x1b[1m"
	cAccent = "\x1b[38;5;203m" // ≈ VALORANT red
	cGreen  = "\x1b[38;5;78m"
	cYellow = "\x1b[38;5;221m"
	cRed    = "\x1b[38;5;203m"
)

func (u *UI) render() {
	u.mu.Lock()
	m := u.m
	qr := u.qr
	u.mu.Unlock()

	var b bytes.Buffer
	// Home + clear screen + clear scrollback.
	b.WriteString("\x1b[H\x1b[2J\x1b[3J")

	line(&b, cBold+cAccent+"  AQUA"+cReset+cDim+"  ·  Remote VALORANT Agent Picker"+cReset)
	line(&b, "")

	if m.UpdateVersion != "" {
		label := "Update available"
		if m.UpdateMandatory {
			label = "Required update"
		}
		line(&b, "  "+cAccent+"▲ "+label+": "+cBold+m.UpdateVersion+cReset+
			cDim+"   quit and run "+cReset+cBold+"Aqua.exe -update"+cReset)
		line(&b, "")
	}

	if !m.RemoteEnabled {
		line(&b, "  "+cYellow+"Remote control is OFF"+cReset)
		line(&b, "  "+cDim+"Press "+cReset+cBold+"r"+cReset+cDim+" to enable remote control from your phone."+cReset)
	} else if m.PairCode == "" {
		line(&b, "  "+cDim+"Connecting to relay and minting a pair code…"+cReset)
	} else {
		// QR block (already ANSI half-blocks), indented two spaces per line.
		for _, ln := range strings.Split(strings.TrimRight(qr, "\n"), "\n") {
			line(&b, "  "+ln)
		}
		line(&b, "")
		line(&b, "  "+cDim+"Scan to pair, or open on your phone:"+cReset)
		line(&b, "  "+cBold+m.PairURL+cReset)
		line(&b, "  "+cDim+"Code:"+cReset+"  "+cBold+cAccent+spaceCode(m.PairCode)+cReset+
			"   "+cDim+expiryHint(m.PairExpires)+cReset)
	}

	line(&b, "")
	line(&b, "  "+cDim+strings.Repeat("─", 44)+cReset)
	line(&b, "  Remote   "+remoteBadge(m.RemoteEnabled))
	line(&b, "  Relay    "+relayBadge(m.Relay))
	line(&b, "  Game     "+gameBadge(m.GameState))
	if m.Notice != "" {
		line(&b, "")
		line(&b, "  "+cGreen+"› "+m.Notice+cReset)
	}
	line(&b, "")
	line(&b, "  "+cDim+"["+cReset+"r"+cDim+"] toggle remote   ["+cReset+"u"+cDim+
		"] unpair phones   ["+cReset+"q"+cDim+"] quit"+cReset)

	u.out.Write(b.Bytes())
}

// line writes s followed by a CRLF (raw mode needs explicit \r).
func line(b *bytes.Buffer, s string) {
	b.WriteString(s)
	b.WriteString("\r\n")
}

func remoteBadge(on bool) string {
	if on {
		return cGreen + "ON" + cReset
	}
	return cDim + "off" + cReset
}

func relayBadge(s string) string {
	switch s {
	case "connected", "authenticated":
		return cGreen + s + cReset
	case "connecting":
		return cYellow + s + cReset
	case "error":
		return cRed + s + cReset
	default:
		return cDim + "off" + cReset
	}
}

func gameBadge(s string) string {
	if s == "" || s == "offline" {
		return cDim + "offline" + cReset + cDim + "  (start VALORANT)" + cReset
	}
	if s == "error" {
		return cRed + "error" + cReset
	}
	return cBold + s + cReset
}

// spaceCode renders ABCD2345 as "ABCD 2345" for easier reading aloud/typing.
func spaceCode(code string) string {
	if len(code) == 8 {
		return code[:4] + " " + code[4:]
	}
	return code
}

func expiryHint(exp time.Time) string {
	if exp.IsZero() {
		return ""
	}
	d := time.Until(exp).Round(time.Minute)
	if d <= 0 {
		return "(expired — press r twice to refresh)"
	}
	return fmt.Sprintf("(valid ~%dm)", int(d.Minutes()))
}

// renderQR produces a compact half-block QR for the given text.
func renderQR(text string) string {
	var buf bytes.Buffer
	qrterminal.GenerateWithConfig(text, qrterminal.Config{
		Level:      qrterminal.M,
		Writer:     &buf,
		HalfBlocks: true,
		BlackChar:  qrterminal.BLACK_BLACK,
		WhiteChar:  qrterminal.WHITE_WHITE,
		QuietZone:  1,
	})
	return buf.String()
}
