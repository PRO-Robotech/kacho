// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package engineplaces строит ПЕРЕПИСЬ МЕСТ обращения к внешнему движку прав:
// сколько мест в дереве его спрашивают, в скольких файлах они лежат, какого
// рода каждый вопрос и что из сырого совпадения вычтено — с причиной.
//
// # Почему по СВОЙСТВУ, а не по имени
//
// Предикат по имени меряет соглашение об именовании, а не предмет: он пропустит
// место, названное иначе, и найдёт комментарий, объясняющий это же место. На
// этом уже обожглись — три числа эпика R7-3 получены предикатом по имени либо в
// чужой единице счёта. Здесь дискриминатор — ТИП клиента движка и порты,
// которым этот тип удовлетворяет; имя тут ровно одно и оно объявлено якорем
// (`AnchorType`), а всё остальное — дом типа, перечень его методов, множество
// портов — выводит КОМПИЛЯТОР.
//
// # Зернистость: классифицируется МЕСТО, а не интерфейс
//
// Удовлетворение интерфейсу в Go структурно, поэтому один и тот же по форме
// порт внутри службы прав связан с клиентом движка, а у соседа — с клиентом к
// нашему СОБСТВЕННОМУ методу (`pkg/authz.CheckClient` — gRPC к
// `InternalIAMService.Check`). Поинтерфейсное правило вычитает такой порт
// целиком, выбрасывая настоящие места, и перепись при этом выглядит ЧИЩЕ — то
// есть ошибается в сторону, которую не видно. Поэтому решение принимается по
// МЕСТУ, а третьим условием (сверх имени метода и его сигнатуры) стоит МЕСТО
// ОБЪЯВЛЕНИЯ конкретного типа: где написана реализация. Оно берётся из
// загруженного пакета (`types.Object.Pkg().Path()`), а не сравнением пути в
// исходнике: путь пакета мутабелен ровно как имя, и переезд каталога пережил бы
// строковый предикат.
//
// По месту надёжнее, чем по графу импортов: импорт бывает транзитивным и
// переживает снятие вызова, а место объявления переезжает только вместе с самим
// типом.
//
// # Что перепись объявляет своей ГРАНИЦЕЙ
//
// Дискриминатор видит ровно то, что проходит через якорный тип. Второй клиент
// движка, живущий вне его дома со своим транспортом и своими адресами
// (измерительный прибор `tools/authzformbench`), не виден ей BY CONSTRUCTION.
// Это не дефект — это граница, и она ПЕЧАТАЕТСЯ: перепись, молчащая о том, чего
// она не видит, зеленеет при живом втором клиенте.
package engineplaces

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/types"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// AnchorType — имя конкретного типа клиента движка. ЕДИНСТВЕННОЕ имя, которое
// перепись знает; всё прочее выводится компилятором. Переименован или снят —
// перепись обязана заявить об этом сама (`Census.AnchorDeclarations`), а не
// молча вернуть ноль мест: снаружи ноль мест и отсутствие якоря неотличимы.
const AnchorType = "OpenFGAHTTPClient"

// NameOnlyNeedle — НАИВНЫЙ предикат, с которым перепись себя сравнивает. Он
// намеренно по имени: его назначение — показать расхождение, а не считать
// предмет. Числа по нему живут в `Census.NameOnly` и НИКОГДА не вычитаются из
// переписи: у них другая единица счёта.
const NameOnlyNeedle = "openfga"

// Единицы счёта. Вычет и перепись обязаны считать ОДНО И ТО ЖЕ: наблюдалось
// «60 − 5», где из мест вычитали удовлетворяющие типы — разное из разного.
const (
	UnitPlace = "место"
	UnitFile  = "файл"
)

// Закрытый перечень причин вычета. Открытый перечень делает «округлил»
// выразимым под другим именем.
const (
	// CategorySelfCall — адаптер зовёт сам себя: вызов на якорном типе внутри
	// его собственного дома. Это его механика, а не место дерева, обращающееся
	// к движку.
	CategorySelfCall = "самовызов адаптера"
	// CategoryNamesake — структурный однофамилец: место идёт через порт,
	// которому якорный тип удовлетворяет СЛУЧАЙНО, а реализация, связанная в
	// этом месте, объявлена вне дома движка.
	CategoryNamesake = "структурный однофамилец"
	// CategoryTestRig — оснастка проб: непроверочный файл, существующий, чтобы
	// обслуживать пробы.
	CategoryTestRig = "оснастка проб"
	// CategoryProse — упоминание в прозе: файл называет движок только в
	// комментарии или строковом литерале. Типизированный дискриминатор не видит
	// его BY CONSTRUCTION; категория существует, чтобы расхождение с наивным
	// предикатом было названо, а не «округлено».
	CategoryProse = "упоминание в прозе"
	// CategoryGenerated — порождённый стаб: файл сгенерирован и правке не
	// подлежит.
	CategoryGenerated = "порождённый стаб"
)

