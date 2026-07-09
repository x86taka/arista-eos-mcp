package eapi

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/x86taka/arista-eos-mcp/internal/config"
)

// A transport error carrying the eAPI URL (with embedded credentials) must not
// leak the username, password, or enable password after scrubbing. Each secret
// is present in the input so the test regresses if any one stops being scrubbed.
func TestScrubSecretsRedactsURLCredentials(t *testing.T) {
	secrets := credentialSecrets(&config.Device{Username: "admin", Password: "s3cr3t", EnablePassword: "en4ble"})
	// The username/password appear in the URL userinfo; the enable password is
	// appended to mimic a body/response echo so all three are actually exercised.
	err := errors.New(`Post "https://admin:s3cr3t@192.0.2.10:443/command-api": dial tcp 192.0.2.10:443: connect: connection refused (enable input "en4ble")`)

	got := scrubSecrets(err, secrets).Error()

	for _, leak := range []string{"admin", "s3cr3t", "en4ble"} {
		if strings.Contains(got, leak) {
			t.Fatalf("credential %q leaked in error: %q", leak, got)
		}
	}
	if !strings.Contains(got, "connection refused") {
		t.Fatalf("scrubbing removed the useful message: %q", got)
	}
}

// A scrubbed error must not leak the credential through any formatting verb,
// including the Go-syntax representation %#v (which would expose a preserved
// wrapping chain if scrubSecrets kept one).
func TestScrubSecretsNoLeakViaFormatting(t *testing.T) {
	secrets := []string{"s3cr3t"}
	err := errors.New(`Post "https://admin:s3cr3t@192.0.2.10:443/command-api": i/o timeout`)
	scrubbed := scrubSecrets(err, secrets)

	for _, verb := range []string{"%v", "%s", "%q", "%+v", "%#v"} {
		if out := fmt.Sprintf(verb, scrubbed); strings.Contains(out, "s3cr3t") {
			t.Fatalf("secret leaked via %s: %q", verb, out)
		}
	}
}

// When no secret appears in the message the original error is returned intact
// so its wrapping chain (errors.Is/As) is preserved.
func TestScrubSecretsPreservesUnaffectedError(t *testing.T) {
	orig := errors.New("dial tcp 192.0.2.10:443: i/o timeout")
	if got := scrubSecrets(orig, []string{"admin", "s3cr3t"}); got != orig {
		t.Fatalf("expected original error preserved, got %v", got)
	}
}

func TestScrubSecretsNil(t *testing.T) {
	if scrubSecrets(nil, []string{"x"}) != nil {
		t.Fatal("nil error should stay nil")
	}
}

// An empty credential must never be used as a replacement target, otherwise
// strings.ReplaceAll would insert the marker between every character.
func TestCredentialSecretsSkipsEmpty(t *testing.T) {
	s := credentialSecrets(&config.Device{Username: "admin", Password: "pw"})
	if len(s) != 2 {
		t.Fatalf("got %d secrets, want 2: %v", len(s), s)
	}
	for _, v := range s {
		if v == "" {
			t.Fatal("empty secret must not be collected")
		}
	}
}
