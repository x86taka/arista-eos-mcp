// Package config loads connection settings for the Arista EOS MCP server.
//
// Two sources are supported:
//   - A JSON inventory file (path in EOS_CONFIG) describing multiple devices.
//   - Legacy single-device environment variables (EOS_HOST, EOS_USERNAME, ...),
//     used when EOS_CONFIG is unset. This produces one device named "default".
package config

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

// Device holds the settings needed to talk to a single EOS device over eAPI
// and/or gNMI.
type Device struct {
	Name     string `json:"name"`
	Host     string `json:"host"`
	Username string `json:"username"`
	Password string `json:"password"`

	// eAPI settings.
	EAPITransport  string `json:"eapi_transport"`  // "https" (default), "http", "http_local", "socket"
	EAPIPort       int    `json:"eapi_port"`       // 0 = transport default (https=443, http=80)
	EnablePassword string `json:"enable_password"` // enable (privileged exec) password; empty = none

	// gNMI settings.
	GNMIPort     int    `json:"gnmi_port"`     // default 6030
	GNMIInsecure *bool  `json:"gnmi_insecure"` // plaintext (no TLS) gRPC; nil defaults to true
	GNMITLSCA    string `json:"gnmi_tls_ca"`   // optional path to CA cert

	// TLS / common.
	TLSSkipVerify *bool `json:"tls_skip_verify"` // skip server cert verification; nil defaults to true
}

// Defaults holds shared credentials and connection settings used for ad-hoc
// hosts — hosts that are not predefined in the inventory but addressed directly
// by hostname/IP through a tool's "device" argument.
type Defaults struct {
	Username       string `json:"username"`
	Password       string `json:"password"`
	EnablePassword string `json:"enable_password"`
	EAPITransport  string `json:"eapi_transport"`
	EAPIPort       int    `json:"eapi_port"`
	GNMIPort       int    `json:"gnmi_port"`
	GNMIInsecure   *bool  `json:"gnmi_insecure"`
	GNMITLSCA      string `json:"gnmi_tls_ca"`
	TLSSkipVerify  *bool  `json:"tls_skip_verify"`
}

// Config is the top-level configuration: a set of devices plus global settings.
type Config struct {
	Devices  []Device      `json:"devices"`
	Defaults *Defaults     `json:"defaults"` // shared credentials for ad-hoc hosts
	ReadOnly bool          `json:"-"`        // reject write/state-changing commands (resolved)
	Timeout  time.Duration `json:"-"`        // per-request timeout (see TimeoutSeconds in JSON)

	// ReadOnlyRaw is the JSON form of ReadOnly; nil defaults to true (safe).
	ReadOnlyRaw *bool `json:"read_only"`

	// AllowDynamicHosts, when set, decides whether ad-hoc hosts are accepted.
	// nil (the default) means "enabled if Defaults credentials are present".
	AllowDynamicHosts *bool `json:"allow_dynamic_hosts"`

	// AllowedManagementPrefixes restricts which addresses may be reached as
	// ad-hoc hosts. Each entry is an IPv4/IPv6 CIDR (e.g. "192.0.2.0/24").
	// When non-empty, an ad-hoc host is accepted only if it is a management IP
	// address inside one of these prefixes; hostnames and out-of-range IPs are
	// rejected. Empty (the default) means no restriction. Configured devices
	// are always reachable and are not subject to this allowlist.
	AllowedManagementPrefixes []string `json:"allowed_management_prefixes"`

	// TimeoutSeconds is the JSON form of Timeout.
	TimeoutSeconds int `json:"timeout_seconds"`

	// mgmtNets is the parsed form of AllowedManagementPrefixes (resolved).
	mgmtNets []*net.IPNet

	// mgmtResolved records that ResolveManagementPrefixes has run, so the
	// allowlist check can fail closed if a Config carrying prefixes was never
	// resolved.
	mgmtResolved bool
}

// ResolveManagementPrefixes parses AllowedManagementPrefixes into mgmtNets,
// returning an error on any malformed CIDR entry.
func (c *Config) ResolveManagementPrefixes() error {
	c.mgmtNets = nil
	for _, p := range c.AllowedManagementPrefixes {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		_, n, err := net.ParseCIDR(p)
		if err != nil {
			return fmt.Errorf("invalid allowed_management_prefixes entry %q: %w", p, err)
		}
		c.mgmtNets = append(c.mgmtNets, n)
	}
	c.mgmtResolved = true
	return nil
}