// Categories — закрытый перечень категорий вычета.
func Categories() []string {
	return []string{
		CategorySelfCall, CategoryNamesake, CategoryTestRig,
		CategoryProse, CategoryGenerated,
	}
}

// testRigSegments — сегменты пути пакета, объявляющие оснастку проб. Перечень
// закрыт и ИСТЕКАЕТ САМ: сегмент, не совпавший ни с одним пакетом, — находка,
// а не «на всякий случай».
var testRigSegments = []string{"testsupport"}

// Place — одно место обращения к движку.
type Place struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	Pkg    string `json:"pkg"`
	Method string `json:"method"`
	Kind   string `json:"kind"`
	// Via — порт, через который идёт вызов; пусто у прямого вызова на якорном
	// типе.
	Via string `json:"via,omitempty"`
	// ViaAmbiguous — порт структурно неоднозначен: его реализуют и якорный тип,
	// и тип, объявленный вне дома движка. Место засчитано по третьему условию;
	// признак печатается, чтобы читатель видел, какие места стоят на нём.
	ViaAmbiguous bool `json:"via_ambiguous,omitempty"`
	// MethodValue — метод не ВЫЗВАН здесь, а взят ЗНАЧЕНИЕМ и отдан дальше.
	// Это тоже место обращения: вызов состоится, просто в другом кадре. Не
	// считать его значило бы занизить перепись ровно там, где движок передают
	// как функцию.
	MethodValue bool `json:"method_value,omitempty"`
}

// Subtraction — одно вычтенное из сырого совпадения, с причиной и единицей.
type Subtraction struct {
	Category string `json:"category"`
	Unit     string `json:"unit"`
	File     string `json:"file"`
	Line     int    `json:"line,omitempty"`
	Pkg      string `json:"pkg,omitempty"`
	Method   string `json:"method,omitempty"`
	Reason   string `json:"reason"`
}

// Port — интерфейс, которому якорный тип удовлетворяет структурно.
type Port struct {
	Type string `json:"type"`
	// Impls — конкретные типы дерева, реализующие этот порт, с домом каждого.
	Impls []string `json:"impls"`
	// ForeignImpls — реализации, объявленные ВНЕ дома движка. Именно они делают
	// порт структурно неоднозначным, и именно их называет причина вычета.
	ForeignImpls []string `json:"foreign_impls,omitempty"`
	// Ambiguous — среди реализаций есть объявленная ВНЕ дома движка.
	Ambiguous bool `json:"ambiguous"`
	Methods   int  `json:"methods"`
}

// TestRigSegment — объявленный сегмент оснастки проб и число совпавших пакетов.
type TestRigSegment struct {
	Segment string `json:"segment"`
	Matched int    `json:"matched"`
}

// Boundary — то, чего перепись не видит, названное вслух.
type Boundary struct {
	Name  string   `json:"name"`
	Count int      `json:"count"`
	Note  string   `json:"note"`
	Items []string `json:"items,omitempty"`
}

// ScanCensus — объём осмотренного. «Ноль находок» обязано быть отличимо от
// «ноль прочитанного».
type ScanCensus struct {
	Requested   int      `json:"requested"`
	Loaded      int      `json:"loaded"`
	ProdFiles   int      `json:"prod_files"`
	CallSites   int      `json:"call_sites"`
	MethodVals  int      `json:"method_values"`
	NamedTypes  int      `json:"named_types"`
	SkippedPkgs []string `json:"skipped_pkgs"`
}

// NameOnlyReconciliation — разбор расхождения наивного предиката с
// дискриминатором, В ЕДИНИЦЕ «ФАЙЛ». Никогда не вычитается из переписи мест:
// единицы разные.
type NameOnlyReconciliation struct {
	// WithPlaces — файлов, которые наивный предикат назвал И в которых
	// дискриминатор нашёл место.
	WithPlaces int `json:"with_places"`
	// MissedByName — файлов с местами, которых наивный предикат НЕ назвал.
	// Это вторая сторона контроля: предикат по имени не только завышает
	// прозой и стабами, он ещё и ПРОПУСКАЕТ настоящие места — те, что
	// названы иначе.
	MissedByName int      `json:"missed_by_name"`
	MissedFiles  []string `json:"missed_files,omitempty"`
	Generated    int      `json:"generated"`
	Prose        int      `json:"prose"`
	TestRig      int      `json:"test_rig"`
	// Wiring — файл называет движок и держит ЯКОРНЫЙ ТИП (конструирует его или
	// объявляет им поле), но вызова в нём нет: это связывание, а не обращение.
	Wiring int `json:"wiring"`
	// AdapterHome — файлы самого дома движка: их вызовы вычтены как самовызов.
	AdapterHome  int `json:"adapter_home"`
	SecondClient int `json:"second_client"`
}

