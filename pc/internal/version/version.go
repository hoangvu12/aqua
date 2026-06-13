// Package version holds Aqua's build version and the public key that authorizes
// auto-update binaries. Both are read by internal/updater.
package version

// Version is the running build's version, injected at link time:
//
//	go build -ldflags "-X aqua/internal/version.Version=v1.2.3" ./cmd/aqua
//
// Un-versioned local builds report "dev", which the updater treats as "older
// than any release" so a real build always shows as an available update.
var Version = "dev"

// PublicKey is the minisign public key the updater verifies every downloaded
// binary against. The matching private key lives only in CI (the
// AQUA_SIGNING_KEY repo secret) and the maintainer's offline backup — never in
// this repo. Generated with `go run ./cmd/aquasign keygen`. Rotating it means
// shipping a new binary with the new key, so do it rarely.
const PublicKey = "RWQls4N64CzUGnqx4NoI9uYdVIQUefoKqxdne8owsET2Tefohy/+y3dH"
