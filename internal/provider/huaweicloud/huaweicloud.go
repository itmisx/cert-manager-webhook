// Package huaweicloud implements the provider.Provider interface on top of
// Huawei Cloud DNS using the official huaweicloud-sdk-go-v3 DNS service (v2).
//
// Unlike AliDNS/DNSPod, Huawei Cloud DNS is zone-ID based: to create a record
// set we first resolve the zone name to its ID via ListPublicZones. Huawei also
// stores TXT values wrapped in double quotes, which this package handles.
package huaweicloud

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/auth/basic"
	dns "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/dns/v2"
	"github.com/huaweicloud/huaweicloud-sdk-go-v3/services/dns/v2/model"
	dnsregion "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/dns/v2/region"

	"github.com/itmisx/cert-manager-webhook/internal/provider"
)

// Name is the cert-manager solverName that selects this provider.
const Name = "huaweicloud"

const (
	recordType = "TXT"
	// defaultRegion is a sensible default for the global public DNS service.
	defaultRegion = "cn-north-4"
	// defaultTTL matches Huawei Cloud DNS' console default.
	defaultTTL = 300

	defaultAccessKeyKey = "access-key"
	defaultSecretKeyKey = "secret-key"
)

// Config is the JSON `config` block for the Huawei Cloud DNS solver.
type Config struct {
	// Region selects the DNS endpoint, e.g. "cn-north-4". Defaults to cn-north-4.
	Region string `json:"region,omitempty"`
	// TTL for the challenge TXT record in seconds. Defaults to 300.
	TTL int32 `json:"ttl,omitempty"`

	AccessKeyRef provider.SecretKeySelector `json:"accessKeySecretRef,omitempty"`
	SecretKeyRef provider.SecretKeySelector `json:"secretKeySecretRef,omitempty"`

	// Inline credentials for local `go test` only. Do NOT use in manifests.
	AccessKey string `json:"accessKey,omitempty"`
	SecretKey string `json:"secretKey,omitempty"`
}

// dnsAPI is the subset of the SDK client the provider needs; declaring it as an
// interface keeps the provider unit-testable.
type dnsAPI interface {
	ListPublicZones(*model.ListPublicZonesRequest) (*model.ListPublicZonesResponse, error)
	CreateRecordSet(*model.CreateRecordSetRequest) (*model.CreateRecordSetResponse, error)
	ListRecordSets(*model.ListRecordSetsRequest) (*model.ListRecordSetsResponse, error)
	DeleteRecordSet(*model.DeleteRecordSetRequest) (*model.DeleteRecordSetResponse, error)
}

type huaweiProvider struct {
	client dnsAPI
	ttl    int32
}

var _ provider.Provider = (*huaweiProvider)(nil)

// New is the provider.Factory for Huawei Cloud DNS.
func New(ctx context.Context, rawConfig []byte, namespace string, resolve provider.SecretResolver) (provider.Provider, error) {
	cfg := Config{}
	if len(rawConfig) > 0 {
		if err := json.Unmarshal(rawConfig, &cfg); err != nil {
			return nil, fmt.Errorf("huaweicloud: decoding solver config: %w", err)
		}
	}

	ak, sk, err := resolveCredentials(ctx, cfg, namespace, resolve)
	if err != nil {
		return nil, err
	}

	region := cfg.Region
	if region == "" {
		region = defaultRegion
	}
	regionValue, err := dnsregion.SafeValueOf(region)
	if err != nil {
		return nil, fmt.Errorf("huaweicloud: unknown region %q: %w", region, err)
	}

	credentials, err := basic.NewCredentialsBuilder().WithAk(ak).WithSk(sk).SafeBuild()
	if err != nil {
		return nil, fmt.Errorf("huaweicloud: building credentials: %w", err)
	}
	hc, err := dns.DnsClientBuilder().WithRegion(regionValue).WithCredential(credentials).SafeBuild()
	if err != nil {
		return nil, fmt.Errorf("huaweicloud: building client: %w", err)
	}

	ttl := cfg.TTL
	if ttl == 0 {
		ttl = defaultTTL
	}
	return &huaweiProvider{client: dns.NewDnsClient(hc), ttl: ttl}, nil
}

