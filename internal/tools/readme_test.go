package tools

import (
	"os"
	"sort"
	"strings"
	"testing"
)

// The README documents every get_show_data topic and the command behind it, by
// hand, in a table of 57 rows. That table is how a human decides whether a
// topic already exists before adding one, so letting it drift from the catalog
// is how duplicate and mis-documented topics get added. Nothing else in the
// build reads it.

const readmePath = "../../README.md"

// readmeTopicTable parses the "| Topic | Command |" table out of the README and
// returns it as topic -> command.
func readmeTopicTable(t *testing.T) map[string]string {
	t.Helper()

	data, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(data), "\n")

	start := -1
	for i, line := range lines {
		if strings.HasPrefix(line, "| Topic") && strings.Contains(line, "Command") {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatalf("%s has no topic table; if it moved, update this test", readmePath)
	}

	table := make(map[string]string)
	for _, line := range lines[start+2:] { // skip the header and its separator
		if !strings.HasPrefix(line, "|") {
			break
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		if len(cells) < 2 {
			t.Fatalf("cannot parse README table row: %s", line)
		}
		topic := strings.Trim(strings.TrimSpace(cells[0]), "`")
		command := strings.Trim(strings.TrimSpace(cells[1]), "`")
		if _, dup := table[topic]; dup {
			t.Errorf("README lists topic %q twice", topic)
		}
		table[topic] = command
	}
	return table
}

func TestREADMEDocumentsTheCatalog(t *testing.T) {
	table := readmeTopicTable(t)

	if len(table) == 0 {
		t.Fatal("parsed no rows out of the README topic table; the check is not doing anything")
	}

	documented := make(map[string]bool, len(showCatalog))
	for _, spec := range showCatalog {
		documented[spec.topic] = true
		command, ok := table[spec.topic]
		if !ok {
			t.Errorf("topic %q is in the catalog but not in the README table", spec.topic)
			continue
		}
		if command != spec.command {
			t.Errorf("README documents topic %q as running %q; the catalog runs %q", spec.topic, command, spec.command)
		}
	}

	var stale []string
	for topic := range table {
		if !documented[topic] {
			stale = append(stale, topic)
		}
	}
	sort.Strings(stale)
	for _, topic := range stale {
		t.Errorf("README documents topic %q, which the catalog does not offer", topic)
	}
}
