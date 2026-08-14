// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
)

// PoolCloseFinding — одно место, где проба закрывает пул БЕЗ предела.
type PoolCloseFinding struct {
	File string // путь от корня дерева
	Line int
	Var  string // имя переменной пула
	Form string // "defer" | "t.Cleanup"
}

// poolProducers — вызовы, чей результат ЕСТЬ пул pgx. Список закрытый: гейт
// разбирает синтаксис и типов не резолвит, поэтому «пул» опознаётся по тому, ЧЕМ
// переменная заведена, а не по её имени. Имя ничего не доказывает — `r.Close`
// рядом закрывает репозиторий, а не пул, и по имени эти два неразличимы.
var poolProducers = map[string]bool{
	"db.NewPool":            true, // pkg/db, под собственным именем пакета
	"coredb.NewPool":        true, // он же под принятым в сервисах псевдонимом
	"pgxpool.New":           true,
	"pgxpool.NewWithConfig": true,
}

// ScanPoolCloses разбирает один файл и возвращает места, где пул закрывается без
// предела.
//
// Что считается находкой — ровно две формы, и обе наблюдались в дереве:
//
//	defer pool.Close()      // отложенное закрытие
//	t.Cleanup(pool.Close)   // то же через очистку
//
// Обе ждут возврата ВСЕХ выданных соединений и обе виснут, когда проба
// завершилась при открытой транзакции. Снятие у обеих одно:
// `pgtest.ClosePoolAtEnd(t, pool)`.
//
// Радиус опознания назван честно: переменная считается пулом, только если она
// заведена в ТОЙ ЖЕ функции вызовом из poolProducers. Пул, приехавший
// параметром, полем структуры или из помощника, гейту не виден — закрыть это
// потребовало бы разрешения типов, чего этот обход не делает.
func ScanPoolCloses(path string, src []byte) ([]PoolCloseFinding, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		return nil, err
	}

	var out []PoolCloseFinding
	ast.Inspect(f, func(n ast.Node) bool {
		var body *ast.BlockStmt
		switch fn := n.(type) {
		case *ast.FuncDecl:
			body = fn.Body
		case *ast.FuncLit:
			body = fn.Body
		default:
			return true
		}
		if body == nil {
			return true
		}
		pools := poolVarsIn(body)
		if len(pools) == 0 {
			return true
		}
		ast.Inspect(body, func(m ast.Node) bool {
			switch st := m.(type) {
			case *ast.DeferStmt:
				if name, ok := closeReceiver(st.Call); ok && pools[name] {
					out = append(out, PoolCloseFinding{
						File: path, Line: fset.Position(st.Pos()).Line, Var: name, Form: "defer",
					})
				}
			case *ast.CallExpr:
				if name, ok := cleanupCloseArg(st); ok && pools[name] {
					out = append(out, PoolCloseFinding{
						File: path, Line: fset.Position(st.Pos()).Line, Var: name, Form: "t.Cleanup",
					})
				}
			}
			return true
		})
		return true
	})

	sort.Slice(out, func(i, j int) bool { return out[i].Line < out[j].Line })
	return out, nil
}

// poolVarsIn — имена, заведённые в этом теле вызовом-производителем пула.
func poolVarsIn(body *ast.BlockStmt) map[string]bool {
	pools := map[string]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || len(as.Lhs) == 0 || len(as.Rhs) != 1 {
			return true
		}
		call, ok := as.Rhs[0].(*ast.CallExpr)
		if !ok || !poolProducers[qualifiedName(call.Fun)] {
			return true
		}
		if id, ok := as.Lhs[0].(*ast.Ident); ok && id.Name != "_" {
			pools[id.Name] = true
		}
		return true
	})
	return pools
}

// closeReceiver — `x.Close()` → "x".
func closeReceiver(call *ast.CallExpr) (string, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Close" {
		return "", false
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok {
		return "", false
	}
	return id.Name, true
}

// cleanupCloseArg — `t.Cleanup(x.Close)` → "x". Форма `t.Cleanup(func(){ x.Close() })`
// сюда не попадает намеренно: тело литерала обходится как отдельная функция, и
// закрытие внутри него разбирается своим проходом.
func cleanupCloseArg(call *ast.CallExpr) (string, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Cleanup" || len(call.Args) != 1 {
		return "", false
	}
	arg, ok := call.Args[0].(*ast.SelectorExpr)
	if !ok || arg.Sel.Name != "Close" {
		return "", false
	}
	id, ok := arg.X.(*ast.Ident)
	if !ok {
		return "", false
	}
	return id.Name, true
}

// qualifiedName — "pkg.Fn" для селектора, "Fn" для голого имени.
func qualifiedName(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.SelectorExpr:
		if id, ok := v.X.(*ast.Ident); ok {
			return id.Name + "." + v.Sel.Name
		}
	case *ast.Ident:
		return v.Name
	}
	return ""
}