func resolveCredentials(ctx context.Context, cfg Config, namespace string, resolve provider.SecretResolver) (ak, sk string, err error) {
	ak, sk = cfg.AccessKey, cfg.SecretKey

	if ak == "" && !cfg.AccessKeyRef.IsZero() {
		sel := cfg.AccessKeyRef
		if sel.Key == "" {
			sel.Key = defaultAccessKeyKey
		}
		if ak, err = resolve(ctx, namespace, sel); err != nil {
			return "", "", fmt.Errorf("huaweicloud: reading access key: %w", err)
		}
	}
	if sk == "" && !cfg.SecretKeyRef.IsZero() {
		sel := cfg.SecretKeyRef
		if sel.Key == "" {
			sel.Key = defaultSecretKeyKey
		}
		if sk, err = resolve(ctx, namespace, sel); err != nil {
			return "", "", fmt.Errorf("huaweicloud: reading secret key: %w", err)
		}
	}

	ak, sk = strings.TrimSpace(ak), strings.TrimSpace(sk)
	if ak == "" || sk == "" {
		return "", "", fmt.Errorf("huaweicloud: access key and secret key must be provided via accessKeySecretRef/secretKeySecretRef")
	}
	return ak, sk, nil
}

// Present creates the challenge TXT record set, idempotently. Huawei Cloud DNS
// uses fully-qualified (rooted) names, so we keep the trailing dots.
func (p *huaweiProvider) Present(ctx context.Context, zone, fqdn, value string) error {
	zoneID, err := p.zoneID(zone)
	if err != nil {
		return err
	}

	existingID, _, err := p.findRecordSet(fqdn, value)
	if err != nil {
		return err
	}
	if existingID != "" {
		return nil // already present
	}

	ttl := p.ttl
	req := &model.CreateRecordSetRequest{
		ZoneId: zoneID,
		Body: &model.CreateRecordSetRequestBody{
			Name:    fqdn,
			Type:    recordType,
			Ttl:     &ttl,
			Records: []string{quote(value)},
		},
	}
	if _, err := p.client.CreateRecordSet(req); err != nil {
		return fmt.Errorf("huaweicloud: creating TXT record %s: %w", fqdn, err)
	}
	return nil
}

// CleanUp deletes exactly the record set whose value matches this challenge.
func (p *huaweiProvider) CleanUp(ctx context.Context, zone, fqdn, value string) error {
	recordSetID, zoneID, err := p.findRecordSet(fqdn, value)
	if err != nil {
		return err
	}
	if recordSetID == "" {
		return nil // nothing to delete
	}

	req := &model.DeleteRecordSetRequest{ZoneId: zoneID, RecordsetId: recordSetID}
	if _, err := p.client.DeleteRecordSet(req); err != nil {
		return fmt.Errorf("huaweicloud: deleting TXT record %s: %w", fqdn, err)
	}
	return nil
}

// zoneID resolves a rooted zone name (e.g. "example.com.") to its Huawei zone ID.
func (p *huaweiProvider) zoneID(zone string) (string, error) {
	name := zone
	resp, err := p.client.ListPublicZones(&model.ListPublicZonesRequest{Name: &name})
	if err != nil {
		return "", fmt.Errorf("huaweicloud: listing zones for %s: %w", zone, err)
	}
	if resp.Zones != nil {
		for _, z := range *resp.Zones {
			// Name filter is a fuzzy search, so match exactly.
			if z.Name != nil && *z.Name == zone && z.Id != nil {
				return *z.Id, nil
			}
		}
	}
	return "", fmt.Errorf("huaweicloud: zone %q not found in this account", zone)
}

// findRecordSet returns (recordSetID, zoneID) for the TXT record set named fqdn
// whose records contain value, or ("", "") if none matches.
func (p *huaweiProvider) findRecordSet(fqdn, value string) (recordSetID, zoneID string, err error) {
	name, typ := fqdn, recordType
	resp, err := p.client.ListRecordSets(&model.ListRecordSetsRequest{Name: &name, Type: &typ})
	if err != nil {
		return "", "", fmt.Errorf("huaweicloud: listing record sets for %s: %w", fqdn, err)
	}
	if resp.Recordsets == nil {
		return "", "", nil
	}
	want := quote(value)
	for _, rs := range *resp.Recordsets {
		if rs.Name == nil || *rs.Name != fqdn || rs.Records == nil {
			continue
		}
		for _, r := range *rs.Records {
			if r == want {
				id, zid := "", ""
				if rs.Id != nil {
					id = *rs.Id
				}
				if rs.ZoneId != nil {
					zid = *rs.ZoneId
				}
				return id, zid, nil
			}
		}
	}
	return "", "", nil
}

// quote wraps a TXT value in double quotes, as Huawei Cloud DNS stores them.
func quote(v string) string {
	return "\"" + v + "\""
}
