// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// bothidentityformsproducer.go — разбор ПРОИЗВОДИТЕЛЕЙ ВХОДА «обе формы личности
// в одном запросе»: предъявленное удостоверение ВМЕСТЕ с переданной личностью.
//
// # Предмет разбора — ВХОД, а не имя пробы
//
// Приёмка KAN-AUTHN-1 редакцией 2 отозвала решение «обе формы разом — отказ»:
// полосы разведены ПОСТРОЕНИЕМ, край снимает арендаторское удостоверение перед
// пересылкой за себя. Держателей снятой ветки приёмка назвала предикатом по
// ИМЕНИ пробы — и недосчитала одного: третий держатель жил КЕЙСОМ ВНУТРИ чужой
// пробы соседнего семейства (перечень причин отказа, закрытый счётчиком) и
// имени снятого сценария не содержал вовсе.
//
// Между именем и предметом отношения нет. Предмет — вход, на котором ветка
// срабатывает, и утверждение о нём живёт под любым именем. Поэтому разбор
// ищет ВХОД: whereAt, где удостоверение и переданная личность сходятся в одном
// запросе.
//
// # Две законные формы, и обе обязаны быть известны разборщику
//
//  1. **одно выражение** — вызов, чьё поддерево несёт обе формы сразу
//     (`s.present(t, raw, withTrustedForwarded(p))`, слияние метаданных на
//     боевой цепочке). Локальные имена подставляются на один уровень: носитель
//     переданной личности часто приезжает переменной.
//  2. **накапливающий носитель** — `metadata.MD` либо запрос HTTP, которому обе
//     формы кладут ПОСЛЕДОВАТЕЛЬНЫМИ вызовами (`in.Set(...)`, `r.Header.Set(...)`).
//     Одного выражения здесь нет вовсе, и разборщик первой формы промолчал бы.
//
// Форма, о которой разборщик не знает, не даёт ни красного, ни зелёного — она
// МОЛЧИТ, и записанное в ней оказывается вне наблюдения. Поэтому обе формы
// доказаны инъекцией порознь, а перепись печатает ОБЕ величины: сколько
// осмотрено и сколько найдено.
//
// # Чего разбор НЕ видит — названо, а не спрятано
//
//  1. **подстановка глубже одного уровня**: `a := forwardedIdentity(...)`,
//     `b := a`, `f(b, credential)`. Цепочка имён длиной два в этом дереве не
//     встречается, и её появление видно по счётчику подстановок.
//  2. **производитель, приехавший из ЧУЖОГО пакета тестов**: замыкание
//     производителей берётся по каталогу (пакету), а не по всему дереву —
//     помощник соседнего пакета не виден. Признак этого — падение числа
//     производителей в переписи.
//  3. **поток значений через поле структуры**: разбор судит выражение по месту,
//     а не по потоку.
package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
	"strings"

	"github.com/PRO-Robotech/corelib/principalwire"
)

// credentialSymbolMarkers — имена, которыми выражение выдаёт в себе ключ
// ПРЕДЪЯВЛЕННОГО удостоверения. Судится последний сегмент селектора, поэтому
// имя пакета значения не имеет.
var credentialSymbolMarkers = map[string]bool{
	"MetadataKey":             true, // presentedcred: ключ метаданных удостоверения
	"MetaBridgedCredential":   true, // край: вторая форма того же удостоверения на проводе
	"HeaderBridgedCredential": true,
}

// credentialLiteralMarkers — те же ключи строкой. Регистр не значим: на проводе
// законны обе формы записи имени.
var credentialLiteralMarkers = []string{
	"authorization",
	"grpcgateway-authorization",
	"grpc-metadata-authorization",
}

// credentialLiteralPrefixes — предъявление узнаётся и по схеме.
var credentialLiteralPrefixes = []string{"bearer "}

