// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// verbvocabulary_test.go — гейт: КАЖДЫЙ литеральный словарь глаголов в дереве
// выводится из канонической модели прав, а не объявляется сам по себе.
//
// Предмет. Ось ТИПОВ давно привязана к модели: гейт дрейфа требует точного
// равенства таблицы типов и модели в обе стороны. Ось ГЛАГОЛОВ не сверялась ни с
// чем: её сторожат литералы, каждый из которых объявляет ожидаемое и ни один — на
// каком основании именно это. Литерал чинится дописыванием в себя, поэтому
// СОГЛАСОВАННОЕ расширение всех литералов проходило молча. Этот гейт спрашивает
// МОДЕЛЬ, поэтому такое расширение краснеет.
//
// Гейт НИЧЕГО не импортирует и читает обе стороны разбором исходного текста:
//
//	(а) он достаёт неэкспортируемую переменную пакета, до которой внешний тестовый
//	    пакет символом не добирается;
//	(б) он не делает ни одну из сторон источником истины для другой — каждая
//	    сверяется С МОДЕЛЬЮ. Именно это сохраняет заявленную независимость гейта
//	    дрейфа: его ожидаемое значение по-прежнему не выводится из словаря эмиттера.
//
// Разбор идёт через go/ast, а не регуляркой: предмет — объявление переменной, и
// текстовый поиск нашёл бы то же имя в комментарии, который эту переменную
// объясняет.
package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// canonicalVerbModelRelPath — каноническая модель прав относительно корня репо.
// Тот же файл, который сторожит гейт дрейфа iam; здесь он читается независимо,
// потому что гейт не импортирует ни один пакет сервиса.
const canonicalVerbModelRelPath = "proto/kacho/cloud/iam/v1/fga_model.fga"

// verbRelationPrefix — приставка, по которой имя отношения модели опознаётся как
// глагольное. Она же — форма, в которой глагол попадает в кортеж.
const verbRelationPrefix = "v_"

// verbLiteral — запись реестра. Реестр есть ДАННЫЕ, а не перечисление в коде:
// запись, которой больше нечего сверять, — находка, а не «просто устарела»
// (см. TestVerbVocabularyRosterEntriesStillHaveSubject).
type verbLiteral struct {
	path       string // путь от корня репо
	varName    string // имя переменной верхнего уровня
	asRelation bool   // true → значения суть имена отношений "v_<глагол>", а не глаголы
	why        string // ЗАЧЕМ эта запись здесь (причина, не факт)
	retireWhen string // условие снятия, привязанное к ВНЕШНЕМУ факту
}

// verbLiteralRoster — все литеральные словари глаголов дерева. Перепись снята по
// имени механизма (объявление `[]string` из глаголов либо из имён `v_*`), а не по
// диффу той правки, в которой словарь заметили.
var verbLiteralRoster = []verbLiteral{
	{
		path: "services/iam/internal/domain/rule_verbs.go", varName: "ClosedVerbs",
		asRelation: false,
		why:        "словарь эмиттера: по нему решается, писать ли v_<глагол>",
		retireWhen: "переменная удалена из дерева (набор читается у типа, XC-3 S1Ф2)",
	},
	{
		path: "services/iam/internal/authzmap/fga_model_drift_test.go", varName: "closedVerbRelations",
		asRelation: true,
		why:        "словарь гейта дрейфа, намеренно продублированный ради независимости от эмиттера",
		retireWhen: "переменная удалена либо гейт дрейфа выводит её из модели",
	},
	{
		path: "services/iam/internal/domain/role_effective_verbs.go", varName: "crudOrder",
		asRelation: false,
		why:        "разворот `*` и канонический порядок в ПУБЛИЧНОМ превью роли (Role.effective_verbs)",
		retireWhen: "переменная удалена (порядок берётся из набора типа, XC-3 S1Ф2)",
	},
}

