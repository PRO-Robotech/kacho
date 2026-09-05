# Перепись зависимостей iam: точный состав будущей внешней зависимости

Решение владельца 2026-09-04: `services/iam` выносится отдельным репозиторием и
отдельным продуктом; общий фундамент `pkg/` и контракты `proto/` **остаются** в
`kacho`, а iam ссылается на них как на внешний versioned-модуль. Полный дебрендинг
отменён — iam by construction импортирует `github.com/PRO-Robotech/kacho/...`.
Консоль (`ui-future/`) остаётся в `kacho`.

Отсюда предмет этой переписи: **объём внешней зависимости надо знать точно**, потому
что каждый её пакет — это то, чью версию будущее изменение в `kacho` ломает у чужого
арендатора iam.

## Как получены числа

Единица счёта у всей переписи одна и та же и названа при каждом числе. Основа —
таблица «файл → импорт», построенная **разбором** (`go/parser`, `parser.ImportsOnly`)
по отслеживаемым файлам git, а не поиском по образцу: путь пакета встречается и в
комментариях, и в строковых литералах, и предикат по слову считал бы их.

```
git ls-files '*.go'                  → 5635 отслеживаемых файлов
разбор всех 5635                     → 39269 рёбер (файл, импортируемый путь), ошибок разбора 0
```

Ревизия дерева: `0c3b3313a798b5844bad9937220aa01dd22909c4`. Числа верны для неё и
устареют; рядом с каждым стоит команда, которой его перемерить.

Вспомогательно — `go list -f '{{.ImportPath}} {{join .Imports " "}}' ./services/iam/...`
(124 пакета Go в `services/iam`); он даёт тот же ответ на уровне пакетов и служил
контролем разбора.

### Исход проверки посылки задания

Перемерено; **посылка не подтвердилась ни по одному из четырёх чисел**, при том что
её качественные утверждения верны.

| утверждение задания | замер сегодня | единица счёта |
|---|---|---|
| «27 уникальных путей `pkg/` (без `pkg/api`)» | **41** путь пакета, либо **28** каталогов верхнего уровня `pkg/<X>` | путь импортируемого пакета · каталог первого уровня |
| «154 прод-файла iam из 633» | **163 из 585** | прод-файл iam (не `_test.go`) хотя бы с одним импортом `pkg/` кроме `pkg/api` |
| «из 20 пакетов iam миноритарный в 16» | из **41** пакета (без `pkg/api`) миноритарный в **34**, мажоритарный в **5**, поровну в **2** | пакет `pkg/`; «мажоритарный» = прод-файлов iam больше, чем прод-файлов всех прочих |
| «6-9 прод-мест тянут корневой `internal/` и `tools/`» | **12** прод-файлов и **200** файлов-проб | файл |

Знаменатель «633» не воспроизводится ни одним из проверенных отборов (сплошного
перебора отборов не делалось): прод-файлов iam сегодня
**585** (`git ls-files 'services/iam' \| grep '\.go$' \| grep -vc '_test\.go$'`),
всего файлов Go у iam — 1773, из них проб 1188. Это 31 % корпуса Go всего дерева.

---

## 1. Состав внешней зависимости

### 1.1 `pkg/` — что нужно ПРОД-коду iam

**41 путь пакета** (`pkg/api` исключён, он разбирается отдельно). По более крупной
единице — каталогу первого уровня `pkg/<X>` — это **28** каталогов из **41**, что
существуют в `pkg/` помимо `api`. Суммарно **28 887 строк** прод-кода Go.

Предикат:

```sh
go list -f '{{join .Imports " "}}' ./services/iam/... \
  | tr ' ' '\n' | grep -o 'github.com/PRO-Robotech/kacho/pkg/[a-zA-Z0-9_/]*' \
  | grep -v '/pkg/api' | sort -u | wc -l
```

Таблица ниже — по каждому пакету: сколько прод-файлов iam его импортируют, сколько
прод-файлов **всех прочих** компонентов, и какие это компоненты. Правая колонка и
есть ответ на вопрос «чью версию ломает будущее изменение».