// forwardedSymbolMarkers — имена, которыми выражение выдаёт в себе ключ
// ПЕРЕДАННОЙ личности.
var forwardedSymbolMarkers = map[string]bool{
	"MDKeyPrincipalType":             true,
	"MDKeyPrincipalID":               true,
	"MDKeyPrincipalDisplay":          true,
	"MetaPrincipalType":              true,
	"MetaPrincipalID":                true,
	"MetaPrincipalDisplay":           true,
	"HeaderPrincipalType":            true,
	"HeaderPrincipalID":              true,
	"HeaderPrincipalDisplay":         true,
	"HeaderGRPCMetaPrincipalType":    true,
	"HeaderGRPCMetaPrincipalID":      true,
	"HeaderGRPCMetaPrincipalDisplay": true,
}

// forwardedLiteralPrefixes — тот же ключ строкой: общий префикс метаданных и
// заголовков переданной личности.
//
// Приставка берётся У ВЛАДЕЛЯ (`pkg/principalwire`), а не пишется своей рукой:
// второе объявление одного имени расходится с первым МОЛЧА — переименование
// одной стороны собирается чисто, а разбор, не найдя своих ключей, читает это
// как «переданной личности нет» и объявляет вход неполным.
var forwardedLiteralPrefixes = []string{
	principalwire.MetaPrincipalPrefix,
	grpcBridgePrefix + principalwire.MetaPrincipalPrefix,
}

// grpcBridgePrefix — приставка, под которой мост переносит заголовок в
// метаданные. Своего владельца у неё в этом модуле нет.
const grpcBridgePrefix = "grpc-metadata-"

// carrierConstructors — из чего рождается НОСИТЕЛЬ запроса. Накопление формы 2
// ведётся только по ним: иначе метки скапливались бы на `err` и на любом другом
// имени, дважды встретившемся в теле пробы, и законная проба, шлющая ДВА разных
// запроса, читалась бы как один с обеими формами.
var carrierConstructors = map[string]bool{
	"MD":                 true, // metadata.MD{}
	"New":                true, // metadata.New(...)
	"Pairs":              true, // metadata.Pairs(...)
	"NewIncomingContext": true,
	"NewOutgoingContext": true,
	"NewRequest":         true, // httptest.NewRequest(...)
	"Header":             true, // http.Header{}
	"Background":         true,
	"TODO":               true,
}

// carrierMutators — методы, которыми носителю ДОКЛАДЫВАЮТ форму.
var carrierMutators = map[string]bool{
	"Set": true, "Add": true, "Append": true,
}

// identityFormKind — какую форму личности несёт выражение.
type identityFormKind int

const (
	formCredential identityFormKind = 1 << iota
	formForwarded
)

// BothFormsSite — координата производителя входа «обе формы разом».
type BothFormsSite struct {
	File string
	Line int
	// Func — проба или помощник, в теле которого производитель стоит. Имя
	// СЛЕДСТВИЕ находки, а не её признак: держателя ищут по входу, а печатают
	// с именем, чтобы находку было где чинить.
	Func string
	// Form — «одно выражение» либо «накапливающий носитель».
	Form string
	// Why — чем именно опознаны обе формы.
	Why string
}

// BothFormsCensus — объём осмотренного.
type BothFormsCensus struct {
	// Funcs — функций и методов осмотрено.
	Funcs int
	// Calls — вызовов осмотрено.
	Calls int
	// CredentialProducers — помощников пакета, признанных производителями
	// удостоверения (замыкание по вызовам).
	CredentialProducers []string
	// ForwardedProducers — то же для переданной личности.
	ForwardedProducers []string
	// CredentialSites, ForwardedSites — выражений, несущих каждую форму.
	CredentialSites int
	ForwardedSites  int
	// Carriers — носителей запроса опознано.
	Carriers int
	// Substitutions — локальных имён подставлено.
	Substitutions int
}

// packageFile — один разобранный файл пакета.
type packageFile struct {
	rel  string
	fset *token.FileSet
	file *ast.File
}