// TestVerbVocabularyLiteralsMatchModel — несущий гейт оси глаголов.
func TestVerbVocabularyLiteralsMatchModel(t *testing.T) {
	root := repoRoot(t)

	// --- предпосылка гейта: модель действительно разобрана ---
	model, defines := modelVerbVocabulary(t, root)
	if len(model) == 0 {
		t.Fatalf("из канонической модели %s не выведено ни одного глагола — предпосылка "+
			"гейта сломана, и его молчание ничего не доказывает (корень=%s)",
			canonicalVerbModelRelPath, root)
	}
	t.Logf("перепись: из модели выведено глаголов: %d (%s); объявлений `define %s*` прочитано: %d",
		len(model), strings.Join(model, ", "), verbRelationPrefix, defines)

	// --- перепись осмотренного: ноль находок ≠ ноль прочитанного ---
	if len(verbLiteralRoster) == 0 {
		t.Fatalf("реестр литеральных словарей пуст — гейту нечего сверять; "+
			"пустой реестр молчит по той же причине, по какой молчит исправное дерево (корень=%s)", root)
	}
	t.Logf("перепись: литеральных словарей в реестре: %d", len(verbLiteralRoster))

	// Каждый литерал — самостоятельный под-тест: расхождение сразу в НЕСКОЛЬКИХ
	// словарях обязано назвать ВСЕ координаты, а не первую. Ровно этот случай —
	// «правка плюс обновление зеркал» — и есть предмет гейта.
	for _, lit := range verbLiteralRoster {
		t.Run(lit.path+":"+lit.varName, func(t *testing.T) {
			got, ok := parseStringSliceVar(t, filepath.Join(root, lit.path), lit.varName)
			if !ok {
				t.Fatalf("%s: переменной %q в дереве нет. Если она удалена законно — снимите "+
					"запись реестра (условие снятия: %s), а не игнорируйте пропажу: следующий "+
					"литерал того же имени унаследует слепую зону молча",
					lit.path, lit.varName, lit.retireWhen)
			}
			want := model
			if lit.asRelation {
				want = withPrefix(model, verbRelationPrefix)
			}
			if d := symmetricDiff(got, want); len(d) != 0 {
				t.Fatalf("%s: %s разошёлся с канонической моделью %s: %v\nлитерал: %v\nмодель:  %v\n"+
					"Словарь глаголов ВЫВОДИТСЯ из модели. Дописать имя в литерал и в его зеркала — "+
					"не исход: эмиттер начнёт писать отношение, которого в модели нет, а владелец "+
					"модели такую запись отвергает окончательно.\nЗачем эта запись: %s",
					lit.path, lit.varName, canonicalVerbModelRelPath, d, got, want, lit.why)
			}
		})
	}
}

// TestVerbVocabularyRosterEntriesStillHaveSubject — самоистечение реестра.
//
// Запись, чья переменная исчезла из дерева, — НАХОДКА, а не «просто устарела»:
// следующий литерал того же имени унаследует слепую зону молча. Условие снятия у
// каждой записи привязано к ВНЕШНЕМУ факту (переменной в дереве), а не к
// наблюдаемому рядом, — иначе предикат снятия отменяется тем же изменением,
// которое его вызвало.
func TestVerbVocabularyRosterEntriesStillHaveSubject(t *testing.T) {
	root := repoRoot(t)
	if len(verbLiteralRoster) == 0 {
		t.Fatalf("реестр пуст — самоистечению нечего проверять")
	}
	alive := 0
	for _, lit := range verbLiteralRoster {
		if _, ok := parseStringSliceVar(t, filepath.Join(root, lit.path), lit.varName); !ok {
			t.Fatalf("запись реестра %s:%s больше нечего исключать — переменной в дереве нет. "+
				"Это находка: снимите запись (условие снятия: %s). Оставленная запись описывает "+
				"вчерашнее дерево и молча покроет собой следующий литерал того же имени",
				lit.path, lit.varName, lit.retireWhen)
		}
		alive++
	}
	t.Logf("перепись: записей реестра с живым предметом: %d из %d", alive, len(verbLiteralRoster))
}

// ---------------------------------------------------------------------------
// разбор модели
// ---------------------------------------------------------------------------