// AllowedManagementHost reports whether host may be used as an ad-hoc target.
// When management prefixes are configured, host must be a management IP address
// within one of them; otherwise (no prefixes configured) any host is allowed.
func (c *Config) AllowedManagementHost(host string) error {
	// Fail closed: if prefixes are configured but were never resolved, refuse
	// rather than silently allowing every host and bypassing the security gate.
	if !c.mgmtResolved && len(c.AllowedManagementPrefixes) > 0 {
		return fmt.Errorf("management prefix allowlist is configured but not initialized; call ResolveManagementPrefixes() first")
	}
	if len(c.mgmtNets) == 0 {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("ad-hoc host %q must be a management IP address within an allowed prefix (%s), not a hostname",
			host, strings.Join(c.AllowedManagementPrefixes, ", "))
	}
	for _, n := range c.mgmtNets {
		if n.Contains(ip) {
			return nil
		}
	}
	return fmt.Errorf("ad-hoc host %q is not within an allowed management prefix (%s)",
		host, strings.Join(c.AllowedManagementPrefixes, ", "))
}

// DynamicHostsEnabled reports whether ad-hoc hosts may be created from default
// credentials.
func (c *Config) DynamicHostsEnabled() bool {
	if c.AllowDynamicHosts != nil {
		return *c.AllowDynamicHosts && c.Defaults.valid()
	}
	return c.Defaults.valid()
}

func (d *Defaults) valid() bool {
	return d != nil && d.Username != "" && d.Password != ""
}

// NewDynamicDevice builds an in-memory Device for an ad-hoc host using these
// defaults. Returns nil if defaults are not usable.
func (d *Defaults) NewDynamicDevice(host string) *Device {
	if !d.valid() {
		return nil
	}
	dev := &Device{
		Name:           host,
		Host:           host,
		Username:       d.Username,
		Password:       d.Password,
		EnablePassword: d.EnablePassword,
		EAPITransport:  d.EAPITransport,
		EAPIPort:       d.EAPIPort,
		GNMIPort:       d.GNMIPort,
		GNMIInsecure:   d.GNMIInsecure,
		GNMITLSCA:      d.GNMITLSCA,
		TLSSkipVerify:  d.TLSSkipVerify,
	}
	applyDeviceDefaults(dev)
	return dev
}

// GNMIInsecureOrDefault returns the effective gNMI insecure flag (default true).
func (d *Device) GNMIInsecureOrDefault() bool {
	if d.GNMIInsecure == nil {
		return true
	}
	return *d.GNMIInsecure
}

// TLSSkipVerifyOrDefault returns the effective TLS skip-verify flag (default true).
func (d *Device) TLSSkipVerifyOrDefault() bool {
	if d.TLSSkipVerify == nil {
		return true
	}
	return *d.TLSSkipVerify
}

// Load builds a Config from EOS_CONFIG (JSON file) if set, otherwise from the
// legacy single-device environment variables.
func Load() (*Config, error) {
	if path := os.Getenv("EOS_CONFIG"); path != "" {
		return loadFile(path)
	}
	return loadEnv()
}

func loadFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read EOS_CONFIG file %q: %w", path, err)
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("failed to parse EOS_CONFIG file %q: %w", path, err)
	}
	if c.TimeoutSeconds <= 0 {
		c.TimeoutSeconds = 30
	}
	c.Timeout = time.Duration(c.TimeoutSeconds) * time.Second
	c.ReadOnly = c.ReadOnlyRaw == nil || *c.ReadOnlyRaw // default true (safe)

	if err := c.ResolveManagementPrefixes(); err != nil {
		return nil, err
	}

	if c.Defaults != nil {
		applyDefaultsConn(c.Defaults)
	}
	if len(c.Devices) == 0 && !c.DynamicHostsEnabled() {
		return nil, fmt.Errorf("EOS_CONFIG file %q defines no devices and no usable default credentials (set \"defaults\" with username/password to allow ad-hoc hosts)", path)
	}

	seen := make(map[string]bool, len(c.Devices))
	for i := range c.Devices {
		d := &c.Devices[i]
		applyDeviceDefaults(d)
		if d.Name == "" {
			return nil, fmt.Errorf("device #%d is missing a name", i+1)
		}
		if seen[d.Name] {
			return nil, fmt.Errorf("duplicate device name %q", d.Name)
		}
		seen[d.Name] = true
		if err := validateDevice(d); err != nil {
			return nil, err
		}
	}
	return &c, nil
}

// applyDefaultsConn fills connection defaults for ad-hoc hosts.
func applyDefaultsConn(d *Defaults) {
	if d.EAPITransport == "" {
		d.EAPITransport = "https"
	}
	if d.EAPIPort == 0 && d.EAPITransport == "https" {
		d.EAPIPort = 443
	}
	if d.GNMIPort == 0 {
		d.GNMIPort = 6030
	}
	if d.GNMIInsecure == nil {
		d.GNMIInsecure = boolPtr(true)
	}
	if d.TLSSkipVerify == nil {
		d.TLSSkipVerify = boolPtr(true)
	}
}