| пакет `pkg/` | прод-файлов iam | прод-файлов прочих | прочих компонентов |
|---|---:|---:|---|
| `pkg/operations` | 70 | 141 | 8 |
| `pkg/safeconv` | **26** | 9 | 2 (vpc, gateway) |
| `pkg/tokenpolicy` | **19** | 7 | 2 (registry, gateway) |
| `pkg/ids` | 16 | 118 | 8 |
| `pkg/grpcsrv` | 15 | 30 | 8 |
| `pkg/validate` | 15 | 111 | 8 |
| `pkg/credsecret` | **7** | 1 | 1 (gateway) |
| `pkg/db` | 6 | 22 | 9 |
| `pkg/outbox/metrics` | 5 | 18 | 6 |
| `pkg/servicecontract` | 5 | 28 | 9 |
| `pkg/filter` · `pkg/outbox/drainer` | 4 | 17 · 10 | 5 · 5 |
| `pkg/db/pgfault` · `pkg/httpbody` · `pkg/identityposture` · `pkg/observability` · `pkg/operations/operationspb` · `pkg/outbox` · `pkg/pagetoken` | 3 | 14 · **1** · **3** · 14 · 14 · 12 · 4 | 7 · **1** · **1** · 7 · 6 · 7 · 2 |
| `pkg/authz` · `pkg/authz/proxytuple` · `pkg/errors` · `pkg/modulemanifest` · `pkg/observability/health` · `pkg/quota/quotadetail` · `pkg/quota/quotaread` · `pkg/subjectchange` · `pkg/subscription` | 2 | 40 · 12 · 21 · **1** · 11 · 11 · 19 · 3 · 11 | 8 · 7 · 6 · **1** · 6 · 5 · 6 · 2 · 5 |
| `pkg/audit` · `pkg/authz/catalogderive` · `pkg/config` · `pkg/grpcclient` · `pkg/migratorcli` · `pkg/migratorcli/cobraargs` · `pkg/outbox/reconciler` · `pkg/quota` · `pkg/quota/quotapb` · `pkg/retention` · `pkg/schemaguard` · `pkg/servicehost` · `pkg/validate/nameform` | 1 | **1** · 8 · 6 · 19 · 8 · 2 · 10 · 17 · 5 · 9 · 12 · 7 · 5 | **1** · 5 · 6 · 8 · 8 · 2 · 5 · 7 · 5 · 6 · 7 · 7 · 3 |

Полная таблица воспроизводится одной командой (она же строила числа выше) —
см. §«Приложение А».

**Мажоритарный потребитель iam ровно у пяти пакетов** — `credsecret` (7 против 1),
`httpbody` (3 против 1), `modulemanifest` (2 против 1), `safeconv` (26 против 9),
`tokenpolicy` (19 против 7); ещё у двух поровну — `audit` (1:1) и `identityposture`
(3:3). У остальных **34** iam миноритарный.

**Пакетов, которые потребляет ТОЛЬКО iam, — ноль.** То есть ни один пакет `pkg/`
нельзя унести вместе с iam «потому что он всё равно ничей»: у каждого есть второй
потребитель в дереве.

Практическое следствие: пять мажоритарных пакетов — это места, где сегодня версию
диктует iam, а после разреза диктовать будет `kacho`, и iam окажется в положении
внешнего потребителя своего же кода. Их семантику надо стабилизировать **до** выноса,
иначе первая же правка в `kacho` ломает продукт у арендатора.

### 1.2 `pkg/api/` — стабы, которые нужны ПРОД-коду iam

Три пакета, 95 прод-файлов iam:

| стаб | прод-файлов iam | прод-файлов прочих | прочих компонентов |
|---|---:|---:|---|
| `pkg/api/kacho/cloud/iam/v1` | **92** | 48 | 9 |
| `pkg/api/kacho/cloud/operation` | 28 | 132 | 8 |
| `pkg/api/kacho/cloud/quota/v1` | 2 | 7 | 2 |

