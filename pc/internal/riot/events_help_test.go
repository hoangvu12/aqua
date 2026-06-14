package riot

import (
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"sort"
	"strings"
	"testing"
	"time"
)

// TestLiveHelp queries the running Riot Client's /help endpoint, which lists every
// event the client can emit. This reveals the *current* resource URI versions
// (e.g. whether presence is chat/v4, v5, or v6) and whether pregame/coregame/party
// have local events — without waiting for one to fire. Gated like the WS test.
//
//	AQUA_LIVE_WS=1 go -C pc test ./internal/riot -run TestLiveHelp -v -count=1
func TestLiveHelp(t *testing.T) {
	if os.Getenv("AQUA_LIVE_WS") != "1" {
		t.Skip("set AQUA_LIVE_WS=1 with VALORANT running to query the live /help endpoint")
	}
	lf, err := ReadLockfile()
	if err != nil {
		t.Skipf("Riot Client not running: %v", err)
	}

	hc := &http.Client{
		Timeout:   8 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
	}
	req, _ := http.NewRequest("GET", "https://127.0.0.1:"+lf.Port+"/help", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("riot:"+lf.Password)))
	resp, err := hc.Do(req)
	if err != nil {
		t.Fatalf("GET /help: %v", err)
	}
	defer resp.Body.Close()

	var help struct {
		Events map[string]json.RawMessage `json:"events"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&help); err != nil {
		t.Fatalf("decode /help: %v", err)
	}

	names := make([]string, 0, len(help.Events))
	for n := range help.Events {
		names = append(names, n)
	}
	sort.Strings(names)

	keywords := []string{"presence", "chat", "messaging", "pregame", "core-game", "coregame", "party", "session"}
	t.Logf("total events: %d. interesting ones:", len(names))
	for _, n := range names {
		low := strings.ToLower(n)
		for _, k := range keywords {
			if strings.Contains(low, k) {
				t.Logf("  %s", n)
				break
			}
		}
	}
}
