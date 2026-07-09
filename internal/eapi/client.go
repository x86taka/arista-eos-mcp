// Package eapi wraps the Arista goeapi client to run read-only show commands
// against an EOS device over eAPI.
package eapi

import (
	"errors"
	"fmt"
	"strings"

	"github.com/aristanetworks/goeapi"

	"github.com/x86taka/arista-eos-mcp/internal/config"
	"github.com/x86taka/arista-eos-mcp/internal/safety"
)

// Client is a thin wrapper around a goeapi Node.
type Client struct {
	node     *goeapi.Node
	readOnly bool
	secrets  []string // credential strings to scrub from returned errors
}

// New connects to the EOS device dev over eAPI. readOnly enables the safety
// guard that rejects write/state-changing commands.
//
// Note on TLS: with the "https" transport, goeapi v1.0.0 always sets
// InsecureSkipVerify=true internally and builds its own http.Transport per
// request, so eAPI over HTTPS never verifies the server certificate. There is
// no library hook to enforce verification, so the device's TLSSkipVerify does
// not affect eAPI (it still governs gNMI). If strict eAPI verification is
// required, front the device with a proxy that terminates TLS, or use the
// "http_local"/"socket" transports on-box.
func New(dev *config.Device, readOnly bool) (*Client, error) {
	secrets := credentialSecrets(dev)
	node, err := goeapi.Connect(dev.EAPITransport, dev.Host, dev.Username, dev.Password, dev.EAPIPort)
	if err != nil {
		return nil, scrubSecrets(fmt.Errorf("eAPI connect to %s failed: %w", dev.Host, err), secrets)
	}

	// When an enable (privileged exec) password is configured, goeapi prepends
	// the {"cmd":"enable","input":<passwd>} sequence to every request.
	if dev.EnablePassword != "" {
		node.EnableAuthentication(dev.EnablePassword)
	}

	return &Client{node: node, readOnly: readOnly, secrets: secrets}, nil
}

// credentialSecrets collects the non-empty credential strings that must never
// appear in an error returned to a caller.
func credentialSecrets(dev *config.Device) []string {
	var s []string
	for _, v := range []string{dev.Password, dev.EnablePassword, dev.Username} {
		if v != "" {
			s = append(s, v)
		}
	}
	return s
}

// scrubSecrets returns an error whose message has every secret replaced with a
// redaction marker. goeapi embeds the username (and, depending on the Go
// version, potentially the password) in the eAPI request URL, which surfaces in
// wrapped net/http errors such as connection timeouts or TLS failures; this
// guarantees those credentials never reach the MCP client.
//
// When a secret is found, the wrapping chain is deliberately severed: a plain
// errors.New value is returned instead of a wrapper that Unwraps to the
// original error. Preserving the chain would re-expose the raw credential via
// errors.Unwrap(err).Error() and %#v / %+v formatting, defeating the redaction.
// No caller inspects these errors with errors.Is/As, so nothing is lost. When
// nothing was redacted the original error (and its chain) is returned unchanged.
func scrubSecrets(err error, secrets []string) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	redacted := msg
	for _, s := range secrets {
		if s != "" {
			redacted = strings.ReplaceAll(redacted, s, "***")
		}
	}
	if redacted == msg {
		return err
	}
	return errors.New(redacted)
}

// Result holds the output of a single command.
type Result struct {
	Command string                 `json:"command"`
	JSON    map[string]interface{} `json:"json,omitempty"`
	Text    string                 `json:"text,omitempty"`
}

// RunCommands executes the given commands and returns their results. encoding
// must be "json" or "text". In read-only mode every command is validated first.
func (c *Client) RunCommands(commands []string, encoding string) ([]Result, error) {
	if encoding == "" {
		encoding = "json"
	}
	if encoding != "json" && encoding != "text" {
		return nil, fmt.Errorf("invalid encoding %q: must be \"json\" or \"text\"", encoding)
	}
	if c.readOnly {
		if err := safety.CheckAll(commands); err != nil {
			return nil, err
		}
	}

	resp, err := c.node.RunCommands(commands, encoding)
	if err != nil {
		return nil, scrubSecrets(fmt.Errorf("eAPI command execution failed: %w", err), c.secrets)
	}
	if resp.Error != nil {
		return nil, scrubSecrets(fmt.Errorf("eAPI returned error: %s", resp.Error.Message), c.secrets)
	}

	results := make([]Result, 0, len(resp.Result))
	for i, r := range resp.Result {
		res := Result{}
		if i < len(commands) {
			res.Command = commands[i]
		}
		if encoding == "text" {
			if out, ok := r["output"].(string); ok {
				res.Text = out
			}
		} else {
			res.JSON = r
		}
		results = append(results, res)
	}
	return results, nil
}

// RunningConfig returns the device running configuration as text.
func (c *Client) RunningConfig() (string, error) {
	if c.readOnly {
		if err := safety.CheckReadOnly("show running-config"); err != nil {
			return "", err
		}
	}
	cfg := c.node.RunningConfig()
	if cfg == "" {
		return "", fmt.Errorf("failed to retrieve running-config")
	}
	return cfg, nil
}
