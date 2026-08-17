package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/x86taka/arista-eos-mcp/internal/manager"
)

// healthCommands are the show commands aggregated by get_device_health.
var healthCommands = []string{
	"show version",
	"show environment all",
	"show interfaces status",
	"show port-channel summary",
	"show mlag",
	"show ip bgp summary",
}

func registerDiagnostics(s *mcp.Server, mgr *manager.Manager) {
	// get_device_health aggregates several show commands into one report. Each
	// command runs independently so one unsupported command doesn't fail the rest.
	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_device_health",
		Description: "One-shot overall health check of a SINGLE device: aggregates version, environment, interface status, port-channels, MLAG, and BGP into one report. Use for open-ended 'is this device healthy / what's its status' questions before drilling into a specific area.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args deviceArgs) (*mcp.CallToolResult, any, error) {
		client, err := mgr.EAPI(args.Device)
		if err != nil {
			return nil, nil, err
		}
		var b strings.Builder
		for _, cmd := range healthCommands {
			fmt.Fprintf(&b, "===== %s =====\n", cmd)
			results, err := client.RunCommands([]string{cmd}, "json")
			if err != nil {
				fmt.Fprintf(&b, "(error: %v)\n\n", err)
				continue
			}
			out, ferr := formatResults(results)
			if ferr != nil {
				fmt.Fprintf(&b, "(format error: %v)\n\n", ferr)
				continue
			}
			b.WriteString(out)
			b.WriteString("\n")
		}
		return textResult(b.String()), nil, nil
	})

	// compare_config diffs the running-config of two devices.
	type compareConfigArgs struct {
		DeviceA string `json:"device_a" jsonschema:"first device (IP preferred)"`
		DeviceB string `json:"device_b" jsonschema:"second device (IP preferred)"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "compare_config",
		Description: "Diff the running-config of TWO devices and report lines unique to each. Use for 'what's different between A and B', config drift, consistency checks across a pair.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args compareConfigArgs) (*mcp.CallToolResult, any, error) {
		a, err := runningConfig(mgr, args.DeviceA)
		if err != nil {
			return nil, nil, err
		}
		b, err := runningConfig(mgr, args.DeviceB)
		if err != nil {
			return nil, nil, err
		}
		return textResult(diffLines(args.DeviceA, a, args.DeviceB, b)), nil, nil
	})

	// compare_command runs the same command on two devices and diffs the text.
	type compareCmdArgs struct {
		DeviceA string `json:"device_a" jsonschema:"first device (IP preferred)"`
		DeviceB string `json:"device_b" jsonschema:"second device (IP preferred)"`
		Command string `json:"command" jsonschema:"the read-only command to run on both devices, e.g. 'show vlan'"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "compare_command",
		Description: "Run the SAME command on TWO devices and diff the output lines. Use to compare specific state between a pair (e.g. 'show vlan', 'show ip bgp summary'). For full config use compare_config; for many devices use fleet_run_command.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args compareCmdArgs) (*mcp.CallToolResult, any, error) {
		if strings.TrimSpace(args.Command) == "" {
			return nil, nil, fmt.Errorf("command is required")
		}
		a, err := commandText(mgr, args.DeviceA, args.Command)
		if err != nil {
			return nil, nil, err
		}
		b, err := commandText(mgr, args.DeviceB, args.Command)
		if err != nil {
			return nil, nil, err
		}
		return textResult(diffLines(args.DeviceA, a, args.DeviceB, b)), nil, nil
	})
}

func runningConfig(mgr *manager.Manager, device string) (string, error) {
	client, err := mgr.EAPI(device)
	if err != nil {
		return "", err
	}
	return client.RunningConfig()
}

func commandText(mgr *manager.Manager, device, command string) (string, error) {
	client, err := mgr.EAPI(device)
	if err != nil {
		return "", err
	}
	results, err := client.RunCommands([]string{command}, "text")
	if err != nil {
		return "", err
	}
	if len(results) > 0 {
		return results[0].Text, nil
	}
	return "", nil
}

// diffLines reports lines present in only one of the two inputs, preserving
// order. It is an order-insensitive set diff, which is well suited to comparing
// device configurations and tabular show output.
func diffLines(nameA, a, nameB, b string) string {
	setA := lineSet(a)
	setB := lineSet(b)

	var onlyA, onlyB []string
	for _, line := range splitLines(a) {
		if !setB[line] {
			onlyA = append(onlyA, line)
		}
	}
	for _, line := range splitLines(b) {
		if !setA[line] {
			onlyB = append(onlyB, line)
		}
	}

	var sb strings.Builder
	if len(onlyA) == 0 && len(onlyB) == 0 {
		fmt.Fprintf(&sb, "No differences: %s and %s produced identical output.\n", nameA, nameB)
		return sb.String()
	}
	fmt.Fprintf(&sb, "## Only in %s (%d lines)\n", nameA, len(onlyA))
	for _, l := range onlyA {
		fmt.Fprintf(&sb, "- %s\n", l)
	}
	fmt.Fprintf(&sb, "\n## Only in %s (%d lines)\n", nameB, len(onlyB))
	for _, l := range onlyB {
		fmt.Fprintf(&sb, "+ %s\n", l)
	}
	return sb.String()
}

func splitLines(s string) []string {
	raw := strings.Split(s, "\n")
	out := make([]string, 0, len(raw))
	for _, l := range raw {
		l = strings.TrimRight(l, "\r")
		if strings.TrimSpace(l) == "" {
			continue
		}
		out = append(out, l)
	}
	return out
}

func lineSet(s string) map[string]bool {
	m := make(map[string]bool)
	for _, l := range splitLines(s) {
		m[l] = true
	}
	return m
}
