// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// own_lane_readers_wiring_injection_test.go — инъекции в обе стороны для судьи
// посадки own (postureBranchOf) на НАСТОЯЩЕМ корне (kacho#2890).
//
// Судья посадки общий у четырёх предметов провязки: читатель нашей сессии
// (`WithHumanSession`), ретрансляция (`NewLoginLaneRelay`), подготовка цели со
// стражем её адреса (`prepareRelayTarget`) и монтаж объявления
// (`MountLoginLaneRoutes`). Каждая ветка if корня, решающая own для этих мест,
// переписывается по позициям разбора в ветку case у switch без тега с тем же
// условием — это законный близнец, и все четыре судьи на нём молчат. Инъекция
// отличается от близнеца ровно одним фактом: перед этой веткой стоит `default:`
// с оператором fallthrough. Такая ветка исполняется при любой посадке, и каждое
// накрытое ею место обязано быть названо своим судьёй — координатой и
// оператором fallthrough; места под другими ветками остаются молчащими.
package main

import (
	"go/ast"
	"go/token"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
	"github.com/stretchr/testify/require"
)

// ownSubject — предмет провязки под own: места его вызова в корне и судья,
// который судит его по корню. Находки судьи — его собственный текст.
type ownSubject struct {
	callee string
	calls  func(f *ast.File) []token.Pos
	judge  func(t *testing.T, fset *token.FileSet, root *ast.File) []string
}

// ownSubjects — четыре предмета, чью посадку решает postureBranchOf.
func ownSubjects() []ownSubject {
	selectorCalls := func(name string) func(*ast.File) []token.Pos {
		return func(f *ast.File) []token.Pos { return f1bFindCall(f, name) }
	}
	sitesJudge := func(callee string) func(*testing.T, *token.FileSet, *ast.File) []string {
		return func(_ *testing.T, fset *token.FileSet, root *ast.File) []string {
			var out []string
			for _, s := range wiringSites(fset, root, callee) {
				if s.posture != "Own" {
					out = append(out, s.pos+": "+callee+" вне ветки посадки own ("+s.why+")")
				}
			}
			return out
		}
	}
	return []ownSubject{
		{"WithHumanSession", selectorCalls("WithHumanSession"), sitesJudge("WithHumanSession")},
		{"NewLoginLaneRelay", selectorCalls("NewLoginLaneRelay"),
			func(_ *testing.T, fset *token.FileSet, root *ast.File) []string {
				return judgeRelayWiring(fset, root, middleware.RelayTargets()).findings
			}},
		{"prepareRelayTarget",
			func(f *ast.File) []token.Pos {
				var out []token.Pos
				for _, call := range plainCalls(f, "prepareRelayTarget") {
					out = append(out, call.Pos())
				}
				return out
			},
			func(t *testing.T, fset *token.FileSet, root *ast.File) []string {
				return judgeRelayGuardPath(fset, root, packageWithRoot(t, fset, root), middleware.RelayTargets()).findings
			}},
		{"MountLoginLaneRoutes", selectorCalls("MountLoginLaneRoutes"), sitesJudge("MountLoginLaneRoutes")},
	}
}

// packageWithRoot — не-тестовые файлы пакета, где место main.go занимает
// переданный корень: путь к стражу судится по всему пакету.
func packageWithRoot(t *testing.T, fset *token.FileSet, root *ast.File) []*ast.File {
	t.Helper()
	out := []*ast.File{root}
	for _, f := range parsePackage(t, fset) {
		if filepath.Base(fset.Position(f.Pos()).Filename) != "main.go" {
			out = append(out, f)
		}
	}
	return out
}

