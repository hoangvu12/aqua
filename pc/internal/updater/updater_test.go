package updater

import (
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"aead.dev/minisign"
	"github.com/minio/selfupdate"
)

// TestSignVerifyRoundTrip is the load-bearing compatibility check: a signature
// produced the way cmd/aquasign produces it (minisign.Sign) must verify with
// the exact verifier the client uses (selfupdate.Verifier). If this breaks,
// every auto-update would be rejected as tampered.
func TestSignVerifyRoundTrip(t *testing.T) {
	pub, priv, err := minisign.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	bin := []byte("pretend this is Aqua.exe\x00\x01\x02")
	sig := minisign.Sign(priv, bin)

	sigPath := filepath.Join(t.TempDir(), "Aqua.exe.minisig")
	if err := os.WriteFile(sigPath, sig, 0o644); err != nil {
		t.Fatal(err)
	}

	v := selfupdate.NewVerifier()
	if err := v.LoadFromFile(sigPath, pub.String()); err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}
	if err := v.Verify(bin); err != nil {
		t.Fatalf("Verify (good binary): %v", err)
	}
	tampered := append([]byte{}, bin...)
	tampered[0] ^= 0xff
	if err := v.Verify(tampered); err == nil {
		t.Fatal("Verify accepted a tampered binary")
	}
}

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v1.2.3", "v1.2.3", 0},
		{"1.2.3", "v1.2.3", 0},
		{"v1.2.4", "v1.2.3", 1},
		{"v1.3.0", "v1.2.9", 1},
		{"v2.0.0", "v1.9.9", 1},
		{"v1.2.3", "v1.2.4", -1},
		{"v1.2.0", "v1.2", 0},        // missing patch == 0
		{"v1.2.3-beta", "v1.2.3", 0}, // pre-release suffix ignored
		{"dev", "v1.0.0", -1},        // un-versioned build is below any release
		{"v1.0.0", "dev", 1},
		{"dev", "dev", 0},
	}
	for _, c := range cases {
		if got := Compare(c.a, c.b); got != c.want {
			t.Errorf("Compare(%q,%q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestDueForCheck(t *testing.T) {
	dir := t.TempDir()
	now := time.Unix(1_700_000_000, 0)
	if !DueForCheck(dir, 24*time.Hour, now) {
		t.Fatal("first run should be due")
	}
	RecordCheck(dir, "", now)
	if DueForCheck(dir, 24*time.Hour, now.Add(time.Hour)) {
		t.Fatal("should not be due an hour later")
	}
	if !DueForCheck(dir, 24*time.Hour, now.Add(25*time.Hour)) {
		t.Fatal("should be due after the interval")
	}
}
