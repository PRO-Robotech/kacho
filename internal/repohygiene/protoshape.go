// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// protoshape.go — узкий разбор контракта: типы, поля, ветви выбора, ведущие
// комментарии.
//
// # Почему разбор, а не поиск по образцу
//
// Гейты формы утверждают о СТРУКТУРЕ («поле стоит на верхнем уровне оболочки, а
// не внутри ветви выбора»), а поиск по образцу структуры не видит: он одинаково
// находит имя поля в объявлении, в комментарии и в чужом сообщении. Разбор
// различает их by construction.
//
// # Почему не дескрипторы
//
// Дескрипторы сгенерённого пакета структуру дают точнее любого разбора, но
// КОММЕНТАРИЕВ не несут: `protoc-gen-go` снимает `SourceCodeInfo` с вшитого
// описания (проверено, а не предположено). Часть требований фазы — «причина
// стоит в контракте», «исход незаданного назван», «у оси проставлен признак
// категории» — есть требования именно к тексту рядом с объявлением. Значит
// источник обязан быть один и тот же для обеих половин, иначе они разойдутся.
//
// # Границы
//
// Разбор рассчитан на контракты ЭТОГО дерева: proto3, один тип на объявление,
// без расширений и групп. Он не притворяется компилятором и на незнакомой
// конструкции ничего не выдумывает — она просто не попадает в перепись, а
// перепись печатается, поэтому «прочитано ноль» отличимо от «находок ноль».
package repohygiene

import (
	"regexp"
	"strconv"
	"strings"
)

// ProtoField — поле сообщения.
type ProtoField struct {
	Name   string
	Type   string
	Label  string // "repeated" | "optional" | ""
	Number int
	// Oneof — имя ветвления, если поле является его ветвью; иначе пусто.
	Oneof string
	// Comment — ведущий комментарий, склеенный без маркеров `//`.
	Comment string
	// Options — текст блока опций поля (`[...]`), как он записан, включая
	// перенесённый на несколько строк. Пусто, если опций нет.
	Options string
	Line    int
}

// ProtoEnumValue — значение перечисления.
type ProtoEnumValue struct {
	Name    string
	Number  int
	Comment string
	Line    int
}

// ProtoType — сообщение либо перечисление.
type ProtoType struct {
	Name string
	Kind string // "message" | "enum"
	// Path — цепочка вложенности от верхнего уровня, включая само имя.
	Path    []string
	Comment string
	Line    int
	Fields  []ProtoField
	Values  []ProtoEnumValue
	// Oneofs — имена ветвлений в порядке объявления, с их ведущими комментариями.
	Oneofs []ProtoOneof
}

// ProtoOneof — ветвление внутри сообщения.
type ProtoOneof struct {
	Name    string
	Comment string
	Line    int
}

// TopLevel — объявлен ли тип на верхнем уровне файла.
func (t ProtoType) TopLevel() bool { return len(t.Path) == 1 }

// Field — поле по имени.
func (t ProtoType) Field(name string) (ProtoField, bool) {
	for _, f := range t.Fields {
		if f.Name == name {
			return f, true
		}
	}
	return ProtoField{}, false
}

// OneofBranches — ветви данного ветвления.
func (t ProtoType) OneofBranches(oneof string) []ProtoField {
	var out []ProtoField
	for _, f := range t.Fields {
		if f.Oneof == oneof {
			out = append(out, f)
		}
	}
	return out
}

// Oneof — ветвление по имени.
func (t ProtoType) Oneof(name string) (ProtoOneof, bool) {
	for _, o := range t.Oneofs {
		if o.Name == name {
			return o, true
		}
	}
	return ProtoOneof{}, false
}

// ProtoFile — разобранный контракт.
type ProtoFile struct {
	Rel      string
	Package  string
	Imports  []string
	Types    []ProtoType
	Services []string
	RPCs     []string
	Lines    int
}

// Type — тип верхнего уровня по имени.
func (f *ProtoFile) Type(name string) (ProtoType, bool) {
	for _, t := range f.Types {
		if t.TopLevel() && t.Name == name {
			return t, true
		}
	}
	return ProtoType{}, false
}