`quota/v1` здесь не случайность: iam **служит** `IdentityQuotaService` — читатели
`services/iam/cmd/kaname/grpc_register.go` и
`services/iam/internal/apps/kaname/api/identityquota/handler.go`.

### 1.3 Что нужно ТОЛЬКО пробам

Сверх прод-набора пробы iam добавляют **ровно три** пути:

- `pkg/ownerregister` — 5 файлов iam при 14 у compute и 11 у storage;
- `pkg/platformmodules` — 5 файлов iam, 11 у корневого `internal/`, 2 у `pkg/`;
- `pkg/api/kacho/cloud/api` — общие опции контракта.

Предикат: `comm -13` двух отсортированных списков прод- и тест-импортов.
То есть тестовая надстройка **почти не расширяет** внешнюю зависимость по `pkg/`:
27 путей из 41, все внутри прод-набора, плюс эти три. Вес отвязки проб лежит
не здесь, а в корневом `internal/` — §2.

### 1.4 `proto/` — что нужно генерации

Транзитивное замыкание импортов контрактов, начиная с четырёх пакетов, чьи стабы iam
импортирует, — **55 файлов `.proto` из 128** в дереве:

| каталог | файлов |
|---|---:|
| `kacho/cloud/iam/v1` | 39 |
| `proto/google/` (восемь файлов стандартных опций) | 8 |
| `kacho/cloud/operation` | 3 |
| `kacho/cloud/api` | 2 |
| `kacho/cloud/quota/v1` | 2 |
| `kacho/iam/authz/v1` | 1 |

Вне домена iam и вне стандартных опций Google — **8 файлов** (пути ниже — относительные внутри `proto/`):
`kacho/cloud/api/operation.proto`, `kacho/cloud/api/secret_options.proto`,
`kacho/cloud/operation/{operation,operation_service,package_options}.proto`,
`kacho/cloud/quota/v1/{quota,identity_quota_service}.proto`,
`kacho/iam/authz/v1/authz_options.proto`.

Генерация ведётся `proto/buf.gen.yaml` (три плагина через `go run`, выход —
`pkg/api`, `paths=source_relative`). Плагины берутся **из модуля**, поэтому версия
генератора приезжает вместе с зависимостью, а не с машиной.

**Второй, менее заметный вход генерации — каталог прав.** Файлы
`gateway/internal/middleware/embed/permission_catalog.json` и
`services/iam/internal/apps/kaname/seed/embedded/permission_catalog.json` **побайтово
одинаковы** (`md5sum` обоих → `7b125fa181546f546045580977a34939`), а производит их
`gateway/scripts/gen-permission-catalog.sh` плагином
`gateway/cmd/protoc-gen-kacho-permissions`. Скрипт в своей же шапке говорит, что
требует **полный checkout монорепо**: каталог собирается из proto **всех** доменов
одним образом. Значит iam в отдельном репозитории свой посевной каталог
**перегенерировать не сможет** — он его только потребляет. Это самая жёсткая связь
разреза, и она не в коде, а в порождении.

---

## 2. Что iam тянет из КОРНЯ и потому не соберётся отдельным модулем

### 2.1 Почему это блокер, а не неудобство — доказано опытом, не цитатой правила

Проверено на отдельном модуле `example.com/xmod`, ссылающемся на дерево через
`replace`. Меняется **ровно один факт** — путь импорта:

```
дефект:  import _ "github.com/PRO-Robotech/kacho/internal/pgtest"
         defect.go:3:8: use of internal package .../internal/pgtest not allowed   rc=1

близнец: import _ "github.com/PRO-Robotech/kacho/pkg/ids"
         (пусто)                                                                  rc=0
```

Два контроля той же формы:

```
pkg/internal/tlsutil          → use of internal package ... not allowed   rc=1
services/iam/internal/domain  → use of internal package ... not allowed   rc=1
```

Третий контроль важен отдельно: он доказывает **обратное направление** — из
`kacho` после разреза нельзя будет импортировать `services/iam/internal/...`.
Сегодня этим никто и не пользуется (§4), поэтому разрез на этой оси свободен.

