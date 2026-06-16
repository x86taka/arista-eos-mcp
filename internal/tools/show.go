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
	{"get_version", "EOS software version, model, serial, and uptime. Use for: 'what version/model is it', firmware, hardware, uptime, serial number. Runs 'show version'.", "show version", "json"},
	{"get_interfaces_status", "Brief up/down status of all interfaces (link state, speed, VLAN, description). Use for: 'which ports are up/down', link status overview. Runs 'show interfaces status'.", "show interfaces status", "json"},
	{"get_interface_counters", "Interface traffic and error counters. Use for: errors, drops, discards, CRC, packet/byte counts, troubleshooting a flapping or lossy link. Runs 'show interfaces counters'.", "show interfaces counters", "json"},
	{"get_mac_address_table", "MAC address (bridging/forwarding) table. Use for: 'where is MAC X learned', which port/VLAN a MAC is on, layer-2 forwarding. Runs 'show mac address-table'.", "show mac address-table", "json"},
	{"get_arp_table", "IPv4 ARP table (IP-to-MAC bindings). Use for: 'what MAC has IP X', ARP entries, neighbor resolution. Runs 'show ip arp'.", "show ip arp", "json"},
	{"get_vlans", "Configured VLANs and their member ports. Use for: VLAN list, which ports are in a VLAN. Runs 'show vlan'.", "show vlan", "json"},
	{"get_lldp_neighbors", "LLDP neighbor table (discovered adjacent devices and ports). Use for: 'what is connected to this switch', cabling/topology, neighbor discovery. Runs 'show lldp neighbors'.", "show lldp neighbors", "json"},
	{"get_port_channels", "Port-channel (LAG / bonding / EtherChannel) summary and member state. Use for: LAG status, which members are active. Runs 'show port-channel summary'.", "show port-channel summary", "json"},
	{"get_mlag", "MLAG (multi-chassis LAG) peer and domain status. Use for: MLAG health, peer-link, active/inactive state. Runs 'show mlag'.", "show mlag", "json"},
	{"get_spanning_tree", "Spanning-tree (STP/RSTP/MSTP) state, roots, and port roles. Use for: STP topology, blocked ports, loops. Runs 'show spanning-tree'.", "show spanning-tree", "json"},
	{"get_bgp_summary", "BGP IPv4 unicast neighbor summary (state, prefixes, uptime). Use FIRST for BGP questions: 'are BGP sessions up', peer states, prefix counts. Runs 'show ip bgp summary'.", "show ip bgp summary", "json"},
	{"get_bgp_neighbors", "Detailed per-neighbor BGP info. Use for: deep BGP diagnosis after get_bgp_summary, capabilities, timers. Runs 'show ip bgp neighbors'.", "show ip bgp neighbors", "json"},
	{"get_ospf_neighbors", "OSPF neighbor/adjacency table. Use for: OSPF questions, adjacency state (Full/Init), stuck neighbors. Runs 'show ip ospf neighbor'.", "show ip ospf neighbor", "json"},
	{"get_transceivers", "Transceiver / optic (SFP/QSFP) detail incl. light levels and DOM. Use for: optical power, Rx/Tx dBm, failing or marginal optics. Runs 'show interfaces transceiver'.", "show interfaces transceiver", "json"},
	{"get_environment", "Power supplies, fans/cooling, and temperatures. Use for: hardware health, overheating, PSU/fan failures. Runs 'show environment all'.", "show environment all", "json"},
	{"get_ntp_status", "NTP synchronization status. Use for: clock sync, time drift, NTP peers. Runs 'show ntp status'.", "show ntp status", "text"},
	{"get_logging", "The 100 most recent syslog messages. Use for: recent errors/events, 'what happened', log review. Runs 'show logging last 100'.", "show logging last 100", "text"},
	{"get_processes", "Per-process CPU and memory snapshot. Use for: high CPU/memory, which process is busy. Runs 'show processes top once'.", "show processes top once", "text"},
	{"get_vxlan_interface", "VXLAN interface (Vxlan1) config and state: VNI-to-VLAN/VRF mappings, source interface, flood lists. Use for: 'how is VXLAN set up', which VNIs are mapped, VTEP source IP. Runs 'show interfaces vxlan 1'.", "show interfaces vxlan 1", "json"},
	{"get_vxlan_vtep", "Remote VTEPs (VXLAN tunnel endpoints) this switch has learned. Use for: 'which VTEPs/leaf switches are in the fabric', overlay peer discovery. Runs 'show vxlan vtep'.", "show vxlan vtep", "json"},
	{"get_vxlan_address_table", "VXLAN MAC forwarding table mapping remote MACs to VTEPs and VNIs. Use for: 'which VTEP is MAC X behind', overlay layer-2 forwarding. Runs 'show vxlan address-table'.", "show vxlan address-table", "json"},
	{"get_bgp_evpn_summary", "BGP EVPN (l2vpn evpn) neighbor summary. Use FIRST for EVPN questions: 'are EVPN sessions up', overlay control-plane peer states, route counts. Runs 'show bgp evpn summary'.", "show bgp evpn summary", "json"},
	{"get_bgp_evpn", "BGP EVPN route table (Type-2 MAC/IP, Type-3 IMET, Type-5 IP prefix). Use for: deep EVPN diagnosis after get_bgp_evpn_summary, 'is MAC/IP X advertised', missing overlay routes. Runs 'show bgp evpn'.", "show bgp evpn", "json"},
	{"get_ip_interface_brief", "Brief table of L3 interfaces with their IPv4 addresses and status. Use for: 'what IP is on interface X', SVI/routed-port addressing overview. Runs 'show ip interface brief'.", "show ip interface brief", "json"},
	{"get_vrfs", "Configured VRFs and their interfaces/route-distinguishers. Use for: VRF list, tenant separation, which interfaces are in a VRF. Runs 'show vrf'.", "show vrf", "json"},
	{"get_bfd_peers", "BFD (Bidirectional Forwarding Detection) peer/session state. Use for: fast failure-detection status, which BFD sessions are up/down, flapping links. Runs 'show bfd peers'.", "show bfd peers", "json"},
	{"get_ipv6_neighbors", "IPv6 neighbor (ND) table — IPv6-to-MAC bindings. Use for: 'what MAC has IPv6 X', IPv6 neighbor resolution. Runs 'show ipv6 neighbors'.", "show ipv6 neighbors", "json"},
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
		Description: "IPv4 routing table (FIB/RIB). Use for: 'how does it reach X', next-hop, default route, route to a prefix, VRF routing. Optionally filter by prefix and/or VRF. Runs 'show ip route'.",
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
