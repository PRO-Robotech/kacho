// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// quotasyncobserver.go — разбор подъёма тянущего величин квот: несёт ли ВЫЗОВ
// сток наблюдаемости.
//
// # Предмет
//
// Тянущий величин работает и с нулевым стоком — это законное состояние ядра, и
// оно объявлено у самого стока. Цена законности в том, что слепой тянущий
// СНАРУЖИ НЕОТЛИЧИМ от живого: на неизменной конфигурации живой правит ноль
// строк, поэтому нулевой счёт строк не говорит ничего, а «ни одного прохода за
// всю жизнь» некому сказать вовсе — рядов нет.
//
// Владелец, заводящий тянущего следующим, скопирует соседний композиционный
// корень. Скопироваться обязан и сток. Компилятор держит половину свойства
// (параметр обязателен, его нельзя не заметить), но `nil` компилируется — и
// именно этот исход выглядит рабочим и молчит. Вторую половину держит этот
// разбор.
//
// # Что именно ищется
//
// Вызов подъёма (`StartLimitSyncer`) и аргумент, стоящий на позиции параметра
// стока. Позиция НЕ ВЫПИСАНА: она выводится из самого объявления — из
// параметра, чей тип называется `Recorder`. Выписанный номер позиции был бы
// прокси-предикатом: он пережил бы перестановку параметров и стал бы судить
// чужой аргумент, оставаясь на вид исправным.
//
// Находка — аргумент, который на этом месте означает «стока нет»:
//
//   - литерал `nil`;
//   - аргумента нет вовсе (вызов старой формы; на дереве такой не собрался бы,
//     но разбор обязан его называть — инъекция подаёт именно его).
//
// # Пересыльщики
//
// Композиционный корень вправе поднимать тянущего не сам, а через свою обёртку
// (так делает vpc: обёртка объявляет порт соседа У ПОТРЕБИТЕЛЯ). Тогда на самом
// вызове стоит имя ПАРАМЕТРА обёртки, и требовать от него большего нечего —
// вопрос переезжает к тому, кто зовёт обёртку. Разбор следует за этим переездом:
// функция, отдающая подъёму свой собственный параметр, объявляется
// ПЕРЕСЫЛЬЩИКОМ, и её вызовы судятся тем же правилом.
//
// # Чего разбор НЕ держит, и это сказано, а не умолчано
//
//   - он не знает ТИПОВ и потому не судит, ЧЕЙ сток провязан: адаптер своего
//     сервиса и чужой одинаковы для него. Это держит компилятор — тип параметра;
//   - пересыльщики прослеживаются в пределах ОДНОГО каталога (пакета). Обёртка,
//     вынесенная в чужой пакет, разбору не видна — и такой случай в дереве
//     отсутствует by construction: подъём зовут композиционные корни, а они
//     пакета не делят;
//   - переменная, пришедшая из другого места того же тела, считается стоком.
//     Синтаксически большего не сказать; ей `nil` можно присвоить только явно, и
//     это уже видно в обзоре.
package repohygiene

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// quotaSyncStarter — имя функции подъёма тянущего величин.
const quotaSyncStarter = "StartLimitSyncer"

// quotaSinkTypeName — имя типа стока наблюдаемости. По нему выводится ПОЗИЦИЯ
// параметра: выписанный номер пережил бы перестановку и судил бы чужой аргумент.
const quotaSinkTypeName = "Recorder"

// quotaSinkFile — один разобранный файл: путь, каталог (единица, в пределах
// которой прослеживаются пересыльщики) и дерево разбора.
type quotaSinkFile struct {
	rel  string
	dir  string
	file *ast.File
}

// QuotaSinkCall — один разобранный вызов подъёма.
type QuotaSinkCall struct {
	// File — путь относительно корня дерева.
	File string
	// Line — строка вызова.
	Line int
	// Arg — текст аргумента, стоящего на позиции стока; пусто, если его нет.
	Arg string
	// Via — имя пересыльщика, если вызов сделан через обёртку; пусто у прямого.
	Via string
}

// QuotaSinkFinding — вызов, поднимающий тянущего без стока.
type QuotaSinkFinding struct {
	File string
	Line int
	Why  string
}

