package tools

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/x86taka/arista-eos-mcp/internal/manager"
)

// registerOpsTools adds active read-only diagnostics: ping, traceroute, and
// BGP route inspection. These remain within the read-only safety allow-list.
func registerOpsTools(s *mcp.Server, mgr *manager.Manager) {
	// ping_host
	type pingArgs struct {
		Device      string `json:"device,omitempty" jsonschema:"target device, given as its management IPv4 address (e.g. '192.0.2.10'). This is the intended input. A configured device name also works, but do NOT pass a hostname. Optional only when a single device is configured"`
		Destination string `json:"destination" jsonschema:"IP address or hostname to ping"`
		Count       int    `json:"count,omitempty" jsonschema:"number of echo requests (default 5)"`
		VRF         string `json:"vrf,omitempty" jsonschema:"optional VRF name"`
		Source      string `json:"source,omitempty" jsonschema:"optional source interface or address"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "ping_host",
		Description: "Ping a destination FROM a device to test reachability/latency. Use for 'can A reach B', connectivity checks, packet loss. Optional count/vrf/source.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args pingArgs) (*mcp.CallToolResult, any, error) {
		if strings.TrimSpace(args.Destination) == "" {
			return nil, nil, fmt.Errorf("destination is required")
		}
		var cmd strings.Builder
		cmd.WriteString("ping ")
		if v := strings.TrimSpace(args.VRF); v != "" {
			cmd.WriteString("vrf " + v + " ")
		}
		cmd.WriteString(strings.TrimSpace(args.Destination))
		count := args.Count
		if count <= 0 {
			count = 5
		}
		cmd.WriteString(" repeat " + strconv.Itoa(count))
		if src := strings.TrimSpace(args.Source); src != "" {
			cmd.WriteString(" source " + src)
		}
		return runEAPICommand(mgr, args.Device, cmd.String(), "text")
	})

	// traceroute_host
	type traceArgs struct {
		Device      string `json:"device,omitempty" jsonschema:"target device, given as its management IPv4 address (e.g. '192.0.2.10'). This is the intended input. A configured device name also works, but do NOT pass a hostname. Optional only when a single device is configured"`
		Destination string `json:"destination" jsonschema:"IP address or hostname to trace"`
		VRF         string `json:"vrf,omitempty" jsonschema:"optional VRF name"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "traceroute_host",
		Description: "Traceroute from a device to a destination to see the hop-by-hop path. Use for 'what path does traffic take', locating where reachability breaks. Optional vrf.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args traceArgs) (*mcp.CallToolResult, any, error) {
		if strings.TrimSpace(args.Destination) == "" {
			return nil, nil, fmt.Errorf("destination is required")
		}
		cmd := "traceroute "
		if v := strings.TrimSpace(args.VRF); v != "" {
			cmd += "vrf " + v + " "
		}
		cmd += strings.TrimSpace(args.Destination)
		return runEAPICommand(mgr, args.Device, cmd, "text")
	})

	// BGP route inspection per neighbor.
	type bgpRoutesArgs struct {
		Device   string `json:"device,omitempty" jsonschema:"target device, given as its management IPv4 address (e.g. '192.0.2.10'). This is the intended input. A configured device name also works, but do NOT pass a hostname. Optional only when a single device is configured"`
		Neighbor string `json:"neighbor" jsonschema:"BGP neighbor IP address"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_bgp_advertised_routes",
		Description: "Prefixes this device ADVERTISES to a specific BGP neighbor. Use for 'what are we sending to peer X', outbound policy checks. Runs 'show ip bgp neighbors <ip> advertised-routes'.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args bgpRoutesArgs) (*mcp.CallToolResult, any, error) {
		if strings.TrimSpace(args.Neighbor) == "" {
			return nil, nil, fmt.Errorf("neighbor is required")
		}
		cmd := fmt.Sprintf("show ip bgp neighbors %s advertised-routes", strings.TrimSpace(args.Neighbor))
		return runEAPICommand(mgr, args.Device, cmd, "json")
	})
	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_bgp_received_routes",
		Description: "Prefixes RECEIVED from a specific BGP neighbor. Use for 'what is peer X sending us', inbound route/policy checks. Runs 'show ip bgp neighbors <ip> received-routes'.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args bgpRoutesArgs) (*mcp.CallToolResult, any, error) {
		if strings.TrimSpace(args.Neighbor) == "" {
			return nil, nil, fmt.Errorf("neighbor is required")
		}
		cmd := fmt.Sprintf("show ip bgp neighbors %s received-routes", strings.TrimSpace(args.Neighbor))
		return runEAPICommand(mgr, args.Device, cmd, "json")
	})
}
