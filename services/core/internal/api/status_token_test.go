package api

import (
	"net/http"
	"net/url"
	"testing"
)

func TestPresentedTokenPrefersTheHeader(t *testing.T) {
	r, err := http.NewRequest(http.MethodGet, "http://core/api/v1/applications/get?id=1&token=from-query", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := presentedToken(r); got != "from-query" {
		t.Errorf("query only = %q, want the query value", got)
	}

	r.Header.Set("X-Status-Token", "from-header")
	if got := presentedToken(r); got != "from-header" {
		t.Errorf("both present = %q, want the header so the fetch path can keep the token out of URLs", got)
	}
}

func TestPresentedTokenTrimsAndTreatsBlanksAsAbsent(t *testing.T) {
	for _, raw := range []string{"", "   ", "\t\n"} {
		u := &url.URL{Path: "/api/v1/applications/get", RawQuery: "token=" + url.QueryEscape(raw)}
		r := &http.Request{Method: http.MethodGet, URL: u, Header: http.Header{}}
		if got := presentedToken(r); got != "" {
			t.Errorf("token %q presented as %q, want absent", raw, got)
		}
	}
}