// QuotaSinkCensus — объём осмотренного плюс находки.
//
// Перепись отдаётся ВСЕГДА: «ноль находок» обязано быть отличимо от «ноль
// прочитанного», а ноль распознанных вызовов при непустом дереве — поломка
// разбора, а не чистота.
type QuotaSinkCensus struct {
	// FilesRead — сколько файлов прочитано.
	FilesRead int
	// FilesParsed — из них разобрано синтаксически.
	FilesParsed int
	// DeclFile / DeclLine — где найдено объявление подъёма.
	DeclFile string
	DeclLine int
	// SinkParam / SinkIndex — имя и позиция параметра стока в объявлении.
	SinkParam string
	SinkIndex int
	// Arity — сколько всего параметров у объявления (для переписи).
	Arity int
	// Calls — все распознанные вызовы подъёма, включая законные.
	Calls []QuotaSinkCall
	// Forwarders — обёртки, отдающие подъёму свой параметр.
	Forwarders []string
	// Findings — вызовы без стока.
	Findings []QuotaSinkFinding
}

// ScanQuotaSyncSink разбирает переданные файлы: находит объявление подъёма,
// выводит из него позицию стока и судит каждый вызов.
//
// Состав файлов приносит вызывающий — на дереве его берут у индекса git, в
// инъекции это синтетика во временном каталоге. Разбор один и тот же: гейт со
// своим парсером для проб и своим для дерева проверял бы сам себя.
func ScanQuotaSyncSink(root string, files []string) (QuotaSinkCensus, error) {
	var c QuotaSinkCensus
	fset := token.NewFileSet()

	var trees []quotaSinkFile
	parsed := map[string]bool{}
	byDir := map[string][]string{}

	// Разбор идёт ДВУМЯ фазами, и это не оптимизация, а исправление настоящей
	// дыры первой редакции. Дешёвый отсев по имени подъёма пропускает файл,
	// который зовёт не подъём, а ОБЁРТКУ над ним, — то есть ровно то место, где
	// решение о стоке и принимается. Первая редакция так и промахнулась мимо
	// композиционного корня vpc: она увидела обёртку и не увидела её вызывающего.
	parse := func(path, rel string) error {
		if parsed[rel] {
			return nil
		}
		body, err := os.ReadFile(path) //nolint:gosec // состав приносит вызывающий
		if err != nil {
			return fmt.Errorf("чтение %s: %w", path, err)
		}
		f, perr := parser.ParseFile(fset, rel, body, parser.SkipObjectResolution)
		if perr != nil {
			return fmt.Errorf("разбор %s: %w", rel, perr)
		}
		parsed[rel] = true
		trees = append(trees, quotaSinkFile{rel: rel, dir: quotaSinkDir(rel), file: f})
		return nil
	}

	// Фаза 1 — файлы, называющие подъём: объявление, прямые вызовы, обёртки.
	pathOf := map[string]string{}
	for _, path := range files {
		body, err := os.ReadFile(path) //nolint:gosec // состав приносит вызывающий
		if err != nil {
			return c, fmt.Errorf("чтение %s: %w", path, err)
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return c, fmt.Errorf("относительный путь %s: %w", path, rerr)
		}
		rel = filepath.ToSlash(rel)
		c.FilesRead++
		dir := quotaSinkDir(rel)
		pathOf[rel] = path
		byDir[dir] = append(byDir[dir], rel)
		// Отсев идёт по ИМЕНИ и потому ловит и упоминание в прозе — их отбросит
		// уже разбор, читающий синтаксис, а не текст.
		if !strings.Contains(string(body), quotaSyncStarter) {
			continue
		}
		if perr := parse(path, rel); perr != nil {
			return c, perr
		}
	}

	if err := c.findDeclaration(fset, trees); err != nil {
		return c, err
	}

	// Прямые вызовы подъёма; они могут стоять где угодно.
	c.judge(fset, trees, map[string]int{quotaSyncStarter: c.SinkIndex}, "")

	// Фаза 2 — пересыльщики: обёртка, отдавшая подъёму свой параметр, судится
	// сама, но уже в пределах СВОЕГО каталога, и ради этого его файлы читаются
	// целиком. Цикл сходится: на каждом обороте берутся только новые записи, а
	// их число конечно.
	judged := map[string]bool{}
	for round := 0; round < 8; round++ {
		fresh := false
		for _, fw := range append([]string(nil), c.Forwarders...) {
			if judged[fw] {
				continue
			}
			judged[fw] = true
			dir, name, idx, ok := splitForwarder(fw)
			if !ok {
				continue
			}
			fresh = true
			for _, rel := range byDir[dir] {
				if perr := parse(pathOf[rel], rel); perr != nil {
					return c, perr
				}
			}
			c.judge(fset, trees, map[string]int{name: idx}, dir)
		}
		if !fresh {
			break
		}
	}
	c.FilesParsed = len(parsed)
	return c, nil
}

