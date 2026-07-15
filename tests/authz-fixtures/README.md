# authz-deny suite — shared fixtures (KAC-122 / KAC-127)

Idempotent bootstrap для authz default-deny newman suite. Pre-creates
Account / Project / User / AccessBinding и активирует invitee через KAC-125
invite-flow.

**KAC-127** расширяет bootstrap двумя non-human моделями субъектов (5-6):
ServiceAccount (Hydra `client_credentials`, Class A workload identity) +
статический API-token (issue / revoke / expire через `SAKeyService`).

## Quick start

```bash
# 1. Port-forward api-gateway → localhost:18080
kubectl port-forward -n kacho svc/api-gateway 18080:8080 &

# 2. Run setup (idempotent; re-run safe)
bash tests/authz-fixtures/setup.sh

# 3. Run newman per service
cd project/kacho-iam/tests/newman    && ./scripts/run.sh --service authz-deny
cd project/kacho-vpc/tests/newman    && ./scripts/run.sh --service authz-deny
cd project/kacho-compute/tests/newman && ./scripts/run.sh --service authz-deny
```

## Environment knobs

| Env-var | Default | Назначение |
|---|---|---|
| `BASE_URL` | `http://localhost:18080` | api-gateway dev-listener |
| `DEV_SECRET` | `kacho-dev-jwt-secret-2026` | HMAC-secret HS256 (`KACHO_API_GATEWAY_AUTHN_DEV_SECRET` на стенде) |
| `EXP_HOURS` | `24` | exp claim в JWT |
| `OUT_DIR` | `tests/authz-fixtures/out` | куда писать `authz-fixtures.json` + `jwts.json` |
| `PATCH_ENV` | `true` | патчить ли `environments/local.postman_environment.json` всех 3 newman-suite'ов |
| `VERBOSE` | `false` | echo каждый curl |

## Что создаётся (минимум)

- 6 users (bootstrap-admin + 5 test users + invitee) через `InternalUserService.UpsertFromIdentity`
- 2 accounts (`authz-test-A`, `authz-test-B`)
- 3 projects (`authz-test-A1`, `authz-test-A2` в account-A; `authz-test-B1` в account-B)
- 4 access bindings:
  - editor on project-A1 → user-PA1
  - admin on account-A → user-AAA
  - admin on account-B → user-AAB
  - owner on account-B → user-INV (его home)
- INV invite в account-A как editor on project-A1 (через `UserService.Invite`, потом активация через повторный UpsertFromIdentity)
- 2 seed networks (для GET-проб): `authz-seed-net-a1` в project-A1, `authz-seed-net-b1` в project-B1

### KAC-127 — модели 5-6 (ServiceAccount + API token)

- 2 service accounts в account-A: `authz-sa-a` (granted) + `authz-sa-nogrant`
- 1 access binding: `vpc-editor on project-A1` → SA-A (`subjectType=service_account`)
- 1 SA-key (Hydra OAuth client) для SA-A через `SAKeyService.Issue`;
  `client_secret` возвращается ОДИН раз, **не персистится** в `authz-fixtures.json`
- токены (HS256 dev-equivalents Hydra-issued JWT — api-gateway dev-mode authn):
  - `jwtSAA` — SA-A токен (`kacho_principal_type=service_account`, `sub=<svaAId>`)
  - `jwtSANoGrant` — SA без grant'ов
  - `apiTokenValid` — статический API-token, scope `vpc.* project:<A1>`
  - `apiTokenRevoked` — валиден по подписи, но SA-key отозван `SAKeyService.Revoke` → 401
  - `apiTokenExpired` — `exp` в прошлом → 401
  - `apiTokenMalformed` — синтаксически битый JWS (2 сегмента) → 401

> Все токены — placeholder dev-credentials, генерируются `setup-jwt.py` от
> `DEV_SECRET`. `out/` под gitignore — реальные значения в репо не попадают.

## Идемпотентность

- `UpsertFromIdentity` — by design KAC-125 (ON CONFLICT external_id+account)
- `AccessBinding.Create` — 5-tuple dedup (KAC-112 §13.4)
- `Account/Project.Create` — find-by-name + skip если уже есть
- `ServiceAccount.Create` — find-by-name + skip если уже есть (KAC-127)
- `UserService.Invite` — KAC-125 ON CONFLICT (PENDING-row reuse по email)

Re-run setup безопасен; ID'ы стабильны между запусками.

## Что НЕ делает

- НЕ удаляет фикстуры после прогона newman (для скорости re-runs)
- НЕ trash'ит существующие данные другого назначения

См. design: [`docs/superpowers/specs/2026-05-19-authz-default-deny-matrix-newman-design.md`](../../docs/superpowers/specs/2026-05-19-authz-default-deny-matrix-newman-design.md).
