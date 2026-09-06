// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package provider

// Имя типа ресурса и пространство ключа повторной подачи — ОДНО значение из ОДНОГО
// объявления.
//
// # Что здесь проверяется и почему это не косметика
//
// Провайдер называет своё имя типа в `Metadata`, а на край шлёт ключ повторной подачи,
// первым слагаемым которого стоит ТО ЖЕ имя. Пока это два написания, они могут разойтись
// молча: обе стороны по отдельности защитимы, компилятор различия не видит, а обзор
// изменения видит одну строку из двух. Разойдясь, они дают не отказ, а тихую потерю защиты
// от дубля: повтор запроса после потерянного ответа приносит ДРУГОЙ ключ, край считает его
// новым намерением и заводит второй ресурс.
//
// # Проверок ДВЕ, и они про разное — снимать ни одну нельзя
//
// Первая — «оба написания дают ОДНО ЗНАЧЕНИЕ». Она переживает любую форму записи и ловит
// расхождение, каким бы способом оно ни возникло.
//
// Вторая — «оба написания читают ОДНО ОБЪЯВЛЕНИЕ». Она требует не значения, а построения:
// там, где источник один, расхождение не «маловероятно», а НЕВЫРАЗИМО — второго места,
// которое могло бы отстать, попросту нет.
//
// Прежняя редакция несла только первую и объясняла отказ от второй так: «константа сводит
// только те места, которые её читают, а литерал остаётся законной формой языка». Довод
// верен ровно наполовину. Верно, что литерал вписать МОЖНО, — потому первая проверка и
// остаётся страховкой. Неверно, что из этого следует мириться с двойной формой: пока она
// в дереве, всякий следующий ресурс пишется по образцу соседа, и число двойных мест
// растёт. Корпус предпочитает невыразимое построением проверяемому — проверку можно снять,
// обойти или не позвать, построение нельзя, — поэтому вторая проверка требует единого
// источника, а первая продолжает страховать форму, которую построение уже сделало
// невыразимой.
//
// Требуется при этом не КОНСТАНТА, а ИМЯ: годится и константа пакета, и поле описания
// ресурса (`r.spec.tfName`), из которого имя приходит в момент работы. Требование
// конкретной формы записи запрещало бы вторую законную форму без нужды.
//
// # Почему разбор, а не поиск по образцу
//
// Имя типа встречается в этом пакете в прозе комментариев, в текстах отказов и в примерах
// документации ресурса. Поиск по образцу не отличает объявление от рассказа о нём и краснел
// бы на собственном объяснении. Судится узел синтаксического дерева: присваивание
// `resp.TypeName` и аргумент вызова.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// createCallSites — вызовы, содержательным аргументом которых идёт пространство ключа
// повторной подачи, и позиция этого аргумента.
//
// Оба пути настоящие: общий (`awaitCreate` доводит создание до операции) и прямой (ресурсы,
// чьё создание операцией не заканчивается). Предикат, знающий один, молчал бы о половине
// ресурсов.
var createCallSites = map[string]int{
	"client.IdempotencyKey": 0,
	"awaitCreate":           4,
}

// typeNameExpr — написание имени типа: чем оно является и откуда взято.
type typeNameExpr struct {
	// value — значение, вычисленное статически. Пустое означает «статически не
	// вычислимо»: имя приходит из таблицы описаний в момент работы.
	value string
	// source — текст выражения-источника. По нему два написания сверяются на то, что
	// они суть ОДНО объявление, а не два совпадающих.
	source string
	// symbol означает «источник — ИМЯ»: константа пакета либо поле описания. Литерал
	// именем не является, даже когда два литерала совпадают дословно: это два
	// объявления, каждое из которых может уехать отдельно.
	symbol bool
	pos    token.Pos
}

// effective — то, по чему два написания сравниваются на равенство ЗНАЧЕНИЯ.
//
// Статически невычислимое имя сравнивается по выражению: то же выражение с обеих сторон
// означает одно значение by construction, разные выражения — два независимых пути.
func (e typeNameExpr) effective() string {
	if e.value != "" {
		return e.value
	}
	return e.source
}

