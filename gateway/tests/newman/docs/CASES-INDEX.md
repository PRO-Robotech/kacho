# kacho-api-gateway — CASES-INDEX

Каталог идентификаторов кейсов набора края (`gateway/tests/newman`). Источник истины —
модули `cases/*.py`; этот файл — производный: строки выведены загрузчиком генератора
(`scripts/gen.py`, `load_cases_module`) из `CASES` каждого модуля, по модулю — раздел.
Описание кейса живёт в шапке его модуля, здесь — только идентификатор, классы, приоритет
и заголовок кейса.

Сверщика у этого набора нет: `validate-cases.py` заведён у наборов служб, у края — нет.
Предикат полноты, которым каталог сверяется руками, — число кейсов из сводки генератора
(`python3 scripts/gen.py` → «кейсов N») равно числу строк таблиц ниже, начинающихся
идентификатором кейса.

Статуса прогона каталог не несёт: исход кейса — отчёт прогона набора, а не этот файл.


## `authn_edge.py`

| case-id | class | prio | проверка |
|---|---|---|---|
| `IBT-04-BOOTSTRAP-RS256-ACCEPTED` | AUTHN/CONF/FLOW | P0 | bootstrap RS256 Bearer is accepted by the edge on a public route → 200 |
| `IBT-10-ANONYMOUS-REJECTED` | AUTHN/NEG/CONF | P0 | no credential on an authenticated route → 401, never a page |
| `IBT-10-HS256-FORGED-REJECTED` | AUTHN/NEG/SEC/CONF | P0 | symmetrically signed (HS256) Bearer asserting a service_account → 401 |
| `IBT-10-ALG-NONE-REJECTED` | AUTHN/NEG/SEC/CONF | P0 | unsigned Bearer (alg=none) asserting a service_account → 401 |
| `IBT-10-RS256-TAMPERED-SIGNATURE-REJECTED` | AUTHN/NEG/SEC/CONF | P0 | genuine bootstrap Bearer with one signature byte flipped → 401 |
| `F1B-D-PLATFORM-ES256-ACCEPTED` | AUTHN/CONF/FLOW | P0 | Bearer minted by OUR issuer (ES256) is accepted by the edge on the strict platform lane → 200 |
| `F1B-D-PLATFORM-USER-TOKEN-ACCEPTED` | AUTHN/CONF/FLOW | P0 | Personal user token minted by OUR issuer (ES256) is accepted by the edge → 200; its principal is a HUMAN, not a service account |
| `F1B-D-PLATFORM-TAMPERED-SIGNATURE-REJECTED` | AUTHN/NEG/SEC/CONF | P0 | Bearer minted by OUR issuer with one signature byte flipped → 401 |

## `cluster_admin.py`

| case-id | class | prio | проверка |
|---|---|---|---|
| `CLUSTER-ADMIN-GET-OK` | CRUD/AUTHZ | P0 | GET /iam/v1/internal/cluster as cluster system_admin → 200, singleton id |
| `CLUSTER-ADMIN-GET-403-ORDINARY` | NEG/AUTHZ | P0 | GET /iam/v1/internal/cluster as a never-granted user → 403 PermissionDenied |
| `CLUSTER-ADMIN-LIST-OK` | CRUD/AUTHZ | P0 | ListAdmins as cluster system_admin → 200, every entry well-formed |
| `CLUSTER-ADMIN-LIST-403-ORDINARY` | NEG/AUTHZ | P0 | ListAdmins as a never-granted user → 403 PermissionDenied (roster not observable) |
| `CLUSTER-ADMIN-GRANT-403-ORDINARY` | NEG/AUTHZ/SEC | P0 | GrantAdmin as a never-granted user → 403 PermissionDenied (no self-escalation) |
| `CLUSTER-ADMIN-GRANT-400-INVALID-USER` | VAL/NEG | P0 | GrantAdmin with a well-formed but non-existent user id → 400 InvalidArgument |
| `CLUSTER-ADMIN-REVOKE-404-NOT-ADMIN` | NEG/VAL | P0 | RevokeAdmin a user who has never been an admin → 404 NotFound, exact text |
| `CLUSTER-ADMIN-GRANT-OK` | CRUD/AUTHZ | P0 | GrantAdmin → 200 Operation(iop) done=true, target appears in ListAdmins |
| `CLUSTER-ADMIN-GRANT-OK-IDEMPOTENT` | CRUD/AUTHZ | P0 | GrantAdmin twice → success, SAME grant id, exactly one roster entry |
| `CLUSTER-ADMIN-REVOKE-OK` | CRUD/AUTHZ | P0 | RevokeAdmin → 200 Operation(iop) done=true, target no longer in ListAdmins |

