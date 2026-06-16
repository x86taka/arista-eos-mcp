package safety

import "testing"

func TestCheckReadOnly(t *testing.T) {
	allowed := []string{
		"show version",
		"show running-config",
		"SHOW INTERFACES Ethernet1",
		"  show ip bgp summary  ",
		"ping 10.0.0.1",
		"traceroute 10.0.0.1",
	}
	for _, c := range allowed {
		if err := CheckReadOnly(c); err != nil {
			t.Errorf("expected %q to be allowed, got error: %v", c, err)
		}
	}

	rejected := []string{
		"",
		"configure terminal",
		"conf t",
		"write memory",
		"copy running-config startup-config",
		"reload",
		"clear counters",
		"no interface Ethernet1",
		"delete flash:foo",
		"bash echo hi",
		"show running-config | tee flash:foo",
	}
	for _, c := range rejected {
		if err := CheckReadOnly(c); err == nil {
			t.Errorf("expected %q to be rejected, but it was allowed", c)
		}
	}
}
