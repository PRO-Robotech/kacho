// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subscriptionstream_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// projection_singularity_test.go — гейт «проекция потока на крае ОДНА».
//
// # Предмет
//
// Признак нарушения назван задачей дословно: на крае появилась вторая точка
// проекции — значит выбран способ, не переносимый на остальные модули, и седьмой
// модуль упрётся в него семикратно.
//
// Вторая точка заводится не злым умыслом: домену нужен поток, общая ручка чем-то
// не подошла, и рядом появляется своя — со своим кадрированием, своим
// возобновлением и своим смыслом пустого параметра. Через семь доменов их семь,
// а «одна проекция» остаётся названием.
//
// # Почему разбор, а не поиск по тексту
//
// И адрес ручки, и тип содержимого стоят в комментариях этого дерева — начиная с
// шапки, которую вы читаете. Гейт по подстроке краснел бы на собственном
// объяснении. Поэтому судятся СТРОКОВЫЕ ЛИТЕРАЛЫ разобранного дерева, а не
// байты файла.
//
// # Что он НЕ судит
//
// Он не судит всякое употребление слова «поток»: пробы, документация и
// комментарии называют предмет своими словами, и предикат по ним был бы неверен
// в обе стороны. Судится ровно то, что есть предмет решения, — объявление адреса
// и объявление типа содержимого.

// edgeSourceRoot — корень прод-дерева края относительно этого пакета.
const edgeSourceRoot = "../.."

// TestSubscriptionProjectionIsSingle — адрес проекции объявлен один раз, тип
// содержимого потока — один раз, и оба в этом пакете.
func TestSubscriptionProjectionIsSingle(t *testing.T) {
	pathLiterals := map[string]int{}
	streamTypeLiterals := map[string]int{}
	filesRead := 0
	literalsRead := 0

	root, err := filepath.Abs(edgeSourceRoot)
	if err != nil {
		t.Fatalf("корень дерева края: %v", err)
	}
	fset := token.NewFileSet()
	walkErr := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			return parseErr
		}
		filesRead++
		rel, _ := filepath.Rel(root, path)
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			literalsRead++
			value, unquoteErr := strconv.Unquote(lit.Value)
			if unquoteErr != nil {
				return true
			}
			if value == "/subscription/v1/events" {
				pathLiterals[rel]++
			}
			if strings.Contains(value, "text/event-stream") {
				streamTypeLiterals[rel]++
			}
			return true
		})
		return nil
	})
	if walkErr != nil {
		t.Fatalf("обход дерева края: %v", walkErr)
	}

	// Перепись печатается ВСЕГДА: «ноль находок» обязано быть отличимо от «ноль
	// прочитанного».
	t.Logf("перепись: файлов Go прочитано %d · строковых литералов осмотрено %d · "+
		"объявлений адреса проекции %d · объявлений типа содержимого потока %d",
		filesRead, literalsRead, total(pathLiterals), total(streamTypeLiterals))

	if filesRead == 0 || literalsRead == 0 {
		t.Fatalf("обход пуст (файлов %d, литералов %d) — гейт ничего не читал, "+
			"и его молчание не означает отсутствия находок", filesRead, literalsRead)
	}

	if n := total(pathLiterals); n != 1 {
		t.Errorf("адрес проекции объявлен %d раз(а) — %v. Он один: его знают композиционный "+
			"корень, полоса прав и этот гейт, и три копии строки разошлись бы молча", n, pathLiterals)
	}
	if n := total(streamTypeLiterals); n != 1 {
		t.Errorf("тип содержимого потока объявлен %d раз(а) — %v. Второе объявление означает "+
			"вторую точку проекции: способ, не переносимый на остальные модули", n, streamTypeLiterals)
	}
	for file := range streamTypeLiterals {
		if !strings.HasPrefix(file, "internal/subscriptionstream/") {
			t.Errorf("кадрирование потока живёт в %s — вне единственной проекции", file)
		}
	}
}

func total(counts map[string]int) int {
	n := 0
	for _, c := range counts {
		n += c
	}
	return n
}
