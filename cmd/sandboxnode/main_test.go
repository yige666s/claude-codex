package main

import "testing"

func TestValidateLoopbackAddress(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:18080", "[::1]:18080"} {
		if err := validateLoopbackAddress(addr); err != nil {
			t.Fatalf("%s rejected: %v", addr, err)
		}
	}
	for _, addr := range []string{":18080", "0.0.0.0:18080", "localhost:18080", "192.168.1.10:18080"} {
		if err := validateLoopbackAddress(addr); err == nil {
			t.Fatalf("unsafe listen %s accepted", addr)
		}
	}
}
