package manager

import (
	"testing"

	"github.com/x86taka/arista-eos-mcp/internal/config"
)

func testConfig() *config.Config {
	return &config.Config{
		Devices: []config.Device{
			{Name: "spine1", Host: "192.0.2.100", Username: "admin", Password: "devpass", EAPITransport: "http", EAPIPort: 80},
		},
		Defaults: &config.Defaults{
			Username: "admin", Password: "defaultpass", EAPITransport: "http", EAPIPort: 80,
		},
	}
}

func TestResolveByName(t *testing.T) {
	m, err := New(testConfig())
	if err != nil {
		t.Fatal(err)
	}
	d, err := m.Device("spine1")
	if err != nil {
		t.Fatal(err)
	}
	if d.Password != "devpass" {
		t.Fatalf("by name: got password %q, want devpass", d.Password)
	}
}

// A request addressed by the device's host IP must reuse that device's
// credentials, not the shared ad-hoc defaults.
func TestResolveByHostUsesDeviceCredentials(t *testing.T) {
	m, err := New(testConfig())
	if err != nil {
		t.Fatal(err)
	}
	d, err := m.Device("192.0.2.100")
	if err != nil {
		t.Fatal(err)
	}
	if d.Name != "spine1" {
		t.Fatalf("by host: got device %q, want spine1", d.Name)
	}
	if d.Password != "devpass" {
		t.Fatalf("by host: got password %q, want the configured device password devpass (not the ad-hoc default)", d.Password)
	}
}

// An unknown host still falls back to ad-hoc default credentials.
func TestResolveUnknownHostUsesDefaults(t *testing.T) {
	m, err := New(testConfig())
	if err != nil {
		t.Fatal(err)
	}
	d, err := m.Device("192.0.2.99")
	if err != nil {
		t.Fatal(err)
	}
	if d.Password != "defaultpass" {
		t.Fatalf("unknown host: got password %q, want defaultpass", d.Password)
	}
}

// prefixConfig returns a config whose ad-hoc hosts are restricted to a
// management prefix.
func prefixConfig(t *testing.T) *config.Config {
	t.Helper()
	c := testConfig()
	c.AllowedManagementPrefixes = []string{"192.0.2.0/24"}
	if err := c.ResolveManagementPrefixes(); err != nil {
		t.Fatal(err)
	}
	return c
}

// An ad-hoc host inside an allowed prefix is accepted.
func TestResolveAdHocHostWithinAllowedPrefix(t *testing.T) {
	m, err := New(prefixConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	d, err := m.Device("192.0.2.50")
	if err != nil {
		t.Fatalf("host within allowed prefix should resolve: %v", err)
	}
	if d.Password != "defaultpass" {
		t.Fatalf("got password %q, want defaultpass", d.Password)
	}
}

// An ad-hoc host outside every allowed prefix is rejected.
func TestResolveAdHocHostOutsideAllowedPrefix(t *testing.T) {
	m, err := New(prefixConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Device("198.51.100.5"); err == nil {
		t.Fatal("host outside allowed prefix should be rejected")
	}
}

// A hostname (non-IP) is rejected when prefixes are configured.
func TestResolveAdHocHostnameRejectedWithPrefixes(t *testing.T) {
	m, err := New(prefixConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Device("switch1.example.com"); err == nil {
		t.Fatal("hostname should be rejected when management prefixes are configured")
	}
}

// A configured device is reachable by name even when it sits outside the
// allowlist — the allowlist only gates ad-hoc hosts.
func TestConfiguredDeviceBypassesPrefixAllowlist(t *testing.T) {
	c := testConfig()
	c.Devices[0].Host = "10.0.0.1" // outside the allowed prefix below
	c.AllowedManagementPrefixes = []string{"192.0.2.0/24"}
	if err := c.ResolveManagementPrefixes(); err != nil {
		t.Fatal(err)
	}
	m, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Device("spine1"); err != nil {
		t.Fatalf("configured device should resolve regardless of allowlist: %v", err)
	}
	if _, err := m.Device("10.0.0.1"); err != nil {
		t.Fatalf("configured device should resolve by host regardless of allowlist: %v", err)
	}
}
