<div align="center">

# 🔐 cert-manager-webhook

**A cert-manager ACME DNS-01 solver webhook for Chinese cloud DNS providers**

Alibaba Cloud · Tencent Cloud · Huawei Cloud · Baidu AI Cloud — one binary, all four

English · [简体中文](./README.md)

![License](https://img.shields.io/badge/License-MIT-blue.svg) ![Go](https://img.shields.io/badge/Go-1.24+-00ADD8?logo=go&logoColor=white) ![Kubernetes](https://img.shields.io/badge/Kubernetes-v1.27+-326CE5?logo=kubernetes&logoColor=white) ![cert-manager](https://img.shields.io/badge/cert--manager-v1.0+-32C671.svg)

</div>

---

cert-manager ships no built-in DNS-01 solver for these Chinese cloud providers. This project fills that gap with **one pluggable binary**: switch provider by `solverName`, each calling its vendor's current official SDK. It is **idempotent, concurrency-safe**, and can issue **wildcard certificates**.

| ☁️ Provider | `solverName` | SDK |
|:-----------|:------------|:----|
| 🟠 Alibaba Cloud DNS (AliDNS) | `alidns` | `alidns-20150109/v5` |
| 🔵 Tencent Cloud DNS (DNSPod) | `tencentcloud` | `tencentcloud-sdk-go` (dnspod v20210323) |
| 🔴 Huawei Cloud DNS | `huaweicloud` | `huaweicloud-sdk-go-v3` (dns v2) |
| ⚪ Baidu AI Cloud DNS | `baiducloud` | `bce-sdk-go` (dns) |

> [!WARNING]
> Before your first issuance, read the [⚠️ Important notes](#-important-notes) at the end. The two most common mistakes — **issuing from staging → untrusted certs** and **CN-vs-SAN → browser rejection** — will otherwise waste your time.

## 📋 Prerequisites

- **Kubernetes cluster** — v1.27+
- **[cert-manager](https://cert-manager.io/docs/installation/) v1.0+** — the chart uses the `cert-manager.io/v1` API and relies on its `cainjector` for CA injection; a recent supported release is recommended
- **Domain + cloud API credentials** — your domain must be hosted on one of these cloud DNS services:

  | Provider | Required permission |
  |:--------|:-------------------|
  | Alibaba Cloud | a RAM user with `AliyunDNSFullAccess` |
  | Tencent Cloud | a CAM sub-account with `QcloudDNSPodFullAccess` |
  | Huawei Cloud | an AK/SK whose account has the **DNS Administrator** role |
  | Baidu AI Cloud | an AK/SK with DNS (云解析 DNS) permissions |

## 📦 Install

Both the image and the Helm chart are published to GitHub Container Registry (GHCR) by the [release workflow](./.github/workflows/release.yml) on every tag, so you can install **without cloning the repo** (Helm 3.8+ supports OCI natively):

```bash
helm install cert-manager-webhook-dns \
  oci://ghcr.io/itmisx/charts/cert-manager-webhook-dns \
  --namespace cert-manager \
  --set groupName=acme.example.com
```

> [!TIP]
> - Installs the latest version by default; pin a specific one with `--version <x.y.z>`.
> - The chart defaults to the image `ghcr.io/itmisx/cert-manager-webhook` (tagged with the chart `appVersion`), so no extra flags are needed.
> - `groupName` can be any domain you control; it just has to match every Issuer's `webhook.groupName`.

<details>
<summary>💻 Install from source (for development)</summary>

```bash
git clone https://github.com/itmisx/cert-manager-webhook.git
helm install cert-manager-webhook-dns ./deploy/cert-manager-webhook-dns \
  -n cert-manager --set groupName=acme.example.com
```

</details>

## 🚀 Quick start

The example below uses AliDNS; the other providers are identical — just swap `solverName` and the `config` block per the [Configuration reference](#-configuration-reference). A full, editable set lives in [`examples/`](./examples/).

### 1️⃣ Create the credential Secret

> [!NOTE]
> For a ClusterIssuer, the Secret must live in **cert-manager's namespace** (default `cert-manager`).

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: alidns-credentials
  namespace: cert-manager
stringData:
  access-key-id: "YOUR_ACCESS_KEY_ID"
  access-key-secret: "YOUR_ACCESS_KEY_SECRET"
```

Save as `secret.yaml`, then apply with `kubectl apply -f secret.yaml`.

### 2️⃣ Create a ClusterIssuer

```yaml
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: letsencrypt
spec:
  acme:
    # Test with staging first: generous rate limits, but browser-untrusted certs.
    # Once it works, switch to the commented production URL below and re-issue
    # (delete the old cert Secret to force a fresh request).
    server: https://acme-staging-v02.api.letsencrypt.org/directory
    # server: https://acme-v02.api.letsencrypt.org/directory
    email: you@example.com
    # Required — cannot be omitted. Secret holding the ACME account private key.
    # You only supply the name; cert-manager generates the key and writes it on
    # first account registration, so you don't create this Secret yourself.
    # Why: it persists the ACME account key so restarts/renewals reuse the same
    #      account instead of re-registering (which hits Let's Encrypt rate limits).
    # Note: this is the ACME *account* key, not the issued certificate's key
    # (the cert's key lives in the Certificate's secretName).
    privateKeySecretRef:
      name: letsencrypt-account-key
    solvers:
      - dns01:
          webhook:
            # The webhook's API group; MUST match the groupName used at Helm install.
            groupName: acme.example.com
            # Which provider's solver to use: alidns / tencentcloud / huaweicloud / baiducloud.
            solverName: alidns
            config:
              regionId: cn-hangzhou
              # Reference to the "AccessKey ID": name = Secret name, key = the key in it.
              accessKeyIDRef:
                name: alidns-credentials
                key: access-key-id
              # Reference to the "AccessKey Secret": name = Secret name, key = the key in it.
              accessKeySecretRef:
                name: alidns-credentials
                key: access-key-secret
```

### 3️⃣ Request a certificate

`dnsNames` become the certificate's SANs (the only thing browsers check); DNS-01 can also issue **wildcards**:

```yaml
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: example-com-tls
  namespace: default
spec:
  secretName: example-com-tls
  issuerRef:
    name: letsencrypt
    kind: ClusterIssuer
  dnsNames:
    - example.com
    - www.example.com
    - "*.example.com"
```

## ⚙️ Configuration reference

`groupName` must match the Helm `groupName`; `solverName` selects the provider. Credentials always come from a Kubernetes Secret in the challenge namespace.

> [!TIP]
> Every provider's credential fields are **unified** as `accessKeyIDRef` / `accessKeySecretRef`, mapping to the Secret's default `access-key-id` / `access-key-secret` keys. No matter which cloud you use, the config looks the same.

<details open>
<summary><b>🟠 <code>solverName: alidns</code> (Alibaba Cloud)</b></summary>

| Field | Type | Required | Default | Description |
|:-----|:----|:--------:|:-------|:-----------|
| `accessKeyIDRef` | object | ✅ | — | Secret ref to the AccessKey **ID** (`key` defaults to `access-key-id`). |
| `accessKeySecretRef` | object | ✅ | — | Secret ref to the AccessKey **Secret** (`key` defaults to `access-key-secret`). |
| `regionId` | string | — | *(unset)* | Optional region, e.g. `cn-hangzhou`. DNS is a global service. |
| `endpoint` | string | — | `alidns.aliyuncs.com` | Override the API endpoint. |
| `ttl` | int | — | `600` | TXT record TTL (seconds). Clamped to ≥ 600 (AliDNS free-plan minimum). |

</details>

<details>
<summary><b>🔵 <code>solverName: tencentcloud</code> (Tencent Cloud / DNSPod)</b></summary>

| Field | Type | Required | Default | Description |
|:-----|:----|:--------:|:-------|:-----------|
| `accessKeyIDRef` | object | ✅ | — | Secret ref to the **SecretId** (`key` defaults to `access-key-id`). |
| `accessKeySecretRef` | object | ✅ | — | Secret ref to the **SecretKey** (`key` defaults to `access-key-secret`). |
| `region` | string | — | *(unset)* | Optional; DNSPod is a global service. |
| `ttl` | int | — | `600` | TXT record TTL (seconds). |

</details>

<details>
<summary><b>🔴 <code>solverName: huaweicloud</code> (Huawei Cloud)</b></summary>

| Field | Type | Required | Default | Description |
|:-----|:----|:--------:|:-------|:-----------|
| `accessKeyIDRef` | object | ✅ | — | Secret ref to the **Access Key (AK)** (`key` defaults to `access-key-id`). |
| `accessKeySecretRef` | object | ✅ | — | Secret ref to the **Secret Key (SK)** (`key` defaults to `access-key-secret`). |
| `region` | string | — | `cn-north-4` | DNS endpoint region. |
| `ttl` | int | — | `300` | TXT record TTL (seconds). |

</details>

<details>
<summary><b>⚪ <code>solverName: baiducloud</code> (Baidu AI Cloud)</b></summary>

| Field | Type | Required | Default | Description |
|:-----|:----|:--------:|:-------|:-----------|
| `accessKeyIDRef` | object | ✅ | — | Secret ref to the **Access Key** (`key` defaults to `access-key-id`). |
| `accessKeySecretRef` | object | ✅ | — | Secret ref to the **Secret Key** (`key` defaults to `access-key-secret`). |
| `endpoint` | string | — | `dns.baidubce.com` | Override the API endpoint. |
| `ttl` | int | — | `300` | TXT record TTL (seconds). |

</details>

## 🔍 Verify & troubleshoot

Check the webhook is registered and watch issuance:

```bash
kubectl get apiservice v1alpha1.acme.example.com   # should be Available=True
kubectl describe certificate example-com-tls
kubectl describe challenge                          # DNS-01 presenting progress
kubectl -n cert-manager logs deploy/cert-manager-webhook-dns
```

Inspect the issued certificate's **issuer and SAN** (fastest way to catch the staging trap):

```bash
kubectl get secret example-com-tls -o jsonpath='{.data.tls\.crt}' \
  | base64 -d | openssl x509 -noout -issuer -subject -ext subjectAltName
```

<div align="center">

**Symptom quick reference**

</div>

| 🚨 Symptom | Likely cause | ✅ Fix |
|:----------|:------------|:------|
| Browser says "not trusted"; issuer contains `(STAGING)` or `YR2` under a staging root | Issued from Let's Encrypt **staging** | Switch `spec.acme.server` to the production URL, delete the old cert Secret, re-issue |
| `NET::ERR_CERT_COMMON_NAME_INVALID` | Hostname not in **SAN** | Add it to `spec.dnsNames` |
| Auth error in logs (`SignatureDoesNotMatch`, `AuthFailure`, `APISIG.*`, …) | Wrong or whitespace-padded credentials | Recreate the Secret (the webhook already trims whitespace) |
| `... is not contained in zone ...` | The zone hosted at your provider isn't the authoritative zone for the name | Host the correct zone at your DNS provider |
| Challenge stuck `presenting` | The credential lacks DNS write permission | Grant the provider's DNS full-access policy (see [Prerequisites](#-prerequisites)) |

## 🏗️ Architecture

One webhook binary registers one **solver per provider** under a single Kubernetes API group, selected by the Issuer's `solverName`. A generic `webhook.Solver` adapts the cert-manager interface to a tiny `provider.Provider` interface — so **adding a provider is one new package plus one line in `main.go`**, and the ACME/RBAC/TLS plumbing never changes.

```text
cert-manager-webhook/
├── cmd/webhook/            # main(): registers providers, starts the API server
├── internal/
│   ├── dnsutil/            # FQDN → (zone, RR) helpers (provider-agnostic)
│   ├── provider/           # Provider interface + implementations (alidns / tencentcloud / huaweicloud / baiducloud)
│   └── webhook/            # generic cert-manager Solver → Provider adapter
├── deploy/cert-manager-webhook-dns/   # Helm chart
├── examples/               # ready-to-edit Issuer / Certificate / Secret
└── testdata/               # cert-manager conformance fixtures
```

> [!NOTE]
> See [docs/adding-a-provider.md](./docs/adding-a-provider.md) for the full guide.

## 🛠️ Development

```bash
make test        # fast unit tests, no cluster required
make build       # build ./bin/webhook
make vet         # go vet
make helm-lint   # lint the chart (needs helm)
```

<details>
<summary><b>🧪 Conformance tests (hit real DNS)</b></summary>

<br>

cert-manager's official DNS-01 suite talks to **real DNS**, so it is gated behind a build tag and skipped by default. Fill `testdata/<provider>-solver/config.json` and a `secret.yaml` with real credentials (see `testdata/alidns-solver/README.md`), then:

```bash
export TEST_ZONE_NAME="example.com."   # trailing dot required
export TEST_PROVIDER=alidns            # or tencentcloud / huaweicloud / baiducloud
make test-conformance                  # downloads envtest binaries and runs the suite
```

</details>

## ⚠️ Important notes

> [!IMPORTANT]
> The two items below are the mistakes most likely to make certs "untrusted / rejected by browsers". **Please read them.**

<details>
<summary><b>Pitfall 1: Staging vs. production → "untrusted certificate" (the <code>YR2</code> / <code>CN</code> confusion)</b></summary>

<br>

Almost every cert-manager DNS-webhook tutorial copies this ACME server URL:

```diff
- https://acme-staging-v02.api.letsencrypt.org/directory   # ❌ staging — untrusted
```

Let's Encrypt's **staging** environment exists for testing. Its certificates are signed by **deliberately untrusted fake roots** that no browser or OS ships, so every browser shows a security warning. If you inspect such a cert you'll see an issuer like **`(STAGING) Ersatz Emmer YR2`** chaining up to **`(STAGING) Yonder Yam Root YR`**.

> **What is "YR2"?** In November 2025 Let's Encrypt introduced a new "Generation Y" CA hierarchy. `YR1`/`YR2`/`YR3` are its RSA intermediate CAs. In **production** they are legitimate; in **staging** the look-alikes (`(STAGING) … YR2`) are intentionally untrusted. Seeing `YR2` as the *issuer* is not itself a bug — the bug is almost always that you issued from **staging**.

**Fix:** switch `server` to the production directory URL and re-issue:

```diff
+ https://acme-v02.api.letsencrypt.org/directory           # ✅ production — trusted
```

Use staging only while debugging, then switch back to production (a staging Issuer for testing is in [`examples/clusterissuer-letsencrypt-staging.yaml`](./examples/clusterissuer-letsencrypt-staging.yaml)).

> [!TIP]
> In production, the new Gen-Y root (Root YR) is cross-signed to the widely-trusted ISRG Root X1 by default, so no action is usually needed; to pin the compatible chain explicitly, set `preferredChain: "ISRG Root X1"` on the Issuer. See [Let's Encrypt chains of trust](https://letsencrypt.org/certificates/).

</details>

<details>
<summary><b>Pitfall 2: Common Name vs. SAN → Chrome/Safari reject the cert</b></summary>

<br>

Since **Chrome 58 (2017)**, browsers **completely ignore the certificate Common Name (CN)** and validate the hostname **only against the Subject Alternative Names (SAN)**. A cert with a CN but no matching SAN fails with `NET::ERR_CERT_COMMON_NAME_INVALID`.

In cert-manager, SANs come from **`spec.dnsNames`**. So always:

```yaml
spec:
  dnsNames:            # ← these become SANs; browsers check THESE
    - example.com
    - www.example.com
  # commonName: example.com   # optional; if set it MUST also be in dnsNames
```

Let's Encrypt always issues SANs, so with a correct production Issuer + a `dnsNames` list, both problems disappear.

</details>

## 📄 License

Released under the [MIT](./LICENSE) License.
