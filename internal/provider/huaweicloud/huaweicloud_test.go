package huaweicloud

import (
	"context"
	"testing"

	"github.com/huaweicloud/huaweicloud-sdk-go-v3/services/dns/v2/model"
)

type fakeClient struct {
	zoneID   string
	zoneName string
	records  map[string]*model.ListRecordSetsWithTags // recordSetID -> record
	nextID   int
	addCalls int
	delCalls int
}

func newFakeClient(zoneName, zoneID string) *fakeClient {
	return &fakeClient{
		zoneID:   zoneID,
		zoneName: zoneName,
		records:  map[string]*model.ListRecordSetsWithTags{},
	}
}

func (f *fakeClient) ListPublicZones(*model.ListPublicZonesRequest) (*model.ListPublicZonesResponse, error) {
	name, id := f.zoneName, f.zoneID
	zones := []model.PublicZoneResp{{Name: &name, Id: &id}}
	return &model.ListPublicZonesResponse{Zones: &zones}, nil
}

func (f *fakeClient) CreateRecordSet(req *model.CreateRecordSetRequest) (*model.CreateRecordSetResponse, error) {
	f.addCalls++
	f.nextID++
	id := string(rune('a' + f.nextID))
	name := req.Body.Name
	typ := req.Body.Type
	records := req.Body.Records
	zid := req.ZoneId
	f.records[id] = &model.ListRecordSetsWithTags{
		Id:      &id,
		Name:    &name,
		Type:    &typ,
		ZoneId:  &zid,
		Records: &records,
	}
	return &model.CreateRecordSetResponse{Id: &id}, nil
}

func (f *fakeClient) ListRecordSets(req *model.ListRecordSetsRequest) (*model.ListRecordSetsResponse, error) {
	var out []model.ListRecordSetsWithTags
	for _, rs := range f.records {
		if req.Name != nil && rs.Name != nil && *rs.Name == *req.Name {
			out = append(out, *rs)
		}
	}
	return &model.ListRecordSetsResponse{Recordsets: &out}, nil
}

func (f *fakeClient) DeleteRecordSet(req *model.DeleteRecordSetRequest) (*model.DeleteRecordSetResponse, error) {
	f.delCalls++
	delete(f.records, req.RecordsetId)
	return &model.DeleteRecordSetResponse{}, nil
}

func newTestProvider() (*huaweiProvider, *fakeClient) {
	fc := newFakeClient("example.com.", "zone-123")
	return &huaweiProvider{client: fc, ttl: defaultTTL}, fc
}

func TestPresentThenCleanUp(t *testing.T) {
	p, fc := newTestProvider()
	ctx := context.Background()
	const (
		zone  = "example.com."
		fqdn  = "_acme-challenge.example.com."
		value = "token-value-abc"
	)

	if err := p.Present(ctx, zone, fqdn, value); err != nil {
		t.Fatalf("Present: %v", err)
	}
	if fc.addCalls != 1 || len(fc.records) != 1 {
		t.Fatalf("expected 1 record, addCalls=%d records=%d", fc.addCalls, len(fc.records))
	}
	// Value must be stored quoted.
	for _, rs := range fc.records {
		if (*rs.Records)[0] != "\""+value+"\"" {
			t.Fatalf("value not quoted: %q", (*rs.Records)[0])
		}
	}

	// Idempotent.
	if err := p.Present(ctx, zone, fqdn, value); err != nil {
		t.Fatalf("Present (2nd): %v", err)
	}
	if fc.addCalls != 1 {
		t.Fatalf("Present not idempotent: addCalls=%d", fc.addCalls)
	}

	if err := p.CleanUp(ctx, zone, fqdn, value); err != nil {
		t.Fatalf("CleanUp: %v", err)
	}
	if len(fc.records) != 0 {
		t.Fatalf("expected record removed, got %d", len(fc.records))
	}

	if err := p.CleanUp(ctx, zone, fqdn, value); err != nil {
		t.Fatalf("CleanUp (2nd): %v", err)
	}
}

func TestZoneNotFound(t *testing.T) {
	p, _ := newTestProvider()
	err := p.Present(context.Background(), "other.org.", "_acme-challenge.other.org.", "v")
	if err == nil {
		t.Fatal("expected error for unknown zone")
	}
}
