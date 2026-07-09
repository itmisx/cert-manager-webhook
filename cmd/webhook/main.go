// Command webhook is a cert-manager ACME dns-01 solver that runs as a
// Kubernetes aggregated API server. Each DNS provider is registered as its own
// solver name; the provider is selected by the `solverName` field in the
// Issuer's webhook config.
package main

import (
	"os"

	"github.com/spf13/cobra"
	genericapiserver "k8s.io/apiserver/pkg/server"
	"k8s.io/component-base/logs"

	"github.com/cert-manager/cert-manager/pkg/acme/webhook/cmd/server"
	logf "github.com/cert-manager/cert-manager/pkg/logs"

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

	logs.InitLogs()
	defer logs.FlushLogs()

	// Register every DNS provider here. Each becomes an addressable solverName
	// under the same API group, so one deployment can serve many providers.
	o := server.NewWebhookServerOptions(GroupName,
		webhook.NewSolver(alidns.Name, alidns.New),
		webhook.NewSolver(tencentcloud.Name, tencentcloud.New),
		webhook.NewSolver(huaweicloud.Name, huaweicloud.New),
		webhook.NewSolver(baiducloud.Name, baiducloud.New),
	)

	// This mirrors the framework's cmd.RunWebhookServer, but injects
	// SkipOpenAPIInstallation before the apiserver is built. Every solver shares
	// the ChallengePayload Kind, so the generic apiserver's OpenAPI builder fails
	// with "duplicate Operation ID" the moment more than one solver is
	// registered. The webhook only serves present/cleanup POSTs and never needs
	// to publish an OpenAPI spec, so skipping it is safe and unblocks the
	// one-binary-many-providers design.
	cmd := &cobra.Command{
		Short: "Launch an ACME solver API server",
		Long:  "Launch an ACME solver API server",
		RunE: func(c *cobra.Command, args []string) error {
			if err := o.Complete(); err != nil {
				return err
			}
			if err := o.Validate(args); err != nil {
				return err
			}

			config, err := o.Config()
			if err != nil {
				return err
			}
			config.GenericConfig.SkipOpenAPIInstallation = true

			srv, err := config.Complete().New()
			if err != nil {
				return err
			}
			return srv.GenericAPIServer.PrepareRun().RunWithContext(c.Context())
		},
	}

	flags := cmd.Flags()
	logf.AddFlags(o.Logging, flags)
	o.RecommendedOptions.AddFlags(flags)

	ctx := genericapiserver.SetupSignalContext()
	if err := cmd.ExecuteContext(ctx); err != nil {
		logf.Log.Error(err, "error running webhook server")
		os.Exit(1)
	}
}
