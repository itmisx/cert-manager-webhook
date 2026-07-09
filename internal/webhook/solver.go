// Package webhook adapts a provider.Provider to the cert-manager ACME dns-01
// webhook.Solver interface. One Solver instance is created per DNS provider and
// all of them are served by a single webhook API server (see cmd/webhook).
package webhook

import (
	"context"
	"fmt"
	"strings"
	"time"

	whapi "github.com/cert-manager/cert-manager/pkg/acme/webhook/apis/acme/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"

	"github.com/itmisx/cert-manager-webhook/internal/provider"
)

// apiTimeout bounds each provider call so a hung cloud API can't wedge the
// webhook. cert-manager retries the challenge on error.
const apiTimeout = 30 * time.Second

// Solver implements webhook.Solver for a single DNS provider by delegating to a
// provider.Factory. It is deliberately provider-agnostic.
type Solver struct {
	name    string
	factory provider.Factory
	kube    kubernetes.Interface
}

// NewSolver wires a solverName to the factory that builds its provider.
func NewSolver(name string, factory provider.Factory) *Solver {
	return &Solver{name: name, factory: factory}
}

// Name is the solverName cert-manager matches against the Issuer config.
func (s *Solver) Name() string { return s.name }

// Initialize is called once by the webhook apiserver at startup. It builds the
// Kubernetes client used to read credential Secrets.
func (s *Solver) Initialize(kubeClientConfig *restclient.Config, _ <-chan struct{}) error {
	client, err := kubernetes.NewForConfig(kubeClientConfig)
	if err != nil {
		return fmt.Errorf("webhook: building kubernetes client: %w", err)
	}
	s.kube = client
	return nil
}

// Present builds the provider from the challenge config and creates the record.
func (s *Solver) Present(ch *whapi.ChallengeRequest) error {
	ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
	defer cancel()

	p, err := s.buildProvider(ctx, ch)
	if err != nil {
		return err
	}
	return p.Present(ctx, ch.ResolvedZone, ch.ResolvedFQDN, ch.Key)
}

// CleanUp builds the provider from the challenge config and removes the record.
func (s *Solver) CleanUp(ch *whapi.ChallengeRequest) error {
	ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
	defer cancel()

	p, err := s.buildProvider(ctx, ch)
	if err != nil {
		return err
	}
	return p.CleanUp(ctx, ch.ResolvedZone, ch.ResolvedFQDN, ch.Key)
}

// buildProvider decodes the raw config and hands it, plus a Secret resolver
// scoped to the challenge namespace, to the provider factory.
func (s *Solver) buildProvider(ctx context.Context, ch *whapi.ChallengeRequest) (provider.Provider, error) {
	var raw []byte
	if ch.Config != nil {
		raw = ch.Config.Raw
	}
	return s.factory(ctx, raw, ch.ResourceNamespace, s.resolveSecret)
}

// resolveSecret reads a single key from a Secret in the given namespace and
// trims surrounding whitespace (a common source of "SignatureDoesNotMatch"
// errors when credentials are pasted with a trailing newline).
func (s *Solver) resolveSecret(ctx context.Context, namespace string, sel provider.SecretKeySelector) (string, error) {
	if s.kube == nil {
		return "", fmt.Errorf("webhook: kubernetes client not initialized")
	}
	secret, err := s.kube.CoreV1().Secrets(namespace).Get(ctx, sel.Name, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("webhook: getting secret %s/%s: %w", namespace, sel.Name, err)
	}
	return valueFromSecret(secret, namespace, sel)
}

func valueFromSecret(secret *corev1.Secret, namespace string, sel provider.SecretKeySelector) (string, error) {
	data, ok := secret.Data[sel.Key]
	if !ok {
		return "", fmt.Errorf("webhook: key %q not found in secret %s/%s", sel.Key, namespace, sel.Name)
	}
	return strings.TrimSpace(string(data)), nil
}