var (
	psMessageOpenRe = regexp.MustCompile(`^\s*message\s+([A-Za-z0-9_]+)\s*\{`)
	psEnumOpenRe    = regexp.MustCompile(`^\s*enum\s+([A-Za-z0-9_]+)\s*\{`)
	psOneofOpenRe   = regexp.MustCompile(`^\s*oneof\s+([A-Za-z0-9_]+)\s*\{`)
	psServiceOpenRe = regexp.MustCompile(`^\s*service\s+([A-Za-z0-9_]+)\s*\{`)
	psRPCRe         = regexp.MustCompile(`^\s*rpc\s+([A-Za-z0-9_]+)\s*\(`)
	psImportRe      = regexp.MustCompile(`^\s*import\s+(?:public\s+|weak\s+)?"([^"]+)"\s*;`)
	psPackageRe     = regexp.MustCompile(`^\s*package\s+([A-Za-z0-9_.]+)\s*;`)
	psFieldRe       = regexp.MustCompile(
		`^\s*(?:(repeated|optional|required)\s+)?([A-Za-z_][A-Za-z0-9_.]*)\s+([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(\d+)\s*[;\[]`)
	psMapFieldRe = regexp.MustCompile(
		`^\s*(map\s*<[^>]*>)\s+([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(\d+)\s*[;\[]`)
	psEnumValueRe = regexp.MustCompile(`^\s*([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(-?\d+)\s*[;\[]`)
	psBlockOpenRe = regexp.MustCompile(`/\*`)
)