// ScanBothIdentityFormsProducers разбирает ОДИН пакет тестов (файлы одного
// каталога) и возвращает производителей входа «обе формы личности разом».
//
// Пакет, а не файл: помощник, кладущий форму в контекст, живёт в соседнем файле
// того же каталога (`harness_test.go` рядом с `reader_test.go`), и разбор по
// одному файлу не увидел бы ни одного производителя.
func ScanBothIdentityFormsProducers(sources map[string][]byte) ([]BothFormsSite, BothFormsCensus, error) {
	var census BothFormsCensus

	rels := make([]string, 0, len(sources))
	for rel := range sources {
		rels = append(rels, rel)
	}
	sort.Strings(rels)

	files := make([]packageFile, 0, len(rels))
	for _, rel := range rels {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, rel, sources[rel], 0)
		if err != nil {
			return nil, census, err
		}
		files = append(files, packageFile{rel: rel, fset: fset, file: f})
	}

	// (1) Замыкание производителей: помощник, чьё тело несёт форму, сам
	// становится её производителем; помощник, зовущий такого помощника, — тоже.
	credProducers := map[string]bool{}
	fwdProducers := map[string]bool{}
	for changed := true; changed; {
		changed = false
		for _, pf := range files {
			for _, decl := range pf.file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				name := fn.Name.Name
				if isTestEntryPoint(name) {
					// Проба вызывающих не имеет: производителем её объявлять
					// незачем, а перепись производителей она бы засорила.
					continue
				}
				kinds := formsOfNode(fn.Body, credProducers, fwdProducers, nil, nil)
				if kinds&formCredential != 0 && !credProducers[name] {
					credProducers[name] = true
					changed = true
				}
				if kinds&formForwarded != 0 && !fwdProducers[name] {
					fwdProducers[name] = true
					changed = true
				}
			}
		}
	}
	for n := range credProducers {
		census.CredentialProducers = append(census.CredentialProducers, n)
	}
	for n := range fwdProducers {
		census.ForwardedProducers = append(census.ForwardedProducers, n)
	}
	sort.Strings(census.CredentialProducers)
	sort.Strings(census.ForwardedProducers)

	// (2) Поиск держателей по телам функций.
	var out []BothFormsSite
	for _, pf := range files {
		for _, decl := range pf.file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			census.Funcs++
			// Производители сами по себе держателями не считаются: их предмет —
			// положить ОДНУ форму, а сходятся формы у вызывающего.
			out = append(out, scanFuncForBothForms(pf, fn, credProducers, fwdProducers, &census)...)
		}
	}
	out = dedupeBothFormsSites(out)
	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		if out[i].Line != out[j].Line {
			return out[i].Line < out[j].Line
		}
		return out[i].Form < out[j].Form
	})
	return out, census, nil
}

// scanFuncForBothForms — обе формы записи предмета в теле одной функции.
func scanFuncForBothForms(
	pf packageFile, fn *ast.FuncDecl,
	credProducers, fwdProducers map[string]bool, census *BothFormsCensus,
) []BothFormsSite {
	// Локальные имена: что несёт каждое и является ли оно носителем запроса.
	local := map[string]identityFormKind{}
	carriers := map[string]bool{}

	// Предварительный проход: единичные присваивания дают подстановку.
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || len(as.Lhs) != 1 || len(as.Rhs) != 1 {
			return true
		}
		id, ok := as.Lhs[0].(*ast.Ident)
		if !ok || id.Name == "_" {
			return true
		}
		if k := formsOfNode(as.Rhs[0], credProducers, fwdProducers, local, carriers); k != 0 {
			local[id.Name] |= k
			census.Substitutions++
		}
		if isCarrierExpr(as.Rhs[0], carriers) {
			carriers[id.Name] = true
			census.Carriers++
		}
		return true
	})

	var out []BothFormsSite

	// Форма 1 — одно выражение.
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		census.Calls++
		k := formsOfNode(call, credProducers, fwdProducers, local, carriers)
		if k&formCredential != 0 {
			census.CredentialSites++
		}
		if k&formForwarded != 0 {
			census.ForwardedSites++
		}
		if k&formCredential != 0 && k&formForwarded != 0 {
			out = append(out, BothFormsSite{
				File: pf.rel,
				Line: pf.fset.Position(call.Pos()).Line,
				Func: fn.Name.Name,
				Form: "одно выражение",
				Why:  "поддерево вызова несёт удостоверение И переданную личность",
			})
		}
		return true
	})

	// Форма 2 — накапливающий носитель.
	accumulated := map[string]identityFormKind{}
	whereAt := map[string]int{}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || !carrierMutators[sel.Sel.Name] {
			return true
		}
		root := bothFormsRootIdent(sel.X)
		if root == "" || !carriers[root] {
			return true
		}
		var k identityFormKind
		for _, a := range call.Args {
			k |= formsOfNode(a, credProducers, fwdProducers, local, carriers)
		}
		if k == 0 {
			return true
		}
		accumulated[root] |= k
		if _, seen := whereAt[root]; !seen || accumulated[root]&formCredential != 0 && accumulated[root]&formForwarded != 0 {
			whereAt[root] = pf.fset.Position(call.Pos()).Line
		}
		return true
	})
	names := make([]string, 0, len(accumulated))
	for n := range accumulated {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		k := accumulated[n]
		if k&formCredential != 0 && k&formForwarded != 0 {
			out = append(out, BothFormsSite{
				File: pf.rel,
				Line: whereAt[n],
				Func: fn.Name.Name,
				Form: "накапливающий носитель",
				Why:  "носителю " + n + " положены обе формы последовательными вызовами",
			})
		}
	}
	return out
}

