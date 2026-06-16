// Package safety provides validation that keeps the MCP server read-only by
// rejecting any EOS command that could change configuration or device state.
package safety

import (
	"fmt"
	"regexp"
	"strings"
)

// writePrefixes are command leading keywords that mutate the device. A command
// starting with any of these (after trimming) is rejected in read-only mode.
var writePrefixes = []string{
	"configure", "conf t",
	"write", "copy", "delete", "rename", "erase",
	"reload", "reboot", "clear", "no ",
	"enable", "disable",
	"bash", "python", "tcpdump",
	"install", "boot", "switch",
	"snapshot", "schedule",
}

// allowedNonShow are read-only commands that don't start with "show".
var allowedNonShow = []string{
	"ping", "traceroute", "trace", "bgp", "dir",
}

var showRe = regexp.MustCompile(`^show(\s|$)`)

// CheckReadOnly returns an error if cmd is not a safe, read-only command.
// It is intentionally conservative: anything not clearly read-only is rejected.
func CheckReadOnly(cmd string) error {
	norm := strings.ToLower(strings.TrimSpace(cmd))
	if norm == "" {
		return fmt.Errorf("empty command")
	}

	// Reject pipes that could write to a file or shell.
	if strings.Contains(norm, "|") {
		if strings.Contains(norm, "redirect") || strings.Contains(norm, "append") ||
			strings.Contains(norm, "tee") || strings.Contains(norm, " > ") {
			return fmt.Errorf("command %q contains a redirect/write pipe and is not allowed in read-only mode", cmd)
		}
	}

	for _, p := range writePrefixes {
		if strings.HasPrefix(norm, p) {
			return fmt.Errorf("command %q is a write/state-changing command and is rejected in read-only mode", cmd)
		}
	}

	if showRe.MatchString(norm) {
		return nil
	}
	for _, a := range allowedNonShow {
		if norm == a || strings.HasPrefix(norm, a+" ") {
			return nil
		}
	}

	return fmt.Errorf("command %q is not a recognized read-only command; only 'show ...' and a small set of diagnostics are allowed in read-only mode", cmd)
}

// CheckAll validates every command in the slice, returning the first error.
func CheckAll(cmds []string) error {
	for _, c := range cmds {
		if err := CheckReadOnly(c); err != nil {
			return err
		}
	}
	return nil
}
