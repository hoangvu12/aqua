package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"aqua/internal/config"
	"aqua/internal/ui"
	"aqua/internal/updater"
	"aqua/internal/version"
)

// updateCheckEvery throttles the background startup check so we hit the network
// at most once a day per machine.
const updateCheckEvery = 24 * time.Hour

// runUpdate is the `-update` mode: check the manifest and, if a newer build is
// offered, download/verify/install it over the running Aqua.exe. It owns stdout
// directly (no console UI) and returns a process exit code.
func runUpdate() int {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	fmt.Printf("Aqua %s — checking for updates…\n", version.Version)
	av, err := updater.Check(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "update check failed: %v\n", err)
		return 1
	}
	if av == nil {
		fmt.Printf("Already up to date (%s).\n", version.Version)
		return 0
	}

	m := av.Manifest
	fmt.Printf("Updating %s → %s …\n", version.Version, m.Version)
	if m.NotesURL != "" {
		fmt.Printf("  release notes: %s\n", m.NotesURL)
	}
	if err := updater.Apply(ctx, m); err != nil {
		fmt.Fprintf(os.Stderr, "update failed: %v\n", err)
		return 1
	}
	fmt.Printf("Updated to %s. Restart Aqua.exe to run the new version.\n", m.Version)
	return 0
}

// startUpdateCheck runs a throttled background check and, if a newer build is
// offered, lights up the UI banner. Non-fatal: any error is silently ignored,
// since the app works fine without updating. No-op in headless mode (u == nil)
// or when AQUA_NO_UPDATE_CHECK is set.
func startUpdateCheck(ctx context.Context, u *ui.UI) {
	if u == nil || os.Getenv("AQUA_NO_UPDATE_CHECK") != "" {
		return
	}
	go func() {
		dir, err := config.Dir()
		if err != nil {
			return
		}
		now := time.Now()
		if !updater.DueForCheck(dir, updateCheckEvery, now) {
			return
		}
		cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		av, err := updater.Check(cctx)
		if err != nil {
			return // offline / GitHub unreachable — try again next launch
		}
		latest := ""
		if av != nil {
			latest = av.Manifest.Version
			u.SetUpdateAvailable(av.Manifest.Version, av.Mandatory)
		}
		updater.RecordCheck(dir, latest, now)
	}()
}
