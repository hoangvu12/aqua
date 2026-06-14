package riot

import "testing"

// Frame shapes mirror techchrism/valorant-websocket-logger output: a 3-element
// [opcode, eventName, {uri, eventType, data}] array, opcode 8 for events.
func TestParseEvent(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantOK  bool
		wantURI string
		wantTyp string
	}{
		{
			name:    "presence update",
			raw:     `[8,"OnJsonApiEvent",{"uri":"/chat/v4/presences","eventType":"Update","data":{"presences":[]}}]`,
			wantOK:  true,
			wantURI: "/chat/v4/presences",
			wantTyp: "Update",
		},
		{
			name:    "messaging service",
			raw:     `[8,"OnJsonApiEvent",{"uri":"/riot-messaging-service/v1/message","eventType":"Create","data":{}}]`,
			wantOK:  true,
			wantURI: "/riot-messaging-service/v1/message",
			wantTyp: "Create",
		},
		{name: "subscribe ack opcode", raw: `[5,"OnJsonApiEvent"]`, wantOK: false},
		{name: "empty frame", raw: ``, wantOK: false},
		{name: "not an array", raw: `{"x":1}`, wantOK: false},
		{name: "wrong arity", raw: `[8,"OnJsonApiEvent"]`, wantOK: false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ev, ok := parseEvent([]byte(c.raw))
			if ok != c.wantOK {
				t.Fatalf("ok = %v, want %v", ok, c.wantOK)
			}
			if !ok {
				return
			}
			if ev.URI != c.wantURI || ev.Type != c.wantTyp {
				t.Fatalf("got {%q %q}, want {%q %q}", ev.URI, ev.Type, c.wantURI, c.wantTyp)
			}
		})
	}
}

func TestIsMatchRelevant(t *testing.T) {
	relevant := []string{
		"/social/v1/presences", // current client (verified live via /help)
		"/chat/v4/presences",   // 2022 clients — must stay matched (version-agnostic)
		"/riot-messaging-service/v1/message",
	}
	for _, uri := range relevant {
		if !IsMatchRelevant(uri) {
			t.Errorf("IsMatchRelevant(%q) = false, want true", uri)
		}
	}
	ignored := []string{
		"/chat/v6/conversations",
		"/chat/v5/messages", // DM traffic, not the messaging service — must not match
		"/riot-messaging-service/v1/out-of-sync",
		"/voice-chat/v3/sessions",
		"/product-session/v1/session-heartbeats/abc",
		"",
	}
	for _, uri := range ignored {
		if IsMatchRelevant(uri) {
			t.Errorf("IsMatchRelevant(%q) = true, want false", uri)
		}
	}
}
