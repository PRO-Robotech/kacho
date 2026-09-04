// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// descriptorfieldreader_test.go — гейт против ОБЪЯВЛЕННОГО, НО НИКЕМ НЕ
// ЧИТАЕМОГО поля дескриптора процесса (`servicecontract.Spec`).
//
// # Предмет
//
// Поле дескриптора выглядит исполненной работой: оно объявлено, у него есть
// godoc, сервис его заполняет, обзор диффа его одобряет. Читателя при этом может
// не быть вовсе — и тогда заполненное значение не меняет НИЧЕГО, а место, ради
// которого поле заводилось, остаётся незакрытым. Отличить такое поле от
// работающего нельзя ничем, кроме переписи читателей.
//
// Класс не гипотетический: он дал ДВА пункта из пяти в правке, которая этот гейт
// и завела. Загрузочный гейт мутаций был объявлен полем и не имел читателей в
// дереве вовсе, при том что три сервиса держали его у себя руками; порт проверки
// существования читался только СОБСТВЕННЫМИ отказами дескриптора, то есть
// проверялся на наличие и никогда не вызывался.
//
// # Три клетки, и они разные
//
//  1. поле без читателя ГДЕ БЫ ТО НИ БЫЛО — мёртвое объявление;
//  2. ПОРТ (поле, чей тип — интерфейс, объявленный тут же), не читаемый
//     носителем: смысл порта в том, чтобы его ВЫЗВАЛИ. Проверка «принесён ли
//     он» вызовом не является — так и жил порт существования;
//  3. ОСЬ, читаемая только предикатом объявленности: значение оси при этом
//     декоративно — его никто не разворачивает.
//
// # Почему разбор AST, а не текст
//
// Имена полей стоят в комментариях этих же пакетов чаще, чем в коде: текстовый
// предикат зеленел бы на собственной документации, а на снятой провязке молчал.
// AST отличает исполняемую часть от комментария и строки по построению.
//
// # Граница, названная честно
//
// Читатель распознаётся по ИМЕНИ селектора (`x.BootGate`), а не по типу
// выражения слева: разрешение типов потребовало бы загрузки пакетов целиком.
// Ошибка такого предиката ОДНОСТОРОННЯЯ — он может засчитать чужой селектор с
// тем же именем за читателя, то есть промолчать там, где надо покраснеть. Имена
// полей дескриптора в этих двух пакетах уникальны, поэтому сегодня совпадений
// нет; перепись печатает координату каждого засчитанного читателя, чтобы
// «читатель есть» можно было проверить глазом, а не принять на слово.
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

const (
	// contractPkgRel / carrierPkgRel — два пакета носителя контура. Первый решает,
	// что можно записать; второй — что с записанным делают.
	contractPkgRel = "pkg/servicecontract"
	carrierPkgRel  = "pkg/servicehost"

	// descriptorTypeName — тип по-процессной плоскости, чьи поля судятся.
	descriptorTypeName = "Spec"

	// axisTypeName — обёртка оси. Её значение обязано кем-то РАЗВОРАЧИВАТЬСЯ,
	// а не только проверяться на объявленность.
	axisTypeName = "Axis"
)

// axisSubstantiveReads — методы оси, разворачивающие ЗНАЧЕНИЕ. `Declared`
// сюда не входит намеренно: он отвечает «автор про ось что-то сказал» и
// одинаково истинен для оси, чьё значение никто не читает.
var axisSubstantiveReads = map[string]struct{}{
	"Get":                  {},
	"NotApplicableBecause": {},
}

// decorativeAxisException — ось, чьё значение сегодня никем не разворачивается.
//
// # Почему перечень, а не молчание
//
// Такая ось — находка по существу: объявить её обязаны, а прочесть некому,
// поэтому содержимое остаётся украшением. Но закрывать её значит менять
// поведение носителя, и делаться это должно своим изменением, а не побочно.
// Молчаливое «гейт их не видит» вернуло бы ровно ту неразличимость, ради которой
// гейт написан.
//
// # Самоистечение
//
// Запись, чью ось УЖЕ читают, — сама находка: она готова укрыть следующую
// декоративную ось, которую здесь заведут. Предикат снятия внешний — состояние
// дерева, — а не выведенный из этого же перечня.
type decorativeAxisException struct {
	// why — почему запись существует и что должно случиться, чтобы она ушла.
	why string
}

var decorativeAxisExceptions = map[string]decorativeAxisException{
	"Registers": {why: "перечень типов, которые сервис регистрирует у владельца прав, сегодня " +
		"только объявляется: носитель его не разворачивает. Запись уйдёт, когда перечень начнут " +
		"сверять с пообъектными типами каталога — тип-цель авторизации без регистрации означает " +
		"ресурс без владельца"},
}

// fieldReaderFinding — одна находка с координатой объявления.
type fieldReaderFinding struct {
	field string
	where string // файл:строка объявления поля
	what  string
}

