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
