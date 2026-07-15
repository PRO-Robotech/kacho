# kacho-deploy — known divergences (by-design)

Deliberate, reviewed departures from a naive default. Not bugs, not tech-debt —
each entry states the decision, the rationale, and the guardrail that keeps it
honest. Add here instead of opening an issue when a choice is intentional.

## 1. api-gateway public ingress is umbrella-owned, backends the `tls` listener

**Decision.** The umbrella renders its own `templates/api-gateway-ingress.yaml`
for the public api-gateway ingress and disables the api-gateway **sub-chart's**
ingress via `api-gateway.ingress.enabled=false` (`helm/umbrella/values.yaml`).
The umbrella ingress backends the api-gateway pod's **`tls`** Service port
(:8443) with `nginx.ingress.kubernetes.io/backend-protocol: "GRPCS"`.

**Why (security — ban #6 / defense-in-depth).** The api-gateway pod runs two
HTTP surfaces behind cmux:

- the plaintext **`cmux`** listener (:8080) — the *cluster-internal* surface. It
  is **not** wrapped by `listenerorigin.ExternalListener`, so the REST dispatcher
  never tags requests arriving there as external-origin and **serves Internal\***
  **REST** (AddressPool, Region/Zone admin, InternalAuthzCache, …). This is
  intentional for in-cluster UI / admin / port-forward, which dial the ClusterIP
  `cmux` port directly.
- the **`tls`** listener (:8443) — wrapped by `listenerorigin.ExternalListener`
  in `cmd/api-gateway/main.go`. Requests arriving here are tagged external-origin
  and the REST dispatcher returns **404 for every Internal\*** path — the exact
  behaviour the api-gateway external-isolation fix introduced.

The api-gateway sub-chart's *default* ingress points at `cmux` (its release-time
default). Routed through the public ingress, that would publish Internal\* REST on
the external edge — violating ban #6 ("`Internal.*` methods не публикуются на
external endpoint"). The public edge **must** traverse the external-marked `tls`
listener so the external-404 gate is live rather than dead code. In-cluster
consumers keep reaching Internal\* on the `cmux` ClusterIP port unchanged.

**Why umbrella-owned rather than a sub-chart values override.** The sub-chart
ingress template hard-codes `backend.service.port.name: cmux` with no value hook
to retarget it, and the sub-chart is vendored from the sibling `kacho-api-gateway`
repo (gitignored `helm/umbrella/charts/api-gateway/` + `*.tgz`), so it cannot be
edited from this repo. Disabling it and rendering an umbrella-owned ingress keeps
the fix entirely within `kacho-deploy`'s git-tracked files, verifiable by
`helm template` / `helm lint`. `host`, `tls.secretName` and `proxyReadTimeout`
are still inherited from the api-gateway sub-chart values (parent reads
`.Values."api-gateway".*`), so per-cluster overlays that set the ingress host keep
working.

**Follow-up (sibling repo, out of scope here).** The api-gateway sub-chart's own
default ingress (`kacho-api-gateway/deploy/templates/ingress.yaml`) still backends
`cmux`; it is only harmless because the umbrella disables it. A sibling-repo
change to backend the `tls` port (so a *standalone* api-gateway install is also
safe) should be tracked as a `kacho-api-gateway` issue.

**Guardrails.**
- `tests/helm/jobs-cronjobs-hardening-test.sh` §5 asserts exactly one Ingress
  named `api-gateway` renders, with `backend.service.port.name == tls` and
  `backend-protocol == GRPCS` (fails on any regression to `cmux`/`GRPC`).
- The umbrella ingress template `fail`s the render if
  `apiGatewayIngress.enabled=true` while `api-gateway.tls.enabled=false` (no
  external-marked TLS listener to route to) — no silent fallback to the insecure
  `cmux` path.

## 2. Auxiliary Jobs / CronJobs carry the same restricted PSS floor as Deployments

**Decision.** Every umbrella-owned pod-bearing workload — not only the
long-running Deployments — enforces the restricted Pod Security Standards floor
(`runAsNonRoot`, `runAsUser` non-zero, `allowPrivilegeEscalation:false`,
`readOnlyRootFilesystem:true`, `capabilities.drop:[ALL]`,
`seccompProfile:RuntimeDefault`). This includes the `openfga-bootstrap` and
`openfga-postgres-init` hook Jobs, the `kacho-iam` `jwks-rotator` CronJob, and the
`kacho-geo` `data-migration` Job. A writable `emptyDir` is mounted at `/tmp`
(`HOME=/tmp`) so the read-only rootfs does not break kubectl discovery cache /
psql history.

