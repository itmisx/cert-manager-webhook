//go:build conformance

// This file is compiled only with `-tags conformance` because the cert-manager
// test suite requires the kubebuilder/envtest control-plane binaries (etcd +
// kube-apiserver) at package-init time. Run it via `make test-conformance`.

package main

import (
	"os"
	"testing"

	acmetest "github.com/cert-manager/cert-manager/test/acme"

	"github.com/itmisx/cert-manager-webhook/internal/provider"
	"github.com/itmisx/cert-manager-webhook/internal/provider/alidns"
	"github.com/itmisx/cert-manager-webhook/internal/provider/baiducloud"
	"github.com/itmisx/cert-manager-webhook/internal/provider/huaweicloud"
	"github.com/itmisx/cert-manager-webhook/internal/provider/tencentcloud"
	"github.com/itmisx/cert-manager-webhook/internal/webhook"
)

// TestRunsSuite runs cert-manager's official DNS01 webhook conformance suite
// against a live DNS provider. It talks to real DNS, so it is skipped unless
// TEST_ZONE_NAME is set. See the README ("Running the conformance tests").
//
// Environment:
//   - TEST_ZONE_NAME: a zone you control, with trailing dot, e.g. "example.com."
//   - TEST_PROVIDER : "alidns" (default), "tencentcloud" or "huaweicloud".
//
// Each provider reads its fixture from testdata/<provider>-solver/, which must
// contain a config.json referencing a real credential Secret plus a secret.yaml
// installing that Secret (see each directory's notes and .gitignore).
func TestRunsSuite(t *testing.T) {
	zone := os.Getenv("TEST_ZONE_NAME")
	if zone == "" {
		t.Skip("TEST_ZONE_NAME is not set; skipping the cert-manager conformance suite")
	}

	name := os.Getenv("TEST_PROVIDER")
	if name == "" {
		name = alidns.Name
	}

	var (
		factory   provider.Factory
		dnsServer string
	)
	switch name {
	case alidns.Name:
		factory, dnsServer = alidns.New, "223.5.5.5:53" // AliDNS PublicDNS
	case tencentcloud.Name:
		factory, dnsServer = tencentcloud.New, "119.29.29.29:53" // DNSPod PublicDNS
	case huaweicloud.Name:
		factory, dnsServer = huaweicloud.New, "100.125.1.250:53" // Huawei Cloud DNS
	case baiducloud.Name:
		factory, dnsServer = baiducloud.New, "180.76.76.76:53" // Baidu PublicDNS
	default:
		t.Fatalf("unknown TEST_PROVIDER %q", name)
	}

	fixture := acmetest.NewFixture(
		webhook.NewSolver(name, factory),
		acmetest.SetResolvedZone(zone),
		acmetest.SetManifestPath("testdata/"+name+"-solver"),
		acmetest.SetDNSServer(dnsServer),
		acmetest.SetUseAuthoritative(false),
	)

	fixture.RunConformance(t)
}
