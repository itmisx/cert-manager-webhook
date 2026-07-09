# cert-manager-webhook — Makefile
#
# Common targets:
#   make build             build the webhook binary into ./bin
#   make test              run unit tests (fast, no cluster needed)
#   make test-conformance  run the cert-manager DNS01 conformance suite
#   make image             build the container image
#   make helm-lint         lint the Helm chart

IMAGE_REPO ?= ghcr.io/itmisx/cert-manager-webhook
IMAGE_TAG  ?= latest
IMAGE      := $(IMAGE_REPO):$(IMAGE_TAG)

# Kubernetes version whose envtest binaries (etcd + kube-apiserver) the
# conformance suite runs against.
ENVTEST_K8S_VERSION ?= 1.31.0

BIN_DIR := $(CURDIR)/bin

.PHONY: all
all: test build

.PHONY: build
build:
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o $(BIN_DIR)/webhook ./cmd/webhook

.PHONY: test
test:
	go test ./... -count=1

.PHONY: vet
vet:
	go vet ./...

.PHONY: tidy
tidy:
	go mod tidy

# setup-envtest downloads the kubebuilder control-plane binaries the conformance
# suite needs, then exports KUBEBUILDER_ASSETS for the tagged test run.
# Requires TEST_ZONE_NAME and a real credential Secret (see testdata/).
.PHONY: test-conformance
test-conformance:
	@command -v setup-envtest >/dev/null 2>&1 || \
		GOBIN=$(BIN_DIR) go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest
	KUBEBUILDER_ASSETS="$$($(BIN_DIR)/setup-envtest use -p path $(ENVTEST_K8S_VERSION) 2>/dev/null || setup-envtest use -p path $(ENVTEST_K8S_VERSION))" \
		go test -tags conformance -count=1 -v .

.PHONY: image
image:
	docker build -t $(IMAGE) .

.PHONY: image-push
image-push:
	docker buildx build --platform linux/amd64,linux/arm64 -t $(IMAGE) --push .

.PHONY: helm-lint
helm-lint:
	helm lint deploy/cert-manager-webhook-dns

.PHONY: helm-template
helm-template:
	helm template cert-manager-webhook-dns deploy/cert-manager-webhook-dns

.PHONY: clean
clean:
	rm -rf $(BIN_DIR)
