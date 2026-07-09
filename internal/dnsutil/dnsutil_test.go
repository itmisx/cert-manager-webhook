package dnsutil

import "testing"

func TestToRR(t *testing.T) {
	cases := []struct {
		name    string
		fqdn    string
		zone    string
		want    string
		wantErr bool
	}{
		{"apex", "example.com.", "example.com.", "@", false},
		{"single label", "_acme-challenge.example.com.", "example.com.", "_acme-challenge", false},
		{"nested", "_acme-challenge.www.example.com.", "example.com.", "_acme-challenge.www", false},
		{"multi-label suffix", "_acme-challenge.example.com.cn.", "example.com.cn.", "_acme-challenge", false},
		{"not in zone", "_acme-challenge.other.org.", "example.com.", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ToRR(tc.fqdn, tc.zone)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("ToRR(%q,%q) = %q, want %q", tc.fqdn, tc.zone, got, tc.want)
			}
		})
	}
}

func TestUnFqdn(t *testing.T) {
	if got := UnFqdn("example.com."); got != "example.com" {
		t.Fatalf("UnFqdn = %q", got)
	}
	if got := UnFqdn("example.com"); got != "example.com" {
		t.Fatalf("UnFqdn (no dot) = %q", got)
	}
}
