// Package manager owns the configured device inventory and lazily creates and
// caches per-device eAPI and gNMI clients.
package manager

import (
	"fmt"
	"strings"
	"sync"

	"github.com/x86taka/arista-eos-mcp/internal/config"
	"github.com/x86taka/arista-eos-mcp/internal/eapi"
	"github.com/x86taka/arista-eos-mcp/internal/gnmi"
)

// Manager resolves device names to clients. It is safe for concurrent use.
type Manager struct {
	cfg     *config.Config
	devices map[string]*config.Device
	order   []string // device names in configuration order

	mu          sync.Mutex
	dynamic     map[string]*config.Device // ad-hoc hosts created on demand
	eapiClients map[string]*eapi.Client
	gnmiClients map[string]*gnmi.Client
}

// New builds a Manager from cfg.
func New(cfg *config.Config) (*Manager, error) {
	m := &Manager{
		cfg:         cfg,
		devices:     make(map[string]*config.Device, len(cfg.Devices)),
		dynamic:     make(map[string]*config.Device),
		eapiClients: make(map[string]*eapi.Client),
		gnmiClients: make(map[string]*gnmi.Client),
	}
	for i := range cfg.Devices {
		d := &cfg.Devices[i]
		m.devices[d.Name] = d
		m.order = append(m.order, d.Name)
	}
	if len(m.order) == 0 && !cfg.DynamicHostsEnabled() {
		return nil, fmt.Errorf("no devices configured and ad-hoc hosts disabled")
	}
	return m, nil
}

// Names returns configured device names in configuration order.
func (m *Manager) Names() []string {
	return append([]string(nil), m.order...)
}

// DynamicHostsEnabled reports whether ad-hoc hosts are accepted.
func (m *Manager) DynamicHostsEnabled() bool {
	return m.cfg.DynamicHostsEnabled()
}

// Device returns the device metadata for name (after resolution).
func (m *Manager) Device(name string) (*config.Device, error) {
	return m.resolve(name)
}

// resolve maps a device name to a configured device. If the name is not in the
// inventory and ad-hoc hosts are enabled, it is treated as a hostname/IP and an
// in-memory device using the shared default credentials is created and cached.
// An empty name is allowed only when exactly one device is configured.
func (m *Manager) resolve(name string) (*config.Device, error) {
	if name == "" {
		if len(m.order) == 1 {
			return m.devices[m.order[0]], nil
		}
		hint := "the 'device' argument is required"
		if m.DynamicHostsEnabled() {
			hint += " (a device name, or a hostname/IP for ad-hoc access)"
		}
		if len(m.order) > 0 {
			hint += "; configured devices: " + strings.Join(m.order, ", ")
		}
		return nil, fmt.Errorf("%s", hint)
	}
	if d, ok := m.devices[name]; ok {
		return d, nil
	}

	if m.DynamicHostsEnabled() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if d, ok := m.dynamic[name]; ok {
			return d, nil
		}
		d := m.cfg.Defaults.NewDynamicDevice(name)
		if d == nil {
			return nil, fmt.Errorf("cannot create ad-hoc host %q: default credentials are not configured", name)
		}
		m.dynamic[name] = d
		return d, nil
	}

	msg := fmt.Sprintf("unknown device %q", name)
	if len(m.order) > 0 {
		msg += "; configured devices: " + strings.Join(m.order, ", ")
	}
	msg += " (ad-hoc hosts are disabled; configure default credentials to enable)"
	return nil, fmt.Errorf("%s", msg)
}

// EAPI returns a cached eAPI client for the named device, creating it on first
// use.
func (m *Manager) EAPI(name string) (*eapi.Client, error) {
	dev, err := m.resolve(name)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if c, ok := m.eapiClients[dev.Name]; ok {
		return c, nil
	}
	c, err := eapi.New(dev, m.cfg.ReadOnly)
	if err != nil {
		return nil, err
	}
	m.eapiClients[dev.Name] = c
	return c, nil
}

// GNMI returns a cached gNMI client for the named device, creating it on first
// use.
func (m *Manager) GNMI(name string) (*gnmi.Client, error) {
	dev, err := m.resolve(name)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if c, ok := m.gnmiClients[dev.Name]; ok {
		return c, nil
	}
	c, err := gnmi.New(dev, m.cfg.Timeout)
	if err != nil {
		return nil, err
	}
	m.gnmiClients[dev.Name] = c
	return c, nil
}
