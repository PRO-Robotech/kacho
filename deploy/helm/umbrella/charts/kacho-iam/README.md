# kacho-iam sub-chart (KAC-127 Phase 3)

Identity / Access Management control-plane service for Kachō Cloud.
Account / Project / User / ServiceAccount / Group / Role / AccessBinding +
WebAuthn/Passkey AuthN (Phase 2) + OpenFGA + OPA AuthZ (Phase 3).

This sub-chart is owned by the `kacho-deploy` umbrella and is not intended
for standalone deployment. The umbrella manages cross-cutting Phase 3 concerns
(OpenFGA bootstrap, OPA sidecar shared ConfigMap, NetworkPolicies) at the parent
level; this sub-chart only declares the kacho-iam Deployment + its supporting
ConfigMap / RBAC / Service objects.

## Phase 3 additions

| Feature | Manifestation |
|---|---|
| OPA sidecar | `templates/deployment.yaml` injects container `opa` when `opaSidecar.enabled=true`. |
| `KACHO_IAM_OPENFGA_MODEL_ID` env | Read from Secret `openfga-model-id`, key `current`. |
| `envFrom: opa-bundle-server-config` | Bulk-import bundle TTL / signing / revision knobs. |
| Pod label `kacho.cloud/opa-sidecar=true` | Matched by umbrella NetworkPolicy `opa-sidecar-egress-allowlist`. |
| Annotation `kacho.cloud/openfga-model-id-rev` | Bumped by `openfga-bootstrap-job` to trigger rolling restart on model change. |
| Config `extapi.openfga.authorization-model-id-from-env` | Sources model id from env at runtime (P3-D1 immutable pinning). |
| Config `authz.opaSidecar.url` | Backend interceptor's localhost OPA endpoint (intra-pod, port 8181). |
| Config `authz.conditions.context-cache-ttl-seconds` | Conditions Context map per-Principal cache (60s default). |

## Bundle signing key rotation (180d schedule)

The OPA bundle is signed with JWS ES256 using a private key managed by the
**Phase 2 JWKS rotator CronJob** (`templates/jwks-rotator-cronjob.yaml`). The
public half lives in ConfigMap `kacho-iam-jwks` (umbrella-managed:
`templates/kacho-iam-jwks-configmap.yaml`). OPA sidecars across the fleet load
this PEM at startup and use it to verify each downloaded bundle.

### Rotation cadence

- **180d** is the **public-key** rotation cadence (acceptance §5.6).
- **90d** is the Phase 2 default for **JWKS overall** (Hydra signing,
  access-token JWT etc.). Phase 3 inherits this cadence; an operator MAY
  configure a separate `bundle-only` key with 180d rotation by setting
  `kacho.iam.jwks.rotationDays=180` AND adding a dedicated kid (Phase 4 work
  if separate key rings are required).

### Rotation procedure

Day 0:
1. JWKS rotator CronJob hits `rotation-days` threshold for the current key.
2. Rotator generates new ES256 keypair, encrypts private with KMS-CMK
   (`KACHO_IAM_JWKS_ENC_KEY`), inserts into `oidc_jwks_keys` table.
3. Rotator marks new row `current=true`, demotes old row `current=false` but
   retains `valid=true` (still usable for verification).
4. Rotator updates ConfigMap `kacho-iam-jwks` — both old kid and new kid
   PEM entries present.
5. `kacho-iam` bundle server reads `KACHO_OPA_BUNDLE_KEY_ROTATION_GRACE_SECONDS`
   (default 7200 = 2h) and signs bundles with new key during the grace window.

Day 0 + 2h (grace expiry):
6. Rotator marks old row `valid=false`. Bundle server stops accepting old kid.
7. Sidecars that have not pulled a new bundle within the grace window fail
   signature verification → fail-closed → alert `opa_bundle_signature_failures_total`.
8. Operator's runbook: force rolling restart of all kacho-* pods. New pods
   re-load the latest PEM ConfigMap and successfully pull/verify the new
   bundle.

