# Arista EOS MCP Server

A read-only [Model Context Protocol](https://modelcontextprotocol.io) (MCP)
server for Arista EOS devices, written in Go. It lets MCP clients such as Claude
Desktop / Claude Code query EOS switches over **eAPI** and **gNMI**.

The server is **read-only by default**: any command that would change the
configuration or device state is rejected before it reaches the device.

## Features

- eAPI tools: `get_show_data` (57 curated read-only datasets behind one `topic`
  argument), `run_show_command`, `get_interfaces`, `get_running_config`
- gNMI tools: `gnmi_get`, `gnmi_capabilities`
- Safety guard that rejects `configure`, `write`, `copy`, `reload`, `clear`,
  `no ...`, `bash`, and other state-changing commands in read-only mode

## Requirements

- Go 1.24+ (built with 1.26)
- An Arista EOS device with eAPI enabled (`management api http-commands`) and,
  optionally, gNMI (`management api gnmi`)

## Install

Prebuilt binaries (linux/darwin/windows, amd64/arm64) are attached to each
[GitHub Release](https://github.com/x86taka/arista-eos-mcp/releases) — download
the archive for your platform and extract the `arista-eos-mcp` binary.

Or install from source with Go:

```sh
go install github.com/x86taka/arista-eos-mcp/cmd/arista-eos-mcp@latest
```

This produces an `arista-eos-mcp` binary on your `PATH`. Pin a specific version
by replacing `@latest` with a tag, e.g. `@v0.1.0`.

## Build from source

```sh
git clone https://github.com/x86taka/arista-eos-mcp.git
cd arista-eos-mcp
go build -o arista-eos-mcp ./cmd/arista-eos-mcp
```

## Configuration

The server supports **one or more devices**. There are two ways to configure
it; `EOS_CONFIG` takes precedence when set.

### Option A: multi-device JSON inventory (recommended)

Point `EOS_CONFIG` at a JSON file describing the devices. Each tool then takes
an optional `device` argument to select the target; when only one device is
configured the argument may be omitted. Use the `list_devices` tool to see the
configured names.

```json
{
  "read_only": true,
  "timeout_seconds": 30,
  "devices": [
    {
      "name": "leaf1",
      "host": "192.0.2.10",
      "username": "admin",
      "password": "secret",
      "enable_password": "",
      "eapi_transport": "https",
      "eapi_port": 443,
      "gnmi_port": 6030,
      "gnmi_insecure": true,
      "tls_skip_verify": true
    },
    {
      "name": "spine1",
      "host": "192.0.2.20",
      "username": "readonly",
      "password": "ro-pass"
    }
  ]
}
```

See [`devices.example.json`](devices.example.json). Per-device fields default
to: `eapi_transport` = `https`, `eapi_port` = `443`, `gnmi_port` = `6030`, and
`gnmi_insecure` / `tls_skip_verify` = `true` (omit them for a lab; set to
`false` to enforce TLS).

```sh
EOS_CONFIG=/path/to/devices.json ./arista-eos-mcp
```

### Option B: single-device environment variables

When `EOS_CONFIG` is unset, the server builds a single device (named `default`,
override with `EOS_DEVICE_NAME`) from these variables.

| Variable                | Required | Default  | Description                                          |
| ----------------------- | -------- | -------- | ---------------------------------------------------- |
| `EOS_HOST`              | no*      | —        | IP/hostname of a predefined device (optional if defaults set) |
| `EOS_USERNAME`          | no*      | —        | eAPI / gNMI username (also the default credential)   |
| `EOS_PASSWORD`          | no*      | —        | password (also the default credential)               |
| `EOS_ENABLE_PASSWORD`   | no       | —        | enable (privileged exec) password, if needed         |
| `EOS_DEFAULT_USERNAME`  | no       | =`EOS_USERNAME` | shared username for ad-hoc hosts              |
| `EOS_DEFAULT_PASSWORD`  | no       | =`EOS_PASSWORD` | shared password for ad-hoc hosts              |
| `EOS_DEFAULT_ENABLE_PASSWORD` | no | =`EOS_ENABLE_PASSWORD` | shared enable password for ad-hoc hosts |
| `EOS_ALLOW_DYNAMIC_HOSTS` | no     | auto     | force-enable/disable ad-hoc hosts (auto = on if default creds set) |
| `EOS_ALLOWED_MANAGEMENT_PREFIXES` | no | —      | comma-separated CIDR allowlist for ad-hoc hosts (e.g. `192.0.2.0/24,10.10.0.0/16`) |
| `EOS_EAPI_TRANSPORT`    | no       | `https`  | `https`, `http`, `http_local`, or `socket`           |
| `EOS_EAPI_PORT`         | no       | `0`      | eAPI port (0 = transport default: https 443/http 80) |
| `EOS_GNMI_PORT`         | no       | `6030`   | gNMI gRPC port                                       |
| `EOS_GNMI_INSECURE`     | no       | `true`   | use plaintext gRPC (no TLS)                          |
| `EOS_GNMI_TLS_CA`       | no       | —        | path to a CA certificate for gNMI TLS                |
| `EOS_TLS_SKIP_VERIFY`   | no       | `true`   | skip TLS certificate verification (lab/self-signed)  |
| `EOS_DEVICE_NAME`       | no       | `default`| name reported for the single env-configured device  |
| `EOS_TIMEOUT_SECONDS`   | no       | `30`     | per-request timeout                                  |
| `EOS_READ_ONLY`         | no       | `true`   | reject write/state-changing commands                 |

\* At least one of these is required: either set `EOS_HOST` (with credentials)
for a predefined device, or provide credentials alone to run with ad-hoc hosts
only. See [Ad-hoc hosts](#ad-hoc-hosts-shared-credentials).

Transport (both config modes):

| Variable         | Default   | Description                                      |
| ---------------- | --------- | ------------------------------------------------ |
| `MCP_TRANSPORT`  | `stdio`   | `stdio` (client-spawned) or `http` (long-running) |
| `MCP_HTTP_ADDR`  | `:8080`   | listen address when `MCP_TRANSPORT=http`         |

> **Lab note:** vEOS/cEOS often use self-signed certs. Set
> `EOS_TLS_SKIP_VERIFY=true` for eAPI over HTTPS, and `EOS_GNMI_INSECURE=true`
> (or provide `EOS_GNMI_TLS_CA`) for gNMI.

## Ad-hoc hosts (shared credentials)

You don't have to predefine every device. If **shared default credentials** are
configured, any tool's `device` argument also accepts a **hostname or IP that is
not in the inventory** — the server connects to it on demand using those
credentials. Predefined devices and ad-hoc hosts can be mixed freely.

Ad-hoc access is enabled automatically when default credentials are present:

- **Env mode:** `EOS_DEFAULT_USERNAME` / `EOS_DEFAULT_PASSWORD`
  (`EOS_DEFAULT_ENABLE_PASSWORD` optional). These fall back to
  `EOS_USERNAME` / `EOS_PASSWORD`, so simply setting those — even without
  `EOS_HOST` — is enough to run with no predefined devices at all.
- **JSON mode:** a top-level `"defaults"` object:

  ```json
  {
    "defaults": {
      "username": "admin",
      "password": "secret",
      "gnmi_insecure": true,
      "tls_skip_verify": true
    },
    "devices": [ { "name": "leaf1", "host": "192.0.2.10", "username": "admin", "password": "secret" } ]
  }
  ```

The `device` value is used verbatim as the connection host. Pass a device's
**management IPv4 address** (e.g. `device: "192.0.2.50"`) — that is the intended
input, and the tool catalog instructs the model to use it. Ad-hoc hosts inherit
the default transport/port/TLS settings. Connections are cached for reuse. Set
`EOS_ALLOW_DYNAMIC_HOSTS=false` (or `"allow_dynamic_hosts": false`) to disable
the behavior even when default credentials exist.

### Restricting ad-hoc targets (management prefix allowlist)

To keep the server safe, you can restrict which addresses an ad-hoc `device`
argument may reach with an **allowlist of management prefixes**. When set, an
ad-hoc host is accepted only if it is an IP address inside one of the prefixes;
a hostname or an out-of-range IP is rejected. Predefined devices in the
inventory are always reachable and are **not** subject to the allowlist.

- **JSON mode:** a top-level `"allowed_management_prefixes"` array of CIDRs.

  ```json
  {
    "allowed_management_prefixes": ["192.0.2.0/24", "10.10.0.0/16"],
    "defaults": { "username": "admin", "password": "secret" }
  }
  ```

- **Env mode:** `EOS_ALLOWED_MANAGEMENT_PREFIXES`, a comma-separated list of
  CIDRs (e.g. `EOS_ALLOWED_MANAGEMENT_PREFIXES="192.0.2.0/24,10.10.0.0/16"`).

Leaving the allowlist empty (the default) keeps the previous behavior — any
host is accepted for ad-hoc access.

`list_devices` reports whether ad-hoc access is enabled.

## Run

```sh
EOS_HOST=192.0.2.10 EOS_USERNAME=admin EOS_PASSWORD=secret \
  EOS_TLS_SKIP_VERIFY=true ./arista-eos-mcp
```

The server speaks MCP over stdio.

## Run with Docker / Docker Compose

The server supports two MCP transports, selected with `MCP_TRANSPORT`:

- `stdio` (default) — the MCP client spawns the process and talks over
  stdin/stdout. Use `docker run -i`.
- `http` — Streamable HTTP, suitable for a long-lived service. Use
  `docker compose up`. Listens on `MCP_HTTP_ADDR` (default `:8080`).

### Docker Compose (HTTP transport, long-running service)

1. Create your inventory at `./devices.json` (copy `devices.example.json`).
2. Start it:

   ```sh
   docker compose up -d --build
   ```

   This runs the server with `MCP_TRANSPORT=http` on `http://localhost:8080`.
   Point an HTTP-capable MCP client at that URL. For Claude Code:

   ```sh
   claude mcp add --transport http arista-eos http://localhost:8080
   ```

### Docker (stdio transport, client-spawned)

Build once, then have the MCP client launch a container per session:

```sh
docker build -t arista-eos-mcp:latest .
```

```json
{
  "mcpServers": {
    "arista-eos": {
      "command": "docker",
      "args": [
        "run", "-i", "--rm",
        "-e", "EOS_CONFIG=/config/devices.json",
        "-v", "/absolute/path/to/devices.json:/config/devices.json:ro",
        "arista-eos-mcp:latest"
      ]
    }
  }
}
```

> The `-i` flag is required for the stdio transport. `MCP_TRANSPORT` defaults to
> `stdio`, so no transport env var is needed for this mode.

## Register with Claude Desktop / Claude Code

Add to your MCP client config (e.g. `claude_desktop_config.json`, or via
`claude mcp add`). Use an **absolute path** for both the binary and the config
file.

Multi-device (recommended):

```json
{
  "mcpServers": {
    "arista-eos": {
      "command": "/absolute/path/to/arista-eos-mcp",
      "env": {
        "EOS_CONFIG": "/absolute/path/to/devices.json"
      }
    }
  }
}
```

Single device:

```json
{
  "mcpServers": {
    "arista-eos": {
      "command": "/absolute/path/to/arista-eos-mcp",
      "env": {
        "EOS_HOST": "192.0.2.10",
        "EOS_USERNAME": "admin",
        "EOS_PASSWORD": "secret"
      }
    }
  }
}
```

For Claude Code:

```sh
# multi-device
claude mcp add arista-eos \
  -e EOS_CONFIG=/absolute/path/to/devices.json \
  -- /absolute/path/to/arista-eos-mcp

# single device
claude mcp add arista-eos \
  -e EOS_HOST=192.0.2.10 -e EOS_USERNAME=admin -e EOS_PASSWORD=secret \
  -- /absolute/path/to/arista-eos-mcp
```

Once connected, ask Claude things like "list the devices", or "show the BGP
summary on leaf1". Every tool accepts a `device` argument; it is optional only
when a single device is configured.

## Enabling APIs on EOS

```
! eAPI
management api http-commands
   no shutdown
   protocol https

! gNMI
management api gnmi
   transport grpc default
```

## Tools

All tools except `list_devices` accept an optional `device` argument selecting
the target by name (required when more than one device is configured).

**Inventory & generic**

| Tool                 | Description                                                  |
| -------------------- | ------------------------------------------------------------ |
| `list_devices`       | List configured device names                                  |
| `get_show_data`      | Fetch a curated read-only dataset by `topic` (see table below) |
| `run_show_command`   | Run any read-only command (`json` or `text` output)           |
| `get_running_config` | Running configuration as text                                 |
| `get_interfaces`     | `show interfaces` (optional interface filter)                 |
| `get_ip_route`       | `show ip route` (optional prefix / VRF filter)                |

**`get_show_data` topics** (eAPI)

`get_show_data` replaces what used to be 57 individual `get_*` tools. Collapsing
them into one tool with a `topic` enum cut the `tools/list` payload every client
puts in the model's context from ~42 kB to ~14 kB.

```json
{ "topic": "bgp_summary", "device": "192.0.2.10" }
```

| Topic                  | Command                        |
| ---------------------- | ------------------------------ |
| `version`              | `show version`                 |
| `interfaces_status`    | `show interfaces status`       |
| `interface_counters`   | `show interfaces counters`     |
| `mac_address_table`    | `show mac address-table`       |
| `arp_table`            | `show ip arp`                  |
| `vlans`                | `show vlan`                    |
| `lldp_neighbors`       | `show lldp neighbors`          |
| `port_channels`        | `show port-channel summary`    |
| `mlag`                 | `show mlag`                    |
| `spanning_tree`        | `show spanning-tree`           |
| `bgp_summary`          | `show ip bgp summary`          |
| `bgp_neighbors`        | `show ip bgp neighbors`        |
| `ospf_neighbors`       | `show ip ospf neighbor`        |
| `transceivers`         | `show interfaces transceiver`  |
| `environment`          | `show environment all`         |
| `ntp_status`           | `show ntp status`              |
| `logging`              | `show logging last 100`        |
| `processes`            | `show processes top once`      |
| `vxlan_interface`      | `show interfaces vxlan 1`      |
| `vxlan_vtep`           | `show vxlan vtep`              |
| `vxlan_address_table`  | `show vxlan address-table`     |
| `bgp_evpn_summary`     | `show bgp evpn summary`        |
| `bgp_evpn`             | `show bgp evpn`                |
| `ip_interface_brief`   | `show ip interface brief`      |
| `vrfs`                 | `show vrf`                     |
| `bfd_peers`            | `show bfd peers`               |
| `ipv6_neighbors`       | `show ipv6 neighbors`          |
| `ipv6_route`           | `show ipv6 route`              |
| `route_summary`        | `show ip route summary`        |
| `ospf`                 | `show ip ospf`                 |
| `isis_neighbors`       | `show isis neighbors`          |
| `pim_neighbors`        | `show ip pim neighbor`         |
| `igmp_snooping_groups` | `show ip igmp snooping groups` |
| `lacp_peers`           | `show lacp peer`               |
| `ip_access_lists`      | `show ip access-lists`         |
| `inventory`            | `show inventory`               |
| `reload_cause`         | `show reload cause`            |
| `hardware_capacity`    | `show hardware capacity`       |
| `clock`                | `show clock`                   |
| `qos_interfaces`       | `show qos interfaces`          |
| `storm_control`        | `show storm-control`           |
| `dhcp_relay`           | `show ip dhcp relay`           |
| `sflow`                | `show sflow`                   |
| `vrrp`                 | `show vrrp`                    |
| `varp`                 | `show ip virtual-router`       |
| `ptp`                  | `show ptp`                     |
| `aaa`                  | `show aaa`                     |
| `tacacs`               | `show tacacs`                  |
| `radius`               | `show radius`                  |
| `snmp`                 | `show snmp`                    |
| `snmp_host`            | `show snmp notification host`  |
| `macsec`               | `show mac security interface`  |
| `macsec_counters`      | `show mac security counters`   |
| `tunnel_fib`           | `show tunnel fib`              |
| `mpls_lfib`            | `show mpls lfib route`         |
| `ldp_neighbors`        | `show mpls ldp neighbor`       |
| `vxlan_counters`       | `show vxlan counters vtep`     |

**Aggregation & diagnostics**

| Tool                | Description                                                       |
| ------------------- | ---------------------------------------------------------------- |
| `get_device_health` | Combined report: version, environment, interfaces, LAG, MLAG, BGP |
| `compare_config`    | Diff the running-config of two devices (`device_a`, `device_b`)  |
| `compare_command`   | Run one command on two devices and diff the output               |

**Active diagnostics** (read-only, still within the safety allow-list)

| Tool                        | Description                                            |
| --------------------------- | ------------------------------------------------------ |
| `ping_host`                 | Ping from a device (`destination`, opt. `count`/`vrf`/`source`) |
| `traceroute_host`           | Traceroute from a device (`destination`, opt. `vrf`)   |
| `get_bgp_advertised_routes` | Routes advertised to a `neighbor`                      |
| `get_bgp_received_routes`   | Routes received from a `neighbor`                      |

**Fleet (multi-device)**

| Tool                | Description                                                       |
| ------------------- | ---------------------------------------------------------------- |
| `fleet_run_command` | Run one command across many devices in parallel (`devices` opt.) |
| `fleet_get_version` | Collect `show version` across many devices                       |

**gNMI**

| Tool                | Description                                                       |
| ------------------- | ---------------------------------------------------------------- |
| `gnmi_get`          | Get one or more paths                                            |
| `gnmi_capabilities` | Supported models and encodings                                   |
| `gnmi_subscribe`    | Collect telemetry over a bounded window (`once`/`sample`/`on-change`) |

## Resources

Per device, the server exposes two read-only MCP resources:

- `eos://<device>/running-config` — running configuration (text)
- `eos://<device>/version` — `show version` output (JSON)

## Prompts

Reusable troubleshooting prompt templates:

| Prompt                  | Arguments                       | Purpose                                  |
| ----------------------- | ------------------------------- | ---------------------------------------- |
| `troubleshoot_interface`| `device`, `interface`           | Investigate a problematic interface      |
| `troubleshoot_bgp`      | `device`, `neighbor` (optional) | Investigate BGP session problems         |
| `device_health_review`  | `device`                        | Full health review and summary           |
| `compare_devices`       | `device_a`, `device_b`          | Compare two devices and explain the diff |

## Safety model

In read-only mode (the default) every eAPI command is validated against an
allow-list: only `show ...` and a small set of diagnostics (`ping`,
`traceroute`) are permitted. Commands starting with `configure`, `write`,
`copy`, `delete`, `reload`, `clear`, `no `, `bash`, etc. are rejected, as are
pipes that redirect/tee to a file. `ping` and `traceroute` are permitted as
read-only diagnostics. gNMI is restricted to `Get`/`Capabilities`/`Subscribe`
(no `Set`); subscriptions are bounded by a time window (max 60s).

To allow write operations in a future version, set `EOS_READ_ONLY=false` — but
no write tools are currently implemented.

## Development

```sh
go test ./...
go vet ./...
```

### Validating the tool catalog

A model can only call a tool correctly if the catalog it reads is correct, so
that is checked in CI rather than by review. The checks run in three layers,
each catching something the one before it cannot.

**1. Invariants** — [`internal/tools/show_test.go`](internal/tools/show_test.go)

Each catalog entry is unique, named in lowercase `snake_case`, runs a plain
`show` command that the read-only guard in
[`internal/safety`](internal/safety/safety.go) accepts, and declares a valid
encoding. No two topics run the same command. The tool description and the
`topic` enum are rebuilt from the catalog and compared, which proves both are
still generated by ranging over the slice in declared order — ranging over the
dispatch map instead would randomize them per process and break prompt caching
in every client. The hand-written topic table in this README is parsed and
compared against the catalog too
([`readme_test.go`](internal/tools/readme_test.go)) — it is how a human decides
whether a topic already exists, so it is not allowed to drift.

**2. Contract** — [`internal/tools/catalog_contract_test.go`](internal/tools/catalog_contract_test.go)

Every topic is called through a real MCP client session against a fake eAPI
endpoint, and the request it produces is inspected. This proves each enum value
passes schema validation, dispatches, survives the read-only guard, and arrives
as exactly one command — the one its catalog entry declares, in the encoding it
declares. `get_ip_route`, which builds its command from arguments, is checked
the same way for each combination of `prefix` and `vrf`.

The whole tool surface is linted alongside it in
[`internal/tools/toolsurface_test.go`](internal/tools/toolsurface_test.go):
input schemas must resolve as JSON Schema, close `additionalProperties`,
declare every required field, and describe every property. Descriptions, prompt
bodies, and the server instructions may only name tools and prompts that are
actually registered — a renamed tool that leaves a stale reference behind sends
the model to call something that does not exist.

[`internal/tools/testdata/tool-surface.golden.json`](internal/tools/testdata/tool-surface.golden.json)
holds a snapshot of everything the model is told: the instructions, every tool
definition, every prompt. Changing the catalog changes that file, so the effect
on what a model sees shows up as a reviewable diff in the same pull request.
After an intended change, regenerate it:

```sh
go test ./internal/tools/ -run TestToolSurfaceGolden -update
```

**3. Live device** — [`internal/tools/catalog_live_test.go`](internal/tools/catalog_live_test.go)

Layers 1 and 2 cannot tell whether a command is one EOS actually accepts: the
catalog is the only source of truth for the command string, so a typo matches
itself. Only a device settles it. This layer is behind the `live` build tag and
runs every catalog command against a real switch:

```sh
EOS_HOST=192.0.2.10 EOS_USERNAME=admin EOS_PASSWORD=… EOS_EAPI_TRANSPORT=https \
  go test -tags=live -count=1 ./internal/tools/ -run AgainstDevice -v
```

A command EOS rejects as invalid, or cannot render in the declared encoding,
fails the test. An error that reflects device state — a feature not configured,
an empty table — is reported but does not fail. Commands genuinely unsupported
on a platform can be baselined per device, and the test fails again if one of
them starts working, so the baseline cannot rot:

```sh
EOS_CATALOG_EXPECTED_UNSUPPORTED=macsec,macsec_counters,ptp
```

| Variable                           | Purpose                                           |
| ---------------------------------- | ------------------------------------------------- |
| `EOS_CATALOG_DEVICE`               | Which device to use from a multi-device inventory |
| `EOS_CATALOG_EXPECTED_UNSUPPORTED` | Topics this platform does not implement           |

Layers 1 and 2 run on every push and pull request in the `tool catalog` job of
[`.github/workflows/test.yml`](.github/workflows/test.yml). Layer 3 runs weekly
and on demand from
[`.github/workflows/catalog-live.yml`](.github/workflows/catalog-live.yml),
which needs `EOS_HOST`/`EOS_USERNAME`/`EOS_PASSWORD` secrets and, since a lab
switch is rarely reachable from a GitHub-hosted runner, usually an
`EOS_LAB_RUNNER` variable naming a self-hosted runner label.

### Adding a `get_show_data` topic

Add one line to `showCatalog` in [`internal/tools/show.go`](internal/tools/show.go)
— it drives the enum, the hint table, and dispatch — add the matching row to the
topic table above, then:

```sh
go test ./internal/tools/...                                       # layers 1 and 2
go test ./internal/tools/ -run TestToolSurfaceGolden -update       # refresh the snapshot
```

Commit the snapshot with the change. Give the topic a `hint` only when its name
does not already say what it returns; every hint costs context in every
conversation. Verify the command against a device with layer 3 before merging.

## Releases

Releases are automated with
[release-please](https://github.com/googleapis/release-please). Commits to
`master` that follow [Conventional Commits](https://www.conventionalcommits.org/)
(`feat:`, `fix:`, `feat!:`/`BREAKING CHANGE:`, …) drive the version bump.

On each push to `master` the workflow maintains a **release PR** that updates
`CHANGELOG.md`, the version in [`cmd/arista-eos-mcp/main.go`](cmd/arista-eos-mcp/main.go)
(marked with `// x-release-please-version`), and `.release-please-manifest.json`.
Merging that PR tags the commit (`vX.Y.Z`) and publishes a GitHub Release. The
same workflow then runs [GoReleaser](https://goreleaser.com)
([`.goreleaser.yaml`](.goreleaser.yaml)) to cross-compile the binaries and
attach the archives + `checksums.txt` to that release.

Config lives in [`release-please-config.json`](release-please-config.json),
[`.goreleaser.yaml`](.goreleaser.yaml), and the workflow in
[`.github/workflows/release-please.yml`](.github/workflows/release-please.yml).

## License

[MIT](LICENSE) © Takaharu Umeda
