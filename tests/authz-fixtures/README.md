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

## Две посадки — один вход (`setup.sh`)

`setup.sh` СНАЧАЛА определяет posture стенда и уже потом решает, откуда брать токены.
Определение читает единственную строку, которую процесс сам пишет после boot-guard'ов
(`msg="boot security posture"`, `pkg/observability/bootposture.go`) — тот же наблюдаемый
факт, по которому судит `deploy/scripts/assert-production-posture.sh`. **Не** ConfigMap:
security-ручки приезжают через `envFrom` и читаются один раз на старте, поэтому хранимое
значение может говорить `production`, пока живой процесс всё ещё `dev`.

| posture | как харнесс получает токены |
|---|---|
| `dev` (`authn.mode=dev`) | как раньше: `setup-jwt.py` чеканит HS256 от `DEV_SECRET` |
| `production` / `production-strict` | делегирует `prodseed_all.py` — **ни один токен не чеканится харнессом** |

Production-путь (`prodseed_all.py` → `prodseed_matrix.py` → `mint_rs256.py`):

1. **admin** — iam `InternalBootstrapTokenService.MintBootstrapToken`, прямой mTLS-gRPC
   на `kacho-iam :9091` с **bootstrap-operator** client-cert (у mint нет REST-маршрута;
   credential — SPIFFE SAN сертификата вызывающего);
2. **каждый субъект** — iam `SAKeyService.Issue` (iam заводит Hydra OAuth-клиента и
   ОДИН раз отдаёт ES256-ключ) → подписываем `private_key_jwt` client_assertion →
   стандартный OAuth2 `client_credentials` обмен. Это единственный санкционированный
   прямой поход в Hydra (RFC 7521/7523 client-flow); выдача, lifecycle и JWKS остаются
   за фасадом iam.

**Почему все субъекты — ServiceAccount, а не User** (проверено по коду, не предположено):
`IssueUserTokenUseCase.resolveAudience` вообще не принимает audience вызывающего и всегда
отдаёт `<prefix>/user/<id>`, что не может совпасть с `ExpectedAudience` шлюза; а токен
`client_credentials` не несёт `acr`, тогда как 292 из 357 RPC каталога требуют
`required_acr_min ≥ 1`, и `StepUpGate.Check` освобождает от acr **только**
`kacho_principal_type == "service_account"`. User с `acr` требует интерактивного
Kratos→Hydra логина, который машинный харнесс не проводит.

**Намеренно НЕ чеканится в production**: `jwtAccountAdminAStepUp` и статические
`apiToken*` — им нужен настоящий step-up/интерактивный credential. Ключи остаются как
есть, чтобы их кейсы падали громко, а не были подделаны.

Клиентские сертификаты для внутреннего порта **добываются сами** из секретов кластера
(`kacho-bootstrap-operator-client-tls` — для mint; `api-gateway-client-tls` — для обычных
internal-RPC) в файлы `0600` под `/tmp`. В репозиторий ключи не попадают; извлекаются
заново каждый прогон (cert-manager перевыпускает internal-CA на свежем `dev-up`, и
оставшийся с прошлого стенда leaf подписан СТАРЫМ CA — постоянный отказ хендшейка).

```bash
# посадку можно форсировать (CI без доступа к кластеру / репетиция)
SEED_POSTURE=production bash tests/authz-fixtures/setup.sh
```

Посадка, в которой посев реально отработал, пишется в `out/seed-posture` — драйверы
(`deploy/scripts/newman-{e2e,parallel}.sh`) ветвятся по ней, а не выводят её заново.

### Что в production-посадке НЕ проверяется и почему (не чинится харнессом)

Замерено на живом стенде 2026-07-26. Это ограничения ПРОДУКТА, а не посева — латать их
подделкой токенов нельзя, иначе набор снова начнёт доказывать не то.

1. **RPC, гейтящиеся на «вызывающий — это владелец-ЧЕЛОВЕК аккаунта»**
   (`authzguard.RequireOwnerMatchesPrincipal`, 12 методов: Account/Project/User/Group/
   Role/ServiceAccount delete-update-addMember…). Принципал-ServiceAccount не может их
   удовлетворить НИКОГДА, а принципал-User не может получить неинтерактивный токен →
   в production они недостижимы ни для одного машинного клиента. Ответ —
   `400 INVALID_ARGUMENT "owner_user_id must match the authenticated principal"`, хотя
   вызывающий это поле вообще не посылал (оно выводится из caller'а).
2. **`AccountService.Create`** выводит `owner_user_id` из вызывающего, поэтому SA-caller
   роняет его на FK: асинхронный `9 FAILED_PRECONDITION "referenced resource not found
   or still in use"`. Создать аккаунт машинным принципалом нельзя.
3. **`jwtAccountAdminAStepUp` / `apiToken*`** — требуют интерактивного step-up (Kratos→
   Hydra). Не чеканятся; их кейсы падают честно.

## Environment knobs

| Env-var | Default | Назначение |
|---|---|---|
| `BASE_URL` | `http://localhost:18080` | api-gateway dev-listener |
| `SEED_POSTURE` | `auto` | `auto`\|`dev`\|`production` — см. выше |
| `HYDRA_PUBLIC_PORT` | `14444` | порт-форвард Hydra public (OAuth2 обмен, только production) |
| `DEV_SECRET` | `kacho-dev-jwt-secret-2026` | HMAC-secret HS256 (`KACHO_API_GATEWAY_AUTHN_DEV_SECRET` на стенде) — **только dev-посадка** |
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
