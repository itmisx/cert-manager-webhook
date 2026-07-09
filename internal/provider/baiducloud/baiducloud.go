// Package baiducloud implements the provider.Provider interface on top of Baidu
// AI Cloud DNS (云解析 DNS) using the official bce-sdk-go DNS service.
package baiducloud

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	dns "github.com/baidubce/bce-sdk-go/services/dns"

	"github.com/itmisx/cert-manager-webhook/internal/dnsutil"
	"github.com/itmisx/cert-manager-webhook/internal/provider"
)

// Name is the cert-manager solverName that selects this provider.
const Name = "baiducloud"

const (
	recordType = "TXT"
	// defaultLine is Baidu Cloud DNS' catch-all resolution line.
	defaultLine = "default"
	// defaultTTL matches Baidu Cloud DNS' console default.
	defaultTTL = 300

	defaultAccessKeyKey = "access-key"
	defaultSecretKeyKey = "secret-key"
)

// Config is the JSON `config` block for the Baidu Cloud DNS solver.
type Config struct {
	// Endpoint overrides the API endpoint. Defaults to the SDK's
	// http://dns.baidubce.com.
	Endpoint string `json:"endpoint,omitempty"`
	// TTL for the challenge TXT record in seconds. Defaults to 300.
	TTL int32 `json:"ttl,omitempty"`

	AccessKeyRef provider.SecretKeySelector `json:"accessKeySecretRef,omitempty"`
	SecretKeyRef provider.SecretKeySelector `json:"secretKeySecretRef,omitempty"`

	// Inline credentials for local `go test` only. Do NOT use in manifests.
	AccessKey string `json:"accessKey,omitempty"`
	SecretKey string `json:"secretKey,omitempty"`
}

// baiduDNSAPI is the subset of the SDK client the provider needs; declaring it
// as an interface keeps the provider unit-testable.
type baiduDNSAPI interface {
	CreateRecord(zoneName string, body *dns.CreateRecordRequest, clientToken string) error
	DeleteRecord(zoneName string, recordID string, clientToken string) error
	ListRecord(zoneName string, request *dns.ListRecordRequest) (*dns.ListRecordResponse, error)
}

type baiduProvider struct {
	client baiduDNSAPI
	ttl    int32
}

var _ provider.Provider = (*baiduProvider)(nil)

// New is the provider.Factory for Baidu Cloud DNS.
func New(ctx context.Context, rawConfig []byte, namespace string, resolve provider.SecretResolver) (provider.Provider, error) {
	cfg := Config{}
	if len(rawConfig) > 0 {
		if err := json.Unmarshal(rawConfig, &cfg); err != nil {
			return nil, fmt.Errorf("baiducloud: decoding solver config: %w", err)
		}
	}

	ak, sk, err := resolveCredentials(ctx, cfg, namespace, resolve)
	if err != nil {
		return nil, err
	}

	client, err := dns.NewClient(ak, sk, cfg.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("baiducloud: creating SDK client: %w", err)
	}

	ttl := cfg.TTL
	if ttl == 0 {
		ttl = defaultTTL
	}
	return &baiduProvider{client: client, ttl: ttl}, nil
}

func resolveCredentials(ctx context.Context, cfg Config, namespace string, resolve provider.SecretResolver) (ak, sk string, err error) {
	ak, sk = cfg.AccessKey, cfg.SecretKey

	if ak == "" && !cfg.AccessKeyRef.IsZero() {
		sel := cfg.AccessKeyRef
		if sel.Key == "" {
			sel.Key = defaultAccessKeyKey
		}
		if ak, err = resolve(ctx, namespace, sel); err != nil {
			return "", "", fmt.Errorf("baiducloud: reading access key: %w", err)
		}
	}
	if sk == "" && !cfg.SecretKeyRef.IsZero() {
		sel := cfg.SecretKeyRef
		if sel.Key == "" {
			sel.Key = defaultSecretKeyKey
		}
		if sk, err = resolve(ctx, namespace, sel); err != nil {
			return "", "", fmt.Errorf("baiducloud: reading secret key: %w", err)
		}
	}

	ak, sk = strings.TrimSpace(ak), strings.TrimSpace(sk)
	if ak == "" || sk == "" {
		return "", "", fmt.Errorf("baiducloud: access key and secret key must be provided via accessKeySecretRef/secretKeySecretRef")
	}
	return ak, sk, nil
}

// Present creates the challenge TXT record, idempotently.
func (p *baiduProvider) Present(ctx context.Context, zone, fqdn, value string) error {
	domain := dnsutil.UnFqdn(zone)
	rr, err := dnsutil.ToRR(fqdn, zone)
	if err != nil {
		return err
	}

	existingID, err := p.findRecordID(domain, rr, value)
	if err != nil {
		return err
	}
	if existingID != "" {
		return nil // already present
	}

	ttl, line := p.ttl, defaultLine
	body := &dns.CreateRecordRequest{
		Rr:    rr,
		Type:  recordType,
		Value: value,
		Ttl:   &ttl,
		Line:  &line,
	}
	if err := p.client.CreateRecord(domain, body, ""); err != nil {
		return fmt.Errorf("baiducloud: creating TXT record %s: %w", fqdn, err)
	}
	return nil
}

// CleanUp deletes exactly the TXT record whose value matches this challenge.
func (p *baiduProvider) CleanUp(ctx context.Context, zone, fqdn, value string) error {
	domain := dnsutil.UnFqdn(zone)
	rr, err := dnsutil.ToRR(fqdn, zone)
	if err != nil {
		return err
	}

	recordID, err := p.findRecordID(domain, rr, value)
	if err != nil {
		return err
	}
	if recordID == "" {
		return nil // nothing to delete
	}

	if err := p.client.DeleteRecord(domain, recordID, ""); err != nil {
		return fmt.Errorf("baiducloud: deleting TXT record %s: %w", fqdn, err)
	}
	return nil
}

// findRecordID returns the Id of the TXT record at rr whose value equals value,
// or "" if none matches.
func (p *baiduProvider) findRecordID(domain, rr, value string) (string, error) {
	resp, err := p.client.ListRecord(domain, &dns.ListRecordRequest{Rr: rr})
	if err != nil {
		return "", fmt.Errorf("baiducloud: listing records for %s.%s: %w", rr, domain, err)
	}
	if resp == nil {
		return "", nil
	}
	for _, rec := range resp.Records {
		if rec.Type == recordType && rec.Rr == rr && rec.Value == value {
			return rec.Id, nil
		}
	}
	return "", nil
}
