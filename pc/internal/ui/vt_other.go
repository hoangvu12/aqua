//go:build !windows

package ui

// enableVT is a no-op off Windows: Unix terminals process ANSI escapes natively.
func enableVT() {}