func loadEnv() (*Config, error) {
	timeout := getenvInt("EOS_TIMEOUT_SECONDS", 30)

	// Shared default credentials for ad-hoc hosts. Fall back to the primary
	// EOS_USERNAME/EOS_PASSWORD so a single set of credentials works for both
	// the (optional) predefined host and any ad-hoc host.
	defaults := &Defaults{
		Username:       getenvDefault("EOS_DEFAULT_USERNAME", os.Getenv("EOS_USERNAME")),
		Password:       getenvDefault("EOS_DEFAULT_PASSWORD", os.Getenv("EOS_PASSWORD")),
		EnablePassword: getenvDefault("EOS_DEFAULT_ENABLE_PASSWORD", os.Getenv("EOS_ENABLE_PASSWORD")),
		EAPITransport:  getenvDefault("EOS_EAPI_TRANSPORT", "https"),
		EAPIPort:       getenvInt("EOS_EAPI_PORT", 443),
		GNMIPort:       getenvInt("EOS_GNMI_PORT", 6030),
		GNMIInsecure:   boolPtr(getenvBool("EOS_GNMI_INSECURE", true)),
		GNMITLSCA:      os.Getenv("EOS_GNMI_TLS_CA"),
		TLSSkipVerify:  boolPtr(getenvBool("EOS_TLS_SKIP_VERIFY", true)),
	}
	applyDefaultsConn(defaults)

	c := &Config{
		Defaults:       defaults,
		ReadOnly:       getenvBool("EOS_READ_ONLY", true),
		Timeout:        time.Duration(timeout) * time.Second,
		TimeoutSeconds: timeout,
	}
	if v := os.Getenv("EOS_ALLOW_DYNAMIC_HOSTS"); v != "" {
		c.AllowDynamicHosts = boolPtr(getenvBool("EOS_ALLOW_DYNAMIC_HOSTS", true))
	}
	if v := os.Getenv("EOS_ALLOWED_MANAGEMENT_PREFIXES"); v != "" {
		for _, p := range strings.Split(v, ",") {
			if p = strings.TrimSpace(p); p != "" {
				c.AllowedManagementPrefixes = append(c.AllowedManagementPrefixes, p)
			}
		}
	}
	if err := c.ResolveManagementPrefixes(); err != nil {
		return nil, err
	}

	// A predefined host is optional. When present, register it as a device.
	if host := os.Getenv("EOS_HOST"); host != "" {
		d := Device{
			Name:           getenvDefault("EOS_DEVICE_NAME", "default"),
			Host:           host,
			Username:       getenvDefault("EOS_USERNAME", defaults.Username),
			Password:       getenvDefault("EOS_PASSWORD", defaults.Password),
			EAPITransport:  defaults.EAPITransport,
			EAPIPort:       defaults.EAPIPort,
			EnablePassword: getenvDefault("EOS_ENABLE_PASSWORD", defaults.EnablePassword),
			GNMIPort:       defaults.GNMIPort,
			GNMIInsecure:   boolPtr(getenvBool("EOS_GNMI_INSECURE", true)),
			GNMITLSCA:      defaults.GNMITLSCA,
			TLSSkipVerify:  boolPtr(getenvBool("EOS_TLS_SKIP_VERIFY", true)),
		}
		applyDeviceDefaults(&d)
		if err := validateDevice(&d); err != nil {
			return nil, err
		}
		c.Devices = []Device{d}
	}

	if len(c.Devices) == 0 && !c.DynamicHostsEnabled() {
		return nil, fmt.Errorf("no device configured: set EOS_HOST, or provide EOS_USERNAME/EOS_PASSWORD (or EOS_DEFAULT_USERNAME/EOS_DEFAULT_PASSWORD) to allow ad-hoc hosts")
	}
	return c, nil
}

func applyDeviceDefaults(d *Device) {
	if d.EAPITransport == "" {
		d.EAPITransport = "https"
	}
	if d.EAPIPort == 0 && d.EAPITransport == "https" {
		d.EAPIPort = 443
	}
	if d.GNMIPort == 0 {
		d.GNMIPort = 6030
	}
}

func validateDevice(d *Device) error {
	var missing []string
	if d.Host == "" {
		missing = append(missing, "host")
	}
	if d.Username == "" {
		missing = append(missing, "username")
	}
	if d.Password == "" {
		missing = append(missing, "password")
	}
	if len(missing) > 0 {
		return fmt.Errorf("device %q is missing required fields: %s", d.Name, strings.Join(missing, ", "))
	}
	return nil
}

func boolPtr(b bool) *bool { return &b }

func getenvDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getenvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func getenvBool(key string, def bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}
