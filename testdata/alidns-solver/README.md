# Conformance test fixtures

`config.json` is merged into the `ChallengeRequest.Config` that cert-manager's
conformance suite passes to the solver. To run the suite (`make test-conformance`)
you must:

1. Edit `config.json` so the Secret refs match a real AliDNS credential Secret.
2. Create that Secret in the envtest control plane. The simplest way is to place
   a `secret.yaml` next to this file — the fixture applies every manifest in this
   directory:

   ```yaml
   apiVersion: v1
   kind: Secret
   metadata:
     name: alidns-credentials
   type: Opaque
   stringData:
     access-key-id: "YOUR_ACCESS_KEY_ID"
     access-key-secret: "YOUR_ACCESS_KEY_SECRET"
   ```

3. Export `TEST_ZONE_NAME` to a zone you actually control on AliDNS, e.g.
   `export TEST_ZONE_NAME="example.com."` (note the trailing dot).

> Do **not** commit a `secret.yaml` containing real credentials — it is listed in
> `.gitignore`.
