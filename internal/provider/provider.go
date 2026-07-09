// Package provider defines the small, DNS-record level abstraction that every
// cloud DNS backend (Alibaba Cloud, Tencent Cloud, …) implements. The generic
// cert-manager Solver in internal/webhook talks only to this interface, so
// adding a new provider never touches the ACME/webhook plumbing.
//
// See docs/adding-a-provider.md for a step-by-step guide.
package provider

import "context"

// Provider knows how to create and remove the single TXT record that satisfies
// an ACME dns-01 challenge.
//
// Both methods receive rooted names exactly as cert-manager produces them:
//
//	zone  – the authoritative zone,      e.g. "example.com."
//	fqdn  – the challenge record name,   e.g. "_acme-challenge.example.com."
//	value – the TXT record value to set / match.
//
// Implementations MUST be idempotent: Present may be called when the record
// already exists, and CleanUp may be called when it is already gone. Neither
// case is an error.
type Provider interface {
	Present(ctx context.Context, zone, fqdn, value string) error
	CleanUp(ctx context.Context, zone, fqdn, value string) error
}

// SecretKeySelector points at a single key inside a Kubernetes Secret that
// lives in the same namespace as the Issuer referencing the webhook. It mirrors
// the shape cert-manager uses elsewhere so the config feels familiar.
type SecretKeySelector struct {
	// Name is the name of the Secret.
	Name string `json:"name"`
	// Key is the key within the Secret's data. Defaults are provider specific.
	Key string `json:"key"`
}

// IsZero reports whether the selector was left unset in the webhook config.
func (s SecretKeySelector) IsZero() bool { return s.Name == "" && s.Key == "" }

// SecretResolver reads a Secret key from the given namespace and returns its
// value with surrounding whitespace trimmed. The generic Solver supplies a
// resolver backed by the Kubernetes API; tests can supply a fake.
type SecretResolver func(ctx context.Context, namespace string, sel SecretKeySelector) (string, error)

// Factory builds a Provider from the raw JSON `config` block of a webhook
// solver, the namespace the challenge is being solved in, and a SecretResolver
// for pulling credentials. It is called once per Present/CleanUp so credentials
// are always read fresh.
type Factory func(ctx context.Context, rawConfig []byte, namespace string, resolve SecretResolver) (Provider, error)
