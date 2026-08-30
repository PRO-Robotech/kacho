// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path"
	"sort"
	"strconv"
	"strings"
)

// operationanyresolution.go — общая часть двух гейтов, стерегущих РАЗРЕШИМОСТЬ
// типов, которые владельцы кладут в `Operation.response` / `Operation.metadata`.
// Предмет целиком описан в `internal/operationany`; здесь только механика чтения
// дерева. Оба гейта живут в `operationanyresolution_test.go`.

// ─────────────────────────────────────────────────────────────────────────────
// Сторона ЛИНКОВКИ: что бинарь вообще способен построить и разрешить
// ─────────────────────────────────────────────────────────────────────────────

// goListFields — одна строка вывода `go list`: путь пакета и его файлы.
type goListPackage struct {
	ImportPath string
	GoFiles    []string
}

// goListDeps отдаёт транзитивное замыкание импортов пакета pkg вместе с составом
// файлов каждого. Формат вывода выбран однострочным (а не JSON) намеренно: JSON
// на 900 пакетов стоит на порядок дороже, а нужны ровно два поля.
func goListDeps(root, pkg string) ([]goListPackage, error) {
	cmd := exec.Command("go", "list", "-deps", // #nosec G204 — операнд из вывода `go list ./...` того же дерева
		"-f", "{{.ImportPath}}{{range .GoFiles}} {{.}}{{end}}", pkg)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list -deps %s: %w", pkg, err)
	}
	var pkgs []goListPackage
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		pkgs = append(pkgs, goListPackage{ImportPath: fields[0], GoFiles: fields[1:]})
	}
	return pkgs, nil
}

// goListMainPackages — все исполняемые пакеты дерева.
func goListMainPackages(root string) ([]string, error) {
	cmd := exec.Command("go", "list", "-f", `{{if eq .Name "main"}}{{.ImportPath}}{{end}}`, "./...")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list ./...: %w", err)
	}
	var mains []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			mains = append(mains, line)
		}
	}
	sort.Strings(mains)
	return mains, nil
}

// registersProtoMessages — регистрирует ли пакет proto-сообщения в реестре
// процесса.
//
// Признак — наличие файла `*.pb.go`: генератор кладёт регистрацию в `init()`
// именно туда, и другого способа попасть в `protoregistry.GlobalTypes` у
// сгенерированного кода нет. Признак читает СОСТАВ файлов, а не их содержимое:
// вопрос «регистрирует ли» решается тем, что пакет сгенерирован, а не тем, какие
// слова в нём написаны.
//
// Признак ШИРЕ предмета в безопасную сторону: пакет с одними лишь gRPC-заглушками
// (`*_grpc.pb.go`) сообщений не регистрирует, но будет засчитан. Ошибка в эту
// сторону даёт лишнее требование к краю, а не пропущенный тип, — и на дереве она
// сегодня не срабатывает ни разу (перепись печатается гейтом).
func (p goListPackage) registersProtoMessages() bool {
	for _, f := range p.GoFiles {
		if strings.HasSuffix(f, ".pb.go") {
			return true
		}
	}
	return false
}

// binaryProtoSurface — proto-регистрирующие пакеты, влинкованные в бинарь, и
// признак линковки заданного пакета-маркера (по нему выводится РОЛЬ бинаря).
type binaryProtoSurface struct {
	Command string
	Proto   map[string]bool
	Links   map[string]bool
}