// display — написание в тексте отказа.
func (e typeNameExpr) display() string {
	if e.value != "" {
		return strconv.Quote(e.value)
	}
	return e.source
}

// typeNameDecl — файл ресурса: что он объявляет именем типа и чем подаёт ключ.
type typeNameDecl struct {
	file     string
	declared typeNameExpr
	sites    []typeNameExpr
}

// namespaceAudit — разбор пакета целиком.
type namespaceAudit struct {
	fset *token.FileSet
	// files — сколько не-тестовых файлов прочитано. «Ноль находок» обязано быть отличимо
	// от «ноль прочитанного».
	files int
	decls []typeNameDecl
	// throughCommonPath — аргументы, сводимые только к параметру функции: это общий путь,
	// куда имя приходит от вызывающего и сверено у него. Названо числом, а не пропущено
	// молча.
	throughCommonPath int
}

// auditIdempotencyNamespaces разбирает пакет провайдера, лежащий в dir.
//
// Каталог — параметр, а не текущий каталог пробы: доказательство падучести подаёт гейту
// КОПИЮ дерева с внесённым дефектом, и состояние, которого проверка не заводила, она не
// трогает.
func auditIdempotencyNamespaces(dir string) (namespaceAudit, error) {
	audit := namespaceAudit{fset: token.NewFileSet()}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return audit, fmt.Errorf("каталог пакета не прочитан: %w", err)
	}

	// Константы собираются ПО ВСЕМУ пакету: объявление имени и его использование лежат в
	// разных файлах by construction — в том и смысл единого словаря.
	files := map[string]*ast.File{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(audit.fset, dir+string(os.PathSeparator)+name, nil, 0)
		if err != nil {
			return audit, fmt.Errorf("разбор %s: %w", name, err)
		}
		files[name] = f
	}
	audit.files = len(files)
	if audit.files == 0 {
		return audit, fmt.Errorf("в каталоге %s не прочитано ни одного не-тестового файла пакета", dir)
	}

	consts := collectStringConsts(files)
	// Имя провайдера читается из РАЗБИРАЕМОГО дерева, а не из скомпилированной
	// константы: гейт обязан судить то дерево, которое ему подали.
	providerName, ok := consts["providerTypeName"]
	if !ok {
		return audit, fmt.Errorf("в %s не объявлено providerTypeName — составные имена типов не с чем сводить", dir)
	}

	for _, name := range sortedFileNames(files) {
		f := files[name]
		declared, hasDecl := declaredTypeNameOf(f, consts, providerName)
		sites, unresolved := createNamespaceArgs(f, consts, providerName)
		audit.throughCommonPath += unresolved

		if !hasDecl {
			// Файл без объявления имени типа, но с вызовом создания: имя туда пришло
			// от вызывающего и сверено у него.
			audit.throughCommonPath += len(sites)
			continue
		}
		audit.decls = append(audit.decls, typeNameDecl{file: name, declared: declared, sites: sites})
	}
	return audit, nil
}

// census — числа переписи и их текст.
func (a namespaceAudit) census() (comparable, singleSource, checkedSites int, lines []string) {
	var names, withoutSite, derivedNames []string
	for _, d := range a.decls {
		names = append(names, d.declared.display())
		if len(d.sites) == 0 {
			// Ресурс, чьё создание в этом файле не найдено, сверке недоступен.
			// Названный числом, он виден; спрятанный в «сошлось» — нет.
			withoutSite = append(withoutSite, d.file)
			continue
		}
		comparable++
		checkedSites += len(d.sites)
		if d.singleSource() {
			singleSource++
			derivedNames = append(derivedNames, d.declared.display())
		}
	}
	sort.Strings(names)
	sort.Strings(withoutSite)
	sort.Strings(derivedNames)

	lines = append(lines, fmt.Sprintf(
		"осмотрено: файлов %d, объявлений имени типа %d, из них со сверяемым созданием %d, "+
			"выведено из одного объявления %d; сверено вызовов создания %d, на общем пути %d",
		a.files, len(a.decls), comparable, singleSource, checkedSites, a.throughCommonPath))
	lines = append(lines, "имена типов: "+strings.Join(names, ", "))
	// Единица переписи — ОБЪЯВЛЕНИЕ, а не ресурс: ресурс, описанный таблицей, объявляет
	// имя однажды на несколько ресурсов и стоит в перечне одним выражением. Сказано
	// прямо, иначе число объявлений читается как число ресурсов, и разница между
	// «покрыто всё» и «часть не прочитана» становится неразличимой.
	lines = append(lines, "единица счёта — объявление: описанные таблицей ресурсы "+
		"объявляют имя однажды на несколько ресурсов")
	if len(withoutSite) > 0 {
		lines = append(lines, "объявляют имя типа, но создания в своём файле не содержат "+
			"(сверке недоступны): "+strings.Join(withoutSite, ", "))
	}
	return comparable, singleSource, checkedSites, lines
}

