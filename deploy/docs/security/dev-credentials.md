# Dev/CI credentials policy (INFRA sec-hardening)

`helm/umbrella/values.dev.yaml` powers the throwaway kind stand (`make dev-up`)
and the newman CI cluster. Every credential in it is a **non-secret placeholder**
committed to git on purpose so the local stand is reproducible.

## What changed

The former well-known literals were replaced with unmistakable placeholders:

| Old (well-known) | Now (placeholder) |
|------------------|-------------------|
| `minioadmin` (root user / access key) | `changeme-dev-minio-access` |
| `minioadmin` (root password / secret key) | `changeme-dev-minio-secret` |
| `dev-vpc-password`, `dev-compute-password`, `dev-iam-password`, `dev-geo-password`, `dev-openfga-password`, `dev-nlb-password`, `dev-kratos-password`, `dev-hydra-password` | `changeme-dev-<service>` |

The JWT signing secret `authn.devSecret: kacho-dev-jwt-secret-2026` is likewise a
committed **dev-only placeholder**. It is deliberately shared with the
authz-fixtures JWT minter (`tests/authz-fixtures/setup.sh` `DEV_SECRET` default) so
the ephemeral stand can mint valid dev tokens without an external IdP. It carries
**no confidentiality** and falls under the same rules below — anyone with the file
can forge dev tokens, so a `values.dev.yaml` cluster must never be network-reachable.
Production runs `authn.mode` against a real IdP / validated JWKS and never uses this
value.

Each placeholder is replaced consistently across every occurrence (the Postgres
`auth.password`, the service DSN/URI, and the MinIO client keys) so the
ephemeral stand still comes up — the values are functional but carry **no
confidentiality**.

## Rules

- **Never** reuse these values on a shared, staging or production cluster. They
  are guessable by design and world-readable in git.
- Production sources every credential from **external-secrets / SealedSecret**:
  `values.prod.yaml` sets `minioDev.enabled=false`, points services at an
  external S3/MinIO, supplies DB passwords via `existingSecret`, and sets
  `ssl-mode=require`. It is fail-closed and guarded by
  `tests/helm/prod-profile-fail-closed-test.sh`.
- When bringing up any non-ephemeral cluster, override every `changeme-dev-*`
  value (and disable `minioDev`) before install.

## Why not generate them at install time?

Random per-install secrets would break the dev stand's cross-component matching
(the Postgres `auth.password` and the DSN embedding it must agree, and MinIO's
root credentials must equal the client access/secret keys). For the **ephemeral,
single-tenant kind stand** a committed, clearly-labelled placeholder is the
pragmatic choice; real secret material belongs to the prod external-secrets path
above. The banner at the top of `values.dev.yaml` states this in-line.
