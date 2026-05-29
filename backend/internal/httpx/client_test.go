package httpx

import (
	"net"
	"strings"
	"testing"
)

func TestValidateURL(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantErr string // substring; "" means expect success
	}{
		{"http public host", "http://example.com/x.jpg", ""},
		{"https public host", "https://example.com/x.jpg", ""},
		{"empty", "", "empty url"},
		{"ftp scheme", "ftp://example.com/x", "unsupported scheme"},
		{"no host", "http:///x", "missing host"},
		{"loopback ipv4", "http://127.0.0.1/x", "blocked address"},
		{"rfc1918", "http://10.0.0.5/x", "blocked address"},
		{"link-local", "http://169.254.169.254/latest/meta-data/", "blocked address"},
		{"loopback ipv6", "http://[::1]/x", "blocked address"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ValidateURL(tc.input)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("want ok, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("want err containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("want err containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestIsBlockedIP(t *testing.T) {
	cases := []struct {
		ip   string
		want bool
	}{
		{"127.0.0.1", true},
		{"10.1.2.3", true},
		{"172.20.0.1", true},
		{"192.168.1.1", true},
		{"169.254.169.254", true},
		{"::1", true},
		{"fc00::1", true},
		{"fe80::1", true},
		{"8.8.8.8", false},
		{"2606:4700:4700::1111", false},
	}
	for _, tc := range cases {
		t.Run(tc.ip, func(t *testing.T) {
			got := isBlockedIP(net.ParseIP(tc.ip))
			if got != tc.want {
				t.Fatalf("ip=%s want=%v got=%v", tc.ip, tc.want, got)
			}
		})
	}
}