## `iam-account-id-at-create.py`

| case-id | class | prio | проверка |
|---|---|---|---|
| `IAM-ACC-ID-K1` | CRUD/CONF | P0 | AID-K1: POST /iam/v1/accounts with id=X by a human cloud admin through the edge → Operation with metadata.accountId = X; done gives response.id = X; GET /accounts/X → 200 |
| `IAM-ACC-ID-K1-FORM` | VAL/NEG | P0 | AID-K1 form twin: POST /iam/v1/accounts with a malformed id by the same human cloud admin → sync 400 INVALID_ARGUMENT (3), "invalid account id 'acc7M3K9Q2X5V8B4N6T1'", field id |
| `IAM-ACC-ID-K2` | NEG/AUTHZ/SEC | P0 | AID-K2: POST /iam/v1/accounts with id=X by a human without a cluster role → 403 PERMISSION_DENIED (7), "permission denied" from the service; account X does not exist |
| `IAM-ACC-ID-K2-GENERATED` | CRUD | P0 | AID-K2 field twin: POST /iam/v1/accounts without id by the same human → Operation done with response; the id is minted in the generator form |

## `iam-account-id-edge-format.py`

| case-id | class | prio | проверка |
|---|---|---|---|
| `IAM-AB-SIA-12-EDGE-LIST-BY-ACCOUNT` | NEG/VAL | P0 | IAM-AB-SIA-12: GET /accounts/not-an-id/accessBindings → sync 400 INVALID_ARGUMENT (3), "invalid resource id 'not-an-id'" from the edge; a well-formed absent account on the same route passes on to the permission check (403) |
| `IAM-ID2-NEG-FORM-EDGE-ACCOUNT-ID` | NEG/VAL | P0 | Malformed account id is refused by the EDGE before the permission model: 400 for the rights holder and for a caller without a single grant, bodies equal; a well-formed absent account passes on to the model (403) |

## `iam-int-1-s3.py`

| case-id | class | prio | проверка |
|---|---|---|---|
| `IAM-INT-1-24-STEPUP-REFUSAL-EVERY-CLASS` | AUTHN/AUTHZ/CONF | P1 | IAM-INT-1-24: человек без второго фактора тремя классами носителей на пол «2» — один вердикт 401, до владельца не доходит ни один; на пол «1» — 200 всеми тремя |
| `IAM-INT-1-25-STEPUP-PASS-RAISED-CLASSES` | AUTHN/AUTHZ/CONF | P1 | IAM-INT-1-25: тот же человек со вторым фактором — сессия и предъявитель «2» проходят пол «2»; базовое удостоверение получает тот же единый отказ, что в 24.3 |

## `iam-interactive-client.py`