### 2.2 Перепись: 12 прод-файлов, 200 файлов-проб, 12 корневых пакетов

Единица счёта — отслеживаемый файл `services/iam/**/*.go` хотя бы с одним импортом
`github.com/PRO-Robotech/kacho/internal/...` либо `.../tools/...`. Рёбер (файл, пакет)
— 220.

**ПРОД — 12 файлов, поимённо:**

| файл | импортирует |
|---|---|
| `services/iam/cmd/migrator/main.go` | `internal/migratorrun` |
| `services/iam/internal/authzmodel/admit.go` | `internal/authzplan` |
| `services/iam/internal/authzmodel/authzmodel.go` | `internal/authzplan` |
| `services/iam/internal/authzmodel/relationsubjects.go` | `internal/authzplan` |
| `services/iam/internal/modelcompose/compose.go` | `internal/authzplan` |
| `services/iam/internal/modelrender/sweep.go` | `internal/authzplan` |
| `services/iam/internal/repo/kaname/pg/relverdict/query.go` | `internal/authzplan` |
| `services/iam/internal/repo/kaname/pg/scalegrid/report.go` | `internal/gitenv` |
| `services/iam/internal/repo/kaname/pg/scalegrid/strength.go` | `internal/gitenv` |
| `services/iam/internal/testsupport/iampgtest/iampgtest.go` | `internal/pgtest` |
| `services/iam/tools/auditlistfilter/profile.go` | `tools/listfiltergate` |
| `services/iam/tools/auditlistfilter/cmd/audit-list-filter/main.go` | `tools/listfiltergate` |

Оговорка к строке `iampgtest.go`: файл не `_test.go`, но это тестовая поддержка —
по существу он принадлежит §2.3, а не проду.

**Разбивка по корневым пакетам** (файлов iam; «прод» по расширению имени):

| корневой пакет | прод | пробы | строк кода | пакетов iam затронуто | кто ещё в дереве потребляет |
|---|---:|---:|---:|---:|---|
| `internal/pgtest` | 1 | **144** | 1047 | 27 | 10 компонентов (все сервисы, gateway, pkg, internal) |
| `internal/authzplan` | 6 | 25 | 1275 | 6 | **только `tools/authzformbench`** |
| `internal/treecorpus` | 0 | 25 | 1390 | 12 | 13 компонентов |
| `internal/gitenv` | 2 | 8 | 374 | 5 | tools, internal, deploy |
| `tools/listfiltergate` | 2 | 0 | 4003 | 2 | compute, geo, vpc, nlb |
| `tools/authzformbench` | 0 | 1 | 7212 | 1 | **никто, кроме iam** |
| `tools/modulemanifests` | 0 | 1 | 596 | 1 | deploy, tools |
| `internal/migratorrun` | 1 | 0 | 242 | 1 | 6 сервисов |
| `internal/dropguard` + `/dropguardtest` | 0 | 2 | 3286 | 2 | storage, compute, vpc, nlb, internal |
| `internal/nameformdb` | 0 | 1 | 830 | 1 | storage, vpc, nlb, compute |
| `internal/listcursorplan` | 0 | 1 | 270 | 1 | 6 сервисов |

Предикат перечня:

```sh
awk -F'\t' '$1 ~ /^services\/iam\// && ($2 ~ /kacho\/internal\// || $2 ~ /kacho\/tools\//)' \
  <таблица разбора> | sort -u
```

### 2.3 Вес отвязки лежит в пробах, а не в проде — и он сосредоточен в одном пакете

`internal/pgtest` (тестовая поддержка Postgres, 6 файлов, 1047 строк) импортируют
**144 файла-пробы iam напрямую**, из **27 пакетов** iam. Ещё 20 файлов ходят через
собственный мост `services/iam/internal/testsupport/iampgtest`, который сам импортирует
`internal/pgtest`. То есть **145 из 220** рёбер корневой зависимости — это одна
тестовая библиотека.

Это же и хорошая новость: мост уже существует. Если `internal/pgtest` переезжает в
`pkg/`, правится **один** файл моста, а 144 прямых импорта переводятся на мост
либо на новый путь механически — предметом правки остаётся один путь, а не логика.

