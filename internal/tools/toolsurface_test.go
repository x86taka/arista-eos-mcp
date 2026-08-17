package tools

import (
	"context"
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// This file lints the whole surface a client sees — every tool definition and
// every prompt — rather than the show catalog alone. A tool is only callable if
// its name is well formed, its input schema is valid JSON Schema the client can
// validate against, and the guidance the model reads names tools that exist.

// mcpNameRe is the shape both this server and its clients expect of a tool or
// prompt name.
var mcpNameRe = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)

// resolveSchema round-trips a tool's InputSchema through JSON — which is how a
// client receives it — and resolves it as JSON Schema. Anything a client would
// choke on fails here.
func resolveSchema(t *testing.T, tool *mcp.Tool) (*jsonschema.Schema, *jsonschema.Resolved) {
	t.Helper()

	raw, err := json.Marshal(tool.InputSchema)
	if err != nil {
		t.Fatalf("%s: input schema does not marshal: %v", tool.Name, err)
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("%s: input schema is not a JSON Schema document: %v", tool.Name, err)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Fatalf("%s: input schema does not resolve: %v", tool.Name, err)
	}
	return &schema, resolved
}

// Names are what a model types back to call a tool, so they must be unique and
// uniform. Descriptions are the only thing telling it when to.
func TestToolNamesAndDescriptions(t *testing.T) {
	seen := make(map[string]bool)
	for _, tool := range listTools(t) {
		if !mcpNameRe.MatchString(tool.Name) {
			t.Errorf("tool name %q is not lowercase snake_case", tool.Name)
		}
		if len(tool.Name) > 64 {
			t.Errorf("tool name %q is %d chars, want <= 64", tool.Name, len(tool.Name))
		}
		if seen[tool.Name] {
			t.Errorf("duplicate tool name %q", tool.Name)
		}
		seen[tool.Name] = true

		if len(strings.TrimSpace(tool.Description)) < 20 {
			t.Errorf("%s: description is too short to route on: %q", tool.Name, tool.Description)
		}
	}
	if len(seen) == 0 {
		t.Fatal("no tools were registered; the check is not doing anything")
	}
}

// Every tool's input schema must be one a client can actually validate
// arguments against: a resolvable object schema, closed to extra properties,
// with every required field declared and every field described.
func TestToolInputSchemasAreValid(t *testing.T) {
	for _, tool := range listTools(t) {
		t.Run(tool.Name, func(t *testing.T) {
			schema, _ := resolveSchema(t, tool)

			if schema.Type != "object" {
				t.Errorf("input schema type is %q, want object", schema.Type)
			}
			if schema.AdditionalProperties == nil {
				t.Error("input schema does not close additionalProperties, so a misspelled argument would be silently ignored")
			}
			for _, req := range schema.Required {
				if _, ok := schema.Properties[req]; !ok {
					t.Errorf("%q is required but is not among the declared properties", req)
				}
			}
			for name, prop := range schema.Properties {
				if !mcpNameRe.MatchString(name) {
					t.Errorf("property %q is not lowercase snake_case", name)
				}
				if strings.TrimSpace(prop.Description) == "" {
					t.Errorf("property %q has no description, so the model has to guess what to pass", name)
				}
			}
		})
	}
}

// A rejected argument is only useful if the model can fix it from the error, so
// prove the schemas validate the way the client will apply them.
func TestToolSchemasRejectBadArguments(t *testing.T) {
	tools := make(map[string]*mcp.Tool)
	for _, tool := range listTools(t) {
		tools[tool.Name] = tool
	}

	showData, ok := tools["get_show_data"]
	if !ok {
		t.Fatal("get_show_data is not registered")
	}
	_, resolved := resolveSchema(t, showData)

	for _, spec := range showCatalog {
		if err := resolved.Validate(map[string]any{"topic": spec.topic}); err != nil {
			t.Errorf("catalog topic %q fails its own schema: %v", spec.topic, err)
		}
	}
	if err := resolved.Validate(map[string]any{"topic": "not_a_topic"}); err == nil {
		t.Error("schema accepted a topic that is not in the catalog")
	}
	if err := resolved.Validate(map[string]any{}); err == nil {
		t.Error("schema accepted a call with no topic")
	}
	if err := resolved.Validate(map[string]any{"topic": "version", "typo": "x"}); err == nil {
		t.Error("schema accepted an undeclared argument")
	}
}

// knownNames collects everything a description may legitimately name: tools,
// prompts, their arguments, and catalog topics.
func knownNames(t *testing.T) (known map[string]bool, prefixes map[string]bool) {
	t.Helper()

	known = make(map[string]bool)
	// prefixes holds the first segment of every tool and prompt name. A
	// snake_case word starting with one of these is taken to be a reference to
	// a tool or prompt, and must therefore resolve to one.
	prefixes = make(map[string]bool)

	addName := func(name string) {
		known[name] = true
		prefixes[strings.SplitN(name, "_", 2)[0]] = true
	}

	for _, tool := range listTools(t) {
		addName(tool.Name)
		schema, _ := resolveSchema(t, tool)
		for prop := range schema.Properties {
			known[prop] = true
		}
	}
	for _, prompt := range listPrompts(t) {
		addName(prompt.Name)
		for _, arg := range prompt.Arguments {
			known[arg.Name] = true
		}
	}
	for _, spec := range showCatalog {
		known[spec.topic] = true
	}
	return known, prefixes
}