// NameOnlyCensus — тот же вопрос НАИВНЫМ предикатом, в СВОЕЙ единице счёта.
type NameOnlyCensus struct {
	Needle     string                 `json:"needle"`
	Files      int                    `json:"files"`
	TestFiles  int                    `json:"test_files"`
	Examples   []string               `json:"examples,omitempty"`
	Reconciled NameOnlyReconciliation `json:"reconciled"`
}

// Census — перепись целиком.
type Census struct {
	Root                string           `json:"root"`
	Patterns            []string         `json:"patterns"`
	Anchor              string           `json:"anchor"`
	AnchorPkg           string           `json:"anchor_pkg"`
	AnchorHomeTree      string           `json:"anchor_home_tree"`
	AnchorDeclarations  int              `json:"anchor_declarations"`
	Places              []Place          `json:"places"`
	Subtractions        []Subtraction    `json:"subtractions"`
	Ports               []Port           `json:"ports"`
	Methods             []MethodKind     `json:"methods"`
	UnclassifiedMethods []string         `json:"unclassified_methods"`
	TestRigSegments     []TestRigSegment `json:"test_rig_segments"`
	Boundaries          []Boundary       `json:"boundaries"`
	Scan                ScanCensus       `json:"scan"`
	NameOnly            NameOnlyCensus   `json:"name_only"`
	// Errors — предпосылка не выполнена: эти пакеты НЕ протипизированы, их
	// места невидимы. Непустой список делает перепись негодной ЦЕЛИКОМ.
	Errors []string `json:"errors"`

	// httpPkgs — каталоги пакетов, импортирующих `net/http`: признак СВОЕГО
	// транспорта. Выводится из импортов, а не из имени файла.
	httpPkgs map[string]bool

	// scannedFiles — непроверочные файлы, которые обход КОМПИЛЯТОРА прочитал.
	// Нужны, чтобы отличить «файл называет движок и мест в нём нет» от «файл
	// вообще не читался»: без этого граница «вне обхода» была бы догадкой.
	scannedFiles map[string]bool
}

// Void — предпосылка не выполнена: переписью пользоваться нельзя.
func (c *Census) Void() bool { return len(c.Errors) > 0 }

// FileCount — вторая единица счёта: в скольких файлах лежат места. Одна единица
// без другой не отвечает ни на «сколько чинить», ни на «сколько трогать».
func (c *Census) FileCount() int {
	files := map[string]bool{}
	for _, p := range c.Places {
		files[p.File] = true
	}
	return len(files)
}

// KindCounts — места по родам вопроса.
func (c *Census) KindCounts() map[string]int {
	m := map[string]int{}
	for _, p := range c.Places {
		m[p.Kind]++
	}
	return m
}

