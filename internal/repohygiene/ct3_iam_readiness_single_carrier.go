// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// carrierFinding — один сервис, объявивший СВОЙ носитель готовности.
type carrierFinding struct {
	Service string
	File    string
	Type    string
	Why     string
}

// carrierCensus — объём осмотренного ПО ОСЯМ.
//
// Двух чисел мало по одному: «носителей всего N» без «из них общих M» не
// различает дерево, где носитель один общий, и дерево, где носитель один
// собственный. Ровно этот случай гейт и стережёт, поэтому обе величины
// печатаются всегда.
type carrierCensus struct {
	FilesRead    int // не-тестовых файлов Go разобрано
	Services     int // сервисов найдено обходом
	CarriersAll  int // носителей готовности найдено ВСЕГО (общий + собственные)
	CarriersOwn  int // из них объявленных внутри сервисов
	CarrierNames []string
}

// auditReadinessCarrierIsSingle — судящая функция гейта «готовность отдаётся
// ОДНИМ носителем на всё дерево сервисов».
//
// # Предмет
//
// Соседний гейт (`TestEveryServiceBuildsReadinessRatherThanServingLiveness`)
// требует, чтобы готовность У СЕРВИСА БЫЛА, и судит по СТРУКТУРЕ литерала, а не
// по имени пакета — намеренно: он обязан засчитывать готовность и тому, кто
// собрал её сам. Из этого следует его граница: сервис со своим носителем
// проходит его законно.
//
// Здесь судится другое и не проверяемое там: сколько РАЗНЫХ носителей у одного
// механизма. Различие не косметическое — правка общего носителя (бюджет чекера,
// перевод готовности в отказ на гашении, зеркало в счётчики) до собственного НЕ
// ДОЕДЕТ, и разойтись им нечем: копии не собираются вместе и друг друга не
// читают, поэтому расхождение приходит молча (`architecture.md` §«Параллельные
// полосы одного механизма обязаны сверяться МЕЖДУ СОБОЙ»).
//
// # Как узнаётся носитель — ПО СТРУКТУРЕ, а не по имени
//
// Носитель — объявление типа-структуры с полем `Name string` и полем
// `Check func(context.Context) error` сразу. Имя типа не судится: собственный
// носитель назовётся как угодно, и привязка к имени сделала бы гейт слепым к
// следующему (`testing.md` §«Гейт на класс», п.7).
//
// Обе половины формы несущие. Тип с полем `Check` другой подписи — не носитель
// готовности, а таблица требований, и такой в дереве живёт
// (`services/iam/internal/apps/kaname/config` — поле `Element` вместо `Name` и
// `Check` двух аргументов). Он обязан молчать, и он стоит законным близнецом в
// инъекции.
//
// # Предпосылка гейта проверяется ИМ ЖЕ
//
// Отрицание («собственных носителей ноль») теряет вход молча: снимут общий
// носитель или переименуют его поля — распознаватель перестанет узнавать
// что-либо вовсе, а напечатает тот же ноль (`testing.md` §«Гейт на класс», п.9).
// Поэтому распознаватель прогоняется ПО ОБЩЕМУ НОСИТЕЛЮ тоже: не найдя его
// там, гейт краснеет на самом себе, а не сообщает о чистом дереве.
//
// # Чего гейт НЕ судит, названо прямо
//
//   - КРАЙ (`gateway/`) — вне популяции, та же граница по КАТЕГОРИИ, что у
//     соседнего гейта: его готовность строится не из именованных зависимостей, а
//     из перечня соседей, к которым он проксирует. Механизм другой, и требовать
//     от него общего носителя сервисов значило бы судить не тот предмет;
//   - ЧТО проверяет каждая зависимость и СКОЛЬКО их — это вопрос композиционного
//     корня, и общий носитель на него намеренно не отвечает.
func auditReadinessCarrierIsSingle(root string, serviceFiles, carrierFiles []string) ([]carrierFinding, carrierCensus, error) {
	var cen carrierCensus
	fset := token.NewFileSet()

	// Положительный контроль ПЕРВЫМ: пока не доказано, что распознаватель умеет
	// узнать носитель там, где тот заведомо есть, его ноль под services/ не
	// значит ничего.
	for _, rel := range carrierFiles {
		decls, err := carrierDeclsIn(root, rel, fset)
		if err != nil {
			return nil, cen, err
		}
		cen.FilesRead++
		for _, name := range decls {
			cen.CarriersAll++
			cen.CarrierNames = append(cen.CarrierNames, "pkg: "+name)
		}
	}

	services := map[string]bool{}
	var findings []carrierFinding
	for _, rel := range serviceFiles {
		slashed := filepath.ToSlash(rel)
		parts := strings.Split(slashed, "/")
		if len(parts) < 3 || parts[0] != "services" {
			continue
		}
		svc := parts[1]
		services[svc] = true

		decls, err := carrierDeclsIn(root, slashed, fset)
		if err != nil {
			return nil, cen, err
		}
		cen.FilesRead++
		for _, name := range decls {
			cen.CarriersAll++
			cen.CarriersOwn++
			cen.CarrierNames = append(cen.CarrierNames, svc+": "+name)
			findings = append(findings, carrierFinding{
				Service: svc,
				File:    slashed,
				Type:    name,
				Why: fmt.Sprintf(
					"объявлен СВОЙ носитель готовности `%s` (%s): поле Name плюс поле "+
						"Check func(context.Context) error. Это второй носитель одного механизма, и "+
						"правка общего до него НЕ ДОЕДЕТ — бюджет одного чекера, перевод готовности в "+
						"отказ на гашении, зеркало результата в счётчики появятся у шести сервисов и не "+
						"появятся здесь. Разойтись формам нечем: они не собираются вместе и друг друга "+
						"не читают, поэтому расхождение придёт молча. Общий носитель — "+
						"pkg/observability/health, он тянет ТОЛЬКО stdlib, поэтому листу графа его "+
						"импорт не добавляет ни ребра, ни чужой зависимости",
					name, slashed),
			})
		}
	}

	cen.Services = len(services)
	sort.Strings(cen.CarrierNames)
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Service != findings[j].Service {
			return findings[i].Service < findings[j].Service
		}
		return findings[i].Type < findings[j].Type
	})
	return findings, cen, nil
}

