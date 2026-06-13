// Command aquasign is the maintainer/CI signing tool for Aqua's auto-updater.
// It is NOT shipped to users — only the public key it prints is embedded in
// Aqua (internal/version.PublicKey). Two subcommands:
//
//	aquasign keygen  -out aqua-minisign.key [-password <pw>]
//	    Generate a fresh minisign keypair. Writes the (encrypted) private key
//	    file and prints the public key to embed + the base64 secret for CI.
//
//	aquasign sign  -in Aqua.exe -version v1.2.3 -url <binURL> -sig-url <sigURL>
//	               [-key aqua-minisign.key | -key-b64 <b64>] [-password <pw>]
//	               [-min-version v1.0.0] [-notes-url <url>] [-critical]
//	               [-out-sig Aqua.exe.minisig] [-out-manifest manifest-windows-amd64.json]
//	    Sign the binary (minisign) and emit the .minisig + the JSON update
//	    manifest the Worker serves and the client verifies.
//
// The signature scheme is minisign (ed25519) — the same one
// github.com/minio/selfupdate verifies on the client. Passwords/keys are read
// from flags or, preferably in CI, the AQUA_SIGNING_PASSWORD / AQUA_SIGNING_KEY
// environment variables.
package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"aead.dev/minisign"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "keygen":
		keygen(os.Args[2:])
	case "sign":
		sign(os.Args[2:])
	default:
		usage()
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: aquasign <keygen|sign> [flags]  (see -h on each subcommand)")
	os.Exit(2)
}

func keygen(args []string) {
	fs := newFlagSet("keygen")
	out := fs.String("out", "aqua-minisign.key", "path to write the encrypted private key file")
	password := fs.String("password", envOr("AQUA_SIGNING_PASSWORD", ""), "private-key password (or $AQUA_SIGNING_PASSWORD)")
	_ = fs.Parse(args)

	pub, priv, err := minisign.GenerateKey(rand.Reader)
	check(err)

	enc, err := minisign.EncryptKey(*password, priv)
	check(err)
	check(os.WriteFile(*out, enc, 0o600))

	fmt.Println("Generated minisign keypair.")
	fmt.Printf("  Private key file : %s  (keep secret; password-protected)\n", *out)
	fmt.Println()
	fmt.Println("Embed this public key in pc/internal/version/version.go (PublicKey):")
	fmt.Printf("  %s\n", pub.String())
	fmt.Println()
	fmt.Println("For GitHub Actions, add repo secrets:")
	fmt.Printf("  AQUA_SIGNING_KEY      = %s\n", base64.StdEncoding.EncodeToString(enc))
	fmt.Printf("  AQUA_SIGNING_PASSWORD = %s\n", *password)
}

func sign(args []string) {
	fs := newFlagSet("sign")
	in := fs.String("in", "Aqua.exe", "binary to sign")
	keyFile := fs.String("key", "", "encrypted private key file")
	keyB64 := fs.String("key-b64", envOr("AQUA_SIGNING_KEY", ""), "encrypted private key, base64 (or $AQUA_SIGNING_KEY)")
	password := fs.String("password", envOr("AQUA_SIGNING_PASSWORD", ""), "private-key password (or $AQUA_SIGNING_PASSWORD)")
	version := fs.String("version", "", "release version, e.g. v1.2.3 (required)")
	minVersion := fs.String("min-version", "", "minimum supported version; older clients treat the update as mandatory")
	binURL := fs.String("url", "", "public download URL of the binary (required)")
	sigURL := fs.String("sig-url", "", "public download URL of the .minisig (required)")
	notesURL := fs.String("notes-url", "", "release notes URL")
	critical := fs.Bool("critical", false, "mark this update as critical")
	outSig := fs.String("out-sig", "", "where to write the signature (default <in>.minisig)")
	outManifest := fs.String("out-manifest", "manifest-windows-amd64.json", "where to write the update manifest")
	platform := fs.String("platform", "windows-amd64", "platform identifier recorded in the manifest")
	_ = fs.Parse(args)

	switch {
	case *version == "":
		fatal("missing -version")
	case *binURL == "":
		fatal("missing -url")
	case *sigURL == "":
		fatal("missing -sig-url")
	}

	// Load the private key, from file or base64 env.
	var priv minisign.PrivateKey
	var err error
	switch {
	case *keyFile != "":
		priv, err = minisign.PrivateKeyFromFile(*password, *keyFile)
	case *keyB64 != "":
		var enc []byte
		enc, err = base64.StdEncoding.DecodeString(*keyB64)
		if err == nil {
			priv, err = minisign.DecryptKey(*password, enc)
		}
	default:
		fatal("provide -key or -key-b64 (or $AQUA_SIGNING_KEY)")
	}
	check(err)

	bin, err := os.ReadFile(*in)
	check(err)

	// minisign signature — github.com/minio/selfupdate verifies this on the
	// client against the embedded public key.
	sig := minisign.Sign(priv, bin)
	sigPath := *outSig
	if sigPath == "" {
		sigPath = *in + ".minisig"
	}
	check(os.WriteFile(sigPath, sig, 0o644))

	sum := sha256.Sum256(bin)
	m := manifest{
		Version:    *version,
		MinVersion: *minVersion,
		Platform:   *platform,
		PubDate:    time.Now().UTC().Format(time.RFC3339),
		Critical:   *critical,
		URL:        *binURL,
		SigURL:     *sigURL,
		Size:       int64(len(bin)),
		SHA256:     hex.EncodeToString(sum[:]),
		NotesURL:   *notesURL,
	}
	data, err := json.MarshalIndent(m, "", "  ")
	check(err)
	check(os.WriteFile(*outManifest, append(data, '\n'), 0o644))

	fmt.Printf("signed %s (%d bytes)\n", *in, len(bin))
	fmt.Printf("  signature: %s\n", sigPath)
	fmt.Printf("  manifest : %s\n", *outManifest)
}

// manifest mirrors updater.Manifest on the client side. Keep the json tags in
// sync with pc/internal/updater/updater.go.
type manifest struct {
	Version    string `json:"version"`
	MinVersion string `json:"min_version,omitempty"`
	Platform   string `json:"platform"`
	PubDate    string `json:"pub_date"`
	Critical   bool   `json:"critical"`
	URL        string `json:"url"`
	SigURL     string `json:"sig_url"`
	Size       int64  `json:"size"`
	SHA256     string `json:"sha256"`
	NotesURL   string `json:"notes_url,omitempty"`
}