// formsOfNode — какие формы несёт поддерево выражения.
//
// Пустая строка среди аргументов вызова производителя удостоверения СНИМАЕТ
// метку удостоверения: ровно так записан законный близнец («тот же вызов без
// предъявленного токена»), и производитель на пустом входе метаданных
// удостоверения не ставит вовсе.
func formsOfNode(
	n ast.Node, credProducers, fwdProducers map[string]bool,
	local map[string]identityFormKind, carriers map[string]bool,
) identityFormKind {
	var k identityFormKind
	ast.Inspect(n, func(node ast.Node) bool {
		switch e := node.(type) {
		case *ast.BasicLit:
			if e.Kind == token.STRING {
				v := strings.ToLower(strings.Trim(e.Value, "`\""))
				for _, m := range credentialLiteralMarkers {
					if v == m {
						k |= formCredential
					}
				}
				for _, p := range credentialLiteralPrefixes {
					if strings.HasPrefix(v, p) {
						k |= formCredential
					}
				}
				for _, p := range forwardedLiteralPrefixes {
					if strings.HasPrefix(v, p) {
						k |= formForwarded
					}
				}
			}
		case *ast.SelectorExpr:
			if credentialSymbolMarkers[e.Sel.Name] {
				k |= formCredential
			}
			if forwardedSymbolMarkers[e.Sel.Name] {
				k |= formForwarded
			}
		case *ast.Ident:
			if credentialSymbolMarkers[e.Name] {
				k |= formCredential
			}
			if forwardedSymbolMarkers[e.Name] {
				k |= formForwarded
			}
			if local != nil {
				k |= local[e.Name]
			}
		case *ast.CallExpr:
			name := bothFormsCallee(e)
			if name == "" {
				return true
			}
			if fwdProducers[name] {
				k |= formForwarded
			}
			if credProducers[name] && !hasEmptyStringArg(e) {
				k |= formCredential
			}
		}
		return true
	})
	return k
}

// hasEmptyStringArg — есть ли среди аргументов пустая строка. Это и есть
// «отличие ровно одним фактом» законного близнеца.
func hasEmptyStringArg(call *ast.CallExpr) bool {
	for _, a := range call.Args {
		lit, ok := a.(*ast.BasicLit)
		if ok && lit.Kind == token.STRING && (lit.Value == `""` || lit.Value == "``") {
			return true
		}
	}
	return false
}

// bothFormsCallee — последний сегмент имени вызываемого.
func bothFormsCallee(call *ast.CallExpr) string {
	switch f := call.Fun.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		return f.Sel.Name
	}
	return ""
}

