# Adding a new DNS provider

The webhook is built around one tiny interface so that a new provider
(Cloudflare, Google Cloud DNS, …) never has to touch the ACME/webhook plumbing.
The four shipped providers — `alidns`, `tencentcloud`, `huaweicloud`,
`baiducloud` — are your reference implementations.

```
internal/
├── dnsutil/        # FQDN → (zone, RR) helpers, provider-agnostic
├── provider/
│   ├── provider.go     # the Provider interface + Factory + SecretResolver
│   ├── alidns/         # reference: split RR, list-by-subdomain
│   ├── tencentcloud/   # reference: "no records" error handling
│   ├── huaweicloud/    # reference: zone-ID lookup, quoted TXT values
│   └── baiducloud/     # reference: zoneName + RR, simple list-by-RR
└── webhook/        # the generic cert-manager Solver (never changes)
```

## The contract

```go
type Provider interface {
    Present(ctx context.Context, zone, fqdn, value string) error
    CleanUp(ctx context.Context, zone, fqdn, value string) error
}
```

`zone` and `fqdn` arrive rooted (trailing dot), exactly as cert-manager produces
them. Use `dnsutil.UnFqdn` and `dnsutil.ToRR` to split them for your API.

Both methods **must be idempotent**: `Present` may run when the record already
exists; `CleanUp` may run when it is already gone. Neither is an error.

## Steps

1. **Create the package** `internal/provider/cloudflare/cloudflare.go`:

   ```go
   package cloudflare

   const Name = "cloudflare"

   type Config struct {
       APITokenRef provider.SecretKeySelector `json:"apiTokenRef"`
       // ...
   }

   func New(ctx context.Context, rawConfig []byte, namespace string, resolve provider.SecretResolver) (provider.Provider, error) {
       // decode config, resolve credentials via `resolve`, build SDK client
   }
   ```

2. **Implement `Present`/`CleanUp`** against your provider's SDK. Match the TXT
   record on its *value* in `CleanUp` so concurrent challenges (wildcard + apex)
   don't clobber each other.

3. **Register it** in `cmd/webhook/main.go`:

   ```go
   cmd.RunWebhookServer(GroupName,
       webhook.NewSolver(alidns.Name, alidns.New),
       webhook.NewSolver(tencentcloud.Name, tencentcloud.New),
       webhook.NewSolver(huaweicloud.Name, huaweicloud.New),
       webhook.NewSolver(baiducloud.Name, baiducloud.New),
       webhook.NewSolver(cloudflare.Name, cloudflare.New), // <— add
   )
   ```

4. **Add a unit test** with a fake SDK client (see
   `internal/provider/alidns/alidns_test.go`) and, optionally, wire the
   conformance suite for it.

That's it — no changes to the Solver, RBAC, Helm chart, or APIService. Users
select your provider by setting `solverName: cloudflare` in their Issuer.