// singleSource — читают ли оба написания ОДНО объявление.
//
// Условий два, и оба обязательны: источник обязан быть ИМЕНЕМ (литерал именем не является,
// даже когда два литерала совпадают дословно — это два объявления) и он обязан быть ТЕМ ЖЕ
// именем с обеих сторон.
func (d typeNameDecl) singleSource() bool {
	if !d.declared.symbol {
		return false
	}
	for _, s := range d.sites {
		if !s.symbol || s.source != d.declared.source {
			return false
		}
	}
	return true
}

// namespaceValueFindings — расхождения ЗНАЧЕНИЙ двух написаний.
func (a namespaceAudit) namespaceValueFindings() []string {
	var out []string
	for _, d := range a.decls {
		for _, site := range d.sites {
			if site.effective() == d.declared.effective() {
				continue
			}
			out = append(out, fmt.Sprintf(
				"%s: имя типа и пространство ключа повторной подачи разошлись.\n"+
					"  объявлено в Metadata (%s): %s\n"+
					"  отправляется на край (%s): %s\n"+
					"Ключ повторной подачи считается от имени типа: разойдясь, два написания "+
					"тихо снимают защиту от дубля — повтор после потерянного ответа приносит "+
					"другой ключ, и край заводит второй ресурс.",
				d.file,
				a.fset.Position(d.declared.pos).String(), d.declared.display(),
				a.fset.Position(site.pos).String(), site.display()))
		}
	}
	return out
}

// namespaceSourceFindings — написания, читающие РАЗНЫЕ объявления.
func (a namespaceAudit) namespaceSourceFindings() []string {
	var out []string
	for _, d := range a.decls {
		if len(d.sites) == 0 || d.singleSource() {
			continue
		}
		site := d.sites[0]
		out = append(out, fmt.Sprintf(
			"%s: имя типа и пространство ключа повторной подачи записаны ДВУМЯ объявлениями.\n"+
				"  объявлено в Metadata (%s): %s\n"+
				"  отправляется на край (%s): %s\n"+
				"Сегодня они, возможно, совпадают — но совпадение держится проверкой, а не "+
				"построением: два места могут разойтись при любой правке. Объявите имя типа "+
				"ОДИН раз (константа пакета либо поле описания ресурса) и прочитайте её с "+
				"обеих сторон — тогда «переехало одно, не переехало второе» перестанет быть "+
				"представимым.",
			d.file,
			a.fset.Position(d.declared.pos).String(), d.declared.display(),
			a.fset.Position(site.pos).String(), site.display()))
	}
	return out
}

// Оба написания дают ОДНО ЗНАЧЕНИЕ. Страховка формы, которую построение уже сделало
// невыразимой: литерал остаётся законной формой языка и может появиться снова.
func TestIdempotencyNamespaceAgreesWithTheDeclaredTypeName(t *testing.T) {
	audit, err := auditIdempotencyNamespaces(".")
	if err != nil {
		t.Fatalf("разбор пакета: %v — обход пуст, вердикт беспредметен", err)
	}
	comparable, _, checkedSites, lines := audit.census()

	for _, f := range audit.namespaceValueFindings() {
		t.Error(f)
	}

	if checkedSites == 0 {
		t.Fatal("сверено ноль вызовов создания — предикат обхода устарел, и «расхождений нет» " +
			"означало бы «ничего не прочитано»")
	}
	if comparable == 0 {
		t.Fatal("объявлений имени типа со сверяемым созданием не найдено — сверять не с чем")
	}
	for _, l := range lines {
		t.Log(l)
	}
}

