// Package alidns implements the provider.Provider interface on top of Alibaba
// Cloud DNS (AliDNS) using the officially supported v2 ("Tea") SDK,
// github.com/alibabacloud-go/alidns-20150109/v5.
package alidns

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	alidns "github.com/alibabacloud-go/alidns-20150109/v5/client"
	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/utils"
	"github.com/alibabacloud-go/tea/tea"

	"github.com/itmisx/cert-manager-webhook/internal/dnsutil"
	"github.com/itmisx/cert-manager-webhook/internal/provider"
)

// Name is the cert-manager solverName that selects this provider. Reference it
// from an Issuer as `spec.acme.solvers[].dns01.webhook.solverName: alidns`.
const Name = "alidns"

const (
	// defaultEndpoint is the central AliDNS API endpoint. DNS is a global
	// service, so this works from any region; override via config.endpoint if
	// you must pin a regional endpoint.
	defaultEndpoint = "alidns.aliyuncs.com"

	// minTTL is the smallest TTL AliDNS accepts on the free "DNS" plan.
	minTTL = 600

	// recordType is the DNS record type used for dns-01 challenges.
	recordType = "TXT"

	// defaultAccessKeyIDKey / defaultAccessKeySecretKey are the Secret keys
	// used when the config omits an explicit `key`.
	defaultAccessKeyIDKey     = "access-key-id"
	defaultAccessKeySecretKey = "access-key-secret"
)

// Config is the JSON `config` block users place under the webhook solver. All
// credential material is referenced from a Kubernetes Secret; inline plaintext
// is supported only for local testing and is intentionally discouraged.
type Config struct {
	// RegionID optionally pins a region, e.g. "cn-hangzhou". Usually unset.
	RegionID string `json:"regionId,omitempty"`
	// Endpoint overrides the API endpoint. Defaults to alidns.aliyuncs.com.
	Endpoint string `json:"endpoint,omitempty"`
	// TTL for the challenge TXT record in seconds. Clamped to >= 600.
	TTL int64 `json:"ttl,omitempty"`

	// AccessKeyIDRef / AccessKeySecretRef reference the RAM credentials.
	AccessKeyIDRef     provider.SecretKeySelector `json:"accessKeyIDRef,omitempty"`
	AccessKeySecretRef provider.SecretKeySelector `json:"accessKeySecretRef,omitempty"`

	// AccessKeyID / AccessKeySecret allow inline credentials for local
	// `go test` runs only. Do NOT use these in cluster manifests.
	AccessKeyID     string `json:"accessKeyId,omitempty"`
	AccessKeySecret string `json:"accessKeySecret,omitempty"`
}

// aliDNSAPI is the subset of the SDK client the provider needs. Declaring it as
// an interface keeps the provider unit-testable without hitting the network.
type aliDNSAPI interface {
	AddDomainRecord(*alidns.AddDomainRecordRequest) (*alidns.AddDomainRecordResponse, error)
	DeleteDomainRecord(*alidns.DeleteDomainRecordRequest) (*alidns.DeleteDomainRecordResponse, error)
	DescribeSubDomainRecords(*alidns.DescribeSubDomainRecordsRequest) (*alidns.DescribeSubDomainRecordsResponse, error)
}

type aliDNSProvider struct {
	client aliDNSAPI
	ttl    int64
}

// Ensure the provider satisfies the interface at compile time.
var _ provider.Provider = (*aliDNSProvider)(nil)

// New is the provider.Factory for AliDNS. It resolves credentials, builds an
// SDK client and returns a ready-to-use Provider.
func New(ctx context.Context, rawConfig []byte, namespace string, resolve provider.SecretResolver) (provider.Provider, error) {
	cfg := Config{}
	if len(rawConfig) > 0 {
		if err := json.Unmarshal(rawConfig, &cfg); err != nil {
			return nil, fmt.Errorf("alidns: decoding solver config: %w", err)
		}
	}

	keyID, secret, err := resolveCredentials(ctx, cfg, namespace, resolve)
	if err != nil {
		return nil, err
	}

	endpoint := cfg.Endpoint
	if endpoint == "" {
		endpoint = defaultEndpoint
	}

	openapiCfg := &openapi.Config{
		AccessKeyId:     tea.String(keyID),
		AccessKeySecret: tea.String(secret),
		Endpoint:        tea.String(endpoint),
	}
	if cfg.RegionID != "" {
		openapiCfg.RegionId = tea.String(cfg.RegionID)
	}

	client, err := alidns.NewClient(openapiCfg)
	if err != nil {
		return nil, fmt.Errorf("alidns: creating SDK client: %w", err)
	}

	return &aliDNSProvider{client: client, ttl: max(cfg.TTL, minTTL)}, nil
}