// readBinaryProtoSurface — один вызов `go list -deps` отвечает на оба вопроса о
// бинаре сразу: что он способен зарегистрировать и какие пакеты-маркеры линкует.
func readBinaryProtoSurface(root, cmdPkg string, markers []string) (binaryProtoSurface, error) {
	deps, err := goListDeps(root, cmdPkg)
	if err != nil {
		return binaryProtoSurface{}, err
	}
	surface := binaryProtoSurface{
		Command: cmdPkg,
		Proto:   map[string]bool{},
		Links:   map[string]bool{},
	}
	markerSet := map[string]bool{}
	for _, m := range markers {
		markerSet[m] = true
	}
	for _, d := range deps {
		if d.registersProtoMessages() {
			surface.Proto[d.ImportPath] = true
		}
		if markerSet[d.ImportPath] {
			surface.Links[d.ImportPath] = true
		}
	}
	return surface, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Сторона ДЕРЕВА: где владельцы упаковывают в `Any`
// ─────────────────────────────────────────────────────────────────────────────

// anyPackSite — одно место упаковки, чей тип НАПИСАН в самом месте вызова.
type anyPackSite struct {
	File    string // rel-путь от корня дерева
	Line    int
	Package string // путь Go-пакета упакованного типа
	Name    string // имя Go-типа
	Form    string // распознанная форма записи — печатается в находке
}

// GoCoordinate — как этот тип пишется в паре «пакет.Тип».
func (s anyPackSite) GoCoordinate() string { return path.Base(s.Package) + "." + s.Name }

// anyPackCensus — перепись обхода. Печатается ЦЕЛИКОМ: «ноль находок» обязано
// быть отличимо от «ноль прочитанного», а разрешённые синтаксически места — от
// тех, которых распознаватель судить не может.
type anyPackCensus struct {
	FilesRead int
	CallsSeen int           // всего мест упаковки, найденных обходом
	Written   []anyPackSite // тип написан в месте вызова — судимы
	Unwritten int           // ГРАНИЦА: аргумент назван переменной или вызовом
	// Resolutions — места, где код РАЗРЕШАЕТ тип по адресу через реестр
	// (`Any.UnmarshalNew`, `protojson.Unmarshal`). Считается тем же обходом:
	// вопрос «кто потребитель предмета» решается синтаксисом, а не подстрокой —
	// иначе имя в комментарии, объясняющем эту самую защиту, давало бы находку.
	Resolutions int
	ParseFailed []string
}

// anyPackFormNames — формы записи, которые распознаватель СУДИТ. Перечень
// печатается переписью и покрыт инъекцией по каждой строке: форма, о которой
// распознаватель не знает, — не редкость и не край, а невидимость.
var anyPackFormNames = []string{
	"anypb.New(&pkg.T{})",
	"anypb.New(pkg.T{})",
	"anypb.New(new(pkg.T))",
	"anypb.New((*pkg.T)(nil))",
	"anypb.MarshalFrom(dst, &pkg.T{}, opts)",
}

// collectAnyPackSites обходит заданные файлы дерева и собирает места упаковки в
// `Any`, чей тип написан в самом месте вызова.
//
// # Что распознаётся и почему именно так
//
// Разбор идёт по СИНТАКСИЧЕСКОМУ ДЕРЕВУ, а не по тексту: слова `anypb.New` и
// `emptypb.Empty` стоят в комментариях, объясняющих ровно эту защиту (в том
// числе в шапке `internal/operationany`), и текстовый поиск зеленел бы тем
// увереннее, чем лучше место задокументировано.
//
// Псевдоним импорта учитывается: имя пакета в месте вызова резолвится через блок
// импортов ЭТОГО файла, а не угадывается по последнему сегменту пути.
//
// # Граница, названная вслух
//
// Аргумент, чей тип в месте вызова НЕ НАПИСАН — `anypb.New(v)`,
// `anypb.New(protoconv.Instance(x))`, — синтаксически неразрешим: его тип знает
// только проверка типов. Такие места здесь НЕ СУДЯТСЯ и НЕ ЗАМАЛЧИВАЮТСЯ — они
// считаются в переписи полем Unwritten. Их держит второй гейт этой пары
// (`TestEdgeResolvesEveryProtoPackageItsOwnersCanProduce`), который о форме
// записи не спрашивает вовсе: он требует, чтобы край линковал всякий
// proto-пакет, влинкованный во владельца, — а тип, которого во владельце нет,
// он и упаковать не может. То есть граница этого распознавателя лежит ВНУТРИ
// предмета соседа, а не снаружи обоих.
func collectAnyPackSites(root string, relFiles []string) anyPackCensus {
	var census anyPackCensus
	fset := token.NewFileSet()
	for _, rel := range relFiles {
		body, err := os.ReadFile(path.Join(root, rel)) // #nosec G304 — путь из индекса git
		if err != nil {
			census.ParseFailed = append(census.ParseFailed, rel+": "+err.Error())
			continue
		}
		file, err := parser.ParseFile(fset, rel, body, parser.SkipObjectResolution)
		if err != nil {
			census.ParseFailed = append(census.ParseFailed, rel+": "+err.Error())
			continue
		}
		census.FilesRead++
		byName, byPath := fileImports(file)
		// anyName пуст, если файл `anypb` не импортирует: тогда упаковки в нём
		// нет by construction, но разрешения типов по адресу — быть могут, и
		// обход всё равно идёт.
		anyName := byPath["google.golang.org/protobuf/types/known/anypb"]
		_, protojsonImported := byPath["google.golang.org/protobuf/encoding/protojson"]
		ast.Inspect(file, func(n ast.Node) bool {
			call, isCall := n.(*ast.CallExpr)
			if !isCall {
				return true
			}
			if isRegistryResolution(call, byName, protojsonImported) {
				census.Resolutions++
			}
			arg, form, isPack := anyPackArgument(call, anyName)
			if !isPack {
				return true
			}
			census.CallsSeen++
			pkgName, typeName, written := writtenMessageType(arg)
			if !written {
				census.Unwritten++
				return true
			}
			full, resolved := byName[pkgName]
			if !resolved {
				// Имя пакета не резолвится блоком импортов файла: это либо
				// точечный импорт, либо локальный тип. Судить нечем.
				census.Unwritten++
				return true
			}
			census.Written = append(census.Written, anyPackSite{
				File:    rel,
				Line:    fset.Position(call.Pos()).Line,
				Package: full,
				Name:    typeName,
				Form:    form,
			})
			return true
		})
	}
	return census
}

// fileImports — блок импортов файла, прочитанный в обе стороны: byName даёт путь
// пакета по имени, под которым он виден В ЭТОМ ФАЙЛЕ, byPath — обратное.
//
// Псевдоним берётся из объявления; без псевдонима — последний сегмент пути. Это
// приближение (имя пакета не обязано совпадать с последним сегментом), и оно
// верно для всех пакетов, задействованных здесь: `anypb`, `emptypb` и прочие
// `types/known/*` названы по сегменту. Ошибка приближения даёт нерезолвившееся
// имя, то есть уход места в границу Unwritten, — а не ложную находку.
func fileImports(file *ast.File) (byName, byPath map[string]string) {
	byName, byPath = map[string]string{}, map[string]string{}
	for _, spec := range file.Imports {
		p, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		name := path.Base(p)
		if spec.Name != nil {
			name = spec.Name.Name
		}
		if name == "_" || name == "." {
			// Пустой и точечный импорт имени в месте вызова не дают: тип из
			// такого пакета уйдёт в границу, а не в ложную находку.
			continue
		}
		byName[name] = p
		byPath[p] = name
	}
	return byName, byPath
}

// anyPackArgument — является ли вызов упаковкой в `Any`, и какое выражение
// упаковывается. Возвращает также имя распознанной формы: находка обязана
// называть не только координату, но и то, чем она распознана.
func anyPackArgument(call *ast.CallExpr, anyPkgName string) (ast.Expr, string, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil, "", false
	}
	ident, ok := sel.X.(*ast.Ident)
	if !ok || ident.Name != anyPkgName {
		return nil, "", false
	}
	switch sel.Sel.Name {
	case "New":
		if len(call.Args) != 1 {
			return nil, "", false
		}
		return call.Args[0], "anypb.New", true
	case "MarshalFrom":
		if len(call.Args) < 2 {
			return nil, "", false
		}
		return call.Args[1], "anypb.MarshalFrom", true
	}
	return nil, "", false
}

