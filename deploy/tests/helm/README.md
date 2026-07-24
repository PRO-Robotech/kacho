# `tests/helm/` — Helm manifest-assertion harness (SEC-F)

**NEW test infrastructure** (acceptance SEC-F §«Тест-харнессы»; there is *no*
"precedent S7.1" — manifest tests are built from scratch here). These are
offline, deterministic black-box assertions over `helm template` output —
they need neither a kind cluster nor the sibling service-chart checkouts
(`file://../../../kacho-*/deploy`). They render the **`cert-manager-config`
subchart standalone** and the **umbrella openfga-NetworkPolicy template in
isolation**, then assert structure with `yq`/`grep`.

## What is covered (TDD RED→GREEN, SEC-F)

| Script | Scenario(s) | Asserts |
|---|---|---|
| `cert-manager-internal-ca-test.sh` | SEC-F-01/02/03/04/14 | internal-CA issuer chain (selfSigned `kacho-selfsigned` root → CA-root `Certificate` → `kacho-internal-ca` CA-issuer), per-svc `Certificate` ×2 in separate secrets, server-cert DNS-SAN, client-cert SPIRE URI-SAN, exactly-one selfSigned issuer, external `letsencrypt-*` unchanged, `internalCA.enabled=false` → none rendered |
| `openfga-networkpolicy-test.sh` | SEC-F-12/14 | openfga ingress-from-iam-only `NetworkPolicy` with `app.kubernetes.io/name` selectors; absent when flag off |
| `mtls-values-profile-test.sh` | SEC-F-05/06/07/14 | `mtls.enabled`/`mtls.edges.*` shape, `values.mtls.yaml` overlay flips on, `spiffe.namespace` single-source, NLB spire-registration aligned `kacho-nlb` |
| `hydra-jwks-url-test.sh` | KAC-127 Phase 2 | api-gateway resolves a REACHABLE cluster-internal Hydra JWKS URL: sibling chart renders `KACHO_HYDRA_JWKS_URL` from `hydra.jwksUrl` (unset → no env, zero regression); umbrella `values.dev.yaml` + `values.prod.yaml` point the gateway pod at `kacho-umbrella-hydra-public:4444/.well-known/jwks.json` (never localhost / public ingress) |
| `jobs-cronjobs-hardening-test.sh` | INFRA sec-hardening r3 | every umbrella-owned Job/CronJob (openfga-bootstrap, openfga-postgres-init, kacho-iam jwks-rotator, kacho-geo data-migration) carries the restricted PSS floor (pod + container: runAsNonRoot, readOnlyRootFilesystem, drop ALL caps, no priv-esc, seccomp RuntimeDefault); the public api-gateway ingress backends the EXTERNAL-marked `tls` listener with `backend-protocol: GRPCS` (never the internal-origin `cmux`/GRPC path that serves Internal\* REST) — see `docs/architecture/known-divergences.md` |
| `bootstrap-mint-operator-identity-test.sh` | #58 hardening (A-1) | the cluster-admin token mint (`InternalBootstrapTokenService/MintBootstrapToken`) is gated ONLY by the caller's client-certificate SPIFFE SAN, and the two halves of that gate are rendered by two different manifests — so this asserts they AGREE: the umbrella issues a dedicated client-auth-only operator leaf (`mtls.bootstrapOperator`) and `kacho-iam` allow-lists **that exact SAN** (`authn.bootstrap-mint.allowed-client-sans`) in both `values.dev.yaml` and `values.dev-prod.yaml`; the identity is NOT the api-gateway's (the gateway must never be a minter); the chart default is an EMPTY allow-list (deny everyone, no default caller); `mtls.enabled=false` issues no operator identity at all. Uses `grep` only — no `yq` flavour dependency |
| `networkpolicy-datastore-test.sh` | INFRA sec-hardening r5b | per-datastore Postgres `:5432` ingress allowlist (`networkPolicy.datastore.enabled`): default-off renders nothing; enabled renders one Ingress-only `NetworkPolicy` per pg instance scoped to `app.kubernetes.io/name=pg-<svc>` + `component=primary` on :5432, allowing only the declared consumer selectors (implicit-deny for the rest — CIS Kubernetes 5.3.2 / OWASP A05:2021) — see `docs/architecture/known-divergences.md` §4 |

## Running

```sh
make helm-manifest-test          # every tests/helm/*-test.sh
bash tests/helm/cert-manager-internal-ca-test.sh
```

Each script is self-contained: it shells `helm template` against the in-repo
chart, pipes through `yq`/`grep`, and exits non-zero on the first failed
assertion (printing `FAIL: <reason>`). A green run prints `PASS: <script> (<n> assertions)`.
No `TODO`/`SKIP`/commented-out asserts (ban #11/#13).
