package middleware

import (
	"crypto/tls"
	"net/http/httptest"
	"testing"
)

func TestCheckOriginEdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		hub            *Hub
		targetURL      string
		hostOverride   string
		origin         string
		xForwardedProto string
		setTLS         bool
		want           bool
	}{
		{
			name:      "nil hub rejects",
			hub:       nil,
			targetURL: "http://example.com/ws",
			origin:    "http://example.com",
			want:      false,
		},
		{
			name:      "missing origin allowed for non-browser clients",
			hub:       NewHub(nil, nil),
			targetURL: "http://example.com/ws",
			origin:    "",
			want:      true,
		},
		{
			name:      "same origin http allowed",
			hub:       NewHub(nil, nil),
			targetURL: "http://example.com/ws",
			origin:    "http://example.com",
			want:      true,
		},
		{
			name:            "same origin via forwarded https allowed",
			hub:             NewHub(nil, nil),
			targetURL:       "http://example.com/ws",
			origin:          "https://example.com",
			xForwardedProto: "https, http",
			want:            true,
		},
		{
			name:      "same origin https via tls allowed",
			hub:       NewHub(nil, nil),
			targetURL: "https://example.com/ws",
			origin:    "https://example.com",
			setTLS:    true,
			want:      true,
		},
		{
			name:      "cross origin rejected when not allowlisted",
			hub:       NewHub(nil, nil),
			targetURL: "https://example.com/ws",
			origin:    "https://evil.example",
			setTLS:    true,
			want:      false,
		},
		{
			name:      "allowlist accepts normalized default https port",
			hub:       NewHub(nil, []string{"https://admin.example.com"}),
			targetURL: "https://example.com/ws",
			origin:    "HTTPS://ADMIN.EXAMPLE.COM:443",
			setTLS:    true,
			want:      true,
		},
		{
			name:      "wildcard allowlist permits any origin",
			hub:       NewHub(nil, []string{"*"}),
			targetURL: "https://example.com/ws",
			origin:    "https://totally-different.example",
			setTLS:    true,
			want:      true,
		},
		{
			name:      "invalid origin rejected",
			hub:       NewHub(nil, nil),
			targetURL: "http://example.com/ws",
			origin:    "not a valid origin",
			want:      false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest("GET", tc.targetURL, nil)
			if tc.hostOverride != "" {
				req.Host = tc.hostOverride
			}
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			if tc.xForwardedProto != "" {
				req.Header.Set("X-Forwarded-Proto", tc.xForwardedProto)
			}
			if tc.setTLS {
				req.TLS = &tls.ConnectionState{}
			}

			got := tc.hub.checkOrigin(req)
			if got != tc.want {
				t.Fatalf("checkOrigin() = %v, want %v", got, tc.want)
			}
		})
	}
}
