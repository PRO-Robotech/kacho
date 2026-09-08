// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// db_footprint_declaration_test.go — стенд, объявивший боевую посадку, обязан
// САМ выбрать, сколько памяти его базе можно и сколько она из неё потратит.
// Обе величины объявляются ПРОФИЛЕМ, и они обязаны сходиться.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// У базы данных две независимые величины, и обе легко получить, не выбрав:
//
//   - ПОТОЛОК — `primary.resources`. Не объявишь — приедет умолчание подчарта
//     postgresql (`requests` 250m/256Mi, `limits` ПУСТЫЕ). Пустой предел памяти
//     означает, что база вправе съесть узел целиком, а крошечный запрос —
//     что планировщик поставит её туда, где памяти уже нет;
//   - ТРАТА — `primary.extendedConfiguration`. Не объявишь — приедут умолчания
//     СБОРКИ PostgreSQL (`max_connections` 100, `shared_buffers` 128MB,
//     `work_mem` 4MB). Их никто не выбирал под этот стенд.
//
// Из-за этого стенд, на котором снимают замер пропускной способности, бывает
// настроен ЛУЧШЕ боевой поставки — и вывод замера переносится на посадку, где
// он не проверялся ни разу. Ровно это и нашлось (#756): профиль `values.prod.yaml`
// не объявлял посадку НИ ОДНОЙ из десяти своих баз, тогда как стенд замера
// объявлял и посадку, и все три настройки.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ «ОБЪЯВЛЕНО ПРОФИЛЕМ», А НЕ «ДЕЙСТВУЮЩЕЕ ЗНАЧЕНИЕ НЕПУСТО»
//
// Умолчание подчарта `primary.resources` НЕПУСТО (`requests` 250m/256Mi) —
// проверено распаковкой postgresql-13.4.4.tgz. Значит проверка, спрашивающая
// «непусто ли действующее», зелена на профиле, который не объявил ничего: она
// увидит умолчание чужого чарта и примет его за выбор. Поэтому спрашивается
// ровно одно — объявил ли ЭТОТ стек своими профилями.
//
// Та же причина, что у соседних dbtls_declaration_test.go и posture_parity_test.go:
// умолчание — свойство ЧАРТА, оно меняется под профилем без единой правки
// профиля. Ни `helm`, ни скачанные зависимости чартов здесь не нужны, поэтому
// проверка не умеет пропуститься.
//
// ─────────────────────────────────────────────────────────────────────────────
// АРИФМЕТИКА, РАДИ КОТОРОЙ ЭТА ПРОВЕРКА И НАПИСАНА
//
// Объявить обе величины мало — они обязаны СХОДИТЬСЯ. Довод не выдуман здесь:
// он дословно стоит в values.a8f60d.yaml у pg-iam («тут же объявлено
// `max_connections = 200` при `work_mem = 8MB`, то есть наполненный пул просит
// до 1.6Gi только на сортировки — больше прежнего предела»). Проверка считает
// то же самое:
//
//	shared_buffers + max_connections × work_mem  ≤  limits.memory
//
// `effective_cache_size` в сумму НЕ входит намеренно: это подсказка планировщику
// о кэше ОС, а не выделение памяти процессом.
//
// Оценка ВЕРХНЯЯ и заведомо грубая: `work_mem` тратится на узел сортировки, а не
// на соединение, поэтому настоящий худший случай ВЫШЕ этой суммы, а типичный —
// сильно ниже. Проверка ловит не «сколько будет израсходовано», а «объявленный
// потолок не вмещает даже объявленную трату» — то есть случай, где две величины
// назначены не глядя друг на друга.
//
// ─────────────────────────────────────────────────────────────────────────────
// ГРАНИЦА ПРЕДМЕТА (названа, чтобы «зелено» не читалось шире, чем есть)
//
//   - Проверяются ТОЛЬКО стеки, ОБЪЯВИВШИЕ боевую посадку — предикат
//     declaresProduction из identity_dev_flag_declaration_test.go, тот же самый,
//     без своей копии. Стенд разработки волен не выбирать ничего.
//   - ПОТОЛОК требуется от КАЖДОГО инстанса Postgres умбреллы: под без предела
//     памяти уносит узел независимо от того, чьи данные он держит.
//   - ТРАТА требуется только от баз НАШИХ служб — тех, чьё имя названо в
//     `db.host` какого-то нашего подчарта (тот же признак, что у соседнего
//     pool_fits_database_test.go). Хранилища Ory и OpenFGA настраивают свои
//     чарты, и решать за них здесь нечего.
//   - Проверка НЕ судит, ВЕРНЫ ли выбранные числа. Она судит, выбраны ли они и
//     сходятся ли между собой. «Мало памяти» — вопрос замера, а не объявления.
//   - Отношение «пул службы ↔ предел базы» здесь НЕ проверяется: им владеет
//     соседний pool_fits_database_test.go. Здесь `max_connections` нужен как
//     СЛАГАЕМОЕ арифметики выше, и требуется только его объявление.
package deploy_test

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// Ведомость известных пропусков.
//
// Существует не затем, чтобы отвернуться от находки, а затем, чтобы чужой
// предмет чинился своим изменением: смешение линий делает вердикт
// непрослеживаемым, а перепись — недоказуемой.
//
// Запись САМОИСТЕКАЕТ: как только связка начинает объявлять своё, ей больше
// нечего исключать — и гейт падает на самой записи
// (TestFootprintLedgerEntriesStillHaveASubject). Иначе исключение пережило бы
// свой предмет, а следующий читатель принял бы его за действующее ограничение.
//
// Ключ — «<стек>/<база>/<вид находки>».
//
// ВЕДОМОСТЬ ПУСТА — и это её нормальное состояние, а не признак поломки. Все
// восемнадцать записей, стоявшие здесь с заведения гейта, сняты вместе со своим
// предметом (#808): три стенда, объявляющие боевую посадку, объявляют теперь и
// посадку своих баз. Слой, где это объявлено, выбран так, чтобы правка не
// доставала до стенда разработки: values.dev-prod.yaml входит в цепочку ровно
// трёх боевых стендов и НЕ входит в цепочку `dev` (deploy/stacks.txt), а
// values.a8f60d.yaml сужает трату там, где потолок кластера вчетверо ниже.
var knownUndeclaredFootprint = map[string]string{}