// fieldReaderResult — исход обхода вместе с ПЕРЕПИСЬЮ осмотренного.
type fieldReaderResult struct {
	findings []fieldReaderFinding
	fields   int
	ports    int
	axes     int
	files    int
	readers  map[string][]string // поле -> координаты читателей
	summary  string
}

// TestEveryDescriptorFieldHasAReader — сам гейт.
//
// Что делать, если сработал, — три исхода, четвёртого нет:
//
//  1. поле нужно → провязать его в носителе (`pkg/servicehost`), а не объявлять
//     исключение: перечень ниже закрыт и пополняется вместе с решением о том,
//     почему значение остаётся декоративным;
//  2. поле не нужно → снять его из дескриптора вместе с godoc и заполнением;
//  3. это не поле дескриптора, а совпадение формы → уточнить распознавание.
//
// Проверено инъекцией в обе стороны (`descriptorfieldreader_injection_test.go`).
func TestEveryDescriptorFieldHasAReader(t *testing.T) {
	res := auditDescriptorFieldReaders(t, repoRoot(t))
	t.Log(res.summary)

	if res.fields == 0 {
		t.Fatalf("не осмотрено ни одного поля дескриптора — «ноль находок» здесь означало бы "+
			"«ноль прочитанного», а не исправное дерево.\n%s", res.summary)
	}
	if res.files == 0 {
		t.Fatalf("не прочитано ни одного файла носителя — читателей не нашлось бы ни у одного поля, "+
			"и гейт покраснел бы на всём подряд по причине, не имеющей отношения к предмету.\n%s", res.summary)
	}

	// (1) находки.
	var lines []string
	for _, f := range res.findings {
		lines = append(lines, fmt.Sprintf("%s (%s): %s", f.field, f.where, f.what))
	}
	// (2) САМОИСТЕЧЕНИЕ: исключение, чью ось уже читают.
	var expired []string
	for field, ex := range decorativeAxisExceptions {
		if len(res.readers[field]) == 0 {
			continue
		}
		if !res.decorative(field) {
			expired = append(expired, fmt.Sprintf("%s: исключение потеряло предмет — значение оси "+
				"теперь разворачивают (%s). Причина, записанная при заведении: %s. Снимите запись: "+
				"оставленная, она укроет следующую декоративную ось", field,
				strings.Join(res.readers[field], ", "), ex.why))
		}
	}

	sort.Strings(lines)
	sort.Strings(expired)
	if len(lines) > 0 {
		t.Errorf("поля дескриптора без читателя (%d):\n  %s", len(lines), strings.Join(lines, "\n  "))
	}
	if len(expired) > 0 {
		t.Errorf("исключения без предмета (%d):\n  %s", len(expired), strings.Join(expired, "\n  "))
	}
}

// decorative сообщает, осталась ли ось читаемой ТОЛЬКО предикатом объявленности.
func (r fieldReaderResult) decorative(field string) bool {
	for _, where := range r.readers[field] {
		if strings.Contains(where, "→ значение") {
			return false
		}
	}
	return true
}

// specField — поле дескриптора вместе с тем, чем оно является.
type specField struct {
	name    string
	where   string
	isPort  bool // тип — интерфейс, объявленный в том же пакете
	isAxis  bool
	portVia string // имя интерфейса, ради текста находки
}

