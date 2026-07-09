// Package tencentcloud implements the provider.Provider interface on top of
// Tencent Cloud DNS (DNSPod) using the official tencentcloud-sdk-go DNSPod
// service (v20210323).
package tencentcloud

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	sdkerrors "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/errors"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	dnspod "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/dnspod/v20210323"

	"github.com/itmisx/cert-manager-webhook/internal/dnsutil"
	"github.com/itmisx/cert-manager-webhook/internal/provider"
)

// Name is the cert-manager solverName that selects this provider.
const Name = "tencentcloud"

const (
	recordType = "TXT"
	// defaultRecordLine is DNSPod's mandatory "line". "默认" ("default") is the
	// catch-all line present on every plan.
	defaultRecordLine = "默认"
	// defaultTTL is DNSPod's free-plan minimum TTL.
	defaultTTL = 600

	defaultSecretIDKey  = "secret-id"
	defaultSecretKeyKey = "secret-key"

	// notFoundCodePrefix is returned by DescribeRecordList when a subdomain has
	// no records. We treat it as "no matching record", not an error.
	notFoundCodePrefix = "ResourceNotFound"
)

// Config is the JSON `config` block for the DNSPod solver.
type Config struct {
	// Region is optional; DNSPod is a global service so it may be left empty.
	Region string `json:"region,omitempty"`
	// TTL for the challenge TXT record in seconds. Defaults to 600.
	TTL uint64 `json:"ttl,omitempty"`

	SecretIDRef  provider.SecretKeySelector `json:"secretIdSecretRef,omitempty"`
	SecretKeyRef provider.SecretKeySelector `json:"secretKeySecretRef,omitempty"`

	// Inline credentials for local `go test` only. Do NOT use in manifests.
	SecretID  string `json:"secretId,omitempty"`
	SecretKey string `json:"secretKey,omitempty"`
}

// dnspodAPI is the subset of the SDK client the provider needs; declaring it as
// an interface keeps the provider unit-testable.
type dnspodAPI interface {
	CreateRecord(*dnspod.CreateRecordRequest) (*dnspod.CreateRecordResponse, error)
	DeleteRecord(*dnspod.DeleteRecordRequest) (*dnspod.DeleteRecordResponse, error)
	DescribeRecordList(*dnspod.DescribeRecordListRequest) (*dnspod.DescribeRecordListResponse, error)
}

type dnspodProvider struct {
	client dnspodAPI
	ttl    uint64
}

var _ provider.Provider = (*dnspodProvider)(nil)

// New is the provider.Factory for Tencent Cloud DNS.
func New(ctx context.Context, rawConfig []byte, namespace string, resolve provider.SecretResolver) (provider.Provider, error) {
	cfg := Config{}
	if len(rawConfig) > 0 {
		if err := json.Unmarshal(rawConfig, &cfg); err != nil {
			return nil, fmt.Errorf("tencentcloud: decoding solver config: %w", err)
		}
	}

	secretID, secretKey, err := resolveCredentials(ctx, cfg, namespace, resolve)
	if err != nil {
		return nil, err
	}

	credential := common.NewCredential(secretID, secretKey)
	client, err := dnspod.NewClient(credential, cfg.Region, profile.NewClientProfile())
	if err != nil {
		return nil, fmt.Errorf("tencentcloud: creating SDK client: %w", err)
	}

	ttl := cfg.TTL
	if ttl == 0 {
		ttl = defaultTTL
	}
	return &dnspodProvider{client: client, ttl: ttl}, nil
}

func resolveCredentials(ctx context.Context, cfg Config, namespace string, resolve provider.SecretResolver) (id, key string, err error) {
	id, key = cfg.SecretID, cfg.SecretKey

	if id == "" && !cfg.SecretIDRef.IsZero() {
		sel := cfg.SecretIDRef
		if sel.Key == "" {
			sel.Key = defaultSecretIDKey
		}
		if id, err = resolve(ctx, namespace, sel); err != nil {
			return "", "", fmt.Errorf("tencentcloud: reading secret id: %w", err)
		}
	}
	if key == "" && !cfg.SecretKeyRef.IsZero() {
		sel := cfg.SecretKeyRef
		if sel.Key == "" {
			sel.Key = defaultSecretKeyKey
		}
		if key, err = resolve(ctx, namespace, sel); err != nil {
			return "", "", fmt.Errorf("tencentcloud: reading secret key: %w", err)
		}
	}

	id, key = strings.TrimSpace(id), strings.TrimSpace(key)
	if id == "" || key == "" {
		return "", "", fmt.Errorf("tencentcloud: secret id and key must be provided via secretIdSecretRef/secretKeySecretRef")
	}
	return id, key, nil
}

// Present creates the challenge TXT record, idempotently.
func (p *dnspodProvider) Present(ctx context.Context, zone, fqdn, value string) error {
	domain := dnsutil.UnFqdn(zone)
	rr, err := dnsutil.ToRR(fqdn, zone)
	if err != nil {
		return err
	}

	existingID, err := p.findRecordID(domain, rr, value)
	if err != nil {
		return err
	}
	if existingID != 0 {
		return nil // already present
	}

	req := dnspod.NewCreateRecordRequest()
	req.Domain = common.StringPtr(domain)
	req.SubDomain = common.StringPtr(rr)
	req.RecordType = common.StringPtr(recordType)
	req.RecordLine = common.StringPtr(defaultRecordLine)
	req.Value = common.StringPtr(value)
	req.TTL = common.Uint64Ptr(p.ttl)

	if _, err := p.client.CreateRecord(req); err != nil {
		return fmt.Errorf("tencentcloud: creating TXT record %s: %w", fqdn, err)
	}
	return nil
}

// CleanUp deletes exactly the TXT record whose value matches this challenge.
func (p *dnspodProvider) CleanUp(ctx context.Context, zone, fqdn, value string) error {
	domain := dnsutil.UnFqdn(zone)
	rr, err := dnsutil.ToRR(fqdn, zone)
	if err != nil {
		return err
	}

	recordID, err := p.findRecordID(domain, rr, value)
	if err != nil {
		return err
	}
	if recordID == 0 {
		return nil // nothing to delete
	}

	req := dnspod.NewDeleteRecordRequest()
	req.Domain = common.StringPtr(domain)
	req.RecordId = common.Uint64Ptr(recordID)
	if _, err := p.client.DeleteRecord(req); err != nil {
		return fmt.Errorf("tencentcloud: deleting TXT record %s: %w", fqdn, err)
	}
	return nil
}

// findRecordID returns the RecordId of the TXT record at rr whose value equals
// value, or 0 if none matches.
func (p *dnspodProvider) findRecordID(domain, rr, value string) (uint64, error) {
	req := dnspod.NewDescribeRecordListRequest()
	req.Domain = common.StringPtr(domain)
	req.Subdomain = common.StringPtr(rr)
	req.RecordType = common.StringPtr(recordType)

	resp, err := p.client.DescribeRecordList(req)
	if err != nil {
		// A subdomain with no records is reported as an error; treat as empty.
		if e, ok := err.(*sdkerrors.TencentCloudSDKError); ok && strings.HasPrefix(e.GetCode(), notFoundCodePrefix) {
			return 0, nil
		}
		return 0, fmt.Errorf("tencentcloud: listing records for %s.%s: %w", rr, domain, err)
	}
	if resp == nil || resp.Response == nil {
		return 0, nil
	}
	for _, rec := range resp.Response.RecordList {
		if rec.Value != nil && *rec.Value == value && rec.RecordId != nil {
			return *rec.RecordId, nil
		}
	}
	return 0, nil
}