### 2.4 Замена в `pkg/` — есть ли

**Нет ни у одного.** Предикат `git ls-files pkg/<имя>` для всех двенадцати даёт пусто.
То есть отвязка требует **переезда**, а не переключения на существующий аналог.

Кандидаты по назначению:

- **переезжают в `pkg/`** (общеплатформенные, много потребителей): `internal/pgtest`,
  `internal/migratorrun`, `internal/listcursorplan`, `internal/nameformdb`,
  `internal/dropguard`, `tools/listfiltergate`;
- **переезжают ВМЕСТЕ с iam** (iam-исключительные): `internal/authzplan` (потребители —
  iam и `tools/authzformbench`) и сам `tools/authzformbench` (потребитель — только iam).
  Вместе это 27 файлов и 8487 строк, которые сегодня лежат в корне, а принадлежат домену
  iam;
- **остаются в `kacho` и iam их теряет**: `internal/treecorpus` и `internal/gitenv` —
  это оснастка гейтов, судящих **индекс git монорепо**. У отдельного репозитория индекс
  другой, и переезд тут не поможет: 25 проб iam, использующих treecorpus, обязаны
  переехать вместе с iam и судить **его** дерево. См. §6.

---

## 3. Обратная привязка: зависит ли `pkg/` от iam

**Реализацию iam `pkg/` не импортирует ни одной строкой.**

```sh
awk -F'\t' '$1 ~ /^pkg\// && $2 ~ /kacho\/services\//' <таблица разбора>   → 0 строк
```

Шире: **ни один** файл `pkg/` не импортирует **ни один** `services/*`. Цикла на уровне
кода нет by construction.

**Стабами iam типизированы 18 файлов `pkg/`** — 10 прод и 8 проб:

| прод | пробы |
|---|---|
| `pkg/api/kacho/cloud/quota/v1/quota.pb.go` | `pkg/listnarrow/narrower_cache_stats_test.go` |
| `pkg/listnarrow/client.go` | `pkg/listnarrow/narrower_contract_test.go` |
| `pkg/listnarrow/narrower.go` | `pkg/listnarrow/page_bench_test.go` |
| `pkg/listnarrow/narrowtest/narrowtest.go` | `pkg/ownerregister/ownerregister_test.go` |
| `pkg/listnarrow/object.go` | `pkg/servicehost/wiring_test.go` |
| `pkg/ownerregister/ownerregister.go` | `pkg/subjectchange/readerpositionlost_test.go` |
| `pkg/quota/quotaiam/delta.go` | `pkg/subjectchange/reader_test.go` |
| `pkg/quota/quotapb/convert.go` | `pkg/subscription/revocation_integration_test.go` |
| `pkg/servicehost/serve.go` | |
| `pkg/subjectchange/reader.go` | |

**Это НЕ цикл — и посылка задания здесь неверна.** Она исходила из того, что контракт
iam уедет вместе с iam; решение владельца 2026-09-04 говорит обратное: `proto/`
остаётся в `kacho`, а с ним остаются и стабы `pkg/api/kacho/cloud/iam/v1`. Тогда
`pkg → pkg/api/.../iam/v1` — ребро **внутри** `kacho`, и снимать его не нужно.

Что при этом надо знать и сказать вслух: **iam перестаёт владеть собственным
контрактом.** Правка `proto/kacho/cloud/iam/v1/*` становится изменением в `kacho`,
которое iam получает выпуском версии. Ровно поэтому здесь есть предмет для решения
владельца — см. §6, шаг 0.

`pkg/quota/quotaiam` (имя обманчиво) — клиентская сторона для разговора **с** iam:
его импортируют пять файлов, и все пять — `limit_client.go` у compute, nlb, registry,
storage, vpc. iam его не импортирует. Он остаётся в `kacho`.

---

## 4. Что ломается у остальных

**Реализацию iam вне iam не импортирует никто:**

```sh
awk -F'\t' '$1 !~ /^services\/iam\// && $2 ~ /kacho\/services\/iam\//' <таблица>  → 0
```

