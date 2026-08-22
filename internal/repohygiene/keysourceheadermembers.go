// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// keysourceheadermembers.go — разбор упоминаний членов заголовка, из которых
// ключ проверки выбираться НЕ МОЖЕТ (приёмка F2, сценарий F2-07, §5).
//
// # Предмет
//
// Доверять ключу, приехавшему ВМЕСТЕ С ПОДПИСЬЮ, значит принимать любую
// подпись: предъявитель приложит тот ключ, которым подписал. Членов, которыми
// ключ можно привезти, несколько, и перечень их обязан быть объявлен ОДИН раз.
// Место, закрывшее один член и забывшее второй, выглядит закрытым — разница
// видна только тому, кто держит перечень в голове.
//
// # Что здесь считается ОБЪЯВЛЕНИЕМ ПЕРЕЧНЯ, а что УПОТРЕБЛЕНИЕМ
//
//	[]string{"jwk", "jku", "x5u", "x5c"}   ← объявление перечня: он выписан
//	hdr.JWK json:"jwk"                     ← употребление ОДНОГО члена
//
// Разграничение по ЧИСЛУ РАЗНЫХ членов, названных В ОДНОМ ВМЕСТИЛИЩЕ, а не по
// форме записи. Причина прямая: назвать один член — значит принять решение об
// этом члене; выписать несколько рядом — значит завести вторую копию перечня,
// которая разойдётся с первой молча.
//
// # Почему вместилище, а НЕ верхнеуровневое объявление
//
// Вместилище — ближайший составной литерал, объявление структуры либо блок
// `const`/`var`; литерал, не лежащий ни в одном из них, считается сам по себе.
// Единицей «одно верхнеуровневое объявление» разбор быть не может, и это
// измерено, а не предположено: функция, читающая `jwk` из заголовка и
// `x5t#S256` из подтверждения владения, называет два члена в РАЗНЫХ ролях —
// перечнем это не является, а по единице «функция» стало бы находкой. Перечень
// для того и перечень, чтобы лежать ВМЕСТЕ.
//
// # Почему одночленное место МОЛЧИТ, а не запрещено
//
// В соседнем механизме дерева то же доверие ЗАКОННО и является его сутью:
// доказательство владения ключом (RFC 9449) устроено наоборот — ключ приходит в
// самом доказательстве, и проверяющий связывает его с токеном по отпечатку. Там
// встроенный ключ не уязвимость, а ПРЕДМЕТ. Гейт, запрещающий всякое упоминание,
// запретил бы этот механизм; гейт, считающий любое упоминание объявлением,
// нашёл бы его находкой. Поэтому судится ЧИСЛО членов в одном месте.
//
// # Чего разбор НЕ видит — названо, а не спрятано
//
//  1. **имя члена, собранное из частей** (`"x5" + "c"`, значение переменной).
//     Разбор читает литералы, а не вычисляет выражения.
//  2. **перечень, разнесённый по РАЗНЫМ вместилищам** одного файла: каждое из
//     них назовёт по одному члену и останется употреблением. Форма
//     противоестественная (перечень для того и перечень, чтобы лежать вместе),
//     и её появление видно по счётчику одночленных мест.
//  3. **перечень в чужом языке** — конфигурация, шаблон, схема. Разбор ходит по
//     Go-исходникам.
package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// KeySourceMention — упоминание члена заголовка в дереве.
type KeySourceMention struct {
	File string
	Line int
	// Member — имя члена заголовка.
	Member string
	// Form — чем член назван: строковым литералом или тегом поля структуры.
	// Печатается в отказе: по одной строке кода форма не восстанавливается.
	Form string
}

// KeySourceSite — ВМЕСТИЛИЩЕ, называющее члены заголовка: составной литерал,
// объявление структуры, блок `const`/`var` либо одиночный литерал.
type KeySourceSite struct {
	File string
	Line int
	// Decl — верхнеуровневое объявление, внутри которого лежит вместилище:
	// имя функции, типа либо переменной. По номеру строки читатель отказа не
	// поймёт, что именно выписало перечень.
	Decl string
	// Members — РАЗНЫЕ члены, названные здесь, по возрастанию.
	Members []string
	// Mentions — сами упоминания с координатами.
	Mentions []KeySourceMention
}

// KeySourceCensus — объём осмотренного одним файлом.
type KeySourceCensus struct {
	// Decls — верхнеуровневых объявлений прочитано.
	Decls int
	// Containers — вместилищ прочитано.
	Containers int
	// StringLiterals — строковых литералов осмотрено.
	StringLiterals int
	// TagNames — имён из тегов полей структур осмотрено.
	TagNames int
	// Mentions — из них признано упоминаниями членов заголовка.
	Mentions int
}