// ─────────────────────────────────────────────────────────────────────────────
// Разбор величин. Обе стороны неравенства записаны РАЗНЫМИ единицами: слева
// postgresql.conf (`128MB`), справа Kubernetes (`1Gi`). Сводятся к байтам.

var (
	// k8sQuantity — величина ресурса Kubernetes. Двоичные суффиксы (Ki/Mi/Gi/Ti)
	// и десятичные (k/M/G/T), плюс голое число байт.
	k8sQuantity = regexp.MustCompile(`^([0-9]+(?:\.[0-9]+)?)\s*(Ki|Mi|Gi|Ti|k|M|G|T)?$`)
	// pgMemoryValue — величина памяти postgresql.conf. Единица ОБЯЗАТЕЛЬНА:
	// голое число там означает блоки по 8kB для shared_buffers и килобайты для
	// work_mem, то есть одна и та же запись значит разное у разных ручек.
	// Принять её молча значило бы посчитать не то, что настроено.
	pgMemoryValue = regexp.MustCompile(`^([0-9]+)\s*(kB|MB|GB|TB)$`)
	// pgConfLine — строка `ключ = значение` postgresql.conf. Комментарии
	// (`#`) отсекаются до разбора: комментарий, объясняющий ручку, не является
	// её объявлением.
	pgConfLine = regexp.MustCompile(`^\s*([a-z_]+)\s*=\s*(.+?)\s*$`)
)

func parseK8sQuantity(s string) (int64, bool) {
	m := k8sQuantity.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0, false
	}
	f, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, false
	}
	mult := map[string]float64{
		"": 1, "Ki": 1 << 10, "Mi": 1 << 20, "Gi": 1 << 30, "Ti": 1 << 40,
		"k": 1e3, "M": 1e6, "G": 1e9, "T": 1e12,
	}[m[2]]
	return int64(f * mult), true
}

func parsePGMemory(s string) (int64, bool) {
	m := pgMemoryValue.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0, false
	}
	n, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		return 0, false
	}
	mult := map[string]int64{"kB": 1 << 10, "MB": 1 << 20, "GB": 1 << 30, "TB": 1 << 40}[m[2]]
	return n * mult, true
}

// parsePGConf разбирает объявленный extendedConfiguration в карту ключ→значение.
func parsePGConf(raw string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(raw, "\n") {
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		if m := pgConfLine.FindStringSubmatch(line); m != nil {
			out[m[1]] = strings.Trim(strings.TrimSpace(m[2]), `'"`)
		}
	}
	return out
}

// humanBytes — величина в мегабайтах, чтобы обе стороны неравенства читались
// в одной единице.
func humanBytes(b int64) string { return fmt.Sprintf("%dMiB", b>>20) }

// ─────────────────────────────────────────────────────────────────────────────
// Факты. Ничего не выписывается руками.

