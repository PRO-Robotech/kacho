# k6 — нагрузочные тесты kacho-vpc

Pre-req:
- k6 v0.55+ (`/usr/local/bin/k6` или `~/.local/bin/k6`)
- kind-кластер с kacho-vpc запущен, port-forward `:18080`
- pre-seeded fixtures: Account/Project, Region `zone`, zones a/b/c/d,
  default AddressPool с CIDR /16+ на zone-a

## Запуск

```bash
# Single scenario
k6 run scripts/network-create-burst.js

# Прогон в CI-режиме: ПЯТЬ сценариев из девяти, по возрастанию нагрузки
# (список — в самом run-all.sh; «все» он не гоняет)
./run-all.sh

# Specific environment
k6 run --env BASE_URL=http://localhost:18080 --env PROJECT_ID=b1gXXX scripts/network-create-burst.js

# Save results
k6 run --out json=results/network-create-burst.json scripts/network-create-burst.js
```

## Сценарии

Перечень сверяется с деревом, а не помнится, и предикат тут **два**, потому что
сценарии лежат в двух местах: `ls scripts/*.js` — на 2026-08-06 восемь файлов,
плюс один сценарий в корне `tests/k6/` (последняя строка таблицы), итого девять.
Один предикат назвать было нельзя: `ls scripts/*.js` даёт восемь, и число «девять»
рядом с ним оказалось бы верным для другого предиката, чем объявленный.

| Файл | Назначение | В `run-all.sh` |
|---|---|---|
| `list-heavy.js` | List под нагрузкой read | да (1-й) |
| `network-create-burst.js` | Burst Network Create | да (2-й) |
| `allocate-external-burst.js` | AllocateExternalIP capacity | да (3-й) |
| `mixed-read-write.js` | Production-like смесь | да (4-й) |
| `breakpoint.js` | Linear ramp до отказа | да (5-й) |
| `network-create-pure.js` | Create без сопутствующего чтения | нет |
| `network-update-burst.js` | Burst Network Update | нет |
| `allocate-external-pure.js` | Аллокация без сопутствующего чтения | нет |
| `list_filter_perf.js` | Стоимость фильтра списка (лежит в корне `tests/k6/`, не в `scripts/`) | нет |

> [!note] Три строки этой таблицы описывали сценарии, которых в дереве нет
> Прежняя редакция называла burst-создание подсети, замер задержки до
> `done=true` и суточный soak — ни одного из этих файлов в `scripts/` нет, и
> ни один не выполнялся бы. Их имена здесь не воспроизводятся: путь в обратных
> кавычках читается следующим как живая координата. Одновременно таблица
> **молчала** о трёх существующих сценариях — расхождение множеств в обе
> стороны, а не «немного устарело».

Профили нагрузки (VU, длительность) заданы **в самих сценариях** и здесь не
дублируются: прежняя редакция несла их отдельной колонкой, которая ни из чего не
выводилась и разошлась бы с первой же правкой `options` в файле.

## SLO targets (local KIND)

| Scenario | RPS sustained | p99 | Error rate |
|---|---|---|---|
| Network Create | ≥ 30 | < 1500ms | < 1% |
| Subnet Create | ≥ 20 | < 1000ms | < 1% |
| Address (ext) | ≥ 50 | < 600ms | < 0.5% |
| Get/List | ≥ 200 | < 100ms | < 0.1% |

## Files

```
tests/k6/
├── scripts/
│   ├── lib/
│   │   ├── client.js     — common HTTP + auth headers
│   │   └── poll-op.js    — LRO polling
│   └── *.js              — сценарии, перечень в таблице выше
├── ghz/                  — gRPC-нагрузка (отдельный инструмент)
├── list_filter_perf.js   — стоимость фильтра списка
├── results/              — gitignored
├── run-all.sh            — прогон ПЯТИ сценариев, не всех
└── README.md
```

Дерево выше — иллюстрация, а не источник: сверяй `ls -R`, не память. Прежняя
редакция называла в `lib/` два помощника, которых здесь нет (один существует у
соседнего сервиса — оттого и не ловился поиском по голому имени), и целый
каталог окружений с файлом настроек. Каталога нет: адреса приезжают флагами
`--env`, а их значения по умолчанию заданы в `run-all.sh`.
