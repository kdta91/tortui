package httpx

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// TestSchemeRuleComparesThePreviousHop pins T-9168 (Backlog T-930, DEC-182):
// a hop to http is refused when the hop before it was https, not only when
// the chain started at https. http -> https is still followed (DEC-064); what
// is refused is dropping back to cleartext after the server upgraded.
func TestSchemeRuleComparesThePreviousHop(t *testing.T) {
	t.Parallel()

	mustParse := func(raw string) *url.URL {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatalf("parse %q: %v", raw, err)
		}

		return u
	}

	cases := []struct {
		name  string
		chain []string
		to    string
		want  error
	}{
		{name: "http, https, back to http is refused", chain: []string{"http://feed.example.org/a", "https://feed.example.org/b"}, to: "http://feed.example.org/c", want: ErrInsecureRedirect},
		{name: "http, https, https stays followed", chain: []string{"http://feed.example.org/a", "https://feed.example.org/b"}, to: "https://feed.example.org/c"},
		{name: "http, http, https is followed", chain: []string{"http://feed.example.org/a", "http://feed.example.org/b"}, to: "https://feed.example.org/c"},
		{name: "http, http, http is followed", chain: []string{"http://feed.example.org/a", "http://feed.example.org/b"}, to: "http://feed.example.org/c"},
		{name: "previous hop scheme case is ignored", chain: []string{"http://feed.example.org/a", "HTTPS://feed.example.org/b"}, to: "http://feed.example.org/c", want: ErrInsecureRedirect},
	}

	rules := map[string]func(*http.Request, []*http.Request) error{
		"strict":    checkRedirect,
		"subdomain": checkRedirectToSubdomain,
	}

	for _, tc := range cases {
		for ruleName, rule := range rules {
			t.Run(ruleName+"/"+tc.name, func(t *testing.T) {
				t.Parallel()

				via := make([]*http.Request, len(tc.chain))
				for i, raw := range tc.chain {
					via[i] = &http.Request{URL: mustParse(raw)}
				}

				err := rule(&http.Request{URL: mustParse(tc.to)}, via)
				if tc.want == nil {
					if err != nil {
						t.Fatalf("%v -> %s = %v, want nil", tc.chain, tc.to, err)
					}

					return
				}

				if !errors.Is(err, tc.want) {
					t.Fatalf("%v -> %s = %v, want %v", tc.chain, tc.to, err, tc.want)
				}
			})
		}
	}
}

// TestUpgradeThenDowngradeChainSendsNoCleartextRequest drives the chain
// through net/http: the http request is upgraded to https, and the https
// answer points back at http with the api_key still in the query. The
// cleartext second request must never be made.
func TestUpgradeThenDowngradeChainSendsNoCleartextRequest(t *testing.T) {
	t.Parallel()

	var requested []string

	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requested = append(requested, r.URL.Scheme)

		next := *r.URL
		if r.URL.Scheme == "http" {
			next.Scheme = "https"
		} else {
			next.Scheme = "http"
		}

		return &http.Response{
			StatusCode: http.StatusFound,
			Status:     "302 Found",
			Header:     http.Header{"Location": []string{next.String()}},
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    r,
		}, nil
	})

	client := New(Config{MinHostInterval: -1, Transport: transport, Credentials: testCredentials()})

	_, err := client.Get(testContext(t), "http://feed.example.org/api", nil)
	if !errors.Is(err, ErrInsecureRedirect) {
		t.Fatalf("error = %v, want ErrInsecureRedirect", err)
	}

	if got := strings.Join(requested, ","); got != "http,https" {
		t.Fatalf("schemes requested = %s, want http,https and no cleartext request after the upgrade", got)
	}

	assertNoCredentialLeak(t, "the refusal", err.Error())
}
