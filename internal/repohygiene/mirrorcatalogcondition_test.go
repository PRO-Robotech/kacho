// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// mirrorcatalogcondition_test.go — УСЛОВИЕ КАТАЛОГА НЕСЁТ КАЖДЫЙ ПИСАТЕЛЬ ЗЕРКАЛА.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// `kacho_iam.resource_mirror` — проекция каталога ресурсов, которую владельцы
// присылают через проксируемую полосу регистрации. Требование «тип обязан иметь
// живую строку каталога» выражено ОПЕРАТОРОМ записи, а не внешним ключом:
// постоянное ограничение запретило бы платформе снять тип, пока у арендатора
// есть ресурс этого типа, то есть сделало бы решение платформы заложником данных
// арендатора (задача продукта #1031, круг 4 приёмки; довод там измерен и здесь
// не пересматривается).
//
// Цена выбора: инвариант стал свойством ПОЛОСЫ, а не свойством ТАБЛИЦЫ.
// Инвариант, выраженный в одном операторе из нескольких, — это инвариант,
// которого нет: второй писатель обходит его молча, и обнаруживается это тогда,
// когда данные уже записаны. Этот гейт — второй рубеж под тем же утверждением.
//
// ─────────────────────────────────────────────────────────────────────────────
// ГЕЙТ СВЕРЯЕТ ПОЛОСЫ МЕЖДУ СОБОЙ, А НЕ С ВЫПИСАННЫМ УСЛОВИЕМ — И ЭТО РЕШЕНИЕ
//
// Требование выводится из ЭТАЛОННОЙ полосы (пакет `resource_mirror` — тот, через
// который идёт регистрация от владельцев), а не задаётся здесь литералом. Две
// причины, и обе несущие:
//
//  1. Условия в дереве СЕГОДНЯ ЕЩЁ НЕТ — его пишет своя задача (#1031, приёмка
//     APPROVED, код не написан). Гейт, требующий выписанного условия, был бы
//     красным на всех трёх писателях сразу, и его пришлось бы гасить ведомостью
//     из трёх записей — то есть ведомостью, исключающей всю популяцию. Такое
//     послабление не истекает никогда.
//  2. Проба каждой полосы отдельно требует знать, КАКИМ свойство должно быть, —
//     а это и есть спорный вопрос, решаемый в #1031. Сверка полос спрашивает
//     другое: «решал ли кто-нибудь, что они различаются». На это ответ есть
//     всегда (`.claude/rules/architecture.md` §«Параллельные полосы одного
//     механизма обязаны сверяться МЕЖДУ СОБОЙ»).
//
// Следствие, ради которого гейт и заводится: в день, когда условие приедет в
// эталонную полосу, гейт КРАСНЕЕТ на остальных писателях, называя их по имени.
// Инвариант не сможет landing'ом остаться свойством одной полосы.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПЕРЕПИСЬ ПЕЧАТАЕТ ДВА ЧИСЛА, А НЕ ОДНО
//
// «полос N · несут условие M». Одно число скрывает ровно тот случай, ради
// которого гейт заведён: «несут 1» без «полос 3» читается как исправность.
//
// ─────────────────────────────────────────────────────────────────────────────
// ГРАНИЦЫ НАЗВАНЫ, А НЕ УМОЛЧАНЫ
//
//   - Гейт судит УЗЕЛ-СТРОКОВЫЙ-ЛИТЕРАЛ разобранного дерева. Имя таблицы стоит и
//     в комментариях — в том числе в комментариях, объясняющих эту самую защиту, —
//     и гейт по подстроке краснел бы на собственном объяснении.
//   - Он судит УПОМИНАНИЕ таблицы каталога в том же операторе, а НЕ эквивалентность
//     предиката. Писатель, назвавший `catalog_resource` в соединении, которое ничего
//     не фильтрует, пройдёт. Эквивалентность предикатов машинно здесь не решается;
//     эту половину держит обзор, и она названа, чтобы её не приняли за исполненную.
//   - Он НЕ видит: SQL миграций (там запись законна — миграция и есть схема), проб
//     (им положено готовить состояние) и имя таблицы, СОБРАННОЕ ИЗ КУСКОВ —
//     настоящая слепая зона, предикатом по литералу не ловится ничем.
//   - Инъекция подаёт вход сама, поэтому исчезновение предмета из дерева её не
//     трогает. Поэтому предпосылки проверяются ЗДЕСЬ: ноль прочитанных файлов,
//     ноль упоминаний таблицы и отсутствие эталонной полосы — ОТКАЗЫ, а не
//     «нарушений нет».
//
// ─────────────────────────────────────────────────────────────────────────────
// ОМОНИМ, НА КОТОРОМ СПОТКНЁТСЯ СЛЕДУЮЩИЙ ЧИТАТЕЛЬ
//
// Предикат по голому имени `resource_mirror` даёт ЧЕТВЁРТОГО «писателя» —
// `services/iam/tools/authzformbench`. Он им НЕ является: прибор заводит СВОЮ таблицу этого
// имени в СВОЕЙ временной схеме (`CREATE TABLE resource_mirror`, `search_path`
// на неё же) и меряет форму реляционного вердикта, а не продуктовую строку.
// Совпадение имени — не свойство дерева. Гейт судит имя ВМЕСТЕ СО СХЕМОЙ
// (`kacho_iam.`), поэтому омонима не видит by construction; а если кто-нибудь
// когда-нибудь нацелит прибор на продуктовую схему, сработает ось «вводящий
// писатель вне services/iam».
//
// Задача продукта: #1886.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/gitenv"
)