Ноль прод-файлов, ноль проб. Соседи ходят в iam только по gRPC.

**Часть этого нуля — by construction, и это надо сказать, иначе он читается как заслуга
дисциплины.** Из 1773 файлов Go у iam **1678 лежат под `services/iam/internal/`**, куда
Go запрещает импорт извне уже сегодня (доказано контролем 3 в §2.1). Всё остальное —
пакеты `main` (четыре под `cmd/`, три под `tools/` и `tests/`), которые не импортируются
by construction тоже.

**Достижимых извне пакетов у iam ровно пять**, и вот они с числом импортёров вне iam:

| пакет | импортёров вне iam |
|---|---:|
| `services/iam/deploy` | 0 |
| `services/iam/tools` | 0 |
| `services/iam/tools/auditlistfilter` | 0 |
| `services/iam/tools/newmanverdict` | 0 |
| `services/iam/tools/subjectstategate` | 0 |

То есть ноль в §4 — это *by construction* на 1678 файлах и *по факту* на пяти пакетах.
Вывод от этого не меняется, но его основание становится прочнее: разрез не может
сломать соседей по коду, потому что ломать нечего.

**Стабы iam импортируют 121 файл вне iam** — 48 прод и 73 пробы:

| компонент | прод | пробы |
|---|---:|---:|
| `gateway` | 6 | 21 |
| `pkg` | 10 | 8 |
| `terraform` | 7 | 0 |
| `services/nlb` | 4 | 15 |
| `services/compute` | 5 | 11 |
| `services/vpc` | 5 | 6 |
| `services/storage` | 5 | 3 |
| `services/registry` | 4 | 5 |
| `internal` | 2 | 4 |

Поскольку стабы остаются в `kacho`, **цена разреза для соседей по коду — ноль**. Все
121 продолжают компилироваться без единой правки. Это самая важная величина всей
переписи: она означает, что разрез не требует согласованной волны по семи компонентам.

Цена для соседей ненулевая в другом месте — в **порождении** каталога прав (§1.4) и в
**гейтах дерева** (§6).

---

## 5. Контракт разметки прав

Файл — `proto/kacho/iam/authz/v1/authz_options.proto`, пакет `kacho.iam.authz.v1`,
`go_package = ".../pkg/api/kacho/iam/authz/v1"`. Обратите внимание: он лежит **вне**
`proto/kacho/cloud/`, то есть уже сегодня объявлен как платформенный словарь, а не как
контракт домена iam.

**Импортируют его 68 контрактов из 128, из них НЕ-iam — 46.**

```sh
grep -rl 'kacho/iam/authz/v1/authz_options.proto' --include='*.proto' proto/ | wc -l   # 68
… | grep -vc '^proto/kacho/cloud/iam/'                                                # 46
```

По доменам: iam 22 · vpc 14 · storage 9 · compute 8 · loadbalancer 5 · registry 3 ·
geo 3 · subscription 1 · quota 1 · operation 1 · api 1.

**Go-сторона отвечает то же самое, и даже резче.** Сгенерённый `authzv1` импортируют
71 файл: 68 в `pkg/` (из них 22 — стабы iam, остальные 46 — стабы прочих доменов),
2 в `gateway/cmd/protoc-gen-kacho-permissions` и 1 в `internal/repohygiene`. Файлов
`services/iam`, импортирующих `authzv1`, — **ноль**.

**Вывод: разметку прав вынести вместе с доменом НЕЛЬЗЯ.** Это словарь аннотаций всей
платформы; его читает генератор каталога, живущий в `gateway`, а не iam. Он остаётся в
`kacho` и приезжает к iam той же внешней зависимостью, что и остальные контракты. Две
трети его потребителей — чужие домены, и унести их за собой он не может.

Обратная сторона, которую надо сказать прямо: **iam получает свой посевной каталог
прав как ВХОД извне.** Сегодня побайтовая одинаковость двух копий — свойство одного
дерева и одного прогона генератора; после разреза она становится свойством **выпуска
версии**, и её обязан держать предикат на стороне iam, а не совпадение содержимого.