// resolveCredentials prefers Secret references and falls back to inline values.
func resolveCredentials(ctx context.Context, cfg Config, namespace string, resolve provider.SecretResolver) (keyID, secret string, err error) {
	keyID, secret = cfg.AccessKeyID, cfg.AccessKeySecret

	if keyID == "" && !cfg.AccessKeyIDRef.IsZero() {
		sel := cfg.AccessKeyIDRef
		if sel.Key == "" {
			sel.Key = defaultAccessKeyIDKey
		}
		if keyID, err = resolve(ctx, namespace, sel); err != nil {
			return "", "", fmt.Errorf("alidns: reading access key id: %w", err)
		}
	}

	if secret == "" && !cfg.AccessKeySecretRef.IsZero() {
		sel := cfg.AccessKeySecretRef
		if sel.Key == "" {
			sel.Key = defaultAccessKeySecretKey
		}
		if secret, err = resolve(ctx, namespace, sel); err != nil {
			return "", "", fmt.Errorf("alidns: reading access key secret: %w", err)
		}
	}

	keyID, secret = strings.TrimSpace(keyID), strings.TrimSpace(secret)
	if keyID == "" || secret == "" {
		return "", "", fmt.Errorf("alidns: access key id and secret must be provided via accessKeyIDRef/accessKeySecretRef")
	}
	return keyID, secret, nil
}

// Present creates the challenge TXT record. It is a no-op if an identical record
// already exists so retries stay safe.
func (p *aliDNSProvider) Present(ctx context.Context, zone, fqdn, value string) error {
	domain := dnsutil.UnFqdn(zone)
	rr, err := dnsutil.ToRR(fqdn, zone)
	if err != nil {
		return err
	}

	existingID, err := p.findRecordID(domain, dnsutil.UnFqdn(fqdn), value)
	if err != nil {
		return err
	}
	if existingID != "" {
		return nil // already present
	}

	req := &alidns.AddDomainRecordRequest{
		DomainName: tea.String(domain),
		RR:         tea.String(rr),
		Type:       tea.String(recordType),
		Value:      tea.String(value),
		TTL:        tea.Int64(p.ttl),
	}
	if _, err := p.client.AddDomainRecord(req); err != nil {
		return fmt.Errorf("alidns: adding TXT record %s: %w", fqdn, err)
	}
	return nil
}

// CleanUp deletes exactly the TXT record whose value matches this challenge,
// leaving any concurrent challenge records (e.g. a wildcard + apex issuance)
// untouched. It is a no-op if the record is already gone.
func (p *aliDNSProvider) CleanUp(ctx context.Context, zone, fqdn, value string) error {
	domain := dnsutil.UnFqdn(zone)

	recordID, err := p.findRecordID(domain, dnsutil.UnFqdn(fqdn), value)
	if err != nil {
		return err
	}
	if recordID == "" {
		return nil // nothing to delete
	}

	req := &alidns.DeleteDomainRecordRequest{RecordId: tea.String(recordID)}
	if _, err := p.client.DeleteDomainRecord(req); err != nil {
		return fmt.Errorf("alidns: deleting TXT record %s: %w", fqdn, err)
	}
	return nil
}

// findRecordID returns the RecordId of the TXT record at subDomain whose value
// equals value, or "" if none matches.
func (p *aliDNSProvider) findRecordID(domain, subDomain, value string) (string, error) {
	req := &alidns.DescribeSubDomainRecordsRequest{
		SubDomain:  tea.String(subDomain),
		DomainName: tea.String(domain),
		Type:       tea.String(recordType),
	}
	resp, err := p.client.DescribeSubDomainRecords(req)
	if err != nil {
		return "", fmt.Errorf("alidns: listing records for %s: %w", subDomain, err)
	}
	if resp == nil || resp.Body == nil || resp.Body.DomainRecords == nil {
		return "", nil
	}
	for _, rec := range resp.Body.DomainRecords.Record {
		if tea.StringValue(rec.Value) == value {
			return tea.StringValue(rec.RecordId), nil
		}
	}
	return "", nil
}
