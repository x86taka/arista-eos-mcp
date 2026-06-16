package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/x86taka/arista-eos-mcp/internal/manager"
)

// fleetConcurrency bounds how many devices are queried at once.
const fleetConcurrency = 8

// fleetResult is the outcome of running a command on one device.
type fleetResult struct {
	device string
	output string
	err    error
}

// runFleet runs command on each named device concurrently (bounded), returning
// results in the input order. An empty devices slice targets all devices.
func runFleet(mgr *manager.Manager, devices []string, command, encoding string) ([]fleetResult, error) {
	if len(devices) == 0 {
		devices = mgr.Names()
	}
	// Validate up front so a typo fails fast rather than per-device.
	for _, d := range devices {
		if _, err := mgr.Device(d); err != nil {
			return nil, err
		}
	}

	results := make([]fleetResult, len(devices))
	sem := make(chan struct{}, fleetConcurrency)
	var wg sync.WaitGroup
	for i, d := range devices {
		i, d := i, d
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			results[i] = fleetResult{device: d}
			client, err := mgr.EAPI(d)
			if err != nil {
				results[i].err = err
				return
			}
			res, err := client.RunCommands([]string{command}, encoding)
			if err != nil {
				results[i].err = err
				return
			}
			out, ferr := formatResults(res)
			if ferr != nil {
				results[i].err = ferr
				return
			}
			results[i].output = out
		}()
	}
	wg.Wait()
	return results, nil
}

// formatFleet renders fleet results with per-device sections and a summary.
func formatFleet(command string, results []fleetResult) string {
	var b strings.Builder
	var ok, failed []string
	for _, r := range results {
		fmt.Fprintf(&b, "========== %s ==========\n", r.device)
		if r.err != nil {
			fmt.Fprintf(&b, "ERROR: %v\n\n", r.err)
			failed = append(failed, r.device)
			continue
		}
		b.WriteString(r.output)
		b.WriteString("\n")
		ok = append(ok, r.device)
	}
	sort.Strings(ok)
	sort.Strings(failed)
	summary := fmt.Sprintf("Command %q across %d device(s): %d ok, %d failed",
		command, len(results), len(ok), len(failed))
	if len(failed) > 0 {
		summary += " (failed: " + strings.Join(failed, ", ") + ")"
	}
	return summary + "\n\n" + b.String()
}

func registerFleetTools(s *mcp.Server, mgr *manager.Manager) {
	type fleetCmdArgs struct {
		Devices  []string `json:"devices,omitempty" jsonschema:"target devices, each given as an IP address whenever possible (names/hostnames also work); empty means all configured devices. Prefer IP addresses"`
		Command  string   `json:"command" jsonschema:"the read-only command to run on each device, e.g. 'show version'"`
		Encoding string   `json:"encoding,omitempty" jsonschema:"response encoding: 'json' (default) or 'text'"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "fleet_run_command",
		Description: "Run the same read-only command on many devices in parallel and return a combined, per-device report.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args fleetCmdArgs) (*mcp.CallToolResult, any, error) {
		if strings.TrimSpace(args.Command) == "" {
			return nil, nil, fmt.Errorf("command is required")
		}
		enc := args.Encoding
		if enc == "" {
			enc = "json"
		}
		results, err := runFleet(mgr, args.Devices, args.Command, enc)
		if err != nil {
			return nil, nil, err
		}
		return textResult(formatFleet(args.Command, results)), nil, nil
	})

	type fleetDevArgs struct {
		Devices []string `json:"devices,omitempty" jsonschema:"target devices, each given as an IP address whenever possible (names/hostnames also work); empty means all configured devices. Prefer IP addresses"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "fleet_get_version",
		Description: "Collect 'show version' from many devices in parallel (model, EOS version, uptime).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args fleetDevArgs) (*mcp.CallToolResult, any, error) {
		results, err := runFleet(mgr, args.Devices, "show version", "json")
		if err != nil {
			return nil, nil, err
		}
		return textResult(formatFleet("show version", results)), nil, nil
	})
}
