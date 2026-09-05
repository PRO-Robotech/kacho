// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// console_stream_kind_dictionary_test.go — НАПИСАНИЕ ВИДА, НАЗВАННОЕ КОНСОЛЬЮ,
// ОБЯЗАНО БЫТЬ ОБЪЯВЛЕНО ВЛАДЕЛЬЦЕМ ЖУРНАЛА.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ (#1546)
//
// Согласие консоли с владельцем журнала держали ДВА гейта, и оба судили только
// ИМЯ ВЛАДЕЛЬЦА: имя, названное консолью, профиль обязан объявить владельцем
// (`gateway/deploy/console_subscription_owner_test.go`; до #1633 там сверялось
// более широкое множество — имена, которые край умеет резолвить), и владелец,
// объявленный краю, обязан быть назван консолью (соседний файл, #1021).
// НАПИСАНИЕ ВИДА не судил ни один.
//
// ЦЕНА АСИММЕТРИЧНА, и в этом весь предмет. Неверное ИМЯ владельца даёт `400`
// на открытии потока — оно видно в журнале браузера и уже стоило двух отказов
// подряд (#1440). Неверное НАПИСАНИЕ ВИДА не даёт НИЧЕГО: край поток откроет,
// владелец пришлёт словарь без этого вида, `hub.covers()` ответит «нет», и
// список молча останется на опросе. Ошибки нет ни в одном журнале, страница
// работает, опрос возвращается по построению — то есть возможность объявлена и
// неисполнима, а неисполнимость невидима.
//
// ─────────────────────────────────────────────────────────────────────────────
// СЛОВАРЬ ВЫВОДИТСЯ ИЗ ДЕРЕВА, И БЕРЁТСЯ ОН НЕ У КЛЮЧА КАРТЫ
//
// Владелец объявляет виды картой `Kinds` своего журнала. Ключ этой карты —
// слово ХРАНИЛИЩА (`Instance`, `nlb_load_balancer`): как владелец записал строку
// в своей таблице. По проводу уходит ДРУГОЕ — `ObjectType` привязки, то есть тип
// объекта модели прав, и именно его перечисляет `knownKinds`
// (`pkg/subscription.Journal.KindDictionary`). Гейт, сверяющий карту консоли с
// ключами, был бы красен на верном дереве и зелен на подменённом — он сравнивал
// бы не то, что уходит клиенту.
//
// Значения `ObjectType` — константы соседних пакетов, поэтому разбор идёт в два
// шага: узел-ссылка у журнала, затем её значение у объявляющего пакета.
// Нерезолвившаяся ссылка — ОТКАЗ, а не пропуск: молча укоротившийся словарь
// объявил бы нарушителями законные виды.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО СУДИТСЯ, А ЧТО НЕТ
//
// Судятся четыре свойства, и каждое отвечает на свой вопрос:
//
//  1. вид, названный консолью, объявлен ХОТЬ ОДНИМ журналом дерева. Это главная
//     половина: опечатка в написании ловится здесь;
//  2. все виды ОДНОГО владельца консоли приходят из ОДНОГО журнала. Вид,
//     приписанный чужому владельцу, разводит его виды по двум журналам;
//  3. один журнал назван НЕ БОЛЕЕ ЧЕМ одним владельцем консоли — обратная
//     сторона второго;
//  4. владелец, у которого в дереве ЕСТЬ ОДНОИМЁННЫЙ каталог с журналом, назван
//     при видах ИМЕННО ЭТОГО журнала.
//
// ЧЕТВЁРТОЕ ЗАВЕДЕНО ПО ДЫРЕ, найденной инъекцией, а не придумано вперёд.
// Свойств 2 и 3 достаточно, пока вид ПЕРЕЕЗЖАЕТ к чужому владельцу: виды
// переехавшего владельца расходятся по двум журналам. Но ЧИСТАЯ ПЕРЕСТАНОВКА
// двух владельцев, у каждого из которых консоль называет по одному виду,
// оставляет и то и другое свойство выполненными — у каждого владельца ровно
// один журнал, у каждого журнала ровно один владелец, — и проходит молча. На
// сегодняшнем дереве такими были бы `compute` и `registry`.
//
// ЧЕТВЁРТОЕ ВЫВОДИТСЯ ИЗ ДЕРЕВА, А НЕ ВЫПИСЫВАЕТСЯ. Связи «имя владельца →
// каталог сервиса» в дереве нет: имя владельца есть домен контракта
// (`loadbalancer`), а каталог зовётся `nlb`, и оба написания исторические.
// Выписать её здесь значило бы завести второе место об одном предмете — оно уже
// есть у края и разошлось бы с этим молча. Поэтому правило спрашивает лишь то,
// что дерево ЗНАЕТ: существует ли каталог `services/<владелец>` с журналом. Есть
// — виды владельца обязаны приходить оттуда; нет — владелец держится свойствами
// 2 и 3, то есть взаимной однозначностью с оставшимся журналом. Перестановка,
// в которой участвует хотя бы один одноимённый владелец, ловится четвёртым; а
// владелец без одноимённого каталога в дереве ровно один, поэтому перестановки
// только между такими не существует by construction.
//
// Скольких владельцев держит имя, а скольких — однозначность, печатает перепись,
// и ОБА края этого числа — отказ, а не заметка. Ноль удержанных именем делает
// четвёртое свойство вакуумным. Больше ОДНОГО удержанного однозначностью
// возвращает ту самую дыру: между двумя такими владельцами перестановка снова
// невидима. Сегодня такой владелец ровно один (`loadbalancer`), и утверждение об
// этом держится проверкой, а не памятью — иначе оно пережило бы свой предмет в
// день, когда домен контракта второй раз разойдётся с именем каталога.
//
// НЕ судится и обратное включение — «каждый вид журнала назван консолью». Это
// ДРУГОЙ предикат, и с 2026-08-30 у него СВОЙ гейт: соседний файл
// `console_stream_kind_coverage_test.go` (#1558). Он судит не покрытие, а
// осознанность — вид, у которого в консоли нет страницы списка, законен, и
// требование покрытия объявило бы его нарушением, — поэтому непокрытый вид
// обязан быть ОБЪЯВЛЕН непоказанным с причиной. Решение и цена обеих сторон —
// `docs/architecture/subscription-console-kind-coverage.md`.
//
// Числа обеих сторон эта перепись печатает по-прежнему: они читаются человеком
// как картина целиком, а не только как вход вердикта.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕГО ЗДЕСЬ НЕТ НАМЕРЕННО
//
// ВЕДОМОСТИ ИСКЛЮЧЕНИЙ. Вид, который консоль обязана называть, а владелец не
// объявляет, сегодня не существует; запись под него была бы слепой зоной,
// выданной вперёд, — и под неё уехало бы настоящее расхождение.