// resourceMirrorTable — зеркало каталога ресурсов.
const resourceMirrorTable = "kacho_iam.resource_mirror"

// mirrorReferenceLane — каталог ЭТАЛОННОЙ полосы: путь, по которому регистрация
// от владельца ресурса доходит до строки зеркала. Требование выводится из него.
//
// Переедет каталог — гейт объявит ОТКАЗ, а не промолчит: сверять «тем же
// условием» станет не с чем, и молчание означало бы, что гейт умер вместе с
// координатой. Правь эту константу тем же изменением, каким двигаешь пакет.
const mirrorReferenceLane = "services/iam/internal/repo/kacho/pg/resource_mirror/"

// mirrorOwnerService — сервис-владелец таблицы. Вводящий писатель вне него
// означает запись в чужую БД (ban #8), и это находка независимо от условий.
const mirrorOwnerService = "services/iam/"

// catalogTablePattern — таблица каталога модуля. Признак — ПРЕФИКС, а не перечень:
// каталог растёт (`catalog_module`, `catalog_resource`, `catalog_verb`, и #1031
// заводит четвёртую), а выписанный перечень отстал бы от него молча — то есть
// новое условие оказалось бы вне наблюдения, не будучи нарушением.
var catalogTablePattern = regexp.MustCompile(`kacho_iam\.catalog_[a-z0-9_]+`)

// mirrorWriteVerbs — глаголы записи. Чтение (`SELECT … FROM`) в перечень не
// входит намеренно: читателей у зеркала десятки, все законны, и они служат
// близнецом, на котором гейт обязан молчать.
//
// `introduces` — вводит ли оператор НОВУЮ строку. Предмет условия есть только у
// вводящих: правка существующей строки нового типа не заводит, а снятие тем
// более. Требовать сверки у них значило бы требовать её там, где сверять нечего.
var mirrorWriteVerbs = []struct {
	verb       string
	introduces bool
}{
	{"INSERT INTO " + resourceMirrorTable, true},
	{"MERGE INTO " + resourceMirrorTable, true},
	{"UPDATE " + resourceMirrorTable, false},
	{"DELETE FROM " + resourceMirrorTable, false},
}

