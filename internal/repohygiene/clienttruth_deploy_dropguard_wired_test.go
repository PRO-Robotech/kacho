// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// clienttruth_deploy_dropguard_wired_test.go — РАНТАЙМНАЯ половина стража сноса.
//
// Соседний гейт (dropguard_test.go, `TestEveryDropIsDeclaredWithANumber`) держит
// СТАТИЧЕСКУЮ половину: каждый DROP TABLE объявлен числом в `dropguard.json`.
// Он проходит у всех семи сервисов — манифесты есть везде, где есть сносы.
//
// Объявленное число ничего не защищает само по себе. Защищает СВЕРКА его с базой,
// которая стоит перед нами прямо сейчас, — `dropguard.Gate` на пути наката. И вот
// её у части сервисов не было: манифест объявлен, а спросить у базы «сколько там
// сегодня строк» некому. Замер на момент заведения гейта: сносы несут 7 сервисов,
// `Gate` звали 4 (compute, storage, registry, geo); vpc, iam и nlb — нет.
//
// Цена измерена и она необратима: миграции едут безусловным initContainer'ом на
// каждом перекате пода, поэтому на несвязанном сервисе обычный `helm upgrade`
// применяет разрушающую миграцию молча. Возврат образа возвращает форму, а не
// строки, — это говорит и сам продукт (services/storage/cmd/migrator/main.go).
//
// Гейт судит ВЫЗОВ, а не текст: он разбирает Go в синтаксическое дерево и ищет
// узел-вызов `dropguard.Gate`. Проза про сам страж — а её в миграторах много —
// под гейт не подпадает by construction, иначе он зеленел бы на собственном
// объяснении.
//
// Единица счёта — СЕРВИС, у которого инвентарь миграций несёт хотя бы один снос.
// Перечень выводится из дерева: сервис, заведённый завтра, попадает под гейт сам.
package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/dropguard"
)

// sharedMigratorRunnerRel — общий накат, сведённый из семи форков. Координата
// объявлена ЗДЕСЬ, а не по местам вызова: литерал, повторённый вызывающими,
// разъезжается молча.
const sharedMigratorRunnerRel = "internal/migratorrun"

// migratorPackageDirs — каталоги, в которых у сервиса живёт накат.
//
// Прежде их было ДВА, потому что мигратор был форкнут: у части сервисов вся
// команда лежала в `cmd/migrator`, у остальных делегировала в снятый с тех пор
// `internal/apps/migrator`. Ствол свёл накат к ОДНОМУ общему прогонщику, и
// вместе с раскладкой сменился предмет проверки: вызов стража теперь один на
// всё дерево, а от сервиса требуется до него ДОЙТИ.
//
// Это ровно тот класс, ради которого гейт и переписан, а не подправлен:
// переименование выводит предмет из-под каждого отбирающего по старому месту, и
// такой отбор не краснеет — он ЗАМОЛКАЕТ. Здесь он не замолчал только потому,
// что рядом стоит перепись прочитанных файлов.
func migratorPackageDirs(root, svc string) []string {
	return []string{
		filepath.Join(root, "services", svc, "cmd", "migrator"),
		filepath.Join(root, sharedMigratorRunnerRel),
	}
}

// callsDropguardGate разбирает все не-тестовые .go каталога и отвечает, есть ли в
// них УЗЕЛ-ВЫЗОВ `dropguard.Gate`. Возвращает также число прочитанных файлов —
// «не нашли» обязано быть отличимо от «не прочитали».
func callsDropguardGate(t *testing.T, dir string) (found bool, filesRead int) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, 0 // каталога нет — это законно, у сервиса своя раскладка
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		path := filepath.Join(dir, name)
		src, rerr := os.ReadFile(path)
		if rerr != nil {
			t.Fatalf("read %s: %v", path, rerr)
		}
		// ParseFile без ParseComments: комментарии в дерево не попадают вовсе,
		// поэтому объяснение стража не может быть принято за его вызов.
		file, perr := parser.ParseFile(fset, path, src, 0)
		if perr != nil {
			t.Fatalf("parse %s: %v", path, perr)
		}
		filesRead++
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel == nil || sel.Sel.Name != "Gate" {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok || pkg.Name != "dropguard" {
				return true
			}
			found = true
			return false
		})
	}
	return found, filesRead
}

// TestEveryServiceThatDropsRowsCountsThemBeforeMigrating — сервис, чьи миграции
// несут снос, обязан звать `dropguard.Gate` на пути наката.
//
// Проваливается на: сервисе со сносами и без вызова (объявленное число никем не
// сверяется с живой базой) и на пустом обходе (гейт, не прочитавший ни одного
// сервиса, не утверждает ничего).
func TestEveryServiceThatDropsRowsCountsThemBeforeMigrating(t *testing.T) {
	root := repoRoot(t)
	dirs := migrationDirs(t, root)

	services := make([]string, 0, len(dirs))
	for svc := range dirs {
		services = append(services, svc)
	}
	sort.Strings(services)

	var withDrops, wired, goFilesRead int
	for _, svc := range services {
		inv, err := dropguard.Inventory(svc, os.DirFS(dirs[svc]))
		if err != nil {
			t.Errorf("%s: %v", svc, err)
			continue
		}
		if len(inv.Drops) == 0 {
			continue // сносить нечего — сверять нечего
		}
		withDrops++

		var found bool
		for _, dir := range migratorPackageDirs(root, svc) {
			ok, read := callsDropguardGate(t, dir)
			goFilesRead += read
			if ok {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: миграции несут %d снос(ов) и объявляют их числом, но накат сервиса "+
				"не зовёт dropguard.Gate — объявленное число не сверяется с живой базой ни на одной "+
				"выкатке. Провязать вызов на пути `up` (образец: services/geo/cmd/migrator/main.go)",
				svc, len(inv.Drops))
			continue
		}
		wired++
	}

	// Перепись: «ноль находок» обязано быть отличимо от «ноль прочитанного».
	if len(services) == 0 {
		t.Fatal("обход пуст: ни одного каталога миграций — гейт не утверждает ничего")
	}
	if withDrops == 0 {
		t.Fatalf("ни у одного из %d сервисов не нашлось сноса — либо дерево перестало ронять "+
			"таблицы, либо разбор перестал их читать; и то и другое находка", len(services))
	}
	if goFilesRead == 0 {
		t.Fatalf("прочитано ноль файлов Go при %d сервисе(ах) со сносами — гейт судил бы по пустоте", withDrops)
	}
	t.Logf("перепись: сервисов %d · со сносами %d · зовут dropguard.Gate %d · файлов Go прочитано %d",
		len(services), withDrops, wired, goFilesRead)
}
