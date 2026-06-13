package updater

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// checkState is the small bit of bookkeeping persisted between runs so the
// startup update check is throttled (and so we remember a found version even if
// the next launch is offline).
type checkState struct {
	LastCheck     int64  `json:"last_check_unix"`
	LatestVersion string `json:"latest_version"`
}

func statePath(dir string) string { return filepath.Join(dir, "update-check.json") }

func loadState(dir string) checkState {
	var s checkState
	if data, err := os.ReadFile(statePath(dir)); err == nil {
		_ = json.Unmarshal(data, &s)
	}
	return s
}

func saveState(dir string, s checkState) {
	if data, err := json.Marshal(s); err == nil {
		_ = os.WriteFile(statePath(dir), data, 0o600)
	}
}

// DueForCheck reports whether at least `every` has elapsed since the last
// recorded check (true on first run / unreadable state). `now` is passed in so
// callers/tests stay deterministic.
func DueForCheck(dir string, every time.Duration, now time.Time) bool {
	last := loadState(dir).LastCheck
	if last == 0 {
		return true
	}
	return now.Sub(time.Unix(last, 0)) >= every
}

// RecordCheck stamps the time of a completed check and the version it saw
// (empty string if up to date), so the throttle and the "remembered" banner
// survive restarts.
func RecordCheck(dir string, latestVersion string, now time.Time) {
	saveState(dir, checkState{LastCheck: now.Unix(), LatestVersion: latestVersion})
}

// CleanupOldBinary removes the <exe>.old left behind by a previous in-place
// update (Windows can't delete the running .exe during the swap, so we sweep it
// on the next launch). Best-effort; a still-locked file is simply skipped.
func CleanupOldBinary() {
	if exe, err := os.Executable(); err == nil {
		_ = os.Remove(exe + ".old")
	}
}