// mirrorWrite — одна найденная запись в зеркало.
type mirrorWrite struct {
	// File — путь от корня дерева; заполняет обходчик.
	File string
	// Func — объемлющая функция; пустое имя означает пакетный уровень.
	Func string
	// Verb — какой оператор записи найден.
	Verb string
	// Introduces — вводит ли оператор новую строку (и значит есть ли у него предмет).
	Introduces bool
	// Catalog — таблицы каталога, названные В ТОМ ЖЕ операторе, по возрастанию.
	Catalog []string
}

// key — координата писателя в ведомости исключений.
func (w mirrorWrite) key() string { return w.File + "::" + w.Func }

// mirrorWritesIn разбирает исходник Go и возвращает записи в зеркало, приписанные
// объемлющей функции, плюс число литералов, называющих таблицу вообще (перепись
// предпосылки: читатели тоже считаются).
func mirrorWritesIn(filename, src string) ([]mirrorWrite, int, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, src, 0)
	if err != nil {
		return nil, 0, err
	}

	type span struct {
		from, to token.Pos
		name     string
	}
	var spans []span
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		name := fn.Name.Name
		if fn.Recv != nil && len(fn.Recv.List) > 0 {
			name = typeName(fn.Recv.List[0].Type) + "." + name
		}
		spans = append(spans, span{from: fn.Body.Pos(), to: fn.Body.End(), name: name})
	}

	var (
		writes   []mirrorWrite
		mentions int
	)
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		if !strings.Contains(lit.Value, resourceMirrorTable) {
			return true
		}
		mentions++
		upper := strings.ToUpper(lit.Value)
		owner := ""
		for _, s := range spans {
			if lit.Pos() >= s.from && lit.End() <= s.to {
				owner = s.name
				break
			}
		}
		// Каталог берётся ИЗ ТОГО ЖЕ ЛИТЕРАЛА: обещание сверки, данное в
		// комментарии рядом, условием не является — иначе писателю довольно было
		// бы пообещать её словами.
		// uniqueSorted — общий помощник пакета (nameformdbprobe.go): второй его
		// экземпляр разошёлся бы с первым молча.
		catalog := uniqueSorted(catalogTablePattern.FindAllString(strings.ToLower(lit.Value), -1))
		for _, v := range mirrorWriteVerbs {
			if strings.Contains(upper, strings.ToUpper(v.verb)) {
				writes = append(writes, mirrorWrite{
					Func:       owner,
					Verb:       v.verb,
					Introduces: v.introduces,
					Catalog:    catalog,
				})
			}
		}
		return true
	})
	return writes, mentions, nil
}

// mirrorConditionLedger — ведомость исключений: писатель → ПРИЧИНА, по которой он
// вправе не спрашивать каталог.
//
// Сегодня она ПУСТА, и это цель, а не недосмотр: все три писателя обязаны нести
// то, что несёт эталонная полоса. Пустая ведомость проходит — отказ на ней толкал
// бы держать запись ради зелёного.
//
// Запись, которой больше нечего исключать (писатель условие получил либо исчез из
// дерева), — НАХОДКА: послабление обязано истекать само, иначе оно переживает свой
// предмет, оставаясь на вид рабочим.
var mirrorConditionLedger = map[string]string{}

// mirrorConditionOutcome — вердикт сверки полос вместе с переписью.
type mirrorConditionOutcome struct {
	// Findings — писатели, обошедшие условие эталонной полосы либо пишущие чужую БД.
	Findings []string
	// Stale — записи ведомости, которым больше нечего исключать.
	Stale []string
	// Required — условие, выведенное из эталонной полосы (таблицы каталога).
	Required []string
	// Lanes — вводящих писателей всего.
	Lanes int
	// LaneKeys — они же поимённо: число без перечня читатель проверить не может.
	LaneKeys []string
	// Carriers — из них несущих требуемое условие целиком.
	Carriers int
	// Exempt — из них погашенных ведомостью.
	Exempt int
	// ReferenceMissing — эталонной полосы в наборе нет; сверять не с чем.
	ReferenceMissing bool
}

