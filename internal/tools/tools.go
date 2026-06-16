// Package tools registers the read-only MCP tools exposed by the server.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/x86taka/arista-eos-mcp/internal/eapi"
	"github.com/x86taka/arista-eos-mcp/internal/manager"
)

// textResult wraps a plain string into a CallToolResult.
func textResult(s string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: s}},
	}
}

// formatResults renders eAPI results as a readable string (pretty JSON or text).
func formatResults(results []eapi.Result) (string, error) {
	var b strings.Builder
	for i, r := range results {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "## %s\n", r.Command)
		if r.Text != "" {
			b.WriteString(r.Text)
			b.WriteString("\n")
			continue
		}
		pretty, err := json.MarshalIndent(r.JSON, "", "  ")
		if err != nil {
			return "", fmt.Errorf("failed to format result for %q: %w", r.Command, err)
		}
		b.Write(pretty)
		b.WriteString("\n")
	}
	return b.String(), nil
}

// Register adds every tool, resource, and prompt to the server.
func Register(s *mcp.Server, mgr *manager.Manager) {
	registerListDevices(s, mgr)
	registerShowTool(s, mgr)
	registerSimpleEAPITools(s, mgr)
	registerShowCatalog(s, mgr) // show.go: curated read-only show tools
	registerDiagnostics(s, mgr) // diagnostics.go: health + compare tools
	registerOpsTools(s, mgr)    // ops.go: ping/traceroute/bgp diagnostics
	registerFleetTools(s, mgr)  // fleet.go: multi-device parallel execution
	registerGNMITools(s, mgr)
	registerResources(s, mgr) // resources.go: per-device resources
	registerPrompts(s)        // resources.go: troubleshooting prompts
}

// runEAPICommand runs a single command on the named device and returns its
// formatted output as a tool result. Shared by tools across the package.
func runEAPICommand(mgr *manager.Manager, device, command, encoding string) (*mcp.CallToolResult, any, error) {
	client, err := mgr.EAPI(device)
	if err != nil {
		return nil, nil, err
	}
	results, err := client.RunCommands([]string{command}, encoding)
	if err != nil {
		return nil, nil, err
	}
	out, err := formatResults(results)
	if err != nil {
		return nil, nil, err
	}
	return textResult(out), nil, nil
}

// --- list_devices ---

func registerListDevices(s *mcp.Server, mgr *manager.Manager) {
	type noArgs struct{}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_devices",
		Description: "List the configured EOS device names that the other tools can target.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ noArgs) (*mcp.CallToolResult, any, error) {
		names := mgr.Names()
		var b strings.Builder
		if len(names) == 0 {
			b.WriteString("No predefined devices.\n")
		} else {
			b.WriteString("Configured devices:\n")
			for _, n := range names {
				fmt.Fprintf(&b, "- %s\n", n)
			}
		}
		if mgr.DynamicHostsEnabled() {
			b.WriteString("\nAd-hoc hosts: enabled. Pass a target as the 'device' argument to connect " +
				"using the shared default credentials — preferably an IP address (e.g. '192.0.2.10'), " +
				"though a hostname also works.\n")
		}
		return textResult(b.String()), nil, nil
	})
}

// --- run_show_command ---

type runShowArgs struct {
	Device   string `json:"device,omitempty" jsonschema:"target device, given as an IP address whenever possible (e.g. '192.0.2.10'); a configured device name or hostname also works. Prefer an IP address. Optional only when a single device is configured"`
	Command  string `json:"command" jsonschema:"the read-only EOS command to run, e.g. 'show version'"`
	Encoding string `json:"encoding,omitempty" jsonschema:"response encoding: 'json' (default) or 'text'"`
}

func registerShowTool(s *mcp.Server, mgr *manager.Manager) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "run_show_command",
		Description: "Run a single read-only EOS command (e.g. 'show version') over eAPI and return the result. Write/config commands are rejected.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args runShowArgs) (*mcp.CallToolResult, any, error) {
		if strings.TrimSpace(args.Command) == "" {
			return nil, nil, fmt.Errorf("command is required")
		}
		client, err := mgr.EAPI(args.Device)
		if err != nil {
			return nil, nil, err
		}
		results, err := client.RunCommands([]string{args.Command}, args.Encoding)
		if err != nil {
			return nil, nil, err
		}
		out, err := formatResults(results)
		if err != nil {
			return nil, nil, err
		}
		return textResult(out), nil, nil
	})
}

// --- simple convenience eAPI tools ---

// deviceArgs is the argument struct for tools that only need a device selector.
type deviceArgs struct {
	Device string `json:"device,omitempty" jsonschema:"target device, given as an IP address whenever possible (e.g. '192.0.2.10'); a configured device name or hostname also works. Prefer an IP address. Optional only when a single device is configured"`
}

