package github

import "testing"

func TestCITitle(t *testing.T) {
	m := ciTitle.FindStringSubmatch("CI workflow run failed for feature/x branch")
	if m == nil || m[1] != "CI" || m[2] != "failed" || m[3] != "feature/x" {
		t.Errorf("got %q", m)
	}
}