// mirrorConditionReport — ЧИСТАЯ функция сверки: по набору записей и ведомости
// возвращает находки и перепись. Вынесена из обхода дерева намеренно — только так
// её способность краснеть и молчать доказывается инъекцией на синтетическом входе,
// не трогая настоящее дерево.
func mirrorConditionReport(writes []mirrorWrite, ledger map[string]string) mirrorConditionOutcome {
	var out mirrorConditionOutcome

	// Требование выводится из эталонной полосы, а не задаётся литералом.
	var refSeen bool
	requiredSet := map[string]bool{}
	for _, w := range writes {
		if !w.Introduces || !strings.Contains("/"+w.File, "/"+mirrorReferenceLane) {
			continue
		}
		refSeen = true
		for _, c := range w.Catalog {
			requiredSet[c] = true
		}
	}
	out.ReferenceMissing = !refSeen
	for c := range requiredSet {
		out.Required = append(out.Required, c)
	}
	sort.Strings(out.Required)

	// used — записи ведомости, которым нашлось что исключать.
	used := map[string]bool{}

	for _, w := range writes {
		if !w.Introduces {
			continue
		}
		out.Lanes++
		out.LaneKeys = append(out.LaneKeys, w.key())

		if !strings.Contains("/"+w.File, "/"+mirrorOwnerService) {
			out.Findings = append(out.Findings, w.File+"::"+w.Func+
				" — вводящий писатель зеркала ВНЕ "+mirrorOwnerService+
				": таблица принадлежит iam, и запись в неё из чужого сервиса означает общую БД (ban #8)")
			continue
		}

		have := map[string]bool{}
		for _, c := range w.Catalog {
			have[c] = true
		}
		var missing []string
		for _, c := range out.Required {
			if !have[c] {
				missing = append(missing, c)
			}
		}
		if len(missing) == 0 {
			out.Carriers++
			continue
		}
		if reason, ok := ledger[w.key()]; ok && strings.TrimSpace(reason) != "" {
			used[w.key()] = true
			out.Exempt++
			continue
		}
		out.Findings = append(out.Findings, w.File+"::"+w.Func+
			" — вводит строку зеркала, НЕ спрашивая "+strings.Join(missing, ", ")+
			", тогда как эталонная полоса ("+mirrorReferenceLane+") спрашивает. "+
			"Инвариант, выраженный в одном операторе из нескольких, — это инвариант, которого нет: "+
			"этот писатель положит строку с типом, которого каталог не знает, МОЛЧА. "+
			"Исходов три: спросить каталог тем же условием · снять писателя · назвать его "+
			"исключением с причиной в mirrorConditionLedger")
	}

	// Ведомость истекает сама.
	var keys []string
	for k := range ledger {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if used[k] {
			continue
		}
		if strings.TrimSpace(ledger[k]) == "" {
			out.Stale = append(out.Stale, k+" — исключение БЕЗ ПРИЧИНЫ: следующий читатель "+
				"либо снимет его как непонятное, либо оставит навсегда, не зная предмета")
			continue
		}
		out.Stale = append(out.Stale, k+" — исключению больше нечего исключать: писатель либо "+
			"получил условие, либо исчез из дерева. Снимите запись — послабление обязано истекать само")
	}
	return out
}