| case-id | class | prio | проверка |
|---|---|---|---|
| `IAM-IC-SECRET-01-CR-ISSUES-SECRET` | CRUD/SEC | P0 | IC-SECRET-01: Create on the own landing answers CreateInteractiveClientResponse with a client_secret_basic client and a non-empty clientSecret; Get describes the same row |
| `IAM-IC-SECRET-04-CR-CONF-DUP-NAME` | CONF/NEG/SEC | P0 | IC-SECRET-04: a second Create under a taken name → sync 409 ALREADY_EXISTS without a secret; the list holds exactly one client under that name; the first secret still proves its client |
| `IAM-IC-SECRET-08-GT-NO-SECRET` | SEC/NEG | P0 | IC-SECRET-08: Get answers the client_secret_basic resource without the secret (compared by value); at the same moment the secret proves the client and a one-character-off secret does not |
| `IAM-IC-SECRET-09-LS-UP-NO-SECRET` | SEC/VAL/NEG | P0 | IC-SECRET-09: the List page carries the client in the Get projection without the secret; a description update answers without the secret and leaves it working; the mask cannot name clientSecret (unknown) nor switch tokenEndpointAuthMethod (immutable) |
| `IAM-IC-SECRET-10-DL-NO-USABLE-SECRET` | SEC/STATE/IDM | P0 | IC-SECRET-10: Delete echoes the resource without the secret; afterwards the secret gets invalid_client byte-identical to a wrong secret and to a never-created client; Get → 404; a repeated Delete has the same outcome |

## `iam-internal-only-check.py`