// isCarrierExpr — родился ли из выражения носитель запроса.
func isCarrierExpr(e ast.Expr, carriers map[string]bool) bool {
	switch x := e.(type) {
	case *ast.CompositeLit:
		return carrierConstructors[typeTailName(x.Type)]
	case *ast.CallExpr:
		if carrierConstructors[bothFormsCallee(x)] {
			return true
		}
		for _, a := range x.Args {
			if isCarrierExpr(a, carriers) {
				return true
			}
		}
	case *ast.Ident:
		return carriers[x.Name]
	}
	return false
}

// typeTailName — последний сегмент имени типа.
func typeTailName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return t.Sel.Name
	}
	return ""
}

// bothFormsRootIdent — корневое имя цепочки селекторов (`r.Header` → `r`).
func bothFormsRootIdent(e ast.Expr) string {
	for {
		switch x := e.(type) {
		case *ast.Ident:
			return x.Name
		case *ast.SelectorExpr:
			e = x.X
		case *ast.CallExpr:
			e = x.Fun
		default:
			return ""
		}
	}
}

// isTestEntryPoint — точка входа прогона: её никто не зовёт, поэтому
// производителем формы она быть не может.
func isTestEntryPoint(name string) bool {
	for _, p := range []string{"Test", "Benchmark", "Fuzz", "Example"} {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// dedupeBothFormsSites — одно выражение даёт вложенные вызовы, и каждый из них
// несёт обе формы. Координата у них одна, поэтому держатель считается по
// координате, а не по числу узлов разбора.
func dedupeBothFormsSites(in []BothFormsSite) []BothFormsSite {
	seen := map[string]bool{}
	out := in[:0]
	for _, s := range in {
		key := s.File + ":" + strconv.Itoa(s.Line) + ":" + s.Form
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, s)
	}
	return out
}

// BothFormsVerdict — исход адъюдикации найденных мест.
type BothFormsVerdict struct {
	// Offenders — места, собравшие вход там, где его больше никто не
	// производит. Каждое — находка.
	Offenders []BothFormsSite
	// AllowedByDir — сколько мест пришлось на каждый разрешённый каталог.
	AllowedByDir map[string]int
	// OrphanEntries — записи разрешения, которым больше нечего разрешать.
	// Такая запись есть слепая зона, выданная вперёд: следующая настоящая
	// находка того же каталога уедет под неё незамеченной.
	OrphanEntries []string
	// Holders — держателей (проб), а не мест: одно утверждение собирает вход
	// и двумя строками. Единица счёта названа, потому что их две.
	Holders int
}

// AdjudicateBothFormsProducers раскладывает найденные места на законные и
// находки. Гейт и его инъекция зовут ЭТУ функцию, а не копию правил.
func AdjudicateBothFormsProducers(sites []BothFormsSite, allowedDirs map[string]string) BothFormsVerdict {
	v := BothFormsVerdict{AllowedByDir: map[string]int{}}
	holders := map[string]bool{}
	for _, s := range sites {
		holders[s.File+"#"+s.Func] = true
		dir := dirOfRel(s.File)
		if _, ok := allowedDirs[dir]; ok {
			v.AllowedByDir[dir]++
			continue
		}
		v.Offenders = append(v.Offenders, s)
	}
	v.Holders = len(holders)
	for dir := range allowedDirs {
		if v.AllowedByDir[dir] == 0 {
			v.OrphanEntries = append(v.OrphanEntries, dir)
		}
	}
	sort.Strings(v.OrphanEntries)
	sort.Slice(v.Offenders, func(i, j int) bool {
		if v.Offenders[i].File != v.Offenders[j].File {
			return v.Offenders[i].File < v.Offenders[j].File
		}
		return v.Offenders[i].Line < v.Offenders[j].Line
	})
	return v
}

// dirOfRel — каталог REL-пути. Пути приходят с прямым слэшем из индекса git,
// поэтому разделитель здесь не платформенный.
func dirOfRel(rel string) string {
	if i := strings.LastIndex(rel, "/"); i >= 0 {
		return rel[:i]
	}
	return "."
}