// Build строит перепись по дереву в root для указанных пакетных шаблонов
// (по умолчанию — всё дерево модуля).
func Build(root string, patterns ...string) (*Census, error) {
	if len(patterns) == 0 {
		patterns = []string{"./..."}
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	res, lerr := load(abs, patterns)
	if lerr != nil {
		return nil, lerr
	}
	res.root = abs

	c := &Census{
		Root:     abs,
		Patterns: patterns,
		Anchor:   AnchorType,
		Errors:   res.Errors,
		Scan: ScanCensus{
			Requested:   res.Requested,
			Loaded:      len(res.Own),
			SkippedPkgs: res.Skipped,
		},
	}
	c.httpPkgs = httpImporters(res)
	c.scannedFiles = map[string]bool{}
	for _, p := range res.Own {
		c.Scan.ProdFiles += len(p.Files)
		for _, f := range p.Files {
			c.scannedFiles[f] = true
		}
	}

	anchor, decls := findAnchor(res.All)
	c.AnchorDeclarations = decls
	if anchor == nil {
		c.Errors = append(c.Errors,
			fmt.Sprintf("якорный тип %q не найден ни в одном протипизированном пакете: "+
				"перепись не знает, что считать движком, и её ноль означал бы отсутствие ПРИБОРА, "+
				"а не отсутствие мест", AnchorType))
		c.nameOnlyPass(abs, nil, nil)
		return c, nil
	}
	c.AnchorPkg = anchor.Obj().Pkg().Path()
	c.AnchorHomeTree = homeTree(c.AnchorPkg)

	methodNames := methodNamesOf(anchor)
	c.Methods, c.UnclassifiedMethods = classifyMethods(methodNames)

	ports, named := discoverPorts(res.All, anchor)
	c.Ports = ports
	c.Scan.NamedTypes = named

	portByType := map[string]Port{}
	for _, p := range ports {
		portByType[p.Type] = p
	}

	rig := map[string]int{}
	c.walkPlaces(res, anchor, portByType, rig)

	for _, seg := range testRigSegments {
		c.TestRigSegments = append(c.TestRigSegments, TestRigSegment{Segment: seg, Matched: rig[seg]})
	}

	placeFiles := map[string]bool{}
	for _, p := range c.Places {
		placeFiles[p.File] = true
	}
	c.nameOnlyPass(abs, placeFiles, anchorRefFiles(res, anchor))

	sort.Slice(c.Places, func(i, j int) bool {
		if c.Places[i].File != c.Places[j].File {
			return c.Places[i].File < c.Places[j].File
		}
		return c.Places[i].Line < c.Places[j].Line
	})
	sort.Slice(c.Subtractions, func(i, j int) bool {
		if c.Subtractions[i].Category != c.Subtractions[j].Category {
			return c.Subtractions[i].Category < c.Subtractions[j].Category
		}
		if c.Subtractions[i].File != c.Subtractions[j].File {
			return c.Subtractions[i].File < c.Subtractions[j].File
		}
		return c.Subtractions[i].Line < c.Subtractions[j].Line
	})
	sort.Strings(c.Errors)
	return c, nil
}

// findAnchor ищет якорный тип и СЧИТАЕТ его объявления: два объявления одного
// имени означают, что перепись не знает, о каком из них она.
func findAnchor(all []*loadedPkg) (*types.Named, int) {
	var found *types.Named
	decls := 0
	for _, p := range all {
		if p.Pkg == nil {
			continue
		}
		obj := p.Pkg.Scope().Lookup(AnchorType)
		if obj == nil {
			continue
		}
		tn, ok := obj.(*types.TypeName)
		if !ok {
			continue
		}
		named, ok := tn.Type().(*types.Named)
		if !ok {
			continue
		}
		decls++
		if found == nil {
			found = named
		}
	}
	return found, decls
}

// methodNamesOf — перечень методов ВЫВОДИТСЯ ИЗ ТИПА, а не выписывается.
func methodNamesOf(named *types.Named) []string {
	ms := types.NewMethodSet(types.NewPointer(named))
	names := make([]string, 0, ms.Len())
	for i := 0; i < ms.Len(); i++ {
		names = append(names, ms.At(i).Obj().Name())
	}
	sort.Strings(names)
	return names
}

// discoverPorts находит интерфейсы, которым якорный тип удовлетворяет, и по
// каждому — конкретные типы дерева, которые его реализуют. Именно это даёт
// третье условие: реализация, объявленная ВНЕ дома движка, делает порт
// структурно неоднозначным.
func discoverPorts(all []*loadedPkg, anchor *types.Named) ([]Port, int) {
	anchorPtr := types.NewPointer(anchor)
	anchorPkg := anchor.Obj().Pkg().Path()

	type ifaceRec struct {
		name  string
		iface *types.Interface
	}
	var ifaces []ifaceRec
	var concretes []*types.Named
	namedCount := 0

	for _, p := range all {
		if p.Pkg == nil {
			continue
		}
		scope := p.Pkg.Scope()
		for _, n := range scope.Names() {
			tn, ok := scope.Lookup(n).(*types.TypeName)
			if !ok || tn.IsAlias() {
				continue
			}
			named, ok := tn.Type().(*types.Named)
			if !ok {
				continue
			}
			namedCount++
			if iface, ok := named.Underlying().(*types.Interface); ok {
				if iface.NumMethods() > 0 && types.Implements(anchorPtr, iface) {
					ifaces = append(ifaces, ifaceRec{name: p.Pkg.Path() + "." + n, iface: iface})
				}
				continue
			}
			if named.NumMethods() > 0 {
				concretes = append(concretes, named)
			}
		}
	}

	ports := make([]Port, 0, len(ifaces))
	for _, ir := range ifaces {
		port := Port{Type: ir.name, Methods: ir.iface.NumMethods()}
		for _, ct := range concretes {
			if !types.Implements(ct, ir.iface) && !types.Implements(types.NewPointer(ct), ir.iface) {
				continue
			}
			home := ct.Obj().Pkg().Path()
			port.Impls = append(port.Impls, home+"."+ct.Obj().Name())
			if home != anchorPkg {
				port.Ambiguous = true
				port.ForeignImpls = append(port.ForeignImpls, home+"."+ct.Obj().Name())
			}
		}
		sort.Strings(port.Impls)
		sort.Strings(port.ForeignImpls)
		ports = append(ports, port)
	}
	sort.Slice(ports, func(i, j int) bool { return ports[i].Type < ports[j].Type })
	return ports, namedCount
}

// homeTree — дерево службы, которой принадлежит дом движка. Выводится из пути
// пакета, а не выписывается: сегмент `services/<имя>` плюс сам путь как запасной
// вариант, если раскладка иная.
func homeTree(pkg string) string {
	const seg = "/services/"
	i := strings.Index(pkg, seg)
	if i < 0 {
		return pkg
	}
	rest := pkg[i+len(seg):]
	if j := strings.Index(rest, "/"); j >= 0 {
		return pkg[:i+len(seg)+j]
	}
	return pkg
}

// anchorRefFiles — файлы, которые ССЫЛАЮТСЯ на якорный тип (конструируют его,
// объявляют им поле, приводят к нему). Это СВЯЗЫВАНИЕ, а не обращение: вызова
// здесь нет, и местом такой файл не является. Нужны, чтобы граница «второй
// клиент» не собрала в себя композиционный корень движка и не превратилась из
// сигнала в шум — гейт, который невозможно прочитать, отключают первым.
func anchorRefFiles(res *loadResult, anchor *types.Named) map[string]bool {
	out := map[string]bool{}
	if anchor == nil {
		return out
	}
	obj := anchor.Obj()
	for _, p := range res.All {
		for id, used := range p.Info.Uses {
			if used != obj {
				continue
			}
			pos := res.FileSet.Position(id.Pos())
			out[relTo(res.root, pos.Filename)] = true
		}
	}
	return out
}

// walkPlaces обходит обращения и раскладывает их на места и вычеты.
//
// Обращением считается ВЫЗОВ метода и ВЗЯТИЕ метода значением: во втором случае
// вызов состоится в другом кадре, и не считать его значило бы занизить перепись
// там, где движок передают функцией.
//
// Продвижение через встраивание НЕ удваивает счёт: `w.Check(…)` на обёртке,
// встроившей порт, имеет получателем саму обёртку — конкретный тип, не движок, —
// а собственный вызов обёртки `w.Порт.Check(…)` уже посчитан. Место одно, и оно
// там, где написано обращение к движку.
func (c *Census) walkPlaces(res *loadResult, anchor *types.Named, ports map[string]Port, rig map[string]int) {
	anchorPkg := anchor.Obj().Pkg().Path()

	for _, p := range res.Own {
		rigSeg := testRigSegmentOf(p.Path)
		if rigSeg != "" {
			rig[rigSeg]++
		}

		called := map[*ast.SelectorExpr]bool{}
		for _, f := range p.Syn {
			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
					called[sel] = true
				}
				return true
			})
		}

		for sel, s := range p.Info.Selections {
			if s.Kind() != types.MethodVal {
				continue
			}
			isCall := called[sel]
			if isCall {
				c.Scan.CallSites++
			} else {
				c.Scan.MethodVals++
			}
			pos := res.FileSet.Position(sel.Sel.Pos())
			file := relTo(c.Root, pos.Filename)
			line := pos.Line
			method := sel.Sel.Name

			// (1) Прямое обращение на якорном типе.
			if named, ok := namedOf(s.Recv()); ok &&
				named.Obj().Name() == anchor.Obj().Name() &&
				named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == anchorPkg {
				switch {
				case p.Path == anchorPkg:
					c.Subtractions = append(c.Subtractions, Subtraction{
						Category: CategorySelfCall, Unit: UnitPlace,
						File: file, Line: line, Pkg: p.Path, Method: method,
						Reason: "обращение внутри дома адаптера (" + shortPkg(anchorPkg) +
							"): собственная механика клиента, а не место дерева, обращающееся к движку",
					})
				case rigSeg != "":
					c.Subtractions = append(c.Subtractions, Subtraction{
						Category: CategoryTestRig, Unit: UnitPlace,
						File: file, Line: line, Pkg: p.Path, Method: method,
						Reason: "пакет несёт объявленный сегмент оснастки проб «" + rigSeg +
							"»: непроверочный файл, существующий ради проб",
					})
				default:
					c.Places = append(c.Places, Place{
						File: file, Line: line, Pkg: p.Path,
						Method: method, Kind: kindOf(method), MethodValue: !isCall,
					})
				}
				continue
			}

			// (2) Обращение через порт, которому якорный тип удовлетворяет.
			iname, ok := interfaceNameOf(s.Recv())
			if !ok {
				continue
			}
			port, ok := ports[iname]
			if !ok {
				continue
			}
			switch {
			case rigSeg != "":
				c.Subtractions = append(c.Subtractions, Subtraction{
					Category: CategoryTestRig, Unit: UnitPlace,
					File: file, Line: line, Pkg: p.Path, Method: method,
					Reason: "пакет несёт объявленный сегмент оснастки проб «" + rigSeg + "»",
				})
			case port.Ambiguous && !inTree(p.Path, c.AnchorHomeTree):
				c.Subtractions = append(c.Subtractions, Subtraction{
					Category: CategoryNamesake, Unit: UnitPlace,
					File: file, Line: line, Pkg: p.Path, Method: method,
					Reason: "порт " + shortPkg(iname) + " структурно неоднозначен (" +
						strconv.Itoa(len(port.Impls)) + " реализаций, из них вне дома движка — " +
						strings.Join(shortAll(port.ForeignImpls), ", ") +
						"), а место лежит вне дерева дома движка " + shortPkg(c.AnchorHomeTree) +
						": связанная здесь реализация объявлена не в доме движка",
				})
			default:
				c.Places = append(c.Places, Place{
					File: file, Line: line, Pkg: p.Path,
					Method: method, Kind: kindOf(method),
					Via: iname, ViaAmbiguous: port.Ambiguous, MethodValue: !isCall,
				})
			}
		}
	}
}