**Why.** These Jobs hold real blast-radius credentials — the bootstrap Job's
ServiceAccount can create Secrets (get/update scoped to two named secrets) and
patch a resourceName-scoped set of consumer Deployments; the
jwks-rotator injects the JWKS AES encryption key + DB password; the postgres-init
Job carries Postgres admin creds. A root, writable-rootfs, full-capability
container maximises the damage from a compromised image or script. The restricted
floor contains it identically to the serve pods (CIS Kubernetes 5.2 / CWE-250).

**Guardrails.**
- `tests/helm/jobs-cronjobs-hardening-test.sh` §1–4 asserts the pod- and
  container-level floor on all four workloads.
- The CI Trivy IaC gate (`.github/workflows/ci.yaml`, `helm-lint` job) now
  enumerates **all** kacho-owned Job/CronJob/Deployment templates (r2 only
  covered the Deployments + two hook Jobs), so a future hardening regression on
  any of them fails the build.

## 3. Image references default to mutable tags; digest-pinning is opt-in

**Decision.** The umbrella `values.yaml` / `values.dev.yaml` / `values.prod.yaml`
reference every workload image by a **mutable registry tag** (`:main-<sha>`, and
for the not-yet-published `kacho-geo` a `:main-latest` placeholder) with
`imagePullPolicy: IfNotPresent`. Immutable `@sha256:` digest pinning exists but is
an **opt-in values override** (`kacho-iam.image.digest`, `kacho-geo.imageDigest`,
`values.digests.example.yaml`, `docs/security/image-digest-pinning.md`), not the
committed default.

**Why (not a defect).**
- **Digests are a release-pipeline artifact, not a source-committed constant.** A
  real `sha256` is only known after CI builds and pushes the image, and it changes
  on every rebuild. Committing *fabricated* digest defaults would be worse than a
  tag (unverifiable, and they rot on the next build). The example overlay ships
  `sha256:REPLACE_WITH_REAL_DIGEST` on purpose — it is a template, layered last
  (`-f values.digests.yaml`) by the deploy pipeline that resolves the real digests.
- **`imagePullPolicy: IfNotPresent` is required for the kind dev target.** `make
  dev-up` builds `:dev` images and `kind load docker-image`s them into the node;
  `Always` would force a registry pull of a tag that does not exist there and break
  the offline dev/CI flow.
- **`kacho-geo:main-latest`** is a documented placeholder — the geo image is not
  yet pushed to the registry (epic kacho-geo); the real `:main-<sha>` tag + pull
  secret are provisioned at deploy time per-cluster (`values.yaml` block comment).
  The prod image-tag overlay `values.prorobotech.yaml` therefore **intentionally has
  no `kacho-geo` entry** while it pins every other service (vpc/compute/api-gateway/
  ui/iam/nlb) to an immutable `:main-<sha>`: there is no published geo image to pin
  to yet. This is not an accidental omission — until the geo image ships, geo is not
  deployable from that overlay at all. **Obligation on publish:** when the geo image
  is first pushed, add a `kacho-geo` entry to `values.prorobotech.yaml` pinned to the
  immutable `:main-<sha>` (or a `sha256:` digest), same as every other service, so the
  production overlay never resolves geo through the mutable `main-latest` tag.

Making digest pinning the *committed default* would either bake in placeholder
(non-deployable) values or couple every source commit to a specific build hash —
both churn without adding provenance the release pipeline does not already own.

**Guardrails.**
- `tests/helm/sec-hardening-test.sh` §5 asserts the digest-pin override is honoured
  for `kacho-iam` and `kacho-geo` (`repository@sha256:...`), so the opt-in path
  stays live and is not dead template code.
- `docs/security/image-digest-pinning.md` documents the resolve → pin → layer-last
  procedure the release pipeline follows for reproducible / provenance-pinned rollouts.

## 4. NetworkPolicy hardening is opt-in prod-layer; umbrella owns datastore + internal-port allowlists, not a namespace default-deny

**Decision.** Every NetworkPolicy this chart ships is **default-off** and enabled
per-cluster:
- `templates/networkpolicy-vpc-internal.yaml` — vpc :9091 internal-port allowlist
  (`vpc.networkPolicy.enabled`).
- `templates/networkpolicy-authz.yaml` — OpenFGA / OPA-bundle / OPA-sidecar-egress
  allowlists (`opaSidecar.networkPolicy.enabled`).
- `templates/networkpolicy-datastore.yaml` — **per-datastore Postgres :5432 ingress
  allowlist** (`networkPolicy.datastore.enabled`, added r5b). Each backing pg pod
  (bitnami sub-chart, label `app.kubernetes.io/name=pg-<svc>`) gets an explicit
  allowlist; being selected by an ingress policy, it then implicitly **denies** all
  other ingress — closing lateral movement to DB credentials (CIS Kubernetes 5.3.2
  / OWASP A05:2021) without a namespace-wide default-deny.

