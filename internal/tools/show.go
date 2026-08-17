package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/x86taka/arista-eos-mcp/internal/manager"
)

// showSpec describes one curated read-only dataset reachable through
// get_show_data. topic is the enum value clients pass; hint is a one-line
// routing note folded into the tool description, left empty when the topic
// name already says what the dataset is.
type showSpec struct {
	topic    string
	hint     string
	command  string
	encoding string // "json" or "text"
}

// showCatalog is the single source of truth for get_show_data: it drives the
// topic enum, the hint table in the description, and command dispatch. JSON
// encoding is used for structured commands; text for commands that EOS only
// renders as text.
var showCatalog = []showSpec{
	{"version", "EOS version, model, serial number, uptime", "show version", "json"},
	{"interfaces_status", "", "show interfaces status", "json"},
	{"interface_counters", "traffic and error counters: drops, discards, CRC", "show interfaces counters", "json"},
	{"mac_address_table", "", "show mac address-table", "json"},
	{"arp_table", "", "show ip arp", "json"},
	{"vlans", "", "show vlan", "json"},
	{"lldp_neighbors", "", "show lldp neighbors", "json"},
	{"port_channels", "LAG/EtherChannel summary and member state", "show port-channel summary", "json"},
	{"mlag", "", "show mlag", "json"},
	{"spanning_tree", "", "show spanning-tree", "json"},
	{"bgp_summary", "BGP IPv4 session states and prefix counts; start here for BGP", "show ip bgp summary", "json"},
	{"bgp_neighbors", "per-neighbor BGP detail; use after bgp_summary", "show ip bgp neighbors", "json"},
	{"ospf_neighbors", "", "show ip ospf neighbor", "json"},
	{"transceivers", "optic/SFP DOM: Rx/Tx light levels", "show interfaces transceiver", "json"},
	{"environment", "power supplies, fans, temperatures", "show environment all", "json"},
	{"ntp_status", "", "show ntp status", "text"},
	{"logging", "100 most recent syslog messages", "show logging last 100", "text"},
	{"processes", "per-process CPU and memory snapshot", "show processes top once", "text"},
	{"vxlan_interface", "VNI-to-VLAN/VRF mappings and VTEP source interface", "show interfaces vxlan 1", "json"},
	{"vxlan_vtep", "remote VTEPs this switch has learned", "show vxlan vtep", "json"},
	{"vxlan_address_table", "remote MAC to VTEP/VNI forwarding table", "show vxlan address-table", "json"},
	{"bgp_evpn_summary", "EVPN control-plane session states; start here for EVPN", "show bgp evpn summary", "json"},
	{"bgp_evpn", "EVPN routes (Type-2/3/5); use after bgp_evpn_summary", "show bgp evpn", "json"},
	{"ip_interface_brief", "L3 interfaces with their IPv4 addresses", "show ip interface brief", "json"},
	{"vrfs", "", "show vrf", "json"},
	{"bfd_peers", "BFD fast-failure-detection session state", "show bfd peers", "json"},
	{"ipv6_neighbors", "", "show ipv6 neighbors", "json"},

	// Routing (beyond BGP/OSPF basics above).
	{"ipv6_route", "", "show ipv6 route", "json"},
	{"route_summary", "route counts by protocol (RIB scale)", "show ip route summary", "json"},
	{"ospf", "OSPF process, areas, timers; pair with ospf_neighbors", "show ip ospf", "json"},
	{"isis_neighbors", "", "show isis neighbors", "json"},

	// Multicast.
	{"pim_neighbors", "PIM multicast routing adjacencies", "show ip pim neighbor", "json"},
	{"igmp_snooping_groups", "which ports joined which multicast groups", "show ip igmp snooping groups", "json"},

	// Aggregation / L2 detail.
	{"lacp_peers", "LACP partner detail; pair with port_channels", "show lacp peer", "json"},

	// Security / policy.
	{"ip_access_lists", "IPv4 ACL rules with hit counters", "show ip access-lists", "json"},

	// System / hardware / platform.
	{"inventory", "chassis, line cards, PSUs, optics with serials", "show inventory", "json"},
	{"reload_cause", "", "show reload cause", "json"},
	{"hardware_capacity", "ASIC/TCAM table utilization against limits", "show hardware capacity", "json"},
	{"clock", "", "show clock", "text"},

	// QoS.
	{"qos_interfaces", "per-port QoS trust mode, shaping, tx-queue mapping", "show qos interfaces", "json"},

	// Storm control.
	{"storm_control", "broadcast/multicast suppression thresholds per port", "show storm-control", "json"},

	// DHCP relay.
	{"dhcp_relay", "DHCP helper-address configuration", "show ip dhcp relay", "json"},

	// sFlow.
	{"sflow", "sFlow collectors, sample rate, datagrams sent", "show sflow", "json"},

	// First-hop redundancy (VRRP / VARP).
	{"vrrp", "first-hop redundancy: master/backup role, virtual IP", "show vrrp", "json"},
	{"varp", "anycast gateway virtual IP/MAC (EVPN/VXLAN fabrics)", "show ip virtual-router", "json"},

	// PTP (Precision Time Protocol).
	{"ptp", "PTP clock: grandmaster, offset-from-master, port roles", "show ptp", "json"},

	// AAA / TACACS+ / RADIUS.
	{"aaa", "authentication/authorization/accounting method lists", "show aaa", "json"},
	{"tacacs", "TACACS+ server reachability and counters", "show tacacs", "json"},
	{"radius", "RADIUS server reachability and counters", "show radius", "json"},

	// SNMP.
	{"snmp", "", "show snmp", "json"},
	{"snmp_host", "configured SNMP trap/notification receivers", "show snmp notification host", "json"},

	// MACsec.
	{"macsec", "link-encryption status per interface", "show mac security interface", "json"},
	{"macsec_counters", "MACsec encrypted/protected counters and errors", "show mac security counters", "json"},

	// Tunnels (GRE / generic).
	{"tunnel_fib", "GRE/VXLAN/MPLS tunnel forwarding entries", "show tunnel fib", "json"},

	// MPLS / LDP.
	{"mpls_lfib", "MPLS label forwarding: in/out labels, FEC", "show mpls lfib route", "json"},
	{"ldp_neighbors", "LDP label-distribution session state", "show mpls ldp neighbor", "json"},

	// VXLAN counters.
	{"vxlan_counters", "per-VTEP encap/decap packet and byte counters", "show vxlan counters vtep", "json"},
}

