// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// http_body_cap_wiring_test.go — гейт: потолок тела стоит В ЦЕПОЧКЕ и СНАРУЖИ
// всего, что тело читает.
//
// Одного наличия ограничителя недостаточно: если он окажется внутри проверки
// прав, та успеет забуферизовать префикс тела и склеить нетронутый остаток
// обратно, то есть потолок будет объявлен и не будет действовать на самом
// дорогом участке. Порядок в этой цепочке выражается порядком присваиваний
// (каждое следующее оборачивает предыдущее), поэтому он проверяется по
// синтаксическому дереву, а не по тексту: упоминание в комментарии за
// присваивание не считается.
package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"
)

// TestHTTPBodyCapIsOutermostBodyReader — ограничитель тела применяется позже
// (то есть снаружи) проверки прав и всего, что тело читает.
//
// Что делать, если гейт сработал:
//
//  1. ограничителя нет вовсе -> добавить middleware.HTTPMaxBodyBytes в цепочку;
//  2. он есть, но раньше проверки прав -> перенести присваивание ПОСЛЕ неё:
//     иначе префикс тела буферизуется до того, как потолок начнёт действовать;
//  3. цепочка перестала собираться накоплением в одну переменную -> уточнить
//     распознавание ниже, а не снимать требование.
//
// Проверено инъекцией в обе стороны: перенос ограничителя внутрь проверки прав
// (и его удаление) красит гейт и называет обе координаты; текущий порядок он
// пропускает молча.
func TestHTTPBodyCapIsOutermostBodyReader(t *testing.T) {
	root := gatewayTreeRootForWiring(t)
	rel := "cmd/api-gateway/main.go"
	body, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatalf("чтение %s: %v", rel, err)
	}

	fset := token.NewFileSet()
	file, parseErr := parser.ParseFile(fset, rel, body, 0)
	if parseErr != nil {
		t.Fatalf("разбор %s: %v", rel, parseErr)
	}

	// Позиции (в байтах от начала файла) вызовов, интересующих гейт. Позиция —
	// прокси порядка присваиваний: цепочка накапливается в одну переменную
	// сверху вниз, поэтому «позже в файле» = «снаружи».
	var capPos, authzPos, idempotencyPos int
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pos := int(call.Pos())
		switch sel.Sel.Name {
		case "HTTPMaxBodyBytes":
			capPos = pos
		case "HTTP":
			// authzMW.HTTP(inner) — проверка прав, читающая префикс тела.
			if x, isIdent := sel.X.(*ast.Ident); isIdent && x.Name == "authzMW" {
				authzPos = pos
			}
		case "HTTPIdempotency":
			idempotencyPos = pos
		}
		return true
	})

	// «Ноль находок» обязано быть отличимо от «ноль прочитанного»: если гейт не
	// узнал НИ ОДНОГО звена цепочки, его распознавание сломано и молчание ничего
	// не доказывает.
	if authzPos == 0 || idempotencyPos == 0 {
		t.Fatalf("гейт не нашёл звенья цепочки в %s (проверка прав: %d, идемпотентность: %d) — "+
			"сборка цепочки изменилась, уточни распознавание", rel, authzPos, idempotencyPos)
	}
	t.Logf("цепочка распознана: идемпотентность@%d, проверка прав@%d, потолок тела@%d",
		idempotencyPos, authzPos, capPos)

	if capPos == 0 {
		t.Fatalf("в цепочке %s нет потолка тела запроса: тело неограниченного размера доезжает "+
			"до разбора, а разбор материализует его кратно — добавь "+
			"middleware.HTTPMaxBodyBytes(middleware.EdgeMaxRequestBodyBytes)", rel)
	}
	if capPos < authzPos {
		t.Errorf("потолок тела применяется РАНЬШЕ проверки прав (%d < %d), то есть оказывается "+
			"внутри неё: проверка успевает забуферизовать префикс тела и склеить остаток "+
			"обратно, и потолок не действует на самом дорогом участке. Перенеси присваивание "+
			"после неё.", capPos, authzPos)
	}
	if capPos < idempotencyPos {
		t.Errorf("потолок тела применяется РАНЬШЕ вычисления ключа идемпотентности (%d < %d): "+
			"тот тоже читает тело", capPos, idempotencyPos)
	}
}

// gatewayTreeRootForWiring — корень дерева шлюза (каталог gateway/).
func gatewayTreeRootForWiring(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		candidate := filepath.Join(dir, "gateway")
		if info, statErr := os.Stat(candidate); statErr == nil && info.IsDir() {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("каталог gateway/ не найден выше %s", dir)
		}
		dir = parent
	}
}