// writtenMessageType — тип, НАПИСАННЫЙ в выражении: имя пакета и имя типа.
//
// Судимые формы (каждая покрыта инъекцией):
//
//	&pkg.T{}      — составной литерал под взятием адреса;
//	pkg.T{}       — тот же литерал без адреса;
//	new(pkg.T)    — встроенный конструктор;
//	(*pkg.T)(nil) — типизированный нуль.
func writtenMessageType(expr ast.Expr) (string, string, bool) {
	switch e := expr.(type) {
	case *ast.UnaryExpr:
		if e.Op == token.AND {
			return writtenMessageType(e.X)
		}
	case *ast.ParenExpr:
		return writtenMessageType(e.X)
	case *ast.CompositeLit:
		return selectorNames(e.Type)
	case *ast.StarExpr:
		return selectorNames(e.X)
	case *ast.CallExpr:
		// new(pkg.T)
		if fn, ok := e.Fun.(*ast.Ident); ok && fn.Name == "new" && len(e.Args) == 1 {
			return selectorNames(e.Args[0])
		}
		// (*pkg.T)(nil) — приведение к типизированному нулю.
		if paren, ok := e.Fun.(*ast.ParenExpr); ok {
			return writtenMessageType(paren.X)
		}
	}
	return "", "", false
}

