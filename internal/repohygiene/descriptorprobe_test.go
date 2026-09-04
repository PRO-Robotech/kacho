// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// descriptorprobe_test.go — гейт против КОМПОЗИЦИОННОГО КОРНЯ, чей дескриптор
// никем не собирается в прогоне.
//
// # Предмет
//
// Дескриптор процесса (`servicecontract.New`) судится конструктором на СТАРТЕ.
// Отказ рантаймовый, поэтому сборка о нём не знает; корень сервиса лежит вне
// прогона носителя, поэтому и тот о нём не знает. Если ни один тест не зовёт
// функцию, которая дескриптор собирает, то поле, ставшее обязательным, ломает
// подъём сервиса, и сказать об этом некому — до самого развёртывания.
//
// Класс не гипотетический: он дал БЛОКЕР ровно в той правке, которая этот гейт
// завела. Три поля стали обязательными в носителе, единственный композиционный
// корень на носителе их не объявил, все предписанные прогоны были зелёными.
//
// # Что именно требуется
//
// У каждой функции, которая зовёт `servicecontract.New` в не-тестовом файле,
// обязан быть вызывающий среди `_test.go` ТОГО ЖЕ каталога. Не «тесты в пакете
// есть» (у geo их было три, и все зелёные), а «эту функцию кто-то зовёт».
//
// # Граница, названная честно
//
// Вызывающий распознаётся по ИМЕНИ функции, а не по разрешению типов: `describe(…)`
// в пробе считается вызовом `describe`. Ошибка предиката ОДНОСТОРОННЯЯ — он может
// засчитать одноимённую чужую функцию, то есть промолчать там, где надо
// покраснеть. Взамен он не краснеет на исправном дереве, а перепись печатает
// координату каждого засчитанного вызова, чтобы «вызывающий есть» проверялось
// глазом, а не принималось на слово.
//
// Разбор AST, а не текст: имя функции стоит в комментариях чаще, чем в коде
// (шапка этого файла — сама тому пример), и текстовый предикат зеленел бы на
// собственной документации.
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

const (
	// descriptorPkgIdent / descriptorCtorName — конструктор, чей вызов делает
	// функцию композиционным корнем.
	descriptorPkgIdent = "servicecontract"
	descriptorCtorName = "New"
)

// descriptorRoot — одна функция, собирающая дескриптор.
type descriptorRoot struct {
	fn    string // имя функции
	dir   string // каталог пакета (rel)
	where string // файл:строка вызова конструктора
}

// descriptorProbeResult — исход обхода вместе с ПЕРЕПИСЬЮ осмотренного.
type descriptorProbeResult struct {
	roots   []descriptorRoot
	callers map[string][]string // "dir\x00fn" -> координаты вызовов в пробах
	files   int
	tests   int
}

func (r descriptorProbeResult) summary() string {
	// «тестовых файлов» здесь — только те, что лежат в каталогах найденных корней:
	// остальные обходу не нужны и не читаются. Уточнение не косметическое —
	// иначе число читалось бы как перепись всех проб дерева.
	return fmt.Sprintf("осмотрено: не-тестовых файлов %d, тестовых файлов в каталогах корней %d, "+
		"композиционных корней (вызовов %s.%s) %d",
		r.files, r.tests, descriptorPkgIdent, descriptorCtorName, len(r.roots))
}