---

## 6. Порядок отвязки

### Шаг 0 — ждёт решения владельца (сделать нельзя, пока не решено)

1. **Кто владеет контрактом iam после разреза.** Сегодня iam владеет и контрактом, и
   реализацией. Решение 2026-09-04 оставляет `proto/` в `kacho` — значит правка
   контракта iam становится правкой в чужом репозитории, а выпуск iam ждёт версии.
   Альтернатива (контракт iam уезжает, `pkg/api/.../iam/v1` остаётся зеркалом в `kacho`)
   заводит ровно тот цикл, которого сегодня нет (§3), и потому по умолчанию отвергается —
   но выбор принадлежит владельцу, а не переписи.
2. **Кто производит посевной каталог прав.** Генератор требует полного дерева proto всех
   доменов (§1.4); iam его перегенерировать не сможет. Варианта два: каталог выпускается
   вместе с версией `kacho` и iam его потребляет, либо генератор переезжает так, чтобы
   работать над **набором** контрактов, а не над деревом.
3. **Обещание совместимости внешней зависимости.** Пять пакетов, где iam сегодня
   мажоритарный потребитель (`safeconv`, `tokenpolicy`, `credsecret`, `httpbody`,
   `modulemanifest`), после разреза меняют владельца де-факто. Нужен объявленный предикат:
   что в `pkg/` считается ломающим изменением для внешнего iam.

### Шаги 1–5 — делаются БЕЗ решений владельца, каждый самостоятельно ценен

Порядок выведен из зависимостей, а не из удобства: каждый следующий шаг опирается на
предыдущий, и каждый оставляет дерево зелёным.

| # | шаг | предмет | предикат снятия |
|---:|---|---|---|
| 1 | **`internal/pgtest` переезжает в `pkg/` (имя каталога — предмет самого шага, здесь координатой не пишется)** | 145 из 220 рёбер корневой зависимости iam; 10 компонентов-потребителей | файлов iam, импортирующих `kacho/internal/pgtest`, — 0 |
| 2 | **`internal/migratorrun`, `internal/listcursorplan`, `internal/nameformdb`, `internal/dropguard`, `tools/listfiltergate` → `pkg/`** | общеплатформенные, 4–7 потребителей каждый; у iam это 3 прод-файла и 4 пробы | тех же импортов из корня у iam — 0 |
| 3 | **`internal/authzplan` + `tools/authzformbench` → `services/iam/internal/`** | iam-исключительный кластер: 27 файлов, 8487 строк; вне iam потребитель один и сам iam-исключительный | `git grep -l 'kacho/internal/authzplan' -- ':!services/iam'` → пусто |
| 4 | **гейты дерева, судящие `services/iam`, переезжают вместе с ним** | `internal/repohygiene` — 781 файл, из них **127 называют `services/iam`** (кандидаты; сколько из них действительно **судят** iam, а не упоминают его в ведомости исключений или в разборе, требует адъюдикации по каждому — сплошной адъюдикации НЕ проводилось). Плюс 25 проб самого iam, обходящих дерево через `internal/treecorpus`, и 8 проб через `internal/gitenv` | у iam есть свой корпус обхода, судящий его индекс; в `kacho` не осталось гейта, чей обход требует несуществующего каталога |
| 5 | **iam собирается отдельным модулем** — контрольный прогон | доказательство: `go build` отдельного модуля с `replace` на дерево | сборка и пробы iam зелены при **нуле** импортов `kacho/internal/**` и `kacho/tools/**` |

Шаг 4 — самый дорогой и наименее измеренный: число 127 получено предикатом
`git grep -l 'services/iam' -- 'internal/repohygiene/*.go' | wc -l`, единица счёта —
**файл, где строка встречается хотя бы раз**, включая комментарии и ведомости
исключений. Это перечень кандидатов, а не перечень находок; отделить «судит iam» от
«упоминает iam» может только адъюдикация по каждому, и она отдельная работа.

### Чего эта перепись НЕ покрывает

