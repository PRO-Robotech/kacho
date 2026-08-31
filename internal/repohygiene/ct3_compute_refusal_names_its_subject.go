// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// Гейт класса: отказ, называющий РОДИТЕЛЯ, а говорящий о его ПОДПОЛЯХ.
//
// `serviceerr.InvalidArg(field, desc)` принимает имя поля и текст правила ДВУМЯ
// независимыми аргументами. Это два утверждения об одном предмете, и расходятся
// они молча: правят текст, не правят имя. Тогда машиночитаемое
// `google.rpc.BadRequest.field_violations[].field` указывает на поле, к которому
// правило не относится, — а клиент-автомат (провайдер, консоль) действует именно
// по нему.
//
// Наблюдалось (#1724): отказ назывался по `boot_source` и перечислял текстом
// четыре его подполя как output-only. Родитель ОБЯЗАТЕЛЕН, поэтому вызывающий,
// сняв названное, получал следующий отказ — уже об обязательности. Два круга
// запроса вместо одного, и первый уводил в неверную сторону.
//
// ПОЧЕМУ РАСПОЗНАВАТЕЛЬ ОПИРАЕТСЯ НА КОНТРАКТ, А НЕ НА ТЕКСТ. Первая редакция
// искала «после имени поля стоит другой идентификатор» — над естественным языком
// это даёт 7 кандидатов на 153 паре и 6 из них ложные: `instanceKind CONTAINER`
// (значение перечисления), `bootSource.type registry.image` (значение),
// `description length` / `email length` (английское существительное). Инструмент,
// у которого 86 % находок ложные, перестают читать.
//
// Разделяет их ровно один вопрос: является ли названный токен НАСТОЯЩИМ подполем
// объявленного поля. Ответ даёт контракт, а не проза, — и все четыре ложных
// исключаются BY CONSTRUCTION: у скалярного поля подполей нет вовсе, поэтому
// `instance_kind`, `boot_source.type`, `description` до правила не доходят.

// ct3ComputeRefusal — одна пара «объявленное поле · текст правила», как её увидит
// клиент.
type ct3ComputeRefusal struct {
	File  string
	Line  int
	Field string // первый аргумент: путь поля (snake_case)
	Desc  string // второй аргумент: текст правила
}

// ct3ComputeFinding — отказ, чей текст говорит о подполях объявленного поля.
type ct3ComputeFinding struct {
	ct3ComputeRefusal
	Children []string // подполя, названные текстом
}

// ct3ComputeCensus — объём осмотренного. Печатается ВСЕГДА: «ноль находок»
// обязано быть отличимо от «ноль прочитанного».
type ct3ComputeCensus struct {
	FilesRead      int // файлов прод-кода прочитано
	Refusals       int // пар «поле · текст» распознано
	Judgeable      int // из них тех, чьё объявленное поле ИМЕЕТ подполя в контракте
	ContractFields int // полей-сообщений в контракте compute (предпосылка гейта)
}

// ct3ComputeRefusalCalls — имена вызовов, производящих пару «поле · текст».
// Перечень выводится из дерева, а не из памяти: см. пробу предпосылки.
var ct3ComputeRefusalCalls = map[string]bool{
	"InvalidArg":        true, // serviceerr.InvalidArg
	"ErrInvalidArg":     true, // shared.ErrInvalidArg
	"AddFieldViolation": true, // coreerrors.Builder
	"add":               true, // локальное замыкание над AddFieldViolation
}