package deploy_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// consoleSubjectEntry — запись карты предметов: спека → владелец и вид.
//
// Судятся ЗНАЧЕНИЯ полей записи, а не член объявленного типа: тип есть обещание
// о множестве имён, а в запрос уходит это.
var consoleSubjectEntry = regexp.MustCompile(
	`([A-Za-z0-9_"-]+)\s*:\s*\{\s*owner\s*:\s*"([^"]*)"\s*,\s*kind\s*:\s*"([^"]*)"\s*,?\s*\}`)

// consoleSubjectOwnerField — ЛЮБОЕ объявление владельца в записи карты, без
// требований к соседнему полю.
//
// Служит счётчиком, а не разбором: число его совпадений сверяется с числом
// разобранных записей, и расхождение — ОТКАЗ. Без такой сверки запись,
// переписанная формой, которой [consoleSubjectEntry] не знает (перенос полей на
// отдельные строки, обратный порядок `kind`/`owner`), просто выпала бы из
// осмотренного: гейт остался бы зелёным, и «ноль находок» означало бы «ноль
// прочитанного» — ровно тот класс, который он сам и стережёт
// (testing.md §«Гейт на класс», п. 7).
var consoleSubjectOwnerField = regexp.MustCompile(`owner\s*:\s*"`)

// journalKindsGlob — владельцы журнала. Перечень ВЫВОДИТСЯ обходом, а не
// выписывается: рукописный список разошёлся бы с деревом молча, и первым, чего
// он не заметил бы, стал бы новый владелец.
//
// Обход идёт через `pkg/treecorpus`, а не по диску: под `services/` на
// всякой машине, где поднимали стенд, лежит игнорируемое (распаковки чартов,
// отчёты прогонов), и состав, взятый диском, у разработчика и у конвейера
// разный. Состав берётся у индекса git — тем же способом, каким его берут
// остальные гейты дерева.
var journalKindsGlob = filepath.Join("services", "*", "internal", "subscriptionjournal", "journal.go")