// dbFootprintFacts — то, что проверке нужно знать про один стек.
type dbFootprintFacts struct {
	stack      string
	production bool
	prodWhy    string
	aliases    []string          // ВСЕ инстансы Postgres умбреллы
	ours       map[string]string // алиас → подчарт, назвавший его своим db.host
	// declared — ТОЛЬКО то, что объявили профили ЭТОГО стека. Умолчаний ни
	// умбреллы, ни подчартов здесь нет: иначе «объявлено» и «унаследовано»
	// становятся неразличимы, а это и есть предмет проверки.
	declared map[string]any
}

// dbFootprintFactsFor собирает факты одного стека.
func dbFootprintFactsFor(t *testing.T, name string, chain []string) dbFootprintFacts {
	t.Helper()

	declared := map[string]any{}
	for _, p := range chain {
		declared = mergeValues(declared, readYAML(t, filepath.Join(umbrellaDir, p)))
	}
	prod, why := declaresProduction(declared)

	// Кто чью базу называет своим хостом — по ПОЛНОМУ дереву значений (с
	// умолчаниями подчартов): боевые профили службу не переобъявляют вовсе, её
	// `db.host` живёт только в её собственном values.yaml.
	full := valuesWithSubchartDefaults(t, chain)
	var pgs []string
	for a := range full {
		if strings.HasPrefix(a, "pg-") {
			pgs = append(pgs, a)
		}
	}
	sort.Strings(pgs)

	subs := make([]string, 0, len(full))
	for a := range full {
		if !strings.HasPrefix(a, "pg-") {
			subs = append(subs, a)
		}
	}
	sort.Strings(subs)

	ours := map[string]string{}
	for _, s := range subs {
		sub, _ := full[s].(map[string]any)
		if sub == nil {
			continue
		}
		host, ok := lookup(sub, "db", "host")
		if !ok {
			continue
		}
		for _, pg := range pgs {
			if strings.HasSuffix(fmt.Sprint(host), pg) {
				ours[pg] = s
			}
		}
	}

	return dbFootprintFacts{
		stack: name, production: prod, prodWhy: why,
		aliases: pgs, ours: ours, declared: declared,
	}
}

