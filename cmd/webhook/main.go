// Command webhook is a cert-manager ACME dns-01 solver that runs as a
// Kubernetes aggregated API server. Each DNS provider is registered as its own
// solver name; the provider is selected by the `solverName` field in the
// Issuer's webhook config.
package main

import (
	"os"

	"github.com/cert-manager/cert-manager/pkg/acme/webhook/cmd"

	"github.com/itmisx/cert-manager-webhook/internal/provider/alidns"
	"github.com/itmisx/cert-manager-webhook/internal/provider/baiducloud"
	"github.com/itmisx/cert-manager-webhook/internal/provider/huaweicloud"
	"github.com/itmisx/cert-manager-webhook/internal/provider/tencentcloud"
	"github.com/itmisx/cert-manager-webhook/internal/webhook"
)

// GroupName is the Kubernetes API group the webhook serves. It must match the
// `groupName` configured in the Issuer and in the deployed APIService. Set via
// the GROUP_NAME environment variable (the Helm chart wires this for you).
var GroupName = os.Getenv("GROUP_NAME")

func main() {
	if GroupName == "" {
		panic("GROUP_NAME must be specified")
	}

	// Register every DNS provider here. Each becomes an addressable solverName
	// under the same API group, so one deployment can serve many providers.
	cmd.RunWebhookServer(GroupName,
		webhook.NewSolver(alidns.Name, alidns.New),
		webhook.NewSolver(tencentcloud.Name, tencentcloud.New),
		webhook.NewSolver(huaweicloud.Name, huaweicloud.New),
		webhook.NewSolver(baiducloud.Name, baiducloud.New),
	)
}