// ownDecisionIfs — ветки if корня, решающие own для мест провязки предметов, в
// порядке позиций, и предметы под каждой. Предпосылка инъекции: у каждого
// предмета есть место, и каждое место решено веткой if без инициализации и без
// else — иначе переписать её в ветку case ровно одним фактом нельзя.
func ownDecisionIfs(t *testing.T, fset *token.FileSet, f *ast.File) ([]*ast.IfStmt, map[*ast.IfStmt][]string) {
	t.Helper()
	under := map[*ast.IfStmt][]string{}
	var ifs []*ast.IfStmt
	for _, s := range ownSubjects() {
		calls := s.calls(f)
		require.NotEmpty(t, calls, "предпосылка инъекции: %s провязан в корне", s.callee)
		for _, pos := range calls {
			path := enclosingNodes(f, pos)
			var decision *ast.IfStmt
			for i := range path {
				if st, ok := path[i].(*ast.IfStmt); ok && decidesOwn(path, i, pos) {
					decision = st
				}
			}
			require.NotNil(t, decision, "предпосылка инъекции: место %s у %s решено веткой if", s.callee, fset.Position(pos))
			require.Nil(t, decision.Init, "предпосылка инъекции: ветка own у %s без инициализации", fset.Position(decision.Pos()))
			require.Nil(t, decision.Else, "предпосылка инъекции: ветка own у %s без else", fset.Position(decision.Pos()))
			if _, seen := under[decision]; !seen {
				ifs = append(ifs, decision)
			}
			if names := under[decision]; len(names) == 0 || names[len(names)-1] != s.callee {
				under[decision] = append(names, s.callee)
			}
		}
	}
	sort.Slice(ifs, func(i, j int) bool { return ifs[i].Pos() < ifs[j].Pos() })
	return ifs, under
}

// switchAt — оператор switch, начинающийся с байта at разобранного корня.
func switchAt(fset *token.FileSet, f *ast.File, at int) *ast.SwitchStmt {
	var out *ast.SwitchStmt
	ast.Inspect(f, func(n ast.Node) bool {
		if sw, ok := n.(*ast.SwitchStmt); ok && offset(fset, sw.Pos()) == at {
			out = sw
		}
		return out == nil
	})
	return out
}

func TestOwnLaneInjection_ACaseEnteredByFallthroughIsNamedForEverySubject(t *testing.T) {
	src, fset, f := rootSource(t)
	for _, s := range ownSubjects() {
		require.Empty(t, s.judge(t, fset, f), "законный близнец — корень как он есть — у судьи %s обязан молчать", s.callee)
	}
	ifs, under := ownDecisionIfs(t, fset, f)

	injected, named, sites := 0, 0, 0
	defer func() {
		t.Logf("перепись: веток own в корне %d · предметов %d · мест под инъекциями %d · инъекций внесено %d · названо находкой %d",
			len(ifs), len(ownSubjects()), sites, injected, named)
	}()
	for _, decision := range ifs {
		from, to := offset(fset, decision.Pos()), offset(fset, decision.Body.Lbrace)+1
		cond := string(src[offset(fset, decision.Cond.Pos()):offset(fset, decision.Cond.End())])
		name := fset.Position(decision.Pos()).String() + " (" + strings.Join(under[decision], ", ") + ")"

		t.Run("близнец: ветка case без fallthrough у "+name, func(t *testing.T) {
			mfset, mf := mutateRoot(t, src, rootEdit{from, to, "switch {\n\tcase " + cond + ":"})
			require.NotNil(t, switchAt(mfset, mf, from), "правка близнеца не поставила switch на место ветки")
			for _, s := range ownSubjects() {
				require.Empty(t, s.judge(t, mfset, mf), "ветка case с условием own у судьи %s обязана молчать", s.callee)
			}
		})

		injected++
		t.Run("инъекция: ветка case, в которую проходит default, у "+name, func(t *testing.T) {
			mfset, mf := mutateRoot(t, src, rootEdit{from, to, "switch {\n\tdefault:\n\t\tfallthrough\n\tcase " + cond + ":"})
			sw := switchAt(mfset, mf, from)
			require.NotNil(t, sw, "правка инъекции не поставила switch на место ветки")
			covered := 0
			for _, s := range ownSubjects() {
				var want []string
				for _, pos := range s.calls(mf) {
					if within(sw, pos) {
						want = append(want, mfset.Position(pos).String())
					}
				}
				findings := s.judge(t, mfset, mf)
				if len(want) == 0 {
					require.Empty(t, findings, "места %s вне инъекции, а судья их назвал", s.callee)
					continue
				}
				covered += len(want)
				for _, finding := range findings {
					require.Contains(t, finding, "fallthrough", "находка %s не называет оператор fallthrough", s.callee)
				}
				for _, at := range want {
					hit := false
					for _, finding := range findings {
						hit = hit || strings.Contains(finding, at)
					}
					require.True(t, hit, "место %s у %s, в которое проходит default, судья не назвал: %v", s.callee, at, findings)
				}
				t.Log("находки " + s.callee + ": " + strings.Join(findings, " | "))
			}
			require.NotZero(t, covered, "инъекция не накрыла ни одного места — инъекция не того предмета")
			sites += covered
			named++
		})
	}
}
