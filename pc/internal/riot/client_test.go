package riot

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestDoErrorClassification locks in how HTTP outcomes map to sentinels — in
// particular that an expired-token 400 BAD_CLAIMS is treated as unauthorized so
// the picker re-authenticates instead of looping on a dead token.
func TestDoErrorClassification(t *testing.T) {
	badClaims := `{"httpStatus":400,"errorCode":"BAD_CLAIMS","message":"Failure validating/decoding RSO Access Token"}`
	cases := []struct {
		name    string
		status  int
		body    string
		want    error // sentinel to match with errors.Is, or nil for "generic non-sentinel"
		generic bool  // expect a non-nil error that is neither ErrNotFound nor ErrUnauthorized
	}{
		{name: "expired token 400 BAD_CLAIMS", status: 400, body: badClaims, want: ErrUnauthorized},
		{name: "401", status: 401, want: ErrUnauthorized},
		{name: "403", status: 403, want: ErrUnauthorized},
		{name: "404", status: 404, want: ErrNotFound},
		{name: "other 400", status: 400, body: `{"errorCode":"BAD_REQUEST"}`, generic: true},
		{name: "500", status: 500, body: "boom", generic: true},
		{name: "200 ok", status: 200, body: `{}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			req, _ := http.NewRequestWithContext(context.Background(), "GET", srv.URL, nil)
			err := do(srv.Client(), req, nil)

			switch {
			case tc.generic:
				if err == nil || errors.Is(err, ErrUnauthorized) || errors.Is(err, ErrNotFound) {
					t.Fatalf("want generic error, got %v", err)
				}
			case tc.want != nil:
				if !errors.Is(err, tc.want) {
					t.Fatalf("want %v, got %v", tc.want, err)
				}
			default: // 200
				if err != nil {
					t.Fatalf("want nil, got %v", err)
				}
			}
		})
	}
}
