package tools

import (
	"context"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/x86taka/arista-eos-mcp/internal/manager"
)

// showSpec describes a curated read-only tool backed by a single EOS command.
type showSpec struct {
	name     string
	desc     string
	command  string
	encoding string // "json" or "text"
}

// showCatalog is the set of convenience read-only tools. JSON encoding is used
// for structured commands; text for commands that EOS only renders as text.
var showCatalog = []showSpec{
	{"get_version", "EOS software version and hardware model ('show version').", "show version", "json"},
	{"get_interfaces_status", "Brief interface status table ('show interfaces status').", "show interfaces status", "json"},
	{"get_interface_counters", "Interface traffic counters ('show interfaces counters').", "show interfaces counters", "json"},
	{"get_mac_address_table", "MAC address (bridging) table ('show mac address-table').", "show mac address-table", "json"},
	{"get_arp_table", "IPv4 ARP table ('show ip arp').", "show ip arp", "json"},
	{"get_vlans", "Configured VLANs ('show vlan').", "show vlan", "json"},
	{"get_lldp_neighbors", "LLDP neighbor table ('show lldp neighbors').", "show lldp neighbors", "json"},
	{"get_port_channels", "Port-channel (LAG) summary ('show port-channel summary').", "show port-channel summary", "json"},
	{"get_mlag", "MLAG status ('show mlag').", "show mlag", "json"},
	{"get_spanning_tree", "Spanning-tree state ('show spanning-tree').", "show spanning-tree", "json"},
	{"get_bgp_summary", "BGP IPv4 unicast summary ('show ip bgp summary').", "show ip bgp summary", "json"},
	{"get_bgp_neighbors", "BGP neighbor detail ('show ip bgp neighbors').", "show ip bgp neighbors", "json"},
	{"get_ospf_neighbors", "OSPF neighbor table ('show ip ospf neighbor').", "show ip ospf neighbor", "json"},
	{"get_transceivers", "Transceiver/optic detail ('show interfaces transceiver').", "show interfaces transceiver", "json"},
	{"get_environment", "Power, cooling, and temperature ('show environment all').", "show environment all", "json"},
	{"get_ntp_status", "NTP synchronization status ('show ntp status').", "show ntp status", "text"},
	{"get_logging", "Recent syslog messages ('show logging last 100').", "show logging last 100", "text"},
	{"get_processes", "Per-process CPU/memory snapshot ('show processes top once').", "show processes top once", "text"},
}

func registerShowCatalog(s *mcp.Server, mgr *manager.Manager) {
	for _, spec := range showCatalog {
		spec := spec
		mcp.AddTool(s, &mcp.Tool{Name: spec.name, Description: spec.desc},
			func(ctx context.Context, _ *mcp.CallToolRequest, args deviceArgs) (*mcp.CallToolResult, any, error) {
				return runEAPICommand(mgr, args.Device, spec.command, spec.encoding)
			})
	}

	// get_ip_route accepts an optional prefix and VRF.
	type routeArgs struct {
		Device string `json:"device,omitempty" jsonschema:"target device, given as an IP address whenever possible (e.g. '192.0.2.10'); a configured device name or hostname also works. Prefer an IP address. Optional only when a single device is configured"`
		Prefix string `json:"prefix,omitempty" jsonschema:"optional IPv4 prefix or address to look up, e.g. '10.0.0.0/24'"`
		VRF    string `json:"vrf,omitempty" jsonschema:"optional VRF name"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_ip_route",
		Description: "IPv4 routing table ('show ip route'). Optionally filter by prefix and/or VRF.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args routeArgs) (*mcp.CallToolResult, any, error) {
		cmd := "show ip route"
		if v := strings.TrimSpace(args.VRF); v != "" {
			cmd += " vrf " + v
		}
		if p := strings.TrimSpace(args.Prefix); p != "" {
			cmd += " " + p
		}
		return runEAPICommand(mgr, args.Device, cmd, "json")
	})
}