func relTo(root, path string) string {
	if rel, err := filepath.Rel(root, path); err == nil {
		return filepath.ToSlash(rel)
	}
	return filepath.ToSlash(path)
}

// shortPkg — путь без префикса модуля: перечень, который невозможно прочитать,
// отключают первым.
func shortPkg(p string) string {
	const mod = "github.com/PRO-Robotech/kacho/"
	return strings.TrimPrefix(p, mod)
}

func shortAll(in []string) []string {
	out := make([]string, 0, len(in))
	for i, s := range in {
		if i == 3 {
			out = append(out, "…ещё "+strconv.Itoa(len(in)-3))
			break
		}
		out = append(out, shortPkg(s))
	}
	return out
}

func kindOf(method string) string {
	if k, ok := methodKind[method]; ok {
		return k
	}
	// Метод без рода уже назван находкой в UnclassifiedMethods; место при этом
	// не теряется — оно получает пустой род и роняет проверку рода.
	return ""
}

func namedOf(t types.Type) (*types.Named, bool) {
	if ptr, ok := t.(*types.Pointer); ok {
		t = ptr.Elem()
	}
	named, ok := t.(*types.Named)
	if !ok || named.Obj() == nil || named.Obj().Pkg() == nil {
		return nil, false
	}
	return named, true
}