// ct3ComputeFieldPath — первый аргумент похож на путь поля контракта.
var ct3ComputeFieldPath = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$`)

// ct3ComputeChildIndex — подполя полей-сообщений контракта compute: имя поля
// (snake) → множество имён его подполей в обеих формах записи (snake и camel),
// приведённых к общему виду.
//
// Индекс строится по ДЕСКРИПТОРАМ, а не по тексту `.proto`: дескрипторы — то же
// самое, что видит вызывающий на проводе, и они не расходятся с генерацией.
func ct3ComputeChildIndex() (map[string]map[string]bool, int) {
	idx := map[string]map[string]bool{}
	total := 0
	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		if !strings.HasPrefix(string(fd.Package()), "kacho.cloud.compute") {
			return true
		}
		var walk func(msgs protoreflect.MessageDescriptors)
		walk = func(msgs protoreflect.MessageDescriptors) {
			for i := 0; i < msgs.Len(); i++ {
				m := msgs.Get(i)
				for j := 0; j < m.Fields().Len(); j++ {
					f := m.Fields().Get(j)
					if f.Kind() != protoreflect.MessageKind && f.Kind() != protoreflect.GroupKind {
						continue
					}
					sub := f.Message()
					if sub == nil || sub.IsMapEntry() {
						continue
					}
					key := ct3ComputeNorm(string(f.Name()))
					if idx[key] == nil {
						idx[key] = map[string]bool{}
						total++
					}
					for k := 0; k < sub.Fields().Len(); k++ {
						c := sub.Fields().Get(k)
						idx[key][ct3ComputeNorm(string(c.Name()))] = true
						idx[key][ct3ComputeNorm(c.JSONName())] = true
					}
				}
				walk(m.Messages())
			}
		}
		walk(fd.Messages())
		return true
	})
	return idx, total
}

// ct3ComputeNorm — общий вид имени: без подчёркиваний, в нижнем регистре.
// `resolved_digest` и `resolvedDigest` — одно имя, записанное двумя законными
// формами.
func ct3ComputeNorm(s string) string {
	return strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(s, "_", ""), "[]", ""))
}

// ct3ComputeCamel — путь поля в форме контракта на проводе (JSON, camelCase).
func ct3ComputeCamel(path string) string {
	segs := strings.Split(path, ".")
	for i, seg := range segs {
		parts := strings.Split(seg, "_")
		for j := 1; j < len(parts); j++ {
			if parts[j] != "" {
				parts[j] = strings.ToUpper(parts[j][:1]) + parts[j][1:]
			}
		}
		segs[i] = strings.Join(parts, "")
	}
	return strings.Join(segs, ".")
}

const ct3ComputeIdent = `[A-Za-z_][A-Za-z0-9_]*`

// ct3ComputeNamedChildren — подполя объявленного поля, названные текстом отказа.
//
// Законных форм записи ДВЕ, и знать надо обе (иначе форма, о которой
// распознаватель не знает, оказывается не находкой, а невидимостью):
//
//	A. точечная      — `bootSource.imageKind is output-only`
//	B. квалификатор  — `bootSource name/resolvedDigest/imageKind are output-only`
//
// Форма B обязательно ЯКОРИТСЯ на имя объявленного поля вплотную. Без якоря
// «networkInterfaceSpecs[].primaryV4AddressSpec is not supported: the address is
// allocated by …» дало бы ложную находку на слове `address`, которое и вправду
// является подполем — но подлежащим тут не является.
func ct3ComputeNamedChildren(r ct3ComputeRefusal, children map[string]bool) []string {
	if len(children) == 0 {
		return nil
	}
	got := map[string]bool{}
	for _, head := range []string{ct3ComputeCamel(r.Field), r.Field} {
		// A: <поле>.<подполе>
		reA := regexp.MustCompile(regexp.QuoteMeta(head) + `(?:\[\])?\.(` + ct3ComputeIdent + `)`)
		for _, m := range reA.FindAllStringSubmatch(r.Desc, -1) {
			if children[ct3ComputeNorm(m[1])] {
				got[m[1]] = true
			}
		}
		// B: <поле> <подполе>[/<подполе>…]
		reB := regexp.MustCompile(regexp.QuoteMeta(head) + `(?:\[\])?\s+(` + ct3ComputeIdent + `(?:\s*[/,]\s*` + ct3ComputeIdent + `)*)`)
		for _, m := range reB.FindAllStringSubmatch(r.Desc, -1) {
			for _, t := range regexp.MustCompile(`[/,]`).Split(m[1], -1) {
				t = strings.TrimSpace(t)
				if children[ct3ComputeNorm(t)] {
					got[t] = true
				}
			}
		}
	}
	out := make([]string, 0, len(got))
	for k := range got {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ct3ComputeAuditRefusalSubjects — судит корпус исходников: отказ, чей текст
// говорит о подполях, обязан ОБЪЯВЛЯТЬ подполе, а не родителя.
//
// Принимает корпус картой «путь → исходник», а не читает дерево сам: тем же
// предикатом судится и настоящее дерево, и синтетика инъекции.
func ct3ComputeAuditRefusalSubjects(sources map[string]string, idx map[string]map[string]bool) ([]ct3ComputeFinding, ct3ComputeCensus) {
	var findings []ct3ComputeFinding
	cen := ct3ComputeCensus{}
	paths := make([]string, 0, len(sources))
	for p := range sources {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	for _, path := range paths {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, sources[path], 0)
		if err != nil {
			continue
		}
		cen.FilesRead++
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) != 2 {
				return true
			}
			var name string
			switch fn := call.Fun.(type) {
			case *ast.SelectorExpr:
				name = fn.Sel.Name
			case *ast.Ident:
				name = fn.Name
			}
			if !ct3ComputeRefusalCalls[name] {
				return true
			}
			field, ok1 := ct3ComputeLiteral(call.Args[0])
			desc, ok2 := ct3ComputeLiteral(call.Args[1])
			if !ok1 || !ok2 || !ct3ComputeFieldPath.MatchString(field) {
				return true
			}
			cen.Refusals++

			segs := strings.Split(field, ".")
			children := idx[ct3ComputeNorm(segs[len(segs)-1])]
			if len(children) == 0 {
				// Скалярное поле: подполя назвать нельзя, судить нечего.
				return true
			}
			cen.Judgeable++

			r := ct3ComputeRefusal{File: path, Line: fset.Position(call.Pos()).Line, Field: field, Desc: desc}
			if named := ct3ComputeNamedChildren(r, children); len(named) > 0 {
				findings = append(findings, ct3ComputeFinding{ct3ComputeRefusal: r, Children: named})
			}
			return true
		})
	}
	return findings, cen
}

// ct3ComputeLiteral — строковый литерал либо склейка литералов через `+`.
// Значение, собранное из переменных, гейт не читает и в перепись не берёт.
func ct3ComputeLiteral(e ast.Expr) (string, bool) {
	switch v := e.(type) {
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(v.Value)
		return s, err == nil
	case *ast.BinaryExpr:
		if v.Op != token.ADD {
			return "", false
		}
		l, ok1 := ct3ComputeLiteral(v.X)
		r, ok2 := ct3ComputeLiteral(v.Y)
		return l + r, ok1 && ok2
	}
	return "", false
}