// TestMirrorRowCatalogConditionReachesEveryWriter — в непроверочном коде Go каждый
// оператор, ВВОДЯЩИЙ строку зеркала ресурсов, спрашивает каталог тем же условием,
// каким его спрашивает эталонная полоса, либо назван исключением с причиной.
func TestMirrorRowCatalogConditionReachesEveryWriter(t *testing.T) {
	root := repoRoot(t)
	out, err := gitenv.Command(root, "ls-files", "-z", "--", "*.go").Output()
	if err != nil {
		t.Fatalf("git ls-files: %v — состав дерева не установлен, и «ноль находок» здесь "+
			"означало бы «ноль прочитанного»", err)
	}

	var (
		filesRead int
		mentions  int
		writes    []mirrorWrite
	)
	for _, rel := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
		if rel == "" || skipPath(rel) || strings.HasSuffix(rel, "_test.go") {
			continue
		}
		b, readErr := os.ReadFile(filepath.Join(root, rel)) // #nosec G304 -- путь из индекса своего дерева
		if readErr != nil {
			t.Fatalf("чтение %s: %v", rel, readErr)
		}
		filesRead++
		body := string(b)
		if !strings.Contains(body, resourceMirrorTable) {
			continue
		}
		w, m, perr := mirrorWritesIn(rel, body)
		if perr != nil {
			t.Fatalf("разбор %s: %v — файл индекса не разобран, и его молчание ничего не значит", rel, perr)
		}
		mentions += m
		for _, one := range w {
			one.File = rel
			writes = append(writes, one)
		}
	}

	if filesRead == 0 {
		t.Fatal("осмотрено ноль файлов — гейт не читал дерева, и его молчание ничего не значит")
	}
	if mentions == 0 {
		t.Fatalf("имя %q не встречается в непроверочном коде НИ РАЗУ — предмета у гейта нет: "+
			"либо таблица переименована (правь resourceMirrorTable вместе с ней), либо её "+
			"перестали и читать, и писать", resourceMirrorTable)
	}

	rep := mirrorConditionReport(writes, mirrorConditionLedger)

	// Перепись печатает ОБА числа. Одно скрывает ровно тот случай, ради которого
	// гейт заведён: «несут 1» без «полос 3» читается как исправность.
	t.Logf("осмотрено непроверочных файлов Go: %d; литералов, называющих %s: %d; "+
		"операторов записи найдено: %d; из них ВВОДЯЩИХ строку — полос: %d; "+
		"несут условие эталонной полосы: %d из %d; погашено ведомостью: %d; "+
		"записей в ведомости: %d",
		filesRead, resourceMirrorTable, mentions, len(writes), rep.Lanes,
		rep.Carriers, rep.Lanes, rep.Exempt, len(mirrorConditionLedger))
	byVerb := map[string]int{}
	for _, w := range writes {
		byVerb[w.Verb]++
	}
	for _, v := range mirrorWriteVerbs {
		t.Logf("  операторов «%s»: %d (вводит строку: %t)", v.verb, byVerb[v.verb], v.introduces)
	}
	for _, k := range rep.LaneKeys {
		t.Logf("  полоса: %s", k)
	}
	if len(rep.Required) == 0 {
		// Без этой строки «несут 3 из 3» читается как исполненность, тогда как
		// требовать сегодня нечего: условие ещё не написано (#1031). Число,
		// которое ничего не утверждает, обязано само об этом сказать.
		t.Logf("условие эталонной полосы ПУСТО: ни один оператор эталона не называет "+
			"%s* — требовать сегодня нечего, и «несут %d из %d» исполненностью НЕ является. "+
			"Сверка вооружена: в день, когда условие приедет в %s, гейт покраснеет на "+
			"остальных полосах поимённо",
			"kacho_iam.catalog_", rep.Carriers, rep.Lanes, mirrorReferenceLane)
	} else {
		t.Logf("условие эталонной полосы: %v", rep.Required)
	}

	if rep.ReferenceMissing {
		t.Fatalf("эталонной полосы %q среди вводящих писателей НЕТ — сверять «тем же условием» "+
			"не с чем. Каталог переехал, а константа mirrorReferenceLane осталась: молчание "+
			"здесь означало бы, что гейт умер вместе с координатой", mirrorReferenceLane)
	}
	if rep.Lanes == 0 {
		t.Fatal("вводящих писателей зеркала ноль при непустой переписи упоминаний — предмет " +
			"сверки исчез, а гейт остался бы зелёным")
	}

	for _, f := range rep.Findings {
		t.Error(f)
	}
	for _, s := range rep.Stale {
		t.Error(s)
	}
}