// TestEveryDescriptorHasAProbe — сам гейт.
//
// Что делать, если сработал, — два исхода, третьего нет:
//
//  1. написать пробу, зовущую эту функцию на конфигурации, с какой процесс
//     поднимается, и требующую от конструктора приёма (эталон —
//     `services/geo/cmd/kacho-geo/describe_test.go`);
//  2. если функция больше не собирает дескриптор — снять её вместе с вызовом.
//
// Молчаливого третьего («потом напишем») нет намеренно: именно он и стоил
// блокера, из-за которого гейт написан.
func TestEveryDescriptorHasAProbe(t *testing.T) {
	res := auditDescriptorProbes(t, repoRoot(t))
	t.Log(res.summary())

	if res.files == 0 {
		t.Fatalf("не прочитано ни одного не-тестового файла — «ноль находок» здесь означало бы "+
			"«ноль прочитанного».\n%s", res.summary())
	}
	if len(res.roots) == 0 {
		t.Fatalf("не найдено НИ ОДНОГО композиционного корня, собирающего дескриптор. Ноль целей "+
			"есть ОТКАЗ, а не успех: гейт без предмета неотличим от гейта, у которого всё в порядке. "+
			"Либо распознавание корня разошлось с деревом (переименовали пакет или конструктор), "+
			"либо на носителе не осталось ни одного сервиса.\n%s", res.summary())
	}

	var lines []string
	for _, root := range res.roots {
		if len(res.callers[root.key()]) > 0 {
			continue
		}
		lines = append(lines, fmt.Sprintf("%s (%s): дескриптор собирается здесь и НЕ собирается "+
			"ни одной пробой каталога %s. Отказ конструктора рантаймовый — сборка его не видит, "+
			"прогон носителя тоже (корень сервиса вне его области), поэтому обязательное поле, "+
			"которого корень не объявил, ломает подъём сервиса молча",
			root.fn, root.where, root.dir))
	}
	sort.Strings(lines)
	if len(lines) > 0 {
		t.Errorf("композиционные корни без пробы (%d):\n  %s", len(lines), strings.Join(lines, "\n  "))
	}

	// Перепись поимённо: «вызывающий есть» обязано проверяться глазом.
	for _, root := range res.roots {
		if c := res.callers[root.key()]; len(c) > 0 {
			t.Logf("%s (%s) собирается пробами: %s", root.fn, root.dir, strings.Join(c, ", "))
		}
	}
}

func (r descriptorRoot) key() string { return r.dir + "\x00" + r.fn }

// auditDescriptorProbes обходит дерево: сначала корни, потом их вызывающие.
func auditDescriptorProbes(t *testing.T, root string) descriptorProbeResult {
	t.Helper()
	files, err := treecorpus.Under(root)
	if err != nil {
		t.Fatalf("состав дерева: %v", err)
	}
	res := descriptorProbeResult{callers: map[string][]string{}}
	fset := token.NewFileSet()

	wanted := map[string]map[string]struct{}{} // dir -> имена функций-корней

	for _, path := range files {
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			continue
		}
		rel, _ := filepath.Rel(root, path)
		if strings.HasPrefix(rel, "pkg/api/") {
			continue // сгенерённые стабы: своего кода там нет
		}
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			continue
		}
		res.files++
		dir := filepath.Dir(rel)
		ast.Inspect(f, func(n ast.Node) bool {
			fn, ok := n.(*ast.FuncDecl)
			if !ok {
				return true
			}
			var at token.Pos
			ast.Inspect(fn.Body, func(inner ast.Node) bool {
				call, ok := inner.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != descriptorCtorName {
					return true
				}
				if ident, ok := sel.X.(*ast.Ident); !ok || ident.Name != descriptorPkgIdent {
					return true
				}
				at = call.Lparen
				return false
			})
			if at == 0 {
				return true
			}
			pos := fset.Position(at)
			res.roots = append(res.roots, descriptorRoot{
				fn:    fn.Name.Name,
				dir:   dir,
				where: fmt.Sprintf("%s:%d", rel, pos.Line),
			})
			if wanted[dir] == nil {
				wanted[dir] = map[string]struct{}{}
			}
			wanted[dir][fn.Name.Name] = struct{}{}
			return true
		})
	}

	for _, path := range files {
		if !strings.HasSuffix(path, "_test.go") {
			continue
		}
		rel, _ := filepath.Rel(root, path)
		dir := filepath.Dir(rel)
		names, ok := wanted[dir]
		if !ok {
			continue
		}
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			continue
		}
		res.tests++
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			ident, ok := call.Fun.(*ast.Ident)
			if !ok {
				return true
			}
			if _, want := names[ident.Name]; !want {
				return true
			}
			pos := fset.Position(call.Lparen)
			key := dir + "\x00" + ident.Name
			res.callers[key] = append(res.callers[key], fmt.Sprintf("%s:%d", rel, pos.Line))
			return true
		})
	}
	sort.Slice(res.roots, func(i, j int) bool { return res.roots[i].where < res.roots[j].where })
	return res
}