// findDeclaration находит объявление подъёма и выводит позицию стока.
//
// Отсутствие объявления, отсутствие параметра стока и ДВА таких параметра —
// каждое роняет разбор: это отказ ПРЕДПОСЫЛКИ. Гейт, продолжающий работать на
// сломанной предпосылке, печатает «находок 0» и означает этим «я ничего не
// понял».
func (c *QuotaSinkCensus) findDeclaration(fset *token.FileSet, trees []quotaSinkFile) error {
	found := false
	for _, p := range trees {
		for _, decl := range p.file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Name == nil || fn.Name.Name != quotaSyncStarter {
				continue
			}
			if found {
				return fmt.Errorf("объявлений %s больше одного (%s и %s) — "+
					"разбор не знает, чью позицию стока применять",
					quotaSyncStarter, c.DeclFile, p.rel)
			}
			found = true
			c.DeclFile = p.rel
			c.DeclLine = fset.Position(fn.Pos()).Line
			names, sinkAt := flattenParams(fn.Type.Params)
			c.Arity = len(names)
			if len(sinkAt) == 0 {
				return fmt.Errorf("%s объявлен без параметра типа %s: подъём тянущего "+
					"величин НЕ ПРИНИМАЕТ стока наблюдаемости вовсе, и слепой тянущий "+
					"заводится не «незаметно», а единственно возможным способом",
					quotaSyncStarter, quotaSinkTypeName)
			}
			if len(sinkAt) > 1 {
				return fmt.Errorf("%s объявлен с %d параметрами типа %s — разбор не знает, "+
					"какой из них сток", quotaSyncStarter, len(sinkAt), quotaSinkTypeName)
			}
			c.SinkIndex = sinkAt[0]
			c.SinkParam = names[sinkAt[0]]
		}
	}
	if !found {
		return errors.New("объявления " + quotaSyncStarter + " в осмотренном дереве нет: " +
			"предмет гейта отпал — снимите его вместе с подъёмом тянущего либо почините имя, " +
			"которым он его ищет")
	}
	return nil
}

// judge судит вызовы перечисленных целей и копит находки с пересыльщиками.
func (c *QuotaSinkCensus) judge(fset *token.FileSet, trees []quotaSinkFile, targets map[string]int, dir string) {
	for _, p := range trees {
		if dir != "" && p.dir != dir {
			continue
		}
		for _, decl := range p.file.Decls {
			fn, _ := decl.(*ast.FuncDecl)
			ast.Inspect(decl, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				name, ok := quotaSinkCalleeName(call.Fun)
				if !ok {
					return true
				}
				idx, watched := targets[name]
				if !watched {
					return true
				}
				line := fset.Position(call.Lparen).Line
				via := ""
				if name != quotaSyncStarter {
					via = name
				}
				if idx >= len(call.Args) {
					c.Calls = append(c.Calls, QuotaSinkCall{File: p.rel, Line: line, Via: via})
					c.Findings = append(c.Findings, QuotaSinkFinding{
						File: p.rel, Line: line,
						Why: "подъём тянущего величин зовут БЕЗ аргумента на позиции стока " +
							"(" + fmt.Sprint(len(call.Args)) + " аргументов, сток ожидается " +
							fmt.Sprint(idx+1) + "-м): такой тянущий работает и снаружи " +
							"неотличим от мёртвого",
					})
					return true
				}
				arg := call.Args[idx]
				text := quotaSinkExprText(fset, arg)
				c.Calls = append(c.Calls, QuotaSinkCall{File: p.rel, Line: line, Arg: text, Via: via})

				if id, isIdent := arg.(*ast.Ident); isIdent {
					switch {
					case id.Name == "nil":
						c.Findings = append(c.Findings, QuotaSinkFinding{
							File: p.rel, Line: line,
							Why: "сток передан как nil: тянущий поднимется и будет работать, " +
								"но «ни одного прохода за всю жизнь» сказать станет нечем — " +
								"рядов нет вовсе, а нулевой счёт строк на неизменной " +
								"конфигурации есть ПРАВИЛЬНЫЙ исход и молчит",
						})
					case fn != nil && paramIndex(fn, id.Name) >= 0:
						// Пересыльщик: вопрос переезжает к тому, кто зовёт обёртку.
						c.Forwarders = appendUnique(c.Forwarders,
							p.dir+"#"+fn.Name.Name+"#"+fmt.Sprint(paramIndex(fn, id.Name)))
					}
				}
				return true
			})
		}
	}
}