// showTopics indexes showCatalog by topic for dispatch.
var showTopics = func() map[string]showSpec {
	m := make(map[string]showSpec, len(showCatalog))
	for _, spec := range showCatalog {
		m[spec.topic] = spec
	}
	return m
}()

// showDataDescriptionPreamble is the fixed part of the tool description, ahead
// of the generated hint table.
const showDataDescriptionPreamble = "Fetch a curated read-only EOS dataset by topic. Most topic names say what they " +
	"return (e.g. 'vlans', 'mac_address_table', 'arp_table'). For the IPv4 routing table with an " +
	"optional prefix/VRF filter use get_ip_route; for anything with no topic use run_show_command. " +
	"Hints for the less obvious topics:"

// showDataDescription is assembled once from showCatalog in declared order.
// It must never be built by ranging over a map: Go randomizes map order per
// process, which would make the tool definition differ between restarts and
// defeat client-side prompt caching.
var showDataDescription = func() string {
	var b strings.Builder
	b.WriteString(showDataDescriptionPreamble)
	for _, spec := range showCatalog {
		if spec.hint == "" {
			continue
		}
		fmt.Fprintf(&b, "\n%s: %s", spec.topic, spec.hint)
	}
	return b.String()
}()

// buildShowDataSchema hand-writes the input schema so that topic can be a real
// JSON Schema enum. Struct tags only produce descriptions (jsonschema-go has no
// enum tag syntax), but the SDK skips reflection entirely when Tool.InputSchema
// is set and validates incoming arguments against the schema given here — so an
// invalid topic is rejected with the full list of valid values.
func buildShowDataSchema() *jsonschema.Schema {
	enum := make([]any, len(showCatalog))
	for i, spec := range showCatalog {
		enum[i] = spec.topic
	}
	return &jsonschema.Schema{
		Type: "object",
		Properties: map[string]*jsonschema.Schema{
			"topic":  {Type: "string", Enum: enum, Description: "which dataset to fetch"},
			"device": {Type: "string", Description: shortDeviceDesc},
		},
		Required: []string{"topic"},
		// Matches the additionalProperties:false that reflected schemas get.
		AdditionalProperties: &jsonschema.Schema{Not: &jsonschema.Schema{}},
	}
}

type showDataArgs struct {
	Topic  string `json:"topic"`
	Device string `json:"device,omitempty"`
}

func registerShowDataTool(s *mcp.Server, mgr *manager.Manager) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_show_data",
		Description: showDataDescription,
		InputSchema: buildShowDataSchema(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args showDataArgs) (*mcp.CallToolResult, any, error) {
		spec, ok := showTopics[args.Topic]
		if !ok {
			// The enum already rejects this during validation; belt and braces.
			return nil, nil, fmt.Errorf("unknown topic %q", args.Topic)
		}
		return runEAPICommand(mgr, args.Device, spec.command, spec.encoding)
	})
}

// registerIPRoute keeps get_ip_route a tool of its own: it takes prefix and VRF
// filters that don't fit get_show_data's uniform topic+device shape.
func registerIPRoute(s *mcp.Server, mgr *manager.Manager) {
	type routeArgs struct {
		Device string `json:"device,omitempty" jsonschema:"target device (IP preferred; name/hostname OK); optional if only one device is configured"`
		Prefix string `json:"prefix,omitempty" jsonschema:"optional IPv4 prefix or address to look up, e.g. '10.0.0.0/24'"`
		VRF    string `json:"vrf,omitempty" jsonschema:"optional VRF name"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_ip_route",
		Description: "IPv4 routing table (FIB/RIB). Use for: 'how does it reach X', next-hop, default route, route to a prefix, VRF routing. Optionally filter by prefix and/or VRF.",
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
