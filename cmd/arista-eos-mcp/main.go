// Command arista-eos-mcp is a read-only Model Context Protocol (MCP) server
// for Arista EOS devices, exposing show commands over eAPI and gNMI Get across
// one or more configured devices.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/x86taka/arista-eos-mcp/internal/config"
	"github.com/x86taka/arista-eos-mcp/internal/manager"
	"github.com/x86taka/arista-eos-mcp/internal/tools"
)

const version = "0.2.0" // x-release-please-version

// serverInstructions guides the client on how to choose among the tools.
const serverInstructions = `This server provides access to Arista EOS switches over eAPI and gNMI.
When EOS_READ_ONLY=true (default), it cannot change configuration; state-changing commands are rejected.

Choosing a target device:
- Every tool takes a "device" argument. Prefer an IP address (e.g. "192.0.2.10").
  A configured device name or hostname also works. It is optional only when a
  single device is configured.
- Call "list_devices" first if you are unsure which devices exist or whether
  ad-hoc hosts (connect to any IP with shared credentials) are enabled.

Choosing a tool:
- Prefer the most SPECIFIC tool for the data you need (e.g. "get_bgp_summary",
  "get_interfaces", "get_mac_address_table") instead of "run_show_command".
- Use "run_show_command" only for read-only commands that have no dedicated tool.
- For ONE command across MANY devices, use "fleet_run_command" / "fleet_get_version".
- To diff TWO devices, use "compare_config" or "compare_command".
- For an overall status check of one device, use "get_device_health".
- For reachability tests use "ping_host" / "traceroute_host".
- For model-driven telemetry or to watch values over time, use the gNMI tools
  ("gnmi_get" for a snapshot, "gnmi_subscribe" for a time window).

The "troubleshoot_*", "device_health_review", and "compare_devices" prompts
provide ready-made investigation workflows.`

func main() {
	// Logs go to stderr so they don't corrupt the stdio MCP transport.
	logger := log.New(os.Stderr, "arista-eos-mcp ", log.LstdFlags|log.Lmsgprefix)

	cfg, err := config.Load()
	if err != nil {
		logger.Fatalf("configuration error: %v", err)
	}

	mgr, err := manager.New(cfg)
	if err != nil {
		logger.Fatalf("manager initialization failed: %v", err)
	}
	logger.Printf("loaded %d device(s): %s (read-only=%v, ad-hoc hosts=%v)",
		len(mgr.Names()), strings.Join(mgr.Names(), ", "), cfg.ReadOnly, mgr.DynamicHostsEnabled())

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "arista-eos-mcp",
		Title:   "Arista EOS (read-only)",
		Version: version,
	}, &mcp.ServerOptions{Instructions: serverInstructions})

	tools.Register(server, mgr)

	// Transport: "stdio" (default) for client-spawned use, or "http" for a
	// long-running service (e.g. docker compose up) using Streamable HTTP.
	switch transport := getenvDefault("MCP_TRANSPORT", "stdio"); transport {
	case "stdio":
		logger.Printf("starting MCP server on stdio")
		if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
			logger.Fatalf("server stopped: %v", err)
		}
	case "http", "streamable", "streamable-http":
		addr := getenvDefault("MCP_HTTP_ADDR", ":8080")
		handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
		logger.Printf("starting MCP server on http %s", addr)
		if err := http.ListenAndServe(addr, handler); err != nil {
			logger.Fatalf("server stopped: %v", err)
		}
	default:
		logger.Fatalf("unknown MCP_TRANSPORT %q: use \"stdio\" or \"http\"", transport)
	}
}

func getenvDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
