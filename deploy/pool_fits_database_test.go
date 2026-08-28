// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy_test

// pool_fits_database_test.go — объявленная посадка не вправе обещать базе больше
// соединений, чем та принимает.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ЭТО ПРОВЕРЯЕТСЯ ПО ОБЪЯВЛЕНИЮ, А НЕ ТОЛЬКО ЗАГРУЗОЧНЫМ СТРАЖЕМ
//
// Величины назначены в РАЗНЫХ файлах и ни одна не знает о другой: пул объявлен на
// ОДНУ реплику в значениях службы, потолок числа реплик — в её же блоке
// автомасштабирования, предел базы — в свободном тексте настроек СУБД в профиле
// умбреллы. Их произведение не записано нигде, поэтому расхождение не видно ни в
// одном файле по отдельности, и обзор изменения его не показывает.
//
// СЛАГАЕМЫХ В ПРОИЗВЕДЕНИИ ДВА, А НЕ ОДНО (kacho#1384). Кроме пула, реплика
// держит соединения ВНЕ его: поток подписки, дренаж очереди, пробуждение
// реконсиляции. Пул о них не знает и вычесть их из своей ширины не может.
// Слагаемое выводится и проверяется на полноту в pool_out_of_pool_test.go; здесь
// оно только складывается — второго изложения одного предмета не заводится.
//
// Загрузочный страж (`pkg/db.ConnBudget`) ловит то же расхождение, но ПОЗЖЕ —
// когда посадка уже раскатана и под отказывается стартовать. Здесь оно ловится ДО
// развёртывания и сразу на всех стеках, включая те, куда рука не доходит.
//
// НАПАРНИКОМ ЕГО СЧИТАТЬ НЕЛЬЗЯ: страж провязан в ОДНОЙ службе из семи. Предикат:
// `grep -rln 'ConnBudget' --include=*.go services | grep -v _test | cut -d/ -f2 |
// sort -u` → `iam`. У остальных шести второго рубежа нет вовсе, и эта проверка у
// них ЕДИНСТВЕННАЯ. Прежняя редакция этой шапки обещала напарника всем — обещание
// защиты, которой нет, и есть тот класс, из-за которого комментарий рядом с
// механизмом бывает опаснее его отсутствия.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОТОЛОК РЕПЛИК — ЭТО `autoscaling.maxReplicas`, А НЕ `replicas` (#709)
//
// `replicas` — это СТАРТОВОЕ число подов, а не потолок: при включённом
// автомасштабировании потолок объявлен рядом, в `autoscaling.maxReplicas`, и
// именно столько подов посадка вправе поднять САМА, без участия человека.
//
// Прежняя редакция этой проверки читала `replicas` и потому судила о посадке из
// одного пода там, где объявлено до десяти. Цена измерена, а не предположена:
// vpc объявлял пул 200 на реплику при потолке автомасштабирования 10 и пределе
// базы 210 — то есть порог переходился уже на ВТОРОМ поде (400 > 207), а проверка
// была зелёной на всех шести стеках дерева. Хуже того, наступает это ровно под
// нагрузкой: масштабирование срабатывает по загрузке процессора, то есть тогда,
// когда соединения уже заняты, и второй под приходит просить их у базы, у которой
// их не осталось.
//
// Когда автомасштабирование ВЫКЛЮЧЕНО, потолком остаётся `replicas`: объявленный
// рядом `maxReplicas` в этом случае ничего не поднимает. Но объявление он не
// теряет — включат его, и произведение пересчитается здесь само.
//
// ─────────────────────────────────────────────────────────────────────────────
// НЕОБЪЯВЛЕННЫЙ ПУЛ — НАХОДКА, А НЕ ПРОПУСК (#709)
//
// Прежняя редакция пропускала связку, у которой ширина пула не объявлена, и
// печатала это в переписи. Пропущенных было 30 из 42 — то есть проверка молчала о
// пяти службах из семи, и молчала она не потому, что у них сходится, а потому,
// что их не спрашивали.
//
// Умолчание драйвера (`max(4, число ядер узла)`) — величина, которую НИКТО не
// выбирал: она зависит от узла, на который попал под, и меняется от смены узла без
// единой правки дерева. Судить о ней отсюда нечем, а значит и объявлять стек
// чистым нельзя. Тот же ход уже сделан в загрузочном страже: там незаявленная
// величина — ОТКАЗ, а не пропуск, потому что ноль в сомножителе обращает
// произведение в ноль и проходит любую проверку.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕГО ЭТА ПРОВЕРКА НЕ ЛОВИТ — НАЗВАНО, А НЕ УМОЛЧАНО
//
//  1. Она судит ОБЪЯВЛЕННУЮ посадку. Реплика, поднятая помимо объявления (правкой
//     живого развёртывания), в объявлении не отражается и здесь не видна by
//     construction. Такую видит только загрузочный страж — а он есть лишь у одной
//     службы из семи (см. шапку выше), значит у остальных её не видит никто.
//  2. Она считает ТОЛЬКО пул. Служба держит соединения и помимо него — миграции на
//     старте, ожидание оповещений, опросчики очередей, — и лишние поды переката
//     (`maxSurge`) тоже приходят со своим пулом. Значит пройденная проверка
//     НЕОБХОДИМА, но не достаточна: это НИЖНЯЯ граница обещанного. Число фоновых
//     соединений здесь не выдумывается — невыдуманного значения у него нет, а
//     вписанное «на глаз» превратило бы проверку в источник ложной точности. Та же
//     граница объявлена и у загрузочного стража, теми же словами и по той же
//     причине.
//  3. Она НЕ судит, верна ли выбранная ширина пула по существу. «Сорок мало» —
//     вопрос замера, а не объявления; здесь проверяется, что объявленное помещается.

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// pgDefaultMaxConnections / pgDefaultSuperuserReserved — умолчания СБОРКИ
// PostgreSQL.
//
// Стоят здесь потому, что предел бывает не объявлен ни одним профилем: чарт базы
// этой настройки не выставляет вовсе, и тогда действует то, с чем собран образ.
// Считать неназванный предел бесконечным значило бы считать самый опасный случай
// самым безопасным — «не объявлено» превратилось бы в «не ограничено».
const (
	pgDefaultMaxConnections    = 100
	pgDefaultSuperuserReserved = 3
)

