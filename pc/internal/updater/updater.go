// Package updater self-updates Aqua.exe. It fetches a small JSON manifest
// straight from the latest GitHub Release (no API call — the stable
// releases/latest/download/<asset> redirect, so there's no rate limit and no
// server of our own in the path). When a newer version is offered it downloads
// the binary from the manifest URL, verifies it against the embedded minisign
// public key (internal/version.PublicKey) plus a SHA-256 checksum, and
// atomically replaces the running executable via github.com/minio/selfupdate
// (which handles Windows' "can't overwrite a running .exe" by renaming in
// place, with automatic rollback on failure).
//
// Only the PC binary self-updates: the phone SPA and the Worker are server-side
// and refresh on their own. See the repo's auto-update notes.
package updater

import (
	"bytes"
	"context"
	"crypto"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"aqua/internal/version"

	"github.com/minio/selfupdate"
)

// Platform identifies the build; the manifest path and asset names key off it.
// Aqua ships windows/amd64 only, so this is a constant rather than runtime.GOOS.
const Platform = "windows-amd64"

// repo is the GitHub repo releases are published to.
const repo = "hoangvu12/aqua"

// ManifestURL is where the update manifest lives. GitHub's
// releases/latest/download/<asset> path is a stable redirect to the newest
// release's asset (served by the CDN, not the rate-limited API), so the client
// can read it directly with no server of ours in the loop. It's a var so tests
// can point it at a local file server.
var ManifestURL = "https://github.com/" + repo + "/releases/latest/download/manifest-" + Platform + ".json"

// maxBinary caps an update download (the real binary is ~10MB; this guards
// against a hostile or misconfigured server streaming forever).
const maxBinary = 100 << 20

// Manifest is the update descriptor published as a GitHub Release asset. The
// json tags must stay in sync with cmd/aquasign's manifest writer.
type Manifest struct {
	Version    string `json:"version"`
	MinVersion string `json:"min_version"`
	Platform   string `json:"platform"`
	PubDate    string `json:"pub_date"`
	Critical   bool   `json:"critical"`
	URL        string `json:"url"`
	SigURL     string `json:"sig_url"`
	Size       int64  `json:"size"`
	SHA256     string `json:"sha256"`
	NotesURL   string `json:"notes_url"`
}

// Available describes an offered update. Check returns nil when up to date.
type Available struct {
	Current   string    // the running version
	Manifest  *Manifest // the newer release
	Mandatory bool      // running version is below Manifest.MinVersion
}

// Fetch downloads and decodes the manifest. The source URL can be overridden
// with $AQUA_MANIFEST_URL (for staging a release or pointing at a test server).
func Fetch(ctx context.Context) (*Manifest, error) {
	url := ManifestURL
	if v := os.Getenv("AQUA_MANIFEST_URL"); v != "" {
		url = v
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("manifest: %s", resp.Status)
	}
	var m Manifest
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&m); err != nil {
		return nil, fmt.Errorf("manifest: %w", err)
	}
	if m.Version == "" || m.URL == "" || m.SigURL == "" {
		return nil, errors.New("manifest: missing required fields")
	}
	return &m, nil
}

// Check fetches the manifest and reports an update if the offered version is
// newer than the running one. Returns (nil, nil) when already up to date.
func Check(ctx context.Context) (*Available, error) {
	m, err := Fetch(ctx)
	if err != nil {
		return nil, err
	}
	if Compare(m.Version, version.Version) <= 0 {
		return nil, nil
	}
	return &Available{
		Current:   version.Version,
		Manifest:  m,
		Mandatory: m.MinVersion != "" && Compare(version.Version, m.MinVersion) < 0,
	}, nil
}

// Apply downloads, verifies, and installs the binary described by m, replacing
// the running executable. On success the caller should relaunch Aqua.
func Apply(ctx context.Context, m *Manifest) error {
	bin, err := download(ctx, m.URL)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}

	// 1. Checksum: cheap integrity + tamper tripwire before the signature check.
	if m.SHA256 != "" {
		want, err := hex.DecodeString(m.SHA256)
		if err != nil {
			return fmt.Errorf("manifest sha256: %w", err)
		}
		got := sha256.Sum256(bin)
		if !bytes.Equal(got[:], want) {
			return errors.New("checksum mismatch — refusing to install")
		}
	}

	// 2. Signature: the real authenticity guarantee. The .minisig is fetched
	// from the manifest and verified against the embedded public key, so even a
	// compromised CDN/host cannot ship a binary we'll install.
	verifier := selfupdate.NewVerifier()
	if err := verifier.LoadFromURL(m.SigURL, version.PublicKey, http.DefaultTransport); err != nil {
		return fmt.Errorf("load signature: %w", err)
	}

	sum := sha256.Sum256(bin)
	opts := selfupdate.Options{
		Checksum: sum[:],
		Hash:     crypto.SHA256,
		Verifier: verifier,
	}
	if exe, err := os.Executable(); err == nil {
		opts.OldSavePath = exe + ".old" // deterministic; cleaned on next launch
	}

	if err := selfupdate.Apply(bytes.NewReader(bin), opts); err != nil {
		if rerr := selfupdate.RollbackError(err); rerr != nil {
			return fmt.Errorf("update failed AND rollback failed (%v); restore Aqua.exe manually: %w", rerr, err)
		}
		return fmt.Errorf("install: %w", err)
	}
	return nil
}

func download(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := (&http.Client{Timeout: 2 * time.Minute}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s", resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxBinary))
}
