package tools

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/x86taka/arista-eos-mcp/internal/safety"
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

// topicRe is the shape a topic must have. The topic is an enum value a model
// types back verbatim, so keep it to lowercase snake_case: no spaces, no
// punctuation, nothing that needs escaping or invites a near-miss spelling.
var topicRe = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)

func TestShowCatalogTopicNaming(t *testing.T) {
	for _, spec := range showCatalog {
		if !topicRe.MatchString(spec.topic) {
			t.Errorf("topic %q is not lowercase snake_case", spec.topic)
		}
		if len(spec.topic) > 40 {
			t.Errorf("topic %q is %d chars, want <= 40", spec.topic, len(spec.topic))
		}
	}
}

// A catalog entry is only reachable if the read-only guard lets its command
// through. safety is a separate source of truth from the catalog, so a command
// that the guard rejects (or simply does not recognize) is caught here rather
// than at call time on a device.
func TestShowCatalogCommandsPassSafety(t *testing.T) {
	for _, spec := range showCatalog {
		if err := safety.CheckReadOnly(spec.command); err != nil {
			t.Errorf("topic %q: %v", spec.topic, err)
		}
		if !strings.HasPrefix(spec.command, "show ") {
			t.Errorf("topic %q runs %q; curated datasets must be plain show commands", spec.topic, spec.command)
		}
		if spec.command != strings.TrimSpace(spec.command) || strings.Contains(spec.command, "  ") {
			t.Errorf("topic %q has stray whitespace in %q", spec.topic, spec.command)
		}
	}
}

// Two topics running the same command means one of them is a wasted enum slot
// and an ambiguous choice for the model.
func TestShowCatalogCommandsUnique(t *testing.T) {
	byCommand := make(map[string]string, len(showCatalog))
	for _, spec := range showCatalog {
		if first, ok := byCommand[spec.command]; ok {
			t.Errorf("topics %q and %q both run %q", first, spec.topic, spec.command)
			continue
		}
		byCommand[spec.command] = spec.topic
	}
}

// Hints are folded into a single description string, one per line, so a hint
// that contains a newline or a colon-prefixed topic name would corrupt the
// table the model reads.
func TestShowCatalogHintsWellFormed(t *testing.T) {
	for _, spec := range showCatalog {
		if spec.hint == "" {
			continue
		}
		if strings.ContainsAny(spec.hint, "\n\r") {
			t.Errorf("hint for %q spans multiple lines", spec.topic)
		}
		if spec.hint != strings.TrimSpace(spec.hint) {
			t.Errorf("hint for %q has leading or trailing whitespace", spec.topic)
		}
		if len(spec.hint) > 120 {
			t.Errorf("hint for %q is %d chars, want <= 120", spec.topic, len(spec.hint))
		}
	}
}

// The tool definition is sent to every client and cached by prompt-caching
// clients, so it must be byte-identical from one process to the next. Both the
// description and the enum are therefore built by ranging over the catalog
// slice in declared order; ranging over showTopics instead would randomize
// them per process. Rebuilding both here proves the order still comes from the
// slice.
func TestShowDataDefinitionIsDeterministic(t *testing.T) {
	var want strings.Builder
	want.WriteString(showDataDescriptionPreamble)
	for _, spec := range showCatalog {
		if spec.hint == "" {
			continue
		}
		fmt.Fprintf(&want, "\n%s: %s", spec.topic, spec.hint)
	}
	if showDataDescription != want.String() {
		t.Errorf("description is not the catalog rendered in order:\n got %q\nwant %q", showDataDescription, want.String())
	}

	enum := buildShowDataSchema().Properties["topic"].Enum
	if len(enum) != len(showCatalog) {
		t.Fatalf("enum has %d values, catalog has %d entries", len(enum), len(showCatalog))
	}
	for i, spec := range showCatalog {
		if enum[i] != spec.topic {
			t.Errorf("enum[%d] is %v, catalog declares %q at that position", i, enum[i], spec.topic)
		}
	}
}
