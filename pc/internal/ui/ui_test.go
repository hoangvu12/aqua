package ui

import (
	"bytes"
	"regexp"
	"strings"
	"testing"
	"time"
)

var ansi = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)

// renderTo captures a single rendered frame with ANSI escapes stripped.
func renderTo(u *UI) string {
	var buf bytes.Buffer
	u.out = &buf
	u.render()
	return ansi.ReplaceAllString(buf.String(), "")
}

func TestRenderRemoteOff(t *testing.T) {
	u := New("dev", Actions{})
	u.SetRemoteEnabled(false)
	out := renderTo(u)
	if !strings.Contains(out, "Remote control is OFF") {
		t.Fatalf("expected OFF hint, got:\n%s", out)
	}
	if !strings.Contains(out, "Remote   off") {
		t.Fatalf("expected remote badge off, got:\n%s", out)
	}
}

func TestRenderPairScreen(t *testing.T) {
	u := New("devbox", Actions{})
	u.SetRemoteEnabled(true)
	u.SetRelay("authenticated")
	u.SetGameState("menus")
	exp := time.Now().Add(5 * time.Minute)
	u.SetPairCode("ABCD2345", "https://aqua.nguyenvu.dev/?code=ABCD2345&device=devbox", exp)

	out := renderTo(u)
	for _, want := range []string{
		"https://aqua.nguyenvu.dev/?code=ABCD2345&device=devbox", // URL
		"ABCD 2345",         // spaced code
		"Scan to pair",      // QR caption
		"Relay    authenticated",
		"Game     menus",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("pair screen missing %q, got:\n%s", want, out)
		}
	}
	// The QR block must have rendered some half-block glyphs.
	if !strings.ContainsAny(out, "▀▄█") {
		t.Fatalf("expected QR half-blocks in output:\n%s", out)
	}
}

func TestNotifyAndClear(t *testing.T) {
	u := New("dev", Actions{})
	u.SetRemoteEnabled(true)
	u.SetPairCode("ABCD2345", "https://x/?code=ABCD2345&device=dev", time.Now().Add(time.Minute))
	u.Notify("all phones unpaired")
	if out := renderTo(u); !strings.Contains(out, "all phones unpaired") {
		t.Fatalf("notice not shown:\n%s", out)
	}
	u.ClearPairCode()
	out := renderTo(u)
	if strings.Contains(out, "ABCD 2345") {
		t.Fatalf("pair code should be cleared:\n%s", out)
	}
}

func TestSpaceCode(t *testing.T) {
	if got := spaceCode("ABCD2345"); got != "ABCD 2345" {
		t.Fatalf("spaceCode = %q", got)
	}
	if got := spaceCode("SHORT"); got != "SHORT" {
		t.Fatalf("spaceCode passthrough = %q", got)
	}
}
