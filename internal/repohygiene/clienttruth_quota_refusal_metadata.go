// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// clienttruth_quota_refusal_metadata.go — величины отказа учёта доезжают до
// клиента МАШИННО, а не остаются в прозе.
//
// ПРЕДМЕТ (задача продукта #1605). Отказ по исчерпанию предела производит ОДИН
// производитель на платформу — `kacho_quota_refuse`, рендерящийся каждому
// владельцу учёта из общего шаблона `pkg/quota/refusal.sql.tmpl`. Он УЖЕ
// посчитал носителя, вид, предел и занятое и положил их в `DETAIL` объектом
// JSON. Мост SQLSTATE→sentinel у каждого владельца свой; сохранив `Message` и не
// прочитав `Detail`, он теряет величины на первом же переходе в Go — и клиент,
// получив `RESOURCE_EXHAUSTED`, не может машинно узнать ни предела, ни занятого,
// ни носителя. `api-conventions.md` §By-lane code-split требует обратного:
// клиент ключуется на `reason`-токен и поля `google.rpc.ErrorInfo`, а не
// разбирает прозу — тон сообщения стабилен, но не парсибелен.
//
// ЧЕМ ЭТОТ ГЕЙТ ОТЛИЧАЕТСЯ ОТ СОСЕДНЕГО. `TestEveryQuotaChargingOwnerMapsTheRefusal`
// (quotarefusalmapping_test.go) держит, что у владельца ЕСТЬ производитель обоих
// исходов и он приклеивает признак полосы. Здесь предмет другой и следующий по
// пути: величины, которые производитель посчитал, ДОЕЗЖАЮТ. Признак без величин
// — исправное состояние для того гейта и находка для этого.
//
// ПОЧЕМУ ДВЕ ПОЛОВИНЫ, А НЕ ОДНА. Мост может величины прочитать и не отдать;
// сборка ответа может место под них оставить и ничего не получить. Каждая
// половина по отдельности защитима, неисполнимость появляется на СТЫКЕ — и
// увидеть её можно только положив половины рядом, чего не делает ни обзор
// изменения, ни сборка.
//
// ПОЧЕМУ РАЗБОР СИНТАКСИСА, А НЕ ПОИСК ПО ТЕКСТУ. Имена, которые здесь ищутся,
// стоят и в комментариях — в том числе в этой самой шапке. Проверка по
// подстроке краснела бы на собственном объяснении и зеленела бы на закомменти-
// рованном вызове (`testing.md` §«Гейт на класс», п.4).
//
// ЧТО РАЗБОР НЕ ВИДИТ — названо, а не спрятано:
//
//  1. КОСВЕННЫЙ вызов — значение функции, положенное в переменную или поле.
//     Разбор судит вызов по месту, а не по потоку значений.
//  2. ТОЧЕЧНЫЙ импорт (`import . "…/quotadetail"`): вызов пишется голым
//     `Attach(...)`, приводить его к пути импорта нечем. В ПРОД-дереве точечных
//     импортов ноль (предикат: `grep -rnE '^\s*\.\s+"' --include=*.go services
//     pkg internal gateway | grep -v _test`); два, что есть в дереве, живут в
//     пробах, а пробы этот обход не читает вовсе.
//  3. ПРАВДИВОСТЬ величин — что в метаданные положено именно то, что посчитал
//     производитель. Это свойство ВЫЗОВА, и держат его пробы владельцев
//     (`quota_metadata_test.go` у каждого из шести).
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

const (
	// quotaDetailPkgPath — дом единственного разбора величин.
	quotaDetailPkgPath = "github.com/PRO-Robotech/kacho/pkg/quota/quotadetail"
	// quotaDetailAttachFunc — то, чем мост приклеивает величины к отказу.
	quotaDetailAttachFunc = "Attach"
	// errdetailsPkgPath — дом `google.rpc.ErrorInfo`.
	errdetailsPkgPath = "google.golang.org/genproto/googleapis/rpc/errdetails"
	// errorInfoType — тип, которым признак и величины уезжают клиенту.
	errorInfoType = "ErrorInfo"
	// quotaExceededSQLSTATE — код, которым производитель сообщает «место кончилось».
	quotaExceededSQLSTATE = "KQ001"
	// quotaExceededReason — признак той же полосы на пути наружу.
	quotaExceededReason = "QUOTA_EXCEEDED"
	// metadataField — поле `ErrorInfo`, штатно предназначенное под величины.
	metadataField = "Metadata"
)

