package alidns

import (
	"context"
	"testing"

	sdk "github.com/alibabacloud-go/alidns-20150109/v5/client"
	"github.com/alibabacloud-go/tea/tea"
)

// fakeClient is an in-memory stand-in for the AliDNS SDK client.
type fakeClient struct {
	records  map[string]sdk.DescribeSubDomainRecordsResponseBodyDomainRecordsRecord
	nextID   int
	addCalls int
	delCalls int
}

func newFakeClient() *fakeClient {
	return &fakeClient{records: map[string]sdk.DescribeSubDomainRecordsResponseBodyDomainRecordsRecord{}}
}

func (f *fakeClient) AddDomainRecord(req *sdk.AddDomainRecordRequest) (*sdk.AddDomainRecordResponse, error) {
	f.addCalls++
	f.nextID++
	id := tea.ToString(f.nextID)
	f.records[id] = sdk.DescribeSubDomainRecordsResponseBodyDomainRecordsRecord{
		RecordId:   tea.String(id),
		RR:         req.RR,
		DomainName: req.DomainName,
		Type:       req.Type,
		Value:      req.Value,
	}
	return &sdk.AddDomainRecordResponse{Body: &sdk.AddDomainRecordResponseBody{RecordId: tea.String(id)}}, nil
}

func (f *fakeClient) DeleteDomainRecord(req *sdk.DeleteDomainRecordRequest) (*sdk.DeleteDomainRecordResponse, error) {
	f.delCalls++
	delete(f.records, tea.StringValue(req.RecordId))
	return &sdk.DeleteDomainRecordResponse{Body: &sdk.DeleteDomainRecordResponseBody{}}, nil
}

func (f *fakeClient) DescribeSubDomainRecords(req *sdk.DescribeSubDomainRecordsRequest) (*sdk.DescribeSubDomainRecordsResponse, error) {
	sub := tea.StringValue(req.SubDomain)
	var matched []*sdk.DescribeSubDomainRecordsResponseBodyDomainRecordsRecord
	for i := range f.records {
		rec := f.records[i]
		full := tea.StringValue(rec.RR) + "." + tea.StringValue(rec.DomainName)
		if full == sub && tea.StringValue(rec.Type) == tea.StringValue(req.Type) {
			r := rec
			matched = append(matched, &r)
		}
	}
	return &sdk.DescribeSubDomainRecordsResponse{
		Body: &sdk.DescribeSubDomainRecordsResponseBody{
			DomainRecords: &sdk.DescribeSubDomainRecordsResponseBodyDomainRecords{Record: matched},
		},
	}, nil
}

func newTestProvider() (*aliDNSProvider, *fakeClient) {
	fc := newFakeClient()
	return &aliDNSProvider{client: fc, ttl: minTTL}, fc
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
		t.Fatalf("expected 1 record added, got addCalls=%d records=%d", fc.addCalls, len(fc.records))
	}

	// Present is idempotent: a second call must not create a duplicate.
	if err := p.Present(ctx, zone, fqdn, value); err != nil {
		t.Fatalf("Present (2nd): %v", err)
	}
	if fc.addCalls != 1 || len(fc.records) != 1 {
		t.Fatalf("Present not idempotent: addCalls=%d records=%d", fc.addCalls, len(fc.records))
	}

	if err := p.CleanUp(ctx, zone, fqdn, value); err != nil {
		t.Fatalf("CleanUp: %v", err)
	}
	if len(fc.records) != 0 {
		t.Fatalf("expected record removed, got %d", len(fc.records))
	}

	// CleanUp is idempotent: deleting a missing record is fine.
	if err := p.CleanUp(ctx, zone, fqdn, value); err != nil {
		t.Fatalf("CleanUp (2nd): %v", err)
	}
}

// TestCleanUpOnlyMatchingValue verifies concurrent challenge records at the same
// name (e.g. wildcard + apex) are not clobbered.
func TestCleanUpOnlyMatchingValue(t *testing.T) {
	p, fc := newTestProvider()
	ctx := context.Background()
	const (
		zone = "example.com."
		fqdn = "_acme-challenge.example.com."
	)

	if err := p.Present(ctx, zone, fqdn, "value-one"); err != nil {
		t.Fatalf("Present one: %v", err)
	}
	if err := p.Present(ctx, zone, fqdn, "value-two"); err != nil {
		t.Fatalf("Present two: %v", err)
	}
	if len(fc.records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(fc.records))
	}

	if err := p.CleanUp(ctx, zone, fqdn, "value-one"); err != nil {
		t.Fatalf("CleanUp one: %v", err)
	}
	if len(fc.records) != 1 {
		t.Fatalf("expected 1 record left, got %d", len(fc.records))
	}
	for _, rec := range fc.records {
		if tea.StringValue(rec.Value) != "value-two" {
			t.Fatalf("wrong record survived: %s", tea.StringValue(rec.Value))
		}
	}
}