// selectorNames — «pkg.T» разобранное на имя пакета и имя типа.
func selectorNames(expr ast.Expr) (string, string, bool) {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return "", "", false
	}
	ident, ok := sel.X.(*ast.Ident)
	if !ok {
		return "", "", false
	}
	return ident.Name, sel.Sel.Name, true
}

// ─────────────────────────────────────────────────────────────────────────────
// Решение о полноте: отделено от чтения дерева, чтобы инъекция подавала ему
// поверхности напрямую — не повторяя логику гейта своей копией и не записывая
// ничего в живое дерево.
// ─────────────────────────────────────────────────────────────────────────────

// protoSurfaceFinding — владелец, способный построить типы, которых край не
// разрешит.
type protoSurfaceFinding struct {
	Owner   string
	Missing []string
}

// auditProtoSurfaces — то самое решение: proto-пакеты владельца обязаны быть
// подмножеством proto-пакетов края.
func auditProtoSurfaces(edge binaryProtoSurface, owners []binaryProtoSurface) []protoSurfaceFinding {
	var findings []protoSurfaceFinding
	for _, owner := range owners {
		var missing []string
		for pkg := range owner.Proto {
			if !edge.Proto[pkg] {
				missing = append(missing, pkg)
			}
		}
		if len(missing) == 0 {
			continue
		}
		sort.Strings(missing)
		findings = append(findings, protoSurfaceFinding{Owner: owner.Command, Missing: missing})
	}
	sort.Slice(findings, func(i, j int) bool { return findings[i].Owner < findings[j].Owner })
	return findings
}

// isRegistryResolution — разрешает ли вызов тип по адресу через реестр типов
// процесса. Две формы, и обе — потребление того же предмета, что у края:
//
//	<любой Any>.UnmarshalNew()  — распаковка по адресу;
//	protojson.Unmarshal(...)    — разбор сообщения, чей Any резолвится реестром.
//
// Признак ШИРЕ предмета: `UnmarshalNew` бывает методом чужого типа. Ошибка в эту
// сторону даёт лишний вопрос к читателю, а не пропущенного потребителя.
func isRegistryResolution(call *ast.CallExpr, byName map[string]string, protojsonImported bool) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	if sel.Sel.Name == "UnmarshalNew" {
		return true
	}
	if !protojsonImported || sel.Sel.Name != "Unmarshal" {
		return false
	}
	ident, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	return byName[ident.Name] == "google.golang.org/protobuf/encoding/protojson"
}