// ScanKeySourceHeaderMembers разбирает один файл и раскладывает упоминания
// членов заголовка по ВМЕСТИЛИЩАМ.
//
// members — перечень имён; гейт берёт его из ЕДИНСТВЕННОГО объявления, а не
// выписывает своей рукой: вторая копия перечня внутри проверки перечня была бы
// тем самым дефектом, который проверка ищет.
func ScanKeySourceHeaderMembers(path string, src []byte, members []string) (
	[]KeySourceSite, KeySourceCensus, error,
) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		return nil, KeySourceCensus{}, err
	}
	want := map[string]bool{}
	for _, m := range members {
		want[m] = true
	}

	var census KeySourceCensus
	type bucket struct {
		line     int
		decl     string
		members  map[string]bool
		mentions []KeySourceMention
	}
	buckets := map[token.Pos]*bucket{}
	var order []token.Pos

	// declName — верхнеуровневое объявление, внутри которого идёт обход.
	declName := ""
	// add кладёт упоминание во вместилище. container == token.NoPos означает,
	// что литерал не лежит ни в одном вместилище: тогда он сам себе место, и
	// два таких литерала рядом остаются ДВУМЯ местами по одному члену.
	add := func(container, own token.Pos, m KeySourceMention) {
		key := container
		if key == token.NoPos {
			key = own
		}
		b, ok := buckets[key]
		if !ok {
			b = &bucket{line: fset.Position(key).Line, decl: declName, members: map[string]bool{}}
			buckets[key] = b
			order = append(order, key)
		}
		b.members[m.Member] = true
		b.mentions = append(b.mentions, m)
	}

	// walk обходит дерево, неся ПОЗИЦИЮ ближайшего вместилища. Рекурсия здесь
	// вместо ast.Inspect намеренно: обработчик Inspect не знает, на каком узле
	// он находится относительно вместилища, и глубину пришлось бы вести стеком
	// с парными снятиями — форма, в которой выход из обычного узла снимает
	// чужое вместилище, и литералы, лежащие рядом, оказываются в одном ведре.
	var walk func(n ast.Node, container token.Pos)
	walk = func(n ast.Node, container token.Pos) {
		if n == nil {
			return
		}
		switch node := n.(type) {
		case *ast.CompositeLit:
			census.Containers++
			container = node.Pos()
		case *ast.StructType:
			census.Containers++
			container = node.Pos()
		case *ast.GenDecl:
			census.Containers++
			container = node.Pos()
		case *ast.BasicLit:
			if node.Kind == token.STRING {
				census.StringLiterals++
				if val, err := strconv.Unquote(node.Value); err == nil && want[val] {
					census.Mentions++
					add(container, node.Pos(), KeySourceMention{
						File: path, Line: fset.Position(node.Pos()).Line,
						Member: val, Form: "строковый литерал",
					})
				}
			}
		case *ast.Field:
			if node.Tag != nil {
				for _, name := range structTagNames(node.Tag.Value) {
					census.TagNames++
					if !want[name] {
						continue
					}
					census.Mentions++
					add(container, node.Tag.Pos(), KeySourceMention{
						File: path, Line: fset.Position(node.Tag.Pos()).Line,
						Member: name, Form: "тег поля структуры",
					})
				}
			}
		}
		for _, child := range astChildren(n) {
			walk(child, container)
		}
	}

	for _, decl := range f.Decls {
		census.Decls++
		declName = topLevelDeclName(decl)
		walk(decl, token.NoPos)
	}

	out := make([]KeySourceSite, 0, len(order))
	for _, key := range order {
		b := buckets[key]
		site := KeySourceSite{File: path, Line: b.line, Decl: b.decl, Mentions: b.mentions}
		for m := range b.members {
			site.Members = append(site.Members, m)
		}
		sort.Strings(site.Members)
		sort.Slice(site.Mentions, func(i, j int) bool { return site.Mentions[i].Line < site.Mentions[j].Line })
		out = append(out, site)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Line < out[j].Line })
	return out, census, nil
}

// astChildren — непосредственные потомки узла.
//
// ast.Inspect обходит дерево сам, но не сообщает обработчику, где он находится
// относительно вместилища; здесь нужен обход, несущий эту величину вниз, —
// значит потомков надо получить явно.
func astChildren(n ast.Node) []ast.Node {
	var out []ast.Node
	first := true
	ast.Inspect(n, func(c ast.Node) bool {
		if first {
			first = false
			return true
		}
		if c == nil {
			return false
		}
		out = append(out, c)
		return false
	})
	return out
}

// structTagNames — имена, которыми поле названо в тегах.
//
// Читаются ВСЕ известные ключи сериализации, а не один: член заголовка,
// названный в теге чужого формата, остаётся тем же членом.
func structTagNames(quoted string) []string {
	raw, err := strconv.Unquote(quoted)
	if err != nil {
		return nil
	}
	tag := reflect.StructTag(raw)
	var out []string
	for _, key := range []string{"json", "yaml", "cbor", "mapstructure"} {
		v := tag.Get(key)
		if v == "" {
			continue
		}
		if i := strings.Index(v, ","); i >= 0 {
			v = v[:i]
		}
		if v != "" && v != "-" {
			out = append(out, v)
		}
	}
	return out
}

// topLevelDeclName — чем названо верхнеуровневое объявление: по номеру строки
// читатель отказа не поймёт, что именно выписало перечень.
func topLevelDeclName(decl ast.Decl) string {
	switch d := decl.(type) {
	case *ast.FuncDecl:
		if d.Recv != nil && len(d.Recv.List) > 0 {
			if recv := renderTypeExpr(d.Recv.List[0].Type); recv != "" {
				return "метод " + recv + "." + d.Name.Name
			}
		}
		return "функция " + d.Name.Name
	case *ast.GenDecl:
		var names []string
		for _, spec := range d.Specs {
			switch s := spec.(type) {
			case *ast.TypeSpec:
				names = append(names, s.Name.Name)
			case *ast.ValueSpec:
				for _, nm := range s.Names {
					names = append(names, nm.Name)
				}
			}
		}
		if len(names) > 0 {
			return d.Tok.String() + " " + strings.Join(names, ", ")
		}
		return d.Tok.String()
	default:
		return "объявление"
	}
}
