# tests/newman — публичный API kacho-storage, regression suite (CS-1)

**Black-box regression-инфраструктура** kacho-storage — покрытие публичных RPC
storage-домена через HTTP api-gateway (external endpoint). Структура — копия
`../kacho-compute/tests/newman/` (kacho-storage выделен из compute-Disk). Источник
истины — декларативные case-файлы `cases/*.py`; коллекции в `collections/`
**генерируются** `scripts/gen.py`.

> Приёмочный контракт — APPROVED acceptance-док
> `docs/specs/sub-phase-CS-1-storage-network-disk-acceptance.md` (эпик
> kacho-workspace#132). Каждый кейс несёт `# verifies CS1-Sx-yy`-аннотацию;
> error-тексты §0.2 assert'ятся **behaviour-level** (код И точное сообщение —
> `testing.md` «regression-lock на уровне обсёрвабла»).

## Структура

```
tests/newman/
├── README.md                — этот файл
├── cases/                   — ИСТОЧНИК ИСТИНЫ: декларативные case-наборы (Python)
│   ├── volume.py            — VolumeService CRUD + FK/peer/size-CAS negatives (S1)
│   ├── snapshot.py          — SnapshotService from-READY + CRUD (S3)
│   ├── disk-type.py         — DiskTypeService public read + admin-Internal-only (S2)
│   ├── operation.py         — OperationService.Get (OpsProxy sop-prefix)
│   ├── authz.py             — INV-10 public listauthz + object-scoped анти-BOLA (fixture-gated)
│   └── internal-volume.py   — InternalVolumeService Internal-only external-absence (S4)
├── collections/             — СГЕНЕРИРОВАННЫЕ Postman-коллекции — НЕ править руками
├── environments/
│   └── local.postman_environment.json   — local stand (port-forward api-gateway → 18080)
├── scripts/
│   ├── gen.py               — генератор коллекций из cases/* (Postman v2.1 JSON)
│   ├── validate-cases.py    — MANDATORY: уникальность case-id + CASES-INDEX coverage
│   └── run.sh               — прогон одного/всех ресурсов (newman + JSON reporter → out/)
├── docs/
│   ├── CASES-INDEX.md        — каталог кейсов + CS1-mapping + уникальные паттерны
│   └── RESULTS.md            — прогон pass/fail + integration-only caveat + known-gated
└── out/                     — newman raw output + summary.txt (gitignored)
```

## Быстрый старт

```bash
# 1. Поднять стенд с задеплоенным kacho-storage + port-forward api-gateway → :18080
cd ../../kacho-deploy && make dev-up && make reload-svc SVC=storage
# 2. Валидация + генерация коллекций из cases/*.py (обязательный workflow нового кейса)
python3 scripts/validate-cases.py     # уникальность case-id + CASES-INDEX
python3 scripts/gen.py                 # все ресурсы; или: python3 scripts/gen.py volume
# 3. Прогон
./scripts/run.sh                       # все коллекции, сводка → out/summary.txt
./scripts/run.sh --service volume      # один ресурс
```

## Принципы (из testing-product-coach)

- **Black-box**: тестируем продукт через публичный REST api-gateway (external), не код.
- **Источник истины**: acceptance-spec CS-1 + proto (`kacho-proto/.../storage/v1/`).
- **Изоляция**: каждый case в своём `runId`; suite внутри pre-allocated
  `existingProjectId` (`_suiteFolderId` из env), Org/Cloud/Project **не создаёт**;
  имена суффиксуются `{{runId}}`.
- **LRO-poll**: каждая мутация (`Volume`/`Snapshot` `Create/Update/Delete`) → `Operation`
  → poll `GET /operations/{sop-id}` (OpsProxy, retry до 8 раз через `setNextRequest`)
  до `done=true` → assert `response`/`error` (код + текст).
- **DiskType admin CRUD — sync** (осознанное исключение INV-3, admin-каталог), выставлен
  **только** на internal-mux (:9091) — на external покрываем лишь его **отсутствие** (INV-7a).

## Границы покрытия (не over-claim — см. `docs/RESULTS.md`)

- **Integration-only (НЕ black-box):** attach-CAS happy/race (CS1-S4-01..12), admin
  DiskType Create/Update/Delete happy + FK-delete-in-use (CS1-S2-02/03/05), не-READY
  Given-состояния (CS1-S4-04 attach-not-ready, CS1-S3-02 from-non-READY). Причина:
  control-plane финализирует READY мгновенно (§0.1), не-READY достижимо только DB-seed;
  attach/admin-CRUD доступны только на :9091 mTLS internal-mux + per-RPC Check. Покрыты
  integration-тестами (testcontainers, concurrent `-race`), не external newman. Здесь
  провокабельная часть — Internal-only **external-absence** (INV-7a, CS1-S2-04/S4-11).
- **Fixture-gated:** `cases/authz.py` (INV-10) требует authz-enforced стенд с alice-identity
  (см. шапку файла) — как compute `authz-deny.py` (`# requires`-профиль стенда).
