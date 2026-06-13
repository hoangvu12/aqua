package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"aqua/internal/updater"
	"aqua/internal/version"
)

// runUpdate is the `-update` mode: check the manifest and, if a newer build is
// offered, download/verify/install it over the running aqua.exe. It owns stdout
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
	fmt.Printf("Updated to %s. Restart aqua.exe to run the new version.\n", m.Version)
	return 0
}

// autoUpdate is the on-launch update step for interactive runs. It checks for a
// newer signed release and, if one exists, installs it and relaunches into the
// new binary in the same console — so opening an old aqua.exe transparently
// becomes the latest. Returns relaunched=true when a replacement process has
// been started and the caller should exit. If an update exists but can't be
// installed, it returns the offered version so the UI can show a banner
// pointing at `aqua.exe -update`. Disabled with AQUA_NO_UPDATE_CHECK; silent
// when offline or already current.
func autoUpdate() (relaunched bool, failedVersion string, mandatory bool) {
	if os.Getenv("AQUA_NO_UPDATE_CHECK") != "" {
		return false, "", false
	}

	// Short timeout so an offline launch isn't held up for long.
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	av, err := updater.Check(ctx)
	if err != nil || av == nil {
		return false, "", false // offline or already up to date — just start
	}

	fmt.Printf("Aqua: new version %s available — updating from %s…\n", av.Manifest.Version, version.Version)
	ictx, icancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer icancel()
	if err := updater.Apply(ictx, av.Manifest); err != nil {
		fmt.Fprintf(os.Stderr, "Aqua: auto-update failed (%v); staying on %s\n", err, version.Version)
		return false, av.Manifest.Version, av.Mandatory
	}

	// Installed. Relaunch into the new binary (same path, now replaced).
	if exe, err := os.Executable(); err == nil {
		fmt.Printf("Aqua: updated to %s — restarting…\n", av.Manifest.Version)
		cmd := exec.Command(exe, os.Args[1:]...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := cmd.Start(); err == nil {
			return true, "", false
		}
	}
	// Couldn't relaunch: the on-disk binary is already updated, so keep running
	// this (still-loaded) version for now; the next launch is the new one.
	fmt.Printf("Aqua: updated to %s — it will take effect next launch.\n", av.Manifest.Version)
	return false, "", false
}
