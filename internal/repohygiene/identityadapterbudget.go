// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
)

// identityadapterbudget.go — разбор для переписи «каждый глагол адаптера края к
// службе доступа ограничен бюджетом» (приёмка KA1, Р4; kacho#2713, kacho#2738).
//
// Предмет — методы `*SessionRevocationsAdapter`
// (`gateway/internal/clients/session_revocations_client.go`), которые зовут
// соседа через поля-клиенты (`a.client`, `a.iam`, `a.human`). Каждый такой
// вызов обязан идти ПОСЛЕ взятия контекста с бюджетом (`a.bounded(ctx)`) в том
// же методе: иначе зависший сосед держит запрос края столько, сколько его держит
// клиент. Новый глагол, дописанный без предела, проходит всё зелёное и ничего
// не нарушает на вид — поэтому свойство держится переписью, а не пробой
// отдельного вопроса.

// IdentityAdapterFile — носитель адаптера.
const IdentityAdapterFile = "gateway/internal/clients/session_revocations_client.go"

// identityAdapterRecv / identityAdapterClients / identityAdapterBound — тип
// адаптера, его поля-клиенты соседа и метод взятия бюджета.
const (
	identityAdapterRecv  = "SessionRevocationsAdapter"
	identityAdapterBound = "bounded"
)

var identityAdapterClients = map[string]bool{"client": true, "iam": true, "human": true}

// IdentityAdapterCensus — объём осмотренного.
type IdentityAdapterCensus struct {
	// Methods — методов адаптера разобрано.
	Methods int
	// Verbs — вызовов соседа (через поля-клиенты) найдено.
	Verbs int
	// Bounded — из них — после взятия бюджета.
	Bounded int
}

// IdentityAdapterFinding — вызов соседа без бюджета.
type IdentityAdapterFinding struct {
	Method string
	Line   int
}

// FindUnboundedIdentityAdapterVerbs судит исходник адаптера.
func FindUnboundedIdentityAdapterVerbs(src string) ([]IdentityAdapterFinding, IdentityAdapterCensus, error) {
	var (
		findings []IdentityAdapterFinding
		census   IdentityAdapterCensus
	)
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, IdentityAdapterFile, src, 0)
	if err != nil {
		return nil, census, err
	}
	for _, d := range file.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || fn.Body == nil || len(fn.Recv.List) != 1 || identityAdapterRecvName(fn.Recv.List[0].Type) != identityAdapterRecv {
			continue
		}
		census.Methods++
		recv := ""
		if len(fn.Recv.List[0].Names) == 1 {
			recv = fn.Recv.List[0].Names[0].Name
		}
		boundAt := token.NoPos
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			// a.bounded(ctx)
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == recv && sel.Sel.Name == identityAdapterBound {
				if boundAt == token.NoPos || call.Pos() < boundAt {
					boundAt = call.Pos()
				}
				return true
			}
			// a.<client>.<Verb>(…)
			inner, ok := sel.X.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if id, ok := inner.X.(*ast.Ident); !ok || id.Name != recv || !identityAdapterClients[inner.Sel.Name] {
				return true
			}
			census.Verbs++
			if boundAt != token.NoPos && boundAt < call.Pos() {
				census.Bounded++
				return true
			}
			findings = append(findings, IdentityAdapterFinding{Method: fn.Name.Name, Line: fset.Position(call.Pos()).Line})
			return true
		})
	}
	sort.Slice(findings, func(i, j int) bool { return findings[i].Line < findings[j].Line })
	return findings, census, nil
}

func identityAdapterRecvName(e ast.Expr) string {
	if st, ok := e.(*ast.StarExpr); ok {
		e = st.X
	}
	if id, ok := e.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}