func interfaceNameOf(t types.Type) (string, bool) {
	named, ok := namedOf(t)
	if !ok {
		return "", false
	}
	if _, ok := named.Underlying().(*types.Interface); !ok {
		return "", false
	}
	return named.Obj().Pkg().Path() + "." + named.Obj().Name(), true
}

func testRigSegmentOf(pkgPath string) string {
	for _, seg := range testRigSegments {
		for _, part := range strings.Split(pkgPath, "/") {
			if part == seg {
				return seg
			}
		}
	}
	return ""
}

func inTree(pkgPath, tree string) bool {
	return pkgPath == tree || strings.HasPrefix(pkgPath, tree+"/")
}

// nameOnlyPass — НАИВНЫЙ предикат в СВОЕЙ единице счёта плюс вычеты в единице
// «файл» и напечатанные границы.
//
// Числа отсюда НИКОГДА не вычитаются из переписи мест: единицы разные, и
// складывать их значило бы получить видимость утверждения вместо утверждения
// (наблюдалось «60 − 5», где из мест вычитали удовлетворяющие типы).
//
// Контраст двусторонний, и вторая сторона важнее первой: предикат по имени не
// только ЗАВЫШАЕТ прозой и порождёнными стабами, он ещё и ПРОПУСКАЕТ настоящие
// места — те, что названы иначе.
func (c *Census) nameOnlyPass(root string, placeFiles, anchorRefs map[string]bool) {
	c.NameOnly.Needle = NameOnlyNeedle
	var (
		secondClient []string
		prose        int
		generated    int
		rigFiles     int
		wiring       int
		adapterHome  int
		withPlaces   int
		untraversed  []string
	)
	traversed := c.scannedFiles
	if traversed == nil {
		traversed = map[string]bool{}
	}
	named := map[string]bool{}

	homeDirPrefix := ""
	if c.AnchorPkg != "" {
		homeDirPrefix = shortPkg(c.AnchorPkg) + "/"
	}

	scanGoFiles(root, func(rel string, body []byte) {
		text := string(body)
		if !strings.Contains(strings.ToLower(text), NameOnlyNeedle) {
			return
		}
		if strings.HasSuffix(rel, "_test.go") {
			c.NameOnly.TestFiles++
			return
		}
		c.NameOnly.Files++
		named[rel] = true
		if len(c.NameOnly.Examples) < 5 {
			c.NameOnly.Examples = append(c.NameOnly.Examples, rel)
		}
		if !traversed[rel] {
			untraversed = append(untraversed, rel)
		}

		switch {
		case placeFiles[rel]:
			withPlaces++
		case homeDirPrefix != "" && strings.HasPrefix(rel, homeDirPrefix):
			// Дом самого адаптера: его обращения вычтены как самовызов, поэтому
			// мест в нём нет by construction. Слепой зоной он не является.
			adapterHome++
		case isGenerated(text):
			generated++
			c.Subtractions = append(c.Subtractions, Subtraction{
				Category: CategoryGenerated, Unit: UnitFile, File: rel,
				Reason: "файл порождён генератором (заголовок «Code generated … DO NOT EDIT»): " +
					"имя движка в нём — часть стаба контракта, а не место обращения",
			})
		case testRigSegmentOf(rel) != "":
			rigFiles++
			c.Subtractions = append(c.Subtractions, Subtraction{
				Category: CategoryTestRig, Unit: UnitFile, File: rel,
				Reason: "файл лежит под объявленным сегментом оснастки проб «" +
					testRigSegmentOf(rel) + "»",
			})
		case !mentionsOutsideProse(text):
			prose++
			c.Subtractions = append(c.Subtractions, Subtraction{
				Category: CategoryProse, Unit: UnitFile, File: rel,
				Reason: "движок назван только в комментарии или строковом литерале: " +
					"исполняемого обращения в файле нет",
			})
		case anchorRefs[rel]:
			// Связывание: файл держит якорный тип (конструирует его, объявляет
			// им поле), но не обращается к нему. Это композиционный корень, а не
			// слепая зона переписи.
			wiring++
		default:
			secondClient = append(secondClient, rel)
		}
	})

	var missed []string
	for f := range placeFiles {
		if !named[f] {
			missed = append(missed, f)
		}
	}
	sort.Strings(missed)
	sort.Strings(secondClient)
	sort.Strings(untraversed)

	c.NameOnly.Reconciled = NameOnlyReconciliation{
		WithPlaces:   withPlaces,
		MissedByName: len(missed),
		MissedFiles:  firstN(missed, 10),
		Generated:    generated,
		Prose:        prose,
		TestRig:      rigFiles,
		Wiring:       wiring,
		AdapterHome:  adapterHome,
		SecondClient: len(secondClient),
	}

	c.Boundaries = append(c.Boundaries, Boundary{
		Name:  "второй клиент движка вне дома якорного типа",
		Count: len(secondClient),
		Note: "исполняемые непроверочные файлы, называющие движок, НЕ проходящие через якорный тип " +
			"и не держащие его вовсе. Признак «свой транспорт» выведен из импортов пакета: " +
			"он и отличает ВТОРОЙ КЛИЕНТ от файла, который просто называет движок. Такой клиент " +
			"дискриминатор по типу не видит BY CONSTRUCTION, и сценарий полноты, не назвавший " +
			"этой границы, зелен при живом втором клиенте",
		Items: groupByPackage(secondClient, c.httpPkgs),
	})
	c.Boundaries = append(c.Boundaries, Boundary{
		Name:  "места, которых предикат ПО ИМЕНИ не назвал бы",
		Count: len(missed),
		Note: "файлы с настоящими местами, где подстрока " + NameOnlyNeedle + " не встречается вовсе. " +
			"Вторая сторона контроля: перепись по имени не только завышена прозой, она ЗАНИЖЕНА " +
			"на эти файлы — и занижение не видно",
		Items: firstN(missed, 20),
	})
	c.Boundaries = append(c.Boundaries, Boundary{
		Name:  "проверочные файлы не читаются по объявлению",
		Count: c.NameOnly.TestFiles,
		Note: "предмет переписи — прод-места; `_test.go` исключены НАМЕРЕННО. " +
			"Число названо, чтобы исключение было видно, а не подразумевалось",
	})
	c.Boundaries = append(c.Boundaries, Boundary{
		Name:  "файлы вне обхода компилятора",
		Count: len(untraversed),
		Note: "называют движок, но не попали ни в один загруженный пакет — отсечены build-тегом " +
			"либо лежат вне пакетных шаблонов. Их места невидимы дискриминатору",
		Items: firstN(untraversed, 20),
	})
	c.Boundaries = append(c.Boundaries, Boundary{
		Name:  "пакеты без единого непроверочного файла",
		Count: len(c.Scan.SkippedPkgs),
		Note:  "сплошь `_test.go` либо всё отсечено build-тегом: мест в них нет BY CONSTRUCTION",
	})
}