**Why default-off (chart default) vs on (prod profile).** The chart *default*
(`values.yaml`) leaves all three flags off: the dev target (kind + kindnet) does
**not** enforce NetworkPolicy, so the policies are inert there, and a namespace-wide
restriction would break `:5432` / `:9091` port-forward debugging in dev. The
**production profile** (`values.prod.yaml`) runs on a NetworkPolicy-enforcing CNI and
flips **all three** on — `vpc.networkPolicy.enabled`, `opaSidecar.networkPolicy.enabled`,
**and** `networkPolicy.datastore.enabled` (the last added r9b; before it, the prod
profile enabled the first two but silently left every pg-<svc>:5432 reachable
namespace-wide — an oversight, now closed and guarded).

**Why per-datastore allowlists rather than a single namespace `default-deny-all`.**
A correct namespace default-deny requires an allow rule for **every** legitimate
path in the namespace — including cross-namespace ingress from the
`ingress-nginx` controller to api-gateway, DNS egress, and each Ory
(kratos/hydra) sub-chart's multi-component (courier/maester/migration) wiring —
whose selectors are **cluster-specific** (ingress-controller namespace/labels,
CNI). An incomplete default-deny silently blackholes the stack. The per-pod
datastore allowlist is **self-contained** (it selects only its own pg pod, so it
can never break non-datastore traffic) and delivers the same implicit-deny
guarantee for the credential-bearing DB listeners — the highest-value targets.
A full serving-pod default-deny across api-gateway / geo / minio-dev /
kratos-selfservice-ui belongs to the **cluster platform NetworkPolicy layer**
(per-cluster ingress-controller + CNI wiring), not the portable umbrella; the
third-party Ory stores (pg-kratos / pg-hydra) are likewise left to the Ory
sub-charts' own NetworkPolicy support (add umbrella entries per cluster if
preferred — the datastore list is data-driven, no template change needed).

**Guardrails.**
- `tests/helm/networkpolicy-datastore-test.sh` asserts the datastore policies are
  default-off, and — when enabled — render one Ingress-only policy per pg instance
  scoped to that pg pod on :5432 with the declared consumer selectors.
- `tests/helm/prod-profile-fail-closed-test.sh` §9 asserts the **production profile**
  actually renders the per-datastore policies (`≥6`, one per kacho-owned pg-<svc>), so
  a regression that drops `networkPolicy.datastore.enabled` from `values.prod.yaml`
  fails the build.

## 5. The chart default is the insecure DEV posture; production is an explicit opt-in profile

**Decision.** A bare `helm install kacho-umbrella ./helm/umbrella` (no `-f`) lands on
the **dev** posture: `authn.mode: dev` (anonymous → full access), Postgres
`ssl-mode: disable`, mTLS toggled for black-box REST, and the git-committed
`changeme-dev-*` / `kacho-dev-jwt-secret-2026` / `please-change-this-32-bytes-*`
placeholder credentials from `values.dev.yaml`/`values.yaml`. Hardened production is
**not** the default — it is reached only by explicitly layering
`-f values.prod.yaml` (auth `production`/`production-strict`, `ssl-mode: require`,
mTLS ON, fail-closed authz, and **zero** secret material — every credential via
`existingSecret`/`secretKeyRef`).

**Why default-dev rather than fail-closed-by-default.** This repo's primary artifact
is the **kind dev stand** (`make dev-up`) + the newman CI cluster, which must come up
reproducibly and offline with no external IdP / secret store. Making the *chart
default* fail-closed would break `make dev-up` and the CI stand (the exact flows that
consume the default), and a fail-closed default still could not run without a
provisioned IdP + secret store — infra the dev stand does not have. The deliberate
split is: **dev = convenient default (throwaway, world-readable placeholders,
never network-reachable); prod = explicit hardened profile.** The committed dev
credentials are labelled `changeme-dev-*` and carry no confidentiality (full policy:
`docs/security/dev-credentials.md`; banner atop `values.dev.yaml`).

**Residual risk (accepted).** An operator who runs a bare `helm install` against a
shared/staging cluster — forgetting `-f values.prod.yaml` — gets the insecure posture
with git-known credentials. This is mitigated by, not eliminated by, documentation:
the `values.prod.yaml` header, the `values.dev.yaml` security banner, and
`docs/security/dev-credentials.md` all state the rule loudly. Promoting a fail-closed
default is a deliberate future option (would require a dev opt-in flag + a dev
secret-gen path) — tracked as an enhancement, not a silent default flip here.

**Guardrails.**
- `tests/helm/prod-profile-fail-closed-test.sh` §7 asserts `values.prod.yaml` contains
  **no** plaintext `password:` and **no** `devSecret:` — the production profile can
  never regress to a git-committed secret; §1–5 assert the full fail-closed posture.
- The Ory `kratos-selfservice-ui` cookie/CSRF signing secret has **no committed
  default** at all — the sub-chart `fail`s render in production if it is unset, so a
  prod install can never fall back to a git-known signing key (§ values.prod.yaml).