// quotaMetadataOwnerFacts — что найдено в прод-коде ОДНОГО владельца учёта.
//
// Хранятся координаты, а не флаги: находка обязана называть файл, иначе
// читателю остаётся искать самому (`testing.md` §«Гейт на класс», п.8 —
// диагностика есть часть свойства).
type quotaMetadataOwnerFacts struct {
	// bridgeFile — файл, опознающий SQLSTATE отказа.
	bridgeFile string
	// attachFile — файл, где тот же мост приклеивает величины.
	attachFile string
	// outwardFile — файл, собирающий `ErrorInfo` полосы учёта.
	outwardFile string
	// metadataFile — он же, если величины в `ErrorInfo` попадают.
	metadataFile string
}

// quotaMetadataCensus — перепись обхода. Печатается ВСЕГДА: «ноль находок»
// обязано быть отличимо от «ноль прочитанного».
type quotaMetadataCensus struct {
	Owners              []string
	Parsed              int
	Facts               map[string]*quotaMetadataOwnerFacts
	BridgesFound        int
	BridgesAttaching    int
	OutwardFound        int
	OutwardWithMetadata int
}

// collectQuotaRefusalMetadata обходит прод-код названных владельцев.
//
// Состав дерева берётся у ИНДЕКСА git, а не у диска: под `services/` на машине,
// где поднимали стенд, лежат распаковки чартов и отчёты прогонов, и вердикт,
// собранный обходом файловой системы, стал бы свойством рабочего каталога, а не
// коммита.
func collectQuotaRefusalMetadata(tree *treecorpus.Tree, owners []string) (quotaMetadataCensus, error) {
	c := quotaMetadataCensus{
		Owners: append([]string(nil), owners...),
		Facts:  make(map[string]*quotaMetadataOwnerFacts, len(owners)),
	}
	sort.Strings(c.Owners)
	for _, o := range c.Owners {
		c.Facts[o] = &quotaMetadataOwnerFacts{}
	}

	for _, rel := range tree.SortedFiles() {
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
			continue
		}
		owner := quotaOwnerOfPath(rel, c.Owners)
		if owner == "" {
			continue
		}

		src, err := os.ReadFile(filepath.Join(tree.Root(), filepath.FromSlash(rel)))
		if err != nil {
			return c, fmt.Errorf("чтение %s: %w", rel, err)
		}
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, rel, src, 0)
		if perr != nil {
			// Неразбираемый файл пропускается молча только здесь: дерево
			// собирается, значит такого файла в нём нет, а синтетика инъекции
			// свои файлы пишет валидными.
			continue
		}
		c.Parsed++

		facts := c.Facts[owner]
		imports := quotaImportAliases(f)
		hasBridge, hasAttach, hasErrorInfo, hasReason, hasMetadata := quotaScanFile(f, imports)
		// Сборка ответа засчитывается ТОЛЬКО как сборка отказа УЧЁТА: у
		// владельца есть и другие `ErrorInfo` (полосы резолва, пиры), и
		// требовать величин от них значило бы краснеть на исправном коде.
		outward := hasErrorInfo && hasReason

		if hasBridge && facts.bridgeFile == "" {
			facts.bridgeFile = rel
		}
		if hasBridge && hasAttach && facts.attachFile == "" {
			facts.attachFile = rel
		}
		if outward && facts.outwardFile == "" {
			facts.outwardFile = rel
		}
		if outward && hasMetadata && facts.metadataFile == "" {
			facts.metadataFile = rel
		}
	}

	for _, o := range c.Owners {
		f := c.Facts[o]
		if f.bridgeFile != "" {
			c.BridgesFound++
		}
		if f.attachFile != "" {
			c.BridgesAttaching++
		}
		if f.outwardFile != "" {
			c.OutwardFound++
		}
		if f.metadataFile != "" {
			c.OutwardWithMetadata++
		}
	}
	return c, nil
}

// quotaOwnerOfPath отвечает, чей это прод-файл; "" — ничей из названных.
func quotaOwnerOfPath(rel string, owners []string) string {
	for _, o := range owners {
		if strings.HasPrefix(rel, "services/"+o+"/internal/") {
			return o
		}
	}
	return ""
}

// quotaImportAliases — «имя, под которым пакет виден в файле» → путь импорта.
//
// Приведение к ПОЛНОМУ пути, а не к имени пакета: имя задаёт вызывающий, и
// одноимённый чужой помощник иначе стал бы нашим (тот же довод, что в шапке
// bodycapsinglesource.go).
func quotaImportAliases(f *ast.File) map[string]string {
	out := make(map[string]string, len(f.Imports))
	for _, im := range f.Imports {
		p, err := strconv.Unquote(im.Path.Value)
		if err != nil {
			continue
		}
		name := p
		if i := strings.LastIndex(p, "/"); i >= 0 {
			name = p[i+1:]
		}
		if im.Name != nil {
			if im.Name.Name == "." || im.Name.Name == "_" {
				continue
			}
			name = im.Name.Name
		}
		out[name] = p
	}
	return out
}

