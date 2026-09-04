// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// readinesscarrier.go — обход дерева для гейта единственного носителя
// готовности. Живёт в НЕ-тестовом файле намеренно: инъекция
// (`readinesscarrier_injection_test.go`) обязана звать ТУ ЖЕ функцию, что и
// гейт, иначе она доказывает свойство своей копии.

// healthCarrierPkg — объявленный носитель разведённых живости и готовности.
// Его шапка называет себя ЕДИНСТВЕННЫМ в дереве; гейт держит это утверждение.
const healthCarrierPkg = "github.com/PRO-Robotech/kacho/pkg/observability/health"

// readyHandlerSel / liveHandlerSel — как носитель отдаёт обработчики. Разъедутся
// с кодом — перепись найдёт ноль построенных носителем, и гейт скажет об этом
// отдельной строкой, а не промолчит.
const (
	readyHandlerSel = "ReadyHandler"
	liveHandlerSel  = "LiveHandler"
)

// readinessMount — координата монтирования `/readyz` и то, чем оно построено.
type readinessMount struct {
	// coord — `<путь>:<строка>`; находка без координаты не действие.
	coord string
	// byCarrier — обработчик построен объявленным носителем: аргументом стоит
	// вызов `<что-то>.ReadyHandler()`, а файл ИМПОРТИРУЕТ носитель.
	//
	// Два условия вместе, а не любое из двух: импорт без вызова бывает ради
	// константы, вызов без импорта — одноимённый метод чужого типа. Порознь
	// каждое даёт слепую зону.
	byCarrier bool
	// localCheckerType — координата локально объявленного типа именованной
	// проверки (`{Name string; Check func(ctx) error}`) в ТОМ ЖЕ пакете, что
	// монтирование. Второй носитель той же формы — предмет задачи #1752.
	localCheckerType string
}

// readinessReach — что обход установил о дереве.
type readinessReach struct {
	// services — сервисы, поднимающие `/readyz` (отсортированы).
	services []string
	// mounts — по сервису: первое найденное монтирование.
	mounts map[string]readinessMount
	// filesRead — объём осмотренного: «ноль находок» обязано быть отличимо от
	// «ноль прочитанного».
	filesRead int
	// servicesSeen — сервисов в дереве всего (независимо от того, поднимают ли
	// они готовность). Печатается порознь с числом поднимающих: одно число
	// скрывает ровно тот случай, ради которого ось заведена, — сервис, у
	// которого готовности нет ВОВСЕ.
	servicesSeen int
}

// byCarrierCount — сколько из поднимающих строит готовность объявленным
// носителем.
func (r readinessReach) byCarrierCount() int {
	n := 0
	for _, s := range r.services {
		if r.mounts[s].byCarrier {
			n++
		}
	}
	return n
}

// localCheckerTypes — сколько поднимающих объявляют СВОЙ тип именованной проверки.
func (r readinessReach) localCheckerTypes() int {
	n := 0
	for _, s := range r.services {
		if r.mounts[s].localCheckerType != "" {
			n++
		}
	}
	return n
}

// scanReadinessCarriers — обход прод-дерева сервисов: кто поднимает `/readyz` и
// строит ли он его объявленным носителем.
//
// Состав берётся у индекса git: обход диска прочитал бы игнорируемое, и вердикт
// стал бы свойством рабочего каталога, а не коммита.
//
// ГРАНИЦА ПОПУЛЯЦИИ НАЗВАНА, а не подразумевается: край (`gateway/`) в обход не
// входит. У него своя поверхность готовности, и она отвечает на ДРУГОЙ вопрос —
// опрашивает `grpc.health.v1.Health` у всех бэкендов, то есть агрегирует чужую
// готовность, а не свои зависимости. Втянуть его в этот гейт значило бы
// потребовать от него формы, которой его предмет не соответствует.
func scanReadinessCarriers(root string) (readinessReach, error) {
	var out readinessReach
	out.mounts = map[string]readinessMount{}

	dir := filepath.Join(root, "services")
	tracked, err := treecorpus.Under(dir)
	if err != nil {
		return out, fmt.Errorf("состав дерева под %s не читается: %w", dir, err)
	}

	seenSvc := map[string]bool{}
	// checkerTypesByDir — координата локального типа именованной проверки по
	// каталогу пакета: тип и монтирование лежат в одном пакете, но могут лежать
	// в разных файлах.
	checkerTypesByDir := map[string]string{}
	type pending struct {
		svc, coord, dir string
		byCarrier       bool
	}
	var mounts []pending

	fset := token.NewFileSet()
	for _, abs := range tracked {
		if !strings.HasSuffix(abs, ".go") || strings.HasSuffix(abs, "_test.go") {
			continue
		}
		rel, rerr := filepath.Rel(root, abs)
		if rerr != nil {
			return out, fmt.Errorf("относительный путь для %s: %w", abs, rerr)
		}
		slash := filepath.ToSlash(rel)
		parts := strings.Split(slash, "/")
		if len(parts) < 2 || parts[0] != "services" {
			continue
		}
		svc := parts[1]
		seenSvc[svc] = true
		out.filesRead++

		f, perr := parser.ParseFile(fset, abs, nil, parser.SkipObjectResolution)
		if perr != nil {
			return out, fmt.Errorf("разбор %s: %w", slash, perr)
		}

		pkgDir := filepath.ToSlash(filepath.Dir(slash))
		if coord := namedCheckerTypeCoord(f, fset, slash); coord != "" && checkerTypesByDir[pkgDir] == "" {
			checkerTypesByDir[pkgDir] = coord
		}

		imports := healthCarrierImported(f)
		for _, m := range readyMounts(f, fset, slash) {
			mounts = append(mounts, pending{
				svc:       svc,
				coord:     m.coord,
				dir:       pkgDir,
				byCarrier: imports && m.byCarrier,
			})
		}
	}

	for _, m := range mounts {
		if _, ok := out.mounts[m.svc]; ok {
			continue
		}
		out.mounts[m.svc] = readinessMount{
			coord:            m.coord,
			byCarrier:        m.byCarrier,
			localCheckerType: checkerTypesByDir[m.dir],
		}
		out.services = append(out.services, m.svc)
	}
	sort.Strings(out.services)
	out.servicesSeen = len(seenSvc)
	return out, nil
}

