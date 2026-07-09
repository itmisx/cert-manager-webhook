package baiducloud

import (
	"context"
	"strconv"
	"testing"

	dns "github.com/baidubce/bce-sdk-go/services/dns"
)

type fakeClient struct {
	records  map[string]dns.Record
	nextID   int
	addCalls int
	delCalls int
}

func newFakeClient() *fakeClient {
	return &fakeClient{records: map[string]dns.Record{}}
}

func (f *fakeClient) CreateRecord(zoneName string, body *dns.CreateRecordRequest, clientToken string) error {
	f.addCalls++
	f.nextID++
	id := strconv.Itoa(f.nextID)
	f.records[id] = dns.Record{
		Id:    id,
		Rr:    body.Rr,
		Type:  body.Type,
		Value: body.Value,
	}
	return nil
}

func (f *fakeClient) DeleteRecord(zoneName string, recordID string, clientToken string) error {
	f.delCalls++
	delete(f.records, recordID)
	return nil
}

func (f *fakeClient) ListRecord(zoneName string, request *dns.ListRecordRequest) (*dns.ListRecordResponse, error) {
	var out []dns.Record
	for _, rec := range f.records {
		if request.Rr == "" || rec.Rr == request.Rr {
			out = append(out, rec)
		}
	}
	return &dns.ListRecordResponse{Records: out}, nil
}

func newTestProvider() (*baiduProvider, *fakeClient) {
	fc := newFakeClient()
	return &baiduProvider{client: fc, ttl: defaultTTL}, fc
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
		if rec.Value != "value-two" {
			t.Fatalf("wrong record survived: %s", rec.Value)
		}
	}
}
