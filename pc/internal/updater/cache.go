package updater

import "os"

// CleanupOldBinary removes the <exe>.old left behind by a previous in-place
// update (Windows can't delete the running .exe during the swap, so we sweep it
// on the next launch). Best-effort; a still-locked file is simply skipped.
func CleanupOldBinary() {
	if exe, err := os.Executable(); err == nil {
		_ = os.Remove(exe + ".old")
	}
}