// carrierDeclsIn возвращает имена типов-носителей готовности, объявленных в файле.
func carrierDeclsIn(root, rel string, fset *token.FileSet) ([]string, error) {
	// #nosec G304 -- путь пришёл из индекса git ЭТОГО дерева (treecorpus, через
	// trackedGoFiles) либо из синтетического каталога инъекции; извне он не приходит
	src, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		return nil, fmt.Errorf("чтение %s: %w", rel, err)
	}
	file, perr := parser.ParseFile(fset, rel, src, 0)
	if perr != nil {
		return nil, fmt.Errorf("разбор %s: %w", rel, perr)
	}

	var out []string
	ast.Inspect(file, func(n ast.Node) bool {
		spec, ok := n.(*ast.TypeSpec)
		if !ok {
			return true
		}
		st, ok := spec.Type.(*ast.StructType)
		if !ok || st.Fields == nil {
			return true
		}
		if isReadinessCarrierStruct(st) {
			out = append(out, spec.Name.Name)
		}
		return true
	})
	return out, nil
}

// isReadinessCarrierStruct — структура несёт ОБЕ половины формы носителя:
// именующее поле `Name string` и тело проверки `Check func(context.Context) error`.
//
// Обе половины обязательны. По одному полю `Check` гейт ловил бы всякую таблицу
// с колонкой-предикатом; по одному полю `Name` — едва ли не всякую структуру
// дерева.
func isReadinessCarrierStruct(st *ast.StructType) bool {
	var name, check bool
	for _, f := range st.Fields.List {
		for _, id := range f.Names {
			switch id.Name {
			case "Name":
				if ct3IsIdent(f.Type, "string") {
					name = true
				}
			case "Check":
				if isContextErrorFunc(f.Type) {
					check = true
				}
			}
		}
	}
	return name && check
}

// isContextErrorFunc — подпись `func(context.Context) error` (имя параметра
// произвольно и может отсутствовать).
//
// Подпись судится целиком намеренно: соседний тип того же дерева несёт поле
// `Check` двух аргументов и носителем готовности не является. Распознаватель,
// смотрящий только на ИМЯ поля, объявил бы его нарушителем — то есть краснел бы
// на исправном коде, а такую проверку снимают первой.
func isContextErrorFunc(expr ast.Expr) bool {
	fn, ok := expr.(*ast.FuncType)
	if !ok || fn.Params == nil || fn.Results == nil {
		return false
	}
	if len(fn.Results.List) != 1 || !ct3IsIdent(fn.Results.List[0].Type, "error") {
		return false
	}
	var params []ast.Expr
	for _, p := range fn.Params.List {
		if len(p.Names) == 0 {
			params = append(params, p.Type)
			continue
		}
		for range p.Names {
			params = append(params, p.Type)
		}
	}
	if len(params) != 1 {
		return false
	}
	sel, ok := params[0].(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Context" {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "context"
}

// ct3IsIdent — выражение является названным предопределённым именем.
func ct3IsIdent(expr ast.Expr, want string) bool {
	id, ok := expr.(*ast.Ident)
	return ok && id.Name == want
}