// healthCarrierImported — импортирует ли файл объявленный носитель.
func healthCarrierImported(f *ast.File) bool {
	for _, imp := range f.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err == nil && path == healthCarrierPkg {
			return true
		}
	}
	return false
}

// readyMounts — монтирования `/readyz` в файле.
//
// Разбор синтаксического дерева, а не текста: путь `/readyz` встречается в этом
// дереве в шапках, комментариях и в перечне публичных путей края. Текстовый
// поиск принял бы объяснение за исполнение — тот самый класс, который гейт ловит.
func readyMounts(f *ast.File, fset *token.FileSet, rel string) []readinessMount {
	var found []readinessMount
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if sel.Sel.Name != "Handle" && sel.Sel.Name != "HandleFunc" {
			return true
		}
		if len(call.Args) < 2 {
			return true
		}
		lit, ok := call.Args[0].(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		pattern, err := strconv.Unquote(lit.Value)
		if err != nil || !patternIsReadyz(pattern) {
			return true
		}
		found = append(found, readinessMount{
			coord:     fmt.Sprintf("%s:%d", rel, fset.Position(lit.Pos()).Line),
			byCarrier: builtByCarrier(call.Args[1]),
		})
		return true
	})
	return found
}

// patternIsReadyz — образец маршрутизатора указывает на `/readyz`.
//
// Форм записи в этом дереве ДВЕ, и распознаватель обязан знать обе
// (`testing.md` §«Гейт на класс», п.7): голый путь (`"/readyz"`) и путь с
// методом (`"GET /readyz"`). Форма, о которой он не знает, не даёт ни красного,
// ни зелёного — она МОЛЧИТ.
func patternIsReadyz(pattern string) bool {
	p := strings.TrimSpace(pattern)
	if i := strings.LastIndex(p, " "); i >= 0 {
		p = strings.TrimSpace(p[i+1:])
	}
	if i := strings.Index(p, "/"); i > 0 {
		p = p[i:] // `host/readyz` — путь начинается с первой косой
	}
	return p == "/readyz"
}

// builtByCarrier — обработчик построен вызовом `<что-то>.ReadyHandler()`.
func builtByCarrier(arg ast.Expr) bool {
	call, ok := arg.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	return sel.Sel.Name == readyHandlerSel
}

// namedCheckerTypeCoord — координата локально объявленного типа именованной
// проверки: структуры ровно с полями `Name string` и `Check func(...) error`.
//
// Это ФОРМА `health.Checker`, объявленная по месту. Предикат структурный, а не
// по имени: второй носитель необязан называться так же, а совпадение имени само
// по себе ничего не значит.
func namedCheckerTypeCoord(f *ast.File, fset *token.FileSet, rel string) string {
	coord := ""
	ast.Inspect(f, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if !ok {
			return true
		}
		st, ok := ts.Type.(*ast.StructType)
		if !ok || st.Fields == nil || len(st.Fields.List) != 2 {
			return true
		}
		hasName, hasCheck := false, false
		for _, fld := range st.Fields.List {
			if len(fld.Names) != 1 {
				return true
			}
			switch fld.Names[0].Name {
			case "Name":
				if id, ok := fld.Type.(*ast.Ident); ok && id.Name == "string" {
					hasName = true
				}
			case "Check":
				ft, ok := fld.Type.(*ast.FuncType)
				if !ok || ft.Results == nil || len(ft.Results.List) != 1 {
					return true
				}
				if id, ok := ft.Results.List[0].Type.(*ast.Ident); ok && id.Name == "error" {
					hasCheck = true
				}
			}
		}
		if hasName && hasCheck && coord == "" {
			coord = fmt.Sprintf("%s:%d", rel, fset.Position(ts.Pos()).Line)
		}
		return true
	})
	return coord
}