func registerSimpleEAPITools(s *mcp.Server, mgr *manager.Manager) {
	// get_interfaces accepts an optional interface name.
	type ifaceArgs struct {
		Device    string `json:"device,omitempty" jsonschema:"target device, given as an IP address whenever possible (e.g. '192.0.2.10'); a configured device name or hostname also works. Prefer an IP address. Optional only when a single device is configured"`
		Interface string `json:"interface,omitempty" jsonschema:"optional interface name to filter, e.g. 'Ethernet1'"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_interfaces",
		Description: "Get interface status and counters ('show interfaces'). Optionally filter by interface name.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args ifaceArgs) (*mcp.CallToolResult, any, error) {
		cmd := "show interfaces"
		if strings.TrimSpace(args.Interface) != "" {
			cmd = "show interfaces " + strings.TrimSpace(args.Interface)
		}
		return runEAPICommand(mgr, args.Device, cmd, "json")
	})

	// get_running_config returns the full running config as text.
	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_running_config",
		Description: "Get the device running configuration as text.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args deviceArgs) (*mcp.CallToolResult, any, error) {
		client, err := mgr.EAPI(args.Device)
		if err != nil {
			return nil, nil, err
		}
		cfg, err := client.RunningConfig()
		if err != nil {
			return nil, nil, err
		}
		return textResult(cfg), nil, nil
	})
}

// --- gNMI tools ---

func registerGNMITools(s *mcp.Server, mgr *manager.Manager) {
	type gnmiGetArgs struct {
		Device   string   `json:"device,omitempty" jsonschema:"target device, given as an IP address whenever possible (e.g. '192.0.2.10'); a configured device name or hostname also works. Prefer an IP address. Optional only when a single device is configured"`
		Paths    []string `json:"paths" jsonschema:"gNMI paths to retrieve, e.g. ['/interfaces/interface/state']"`
		Encoding string   `json:"encoding,omitempty" jsonschema:"encoding: 'json_ietf' (default), 'json', 'ascii', 'proto'"`
		DataType string   `json:"data_type,omitempty" jsonschema:"data type: 'all' (default), 'config', 'state', 'operational'"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "gnmi_get",
		Description: "Issue a gNMI Get for one or more paths and return the response as JSON.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args gnmiGetArgs) (*mcp.CallToolResult, any, error) {
		if len(args.Paths) == 0 {
			return nil, nil, fmt.Errorf("at least one path is required")
		}
		client, err := mgr.GNMI(args.Device)
		if err != nil {
			return nil, nil, err
		}
		out, err := client.Get(ctx, args.Paths, args.Encoding, args.DataType)
		if err != nil {
			return nil, nil, err
		}
		return textResult(out), nil, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "gnmi_capabilities",
		Description: "Get the device gNMI capabilities (supported models and encodings).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args deviceArgs) (*mcp.CallToolResult, any, error) {
		client, err := mgr.GNMI(args.Device)
		if err != nil {
			return nil, nil, err
		}
		out, err := client.Capabilities(ctx)
		if err != nil {
			return nil, nil, err
		}
		return textResult(out), nil, nil
	})

	// gnmi_subscribe collects telemetry updates over a bounded window.
	type gnmiSubArgs struct {
		Device        string   `json:"device,omitempty" jsonschema:"target device, given as an IP address whenever possible (e.g. '192.0.2.10'); a configured device name or hostname also works. Prefer an IP address. Optional only when a single device is configured"`
		Paths         []string `json:"paths" jsonschema:"gNMI paths to subscribe to, e.g. ['/interfaces/interface/state/counters']"`
		Mode          string   `json:"mode,omitempty" jsonschema:"subscription mode: 'on-change' (default), 'sample', or 'once'"`
		SampleSeconds int      `json:"sample_seconds,omitempty" jsonschema:"sample interval in seconds for 'sample' mode (default 10)"`
		WindowSeconds int      `json:"window_seconds,omitempty" jsonschema:"how long to collect updates, in seconds (default 15, max 60)"`
		MaxUpdates    int      `json:"max_updates,omitempty" jsonschema:"cap on number of updates to collect (default 500)"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "gnmi_subscribe",
		Description: "Subscribe to gNMI paths and collect telemetry updates over a bounded time window, then return them. Use 'once' for a single snapshot, 'sample' for periodic samples, or 'on-change' for change events.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args gnmiSubArgs) (*mcp.CallToolResult, any, error) {
		if len(args.Paths) == 0 {
			return nil, nil, fmt.Errorf("at least one path is required")
		}
		window := args.WindowSeconds
		if window <= 0 {
			window = 15
		}
		if window > 60 {
			window = 60 // safety cap so a tool call can't block indefinitely
		}
		client, err := mgr.GNMI(args.Device)
		if err != nil {
			return nil, nil, err
		}
		out, err := client.Subscribe(ctx, args.Paths, args.Mode,
			time.Duration(args.SampleSeconds)*time.Second,
			time.Duration(window)*time.Second,
			args.MaxUpdates)
		if err != nil {
			return nil, nil, err
		}
		return textResult(out), nil, nil
	})
}
