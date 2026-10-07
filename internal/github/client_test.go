package github

import "testing"

func TestLinkRel(t *testing.T) {
	h := `<https://api.github.com/x?page=2>; rel="next", <https://api.github.com/x?page=5>; rel="last"`
	if got := linkRel(h, "next"); got != "https://api.github.com/x?page=2" {
		t.Errorf("next: %q", got)
	}
	if got := linkRel(h, "last"); got != "https://api.github.com/x?page=5" {
		t.Errorf("last: %q", got)
	}
	if got := linkRel("", "next"); got != "" {
		t.Errorf("empty: %q", got)
	}
}

func TestHTMLURL(t *testing.T) {
	tests := map[string]string{
		"https://api.github.com/repos/o/r/pulls/1":     "https://github.com/o/r/pull/1",
		"https://api.github.com/repos/o/r/commits/abc": "https://github.com/o/r/commit/abc",
		"https://api.github.com/repos/o/r/issues/2":    "https://github.com/o/r/issues/2",
		"https://api.github.com/repos/o/pulls/pulls/3": "https://github.com/o/pulls/pull/3",
		"https://api.github.com/repos/o/r":             "https://github.com/o/r",
		"https://example.com/not-the-api":              "https://example.com/not-the-api",
	}
	for in, want := range tests {
		if got := htmlURL(in); got != want {
			t.Errorf("htmlURL(%q) = %q, want %q", in, got, want)
		}
	}
}