// auditDescriptorFieldReaders обходит ОБА пакета носителя.
//
// Состав берётся у git (`treecorpus`), а не с диска: вердикт обязан быть
// свойством коммита, а не рабочего каталога с чужими распаковками.
func auditDescriptorFieldReaders(t *testing.T, root string) fieldReaderResult {
	t.Helper()
	res := fieldReaderResult{readers: map[string][]string{}}
	fset := token.NewFileSet()

	contractFiles := trackedGoFiles(t, filepath.Join(root, contractPkgRel))
	carrierFiles := trackedGoFiles(t, filepath.Join(root, carrierPkgRel))
	res.files = len(contractFiles) + len(carrierFiles)

	// 1. Интерфейсы, объявленные в пакете дескриптора: поле такого типа есть ПОРТ,
	//    и смысл порта в том, чтобы его вызвали.
	ports := map[string]struct{}{}
	for _, abs := range contractFiles {
		file := parseDescriptorGo(t, fset, abs)
		ast.Inspect(file, func(n ast.Node) bool {
			ts, ok := n.(*ast.TypeSpec)
			if !ok {
				return true
			}
			if _, isIface := ts.Type.(*ast.InterfaceType); isIface {
				ports[ts.Name.Name] = struct{}{}
			}
			return true
		})
	}

	// 2. Поля дескриптора.
	var fields []specField
	for _, abs := range contractFiles {
		file := parseDescriptorGo(t, fset, abs)
		ast.Inspect(file, func(n ast.Node) bool {
			ts, ok := n.(*ast.TypeSpec)
			if !ok || ts.Name.Name != descriptorTypeName {
				return true
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				return true
			}
			for _, fld := range st.Fields.List {
				for _, name := range fld.Names {
					if !name.IsExported() {
						continue
					}
					f := specField{
						name:  name.Name,
						where: fmt.Sprintf("%s:%d", relTo(root, abs), fset.Position(name.Pos()).Line),
					}
					for _, id := range typeIdents(fld.Type) {
						if id == axisTypeName {
							f.isAxis = true
						}
						if _, isPort := ports[id]; isPort {
							f.isPort, f.portVia = true, id
						}
					}
					fields = append(fields, f)
				}
			}
			return true
		})
	}
	res.fields = len(fields)

	// 3. Читатели — по обоим пакетам, с координатой и с тем, ЧТО именно прочитано.
	collect := func(files []string, pkg string) {
		for _, abs := range files {
			file := parseDescriptorGo(t, fset, abs)
			ast.Inspect(file, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				where := fmt.Sprintf("%s %s:%d", pkg, relTo(root, abs), fset.Position(sel.Pos()).Line)
				// Чтение самого поля.
				res.readers[sel.Sel.Name] = append(res.readers[sel.Sel.Name], where)
				// Разворот значения оси: `x.<Поле>.Get(...)` — селектор поверх
				// селектора. Помечаем стрелкой, чтобы клетка «только объявленность»
				// отличалась от клетки «значение прочитано».
				if inner, ok := sel.X.(*ast.SelectorExpr); ok {
					if _, substantive := axisSubstantiveReads[sel.Sel.Name]; substantive {
						res.readers[inner.Sel.Name] = append(res.readers[inner.Sel.Name],
							where+" → значение")
					}
				}
				return true
			})
		}
	}
	collect(contractFiles, "дескриптор")
	collect(carrierFiles, "носитель")

	// 4. Судейство.
	for _, f := range fields {
		if f.isAxis {
			res.axes++
		}
		if f.isPort {
			res.ports++
		}
		readers := res.readers[f.name]
		if len(readers) == 0 {
			res.findings = append(res.findings, fieldReaderFinding{
				field: f.name, where: f.where,
				what: "объявлено и не читается НИГДЕ — ни отказами дескриптора, ни носителем. " +
					"Заполненное значение не меняет ничего, а место, ради которого поле заводилось, " +
					"остаётся незакрытым",
			})
			continue
		}
		if f.isPort && !hasReaderIn(readers, "носитель") {
			res.findings = append(res.findings, fieldReaderFinding{
				field: f.name, where: f.where,
				what: "порт (" + f.portVia + ") читается только отказами дескриптора: его проверяют " +
					"на наличие и никогда не вызывают. Смысл порта в вызове, поэтому «принесён» " +
					"вызовом не является",
			})
			continue
		}
		if f.isAxis && res.decorative(f.name) {
			if _, excused := decorativeAxisExceptions[f.name]; excused {
				continue
			}
			res.findings = append(res.findings, fieldReaderFinding{
				field: f.name, where: f.where,
				what: "ось читается только предикатом объявленности: ЗНАЧЕНИЕ её никто не " +
					"разворачивает (нет ни Get, ни NotApplicableBecause), то есть содержимое оси " +
					"декоративно",
			})
		}
	}

	res.summary = fmt.Sprintf(
		"перепись: полей дескриптора %d (из них портов %d, осей %d), файлов носителя прочитано %d, "+
			"полей с читателем %d, находок %d, исключений объявлено %d",
		res.fields, res.ports, res.axes, res.files, countWithReaders(fields, res),
		len(res.findings), len(decorativeAxisExceptions))
	return res
}

func countWithReaders(fields []specField, res fieldReaderResult) int {
	n := 0
	for _, f := range fields {
		if len(res.readers[f.name]) > 0 {
			n++
		}
	}
	return n
}

func hasReaderIn(readers []string, pkg string) bool {
	for _, r := range readers {
		if strings.HasPrefix(r, pkg+" ") {
			return true
		}
	}
	return false
}

// typeIdents собирает имена идентификаторов типа, включая параметры обобщения:
// `Axis[BootGate]` даёт и обёртку, и то, что в неё вложено. Без второго порт,
// спрятанный в ось, перестал бы быть портом для гейта.
func typeIdents(e ast.Expr) []string {
	var out []string
	ast.Inspect(e, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			out = append(out, id.Name)
		}
		return true
	})
	return out
}

func parseDescriptorGo(t *testing.T, fset *token.FileSet, abs string) *ast.File {
	t.Helper()
	file, err := parser.ParseFile(fset, abs, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("разбор %s: %v", abs, err)
	}
	return file
}

// trackedGoFiles — не-тестовые .go-файлы каталога, ОТСЛЕЖИВАЕМЫЕ git.
func trackedGoFiles(t *testing.T, dir string) []string {
	t.Helper()
	tracked, err := treecorpus.Under(dir)
	if err != nil {
		t.Fatalf("состав дерева под %s не читается: %v", dir, err)
	}
	var out []string
	for _, abs := range tracked {
		if !strings.HasSuffix(abs, ".go") || strings.HasSuffix(abs, "_test.go") {
			continue
		}
		out = append(out, abs)
	}
	sort.Strings(out)
	return out
}
