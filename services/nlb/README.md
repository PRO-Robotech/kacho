# kacho-nlb

L4 Network Load Balancer control-plane сервис Kachō (sub-phase 4.0).

**Статус**: реализован. Замер на 2026-08-06: под `internal/` — 352 файла Go,
29 применённых миграций, 17 документов `docs/architecture/`, свои наборы newman и
k6. Прежняя редакция объявляла сервис «scaffold, пустые stubs, реализация
впереди» — утверждение пережило свой предмет на несколько месяцев и устарело в
сторону «проще, чем есть», а такая ложь опаснее обратной: она отговаривает
читать код.

**Acceptance**: `docs/specs/sub-phase-4.0-nlb-acceptance.md` — в репозитории
воркспейса `PRO-Robotech/kacho-workspace` (APPROVED). Ссылка дана путём внутри
того репозитория, а не относительной: файловый путь «через два уровня вверх»
описывал прежнюю раскладку из соседних репозиториев и не резолвится ни в
воркспейсе, ни в отдельном клоне продукта.

**Design**: проектный документ лежал в каталоге сторонних артефактов под `docs/`
монорепо; каталог удалён целиком решением владельца 2026-06-11 (коммит
`28778ef4`). Адрес не воспроизводится — процитированный, он читается как живой;
текст восстанавливается из истории по этому коммиту.

## Что это

Control-plane (без data-plane sibling) для трёх публичных ресурсов:

| Resource              | ID prefix | REST namespace              |
|-----------------------|-----------|-----------------------------|
| NetworkLoadBalancer   | `nlb`     | `/nlb/v1/networkLoadBalancers` |
| Listener              | `lst`     | `/nlb/v1/listeners`            |
| TargetGroup           | `tgr`     | `/nlb/v1/targetGroups`         |

- **Async LRO**: каждая мутация возвращает `operation.Operation`.
- **FGA REBAC** (KAC-108): per-RPC `iam.InternalIAMService.Check`.
- **Outbox + LISTEN/NOTIFY** на канал `nlb_outbox` (D-13 lifecycle stream).
- **Cross-service refs** (vpc / compute / iam) — soft sync precheck + graceful dangling.
- **DB-уровень инварианты** (FK / partial UNIQUE / atomic CAS) — workspace CLAUDE.md §10.

`GlobalLoadBalancer` (cross-region) — зарезервированный slot (`glb` prefix), реализация out-of-scope.

## Layout

См. `docs/architecture/01-layout.md` и evgeniy skill §1.A:

Иллюстрация ниже — не источник: сверяй `ls internal/` и `ls docs/architecture/`.

```
cmd/{kacho-loadbalancer,migrator}/main.go
internal/apps/kacho/{api,jobs,config,utils}
internal/{domain,repo,dto,clients,check,migrations,authzfilter,fgaboot,
          observability,operationresolver}
deploy/                # Helm chart
docs/architecture/     # ER, lifecycle, FGA, outbox, …
tests/{newman,k6}      # E2E + load
```

Прежняя редакция называла в `internal/` каталог записи в хранилище прав, которого
здесь нет, молчала о четырёх существующих и фиксировала число документов
архитектуры — оно с тех пор выросло. Число выведено из `ls docs/architecture/*.md`
в разделе «Статус» выше, а не выписано в двух местах.

## Build

Модуль в дереве один — `github.com/PRO-Robotech/kacho`; соседних репозиториев
фундамента и контрактов не существует с переходом на монорепо, поэтому сборка
ничего рядом с собой не требует. Команды запускаются от **корня репозитория**:

```bash
go build ./services/nlb/...
go vet ./services/nlb/...
go test ./services/nlb/... -race -short
```

## Образ

Образ сервиса собирает стенд: `make -C deploy build-services` берёт контекст от
корня репозитория и `services/nlb/Dockerfile`. У сервисного `Makefile` есть
собственная цель сборки образа, но она **не работает** — писана под раскладку из
соседних репозиториев и ищет Dockerfile по пути, которого в этом дереве нет.
Здесь она не цитируется как команда именно поэтому: процитированную её скопируют.
Долг числом: одна цель в `services/nlb/Makefile`, чинится вместе с выбором
контекста сборки.

## Conventions

- **Conventional Commits** + `KAC-<N>` в commit-message.
- **Branch per ticket** — `git checkout -b KAC-<N>` от `main`.
- **No `Co-Authored-By: Claude*`** trailers (workspace CLAUDE.md).
- **Никаких чужих облаков** в коде / комментариях (workspace CLAUDE.md §запрет 2);
  гейт — `go run ./tools/foreignclouds/cmd/verify-no-foreign-clouds` **от корня
  репозитория** (инструмент общий на всё дерево, не сервисный).
- **Test-first** — RED (падающий тест) → код фикса (GREEN). Newman-кейс / integration-тест
  до кода фичи; PR без тестов в том же PR не мерж'ат (запрет #11).

## Links

- Правила разработки живут в репозитории воркспейса `PRO-Robotech/kacho-workspace`
  (корневой индекс и модули `.claude/rules/`). Относительной ссылки сюда нет и не
  должно быть: отдельно склонированный продукт оснастки не несёт по построению, и
  такая ссылка ломалась бы у каждого, кто клонирует только его.
- Сервисных правил для AI-агентов у nlb нет: во всём репозитории продукта отслеживается
  **один** файл `CLAUDE.md` — в каталоге развёртывания (предикат:
  `git ls-files | grep CLAUDE.md`). Прежняя редакция ссылалась отсюда на соседний файл,
  которого нет; ссылка резолвилась поиском по одному лишь имени и потому не краснела.
- Permission catalog: `gateway/internal/middleware/permission_catalog.go` (+ встроенная
  копия `gateway/internal/middleware/embed/permission_catalog.json`), записи
  пространства `loadbalancer.*`. Прежняя редакция называла адрес внутри iam — каталог
  генерируется из proto и встраивается в двух местах, ни одно из которых там не лежит.
- Proto: `proto/kacho/cloud/loadbalancer/v1/*.proto` — **единственный** дом контрактов
  этого домена в монорепо; слова «vendored» здесь больше нечему соответствовать.
