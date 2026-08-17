package tools

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The golden file is the model-visible surface of this server — the
// instructions, every tool definition, every prompt — captured in one document
// that lives in the repository. Its purpose is review, not just regression:
// adding a topic to the catalog or reworking a description shows up as a diff a
// reviewer can read, in the same pull request as the code change, instead of
// being a change nobody sees until a model behaves differently.
//
// Regenerate with:
//
//	go test ./internal/tools/ -run TestToolSurfaceGolden -update

var update = flag.Bool("update", false, "rewrite the golden files instead of comparing against them")

const goldenPath = "testdata/tool-surface.golden.json"

// toolSurface is the serialized form of everything a client is told about.
type toolSurface struct {
	Instructions string            `json:"instructions"`
	Tools        []json.RawMessage `json:"tools"`
	Prompts      []json.RawMessage `json:"prompts"`
}

func captureToolSurface(t *testing.T) []byte {
	t.Helper()

	surface := toolSurface{Instructions: ServerInstructions}

	tools := listTools(t)
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	for _, tool := range tools {
		b, err := json.Marshal(tool)
		if err != nil {
			t.Fatalf("%s: %v", tool.Name, err)
		}
		surface.Tools = append(surface.Tools, b)
	}

	prompts := listPrompts(t)
	sort.Slice(prompts, func(i, j int) bool { return prompts[i].Name < prompts[j].Name })
	for _, prompt := range prompts {
		b, err := json.Marshal(prompt)
		if err != nil {
			t.Fatalf("%s: %v", prompt.Name, err)
		}
		surface.Prompts = append(surface.Prompts, b)
	}

	// Indent the assembled document so the file diffs line by line.
	raw, err := json.Marshal(surface)
	if err != nil {
		t.Fatal(err)
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, raw, "", "  "); err != nil {
		t.Fatal(err)
	}
	pretty.WriteByte('\n')
	return pretty.Bytes()
}

func TestToolSurfaceGolden(t *testing.T) {
	got := captureToolSurface(t)

	if *update {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s (%d bytes)", goldenPath, len(got))
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("%v\nrun: go test ./internal/tools/ -run TestToolSurfaceGolden -update", err)
	}
	if bytes.Equal(got, want) {
		return
	}

	t.Errorf("the tool surface changed but %s was not updated.\n"+
		"If the change is intended, run:\n"+
		"  go test ./internal/tools/ -run TestToolSurfaceGolden -update\n"+
		"and review the diff — it is exactly what every model will now see.\n\n%s",
		goldenPath, lineDiff(string(want), string(got)))
}

// lineDiff reports differing lines in a form that stays readable in CI logs.
// The documents are large, so only the first few differences are shown in full
// along with a count of the rest.
func lineDiff(want, got string) string {
	wantLines := strings.Split(want, "\n")
	gotLines := strings.Split(got, "\n")

	const maxShown = 10
	var b strings.Builder
	shown, differing := 0, 0
	for i := 0; i < len(wantLines) || i < len(gotLines); i++ {
		w := lineAt(wantLines, i)
		g := lineAt(gotLines, i)
		if w == g {
			continue
		}
		differing++
		if shown < maxShown {
			shown++
			fmt.Fprintf(&b, "line %d:\n  golden: %s\n  actual: %s\n", i+1, truncate(w), truncate(g))
		}
	}
	if differing > shown {
		fmt.Fprintf(&b, "... and %d more differing lines\n", differing-shown)
	}
	return b.String()
}

func lineAt(lines []string, i int) string {
	if i < len(lines) {
		return lines[i]
	}
	return ""
}

func truncate(s string) string {
	const max = 160
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