// Оба написания читают ОДНО ОБЪЯВЛЕНИЕ. Требование к построению, а не к значению.
func TestTypeNameAndIdempotencyNamespaceComeFromOneDeclaration(t *testing.T) {
	audit, err := auditIdempotencyNamespaces(".")
	if err != nil {
		t.Fatalf("разбор пакета: %v — обход пуст, вердикт беспредметен", err)
	}
	comparable, singleSource, _, lines := audit.census()

	for _, f := range audit.namespaceSourceFindings() {
		t.Error(f)
	}

	if comparable == 0 {
		t.Fatal("объявлений имени типа со сверяемым созданием не найдено — требовать единого " +
			"источника не от чего")
	}
	if singleSource != comparable {
		t.Errorf("выведено из одного объявления %d из %d — перепись не сошлась",
			singleSource, comparable)
	}
	for _, l := range lines {
		t.Log(l)
	}
}

// resourceMetadataFunc — метод `Metadata` РЕСУРСА, если файл его объявляет.
func resourceMetadataFunc(f *ast.File) *ast.FuncDecl {
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "Metadata" || fn.Type.Params == nil {
			continue
		}
		params := fn.Type.Params.List
		if len(params) == 0 {
			continue
		}
		star, ok := params[len(params)-1].Type.(*ast.StarExpr)
		if !ok {
			continue
		}
		if exprText(star.X) == "resource.MetadataResponse" {
			return fn
		}
	}
	return nil
}

// declaredTypeNameOf — имя типа, объявляемое файлом в его Metadata.
func declaredTypeNameOf(f *ast.File, consts map[string]string, providerName string) (typeNameExpr, bool) {
	var (
		out   typeNameExpr
		found bool
	)
	// Метод `Metadata` есть и у САМОГО провайдера — он называет своё имя тем же полем.
	// Различает не имя переменной (у обоих `resp`), а тип ответа: ресурс отвечает
	// `*resource.MetadataResponse`, провайдер — `*provider.MetadataResponse`. Спутать их
	// значило бы сверять имя провайдера с пространством ключа ресурса.
	decl := resourceMetadataFunc(f)
	if decl == nil {
		return out, false
	}
	ast.Inspect(decl, func(n ast.Node) bool {
		asn, ok := n.(*ast.AssignStmt)
		if !ok || len(asn.Lhs) != 1 || len(asn.Rhs) != 1 {
			return true
		}
		sel, ok := asn.Lhs[0].(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "TypeName" {
			return true
		}
		ident, ok := sel.X.(*ast.Ident)
		if !ok || ident.Name != "resp" {
			return true
		}
		if e, ok := resolveTypeExpr(asn.Rhs[0], consts, providerName); ok {
			e.pos = asn.Pos()
			out, found = e, true
			return false
		}
		return true
	})
	return out, found
}

// createNamespaceArgs — аргументы пространства ключа во всех вызовах создания файла.
//
// Второе значение — число аргументов, не сводимых ни к значению, ни к выражению-источнику:
// это общий путь, куда имя приходит параметром. Возвращается числом, а не пропускается
// молча: иначе «сверено N» не отличалось бы от «прочитано N».
func createNamespaceArgs(f *ast.File, consts map[string]string, providerName string) ([]typeNameExpr, int) {
	var out []typeNameExpr
	unresolved := 0
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		idx, ok := createCallSites[calleeName(call.Fun)]
		if !ok || idx >= len(call.Args) {
			return true
		}
		e, ok := resolveTypeExpr(call.Args[idx], consts, providerName)
		if !ok {
			unresolved++
			return true
		}
		e.pos = call.Args[idx].Pos()
		out = append(out, e)
		return true
	})
	return out, unresolved
}

