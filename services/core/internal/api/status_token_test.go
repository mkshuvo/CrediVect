package api

import (
	"net/http"
	"net/url"
	"testing"
)

// The credential is header-only. A status token that reaches a URL is already in
// access logs, browser history and referrer headers, so the query string is not
// an accepted carrier even though it is the more convenient one.
func TestPresentedTokenIgnoresTheQueryString(t *testing.T) {
	r, err := http.NewRequest(http.MethodGet, "http://core/api/v1/applications/get?id=1&token=from-query", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := presentedToken(r); got != "" {
		t.Errorf("query token presented as %q, want it ignored", got)
	}

	r.Header.Set("X-Status-Token", "from-header")
	if got := presentedToken(r); got != "from-header" {
		t.Errorf("header = %q, want the header value", got)
	}
}

func TestPresentedTokenTrimsAndTreatsBlanksAsAbsent(t *testing.T) {
	for _, raw := range []string{"", "   ", "\t\n"} {
		u := &url.URL{Path: "/api/v1/applications/get", RawQuery: "id=1"}
		r := &http.Request{Method: http.MethodGet, URL: u, Header: http.Header{}}
		r.Header.Set("X-Status-Token", raw)
		if got := presentedToken(r); got != "" {
			t.Errorf("token %q presented as %q, want absent", raw, got)
		}
	}
}
