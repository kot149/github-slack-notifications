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
