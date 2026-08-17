package tools

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/x86taka/arista-eos-mcp/internal/config"
	"github.com/x86taka/arista-eos-mcp/internal/manager"
)

// closedPort returns a loopback port with nothing listening on it, so a tool
// handler that does reach the network fails immediately with connection
// refused instead of waiting out a dial timeout.
func closedPort(t *testing.T) int {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	return port
}

// testSession wires a client to a fully registered server over an in-memory
// transport. No device is contacted unless a tool handler actually runs.
func testSession(t *testing.T) *mcp.ClientSession {
	t.Helper()

	mgr, err := manager.New(&config.Config{
		Devices: []config.Device{
			{Name: "spine1", Host: "127.0.0.1", Username: "admin", Password: "devpass", EAPITransport: "http", EAPIPort: closedPort(t)},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	server := mcp.NewServer(&mcp.Implementation{Name: "test-server", Version: "test"}, nil)
	Register(server, mgr)

	ctx := context.Background()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	ss, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ss.Close() })

	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "test"}, nil).
		Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

// listTools returns the tool definitions exactly as a client receives them.
func listTools(t *testing.T) []*mcp.Tool {
	t.Helper()

	var out []*mcp.Tool
	for tool, err := range testSession(t).Tools(context.Background(), nil) {
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, tool)
	}
	return out
}

// The whole point of collapsing the show catalog was the size of tools/list,
// which every client puts in the model's context. Before the collapse this was
// 74 tools and roughly 40k bytes; guard against drifting back.
func TestToolsListSize(t *testing.T) {
	tools := listTools(t)

	total := 0
	for _, tool := range tools {
		b, err := json.Marshal(tool)
		if err != nil {
			t.Fatal(err)
		}
		total += len(b)
	}
	t.Logf("tools/list: %d tools, %d bytes", len(tools), total)

	if want := 18; len(tools) != want {
		t.Errorf("got %d tools, want %d", len(tools), want)
	}
	if max := 20000; total > max {
		t.Errorf("tools/list payload is %d bytes, want <= %d", total, max)
	}
}

// callFailure returns the failure text of a tool call, from either a transport
// error or an IsError result. It fails the test if the call succeeded.
//
// The deadline keeps the test quick: a topic that passes validation reaches the
// handler, which then tries to dial an address that will never answer.
func callFailure(t *testing.T, cs *mcp.ClientSession, args map[string]any) string {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "get_show_data", Arguments: args})
	if err != nil {
		return err.Error()
	}
	if !res.IsError {
		t.Fatalf("call with %v unexpectedly succeeded", args)
	}
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

// topic is a hand-written enum rather than a reflected struct tag, so prove the
// SDK actually enforces it: a bad topic must be rejected before any dispatch,
// and a good one must get through to the (unreachable, in tests) device.
func TestShowDataEnforcesTopicEnum(t *testing.T) {
	cs := testSession(t)

	bad := callFailure(t, cs, map[string]any{"topic": "definitely_not_a_topic"})
	if !strings.Contains(bad, "topic") {
		t.Errorf("rejection did not mention the topic field: %s", bad)
	}
	// The validator echoes the permitted values, which is what lets a model
	// correct itself in the same turn.
	if !strings.Contains(bad, "bgp_summary") {
		t.Errorf("rejection did not list the valid topics: %s", bad)
	}

	// A valid topic passes validation and dispatches, so it fails on the dial
	// rather than on the schema — the failure must not name the topic at all.
	good := callFailure(t, cs, map[string]any{"topic": "version"})
	if strings.Contains(good, "unknown topic") || strings.Contains(good, "does not equal any of") {
		t.Errorf("valid topic \"version\" was rejected: %s", good)
	}
}

// The device blurb is duplicated across nine struct tags that cannot reference
// shortDeviceDesc, so check the generated schemas rather than the source.
func TestDeviceDescriptionConsistent(t *testing.T) {
	checked := 0
	for _, tool := range listTools(t) {
		var schema struct {
			Properties map[string]struct {
				Description string `json:"description"`
			} `json:"properties"`
		}
		b, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(b, &schema); err != nil {
			t.Fatal(err)
		}

		prop, ok := schema.Properties["device"]
		if !ok {
			continue
		}
		checked++
		if prop.Description != shortDeviceDesc {
			t.Errorf("%s: device description drifted:\n got %q\nwant %q", tool.Name, prop.Description, shortDeviceDesc)
		}
	}
	if checked == 0 {
		t.Fatal("no tool exposed a device property; the check is not doing anything")
	}
	t.Logf("checked %d tools with a device property", checked)
}