Day 0 + 180d (public-key audit cycle):
9. Operator reviews `oidc_jwks_keys` table audit log. Any keys older than
   180d that have been `valid=false` for > 7d are safe to purge (no in-flight
   verification possible).

### Disaster: signing key compromise

If the **private** signing key leaks (e.g., dev-cluster Secret leak):

1. Trigger immediate rotation: `kubectl create job --from=cronjob/kacho-iam-jwks-rotator
   kacho-iam-jwks-emergency-rotate-$(date +%s) -n kacho-system`.
2. After rotator completes (≤5s), force rolling restart of every kacho-*
   pod: `kubectl rollout restart deployment -n kacho-system -l app.kubernetes.io/part-of=kacho`.
3. Set `bundle.keyRotationGraceSeconds=60` for the duration of incident
   response (compressed window — accept temporary fail-closed during sidecar
   pull lag).
4. Invalidate ALL existing OPA bundles in CDN/cache (force re-pull): bump
   `KACHO_BUILD_SHA` env on kacho-iam Deployment (annotation change forces
   re-render of `.manifest.revision`, OPA sidecars detect new revision and
   re-pull).
5. Audit: query `oidc_jwks_keys` for any `current=true, valid=true` rows older
   than incident timestamp — anything else is suspect.

## Sealed-secret integration (operator setup)

For production deployments, the AES-GCM KMS key
(`KACHO_IAM_JWKS_ENC_KEY`, default Secret name `kacho-iam-jwks-enc-key`) must
be provisioned BEFORE first-deploy of this chart. Two supported patterns:

### Pattern A: external-secrets-operator (recommended)

```yaml
apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata:
  name: kacho-iam-jwks-enc-key
  namespace: kacho-system
spec:
  refreshInterval: 1h
  secretStoreRef:
    name: vault-backend
    kind: ClusterSecretStore
  target:
    name: kacho-iam-jwks-enc-key
    creationPolicy: Owner
  data:
    - secretKey: enc_key
      remoteRef:
        key: kacho/prod/iam/jwks-enc-key
        property: aes_gcm_b64url
```

### Pattern B: sealed-secrets (legacy)

```bash
echo -n "$(openssl rand -base64 32 | tr -d '=')" | \
  kubeseal --raw --namespace kacho-system --name kacho-iam-jwks-enc-key > enc_key.sealed
```

Then in `clusters/<cluster>/overrides.yaml`:
```yaml
extraSecrets:
  - name: kacho-iam-jwks-enc-key
    sealedData:
      enc_key: <contents of enc_key.sealed>
```

## Troubleshooting

| Symptom | Likely cause | Remediation |
|---|---|---|
| `OPA sidecar /health returns {"bundles":{"...":{"active_revision":""}}}` | First bundle pull pending | Wait ≤90s on dev / ≤65min on prod (OPA pollMinDelaySeconds). |
| `OPA sidecar logs: signature verification failed: invalid key` | Public-key ConfigMap stale | `kubectl rollout restart deployment -n kacho-system -l app.kubernetes.io/part-of=kacho`. |
| `kacho-iam logs: KACHO_IAM_OPENFGA_MODEL_ID is empty` | bootstrap-job hasn't run yet | `kubectl get job -n kacho-system openfga-bootstrap` — check completion status. |
| `Backend gRPC returns PermissionDenied: "authz unavailable"` | FGA engine down | `kubectl get po -n kacho-system -l app.kubernetes.io/name=openfga` — restart if not Ready 3/3. |
| `Backend gRPC returns PermissionDenied: "policy: <msg>"` | OPA deny-rule fired (expected) | Review `<msg>` against acceptance §4.6 Rego rules. |

## See also

- `docs/specs/sub-phase-3.3-iam-authz-fga-conditions-opa-acceptance.md` — full design + GWT.
- `docs/superpowers/specs/2026-05-19-iam-prod-ready-next-gen-design.md` §4 — DSL v2 + Rego.
- Umbrella templates (Phase 3): `helm/umbrella/templates/{openfga-*,opa-*}*.yaml`.
