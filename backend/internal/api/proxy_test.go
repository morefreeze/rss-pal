package api

import (
	"net"
	"testing"
)

func TestIsBlockedIP(t *testing.T) {
	cases := []struct {
		ip      string
		blocked bool
	}{
		{"127.0.0.1", true},
		{"10.0.0.1", true},
		{"172.16.0.1", true},
		{"192.168.1.1", true},
		{"169.254.169.254", true}, // metadata
		{"::1", true},
		{"fc00::1", true},
		{"fe80::1", true},
		{"8.8.8.8", false},
		{"1.1.1.1", false},
		{"2606:4700:4700::1111", false},
	}
	for _, tc := range cases {
		ip := net.ParseIP(tc.ip)
		if ip == nil {
			t.Fatalf("parse %q failed", tc.ip)
		}
		if got := isBlockedIP(ip); got != tc.blocked {
			t.Errorf("isBlockedIP(%s) = %v, want %v", tc.ip, got, tc.blocked)
		}
	}
}

func TestValidateImageURL(t *testing.T) {
	cases := []struct {
		raw     string
		wantErr bool
	}{
		{"https://example.com/img.png", false},
		{"http://example.com/img.png", false},
		{"ftp://example.com/img.png", true},
		{"file:///etc/passwd", true},
		{"https://127.0.0.1/img.png", true},
		{"https://192.168.1.1/img.png", true},
		{"not-a-url", true},
		{"", true},
	}
	for _, tc := range cases {
		_, err := validateImageURL(tc.raw)
		if (err != nil) != tc.wantErr {
			t.Errorf("validateImageURL(%q) err=%v, wantErr=%v", tc.raw, err, tc.wantErr)
		}
	}
}