| case-id | class | prio | проверка |
|---|---|---|---|
| `IAM-INT-NEG-EXT-REST-ALIVE` | SEC | P0 | Control: the advertised external TLS listener serves REST (so a 404 below means 'not routed', not 'nothing there') |
| `IAM-INT-NEG-EXT-IAM-LOOKUPSUBJECT` | NEG/SEC | P0 | InternalIAMService.LookupSubject on the external TLS listener → 404 mux miss (internal-only, ban #6) |
| `IAM-INT-NEG-EXT-IAM-CHECK` | NEG/SEC | P0 | InternalIAMService.Check on the external TLS listener → 404 mux miss (internal-only, ban #6) |
| `IAM-INT-NEG-EXT-UNBOUND-NEVER-SUCCEEDS` | NEG/SEC | P0 | Unbound Internal* RPCs never succeed on the external TLS listener (fail-closed; NOT a route-isolation proof — see case notes) |
| `IAM-INT-OK-INT-IAM-LOOKUPSUBJECT` | CRUD/SEC | P1 | InternalIAMService.LookupSubject on cluster-internal listener — the seed person's externalId resolves to that person → 200 |
| `IAM-INT-OK-INT-IAM-LOOKUPSUBJECT-UNKNOWN` | NEG/SEC | P1 | InternalIAMService.LookupSubject for unknown externalId → 404 ВЛАДЕЛЬЦА (текст называет запрошенный external_id; код 5 несёт и промах mux) |
| `IAM-INT-OK-INT-IAM-CHECK` | CRUD/SEC | P1 | InternalIAMService.Check on cluster-internal listener → 200 (allowed=true or false) |
| `IAM-INT-NEG-EXT-IC-LIST` | NEG/SEC | P0 | InternalInteractiveClientService.List on the external TLS listener → 404 mux miss (internal-only, ban #6) — refused even to the principal authorised for it |
| `IAM-INT-NEG-EXT-IC-CREATE` | NEG/SEC | P0 | InternalInteractiveClientService.Create on the external TLS listener → 404 mux miss (internal-only, ban #6) — no client is registered at the provider from the outside |
| `IAM-INT-OK-INT-IC-LIST` | CRUD/SEC | P0 | InternalInteractiveClientService.List on the cluster-internal listener → 200 with a page (positive control: the two 404s above mean 'not routed here', not 'nowhere at all') |

## `iam-project-edge-format.py`

| case-id | class | prio | проверка |
|---|---|---|---|
| `IAM-PRJ-DL-NEG-MALFORMED-PREFIX` | NEG/VAL | P1 | Delete /projects/{string whose prefix is unknown to the platform} → sync 400 INVALID_ARGUMENT (3) from the edge; a known prefix of the wrong length passes on to the permission check (403) |

## `iam-step-up-access-key-begin.py`

| case-id | class | prio | проверка |
|---|---|---|---|
| `F8-S7-80-IAM-STEPUP-AKBEGIN-NEG-NO-KEY` | NEG/CONF/STATE | P0 | F8-S7-80: verified human without an access key — POST /iam/v1/auth/step-up/access-key/begin through the edge → the service refusal relayed unchanged: 400 FAILED_PRECONDITION (9), ACCESS_KEY_NOT_REGISTERED |
| `F8-S7-80-IAM-STEPUP-AKBEGIN-AUTHZ-UNVERIFIED` | AUTHZ/NEG/CONF | P0 | F8-S7-80 twin: human with an unverified address — the same request → 403 PERMISSION_DENIED (7), EMAIL_NOT_VERIFIED; body byte-equal to the edge answer on POST /iam/v1/auth/step-up |

## `system_class_anonymous_edge.py`

| case-id | class | prio | проверка |
|---|---|---|---|
| `SC36-ANON-REFUSED-MACHINE-TYPES` | AUTHN/AUTHZ/NEG | P0 | SC-36: /compute/v1/machineTypes — без Authorization 401/UNAUTHENTICATED, вошедший без выдач 200 |
| `SC36-ANON-REFUSED-REGIONS` | AUTHN/AUTHZ/NEG | P0 | SC-36: /geo/v1/regions — без Authorization 401/UNAUTHENTICATED, вошедший без выдач 200 |
| `SC36-ANON-REFUSED-ZONES` | AUTHN/AUTHZ/NEG | P0 | SC-36: /geo/v1/zones — без Authorization 401/UNAUTHENTICATED, вошедший без выдач 200 |
| `SC36-ANON-REFUSED-DISK-TYPES` | AUTHN/AUTHZ/NEG | P0 | SC-36: /storage/v1/diskTypes — без Authorization 401/UNAUTHENTICATED, вошедший без выдач 200 |
| `SC36-ANON-REFUSED-PERMISSION-CATALOG` | AUTHN/AUTHZ/NEG | P0 | SC-36: /iam/v1/permissionCatalog — без Authorization 401/UNAUTHENTICATED, вошедший без выдач 200 |
| `SC36-ANON-REFUSAL-HIDES-EXISTENCE` | AUTHN/SEC/NEG | P0 | SC-36/Р7: GET региона, который есть, и правильной формы, которого нет, без Authorization — одно тело 401 побайтово |
| `SC41-SIGNED-IN-RIGHTS-UNCHANGED-AFTER-MOVE` | AUTHZ/CONF/STATE | P0 | SC-41: системная выдача переехала на system_class/authenticated (Дано SC-40); вошедший без выдач читает /geo/v1/regions 200, без Authorization — 401 |
| `SC18-EDGE-OTHER-TENANT-READS-CLASS-GRANT` | AUTHZ/CRUD/STATE | P0 | SC-18 край: выдача authenticated на сеть acc-A → принципал вне acc-A читает её 200; до выдачи и после отзыва — отказ формой каталога (404/NOT_FOUND), без данных объекта |

## `system_class_subject_check.py`

| case-id | class | prio | проверка |
|---|---|---|---|
| `SC10-ANONYMOUS-DOES-NOT-INHERIT-AUTHENTICATED` | AUTHZ/NEG/CONF | P0 | SC-10: выдача authenticated на сеть платформы → Check анонима allowed=false, denyReasons пуст; близнецы человек и учётка allowed=true |
| `SC11-SIGNED-IN-INHERITS-ANONYMOUS` | AUTHZ/NEG/CONF | P0 | SC-11: выдача anonymous на сеть платформы → человек и аноним allowed=true; группа без выдач allowed=false |
| `SC16-EXPAND-ACCESS-DOES-NOT-LIST-CLASSES` | AUTHZ/CONF/STATE | P0 | SC-16: expandAccess на сеть платформы — человек прямой выдачи есть, элемента класса нет, truncated=false; перечень до и после выдачи authenticated побайтово один |
| `SC18-OTHER-TENANT-SIGNED-IN-READS-NAMED-OBJECT` | AUTHZ/NEG/STATE | P0 | SC-18: выдача authenticated на сеть арендатора A → человек и учётка арендатора B allowed=true; соседняя сеть закрыта; после отзыва опрос доходит до allowed=false у обоих |
