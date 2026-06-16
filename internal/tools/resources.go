package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/x86taka/arista-eos-mcp/internal/manager"
)

// registerResources exposes per-device read-only resources: the running
// configuration (text) and the version info (JSON). URIs look like
// "eos://<device>/running-config".
func registerResources(s *mcp.Server, mgr *manager.Manager) {
	for _, name := range mgr.Names() {
		name := name

		cfgURI := fmt.Sprintf("eos://%s/running-config", name)
		s.AddResource(&mcp.Resource{
			Name:        fmt.Sprintf("%s running-config", name),
			URI:         cfgURI,
			MIMEType:    "text/plain",
			Description: fmt.Sprintf("Running configuration of %s", name),
		}, func(ctx context.Context, _ *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			cfg, err := runningConfig(mgr, name)
			if err != nil {
				return nil, err
			}
			return &mcp.ReadResourceResult{
				Contents: []*mcp.ResourceContents{{URI: cfgURI, MIMEType: "text/plain", Text: cfg}},
			}, nil
		})

		verURI := fmt.Sprintf("eos://%s/version", name)
		s.AddResource(&mcp.Resource{
			Name:        fmt.Sprintf("%s version", name),
			URI:         verURI,
			MIMEType:    "application/json",
			Description: fmt.Sprintf("Software/hardware version of %s (show version)", name),
		}, func(ctx context.Context, _ *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			out, err := commandJSON(mgr, name, "show version")
			if err != nil {
				return nil, err
			}
			return &mcp.ReadResourceResult{
				Contents: []*mcp.ResourceContents{{URI: verURI, MIMEType: "application/json", Text: out}},
			}, nil
		})
	}
}

func commandJSON(mgr *manager.Manager, device, command string) (string, error) {
	client, err := mgr.EAPI(device)
	if err != nil {
		return "", err
	}
	results, err := client.RunCommands([]string{command}, "json")
	if err != nil {
		return "", err
	}
	if len(results) > 0 {
		b, err := json.MarshalIndent(results[0].JSON, "", "  ")
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
	return "{}", nil
}

// registerPrompts adds reusable troubleshooting prompt templates. Each returns
// a user message that steers the model toward the relevant read-only tools.
func registerPrompts(s *mcp.Server) {
	userPrompt := func(text string) *mcp.GetPromptResult {
		return &mcp.GetPromptResult{
			Messages: []*mcp.PromptMessage{
				{Role: "user", Content: &mcp.TextContent{Text: text}},
			},
		}
	}
	arg := func(name, desc string, required bool) *mcp.PromptArgument {
		return &mcp.PromptArgument{Name: name, Description: desc, Required: required}
	}

	s.AddPrompt(&mcp.Prompt{
		Name:        "troubleshoot_interface",
		Description: "Guide an investigation of a problematic interface on a device.",
		Arguments:   []*mcp.PromptArgument{arg("device", "device name", true), arg("interface", "interface name, e.g. Ethernet1", true)},
	}, func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		d := req.Params.Arguments["device"]
		iface := req.Params.Arguments["interface"]
		return userPrompt(fmt.Sprintf(
			"Investigate interface %s on device %q. Use the read-only tools: call get_interfaces (interface=%s) for status/counters, "+
				"get_interface_counters for error/discard counters, get_transceivers to check optic levels, and get_lldp_neighbors to "+
				"confirm the expected neighbor. Summarize the link state, errors, light levels, and any likely root cause.",
			iface, d, iface)), nil
	})

	s.AddPrompt(&mcp.Prompt{
		Name:        "troubleshoot_bgp",
		Description: "Guide an investigation of BGP session problems on a device.",
		Arguments:   []*mcp.PromptArgument{arg("device", "device name", true), arg("neighbor", "optional BGP neighbor IP", false)},
	}, func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		d := req.Params.Arguments["device"]
		nbr := req.Params.Arguments["neighbor"]
		focus := "all BGP neighbors"
		if nbr != "" {
			focus = "BGP neighbor " + nbr
		}
		return userPrompt(fmt.Sprintf(
			"Investigate %s on device %q. Use get_bgp_summary for session states and prefix counts, then get_bgp_neighbors for detail. "+
				"If a session is not Established, check get_ip_route and get_interfaces for the underlying reachability. "+
				"Report each neighbor's state, uptime, accepted/advertised prefixes, and the most likely cause of any problem.",
			focus, d)), nil
	})

	s.AddPrompt(&mcp.Prompt{
		Name:        "device_health_review",
		Description: "Run a full health review of a device and summarize findings.",
		Arguments:   []*mcp.PromptArgument{arg("device", "device name", true)},
	}, func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		d := req.Params.Arguments["device"]
		return userPrompt(fmt.Sprintf(
			"Perform a health review of device %q. Call get_device_health, then drill into anything concerning with get_environment, "+
				"get_interfaces_status, get_mlag, and get_logging. Produce a concise report grouped into: hardware/environment, "+
				"interfaces, redundancy (MLAG/port-channels), routing/BGP, and recent log anomalies. Flag any item that needs attention.",
			d)), nil
	})

	s.AddPrompt(&mcp.Prompt{
		Name:        "compare_devices",
		Description: "Compare configuration between two devices and explain the differences.",
		Arguments:   []*mcp.PromptArgument{arg("device_a", "first device name", true), arg("device_b", "second device name", true)},
	}, func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		a := req.Params.Arguments["device_a"]
		b := req.Params.Arguments["device_b"]
		return userPrompt(fmt.Sprintf(
			"Compare devices %q and %q. Call compare_config to diff their running configurations, and compare_command for key state "+
				"(e.g. 'show vlan', 'show ip bgp summary'). Explain the meaningful differences, distinguishing expected per-device "+
				"settings (hostname, IPs, router-id) from configuration drift that may be unintended.",
			a, b)), nil
	})
}