// consoleStreamSubject — одна запись карты предметов консоли.
type consoleStreamSubject struct {
	Spec  string
	Owner string
	Kind  string
}

// objectTypeRef — ссылка на написание вида в объявлении владельца.
type objectTypeRef struct {
	// Qualifier — квалификатор пакета; пусто означает «объявлено рядом».
	Qualifier string
	Name      string
	// Literal — написание задано строкой прямо в журнале.
	Literal string
	HasLit  bool
	// Pos — координата для отказа резолва.
	Pos string
}

// journalKindDecl — что журнал объявляет и через какие импорты.
type journalKindDecl struct {
	Refs    []objectTypeRef
	Imports map[string]string
}

// consoleStreamSubjectsOf разбирает карту предметов консоли.
//
// Комментарии снимаются `stripSubjectComments` (соседний файл): прозы про виды в
// `subjects.ts` много — она объясняет ровно ту разницу написаний, из которой
// гейт и вырос, — поэтому сверка по сырому тексту краснела бы на собственном
// объяснении.
func consoleStreamSubjectsOf(src string) []consoleStreamSubject {
	code := stripSubjectComments(src)
	var out []consoleStreamSubject
	for _, m := range consoleSubjectEntry.FindAllStringSubmatch(code, -1) {
		if m[2] == "" || m[3] == "" {
			continue
		}
		out = append(out, consoleStreamSubject{
			Spec:  strings.Trim(m[1], `"`),
			Owner: m[2],
			Kind:  m[3],
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Spec < out[j].Spec })
	return out
}

// consoleSubjectCounts — сколько записей РАЗОБРАНО и сколько владельцев
// ОБЪЯВЛЕНО. Расхождение означает запись, записанную формой, которой разбор не
// знает: она выпала бы из осмотренного молча.
func consoleSubjectCounts(src string) (parsed, declared int) {
	code := stripSubjectComments(src)
	return len(consoleStreamSubjectsOf(src)),
		len(consoleSubjectOwnerField.FindAllString(code, -1))
}

// journalKindRefsOf разбирает объявление владельца и возвращает ССЫЛКИ на
// написания видов — узлами, а не словами.
//
// Берётся поле `ObjectType` привязки, а не ключ карты: ключ есть слово
// хранилища и клиенту не виден (см. шапку).
func journalKindRefsOf(rel, src string) (journalKindDecl, error) {
	decl := journalKindDecl{Imports: map[string]string{}}

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, rel, src, 0)
	if err != nil {
		return decl, fmt.Errorf("%s: разбор не удался: %w", rel, err)
	}
	for _, spec := range file.Imports {
		p, uerr := strconv.Unquote(spec.Path.Value)
		if uerr != nil {
			continue
		}
		name := path.Base(p)
		if spec.Name != nil {
			name = spec.Name.Name
		}
		decl.Imports[name] = p
	}

	ast.Inspect(file, func(n ast.Node) bool {
		kv, ok := n.(*ast.KeyValueExpr)
		if !ok {
			return true
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok || key.Name != "Kinds" {
			return true
		}
		lit, ok := kv.Value.(*ast.CompositeLit)
		if !ok {
			return true
		}
		for _, elt := range lit.Elts {
			binding, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			body, ok := binding.Value.(*ast.CompositeLit)
			if !ok {
				continue
			}
			for _, field := range body.Elts {
				fkv, ok := field.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				fname, ok := fkv.Key.(*ast.Ident)
				if !ok || fname.Name != "ObjectType" {
					continue
				}
				decl.Refs = append(decl.Refs,
					objectTypeRefOf(fkv.Value, fset.Position(fkv.Value.Pos()).String()))
			}
		}
		return true
	})
	return decl, nil
}

// objectTypeRefOf переводит выражение поля в ссылку. Формы три и все законны:
// строка прямо в журнале, местная константа и константа соседнего пакета.
func objectTypeRefOf(expr ast.Expr, pos string) objectTypeRef {
	switch v := expr.(type) {
	case *ast.BasicLit:
		if v.Kind == token.STRING {
			if unq, err := strconv.Unquote(v.Value); err == nil {
				return objectTypeRef{Literal: unq, HasLit: true, Pos: pos}
			}
		}
	case *ast.Ident:
		return objectTypeRef{Name: v.Name, Pos: pos}
	case *ast.SelectorExpr:
		if id, ok := v.X.(*ast.Ident); ok {
			return objectTypeRef{Qualifier: id.Name, Name: v.Sel.Name, Pos: pos}
		}
	}
	return objectTypeRef{Pos: pos}
}

// constLookup — значение строковой константы пакета. Отдельным параметром,
// чтобы инъекция подавала объявления соседей строкой и не трогала дерева.
type constLookup func(importPath, name string) (string, bool)

// resolveJournalKinds разрешает ссылки в написания. Нерезолвившаяся ссылка —
// ОТКАЗ: молча укоротившийся словарь объявил бы нарушителями законные виды.
func resolveJournalKinds(rel string, decl journalKindDecl, selfPkg string, lookup constLookup) ([]string, error) {
	seen := map[string]bool{}
	out := make([]string, 0, len(decl.Refs))
	for _, ref := range decl.Refs {
		var (
			value string
			ok    bool
		)
		switch {
		case ref.HasLit:
			value, ok = ref.Literal, true
		case ref.Name == "":
			return nil, fmt.Errorf("%s (%s): написание вида задано выражением, которого "+
				"разбор не знает — словарь получился бы короче объявленного, и законные "+
				"виды консоли стали бы нарушителями", rel, ref.Pos)
		case ref.Qualifier == "":
			value, ok = lookup(selfPkg, ref.Name)
		default:
			importPath, known := decl.Imports[ref.Qualifier]
			if !known {
				return nil, fmt.Errorf("%s (%s): квалификатор %q не назван ни одним импортом",
					rel, ref.Pos, ref.Qualifier)
			}
			value, ok = lookup(importPath, ref.Name)
		}
		if !ok {
			return nil, fmt.Errorf("%s (%s): значение %s не найдено — словарь получился бы "+
				"короче объявленного", rel, ref.Pos, ref.Qualifier+"."+ref.Name)
		}
		if seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out, nil
}

// consoleKindVerdict — перечни находок, по одному на судимое свойство, плюс
// перепись того, чем держится привязка владельцев.
type consoleKindVerdict struct {
	// Undeclared — вид, которого нет ни в одном словаре дерева.
	Undeclared []string
	// OwnerSpansJournals — владелец консоли назвал виды двух журналов.
	OwnerSpansJournals []string
	// JournalSharedByOwners — журнал назван двумя владельцами консоли.
	JournalSharedByOwners []string
	// NamedJournalMismatch — у владельца есть одноимённый журнал, а виды его
	// пришли из другого.
	NamedJournalMismatch []string
	// PinnedByName — владельцев, чью привязку держит одноимённый каталог.
	PinnedByName int
	// PinnedByBijection — владельцев, чью привязку держит только однозначность.
	PinnedByBijection int
}

func (v consoleKindVerdict) empty() bool {
	return len(v.Undeclared) == 0 && len(v.OwnerSpansJournals) == 0 &&
		len(v.JournalSharedByOwners) == 0 && len(v.NamedJournalMismatch) == 0
}

// journalDirOfOwner — каталог журнала, одноимённый владельцу. Форма пути одна на
// дерево и выводится из того же образца, которым собран словарь.
func journalDirOfOwner(owner string) string {
	return path.Join("services", owner, "internal", "subscriptionjournal")
}

// judgeConsoleKinds сверяет карту предметов со словарями дерева.
func judgeConsoleKinds(subjects []consoleStreamSubject, dict map[string][]string) consoleKindVerdict {
	byKind := map[string][]string{}
	for owner, kinds := range dict {
		for _, kind := range kinds {
			byKind[kind] = append(byKind[kind], owner)
		}
	}
	for kind := range byKind {
		sort.Strings(byKind[kind])
	}

	var verdict consoleKindVerdict
	ownerJournals := map[string]map[string]bool{}
	journalOwners := map[string]map[string]bool{}

	for _, s := range subjects {
		journals := byKind[s.Kind]
		if len(journals) == 0 {
			verdict.Undeclared = append(verdict.Undeclared, fmt.Sprintf(
				"спека %q называет владельцем %q вид %q, которого НЕ ОБЪЯВЛЯЕТ ни один "+
					"журнал дерева. Поток откроется, словарь владельца этого вида не "+
					"принесёт, `hub.covers()` ответит «нет» — и список молча останется на "+
					"опросе: ошибки не будет ни в одном журнале",
				s.Spec, s.Owner, s.Kind))
			continue
		}
		for _, j := range journals {
			if ownerJournals[s.Owner] == nil {
				ownerJournals[s.Owner] = map[string]bool{}
			}
			ownerJournals[s.Owner][j] = true
			if journalOwners[j] == nil {
				journalOwners[j] = map[string]bool{}
			}
			journalOwners[j][s.Owner] = true
		}
	}

	for _, owner := range sortedMapKeys(ownerJournals) {
		if len(ownerJournals[owner]) < 2 {
			continue
		}
		verdict.OwnerSpansJournals = append(verdict.OwnerSpansJournals, fmt.Sprintf(
			"владелец %q назван при видах ДВУХ журналов сразу (%s) — значит хотя бы один "+
				"вид приписан чужому владельцу. Поток уйдёт тому, кто этого вида не "+
				"объявляет, и молча промолчит",
			owner, strings.Join(sortedSetKeys(ownerJournals[owner]), ", ")))
	}
	for _, journal := range sortedMapKeys(journalOwners) {
		if len(journalOwners[journal]) < 2 {
			continue
		}
		verdict.JournalSharedByOwners = append(verdict.JournalSharedByOwners, fmt.Sprintf(
			"журнал %s назван ДВУМЯ владельцами консоли (%s) — у сервиса владелец один, "+
				"значит имена владельцев в карте перепутаны",
			journal, strings.Join(sortedSetKeys(journalOwners[journal]), ", ")))
	}

	// Свойство 4: одноимённый каталог, если он в дереве есть, привязку РЕШАЕТ.
	for _, owner := range sortedMapKeys(ownerJournals) {
		own := journalDirOfOwner(owner)
		if _, exists := dict[own]; !exists {
			verdict.PinnedByBijection++
			continue
		}
		verdict.PinnedByName++
		if ownerJournals[owner][own] && len(ownerJournals[owner]) == 1 {
			continue
		}
		verdict.NamedJournalMismatch = append(verdict.NamedJournalMismatch, fmt.Sprintf(
			"владелец %q назван при видах журнала %s, тогда как в дереве есть его "+
				"ОДНОИМЁННЫЙ журнал %s — имена владельцев в карте перепутаны. Чистая "+
				"перестановка двух владельцев не разводит виды по журналам и потому "+
				"видна только отсюда",
			owner, strings.Join(sortedSetKeys(ownerJournals[owner]), ", "), own))
	}

	sort.Strings(verdict.Undeclared)
	return verdict
}

func sortedMapKeys(m map[string]map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedSetKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// treeConstLookup читает строковые константы пакета из дерева, с памятью на
// разобранное: один пакет объявляет виды сразу нескольких владельцев.
func treeConstLookup(t *testing.T, root, modulePath string) constLookup {
	t.Helper()
	cache := map[string]map[string]string{}
	return func(importPath, name string) (string, bool) {
		consts, done := cache[importPath]
		if !done {
			consts = map[string]string{}
			rel := strings.TrimPrefix(importPath, modulePath+"/")
			if rel != importPath {
				collectStringConsts(t, filepath.Join(root, filepath.FromSlash(rel)), consts)
			}
			cache[importPath] = consts
		}
		v, ok := consts[name]
		return v, ok
	}
}

// collectStringConsts собирает строковые константы пакета. Пробы исключены:
// синтетика соседней пробы не является объявлением владельца.
func collectStringConsts(t *testing.T, dir string, out map[string]string) {
	t.Helper()
	entries, err := treecorpus.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatalf("перечень файлов пакета %s не собрался: %v", dir, err)
	}
	for _, p := range entries {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		src, rerr := os.ReadFile(p) // #nosec G304 -- обход собственного дерева
		if rerr != nil {
			t.Fatalf("файл %s не читается: %v", p, rerr)
		}
		fset := token.NewFileSet()
		file, perr := parser.ParseFile(fset, p, src, 0)
		if perr != nil {
			t.Fatalf("файл %s не разобрался: %v", p, perr)
		}
		for _, d := range file.Decls {
			gen, ok := d.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, s := range gen.Specs {
				vs, ok := s.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, ident := range vs.Names {
					if i >= len(vs.Values) {
						continue
					}
					bl, ok := vs.Values[i].(*ast.BasicLit)
					if !ok || bl.Kind != token.STRING {
						continue
					}
					if unq, uerr := strconv.Unquote(bl.Value); uerr == nil {
						out[ident.Name] = unq
					}
				}
			}
		}
	}
}

// modulePathOf читает путь модуля из go.mod. Выводится, а не выписывается: имя
// модуля живёт в одном месте, и копия разошлась бы с ним молча.
func modulePathOf(t *testing.T, root string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, "go.mod")) // #nosec G304 -- корень этого дерева
	if err != nil {
		t.Fatalf("go.mod не читается: %v", err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
			return strings.TrimSpace(rest)
		}
	}
	t.Fatal("go.mod не объявляет модуля — резолв ссылок владельца был бы беспредметен")
	return ""
}

