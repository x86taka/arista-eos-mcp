package tools

import (
	"strings"
	"testing"
)

// The catalog is the single source of truth for the topic enum, the hint table
// and command dispatch, so its entries must be unique and well formed.
func TestShowCatalogTopics(t *testing.T) {
	seen := make(map[string]bool, len(showCatalog))
	for _, spec := range showCatalog {
		if spec.topic == "" {
			t.Errorf("catalog entry for %q has an empty topic", spec.command)
		}
		if seen[spec.topic] {
			t.Errorf("duplicate topic %q", spec.topic)
		}
		seen[spec.topic] = true

		if spec.command == "" {
			t.Errorf("topic %q has no command", spec.topic)
		}
		if spec.encoding != "json" && spec.encoding != "text" {
			t.Errorf("topic %q has encoding %q, want json or text", spec.topic, spec.encoding)
		}
	}
}

// Every catalog entry must be reachable: present in the enum clients validate
// against, and present in the map the handler dispatches through.
func TestShowDataSchemaCoversCatalog(t *testing.T) {
	schema := buildShowDataSchema()
	topic, ok := schema.Properties["topic"]
	if !ok {
		t.Fatal("schema has no topic property")
	}
	if len(topic.Enum) != len(showCatalog) {
		t.Fatalf("enum has %d values, catalog has %d entries", len(topic.Enum), len(showCatalog))
	}

	inEnum := make(map[string]bool, len(topic.Enum))
	for _, v := range topic.Enum {
		s, ok := v.(string)
		if !ok {
			t.Fatalf("enum value %v is %T, want string", v, v)
		}
		inEnum[s] = true
	}
	for _, spec := range showCatalog {
		if !inEnum[spec.topic] {
			t.Errorf("topic %q missing from the enum", spec.topic)
		}
		if _, ok := showTopics[spec.topic]; !ok {
			t.Errorf("topic %q missing from the dispatch map", spec.topic)
		}
	}

	if len(showTopics) != len(showCatalog) {
		t.Errorf("dispatch map has %d entries, catalog has %d", len(showTopics), len(showCatalog))
	}
}

// Hints are only worth their bytes if they describe a topic that exists, and
// the description must stay well under the size the byte budget assumes.
func TestShowDataDescription(t *testing.T) {
	for _, spec := range showCatalog {
		if spec.hint == "" {
			continue
		}
		if !strings.Contains(showDataDescription, "\n"+spec.topic+": "+spec.hint) {
			t.Errorf("hint for topic %q is missing from the description", spec.topic)
		}
	}
	if n := len(showDataDescription); n > 3000 {
		t.Errorf("description is %d chars, want <= 3000 (hints are getting expensive)", n)
	}
}
