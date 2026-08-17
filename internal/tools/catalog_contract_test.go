package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/x86taka/arista-eos-mcp/internal/config"
	"github.com/x86taka/arista-eos-mcp/internal/manager"
)

// This file is the contract check between the show catalog and the wire: every
// topic a client can pass must survive schema validation, dispatch, and the
// read-only safety guard, and must arrive at the device as exactly the command
// and encoding the catalog declares. No device is needed — a fake eAPI endpoint
// records each request. A typo in a command, a topic missing from the dispatch
// map, or an encoding the safety guard rejects fails here rather than on a
// customer's switch.

// eapiRequest is the part of the eAPI JSON-RPC request the fake device reads.
// It mirrors goeapi's Request/Parameters types.
type eapiRequest struct {
	Method string `json:"method"`
	ID     string `json:"id"`
	Params struct {
		Version int    `json:"version"`
		Cmds    []any  `json:"cmds"`
		Format  string `json:"format"`
	} `json:"params"`
}

// commands returns the requested commands as strings. goeapi always prepends an
// "enable" step, which is either a bare string or, when an enable password is
// configured, a {"cmd":"enable","input":...} object; both forms are flattened.
func (r eapiRequest) commands() []string {
	out := make([]string, 0, len(r.Params.Cmds))
	for _, c := range r.Params.Cmds {
		switch v := c.(type) {
		case string:
			out = append(out, v)
		case map[string]any:
			if s, ok := v["cmd"].(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

// fakeEAPI is an httptest server that speaks just enough eAPI to record what a
// tool put on the wire and hand back a well-formed response.
type fakeEAPI struct {
	server *httptest.Server

	mu   sync.Mutex
	reqs []eapiRequest
}

func newFakeEAPI(t *testing.T) *fakeEAPI {
	t.Helper()

	f := &fakeEAPI{}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/command-api" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		var req eapiRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		f.reqs = append(f.reqs, req)
		f.mu.Unlock()

		// eAPI returns one result per command, including the enable step that
		// goeapi prepends and then pops off the response.
		results := make([]map[string]any, len(req.Params.Cmds))
		for i := range results {
			if req.Params.Format == "text" {
				results[i] = map[string]any{"output": "fake text output\n"}
			} else {
				results[i] = map[string]any{"fakeField": "fake value"}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"result":  results,
		}); err != nil {
			t.Errorf("fake eAPI failed to write response: %v", err)
		}
	}))
	t.Cleanup(f.server.Close)
	return f
}

// hostPort splits the fake server's address for config.Device.
func (f *fakeEAPI) hostPort(t *testing.T) (string, int) {
	t.Helper()

	u, err := url.Parse(f.server.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	return u.Hostname(), port
}

func (f *fakeEAPI) reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reqs = nil
}

// soleRequest returns the one request recorded since the last reset. A tool
// that never reached the wire, or that quietly fanned out into several eAPI
// round trips, fails here.
func (f *fakeEAPI) soleRequest(t *testing.T) eapiRequest {
	t.Helper()

	f.mu.Lock()
	defer f.mu.Unlock()
	switch len(f.reqs) {
	case 1:
		return f.reqs[0]
	case 0:
		t.Fatal("no eAPI request was made")
	default:
		t.Fatalf("%d eAPI requests were made, want exactly 1", len(f.reqs))
	}
	return eapiRequest{}
}

// fakeSession wires a client to a fully registered server whose single device
// points at a fake eAPI endpoint. ReadOnly is on, so every command a tool sends
// also has to pass the safety guard.
//
// The session is returned warm: the first tool call against a device makes the
// manager create its eAPI client, and goeapi.Connect probes the device with its
// own "show version" before the tool's command goes out. Warming up here and
// clearing the recording keeps that probe out of every caller's assertions.
func fakeSession(t *testing.T) (*mcp.ClientSession, *fakeEAPI) {
	t.Helper()

	fake := newFakeEAPI(t)
	host, port := fake.hostPort(t)

	mgr, err := manager.New(&config.Config{
		ReadOnly: true,
		Devices: []config.Device{
			{Name: "spine1", Host: host, Username: "admin", Password: "devpass", EAPITransport: "http", EAPIPort: port},
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

	callOK(t, cs, "get_show_data", map[string]any{"topic": "version"})
	fake.reset()
	return cs, fake
}

// callOK calls a tool the way a client does and fails on any error, whether it
// comes back as a transport error or as an IsError result.
func callOK(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) string {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s%v: %v", name, args, err)
	}
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	if res.IsError {
		t.Fatalf("%s%v returned an error result: %s", name, args, b.String())
	}
	return b.String()
}

// Every topic must be callable end to end and must send the command and
// encoding its catalog entry declares — nothing else.
func TestCatalogTopicsReachEAPI(t *testing.T) {
	cs, fake := fakeSession(t)

	for _, spec := range showCatalog {
		t.Run(spec.topic, func(t *testing.T) {
			fake.reset()

			out := callOK(t, cs, "get_show_data", map[string]any{"topic": spec.topic})

			req := fake.soleRequest(t)
			if req.Method != "runCmds" {
				t.Errorf("eAPI method is %q, want runCmds", req.Method)
			}
			if req.Params.Format != spec.encoding {
				t.Errorf("encoding on the wire is %q, catalog declares %q", req.Params.Format, spec.encoding)
			}

			// goeapi prepends "enable"; the tool's own command is what follows.
			cmds := req.commands()
			if len(cmds) != 2 {
				t.Fatalf("sent %d commands %v, want the enable step plus exactly one command", len(cmds), cmds)
			}
			if cmds[1] != spec.command {
				t.Errorf("command on the wire is %q, catalog declares %q", cmds[1], spec.command)
			}

			// The result is rendered by formatResults, which heads each block
			// with the command — proof the response round-tripped as well.
			if !strings.Contains(out, "## "+spec.command) {
				t.Errorf("output does not report the command it ran:\n%s", out)
			}
			if spec.encoding == "text" && !strings.Contains(out, "fake text output") {
				t.Errorf("text topic did not render the device output:\n%s", out)
			}
			if spec.encoding == "json" && !strings.Contains(out, "fakeField") {
				t.Errorf("json topic did not render the device output:\n%s", out)
			}
		})
	}
}

// The device argument must be honoured as well as omitted: a catalog topic
// addressed to a named device has to reach that device.
func TestCatalogTopicHonoursDeviceArgument(t *testing.T) {
	cs, fake := fakeSession(t)
	fake.reset()

	callOK(t, cs, "get_show_data", map[string]any{"topic": "version", "device": "spine1"})

	if cmds := fake.soleRequest(t).commands(); len(cmds) != 2 || cmds[1] != "show version" {
		t.Errorf("explicit device changed what was sent: %v", cmds)
	}
}

// get_ip_route is the one show tool left outside the catalog because it builds
// its command from arguments — the place a wrong command is easiest to ship.
func TestIPRouteBuildsCommand(t *testing.T) {
	cs, fake := fakeSession(t)

	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{"bare", map[string]any{}, "show ip route"},
		{"prefix", map[string]any{"prefix": "10.0.0.0/24"}, "show ip route 10.0.0.0/24"},
		{"vrf", map[string]any{"vrf": "MGMT"}, "show ip route vrf MGMT"},
		{"vrf and prefix", map[string]any{"vrf": "MGMT", "prefix": "10.0.0.0/24"}, "show ip route vrf MGMT 10.0.0.0/24"},
		{"whitespace is trimmed", map[string]any{"prefix": "  10.0.0.0/24  "}, "show ip route 10.0.0.0/24"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake.reset()

			callOK(t, cs, "get_ip_route", tc.args)

			cmds := fake.soleRequest(t).commands()
			if len(cmds) != 2 {
				t.Fatalf("sent %d commands %v, want the enable step plus exactly one command", len(cmds), cmds)
			}
			if cmds[1] != tc.want {
				t.Errorf("command on the wire is %q, want %q", cmds[1], tc.want)
			}
		})
	}
}

// A stale dispatch map would make a topic in the enum fail at call time with
// "unknown topic". The table-driven test above would catch it, but this states
// the invariant directly against the handler's own lookup.
func TestEveryEnumValueDispatches(t *testing.T) {
	schema := buildShowDataSchema()
	for _, v := range schema.Properties["topic"].Enum {
		topic, ok := v.(string)
		if !ok {
			t.Fatalf("enum value %v is %T, want string", v, v)
		}
		spec, ok := showTopics[topic]
		if !ok {
			t.Errorf("enum offers topic %q but the dispatch map has no entry for it", topic)
			continue
		}
		if spec.command == "" {
			t.Errorf("topic %q dispatches to an empty command", topic)
		}
	}
}

// Reject a call the same way a device would if the argument shape drifted: a
// topic the enum does not list must never reach the wire.
func TestUnknownTopicNeverReachesEAPI(t *testing.T) {
	cs, fake := fakeSession(t)
	fake.reset()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_show_data",
		Arguments: map[string]any{"topic": "show running-config"},
	})
	if err == nil && !res.IsError {
		t.Fatal("an unlisted topic was accepted")
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.reqs) != 0 {
		t.Errorf("a rejected topic still reached the device: %v", fake.reqs)
	}
}

// Sanity check on the fake itself: if it ever stopped recording, or if reset
// stopped clearing, every assertion above would pass vacuously.
func TestFakeEAPIRecordsRequests(t *testing.T) {
	cs, fake := fakeSession(t)

	fake.mu.Lock()
	pending := len(fake.reqs)
	fake.mu.Unlock()
	if pending != 0 {
		t.Fatalf("session was handed back with %d requests still recorded", pending)
	}

	callOK(t, cs, "get_show_data", map[string]any{"topic": "version"})

	req := fake.soleRequest(t)
	if got := fmt.Sprint(req.commands()); !strings.Contains(got, "show version") {
		t.Fatalf("recorded request does not contain the command: %s", got)
	}
}