func allDBFootprintFacts(t *testing.T) []dbFootprintFacts {
	t.Helper()
	chains := deployStacks(t)
	names := make([]string, 0, len(chains))
	for n := range chains {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]dbFootprintFacts, 0, len(names))
	for _, n := range names {
		out = append(out, dbFootprintFactsFor(t, n, chains[n]))
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// ЯДРО — чистая функция над фактами, чтобы самопроверка могла подать ей
// синтетический вход, а не подделывать дерево.

type footprintFinding struct {
	stack string
	alias string
	kind  string // см. константы ниже
	why   string
}

// key — ключ ведомости. ВИД находки входит в ключ намеренно: без него запись,
// заведённая под «трата не объявлена», молча накрыла бы и «трата больше
// потолка», если бы связка объявила трату и промахнулась мимо своего потолка.
// Тогда исключение расширялось бы само, без единой правки ведомости.
func (f footprintFinding) key() string { return f.stack + "/" + f.alias + "/" + f.kind }

const (
	kindNoCeiling = "потолок не объявлен"  // primary.resources
	kindNoBudget  = "трата не объявлена"   // primary.extendedConfiguration
	kindOverspend = "трата больше потолка" // арифметика
)

// pgBudgetKnobs — ручки, из которых складывается объявленная трата. Перечислены
// здесь, потому что это и есть предмет: слагаемые суммы плюс её множитель.
var pgBudgetKnobs = []string{"max_connections", "shared_buffers", "work_mem"}

func scanDBFootprint(facts []dbFootprintFacts) []footprintFinding {
	var out []footprintFinding
	for _, f := range facts {
		// Требований ДВА, и они адресованы РАЗНЫМ множествам стеков.
		//
		//   ОБЯЗАН ОБЪЯВИТЬ — только боевой стек. «Стенд разработки волен не
		//   выбирать ничего» остаётся в силе дословно: невыбравший вне предмета.
		//
		//   ОБЪЯВЛЕННОЕ ИСПОЛНИМО — всякий стек, который величины НАЗВАЛ. Здесь
		//   боевой он или нет, значения не имеет: объявив потолок памяти и
		//   max_connections, стек утверждает, что второе умещается в первое, —
		//   и это утверждение либо верно, либо нет.
		//
		// Различие не педантское, и цена его отсутствия измерена (#883). Стенд
		// разработки объявлял обе величины, и они не сходились в 1.7 раза:
		// 128MiB + 200 × 8MiB = 1728MiB против потолка 1GiB. База прав падала
		// с кодом 137 под нагрузкой ровно так, как эта арифметика предсказывает,
		// а замеры пропускной способности, снятые на таком стенде, были
		// недействительны — при том что здесь ими и меряют.
		//
		// Прежняя редакция отсекала небоевой стек ЦЕЛИКОМ, поэтому проверка
		// молчала не потому, что арифметика сошлась, а потому, что её не
		// спрашивали.
		mustDeclare := f.production
		for _, alias := range f.aliases {
			node, _ := lookup(f.declared, alias, "primary")
			primary, _ := node.(map[string]any)

			// ── ПОТОЛОК ──
			res, _ := primary["resources"].(map[string]any)
			limitMem, limitOK := int64(0), false
			var missing []string
			if len(res) == 0 {
				missing = append(missing, "resources")
			} else {
				for _, p := range [][]string{
					{"requests", "cpu"}, {"requests", "memory"}, {"limits", "memory"},
				} {
					v, ok := lookup(res, p...)
					if !ok || fmt.Sprint(v) == "" {
						missing = append(missing, "resources."+strings.Join(p, "."))
						continue
					}
					if p[1] == "memory" && p[0] == "limits" {
						if b, ok := parseK8sQuantity(fmt.Sprint(v)); ok {
							limitMem, limitOK = b, true
						} else {
							missing = append(missing, "resources.limits.memory (не разобрано: "+fmt.Sprint(v)+")")
						}
					}
				}
			}
			if len(missing) > 0 {
				if mustDeclare {
					out = append(out, footprintFinding{f.stack, alias, kindNoCeiling,
						"не объявлено профилем: " + strings.Join(missing, ", ")})
				}
				continue // без потолка спрашивать про трату нечего — иначе один пропуск считается дважды
			}

			// ── ТРАТА ── только у баз НАШИХ служб.
			if _, mine := f.ours[alias]; !mine {
				continue
			}
			rawConf, hasConf := primary["extendedConfiguration"]
			conf := map[string]string{}
			if hasConf {
				conf = parsePGConf(fmt.Sprint(rawConf))
			}
			var absent []string
			for _, k := range pgBudgetKnobs {
				if _, ok := conf[k]; !ok {
					absent = append(absent, k)
				}
			}
			if len(absent) > 0 {
				if mustDeclare {
					out = append(out, footprintFinding{f.stack, alias, kindNoBudget,
						"не объявлено профилем: extendedConfiguration " + strings.Join(absent, ", ")})
				}
				continue
			}

			// ── АРИФМЕТИКА ──
			conns, connsErr := strconv.Atoi(conf["max_connections"])
			shared, sharedOK := parsePGMemory(conf["shared_buffers"])
			work, workOK := parsePGMemory(conf["work_mem"])
			switch {
			case connsErr != nil:
				out = append(out, footprintFinding{f.stack, alias, kindNoBudget,
					"max_connections не число: " + conf["max_connections"]})
			case !sharedOK:
				out = append(out, footprintFinding{f.stack, alias, kindNoBudget,
					"shared_buffers без единицы измерения либо не разобран: " + conf["shared_buffers"]})
			case !workOK:
				out = append(out, footprintFinding{f.stack, alias, kindNoBudget,
					"work_mem без единицы измерения либо не разобран: " + conf["work_mem"]})
			case limitOK && shared+int64(conns)*work > limitMem:
				out = append(out, footprintFinding{f.stack, alias, kindOverspend, fmt.Sprintf(
					"shared_buffers %s + max_connections %d × work_mem %s = %s, "+
						"а limits.memory = %s",
					humanBytes(shared), conns, humanBytes(work),
					humanBytes(shared+int64(conns)*work), humanBytes(limitMem))})
			}
		}
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// САМА ПРОВЕРКА.

func TestProductionStacksDeclareTheirDatabaseFootprint(t *testing.T) {
	facts := allDBFootprintFacts(t)

	// Проверка СВОЕЙ предпосылки. «Ноль находок» обязано быть отличимо от «ноль
	// прочитанного»: обход, переставший узнавать стеки, инстансы Postgres или
	// боевую посадку, объявит дерево чистым, ничего не осмотрев.
	var prodStacks, pairs, ourPairs int
	for _, f := range facts {
		if !f.production {
			continue
		}
		prodStacks++
		pairs += len(f.aliases)
		ourPairs += len(f.ours)
	}
	if len(facts) == 0 || prodStacks == 0 || pairs == 0 || ourPairs == 0 {
		t.Fatalf("обход ничего не прочитал: стеков=%d, из них боевых=%d, связок стек→база=%d, "+
			"из них баз наших служб=%d — предикат перестал узнавать дерево, "+
			"а не дерево стало чистым", len(facts), prodStacks, pairs, ourPairs)
	}
	for _, f := range facts {
		t.Logf("стек %-12s боевой=%-5v (%s), инстансов Postgres=%d, из них наших служб=%d",
			f.stack, f.production, f.prodWhy, len(f.aliases), len(f.ours))
	}
	t.Logf("осмотрено: боевых стеков=%d, связок стек→база=%d (потолок), из них баз наших служб=%d (трата)",
		prodStacks, pairs, ourPairs)

	found := scanDBFootprint(facts)
	var live, excused int
	for _, f := range found {
		if why, known := knownUndeclaredFootprint[f.key()]; known {
			excused++
			t.Logf("известный пропуск %s: %s — %s", f.key(), f.why, why)
			continue
		}
		live++
		switch f.kind {
		case kindNoCeiling:
			t.Errorf("%s — %s. Умолчание подчарта НЕПУСТО (requests 250m/256Mi, limits ПУСТЫЕ), "+
				"поэтому «не объявлено» здесь означает не «ноль», а «за нас выбрал чужой чарт»: "+
				"без предела памяти база вправе унести узел, а с запросом 256Mi её поставят туда, "+
				"где памяти уже нет. Объяви primary.resources в профиле стека",
				f.key(), f.why)
		case kindNoBudget:
			t.Errorf("%s — %s. Тогда действуют умолчания СБОРКИ PostgreSQL "+
				"(max_connections 100, shared_buffers 128MB, work_mem 4MB) — их никто не выбирал "+
				"под этот стенд, и замер, снятый на другом, к нему не относится. "+
				"Объяви primary.extendedConfiguration в профиле стека "+
				"(и повтори в нём autovacuum_naptime: скаляр замещается целиком)",
				f.key(), f.why)
		case kindOverspend:
			t.Errorf("%s — %s. Две величины назначены не глядя друг на друга: "+
				"объявленный потолок не вмещает даже объявленную трату (оценка ВЕРХНЯЯ и грубая — "+
				"work_mem тратится на узел сортировки, а не на соединение). "+
				"Сведи одно с другим: подними limits.memory либо опусти max_connections/work_mem/shared_buffers",
				f.key(), f.why)
		}
	}
	t.Logf("находок всего=%d, из них действующих=%d, покрытых ведомостью=%d (записей в ведомости=%d)",
		len(found), live, excused, len(knownUndeclaredFootprint))
}

// Запись ведомости обязана иметь предмет. Запись, которой больше нечего
// исключать, — находка: её унаследует следующая слепая зона.
func TestFootprintLedgerEntriesStillHaveASubject(t *testing.T) {
	live := map[string]bool{}
	for _, f := range scanDBFootprint(allDBFootprintFacts(t)) {
		live[f.key()] = true
	}
	for key := range knownUndeclaredFootprint {
		if !live[key] {
			t.Errorf("записи %q в ведомости больше НЕЧЕГО исключать: связка объявляет свою посадку "+
				"либо исчезла из дерева. Снимите запись — исключение, пережившее свой предмет, "+
				"читается как действующее ограничение (%s)", key, knownUndeclaredFootprint[key])
		}
	}
	t.Logf("записей в ведомости=%d, действующих пропусков=%d", len(knownUndeclaredFootprint), len(live))
}

// ─────────────────────────────────────────────────────────────────────────────
// САМОПРОВЕРКА — инъекция в ОБЕ стороны на синтетическом входе той же формы.
//
// Без положительного контроля отрицание зеленеет на всём сломанном: обход,
// переставший что-либо узнавать, «не находит» ровно так же, как чистое дерево.
// Поэтому у каждого внесённого дефекта здесь стоит ЗАКОННЫЙ БЛИЗНЕЦ той же
// формы, на котором проверка обязана молчать.

// fpFacts — синтетический стек той же формы, что читается из дерева.
func fpFacts(resources map[string]any, extConf string) []dbFootprintFacts {
	primary := map[string]any{}
	if resources != nil {
		primary["resources"] = resources
	}
	if extConf != "" {
		primary["extendedConfiguration"] = extConf
	}
	return []dbFootprintFacts{{
		stack: "injected", production: true, prodWhy: "синтетика",
		aliases:  []string{"pg-iam"},
		ours:     map[string]string{"pg-iam": "kaname"},
		declared: map[string]any{"pg-iam": map[string]any{"primary": primary}},
	}}
}

// fpFullCeiling — потолок той же формы, что объявляют профили дерева.
func fpFullCeiling(limitMem string) map[string]any {
	return map[string]any{
		"requests": map[string]any{"cpu": "500m", "memory": "1Gi"},
		"limits":   map[string]any{"cpu": "3000m", "memory": limitMem},
	}
}

const fpFullBudget = "max_connections = 200\nshared_buffers = 512MB\nwork_mem = 8MB\nautovacuum_naptime = 5s\n"

func TestScanDBFootprint_InjectionBothWays(t *testing.T) {
	only := func(t *testing.T, got []footprintFinding, wantKind string) footprintFinding {
		t.Helper()
		if len(got) != 1 {
			t.Fatalf("ждали ровно одну находку вида %q, получили %d: %+v", wantKind, len(got), got)
		}
		if got[0].kind != wantKind {
			t.Fatalf("вид находки %q, ждали %q (%+v)", got[0].kind, wantKind, got[0])
		}
		if got[0].alias != "pg-iam" || got[0].stack != "injected" {
			t.Fatalf("находка без координаты: %+v", got[0])
		}
		return got[0]
	}

	// ── (а) ДЕФЕКТ: потолок не объявлен вовсе — ровно то состояние prod до #756.
	f := only(t, scanDBFootprint(fpFacts(nil, fpFullBudget)), kindNoCeiling)
	if !strings.Contains(f.why, "resources") {
		t.Fatalf("находка не называет ручку: %+v", f)
	}

	// ── (б) ДЕФЕКТ: потолок объявлен, но БЕЗ предела памяти — умолчание подчарта
	//        ровно таково (limits пустые), и это худший из случаев.
	f = only(t, scanDBFootprint(fpFacts(map[string]any{
		"requests": map[string]any{"cpu": "500m", "memory": "1Gi"},
	}, fpFullBudget)), kindNoCeiling)
	if !strings.Contains(f.why, "limits.memory") {
		t.Fatalf("пропуск предела памяти не назван: %+v", f)
	}

	// ── (в) ДЕФЕКТ: трата не объявлена.
	f = only(t, scanDBFootprint(fpFacts(fpFullCeiling("4Gi"), "")), kindNoBudget)
	for _, knob := range pgBudgetKnobs {
		if !strings.Contains(f.why, knob) {
			t.Fatalf("находка не назвала ручку %s: %+v", knob, f)
		}
	}

	// ── (г) ДЕФЕКТ: трата объявлена НЕ ПОЛНОСТЬЮ — одной ручки нет.
	//        Утверждается не только имя ручки, но и ВИД сообщения: «не объявлено»
	//        обязано быть отличимо от «объявлено, но не разобрано». Без этого
	//        разбора обе ветки несут слово work_mem, и проверка зеленела бы на
	//        второй, думая, что поймала первую.
	f = only(t, scanDBFootprint(fpFacts(fpFullCeiling("4Gi"),
		"max_connections = 200\nshared_buffers = 512MB\n")), kindNoBudget)
	if !strings.Contains(f.why, "не объявлено профилем") || !strings.Contains(f.why, "work_mem") ||
		strings.Contains(f.why, "shared_buffers") {
		t.Fatalf("находка обязана назвать ИМЕННО недостающую ручку как НЕОБЪЯВЛЕННУЮ: %+v", f)
	}

	// ── (д) ДЕФЕКТ: величина без единицы измерения. Голое число в
	//        postgresql.conf значит разное у разных ручек (блоки 8kB против kB),
	//        поэтому принять его молча — значит посчитать не то, что настроено.
	f = only(t, scanDBFootprint(fpFacts(fpFullCeiling("4Gi"),
		"max_connections = 200\nshared_buffers = 65536\nwork_mem = 8MB\n")), kindNoBudget)
	if !strings.Contains(f.why, "единицы измерения") {
		t.Fatalf("голое число не поймано: %+v", f)
	}

	// ── (е) ДЕФЕКТ: арифметика не сходится — дословно довод values.a8f60d.yaml.
	f = only(t, scanDBFootprint(fpFacts(fpFullCeiling("1Gi"), fpFullBudget)), kindOverspend)
	for _, want := range []string{"1024MiB", "2112MiB"} {
		if !strings.Contains(f.why, want) {
			t.Fatalf("находка не называет обе стороны неравенства (нет %s): %+v", want, f)
		}
	}

	// ── (ж) ЗАКОННЫЙ БЛИЗНЕЦ: и потолок, и трата объявлены и сходятся. Молчит.
	if got := scanDBFootprint(fpFacts(fpFullCeiling("4Gi"), fpFullBudget)); len(got) != 0 {
		t.Fatalf("законное объявление покрашено: %+v", got)
	}

	// ── (з) ЗАКОННЫЙ БЛИЗНЕЦ: ровно на границе — сумма РАВНА потолку. Молчит:
	//        неравенство нестрогое, иначе точный расчёт читался бы как ошибка.
	if got := scanDBFootprint(fpFacts(fpFullCeiling("2112Mi"), fpFullBudget)); len(got) != 0 {
		t.Fatalf("сумма, равная потолку, покрашена: %+v", got)
	}

	// ── (и) ЗАКОННЫЙ БЛИЗНЕЦ: чужая база (не наших служб) траты не объявляет —
	//        её настраивает свой чарт. Потолок с неё по-прежнему спрашивается,
	//        поэтому близнец несёт его.
	foreign := fpFacts(fpFullCeiling("4Gi"), "")
	foreign[0].ours = map[string]string{}
	if got := scanDBFootprint(foreign); len(got) != 0 {
		t.Fatalf("чужое хранилище покрашено за необъявленную трату: %+v", got)
	}

	// ── (к) ЗАКОННЫЙ БЛИЗНЕЦ: стенд разработки. Не обязан выбирать ничего.
	devStand := fpFacts(nil, "")
	devStand[0].production = false
	if got := scanDBFootprint(devStand); len(got) != 0 {
		t.Fatalf("стенд разработки покрашен: %+v", got)
	}

	// ── (к2) ДЕФЕКТ: стенд разработки НАЗВАЛ обе величины, и они не сходятся.
	//         Прежняя редакция отсекала небоевой стек целиком, поэтому такой
	//         случай был невидим — а именно он и наблюдался вживую (#883):
	//         база прав падала с кодом 137 под нагрузкой, и замеры пропускной
	//         способности, снятые на этом стенде, были недействительны.
	//
	//         «Волен не выбирать» и «выбрал невозможное» — разные состояния;
	//         послабление относится к первому и не покрывает второе.
	devOverspend := fpFacts(fpFullCeiling("1Gi"), fpFullBudget)
	devOverspend[0].production = false
	if got := only(t, scanDBFootprint(devOverspend), kindOverspend); !strings.Contains(got.why, "1024MiB") {
		t.Fatalf("находка не называет объявленный потолок: %+v", got)
	}

	// ── (к3) ЗАКОННЫЙ БЛИЗНЕЦ: тот же небоевой стек, но величины СХОДЯТСЯ.
	//         Без этой пробы (к2) зеленела бы и на проверке, красящей всякий
	//         небоевой стек, назвавший хоть что-нибудь.
	devFits := fpFacts(fpFullCeiling("4Gi"), fpFullBudget)
	devFits[0].production = false
	if got := scanDBFootprint(devFits); len(got) != 0 {
		t.Fatalf("небоевой стек со сходящимся бюджетом покрашен: %+v", got)
	}

	// ── (к4) ЗАКОННЫЙ БЛИЗНЕЦ: небоевой стек назвал ПОТОЛОК, но не назвал
	//         ручек траты. Требование ОБЪЯВИТЬ адресовано только боевым, и
	//         это остаётся в силе: спрашивать нечего, находки быть не должно.
	devCeilingOnly := fpFacts(fpFullCeiling("1Gi"), "")
	devCeilingOnly[0].production = false
	if got := scanDBFootprint(devCeilingOnly); len(got) != 0 {
		t.Fatalf("небоевой стек покрашен за НЕобъявленную трату: %+v", got)
	}

	// ── (к5) КОНТРОЛЬ В ОБРАТНУЮ СТОРОНУ: тот же вход, но стек БОЕВОЙ —
	//         обязан покраснеть за необъявленную трату. Без него (к4) была бы
	//         неотличима от проверки, разучившейся требовать объявление вовсе.
	prodCeilingOnly := fpFacts(fpFullCeiling("1Gi"), "")
	if got := only(t, scanDBFootprint(prodCeilingOnly), kindNoBudget); got.stack != "injected" {
		t.Fatalf("боевой стек без траты не покрашен как надо: %+v", got)
	}

	// ── (м) ДЕФЕКТ: ручка стоит ТОЛЬКО в комментарии. Разбор обязан читать
	//        исполняемую часть, а не текст: закомментированная строка — это
	//        объяснение ручки, а не её объявление, и ровно так же выглядит
	//        строка, которую кто-то временно выключил.
	//
	//        Строка подобрана так, чтобы БЕЗ отсечения комментария она разбиралась
	//        УСПЕШНО (`# work_mem = 8MB` без пояснительного хвоста). Иначе разбор,
	//        не умеющий отсекать `#`, дал бы находку «не разобрано» — и проверка
	//        зеленела бы, поймав не тот дефект. Проверено мутацией: замена
	//        отсечения комментария на TrimLeft("# ") обязана краснить этот случай.
	commented := scanDBFootprint(fpFacts(fpFullCeiling("4Gi"),
		"max_connections = 200\nshared_buffers = 512MB\n# work_mem = 8MB\n"))
	f = only(t, commented, kindNoBudget)
	if !strings.Contains(f.why, "не объявлено профилем") || !strings.Contains(f.why, "work_mem") {
		t.Fatalf("ручка, стоящая только в комментарии, зачтена как объявленная: %+v", f)
	}
}

// Разбор величин обязан узнавать ОБЕ единицы — Kubernetes и postgresql.conf.
func TestFootprintQuantityParsers(t *testing.T) {
	for _, c := range []struct {
		in   string
		want int64
		ok   bool
	}{
		{"256Mi", 256 << 20, true}, {"1Gi", 1 << 30, true}, {"4Gi", 4 << 30, true},
		{"512M", 512e6, true}, {"1024", 1024, true}, {"", 0, false}, {"1Gb", 0, false},
	} {
		got, ok := parseK8sQuantity(c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("parseK8sQuantity(%q) = %d,%v; ждали %d,%v", c.in, got, ok, c.want, c.ok)
		}
	}
	for _, c := range []struct {
		in   string
		want int64
		ok   bool
	}{
		{"128MB", 128 << 20, true}, {"8MB", 8 << 20, true}, {"2GB", 2 << 30, true},
		{"1024kB", 1 << 20, true},
		// Голое число единицы НЕ несёт: у shared_buffers это блоки по 8kB, у
		// work_mem — килобайты. Разбирать его значило бы гадать.
		{"65536", 0, false}, {"128 mb", 0, false}, {"", 0, false},
	} {
		got, ok := parsePGMemory(c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("parsePGMemory(%q) = %d,%v; ждали %d,%v", c.in, got, ok, c.want, c.ok)
		}
	}
}

// Предикаты обязаны узнавать НАСТОЯЩЕЕ дерево, а не только синтетику: иначе
// самопроверка выше зелёная, а обход читает ноль. Проверяются ровно те факты,
// на которых стоит вся проверка, и каждый — на дереве.
func TestFootprintPredicates_RecogniseTheRealTree(t *testing.T) {
	facts := allDBFootprintFacts(t)
	byName := map[string]dbFootprintFacts{}
	for _, f := range facts {
		byName[f.stack] = f
	}

	// (1) Боевая посадка узнаётся, и стенд разработки под неё НЕ подводится.
	if f, ok := byName["prod"]; !ok || !f.production {
		t.Fatalf("стек prod не признан боевым: %+v", f)
	}
	if f, ok := byName["dev"]; !ok || f.production {
		t.Fatalf("стек dev признан боевым — предикат подводит стенд разработки под правило: %+v", f)
	}

	// (2) Инстансы Postgres узнаются все, а не «те, что нашлись».
	want := pgAliases(t)
	if len(want) == 0 {
		t.Fatal("в Chart.yaml не нашлось ни одного алиаса pg-* — предикат перестал их узнавать")
	}
	got := byName["prod"].aliases
	if len(got) != len(want) {
		t.Fatalf("инстансов Postgres у стека prod=%d, а зависимостей pg-* в Chart.yaml=%d: %v против %v",
			len(got), len(want), got, want)
	}

	// (3) «Наши» базы отделены от чужих хранилищ по db.host, а не по списку имён.
	ours := byName["prod"].ours
	for _, a := range []string{"pg-iam", "pg-vpc", "pg-compute", "pg-geo", "pg-nlb", "pg-registry", "pg-storage"} {
		if _, ok := ours[a]; !ok {
			t.Errorf("база %s не признана базой нашей службы — признак db.host перестал её узнавать", a)
		}
	}
	for _, a := range []string{"pg-hydra", "pg-kratos", "pg-openfga"} {
		if sub, ok := ours[a]; ok {
			t.Errorf("чужое хранилище %s признано нашим (через %s) — тогда проверка начнёт "+
				"требовать настройки от чарта, который мы не сопровождаем", a, sub)
		}
	}

	// (4) Объявленное дерево — ТОЛЬКО профили стека. Умолчание умбреллы
	//     (autovacuum_naptime у pg-iam в values.yaml) сюда попадать НЕ должно:
	//     иначе «объявлено» и «унаследовано» станут неразличимы, а это предмет.
	if v, ok := lookup(byName["prod"].declared, "pg-iam", "primary", "extendedConfiguration"); ok {
		if strings.Contains(fmt.Sprint(v), "autovacuum_naptime") &&
			!strings.Contains(fmt.Sprint(v), "max_connections") {
			t.Errorf("в объявленное дерево стека просочилось умолчание умбреллы: %q", v)
		}
	}
	t.Logf("осмотрено: стеков=%d, инстансов Postgres у prod=%d, из них баз наших служб=%d",
		len(facts), len(got), len(ours))
}
