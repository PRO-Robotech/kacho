# tests/newman — публичный API kacho-compute, regression suite

**Главная regression-инфраструктура** kacho-compute (`tests/newman/`; рядом `tests/k6/` —
нагрузочные сценарии). Black-box покрытие всех публичных RPC compute-домена через HTTP
api-gateway. Спроектирована по `testing-product-coach` (формальные техники test design) с
naming/structure по `testing-code-coach`. Структура — копия `../kacho-vpc/tests/newman/`.
Источник истины — декларативные case-файлы `cases/*.py`; коллекции в `collections/`
**генерируются** скриптом `scripts/gen.py`.

> Критерий приёмки: **любой compute-кейс зеленеет на собственных чёрных ящиках, и ни одно
> изменение не ломает объявленный контракт Kachō**. Кейсы с `# probe-needed:` фиксируют
> текущее поведение там, где точная формулировка контракта ещё не закреплена — список
> в `docs/REQUIREMENTS.md` §A.

## Структура

```
tests/newman/
├── README.md                — этот файл
├── cases/                   — ИСТОЧНИК ИСТИНЫ: декларативные case-наборы (Python), по ресурсу
│   ├── disk.py / image.py / snapshot.py / instance.py   — мутируемые ресурсы (Disk/Image/Snapshot/Instance)
│   ├── disk-type.py / zone.py                            — read-only справочники
│   ├── region-zone.py                                   — geography: Region (новый ресурс) + Zone admin-CRUD (InternalRegion/ZoneService на internal mux; эпик KAC-15)
│   └── operation.py                                     — OperationService (Get/Cancel через api-gateway OpsProxy)
├── collections/             — СГЕНЕРИРОВАННЫЕ Postman-коллекции (по ресурсу) — НЕ править руками
│   └── {…}.postman_collection.json
├── environments/
│   └── local.postman_environment.json   — local stand (port-forward api-gateway → 18080)
├── scripts/
│   ├── gen.py                — генератор коллекций из cases/* (Postman v2.1 JSON)
│   ├── run.sh                — прогон одного/всех ресурсов целиком (newman + JSON reporter → out/)
│   ├── run-incremental.sh    — прогон ПО ОДНОМУ кейсу за раз + зачистка ресурсов после каждого (quota-safe); --resume / --failed / --cases / --cleanup-only
│   └── run-incremental.js    — драйвер (newman library API — без per-case process startup; env SERVICES=... / CASES=... ограничивает список)
├── docs/
│   ├── TAXONOMY.md            — классы кейсов и naming convention
│   ├── TEST-PLAN.md           — карта покрытия (RPC × класс)
│   ├── CASES-INDEX.md         — каталог кейсов + уникальные паттерны
│   ├── PRODUCT-REQUIREMENTS.md — НОРМАТИВНЫЙ регламент REQ-* (от QA; сверяется при ревью изменений)
│   ├── REQUIREMENTS.md        — бэклог *улучшений* (testability / probe-needed asks — не нормативный)
│   └── RESULTS.md             — последний прогон pass/fail + история версий + skill-mapping
└── out/                     — newman raw output + summary.txt (gitignored snap-логи)
```
(Найденные дефекты/наблюдения — в GitHub Issues `PRO-Robotech/kacho`, по правилу документооборота воркспейса;
by-design отступления от конвенций Kachō — `docs/engineering/architecture/07-known-divergences.md`. Отдельного bug-map нет.)

## Быстрый старт

