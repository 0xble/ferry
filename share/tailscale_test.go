package share

import "testing"

func TestShareProxyMatchesPort(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		proxy     string
		port      int
		shouldHit bool
	}{
		{
			name:      "localhost proxy",
			proxy:     "http://127.0.0.1:39124",
			port:      39124,
			shouldHit: true,
		},
		{
			name:      "path suffix still matches",
			proxy:     "http://127.0.0.1:39124/foo",
			port:      39124,
			shouldHit: true,
		},
		{
			name:      "different port",
			proxy:     "http://127.0.0.1:39125",
			port:      39124,
			shouldHit: false,
		},
		{
			name:      "invalid url",
			proxy:     "not a url",
			port:      39124,
			shouldHit: false,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := shareProxyMatchesPort(tc.proxy, tc.port); got != tc.shouldHit {
				t.Fatalf("shareProxyMatchesPort(%q, %d) = %v, want %v", tc.proxy, tc.port, got, tc.shouldHit)
			}
		})
	}
}
