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

# 3. Run newman per service (пути — от корня ЭТОГО репозитория)
(cd services/iam/tests/newman     && ./scripts/run.sh --service authz-deny)
(cd services/vpc/tests/newman     && ./scripts/run.sh --service authz-deny)
(cd services/compute/tests/newman && ./scripts/run.sh --service authz-deny)
```

Прежняя редакция звала эти три команды в отдельные репозитории сервисов —
разработка ведётся в одном репозитории с 2026-08, и цепочка `cd` без подоболочки
уводила из первого же каталога, поэтому вторая и третья строки не выполнились бы
даже при живых путях.

## Две посадки — один вход (`setup.sh`)

`setup.sh` СНАЧАЛА определяет posture стенда и уже потом решает, откуда брать токены.
Определение читает единственную строку, которую процесс сам пишет после boot-guard'ов
(`msg="boot security posture"`, `pkg/observability/bootposture.go`) — тот же наблюдаемый
факт, по которому судит `deploy/scripts/assert-production-posture.sh`. **Не** ConfigMap:
security-ручки приезжают через `envFrom` и читаются один раз на старте, поэтому хранимое
значение может говорить `production`, пока живой процесс всё ещё `dev`.

| posture | как харнесс получает токены |
|---|---|
| `production` / `production-strict` | делегирует `prodseed_all.py` — **ни один токен не чеканится харнессом** |
| `dev` (`authn.mode=dev`) | **отказ с названной причиной** — такой посадки не производит ни один профиль развёртывания |
| что-либо ещё (опечатка, неизвестное имя) | **отказ с названной причиной** — корзины «всё остальное» у гейта нет |

Строка `dev` в таблице оставлена намеренно: это не «поддерживаемый вариант», а
объявление того, что произойдёт. Прежде она означала «чеканим симметрично своим
ключом»; теперь такого стенда не существует — у чарта нет ручки для общего ключа,
а процесс отказывается стартовать, если ключ доедет до него иначе. Стенд, который
всё же о ней сообщает, отстал: его накатывают заново, а не обходят посевом.

Production-путь (`prodseed_all.py` → `prodseed_matrix.py` → `mint_rs256.py`):

1. **admin** — iam `InternalBootstrapTokenService.MintBootstrapToken`, прямой mTLS-gRPC
   на `kaname :9091` с **bootstrap-operator** client-cert (у mint нет REST-маршрута;
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
`kaname_principal_type == "service_account"`. User с `acr` требует интерактивного
Kratos→Hydra логина, который машинный харнесс не проводит.

**Намеренно НЕ чеканится в production**: `jwtAccountAdminAStepUp` и статические
`apiToken*` — им нужен настоящий step-up/интерактивный credential. Ключи остаются как
есть, чтобы их кейсы падали громко, а не были подделаны.

Клиентские сертификаты для внутреннего порта **добываются сами** из секретов кластера
(`kacho-bootstrap-operator-client-tls` — для mint; `api-gateway-client-tls` — для обычных
internal-RPC) в файлы `0600` под `/tmp`. В репозиторий ключи не попадают; извлекаются
заново каждый прогон (cert-manager перевыпускает internal-CA на свежем `dev-up`, и
оставшийся с прошлого стенда leaf подписан СТАРЫМ CA — постоянный отказ хендшейка).

### Как опознаётся посадка (и почему её нельзя назначить)

Посев спрашивает **сам край** по `BASE_URL`, а не читает чью-либо запись о нём:

1. **контроль** — запрос по `POSTURE_PROBE_PATH` с заведомо негодным удостоверением;
   обязан вернуть `401`. Он доказывает, что адрес пробы доходит до гейта аутентификации,
   и про посадку не говорит ничего (негодное удостоверение отвергается в любой посадке);
2. **решающая проба** — тот же запрос **без** заголовка `Authorization`. `401` ⇒ край
   требует удостоверение ⇒ посев идёт дальше. Любой другой ответ ⇒ край обслужил запрос
   без удостоверения ⇒ отказ.

Три исхода — три кода возврата: `0` посев отдан выдающему · `1` посадка доказана и она
не та (либо вызов отвергнут) · `3` **посадку установить нечем** — это не вердикт о
стенде, а отсутствие свидетельства, и оно не приравнивается ни к чему.

Прежде посадка читалась из строки запуска в **журнале контейнера** края. Журнал
ротируется, поэтому на стенде старше одного оборота свидетельства уже нет — и
классификатор докладывал «посадка не опознана», приписывал это отсутствию доступа к
кластеру и предлагал продавить классификацию переменной. `SEED_POSTURE` **удалена**: её
единственным обоснованием был «CI без доступа к кластеру», а нынешняя проба доступа к
кластеру не требует вовсе. Переменная не игнорируется, а **отвергается** — тихо
проигнорированная, она читалась бы как «я продавил».

Посадка, в которой посев отработал, пишется в `out/seed-posture` как **провенанс** для
того, кто позже читает этот каталог, — не как переключатель: производимое значение одно
(`production`), поэтому ветвление на нём было бы ветвью, которая не может быть ложной.
Гейт `deploy/scripts/assert-posture-branches-can-be-taken.py` держит это свойство.

### Что в production-посадке НЕ проверяется и почему (не чинится харнессом)

Замерено на живом стенде 2026-07-26. Это ограничения ПРОДУКТА, а не посева — латать их
подделкой токенов нельзя, иначе набор снова начнёт доказывать не то.

1. ~~**RPC, гейтящиеся на «вызывающий — это владелец-ЧЕЛОВЕК аккаунта»**~~ — **СНЯТО
   2026-07-27.** `authzguard.RequireOwnerMatchesPrincipal` удалена из всех 12 методов
   (Account/Project/User/Group/Role/ServiceAccount delete-update-addMember…): решение о
   доступе к ним принимает МОДЕЛЬ (каталог + per-object Check `v_delete`/`v_update` на
   самом объекте), и она принимает машинного принципала как первоклассный субъект.
   Проверка рядом с моделью была УЖЕ, коарснее (ключевалась на `accounts.owner_user_id`),
   не выдавалась/не отзывалась/не попадала в аудит и делала все 12 недостижимыми для
   любого машинного клиента by construction. Теперь SA-принципал с нужным грантом их
   проходит; без гранта — по-прежнему `403` от gateway. Регрессия зафиксирована в
   `gateway/internal/middleware/authz_iam_owner_guard_model_gate_test.go` (модель гейтит
   все 12) + `…/api/*/machine_principal_reaches_usecase_test.go` (use-case не пере-решает).
2. **`AccountService.Create`** выводит `owner_user_id` из вызывающего, поэтому SA-caller
   роняет его на FK: асинхронный `9 FAILED_PRECONDITION "referenced resource not found
   or still in use"`. Создать аккаунт машинным принципалом нельзя.
3. **`jwtAccountAdminAStepUp` / `apiToken*`** — требуют интерактивного step-up (Kratos→
   Hydra). Не чеканятся; их кейсы падают честно.

## Environment knobs

| Env-var | Default | Назначение |
|---|---|---|
| `BASE_URL` | `http://localhost:18080` | api-gateway dev-listener; **этот же адрес опознаёт посадку**, и `setup.sh` его экспортирует, чтобы гейт и посев не разъехались по двум литералам |
| `POSTURE_PROBE_PATH` | `/iam/v1/projects` | маршрут пробы посадки; обязан требовать аутентификации (никогда не pre-auth allowlist) |
| `POSTURE_PROBE_RETRIES` | `5` | повторы **только** на отсутствие ответа транспорта; решённый статус не повторяется |
| ~~`SEED_POSTURE`~~ | — | **удалена.** Посадку нельзя назначить снаружи; заданная переменная отвергает вызов (см. выше) |
| `HYDRA_PUBLIC_PORT` | `14444` | порт-форвард Hydra public (OAuth2 обмен, только production) |
| `OUT_DIR` | `tests/authz-fixtures/out` | куда посев пишет свои артефакты. Каталог целиком под `.gitignore` этого каталога, поэтому в дереве его файлов нет by construction. Что туда кладут: реестр фикстур (пишет `prodseed_all.py`) и провенанс посадки (`out/seed-posture`, см. выше). Прежняя редакция называла здесь ещё один файл с токенами — его **не пишет ни одна строка дерева**: единственное упоминание того имени было в этой же таблице |
| `PATCH_ENV` | `true` | патчить ли окружение newman-суит. Отслеживается git **шаблон** — `environments/local.postman_environment.template.json` каждой суиты; рабочий файл прогонщик делает из него копией и патчит уже копию. Копия намеренно не отслеживается (корневой `.gitignore`), потому что несёт значения конкретного стенда |
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
  `client_secret` возвращается ОДИН раз и **не персистится** в реестр фикстур
  (`OUT_DIR`, см. таблицу выше — каталог целиком под `.gitignore`)
- токены (HS256 dev-equivalents Hydra-issued JWT — api-gateway dev-mode authn):
  - `jwtSAA` — SA-A токен (`kaname_principal_type=service_account`, `sub=<svaAId>`)
  - `jwtSANoGrant` — SA без grant'ов
  - `apiTokenValid` — статический API-token, scope `vpc.* project:<A1>`
  - `apiTokenRevoked` — валиден по подписи, но SA-key отозван `SAKeyService.Revoke` → 401
  - `apiTokenExpired` — `exp` в прошлом → 401
  - `apiTokenMalformed` — синтаксически битый JWS (2 сегмента) → 401

> Абзац выше описывает РОЛИ, а не способ их подписи. Способ теперь один: те же роли
> получают токены через iam (`prodseed_matrix.py` → `SAKeyService.Issue` → OAuth2
> `client_credentials`). Ветка, чеканившая их симметрично общим ключом, и сам ключ
> **удалены** — это и есть тот «отдельный шаг вместе с текстом», который здесь
> объявлялся. `out/` под gitignore — реальные значения в репо не попадают.
>
> Возврат ключа в дерево ловит `internal/repohygiene/sharedsigningliteral_test.go`;
> отсутствие корзины «всё остальное» у посадочного гейта —
> `internal/repohygiene/seedposturegate_test.go`. Сам **выданный токен**, попавший
> в индекс, — `internal/repohygiene/committedcredential_test.go`: он судит по
> предъявимости (подпись той длины, что производит объявленный алгоритм) и не
> читает имени ключа, поэтому ловит и слот шаблона, и токен в отчёте прогона,
> и приватный ключ в профиле развёртывания.

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

Проектный документ этой матрицы лежал в каталоге сторонних артефактов под `docs/`.
Каталог удалён целиком решением владельца 2026-06-11 (коммит `28778ef4`), поэтому
здесь стояла ссылка в никуда; адрес не воспроизводится — процитированный, он
читается как живой. Текст восстанавливается из истории по этому коммиту.
