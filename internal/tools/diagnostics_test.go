package tools

import "testing"

func TestDiffLines(t *testing.T) {
	a := "hostname leaf1\nvlan 10\nvlan 20\n"
	b := "hostname leaf2\nvlan 10\nvlan 30\n"
	out := diffLines("leaf1", a, "leaf2", b)

	for _, want := range []string{
		"Only in leaf1", "hostname leaf1", "vlan 20",
		"Only in leaf2", "hostname leaf2", "vlan 30",
	} {
		if !contains(out, want) {
			t.Errorf("diff output missing %q\n--- output ---\n%s", want, out)
		}
	}
	if contains(out, "vlan 10") {
		t.Errorf("shared line 'vlan 10' should not appear in diff\n%s", out)
	}
}

func TestDiffLinesIdentical(t *testing.T) {
	s := "hostname x\nvlan 10\n"
	out := diffLines("a", s, "b", s)
	if !contains(out, "No differences") {
		t.Errorf("expected no-difference message, got:\n%s", out)
	}
}

func contains(haystack, needle string) bool {
	return len(needle) == 0 || (len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