// ОБХОД ОГРАНИЧЕН КОРНЕМ, а не идёт `filepath.Walk`.
//
// Обход, склеивающий путь и затем работающий с ним, гоняется по имени, которое
// между проверкой и открытием может стать ссылкой в чужое дерево. Здесь корень
// открывается ОДИН раз, и всё читается ОТНОСИТЕЛЬНО него: ссылка наружу
// отвергается самим ядром, а не бдительностью читающего. Заодно исчезает нужда
// в послаблении статического анализатора — послабление здесь означало бы
// «знаем и мирится», тогда как предмет устраняется целиком.
//
// Каталоги, в которые перепись не заходит, названы: служебный каталог системы
// контроля версий и деревья чужих зависимостей. Это НЕ вычет — файлов продукта
// там нет by construction, и в перечень вычетов они не попадают.
var skipDirs = map[string]bool{".git": true, "node_modules": true, "vendor": true}

// scanGoFiles обходит дерево под корнем и отдаёт каждый `.go` вместе с телом.
// Корень открывается ОДИН раз — и на обход, и на чтение.
func scanGoFiles(root string, visit func(rel string, body []byte)) {
	r, err := os.OpenRoot(root)
	if err != nil {
		return
	}
	defer func() { _ = r.Close() }()

	var walk func(rel string)
	walk = func(rel string) {
		name := rel
		if name == "" {
			name = "."
		}
		d, oerr := r.Open(name)
		if oerr != nil {
			return
		}
		entries, rerr := d.ReadDir(-1)
		_ = d.Close()
		if rerr != nil {
			return
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		for _, e := range entries {
			child := e.Name()
			if rel != "" {
				child = rel + "/" + e.Name()
			}
			switch {
			case e.IsDir():
				if skipDirs[e.Name()] {
					continue
				}
				walk(child)
			case e.Type().IsRegular() && strings.HasSuffix(e.Name(), ".go"):
				body, ferr := r.ReadFile(child)
				if ferr != nil {
					continue
				}
				visit(child, body)
			}
		}
	}
	walk("")
}

// httpImportLiteral — литерал пути импорта `net/http` в том виде, в каком его
// держит дерево разбора (вместе с кавычками).
const httpImportLiteral = `"net/http"`

// httpImporters — каталоги пакетов, импортирующих `net/http`. Признак того, что
// у пакета есть СВОЙ транспорт: именно он отличает второй клиент движка от
// файла, который движок просто называет.
func httpImporters(res *loadResult) map[string]bool {
	out := map[string]bool{}
	for _, p := range res.All {
		for _, f := range p.Syn {
			for _, imp := range f.Imports {
				if imp.Path != nil && imp.Path.Value == httpImportLiteral {
					for _, rel := range p.Files {
						out[path.Dir(rel)] = true
					}
				}
			}
		}
	}
	return out
}

// groupByPackage сводит файлы к каталогам: перечень из тридцати путей не
// читается, а перечень из двух каталогов с числом файлов — читается.
func groupByPackage(files []string, httpPkgs map[string]bool) []string {
	byDir := map[string]int{}
	for _, f := range files {
		byDir[path.Dir(f)]++
	}
	dirs := make([]string, 0, len(byDir))
	for d := range byDir {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	out := make([]string, 0, len(dirs))
	for _, d := range dirs {
		mark := "без собственного транспорта"
		if httpPkgs[d] {
			mark = "СВОЙ ТРАНСПОРТ (net/http)"
		}
		out = append(out, fmt.Sprintf("%s — файлов %d, %s", d, byDir[d], mark))
	}
	return out
}

func firstN(s []string, n int) []string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func isGenerated(text string) bool {
	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "// Code generated ") && strings.HasSuffix(line, " DO NOT EDIT.") {
			return true
		}
	}
	return false
}

// mentionsOutsideProse — грубая, но ЧЕСТНАЯ проверка: встречается ли имя движка
// вне комментариев. Она нужна только для РАЗБОРА расхождения с наивным
// предикатом и на перепись мест не влияет: места считает компилятор.
func mentionsOutsideProse(text string) bool {
	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	inBlock := false
	for sc.Scan() {
		line := sc.Text()
		trimmed := strings.TrimSpace(line)
		if inBlock {
			if idx := strings.Index(trimmed, "*/"); idx >= 0 {
				inBlock = false
				trimmed = trimmed[idx+2:]
			} else {
				continue
			}
		}
		if strings.HasPrefix(trimmed, "//") {
			continue
		}
		if idx := strings.Index(trimmed, "/*"); idx >= 0 {
			if !strings.Contains(trimmed[idx:], "*/") {
				inBlock = true
			}
			trimmed = trimmed[:idx]
		}
		if idx := strings.Index(trimmed, "//"); idx >= 0 {
			trimmed = trimmed[:idx]
		}
		if strings.Contains(strings.ToLower(trimmed), NameOnlyNeedle) {
			return true
		}
	}
	return false
}