// snakeWordRe matches snake_case words, including the wildcard form used in
// prose ("fleet_*").
var snakeWordRe = regexp.MustCompile(`[a-z][a-z0-9]*(?:_[a-z0-9]+)*_?\*?`)

// toolRefs splits the tool-shaped words in text into those that name something
// this server registers and those that name nothing. Returning both lets the
// tests assert that references are actually being found, so a regex that
// stopped matching cannot make the check pass vacuously.
func toolRefs(text string, known, prefixes map[string]bool) (found, unknown []string) {
	for _, word := range snakeWordRe.FindAllString(text, -1) {
		wildcard := strings.HasSuffix(word, "*")
		stem := strings.TrimSuffix(strings.TrimSuffix(word, "*"), "_")
		if !strings.Contains(stem, "_") && !wildcard {
			continue // a plain word, not a tool reference
		}
		if !prefixes[strings.SplitN(stem, "_", 2)[0]] {
			continue // does not start like any tool or prompt name
		}
		resolves := known[stem]
		if wildcard {
			resolves = false
			for name := range known {
				if strings.HasPrefix(name, stem+"_") {
					resolves = true
					break
				}
			}
		}
		if resolves {
			found = append(found, word)
		} else {
			unknown = append(unknown, word)
		}
	}
	sort.Strings(found)
	sort.Strings(unknown)
	return found, unknown
}

// Descriptions and prompt bodies steer the model toward other tools by name. A
// renamed tool that leaves a stale reference behind sends the model to call
// something that does not exist, which it cannot recover from.
func TestGuidanceOnlyNamesRegisteredTools(t *testing.T) {
	known, prefixes := knownNames(t)

	// The server instructions are the first thing the model reads and name more
	// tools than any single description.
	found, bad := toolRefs(ServerInstructions, known, prefixes)
	if len(bad) > 0 {
		t.Errorf("ServerInstructions names unregistered tools %v", bad)
	}
	refs := len(found)

	for _, tool := range listTools(t) {
		found, bad := toolRefs(tool.Description, known, prefixes)
		refs += len(found)
		if len(bad) > 0 {
			t.Errorf("%s: description names unregistered tools %v", tool.Name, bad)
		}
	}
	for _, prompt := range listPrompts(t) {
		found, bad := toolRefs(prompt.Description, known, prefixes)
		refs += len(found)
		if len(bad) > 0 {
			t.Errorf("prompt %s: description names unregistered tools %v", prompt.Name, bad)
		}
	}

	// The descriptions cross-reference each other heavily; finding none would
	// mean the extraction broke rather than that everything is clean.
	if refs < 5 {
		t.Fatalf("only %d tool references were found across all descriptions; the check is not doing anything", refs)
	}
	t.Logf("checked %d tool references across tool and prompt descriptions", refs)
}

// The prompt bodies are where tool names are most easily orphaned: they are
// built at Get time, so nothing else in the build sees them.
func TestPromptBodiesOnlyNameRegisteredTools(t *testing.T) {
	known, prefixes := knownNames(t)
	cs := testSession(t)

	bodies, refs := 0, 0
	for _, prompt := range listPrompts(t) {
		args := make(map[string]string, len(prompt.Arguments))
		for _, arg := range prompt.Arguments {
			args[arg.Name] = "spine1"
		}
		res, err := cs.GetPrompt(context.Background(), &mcp.GetPromptParams{Name: prompt.Name, Arguments: args})
		if err != nil {
			t.Errorf("prompt %s: %v", prompt.Name, err)
			continue
		}
		for _, msg := range res.Messages {
			tc, ok := msg.Content.(*mcp.TextContent)
			if !ok {
				continue
			}
			bodies++
			found, bad := toolRefs(tc.Text, known, prefixes)
			refs += len(found)
			if len(bad) > 0 {
				t.Errorf("prompt %s: body names unregistered tools %v\n%s", prompt.Name, bad, tc.Text)
			}
		}
	}
	if bodies == 0 {
		t.Fatal("no prompt bodies were checked; the check is not doing anything")
	}
	// Every prompt body exists to steer the model at named tools.
	if refs < bodies {
		t.Fatalf("found %d tool references across %d prompt bodies; the extraction is not working", refs, bodies)
	}
	t.Logf("checked %d tool references across %d prompt bodies", refs, bodies)
}

// listPrompts returns the prompt definitions exactly as a client receives them.
func listPrompts(t *testing.T) []*mcp.Prompt {
	t.Helper()

	var out []*mcp.Prompt
	for prompt, err := range testSession(t).Prompts(context.Background(), nil) {
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, prompt)
	}
	return out
}