```bash
# Команды — ОТ КОРНЯ РЕПОЗИТОРИЯ. Скрипты набора находят свой корень сами
# (`Path(__file__).parents[1]` / `cd "$(dirname "$0")/.."`), поэтому звать их
# можно откуда угодно — а смешивать в одном блоке два разных «где я» нельзя.
# 1. Поднять стенд с задеплоенным compute + port-forward api-gateway → localhost:18080
make -C deploy dev-up && make -C deploy reload-svc SVC=compute
# 2. Перегенерить коллекции из cases/*.py (если меняли cases или код)
python3 services/compute/tests/newman/scripts/gen.py        # все ресурсы; или: … gen.py instance-redesign
# 3a. Прогнать всё одним махом (быстро; во время прогона создаётся много ресурсов разом)
./services/compute/tests/newman/scripts/run.sh                  # сводка → out/summary.txt
./services/compute/tests/newman/scripts/run.sh --service instance-redesign   # один ресурс
# 3b. Прогнать ПО ОДНОМУ кейсу за раз с зачисткой ресурсов после каждого
#     (низкий resource-footprint в любой момент → безопасно при quota-guard)
./services/compute/tests/newman/scripts/run-incremental.sh                        # все ~296 кейсов; сводка → out/incremental/summary.txt
./services/compute/tests/newman/scripts/run-incremental.sh --resume               # продолжить прерванный прогон
./services/compute/tests/newman/scripts/run-incremental.sh --service machine-type     # один ресурс
./services/compute/tests/newman/scripts/run-incremental.sh --failed               # только упавшие из прошлого прогона (после фикса)
./services/compute/tests/newman/scripts/run-incremental.sh --cases INST-RD-CR-CRUD-VM-OK,INST-RD-CR-NEG-DUP-NAME   # явный список кейсов
./services/compute/tests/newman/scripts/run-incremental.sh --cleanup-only         # просто стереть throwaway-ресурсы в тест-папках
#     тюнинг через env: CLEANUP_EVERY (как часто periodic-cleanup, default 25), DELAY_REQUEST (ms, default 30), SERVICES='r1 r2 ...'
```

## Принципы (из testing-product-coach)

- **Black-box**: тестируем продукт через публичный gRPC/REST api-gateway, не код. Тест не знает о
  SQLSTATE, имени constraint'а, конкретной БД.
- **Источник истины**: acceptance-spec + proto-определения (`proto/kacho/cloud/compute/v1/`).
- **Изоляция**: каждый case-сценарий внутри своего `runId`; suite внутри pre-allocated
  `existingProjectId`/`existingProjectCrossId` (env), проектов **не создаёт**; имена суффиксуются `{{runId}}`.
- **Актор — ПРОЕКТНЫЙ, отступления объявляет шаг.** Дефолтный Bearer всех шести коллекций —
  `jwtProjectAdminA1` (editor на `project-A1` и `project-A2`, служебная учётка посева
  `tests/authz-fixtures/`). Бутстрап-админ дефолтом быть не может: он держит права на всё,
  поэтому под ним шаг проходит независимо от того, работает ли project-scope авторизация, —
  и суита перестаёт отличать исправное от сломанного (этот класс уже ловился в дереве:
  карта прав сервиса разошлась с каталогом края по паре scope+relation, проектный принципал
  получал отказ на своих ресурсах, бутстрап этого не видел). Cluster-admin остаётся ровно
  там, где проектный актор недоступен by construction, и каждое такое место объявлено
  шагом: `auth=ADMIN_AUTH` — админ-CRUD каталога размерностей (`InternalMachineTypeService`,
  `system_admin` на cluster-singleton) и кейс `OP-GET-CRUD-FAILED-OP`, чей спусковой крючок
  — создание в чужом проекте — проектному актору недоступен. **Читатель Operation несёт
  того же актора, что её создатель**: `OperationService.Get/Cancel` энфорсит владение и
  отвечает чужому `NotFound`. Все три свойства держит проверка 4 в `scripts/validate-cases.py`
  (её доказательство инъекцией исполняемо: `validate-cases.py --self-test`), а не соглашение.
- **LRO-poll**: каждая мутация (`Create/Update/Delete/Start/Stop/Restart/Attach/Detach`)
  → `Operation` → poll `GET /operations/{id}` (retry до 8 раз через `setNextRequest`) до `done=true` → assert `response`/`error`.
- **Формальные техники**: ECP, BVA, decision tables, state transition, error guessing, security — все классы кейсов выводятся системно.
- **Conformance**: кейс проверяет контракт Kachō — форму ресурса, коды и тон ошибок,
  sync-vs-async — против proto и acceptance-дока, а не против чужого API.
- **Risk-prioritization**: high-risk зоны (security, data-integrity FK, Instance state-машина, Disk-delete-while-attached) — P0, больше кейсов.

См. подробности в `docs/TAXONOMY.md`. Cross-service зависимости (Instance NIC → kacho-vpc subnet/SG;
`project_id` → kaname) и флаг `KACHO_COMPUTE_SKIP_PEER_VALIDATION` — см. там же и в `docs/RESULTS.md` §«Деплоймент-замечания».
