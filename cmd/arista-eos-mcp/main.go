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
		Version: version,
	}, nil)

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