// quotaSinkCalleeName отдаёт имя вызываемого: `f(…)` и `pkg.f(…)` дают одно имя.
//
// Разбор синтаксиса, а не поиск подстроки: имя подъёма стоит и в прозе этого
// файла, и в комментариях композиционных корней, и гейт по подстроке краснел бы
// на собственном объяснении.
func quotaSinkCalleeName(fun ast.Expr) (string, bool) {
	switch f := fun.(type) {
	case *ast.Ident:
		return f.Name, true
	case *ast.SelectorExpr:
		if f.Sel == nil {
			return "", false
		}
		return f.Sel.Name, true
	}
	return "", false
}

// flattenParams разворачивает список параметров в плоский перечень имён и
// отдаёт позиции тех, чей тип называется именем стока.
func flattenParams(fl *ast.FieldList) (names []string, sinkAt []int) {
	if fl == nil {
		return nil, nil
	}
	for _, field := range fl.List {
		isSink := typeNamed(field.Type, quotaSinkTypeName)
		if len(field.Names) == 0 {
			if isSink {
				sinkAt = append(sinkAt, len(names))
			}
			names = append(names, "_")
			continue
		}
		for _, n := range field.Names {
			if isSink {
				sinkAt = append(sinkAt, len(names))
			}
			names = append(names, n.Name)
		}
	}
	return names, sinkAt
}

// typeNamed — тип называется именем name (`Recorder` либо `quota.Recorder`).
func typeNamed(t ast.Expr, name string) bool {
	switch v := t.(type) {
	case *ast.Ident:
		return v.Name == name
	case *ast.SelectorExpr:
		return v.Sel != nil && v.Sel.Name == name
	}
	return false
}

// paramIndex — позиция параметра с именем name; -1, если такого нет.
func paramIndex(fn *ast.FuncDecl, name string) int {
	if fn == nil || fn.Type == nil {
		return -1
	}
	names, _ := flattenParams(fn.Type.Params)
	for i, n := range names {
		if n == name {
			return i
		}
	}
	return -1
}

// splitForwarder разбирает запись пересыльщика «каталог#имя#позиция».
func splitForwarder(s string) (dir, name string, idx int, ok bool) {
	parts := strings.Split(s, "#")
	if len(parts) != 3 {
		return "", "", 0, false
	}
	n, err := strconv.Atoi(parts[2])
	if err != nil {
		return "", "", 0, false
	}
	return parts[0], parts[1], n, true
}

func appendUnique(dst []string, v string) []string {
	for _, x := range dst {
		if x == v {
			return dst
		}
	}
	return append(dst, v)
}

// exprText печатает выражение так, как оно записано, — для координаты находки.
func quotaSinkExprText(fset *token.FileSet, e ast.Expr) string {
	var buf bytes.Buffer
	if err := printer.Fprint(&buf, fset, e); err != nil {
		return "<не удалось напечатать>"
	}
	return buf.String()
}

// quotaSinkDir — каталог файла; единица, в пределах которой прослеживаются
// пересыльщики.
func quotaSinkDir(rel string) string {
	slash := filepath.ToSlash(rel)
	if i := strings.LastIndex(slash, "/"); i >= 0 {
		return slash[:i]
	}
	return "."
}