// quotaScanFile отвечает на четыре вопроса об одном файле, судя УЗЛЫ, а не текст.
func quotaScanFile(f *ast.File, imports map[string]string) (bridge, attach, errorInfo, reason, metadata bool) {
	// Переменные, которым присвоен `&errdetails.ErrorInfo{…}` — чтобы форма
	// «собрать, затем доставить поле» опознавалась наравне с литеральной.
	infoVars := map[string]bool{}

	ast.Inspect(f, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.BasicLit:
			// Строковый литерал в КОДЕ; комментарии сюда не попадают by
			// construction — ради этого и взят разбор синтаксиса.
			if v.Kind != token.STRING {
				return true
			}
			s, err := strconv.Unquote(v.Value)
			if err != nil {
				return true
			}
			switch s {
			case quotaExceededSQLSTATE:
				bridge = true
			case quotaExceededReason:
				// Признак полосы учёта — им опознаётся сборка ответа именно
				// ЭТОГО отказа, а не любого другого `ErrorInfo` в файле.
				reason = true
			}
		case *ast.CallExpr:
			if quotaCallIs(v, imports, quotaDetailPkgPath, quotaDetailAttachFunc) {
				attach = true
			}
		case *ast.CompositeLit:
			if !quotaTypeIs(v.Type, imports, errdetailsPkgPath, errorInfoType) {
				return true
			}
			errorInfo = true
			for _, el := range v.Elts {
				kv, ok := el.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				if id, isID := kv.Key.(*ast.Ident); isID && id.Name == metadataField {
					metadata = true
				}
			}
		case *ast.AssignStmt:
			// ВТОРАЯ ЗАКОННАЯ ФОРМА: поле доставляется отдельным присваиванием
			// (`info.Metadata = …`). Не край и не редкость — обычная запись
			// условного заполнения; распознаватель, знающий одну форму, всё
			// записанное второй оставил бы ВНЕ наблюдения (`testing.md`
			// §«Гейт на класс», п.7).
			for _, lhs := range v.Lhs {
				sel, ok := lhs.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != metadataField {
					continue
				}
				if id, isID := sel.X.(*ast.Ident); isID && infoVars[id.Name] {
					metadata = true
				}
			}
			for i, rhs := range v.Rhs {
				if i >= len(v.Lhs) {
					break
				}
				if !quotaExprBuildsErrorInfo(rhs, imports) {
					continue
				}
				if id, isID := v.Lhs[i].(*ast.Ident); isID {
					infoVars[id.Name] = true
				}
			}
		}
		return true
	})
	return bridge, attach, errorInfo, reason, metadata
}

// quotaExprBuildsErrorInfo — выражение, порождающее `ErrorInfo` (с адресом или без).
func quotaExprBuildsErrorInfo(e ast.Expr, imports map[string]string) bool {
	if u, ok := e.(*ast.UnaryExpr); ok && u.Op == token.AND {
		e = u.X
	}
	cl, ok := e.(*ast.CompositeLit)
	if !ok {
		return false
	}
	return quotaTypeIs(cl.Type, imports, errdetailsPkgPath, errorInfoType)
}

// quotaTypeIs — тип есть `<pkg>.<name>`, где pkg приведён к пути импорта.
func quotaTypeIs(e ast.Expr, imports map[string]string, pkgPath, name string) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != name {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	return imports[id.Name] == pkgPath
}

// quotaCallIs — вызов есть `<pkg>.<fn>`, где pkg приведён к пути импорта.
func quotaCallIs(c *ast.CallExpr, imports map[string]string, pkgPath, fn string) bool {
	return quotaTypeIs(c.Fun, imports, pkgPath, fn)
}

// quotaRefusalMetadataFindings — расхождения, каждое с координатой.
func quotaRefusalMetadataFindings(c quotaMetadataCensus) []string {
	var out []string
	for _, o := range c.Owners {
		f := c.Facts[o]
		if f.bridgeFile != "" && f.attachFile == "" {
			out = append(out, fmt.Sprintf(
				"%s: мост опознаёт отказ (%s), но величин производителя не приклеивает — "+
					"клиент получит код и прозу без предела, занятого и носителя "+
					"(%s.%s)", o, f.bridgeFile, "quotadetail", quotaDetailAttachFunc))
		}
		if f.outwardFile != "" && f.metadataFile == "" {
			out = append(out, fmt.Sprintf(
				"%s: ответ собирает google.rpc.ErrorInfo (%s) без поля %s — "+
					"величины некуда положить, и полосы остаются различимы только прозой",
				o, f.outwardFile, metadataField))
		}
	}
	return out
}
