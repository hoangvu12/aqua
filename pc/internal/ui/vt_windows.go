//go:build windows

package ui

import (
	"os"

	"golang.org/x/sys/windows"
)

// enableVT turns on ENABLE_VIRTUAL_TERMINAL_PROCESSING for stdout so our ANSI
// escapes (colors, cursor moves, the half-block QR) render on Windows. Modern
// Windows Terminal / conhost support it; this just makes sure it is on. Failures
// are non-fatal — worst case the output has escape codes but still works in a
// VT-capable terminal.
func enableVT() {
	h := windows.Handle(os.Stdout.Fd())
	var mode uint32
	if err := windows.GetConsoleMode(h, &mode); err != nil {
		return
	}
	_ = windows.SetConsoleMode(h, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING)
}