- **Не-Go поверхность.** Чарт `deploy/helm/umbrella/charts/kaname` (42 файла с `iam`
  в пути под `deploy/`), консоль (141 файл под `ui-future/`), документация. Консоль по
  решению владельца остаётся в `kacho` — значит появляется ещё одна связь «продукт iam ↔
  его консоль в чужом репозитории», и она здесь не разобрана.
- **Рантайм-рёбра.** Перепись читает **импорты**, а не вызовы gRPC. То, что реализацию
  iam не импортирует никто (§4), не означает, что iam ни от кого не зависит на пути
  запроса: пять сервисов ходят к нему за пределами, gateway — за каталогом и JWKS.
  Перечень рёбер живёт в модуле правил воркспейса о топологии репозиториев и здесь не дублируется.
- **Адъюдикация 127 гейтов** (шаг 4) — названа кандидатом, не сделана.
- **Внешние зависимости модуля** (`go.mod` третьих сторон) — не мерялись; предмет
  переписи был `github.com/PRO-Robotech/kacho/**`.

---

## Приложение А. Как воспроизвести перепись целиком

```sh
# 1. таблица «файл → импорт» разбором (не поиском по образцу)
#    крошечная программа на go/parser, parser.ImportsOnly, по git ls-files '*.go'
git ls-files '*.go' | xargs <разборщик> > imports.tsv      # 39269 рёбер, 0 ошибок разбора

M=github.com/PRO-Robotech/kacho

# 2. пакеты pkg/, нужные проду iam
awk -F'\t' -v m=$M '$1 ~ /^services\/iam\// && $1 !~ /_test\.go$/ &&
  index($2,m"/pkg/")==1 && index($2,m"/pkg/api")!=1 {print $2}' imports.tsv | sort -u

# 3. мажоритарность: прод-файлы iam против прод-файлов прочих, по пакету
awk -F'\t' -v m=$M 'index($2,m"/pkg/")==1 && $1 !~ /_test\.go$/ {
  p=$2; sub("^"m"/","",p); n=split($1,a,"/"); c=(a[1]=="services")?a[1]"/"a[2]:a[1];
  if(c=="services/iam") iam[p SUBSEP $1]=1; else oth[p SUBSEP $1]=1 }
  END{ for(k in iam){split(k,b,SUBSEP); ni[b[1]]++}
       for(k in oth){split(k,b,SUBSEP); no[b[1]]++}
       for(p in ni) print p, ni[p], no[p]+0 }' imports.tsv | sort

# 4. корневой internal/ и tools/ у iam
awk -F'\t' -v m=$M '$1 ~ /^services\/iam\// &&
  (index($2,m"/internal/")==1 || index($2,m"/tools/")==1) {print $1"\t"$2}' imports.tsv | sort -u

# 5. обратная привязка: pkg → iam
awk -F'\t' -v m=$M '$1 ~ /^pkg\// && index($2,m"/services/")==1' imports.tsv      # пусто
awk -F'\t' -v m=$M '$1 ~ /^pkg\// && $2==m"/pkg/api/kacho/cloud/iam/v1" {print $1}' imports.tsv | sort -u

# 6. цена для соседей
awk -F'\t' -v m=$M '$1 !~ /^services\/iam\// && index($2,m"/services/iam/")==1' imports.tsv   # пусто
awk -F'\t' -v m=$M '$1 !~ /^services\/iam\// && index($2,m"/pkg/api/kacho/cloud/iam/")==1 {print $1}' imports.tsv | sort -u | wc -l

# 7. разметка прав
grep -rl 'kacho/iam/authz/v1/authz_options.proto' --include='*.proto' proto/ | wc -l
grep -rl 'kacho/iam/authz/v1/authz_options.proto' --include='*.proto' proto/ | grep -vc '^proto/kacho/cloud/iam/'

# 8. доказательство недостижимости корневого internal/ из чужого модуля
#    отдельный модуль с replace на дерево; меняется РОВНО один факт — путь импорта
#    internal/pgtest → "use of internal package ... not allowed", rc=1
#    pkg/ids        → пусто, rc=0
```
