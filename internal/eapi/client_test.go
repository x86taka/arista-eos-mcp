package eapi

import (
	"errors"
	"strings"
	"testing"

	"github.com/x86taka/arista-eos-mcp/internal/config"
)

// A transport error carrying the eAPI URL (with embedded credentials) must not
// leak the username or password after scrubbing.
func TestScrubSecretsRedactsURLCredentials(t *testing.T) {
	secrets := credentialSecrets(&config.Device{Username: "admin", Password: "s3cr3t", EnablePassword: "en4ble"})
	err := errors.New(`Post "https://admin:s3cr3t@192.0.2.10:443/command-api": dial tcp 192.0.2.10:443: connect: connection refused`)

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
