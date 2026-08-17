//go:build live

package tools

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/x86taka/arista-eos-mcp/internal/config"
	"github.com/x86taka/arista-eos-mcp/internal/manager"
)

// The offline tests prove every topic dispatches and reaches the wire as the
// command the catalog declares. They cannot prove that command is one EOS
// accepts: the catalog is the only source of truth for the command string, so a
// typo matches itself. Only a device settles it.
//
// This file is behind the "live" build tag and needs a reachable switch:
//
//	EOS_HOST=192.0.2.10 EOS_USERNAME=admin EOS_PASSWORD=… \
//	  go test -tags=live ./internal/tools/ -run TestCatalogAgainstDevice -v
//
// A command EOS rejects as invalid, or that it cannot render in the encoding
// the catalog declares, is a catalog bug and fails the test. Anything else —
// a feature that is simply not configured, a table that is empty — is reported
// but does not fail: that is the device's state, not the catalog's problem.
//
// Commands unsupported on a given platform (MACsec on a switch without it, say)
// can be baselined for that device with:
//
//	EOS_CATALOG_EXPECTED_UNSUPPORTED=macsec,macsec_counters,ptp

// eosRejections are the EOS error fragments that mean "this command is wrong",
// as opposed to "this feature is not in use". EOS reports an unparsable command
// with error code 1002 and a command it cannot render as JSON with 1003; the
// wording below is what those carry through eAPI.
var eosRejections = []string{
	"invalid command",
	"invalid input",
	"incomplete command",
	"unconverted command", // the command has no JSON form on this EOS version
	"unavailable command",
	"unrecognized command",
}

// liveSession connects to the device described by the environment, exactly as
// the real server does, and returns a client session plus the device to aim at.
// The device is named explicitly on every call: an inventory with more than one
// device would otherwise fail every topic on the missing "device" argument and
// prove nothing about the catalog.
func liveSession(t *testing.T) (*mcp.ClientSession, string) {
	t.Helper()

	if os.Getenv("EOS_HOST") == "" && os.Getenv("EOS_CONFIG") == "" {
		t.Skip("no device configured: set EOS_HOST (with EOS_USERNAME/EOS_PASSWORD) or EOS_CONFIG")
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("configuration error: %v", err)
	}
	mgr, err := manager.New(cfg)
	if err != nil {
		t.Fatalf("manager initialization failed: %v", err)
	}

	// EOS_CATALOG_DEVICE picks one out of a larger inventory; otherwise the
	// first configured device is used.
	device := strings.TrimSpace(os.Getenv("EOS_CATALOG_DEVICE"))
	if device == "" {
		names := mgr.Names()
		if len(names) == 0 {
			t.Skip("configuration defines no devices to validate the catalog against")
		}
		device = names[0]
	}
	t.Logf("validating the catalog against device %q", device)

	server := mcp.NewServer(&mcp.Implementation{Name: "live-test", Version: "test"}, nil)
	Register(server, mgr)

	ctx := context.Background()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	ss, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ss.Close() })

	cs, err := mcp.NewClient(&mcp.Implementation{Name: "live-test-client", Version: "test"}, nil).
		Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs, device
}

// expectedUnsupported reads the per-device baseline of topics whose commands
// this platform is known not to support.
func expectedUnsupported() map[string]bool {
	out := map[string]bool{}
	for _, topic := range strings.Split(os.Getenv("EOS_CATALOG_EXPECTED_UNSUPPORTED"), ",") {
		if topic = strings.TrimSpace(topic); topic != "" {
			out[topic] = true
		}
	}
	return out
}

// callResult is the outcome of one topic against the device.
type callResult struct {
	topic string
	err   string // empty when the call succeeded
}

// rejected reports whether the error is EOS refusing the command itself.
func (r callResult) rejected() bool {
	msg := strings.ToLower(r.err)
	for _, fragment := range eosRejections {
		if strings.Contains(msg, fragment) {
			return true
		}
	}
	return false
}

// Every topic in the catalog must name a command this device accepts, and must
// declare an encoding the device can render it in.
func TestCatalogAgainstDevice(t *testing.T) {
	cs, device := liveSession(t)
	baseline := expectedUnsupported()

	var ok, stateErrors []string
	var rejections, unexpectedlySupported []callResult

	for _, spec := range showCatalog {
		res := runTopic(t, cs, device, spec.topic)
		switch {
		case res.err == "":
			ok = append(ok, spec.topic)
			if baseline[spec.topic] {
				unexpectedlySupported = append(unexpectedlySupported, res)
			}
		case res.rejected():
			rejections = append(rejections, res)
		default:
			stateErrors = append(stateErrors, fmt.Sprintf("%s: %s", spec.topic, oneLine(res.err)))
		}
	}

	t.Logf("%d/%d topics returned data", len(ok), len(showCatalog))
	// If nothing at all worked, the device is unreachable or the credentials
	// are wrong. Reporting that as "no catalog problems found" would be worse
	// than useless — the run proved nothing.
	if len(ok) == 0 {
		t.Fatalf("not one topic returned data; the device is unreachable or rejecting authentication, so the catalog was not validated")
	}
	if len(stateErrors) > 0 {
		// Not a catalog problem: the command is valid, the device just had
		// nothing to say (feature disabled, table empty, license absent).
		sort.Strings(stateErrors)
		t.Logf("%d topics errored on device state rather than on the command:\n  %s",
			len(stateErrors), strings.Join(stateErrors, "\n  "))
	}

	for _, res := range rejections {
		if baseline[res.topic] {
			t.Logf("topic %q is unsupported on this platform, as expected: %s", res.topic, oneLine(res.err))
			continue
		}
		t.Errorf("topic %q sends a command this device rejects — the catalog entry is wrong, "+
			"or add it to EOS_CATALOG_EXPECTED_UNSUPPORTED if this platform simply lacks the feature:\n  %s",
			res.topic, oneLine(res.err))
	}
	for _, res := range unexpectedlySupported {
		t.Errorf("topic %q is listed in EOS_CATALOG_EXPECTED_UNSUPPORTED but works on this device; drop it from the baseline", res.topic)
	}
}

// get_ip_route builds its command from arguments, so exercise each shape.
func TestIPRouteAgainstDevice(t *testing.T) {
	cs, device := liveSession(t)

	cases := []struct {
		name string
		args map[string]any
	}{
		{"bare", map[string]any{}},
		{"default vrf", map[string]any{"vrf": "default"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := map[string]any{"device": device}
			for k, v := range tc.args {
				args[k] = v
			}
			res := runTool(t, cs, "get_ip_route", args)
			if res.rejected() {
				t.Errorf("get_ip_route%v sends a command this device rejects:\n  %s", args, oneLine(res.err))
			} else if res.err != "" {
				t.Logf("get_ip_route%v errored on device state: %s", args, oneLine(res.err))
			}
		})
	}
}

func runTopic(t *testing.T, cs *mcp.ClientSession, device, topic string) callResult {
	t.Helper()
	res := runTool(t, cs, "get_show_data", map[string]any{"topic": topic, "device": device})
	res.topic = topic
	return res
}

// runTool calls a tool the way a client does and reports the failure text, if
// any, without failing the test — the caller classifies it.
func runTool(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) callResult {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return callResult{err: err.Error()}
	}
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	if res.IsError {
		return callResult{err: b.String()}
	}
	if strings.TrimSpace(b.String()) == "" {
		return callResult{err: "the device returned an empty response"}
	}
	return callResult{}
}

func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	const max = 200
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}
