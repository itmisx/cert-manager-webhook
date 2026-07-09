package tencentcloud

import (
	"context"
	"testing"

	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	sdkerrors "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/errors"
	dnspod "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/dnspod/v20210323"
)

type fakeClient struct {
	records  map[uint64]*dnspod.RecordListItem
	nextID   uint64
	addCalls int
	delCalls int
}

func newFakeClient() *fakeClient {
	return &fakeClient{records: map[uint64]*dnspod.RecordListItem{}}
}

func (f *fakeClient) CreateRecord(req *dnspod.CreateRecordRequest) (*dnspod.CreateRecordResponse, error) {
	f.addCalls++
	f.nextID++
	id := f.nextID
	f.records[id] = &dnspod.RecordListItem{
		RecordId: common.Uint64Ptr(id),
		Value:    req.Value,
		Name:     req.SubDomain,
		Type:     req.RecordType,
	}
	resp := dnspod.NewCreateRecordResponse()
	resp.Response = &dnspod.CreateRecordResponseParams{RecordId: common.Uint64Ptr(id)}
	return resp, nil
}

func (f *fakeClient) DeleteRecord(req *dnspod.DeleteRecordRequest) (*dnspod.DeleteRecordResponse, error) {
	f.delCalls++
	delete(f.records, *req.RecordId)
	return dnspod.NewDeleteRecordResponse(), nil
}

func (f *fakeClient) DescribeRecordList(req *dnspod.DescribeRecordListRequest) (*dnspod.DescribeRecordListResponse, error) {
	var list []*dnspod.RecordListItem
	for _, rec := range f.records {
		if *rec.Name == *req.Subdomain && *rec.Type == *req.RecordType {
			list = append(list, rec)
		}
	}
	if len(list) == 0 {
		// Mirror DNSPod: an empty subdomain returns a ResourceNotFound error.
		return nil, &sdkerrors.TencentCloudSDKError{Code: "ResourceNotFound.NoDataOfRecord"}
	}
	resp := dnspod.NewDescribeRecordListResponse()
	resp.Response = &dnspod.DescribeRecordListResponseParams{RecordList: list}
	return resp, nil
}

func newTestProvider() (*dnspodProvider, *fakeClient) {
	fc := newFakeClient()
	return &dnspodProvider{client: fc, ttl: defaultTTL}, fc
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

	// CleanUp when already gone is fine.
	if err := p.CleanUp(ctx, zone, fqdn, value); err != nil {
		t.Fatalf("CleanUp (2nd): %v", err)
	}
}

func TestCleanUpOnlyMatchingValue(t *testing.T) {
	p, fc := newTestProvider()
	ctx := context.Background()
	const (
		zone = "example.com."
		fqdn = "_acme-challenge.example.com."
	)

	_ = p.Present(ctx, zone, fqdn, "value-one")
	_ = p.Present(ctx, zone, fqdn, "value-two")
	if len(fc.records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(fc.records))
	}

	if err := p.CleanUp(ctx, zone, fqdn, "value-one"); err != nil {
		t.Fatalf("CleanUp: %v", err)
	}
	if len(fc.records) != 1 {
		t.Fatalf("expected 1 record left, got %d", len(fc.records))
	}
	for _, rec := range fc.records {
		if *rec.Value != "value-two" {
			t.Fatalf("wrong record survived: %s", *rec.Value)
		}
	}
}