var maxConnectionsLine = regexp.MustCompile(`(?m)^\s*max_connections\s*=\s*(\d+)`)

// poolPaths — известные формы пути к ширине пула в значениях службы.
//
// Форм несколько потому, что службы раскладывались в разное время; перечень нужен
// затем, чтобы «ширина не найдена» означало ИМЕННО отсутствие объявления, а не
// незнание проверки о четвёртой форме. Служба, ни одной формы не объявившая,
// попадает в находки — молча она не исчезает.
var poolPaths = [][]string{
	{"config", "repository", "postgres", "maxConns"},
	{"repository", "postgres", "maxConns"},
	{"db", "maxConns"},
}

// replicaPaths — известные формы пути к СТАРТОВОМУ числу подов.
var replicaPaths = []string{"replicas", "replicaCount"}

func asInt(v any) (int, bool) {
	switch t := v.(type) {
	case int:
		return t, true
	case int64:
		return int(t), true
	case float64:
		return int(t), true
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(t))
		return n, err == nil
	}
	return 0, false
}

func asBool(v any) (bool, bool) {
	switch t := v.(type) {
	case bool:
		return t, true
	case string:
		b, err := strconv.ParseBool(strings.TrimSpace(t))
		return b, err == nil
	}
	return false, false
}

// valuesWithSubchartDefaults — значения, которые helm получит для этого стека:
// умолчания КАЖДОГО ПОДЧАРТА под умолчаниями умбреллы под профилями цепочки.
//
// Отличается от соседнего `effectiveValues` (managed_cluster_profile_test.go)
// ровно первым слоем, и слой этот здесь обязателен: боевые профили службу не
// переобъявляют вовсе, её ширина пула и потолок реплик живут только в её
// собственном values.yaml. Проверка, читающая одни файлы умбреллы, увидела бы там
// пустоту и объявила бы боевой стек чистым — «ноль находок» вместо «ноль
// прочитанного». Соседняя функция не расширена намеренно: её читатель спрашивает
// про другой предмет, и менять его основание ради этой проверки значило бы
// двигать чужой вердикт.
func valuesWithSubchartDefaults(t *testing.T, chain []string) map[string]any {
	t.Helper()
	out := map[string]any{}
	for alias, dir := range subchartDirs(t) {
		vals := readYAML(t, filepath.Join(dir, "values.yaml"))
		out[alias] = mergeValues(map[string]any{}, vals)
	}
	out = mergeValues(out, readYAML(t, filepath.Join(umbrellaDir, "values.yaml")))
	for _, p := range chain {
		out = mergeValues(out, readYAML(t, filepath.Join(umbrellaDir, p)))
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// ФАКТЫ — то, что прочитано из дерева. Отделены от разбора, чтобы самопроверка
// подавала разбору синтетический вход, а не подделывала дерево.

// poolCeiling — что база готова принять.
type poolCeiling struct {
	maxConnections int
	// declaredByProfile — объявлен ли предел ПРОФИЛЕМ, или приехал из сборки
	// образа. Различие идёт в текст находки: «предел мал» и «предел никто не
	// выбирал» чинятся по-разному.
	declaredByProfile bool
}

// poolLink — одна связка «служба → её база» в одном стеке.
type poolLink struct {
	service string
	pg      string

	// pool — ширина пула на ОДНУ реплику. 0 означает «не объявлена»; путь, по
	// которому она нашлась, идёт в текст находки, чтобы правку не искали.
	pool     int
	poolPath string

	// replicas — стартовое число подов и путь, по которому оно объявлено.
	replicas     int
	replicasPath string

	// hpa* — блок автомасштабирования. Когда он включён, ПОТОЛОК реплик — это
	// hpaMax, а не replicas.
	hpaDeclared bool
	hpaEnabled  bool
	hpaMax      int

	// outOfPool — соединения, которые реплика держит ВНЕ пула: поток подписки,
	// дренаж очереди, пробуждение реконсиляции. Пул о них не знает и вычесть их
	// из своей ширины не может, поэтому это ВТОРОЕ СЛАГАЕМОЕ произведения, а не
	// часть первого. Как оно выводится и чем держится его полнота —
	// pool_out_of_pool_test.go.
	outOfPool int
	// outOfPoolWhy — разбор слагаемого по видам; идёт в текст находки, чтобы
	// правку не искали.
	outOfPoolWhy []string
	// outOfPoolUnknown — вид, чьё слагаемое ВЫВЕСТИ НЕ УДАЛОСЬ. Это находка, а не
	// ноль: неизвестное слагаемое не бывает нулём, и молчание здесь вернуло бы
	// ровно тот дефект, ради которого слагаемое заводится.
	outOfPoolUnknown []string
}

// perReplica — сколько соединений реплика обещает базе: ширина пула плюс всё,
// что она держит вне пула.
func (l poolLink) perReplica() int { return l.pool + l.outOfPool }

// ceiling — сколько подов посадка вправе поднять ОДНОВРЕМЕННО, и откуда это
// известно. ok=false означает «потолок не объявлен» — это находка, а не единица.
func (l poolLink) ceiling() (n int, why string, ok bool) {
	if l.hpaDeclared && l.hpaEnabled {
		if l.hpaMax <= 0 {
			return 0, "autoscaling.enabled=true, но autoscaling.maxReplicas не объявлен", false
		}
		return l.hpaMax, "autoscaling.maxReplicas", true
	}
	if l.replicas <= 0 {
		return 0, "ни autoscaling.maxReplicas, ни " + strings.Join(replicaPaths, "/"), false
	}
	why = l.replicasPath
	if l.hpaDeclared {
		why += " (автомасштабирование объявлено и выключено)"
	}
	return l.replicas, why, true
}

// poolFacts — один стек целиком.
type poolFacts struct {
	stack    string
	ceilings map[string]poolCeiling
	links    []poolLink
}

func poolFactsFor(t *testing.T, name string, chain []string, tree *outOfPoolTree) poolFacts {
	t.Helper()
	vals := valuesWithSubchartDefaults(t, chain)

	f := poolFacts{stack: name, ceilings: map[string]poolCeiling{}}

	for alias, node := range vals {
		if !strings.HasPrefix(alias, "pg-") {
			continue
		}
		sub, _ := node.(map[string]any)
		if sub == nil {
			continue
		}
		c := poolCeiling{maxConnections: pgDefaultMaxConnections}
		if raw, ok := lookup(sub, "primary", "extendedConfiguration"); ok {
			if m := maxConnectionsLine.FindStringSubmatch(fmt.Sprint(raw)); m != nil {
				n, _ := strconv.Atoi(m[1])
				c.maxConnections, c.declaredByProfile = n, true
			}
		}
		f.ceilings[alias] = c
	}
	if len(f.ceilings) == 0 {
		t.Fatalf("стек %q: не нашлось ни одной базы (ключи pg-*) — предикат перестал их "+
			"узнавать, а не базы исчезли", name)
	}

	for alias, node := range vals {
		sub, _ := node.(map[string]any)
		if sub == nil || strings.HasPrefix(alias, "pg-") {
			continue
		}
		host, ok := lookup(sub, "db", "host")
		if !ok {
			continue // подчарт без своей базы — не наш предмет
		}
		var pgAlias string
		for pg := range f.ceilings {
			if strings.HasSuffix(fmt.Sprint(host), pg) {
				pgAlias = pg
				break
			}
		}
		if pgAlias == "" {
			continue
		}

		l := poolLink{service: alias, pg: pgAlias}
		for _, p := range poolPaths {
			if raw, ok := lookup(sub, p...); ok {
				if n, ok := asInt(raw); ok {
					l.pool, l.poolPath = n, strings.Join(p, ".")
					break
				}
			}
		}
		for _, k := range replicaPaths {
			if raw, ok := lookup(sub, k); ok {
				if n, ok := asInt(raw); ok {
					l.replicas, l.replicasPath = n, k
				}
			}
		}
		if raw, ok := lookup(sub, "autoscaling", "enabled"); ok {
			if b, ok := asBool(raw); ok {
				l.hpaDeclared, l.hpaEnabled = true, b
			}
		}
		if raw, ok := lookup(sub, "autoscaling", "maxReplicas"); ok {
			if n, ok := asInt(raw); ok {
				l.hpaMax = n
			}
		}
		l.outOfPool, l.outOfPoolWhy, l.outOfPoolUnknown = outOfPoolPerReplica(t, alias, tree)
		f.links = append(f.links, l)
	}
	sort.Slice(f.links, func(i, j int) bool { return f.links[i].service < f.links[j].service })
	return f
}

func allPoolFacts(t *testing.T, tree *outOfPoolTree) []poolFacts {
	t.Helper()
	chains := deployStacks(t)
	names := make([]string, 0, len(chains))
	for n := range chains {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]poolFacts, 0, len(names))
	for _, n := range names {
		out = append(out, poolFactsFor(t, n, chains[n], tree))
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// РАЗБОР — чистая функция над фактами.

type poolFinding struct {
	stack string
	// subject — служба (для находок про объявление) либо база (для находки про
	// сумму обещанного). Оба вида идут в один ключ ведомости, поэтому вид входит
	// в ключ: иначе запись, заведённая под «ширина не объявлена», молча накрыла
	// бы и превышение потолка.
	subject string
	kind    string
	why     string
}

func (f poolFinding) key() string { return f.stack + "/" + f.subject + "/" + f.kind }

const (
	kindPoolUndeclared    = "ширина пула не объявлена"
	kindCeilingUndeclared = "потолок реплик не объявлен"
	kindOverPromise       = "обещано больше принимаемого"
	kindOutOfPoolUnknown  = "слагаемое вне пула не выведено"
)

// knownUnfitting — связки, чьё несоответствие ЭТОТ гейт нашёл и которые заведены
// СВОИМ предметом.
//
// Перечень существует не затем, чтобы отвернуться от находки, а затем, чтобы
// чужой предмет чинился своим изменением: смешение линий делает вердикт
// непрослеживаемым, а перепись — недоказуемой.
//
// Запись САМОИСТЕКАЕТ: как только связка начинает помещаться, ей больше нечего
// исключать — и гейт падает на самой записи. Иначе исключение пережило бы свой
// предмет, а следующий читатель принял бы его за действующее ограничение.
//
// ВЕДОМОСТЬ ПУСТА до тех пор, пока гейт не нашёл несоответствия. Сегодня она
// несёт ОДИН предмет, размноженный по стекам: пять записей — это пять профилей,
// наследующих одни и те же величины базы, а не пять разных дефектов.
var knownUnfitting = map[string]string{
	"a8f60d/pg-compute/" + kindOverPromise:      overPromiseComputeWhy,
	"dev-prod/pg-compute/" + kindOverPromise:    overPromiseComputeWhy,
	"fe3455/pg-compute/" + kindOverPromise:      overPromiseComputeWhy,
	"prod/pg-compute/" + kindOverPromise:        overPromiseComputeWhy,
	"prorobotech/pg-compute/" + kindOverPromise: overPromiseComputeWhy,
}

// overPromiseComputeWhy — причина и предикат снятия одной записи.
//
// Находка НАСТОЯЩАЯ и появилась ровно тогда, когда арифметика стала полной
// (kacho#1384: слагаемых было одно, а мест, держащих соединение, несколько).
// Чинится она ВЕЛИЧИНАМИ — предел базы, потолок потоков, ширина пула, потолок
// подов, — и выбирать между ними должен владелец домена по замеру спроса, а не
// автор гейта по тому, что дешевле правится. Поэтому предмет заведён своим
// изменением, а не закрыт здесь же: смешение линий делает вердикт
// непрослеживаемым.
const overPromiseComputeWhy = "kacho#1451 — 15 (пул) + 16 (потолок потоков подписки) + " +
	"1 (дренаж) = 32 на реплику × 5 подов = 160 при принимаемых 97; " +
	"предикат снятия: связка помещается, и запись самоистекает"

func scanPoolFits(facts []poolFacts) (findings []poolFinding, examined int, lines []string) {
	for _, f := range facts {
		promised := map[string]int{}
		breakdown := map[string][]string{}

		for _, l := range f.links {
			ceiling, ceilWhy, ceilOK := l.ceiling()

			if l.pool <= 0 {
				where := "ни по одному из известных путей (" + pathsList() + ")"
				if l.poolPath != "" {
					where = l.poolPath + " = " + strconv.Itoa(l.pool)
				}
				findings = append(findings, poolFinding{
					stack: f.stack, subject: l.service, kind: kindPoolUndeclared,
					why: fmt.Sprintf("%s: ширина пула не объявлена (%s) — действует умолчание "+
						"драйвера max(4, число ядер УЗЛА), которое никто не выбирал и которое "+
						"меняется от смены узла без единой правки дерева. Ноль в сомножителе "+
						"обращает произведение «пул × реплики» в ноль и проходит любую проверку, "+
						"поэтому «не объявлено» здесь — находка, а не пропуск",
						l.service, where),
				})
				continue
			}
			if len(l.outOfPoolUnknown) > 0 {
				findings = append(findings, poolFinding{
					stack: f.stack, subject: l.service, kind: kindOutOfPoolUnknown,
					why: fmt.Sprintf("%s: %s. Слагаемое вне пула не выведено, а неизвестное "+
						"слагаемое не бывает нулём: ноль в сумме проходит любую проверку и "+
						"обращает её в форму без содержания",
						l.service, strings.Join(l.outOfPoolUnknown, "; ")),
				})
				continue
			}
			if !ceilOK {
				findings = append(findings, poolFinding{
					stack: f.stack, subject: l.service, kind: kindCeilingUndeclared,
					why: fmt.Sprintf("%s: потолок числа подов не объявлен — %s. Без него "+
						"неизвестно, на сколько умножать ширину пула", l.service, ceilWhy),
				})
				continue
			}

			examined++
			promised[l.pg] += l.perReplica() * ceiling
			outside := ""
			if l.outOfPool > 0 {
				outside = fmt.Sprintf(" + %d вне пула [%s]", l.outOfPool, strings.Join(l.outOfPoolWhy, "; "))
			}
			breakdown[l.pg] = append(breakdown[l.pg],
				fmt.Sprintf("%s: (%d (%s)%s) × %d (%s) = %d",
					l.service, l.pool, l.poolPath, outside, ceiling, ceilWhy, l.perReplica()*ceiling))
			lines = append(lines, fmt.Sprintf("стек %s: %s → %s, на реплику %d = пул %d (%s)%s, "+
				"× %d подов (потолок из %s) = %d", f.stack, l.service, l.pg, l.perReplica(),
				l.pool, l.poolPath, outside, ceiling, ceilWhy, l.perReplica()*ceiling))
		}

		pgs := make([]string, 0, len(promised))
		for pg := range promised {
			pgs = append(pgs, pg)
		}
		sort.Strings(pgs)
		for _, pg := range pgs {
			total := promised[pg]
			c := f.ceilings[pg]
			available := c.maxConnections - pgDefaultSuperuserReserved
			if total <= available {
				continue
			}
			note := "объявлен профилем"
			if !c.declaredByProfile {
				note = "НЕ ОБЪЯВЛЕН ни одним профилем — действует умолчание сборки образа, " +
					"то есть предел никто не выбирал"
			}
			findings = append(findings, poolFinding{
				stack: f.stack, subject: pg, kind: kindOverPromise,
				why: fmt.Sprintf("базе %s обещают %d соединений, а она принимает %d "+
					"(max_connections %d, %s; запас суперпользователя %d). Слагаемые: %s. "+
					"Обещанное сверх принимаемого не «иногда медленнее» — это отказ в "+
					"подключении ровно тогда, когда соединения понадобились, и при "+
					"автомасштабировании наступает он ПОД НАГРУЗКОЙ: поды приходят сами",
					pg, total, available, c.maxConnections, note, pgDefaultSuperuserReserved,
					strings.Join(breakdown[pg], "; ")),
			})
		}
	}
	sort.Slice(findings, func(i, j int) bool { return findings[i].key() < findings[j].key() })
	return findings, examined, lines
}

func pathsList() string {
	var out []string
	for _, p := range poolPaths {
		out = append(out, strings.Join(p, "."))
	}
	return strings.Join(out, " | ")
}

// applyLedger разводит находки на те, что обязаны уронить прогон, и те, что
// прощены ведомостью; сверх того возвращает записи ведомости, которым больше
// нечего исключать.
//
// Вынесена из тела проверки НАМЕРЕННО. Ведомость сегодня пуста — и это её
// нормальное состояние, — поэтому в теле проверки обе её ветки не исполняются
// НИ РАЗУ: и прощение, и самоистечение остались бы кодом, о котором нельзя
// сказать, работает ли он. Гейт, чей предмет — отсутствие, зеленеет легче
// всех, поэтому рядом с ним стоит положительная проба
// (TestPoolGateLedgerForgivesItsSubjectAndExpiresWithoutOne).
func applyLedger(findings []poolFinding, ledger map[string]string) (fail, forgiven, stale []string) {
	live := map[string]bool{}
	for _, f := range findings {
		if why, known := ledger[f.key()]; known {
			live[f.key()] = true
			forgiven = append(forgiven, fmt.Sprintf("известное несоответствие %s: %s — %s",
				f.key(), f.why, why))
			continue
		}
		fail = append(fail, fmt.Sprintf("стек %q: %s", f.stack, f.why))
	}
	keys := make([]string, 0, len(ledger))
	for k := range ledger {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if live[key] {
			continue
		}
		stale = append(stale, fmt.Sprintf("записи %q в ведомости известных несоответствий "+
			"больше НЕЧЕГО исключать: находки с таким ключом нет. Снимите запись — "+
			"исключение, пережившее свой предмет, читается как действующее ограничение (%s)",
			key, ledger[key]))
	}
	return fail, forgiven, stale
}

// ─────────────────────────────────────────────────────────────────────────────

func TestDeclaredPoolFitsTheDatabaseItConnectsTo(t *testing.T) {
	tree := readOutOfPoolTree(t)
	facts := allPoolFacts(t, tree)
	findings, examined, lines := scanPoolFits(facts)
	findings = append(findings, unattributedCaptures(t, tree)...)
	sort.Slice(findings, func(i, j int) bool { return findings[i].key() < findings[j].key() })

	for _, l := range lines {
		t.Log(l)
	}

	byKind := map[string]int{}
	for _, f := range findings {
		byKind[f.kind]++
	}

	fail, forgiven, stale := applyLedger(findings, knownUnfitting)
	for _, l := range forgiven {
		t.Log(l)
	}
	for _, l := range fail {
		t.Error(l)
	}
	for _, l := range stale {
		t.Error(l)
	}

	// ПЕРЕПИСЬ: «ноль находок» обязано быть отличимо от «ноль прочитанного».
	//
	// Захваты вне пула печатаются ДВУМЯ числами — сколько их и сколько из них
	// НЕ ПРИПИСАНО. Одного первого мало ровно там, где проверка и слепа:
	// разбор, переставший узнавать захват, даёт то же «неучтённых 0», что и
	// полная арифметика.
	t.Logf("осмотрено: стеков %d, связок служба→база с объявленными пулом и потолком %d, "+
		"файлов прод-кода Go %d, захватов соединения вне пула %d (не приписано %d), "+
		"находок %d (%s %d, %s %d, %s %d, %s %d), записей в ведомости %d",
		len(facts), examined, tree.files, len(tree.captures), byKind[kindOutOfPoolUnattributed],
		len(findings),
		kindPoolUndeclared, byKind[kindPoolUndeclared],
		kindCeilingUndeclared, byKind[kindCeilingUndeclared],
		kindOverPromise, byKind[kindOverPromise],
		kindOutOfPoolUnknown, byKind[kindOutOfPoolUnknown],
		len(knownUnfitting))
	if examined == 0 {
		t.Fatal("ни одной связки служба→база не осмотрено — проверка ничего не утверждает, " +
			"хотя выглядит зелёной")
	}
	if tree.files == 0 || len(tree.captures) == 0 {
		t.Fatalf("файлов прод-кода %d, захватов вне пула %d — обход пуст: «слагаемых вне пула нет» "+
			"стало неотличимо от «дерево не прочитано»", tree.files, len(tree.captures))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// САМОПРОВЕРКА — инъекция в ОБЕ стороны на синтетическом входе той же формы.
//
// Без положительного контроля отрицание зеленеет на всём сломанном: разбор,
// переставший что-либо узнавать, «не находит» ровно так же, как сходящееся
// дерево. Поэтому у каждого внесённого дефекта здесь стоит ЗАКОННЫЙ БЛИЗНЕЦ той
// же формы, на котором проверка обязана молчать.

// pfStack — синтетический стек той же формы, что читается из дерева: одна база с
// объявленным пределом и одна служба, к ней подключённая.
func pfStack(name string, maxConns int, l poolLink) poolFacts {
	l.pg = "pg-synthetic"
	return poolFacts{
		stack:    name,
		ceilings: map[string]poolCeiling{"pg-synthetic": {maxConnections: maxConns, declaredByProfile: true}},
		links:    []poolLink{l},
	}
}

func kindsOf(findings []poolFinding) map[string]int {
	out := map[string]int{}
	for _, f := range findings {
		out[f.kind]++
	}
	return out
}

func TestPoolGateReadsTheAutoscalingCeilingNotTheStartingCount(t *testing.T) {
	// ДЕФЕКТ: ровно тот, что дал #709 — пул помещается при одном поде и не
	// помещается при потолке автомасштабирования.
	broken := pfStack("injected", 210, poolLink{
		service: "vpc", pool: 200, poolPath: "repository.postgres.maxConns",
		replicas: 1, replicasPath: "replicas",
		hpaDeclared: true, hpaEnabled: true, hpaMax: 10,
	})
	got, examined, _ := scanPoolFits([]poolFacts{broken})
	if kindsOf(got)[kindOverPromise] != 1 {
		t.Fatalf("гейт не увидел превышения при включённом автомасштабировании: находки %+v", got)
	}
	if examined != 1 {
		t.Fatalf("осмотренных связок %d, ожидалась 1 — разбор перестал считать", examined)
	}
	if !strings.Contains(got[0].why, "pg-synthetic") || !strings.Contains(got[0].why, "vpc") {
		t.Fatalf("находка не называет ни базу, ни службу: %q", got[0].why)
	}

	// ЗАКОННЫЙ БЛИЗНЕЦ ПЕРВЫЙ: та же форма, но произведение помещается.
	fits := pfStack("injected", 210, poolLink{
		service: "vpc", pool: 40, poolPath: "repository.postgres.maxConns",
		replicas: 1, replicasPath: "replicas",
		hpaDeclared: true, hpaEnabled: true, hpaMax: 5,
	})
	if got, _, _ := scanPoolFits([]poolFacts{fits}); len(got) != 0 {
		t.Fatalf("проверка сработала на посадке, которая помещается: %+v", got)
	}

	// ЗАКОННЫЙ БЛИЗНЕЦ ВТОРОЙ: автомасштабирование ОБЪЯВЛЕНО И ВЫКЛЮЧЕНО —
	// потолком остаётся стартовое число подов, и `maxReplicas` ничего не поднимает.
	// Без этой пробы гейт, читающий `maxReplicas` безусловно, был бы неотличим от
	// верного: он краснел бы на посадке, которая ничего не нарушает.
	off := pfStack("injected", 210, poolLink{
		service: "nlb", pool: 200, poolPath: "config.repository.postgres.maxConns",
		replicas: 1, replicasPath: "replicas",
		hpaDeclared: true, hpaEnabled: false, hpaMax: 5,
	})
	if got, _, _ := scanPoolFits([]poolFacts{off}); len(got) != 0 {
		t.Fatalf("проверка приняла выключенное автомасштабирование за действующий потолок: %+v", got)
	}
}

func TestPoolGateTreatsAnUndeclaredWidthAsAFindingNotASkip(t *testing.T) {
	// ДЕФЕКТ: ширина пула не объявлена вовсе — действует умолчание драйвера.
	none := pfStack("injected", 210, poolLink{
		service: "storage", replicas: 1, replicasPath: "replicaCount",
	})
	got, examined, _ := scanPoolFits([]poolFacts{none})
	if kindsOf(got)[kindPoolUndeclared] != 1 {
		t.Fatalf("необъявленная ширина пула не стала находкой: %+v", got)
	}
	if examined != 0 {
		t.Fatalf("связка без объявленной ширины попала в осмотренные (%d) — тогда перепись "+
			"утверждала бы больше проверенного", examined)
	}
	if !strings.Contains(got[0].why, "storage") {
		t.Fatalf("находка не называет службу: %q", got[0].why)
	}

	// ДЕФЕКТ ТОГО ЖЕ ВИДА, но записанный нулём: `maxConns: 0` — это не «пул
	// шириной ноль», это «действует умолчание драйвера», то есть ровно предыдущий
	// случай, только не отличимый от объявления при беглом чтении.
	zero := pfStack("injected", 210, poolLink{
		service: "registry", pool: 0, poolPath: "db.maxConns",
		replicas: 1, replicasPath: "replicaCount",
	})
	gotZero, _, _ := scanPoolFits([]poolFacts{zero})
	if kindsOf(gotZero)[kindPoolUndeclared] != 1 {
		t.Fatalf("нулевая ширина пула не стала находкой: %+v", gotZero)
	}
	if !strings.Contains(gotZero[0].why, "db.maxConns = 0") {
		t.Fatalf("находка не называет путь, по которому стоит ноль: %q", gotZero[0].why)
	}

	// ЗАКОННЫЙ БЛИЗНЕЦ: ширина объявлена и помещается — молчание.
	declared := pfStack("injected", 210, poolLink{
		service: "storage", pool: 20, poolPath: "db.maxConns",
		replicas: 1, replicasPath: "replicaCount",
	})
	if got, _, _ := scanPoolFits([]poolFacts{declared}); len(got) != 0 {
		t.Fatalf("проверка сработала на объявленной и помещающейся ширине: %+v", got)
	}
}

func TestPoolGateRefusesAnUndeclaredReplicaCeiling(t *testing.T) {
	// ДЕФЕКТ: автомасштабирование включено, а его потолок не объявлен — умножать
	// не на что.
	noMax := pfStack("injected", 210, poolLink{
		service: "compute", pool: 15, poolPath: "db.maxConns",
		replicas: 1, replicasPath: "replicas",
		hpaDeclared: true, hpaEnabled: true,
	})
	got, examined, _ := scanPoolFits([]poolFacts{noMax})
	if kindsOf(got)[kindCeilingUndeclared] != 1 {
		t.Fatalf("необъявленный потолок реплик не стал находкой: %+v", got)
	}
	if examined != 0 {
		t.Fatalf("связка без потолка попала в осмотренные (%d)", examined)
	}

	// ЗАКОННЫЙ БЛИЗНЕЦ: потолок объявлен — молчание.
	withMax := pfStack("injected", 210, poolLink{
		service: "compute", pool: 15, poolPath: "db.maxConns",
		replicas: 1, replicasPath: "replicas",
		hpaDeclared: true, hpaEnabled: true, hpaMax: 5,
	})
	if got, _, _ := scanPoolFits([]poolFacts{withMax}); len(got) != 0 {
		t.Fatalf("проверка сработала на объявленном потолке: %+v", got)
	}
}

func TestPoolGateCountsTheSuperuserReserveAndSumsPerDatabase(t *testing.T) {
	// ГРАНИЦА: запас суперпользователя — часть предела. 207 помещается, 208 нет.
	// Контроль на границе и на волосок за ней, иначе константа 3 проверялась бы
	// только тем, что её никто не трогал.
	edge := func(pool int) []poolFinding {
		got, _, _ := scanPoolFits([]poolFacts{pfStack("injected", 210, poolLink{
			service: "vpc", pool: pool, poolPath: "repository.postgres.maxConns",
			replicas: 1, replicasPath: "replicas",
		})})
		return got
	}
	if got := edge(207); len(got) != 0 {
		t.Fatalf("207 при max_connections 210 обязано помещаться (210 − 3): %+v", got)
	}
	if got := edge(208); kindsOf(got)[kindOverPromise] != 1 {
		t.Fatalf("208 при max_connections 210 обязано быть находкой: %+v", got)
	}

	// СУММА ПО БАЗЕ: две службы на одной базе, каждая по отдельности помещается,
	// вместе — нет. Без суммирования обе прошли бы.
	two := poolFacts{
		stack:    "injected",
		ceilings: map[string]poolCeiling{"pg-synthetic": {maxConnections: 210, declaredByProfile: true}},
		links: []poolLink{
			{service: "a", pg: "pg-synthetic", pool: 150, poolPath: "db.maxConns", replicas: 1, replicasPath: "replicas"},
			{service: "b", pg: "pg-synthetic", pool: 100, poolPath: "db.maxConns", replicas: 1, replicasPath: "replicas"},
		},
	}
	got, _, _ := scanPoolFits([]poolFacts{two})
	if kindsOf(got)[kindOverPromise] != 1 {
		t.Fatalf("сумма по базе не посчитана: %+v", got)
	}
	if !strings.Contains(got[0].why, "250") {
		t.Fatalf("находка не называет сумму обещанного: %q", got[0].why)
	}
}

func TestPoolGateLedgerForgivesItsSubjectAndExpiresWithoutOne(t *testing.T) {
	// Ведомость в дереве ПУСТА, и это её нормальное состояние. Значит обе её
	// ветки в теле проверки не исполняются ни разу — без этой пробы нельзя
	// сказать, работают ли они вообще.
	f := poolFinding{stack: "injected", subject: "pg-synthetic", kind: kindOverPromise, why: "…"}

	// БЕЗ ведомости находка обязана ронять прогон.
	fail, forgiven, stale := applyLedger([]poolFinding{f}, map[string]string{})
	if len(fail) != 1 || len(forgiven) != 0 || len(stale) != 0 {
		t.Fatalf("находка без записи в ведомости не уронила прогон: fail=%v forgiven=%v stale=%v",
			fail, forgiven, stale)
	}

	// С записью — прощена, и прощение НАЗВАНО (молчаливое прощение неотличимо
	// от отсутствия находки).
	ledger := map[string]string{f.key(): "чужой предмет, заведён задачей #0"}
	fail, forgiven, stale = applyLedger([]poolFinding{f}, ledger)
	if len(fail) != 0 || len(forgiven) != 1 || len(stale) != 0 {
		t.Fatalf("запись ведомости не простила свою находку: fail=%v forgiven=%v stale=%v",
			fail, forgiven, stale)
	}
	if !strings.Contains(forgiven[0], f.key()) {
		t.Fatalf("прощение не называет ключ: %q", forgiven[0])
	}

	// САМОИСТЕЧЕНИЕ: находки не стало — записи больше нечего исключать, и это
	// находка сама по себе. Без этой ветки исключение пережило бы свой предмет.
	fail, forgiven, stale = applyLedger(nil, ledger)
	if len(fail) != 0 || len(forgiven) != 0 || len(stale) != 1 {
		t.Fatalf("запись, которой нечего исключать, не стала находкой: "+
			"fail=%v forgiven=%v stale=%v", fail, forgiven, stale)
	}
	if !strings.Contains(stale[0], f.key()) {
		t.Fatalf("сообщение о просроченной записи не называет её ключ: %q", stale[0])
	}

	// ЗАКОННЫЙ БЛИЗНЕЦ: пустая ведомость на пустых находках молчит — идеал не
	// превращён в поломку.
	fail, forgiven, stale = applyLedger(nil, map[string]string{})
	if len(fail)+len(forgiven)+len(stale) != 0 {
		t.Fatalf("пустая ведомость на чистом дереве заговорила: fail=%v forgiven=%v stale=%v",
			fail, forgiven, stale)
	}
}

func TestPoolGateTreatsAnUnsetCeilingAsTheBuildDefaultNotAsInfinity(t *testing.T) {
	// Предел базы, не объявленный ни одним профилем, — это умолчание СБОРКИ (100),
	// а не «ограничения нет». Проверка обязана и сработать, и сказать, что предел
	// никто не выбирал: «предел мал» и «предел не выбран» чинятся по-разному.
	unset := poolFacts{
		stack:    "injected",
		ceilings: map[string]poolCeiling{"pg-synthetic": {maxConnections: pgDefaultMaxConnections}},
		links: []poolLink{{
			service: "registry", pg: "pg-synthetic", pool: 120, poolPath: "db.maxConns",
			replicas: 1, replicasPath: "replicaCount",
		}},
	}
	got, _, _ := scanPoolFits([]poolFacts{unset})
	if kindsOf(got)[kindOverPromise] != 1 {
		t.Fatalf("необъявленный предел базы принят за бесконечный: %+v", got)
	}
	if !strings.Contains(got[0].why, "НЕ ОБЪЯВЛЕН") {
		t.Fatalf("находка не различает «предел мал» и «предел никто не выбирал»: %q", got[0].why)
	}

	// ЗАКОННЫЙ БЛИЗНЕЦ: то же умолчание сборки, но пул в него помещается.
	fits := poolFacts{
		stack:    "injected",
		ceilings: map[string]poolCeiling{"pg-synthetic": {maxConnections: pgDefaultMaxConnections}},
		links: []poolLink{{
			service: "registry", pg: "pg-synthetic", pool: 20, poolPath: "db.maxConns",
			replicas: 1, replicasPath: "replicaCount",
		}},
	}
	if got, _, _ := scanPoolFits([]poolFacts{fits}); len(got) != 0 {
		t.Fatalf("проверка сработала на пуле, помещающемся в умолчание сборки: %+v", got)
	}
}