// modelVerbVocabulary возвращает отсортированный список глаголов, выведенный из
// имён отношений `v_*` канонической модели, и число прочитанных объявлений
// (перепись: «ноль глаголов» обязано быть отличимо от «ноль прочитанного»).
func modelVerbVocabulary(t *testing.T, root string) (verbs []string, defines int) {
	t.Helper()
	p := filepath.Join(root, canonicalVerbModelRelPath)
	data, err := os.ReadFile(p) //nolint:gosec // путь выведен из корня репо
	if err != nil {
		t.Fatalf("каноническая модель %s не прочитана (%v) — у гейта нет источника истины; "+
			"это ОТКАЗ, а не пропуск: отсутствие источника и есть тот дефект, который гейт ловит",
			canonicalVerbModelRelPath, err)
	}
	set := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		name, ok := defineRelationName(line)
		if !ok || !strings.HasPrefix(name, verbRelationPrefix) {
			continue
		}
		defines++
		set[strings.TrimPrefix(name, verbRelationPrefix)] = true
	}
	for v := range set {
		verbs = append(verbs, v)
	}
	sort.Strings(verbs)
	return verbs, defines
}

// defineRelationName достаёт имя отношения из строки вида `    define <name>: …`.
// Строка объявления типа (колонка 0) отношением не является.
func defineRelationName(line string) (string, bool) {
	trimmed := strings.TrimLeft(line, " \t")
	if len(trimmed) == len(line) { // не с отступом → не тело типа
		return "", false
	}
	const kw = "define "
	if !strings.HasPrefix(trimmed, kw) {
		return "", false
	}
	rest := trimmed[len(kw):]
	i := strings.IndexByte(rest, ':')
	if i <= 0 {
		return "", false
	}
	name := strings.TrimSpace(rest[:i])
	if name == "" || strings.ContainsAny(name, " \t") {
		return "", false
	}
	return name, true
}

// ---------------------------------------------------------------------------
// разбор Go-литералов (AST, не текст)
// ---------------------------------------------------------------------------

// parseStringSliceVar возвращает значения объявления `var <name> = []string{…}`
// верхнего уровня. Разбор синтаксического дерева, а не текста: то же имя
// встречается в комментарии, который переменную объясняет, и текстовый поиск
// зеленел бы на удалённой переменной с живым комментарием.
func parseStringSliceVar(t *testing.T, path, name string) ([]string, bool) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("%s: разбор не удался (%v) — гейт не вправе трактовать неразобранный "+
			"файл как «переменной нет»", path, err)
	}
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, ident := range vs.Names {
				if ident.Name != name || i >= len(vs.Values) {
					continue
				}
				lit, ok := vs.Values[i].(*ast.CompositeLit)
				if !ok {
					t.Fatalf("%s: %s объявлена не составным литералом — гейт читает только "+
						"`var %s = []string{…}`; смена формы объявления обязана быть осознанной",
						path, name, name)
				}
				out := make([]string, 0, len(lit.Elts))
				for _, e := range lit.Elts {
					bl, ok := e.(*ast.BasicLit)
					if !ok || bl.Kind != token.STRING {
						t.Fatalf("%s: %s несёт элемент, который не является строковым литералом — "+
							"вычисляемый словарь глаголов этот гейт не сверяет и молчать о нём не вправе",
							path, name)
					}
					s, err := strconv.Unquote(bl.Value)
					if err != nil {
						t.Fatalf("%s: %s: элемент %s не разкавычен (%v)", path, name, bl.Value, err)
					}
					out = append(out, s)
				}
				return out, true
			}
		}
	}
	return nil, false
}

// ---------------------------------------------------------------------------
// множества
// ---------------------------------------------------------------------------

// withPrefix возвращает копию списка с приставкой у каждого элемента.
func withPrefix(in []string, prefix string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		out = append(out, prefix+s)
	}
	return out
}

// symmetricDiff возвращает отсортированную симметрическую разность двух множеств
// строк с пометкой стороны: `+x` есть только слева, `-x` — только справа.
func symmetricDiff(got, want []string) []string {
	l := map[string]bool{}
	for _, s := range got {
		l[s] = true
	}
	r := map[string]bool{}
	for _, s := range want {
		r[s] = true
	}
	var out []string
	for s := range l {
		if !r[s] {
			out = append(out, "+"+s)
		}
	}
	for s := range r {
		if !l[s] {
			out = append(out, "-"+s)
		}
	}
	sort.Strings(out)
	return out
}