// ParseProtoFile разбирает один контракт.
func ParseProtoFile(rel, body string) *ProtoFile {
	f := &ProtoFile{Rel: rel}
	lines := strings.Split(body, "\n")
	f.Lines = len(lines)

	type frame struct {
		kind  string // "message" | "enum" | "service" | "oneof" | "other"
		idx   int    // индекс типа в f.Types, если kind — message/enum
		oneof string
	}
	var stack []frame
	var pending []string
	inBlock := false

	// idxOf — индекс текущего охватывающего типа (message/enum), либо -1.
	idxOf := func() int {
		for i := len(stack) - 1; i >= 0; i-- {
			if stack[i].kind == "message" || stack[i].kind == "enum" {
				return stack[i].idx
			}
		}
		return -1
	}
	oneofOf := func() string {
		if n := len(stack); n > 0 && stack[n-1].kind == "oneof" {
			return stack[n-1].oneof
		}
		return ""
	}
	pathOf := func(name string) []string {
		var p []string
		for _, fr := range stack {
			if fr.kind == "message" || fr.kind == "enum" {
				p = append(p, f.Types[fr.idx].Name)
			}
		}
		return append(p, name)
	}
	take := func() string {
		c := strings.TrimSpace(strings.Join(pending, "\n"))
		pending = nil
		return c
	}

	// takeOptions дочитывает блок опций поля, начиная со строки i. Возвращает
	// текст блока и индекс последней прочитанной строки: опции переносятся на
	// несколько строк, и поле, помеченное на второй из них, иначе осталось бы
	// невидимым — а именно так помечают поле обязательным в этом дереве.
	takeOptions := func(i int) (string, int) {
		open := strings.Index(lines[i], "[")
		if open < 0 {
			return "", i
		}
		var b strings.Builder
		for j := i; j < len(lines); j++ {
			seg := lines[j]
			if j == i {
				seg = seg[open:]
			}
			b.WriteString(seg)
			b.WriteString("\n")
			if strings.Contains(seg, "]") {
				return b.String(), j
			}
		}
		return b.String(), len(lines) - 1
	}

	for i := 0; i < len(lines); i++ {
		raw := lines[i]
		line := raw
		lineNo := i + 1

		// Блочные комментарии: содержимое не разбирается, но и не сдвигает
		// нумерацию строк — координата находки обязана указывать в файл.
		if inBlock {
			if j := strings.Index(line, "*/"); j >= 0 {
				inBlock = false
				line = line[j+2:]
			} else {
				continue
			}
		}
		for {
			loc := psBlockOpenRe.FindStringIndex(line)
			if loc == nil {
				break
			}
			rest := line[loc[1]:]
			if j := strings.Index(rest, "*/"); j >= 0 {
				line = line[:loc[0]] + rest[j+2:]
				continue
			}
			line = line[:loc[0]]
			inBlock = true
			break
		}

		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			// Пустая строка отрывает комментарий от объявления — так же, как
			// это делает protoc, разбирая ведущий комментарий.
			pending = nil
			continue
		}
		if strings.HasPrefix(trimmed, "//") {
			pending = append(pending, strings.TrimSpace(strings.TrimPrefix(trimmed, "//")))
			continue
		}

		switch {
		case psMessageOpenRe.MatchString(line):
			name := psMessageOpenRe.FindStringSubmatch(line)[1]
			f.Types = append(f.Types, ProtoType{
				Name: name, Kind: "message", Path: pathOf(name), Comment: take(), Line: lineNo,
			})
			stack = append(stack, frame{kind: "message", idx: len(f.Types) - 1})
			continue
		case psEnumOpenRe.MatchString(line):
			name := psEnumOpenRe.FindStringSubmatch(line)[1]
			f.Types = append(f.Types, ProtoType{
				Name: name, Kind: "enum", Path: pathOf(name), Comment: take(), Line: lineNo,
			})
			stack = append(stack, frame{kind: "enum", idx: len(f.Types) - 1})
			continue
		case psOneofOpenRe.MatchString(line):
			name := psOneofOpenRe.FindStringSubmatch(line)[1]
			if k := idxOf(); k >= 0 {
				f.Types[k].Oneofs = append(f.Types[k].Oneofs,
					ProtoOneof{Name: name, Comment: take(), Line: lineNo})
			}
			stack = append(stack, frame{kind: "oneof", oneof: name})
			continue
		case psServiceOpenRe.MatchString(line):
			f.Services = append(f.Services, psServiceOpenRe.FindStringSubmatch(line)[1])
			stack = append(stack, frame{kind: "service"})
			pending = nil
			continue
		case psRPCRe.MatchString(line):
			f.RPCs = append(f.RPCs, psRPCRe.FindStringSubmatch(line)[1])
			pending = nil
		case psImportRe.MatchString(line):
			f.Imports = append(f.Imports, psImportRe.FindStringSubmatch(line)[1])
			pending = nil
			continue
		case psPackageRe.MatchString(line) && f.Package == "":
			f.Package = psPackageRe.FindStringSubmatch(line)[1]
			pending = nil
			continue
		}

		k := idxOf()
		if k >= 0 && f.Types[k].Kind == "message" {
			if m := psMapFieldRe.FindStringSubmatch(line); m != nil {
				n, _ := strconv.Atoi(m[3])
				opts, last := takeOptions(i)
				f.Types[k].Fields = append(f.Types[k].Fields, ProtoField{
					Name: m[2], Type: strings.Join(strings.Fields(m[1]), ""), Number: n,
					Oneof: oneofOf(), Comment: take(), Options: opts, Line: lineNo,
				})
				i = last
				continue
			}
			if m := psFieldRe.FindStringSubmatch(line); m != nil {
				n, _ := strconv.Atoi(m[4])
				opts, last := takeOptions(i)
				f.Types[k].Fields = append(f.Types[k].Fields, ProtoField{
					Name: m[3], Type: m[2], Label: m[1], Number: n,
					Oneof: oneofOf(), Comment: take(), Options: opts, Line: lineNo,
				})
				i = last
				continue
			}
		}
		if k >= 0 && f.Types[k].Kind == "enum" {
			if m := psEnumValueRe.FindStringSubmatch(line); m != nil {
				n, _ := strconv.Atoi(m[2])
				f.Types[k].Values = append(f.Types[k].Values, ProtoEnumValue{
					Name: m[1], Number: n, Comment: take(), Line: lineNo,
				})
				continue
			}
		}

		// Закрывающие скобки: снимаем ровно столько рамок, сколько закрыто.
		if c := strings.Count(line, "}"); c > 0 {
			for ; c > 0 && len(stack) > 0; c-- {
				stack = stack[:len(stack)-1]
			}
			pending = nil
			continue
		}
		pending = nil
	}
	return f
}