// journalDictionaries — словари всех владельцев журнала дерева.
func journalDictionaries(t *testing.T, root string) map[string][]string {
	t.Helper()
	modulePath := modulePathOf(t, root)
	lookup := treeConstLookup(t, root, modulePath)

	paths, err := treecorpus.Glob(filepath.Join(root, journalKindsGlob))
	if err != nil {
		t.Fatalf("перечень владельцев журнала не собрался: %v", err)
	}
	sort.Strings(paths)

	out := map[string][]string{}
	for _, p := range paths {
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			t.Fatalf("путь не разрешился: %v", rerr)
		}
		rel = filepath.ToSlash(rel)
		src, rerr := os.ReadFile(p) // #nosec G304 -- обход собственного дерева
		if rerr != nil {
			t.Fatalf("объявление владельца %s не читается: %v", rel, rerr)
		}
		decl, derr := journalKindRefsOf(rel, string(src))
		if derr != nil {
			t.Fatalf("%v", derr)
		}
		selfPkg := modulePath + "/" + path.Dir(rel)
		kinds, kerr := resolveJournalKinds(rel, decl, selfPkg, lookup)
		if kerr != nil {
			t.Fatalf("%v", kerr)
		}
		out[path.Dir(rel)] = kinds
	}
	return out
}

