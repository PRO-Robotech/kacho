// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// TestOperationsTableOwnersWireTheRetentionSweep — владелец таблицы операций
// обязан ПРОВЯЗАТЬ её уборку.
//
// # Предмет
//
// Задача #1360: строка таблицы операций заводится КАЖДОЙ мутацией платформы, а
// снятия строк не было ни у одного из восьми владельцев. Механизм заведён один
// раз в `pkg/operations`, но механизм в общем пакете БЕЗ прод-вызывающего — это
// объявленный уборщик, который не убирает: гейт роста при этом зеленеет (оператор
// снятия в дереве есть), а таблица растёт по-прежнему.
//
// Соседний гейт `TestDeclaredRetentionSweepersHaveAProductionCaller` этого НЕ
// закрывает, и различие существенное: он проверяет, что у уборщика есть
// вызывающий, и `pkg/operations.RetentionSubject` его удовлетворяет — сам ни
// разу не будучи позванным. Он судит ОДНО звено цепочки; здесь проверяется, что
// цепочка доходит до композиционного корня КАЖДОГО владельца.
//
// # Предикат
//
// Владелец — пакет, который строит репозиторий операций (`operations.NewRepo`).
// От него требуется вызов `operations.StartRetentionSweep` в том же пакете.
//
// Проверка идёт РАЗБОРОМ, а не поиском подстроки: оба имени встречаются в
// комментариях этого дерева (в том числе в шапке самого механизма), и предикат
// по тексту краснел бы на собственном объяснении.
func TestOperationsTableOwnersWireTheRetentionSweep(t *testing.T) {
	root := repoRoot(t)

	type pkgState struct {
		buildsRepo bool
		startsSwep bool
		file       string
	}
	pkgs := map[string]*pkgState{}
	var filesRead int

	for _, sub := range []string{"services", "gateway"} {
		base := filepath.Join(root, sub)
		files, err := treecorpus.UnderWithSuffix(base, ".go")
		if err != nil {
			t.Fatalf("состав дерева под %s: %v", base, err)
		}
		for _, path := range files {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			fset := token.NewFileSet()
			f, perr := parser.ParseFile(fset, path, nil, 0)
			if perr != nil {
				continue
			}
			filesRead++
			rel, _ := filepath.Rel(root, path)
			dir := filepath.Dir(rel)
			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				ident, ok := sel.X.(*ast.Ident)
				if !ok || ident.Name != "operations" {
					return true
				}
				st := pkgs[dir]
				if st == nil {
					st = &pkgState{}
					pkgs[dir] = st
				}
				switch sel.Sel.Name {
				case "NewRepo":
					st.buildsRepo = true
					st.file = rel
				case "StartRetentionSweep":
					st.startsSwep = true
				}
				return true
			})
		}
	}

	var owners, wired []string
	for dir, st := range pkgs {
		if !st.buildsRepo {
			continue
		}
		owners = append(owners, dir)
		if st.startsSwep {
			wired = append(wired, dir)
		}
	}
	sort.Strings(owners)
	sort.Strings(wired)

	// Перепись ПЕЧАТАЕТСЯ ВСЕГДА: «ноль находок» обязано быть отличимо от «ноль
	// прочитанного».
	t.Logf("перепись: файлов Go прочитано %d; владельцев таблицы операций %d; из них провязали уборку %d",
		filesRead, len(owners), len(wired))

	// Проверка СВОЕЙ предпосылки: гейт судит по вызовам `operations.*`, и если
	// их в дереве не нашлось вовсе — судить не о чем, а зелёное означало бы
	// «ноль прочитанного».
	if len(owners) == 0 {
		t.Fatal("владельцев таблицы операций не найдено НИ ОДНОГО — предпосылка гейта не " +
			"выполняется: либо обход пуст, либо репозиторий операций строят иначе. " +
			"Зелёное здесь означало бы ноль прочитанного, а не ноль находок")
	}

	for _, dir := range owners {
		if pkgs[dir].startsSwep {
			continue
		}
		t.Errorf("%s строит репозиторий операций (%s), но НЕ зовёт operations.StartRetentionSweep: "+
			"таблица растёт каждой мутацией, а уборка до этого владельца не доходит. "+
			"Механизм заведён в pkg/operations один раз — провязка стоит одной строкой; "+
			"объявленный уборщик без вызывающего есть форма без содержания",
			dir, pkgs[dir].file)
	}
}