// resolveTypeExpr сводит выражение к написанию имени типа.
//
// Форм ЧЕТЫРЕ, и все четыре настоящие: строковый литерал, склейка с именем провайдера,
// константа пакета и поле описания ресурса. Предикат, знающий три из четырёх, объявил бы
// часть ресурсов несверяемыми — и молчал бы там, где расхождение как раз и опасно.
func resolveTypeExpr(e ast.Expr, consts map[string]string, providerName string) (typeNameExpr, bool) {
	switch v := e.(type) {
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return typeNameExpr{}, false
		}
		s, err := strconv.Unquote(v.Value)
		if err != nil {
			return typeNameExpr{}, false
		}
		return typeNameExpr{value: s, source: strconv.Quote(s)}, true
	case *ast.Ident:
		s, ok := consts[v.Name]
		if !ok {
			return typeNameExpr{}, false
		}
		return typeNameExpr{value: s, source: v.Name, symbol: true}, true
	case *ast.BinaryExpr:
		if v.Op != token.ADD {
			return typeNameExpr{}, false
		}
		left := exprText(v.X)
		right, ok := resolveTypeExpr(v.Y, consts, providerName)
		if left != "req.ProviderTypeName" || !ok || right.value == "" {
			return typeNameExpr{}, false
		}
		// Склейка с именем провайдера — ДВА объявления, а не одно: суффикс вписан на
		// месте, и второе его написание (в ключе подачи) от первого не зависит.
		return typeNameExpr{value: providerName + right.value, source: left + " + " + right.source}, true
	case *ast.SelectorExpr:
		// `r.spec.tfName` и `r.kind.tfName` — имя приходит из таблицы описаний и
		// вычисляется в момент работы. Источник при этом ОДИН: то же поле того же
		// описания читают оба написания.
		if v.Sel.Name != "tfName" {
			return typeNameExpr{}, false
		}
		return typeNameExpr{source: exprText(v), symbol: true}, true
	}
	return typeNameExpr{}, false
}

// calleeName — имя вызываемого: `f`, `pkg.F` либо `x.f`.
func calleeName(e ast.Expr) string { return exprText(e) }

// exprText — выражение в виде текста, достаточного для сравнения его с самим собой.
func exprText(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.SelectorExpr:
		return exprText(v.X) + "." + v.Sel.Name
	}
	return ""
}

// collectStringConsts — строковые константы пакета.
//
// Разрешение идёт до НЕПОДВИЖНОЙ ТОЧКИ, а не одним проходом: имя типа платформы объявлено
// как склейка имени провайдера с суффиксом (`providerTypeName + "_vpc_network"`), то есть
// значение одной константы вычисляется через другую, и порядок их объявления в файлах
// произволен. Проход, знающий только литералы, объявил бы такие имена невычислимыми — и
// молчал бы о ресурсах, которые как раз и сведены к одному объявлению.
func collectStringConsts(files map[string]*ast.File) map[string]string {
	type pending struct {
		name string
		expr ast.Expr
	}
	var todo []pending
	for _, name := range sortedFileNames(files) {
		for _, d := range files[name].Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			for _, sp := range gd.Specs {
				vs, ok := sp.(*ast.ValueSpec)
				if !ok || len(vs.Names) != len(vs.Values) {
					continue
				}
				for i, n := range vs.Names {
					todo = append(todo, pending{name: n.Name, expr: vs.Values[i]})
				}
			}
		}
	}

	out := map[string]string{}
	for progress := true; progress; {
		progress = false
		rest := todo[:0]
		for _, p := range todo {
			s, ok := constStringValue(p.expr, out)
			if !ok {
				rest = append(rest, p)
				continue
			}
			out[p.name] = s
			progress = true
		}
		todo = rest
	}
	return out
}

// constStringValue — значение константного строкового выражения по уже известным.
func constStringValue(e ast.Expr, known map[string]string) (string, bool) {
	switch v := e.(type) {
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(v.Value)
		return s, err == nil
	case *ast.Ident:
		s, ok := known[v.Name]
		return s, ok
	case *ast.BinaryExpr:
		if v.Op != token.ADD {
			return "", false
		}
		l, lok := constStringValue(v.X, known)
		r, rok := constStringValue(v.Y, known)
		if !lok || !rok {
			return "", false
		}
		return l + r, true
	}
	return "", false
}

func sortedFileNames(m map[string]*ast.File) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
