// Package dnsutil contains small helpers for turning the fully-qualified
// challenge names that cert-manager hands to a webhook into the (base domain,
// host record) pair that most DNS provider APIs expect.
package dnsutil

import (
	"fmt"
	"strings"
)

// UnFqdn strips the trailing dot from a fully-qualified domain name.
//
// cert-manager always passes ResolvedFQDN / ResolvedZone as rooted names such
// as "_acme-challenge.example.com." — provider APIs almost never want the dot.
func UnFqdn(s string) string {
	return strings.TrimSuffix(s, ".")
}

// ToRR splits a challenge FQDN into the "host record" (RR) relative to its
// zone. Alibaba Cloud DNS, Tencent Cloud DNS and most other providers require
// the base domain and the sub-record to be supplied separately.
//
//	fqdn = "_acme-challenge.www.example.com."  zone = "example.com."
//	  -> "_acme-challenge.www"
//
//	fqdn = "_acme-challenge.example.com."      zone = "example.com."
//	  -> "_acme-challenge"
//
//	fqdn = "example.com."                       zone = "example.com."
//	  -> "@"   (the apex record)
func ToRR(fqdn, zone string) (string, error) {
	fqdn = UnFqdn(fqdn)
	zone = UnFqdn(zone)

	if fqdn == zone {
		return "@", nil
	}

	suffix := "." + zone
	if !strings.HasSuffix(fqdn, suffix) {
		return "", fmt.Errorf("fqdn %q is not contained in zone %q", fqdn, zone)
	}
	return strings.TrimSuffix(fqdn, suffix), nil
}