// TestEveryKindTheConsoleNamesIsDeclaredByItsOwner — три свойства на дереве.
func TestEveryKindTheConsoleNamesIsDeclaredByItsOwner(t *testing.T) {
	root := repoRootFromTest(t)

	raw, err := os.ReadFile(filepath.Join(root, consoleSubjectsRel)) // #nosec G304 -- корень этого дерева
	if err != nil {
		t.Fatalf("карта предметов консоли %s не читается (%v) — сверять нечего", consoleSubjectsRel, err)
	}
	subjects := consoleStreamSubjectsOf(string(raw))
	_, declaredOwnerFields := consoleSubjectCounts(string(raw))
	dict := journalDictionaries(t, root)

	declared := 0
	perOwner := make([]string, 0, len(dict))
	for _, owner := range sortedDictKeys(dict) {
		declared += len(dict[owner])
		perOwner = append(perOwner, fmt.Sprintf("%s %d", owner, len(dict[owner])))
	}
	named := map[string]int{}
	for _, s := range subjects {
		named[s.Owner]++
	}
	namedPer := make([]string, 0, len(named))
	for _, owner := range sortedCountKeys(named) {
		namedPer = append(namedPer, fmt.Sprintf("%s %d", owner, named[owner]))
	}

	t.Logf("осмотрено: владельцев журнала %d, видов объявлено %d (%s); "+
		"карта предметов консоли (%s) называет спек %d, владельцев %d, видов %s",
		len(dict), declared, strings.Join(perOwner, " · "),
		consoleSubjectsRel, len(subjects), len(named), strings.Join(namedPer, " · "))

	// Премисы, а не вежливость: ноль прочитанных с любой стороны делает молчание
	// гейта неотличимым от «нарушений нет».
	if len(dict) == 0 {
		t.Fatalf("владельцев журнала найдено 0 по образцу %s — вердикт беспредметен",
			filepath.ToSlash(journalKindsGlob))
	}
	if declared == 0 {
		t.Fatal("владельцы не объявляют ни одного вида — сверять карту консоли не с чем")
	}
	if len(subjects) == 0 {
		t.Fatalf("%s не называет ни одной пары владелец+вид — прочитано ноль, и молчание "+
			"проверки не является утверждением о согласии", consoleSubjectsRel)
	}
	if declaredOwnerFields != len(subjects) {
		t.Fatalf("%s объявляет владельца %d раз, а разобрано записей %d — %d записей "+
			"записаны формой, которой разбор не знает, и вида в них НИКТО не судит. "+
			"Почини распознаватель, а не карту: невидимая запись — не редкость, а "+
			"слепая зона, и «ноль находок» в ней означает «ноль прочитанного»",
			consoleSubjectsRel, declaredOwnerFields, len(subjects),
			declaredOwnerFields-len(subjects))
	}

	verdict := judgeConsoleKinds(subjects, dict)
	t.Logf("привязка владельцев: одноимённым каталогом держится %d, взаимной "+
		"однозначностью %d", verdict.PinnedByName, verdict.PinnedByBijection)

	// Премиса свойства 4, с ОБЕИХ сторон.
	if verdict.PinnedByName == 0 {
		t.Fatal("ни один владелец консоли не совпал именем с каталогом журнала — " +
			"привязку держит только взаимная однозначность, а она допускает " +
			"перестановку имён целиком")
	}
	if verdict.PinnedByBijection > 1 {
		t.Fatalf("владельцев без одноимённого каталога журнала стало %d — между двумя "+
			"такими перестановка имён снова невидима: у каждого ровно один журнал, у "+
			"каждого журнала ровно один владелец, и свойство 4 к ним неприменимо. "+
			"Нужна связь «имя владельца → каталог сервиса», выведенная из дерева либо "+
			"объявленная решением, а не молчаливое расширение слепой зоны",
			verdict.PinnedByBijection)
	}

	for _, text := range verdict.NamedJournalMismatch {
		t.Errorf("%s: %s", consoleSubjectsRel, text)
	}
	for _, text := range verdict.Undeclared {
		t.Errorf("%s: %s.\nИсходов два: поправить написание на то, что владелец "+
			"перечисляет в `knownKinds` (тип объекта модели прав — поле `ObjectType` его "+
			"карты `Kinds`, НЕ ключ этой карты), либо снять запись, если журнала у этого "+
			"ресурса нет — тогда список остаётся на опросе осознанно",
			consoleSubjectsRel, text)
	}
	for _, text := range verdict.OwnerSpansJournals {
		t.Errorf("%s: %s", consoleSubjectsRel, text)
	}
	for _, text := range verdict.JournalSharedByOwners {
		t.Errorf("%s: %s", consoleSubjectsRel, text)
	}
}

func sortedDictKeys(m map[string][]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedCountKeys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
