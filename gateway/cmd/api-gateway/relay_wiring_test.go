// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// relay_wiring_test.go — провязка ретрансляторов и монтаж объявления в
// композиционном корне (замысел LINE-A-1 §5.1б п. 2а, §7 инв. 30, 33, 36;
// полоса L13, kacho#2817).
//
// # Что судится
//
// Гейт прежде судил КОНСТАНТУ: `len(relays) != 1`. Цели ретрансляции теперь две
// — слушатель формы и слушатель выдачи, — и сводимы они не к одной: разные
// ручки адреса, разные порты, разный режим предъявления клиента. Гейт судит
// МНОЖЕСТВО:
//
//   - провязок `NewLoginLaneRelay` столько, сколько целей в закрытом перечне
//     `middleware.RelayTargets()`, и у каждой цель названа ключом `Serves:`
//     (ось различения); лишняя провязка краснит и НАЗЫВАЕТ позицию, цель без
//     провязки краснит тоже;
//   - каждая провязка стоит под веткой посадки `own`;
//   - адрес каждой — СВОЯ ручка конфигурации (`Target: cfg.<Поле>`): два
//     ретранслятора на одно поле были бы одной целью под двумя именами;
//   - предел ни у одной не переопределён (`Timeout:` не задан): предел
//     наследуется у механизма — одна названная величина на обе цели (инв. 30);
//   - монтаж объявления — ровно один вызов `MountLoginLaneRoutes`, под `own`:
//     снятый монтаж оставлял бы все пробы зелёными, а пути — мёртвыми (инв. 33).
package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// relaySite — одна провязка ретранслятора в корне.
type relaySite struct {
	pos     string
	posture string
	serves  string // имя селектора цели (`RelayTargetForm`), "" если не названа
	target  string // поле конфигурации адреса (`LoginLaneURL`), "" если не cfg.<Поле>
	timeout bool   // задан ли `Timeout:`
}

type relayWiring struct {
	sites    []relaySite
	findings []string
}

// relayTargetSelector — имя константы цели в пакете middleware.
func relayTargetSelector(tg middleware.RelayTarget) string {
	switch tg {
	case middleware.RelayTargetForm:
		return "RelayTargetForm"
	case middleware.RelayTargetIssuance:
		return "RelayTargetIssuance"
	}
	return ""
}

// judgeRelayWiring — вердикт по провязкам ретранслятора в разобранном корне.
func judgeRelayWiring(fset *token.FileSet, f *ast.File, targets []middleware.RelayTarget) relayWiring {
	var out relayWiring
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "NewLoginLaneRelay" {
			return true
		}
		s := relaySite{pos: fset.Position(call.Pos()).String(), posture: postureBranchOf(f, call.Pos())}
		if len(call.Args) == 1 {
			if lit, ok := call.Args[0].(*ast.CompositeLit); ok {
				for _, el := range lit.Elts {
					kv, ok := el.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					key, _ := kv.Key.(*ast.Ident)
					if key == nil {
						continue
					}
					switch key.Name {
					case "Serves":
						if v, ok := kv.Value.(*ast.SelectorExpr); ok {
							s.serves = v.Sel.Name
						}
					case "Target":
						if v, ok := kv.Value.(*ast.SelectorExpr); ok {
							if id, ok := v.X.(*ast.Ident); ok && id.Name == "cfg" {
								s.target = v.Sel.Name
							}
						}
					case "Timeout":
						s.timeout = true
					}
				}
			}
		}
		out.sites = append(out.sites, s)
		return true
	})

	if len(targets) == 0 {
		out.findings = append(out.findings, "закрытый перечень целей пуст — судить нечего, и это не зелёный")
		return out
	}
	want := map[string]bool{}
	for _, tg := range targets {
		name := relayTargetSelector(tg)
		if name == "" {
			out.findings = append(out.findings, "цель "+string(tg)+" объявлена без имени константы — гейт не умеет её назвать")
			continue
		}
		want[name] = true
	}
	servedBy := map[string]string{}
	fieldBy := map[string]string{}
	for _, s := range out.sites {
		switch {
		case s.serves == "":
			out.findings = append(out.findings, s.pos+": провязка ретранслятора без названной цели (`Serves:`) — ось различения потеряна")
		case !want[s.serves]:
			out.findings = append(out.findings, s.pos+": провязка ретранслятора цели "+s.serves+", которой нет в закрытом перечне")
		case servedBy[s.serves] != "":
			out.findings = append(out.findings, s.pos+": лишняя провязка ретранслятора цели "+s.serves+" — первая стоит в "+servedBy[s.serves])
		default:
			servedBy[s.serves] = s.pos
		}
		if s.posture != "Own" {
			out.findings = append(out.findings, s.pos+": ретранслятор заведён вне ветки посадки own (ветка: \""+s.posture+"\") — под external пути объявления обязаны отвечать 404")
		}
		if s.target == "" {
			out.findings = append(out.findings, s.pos+": адрес ретранслятора не взят из ручки конфигурации (`Target: cfg.<Поле>`)")
		} else if prev := fieldBy[s.target]; prev != "" {
			out.findings = append(out.findings, s.pos+": адрес ретранслятора — та же ручка cfg."+s.target+", что у провязки в "+prev+" — одна цель под двумя именами")
		} else {
			fieldBy[s.target] = s.pos
		}
		if s.timeout {
			out.findings = append(out.findings, s.pos+": предел ретрансляции переопределён — он наследуется у механизма одной величиной (инв. 30)")
		}
	}
	missing := []string{}
	for name := range want {
		if servedBy[name] == "" {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	for _, name := range missing {
		out.findings = append(out.findings, "объявленная цель "+name+" осталась без провязки ретранслятора — её записи отвечали бы 404")
	}
	return out
}

// Настоящий корень: провязок ровно по цели, находок ноль.
func TestRelayWiring_L13_EveryDeclaredTargetIsWiredOnceUnderOwn(t *testing.T) {
	fset, f := parseMain(t)
	got := judgeRelayWiring(fset, f, middleware.RelayTargets())
	for _, finding := range got.findings {
		t.Error(finding)
	}
	if len(got.sites) != len(middleware.RelayTargets()) {
		t.Errorf("провязок ретранслятора %d, целей %d", len(got.sites), len(middleware.RelayTargets()))
	}
	t.Logf("перепись: провязок ретранслятора %d · целей объявлено %d · находок %d", len(got.sites), len(middleware.RelayTargets()), len(got.findings))
}

// Монтаж объявления — ровно один вызов, под `own` (инв. 33).
func TestRelayWiring_L13_TheDeclarationIsMountedOnceUnderOwn(t *testing.T) {
	fset, f := parseMain(t)
	mounts := wiringSites(fset, f, "MountLoginLaneRoutes")
	if len(mounts) != 1 {
		t.Fatalf("монтаж объявления вызван %d раз, ожидался 1: без него пути объявления отвечают «не найдено», и ни одна проба пакета этого не видит", len(mounts))
	}
	if mounts[0].posture != "Own" {
		t.Fatalf("монтаж объявления стоит вне ветки посадки own: %s (ветка %q)", mounts[0].pos, mounts[0].posture)
	}
	// Второго, ручного монтажа путей объявления нет: цикл по перечню с
	// `Handle` мимо функции монтажа разошёлся бы с ней молча.
	loops := 0
	ast.Inspect(f, func(n ast.Node) bool {
		rs, ok := n.(*ast.RangeStmt)
		if !ok {
			return true
		}
		if len(callsNamedIn(rs.X, "LoginLaneRoutes")) > 0 && len(callsNamedIn(rs.Body, "Handle")) > 0 {
			loops++
			t.Errorf("%s: ручной монтаж путей объявления мимо MountLoginLaneRoutes", fset.Position(rs.Pos()))
		}
		return true
	})
	t.Logf("перепись: вызовов монтажа %d (под own) · ручных циклов монтажа %d", len(mounts), loops)
}

// parseSource — разбор синтетики под именем корня.
func parseSource(t *testing.T, src string) (*token.FileSet, *ast.File) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "main.go", src, 0)
	if err != nil {
		t.Fatalf("синтетика не разбирается: %v", err)
	}
	return fset, f
}

// callsNamedIn — позиции вызовов с данным именем внутри узла.
func callsNamedIn(root ast.Node, name string) []token.Pos {
	var out []token.Pos
	ast.Inspect(root, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == name {
			out = append(out, call.Pos())
		}
		return true
	})
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// Инъекции в обе стороны — синтетика.

const relayWiringFixture = `package main
import "github.com/PRO-Robotech/corelib/identityposture"
func wire(lane identityposture.Provider) {
	if lane == identityposture.Own {
		a, _ := handler.NewLoginLaneRelay(handler.LoginLaneRelayConfig{Serves: middleware.RelayTargetForm, Target: cfg.LoginLaneURL})
		b, _ := handler.NewLoginLaneRelay(handler.LoginLaneRelayConfig{Serves: middleware.RelayTargetIssuance, Target: cfg.IAMIssuanceURL})
%s
	}
%s
}
`

func judgeRelayFixture(t *testing.T, inside, outside string) relayWiring {
	t.Helper()
	src := strings.Replace(strings.Replace(relayWiringFixture, "%s", inside, 1), "%s", outside, 1)
	fset, f := parseSource(t, src)
	return judgeRelayWiring(fset, f, middleware.RelayTargets())
}

func TestRelayWiring_L13_Twin_TwoTargetsTwoWiringsIsSilent(t *testing.T) {
	if got := judgeRelayFixture(t, "", ""); len(got.findings) != 0 {
		t.Fatalf("законная провязка обеих целей дала находки: %v", got.findings)
	}
}

func TestRelayWiring_L13_Injection_AnExtraWiringIsNamedByPosition(t *testing.T) {
	got := judgeRelayFixture(t, "\t\tc, _ := handler.NewLoginLaneRelay(handler.LoginLaneRelayConfig{Serves: middleware.RelayTargetIssuance, Target: cfg.Other})", "")
	if len(got.findings) != 1 || !strings.Contains(got.findings[0], "лишняя провязка") || !strings.HasPrefix(got.findings[0], "main.go:7:") {
		t.Fatalf("лишняя провязка не названа позицией: %v", got.findings)
	}
}

func TestRelayWiring_L13_Injection_ATargetWithoutWiringIsFound(t *testing.T) {
	src := strings.Replace(relayWiringFixture, "\t\tb, _ := handler.NewLoginLaneRelay(handler.LoginLaneRelayConfig{Serves: middleware.RelayTargetIssuance, Target: cfg.IAMIssuanceURL})\n", "", 1)
	fset, f := parseSource(t, strings.Replace(strings.Replace(src, "%s", "", 1), "%s", "", 1))
	got := judgeRelayWiring(fset, f, middleware.RelayTargets())
	if len(got.findings) != 1 || !strings.Contains(got.findings[0], "RelayTargetIssuance осталась без провязки") {
		t.Fatalf("цель без провязки не найдена: %v", got.findings)
	}
}

func TestRelayWiring_L13_Injection_OutsideOwnSharedKnobAndOverriddenLimitAreFound(t *testing.T) {
	outside := judgeRelayFixture(t, "", "\tc, _ := handler.NewLoginLaneRelay(handler.LoginLaneRelayConfig{Serves: middleware.RelayTargetForm, Target: cfg.X})")
	joined := strings.Join(outside.findings, "\n")
	if !strings.Contains(joined, "вне ветки посадки own") || !strings.Contains(joined, "лишняя провязка") {
		t.Fatalf("провязка вне own не найдена: %v", outside.findings)
	}
	src := strings.Replace(relayWiringFixture, "Target: cfg.IAMIssuanceURL}", "Target: cfg.LoginLaneURL, Timeout: 30}", 1)
	fset, f := parseSource(t, strings.Replace(strings.Replace(src, "%s", "", 1), "%s", "", 1))
	got := strings.Join(judgeRelayWiring(fset, f, middleware.RelayTargets()).findings, "\n")
	if !strings.Contains(got, "та же ручка cfg.LoginLaneURL") || !strings.Contains(got, "предел ретрансляции переопределён") {
		t.Fatalf("общая ручка адреса и переопределённый предел не найдены: %s", got)
	}
}

func TestRelayWiring_L13_Injection_AnEmptyTargetListIsNotSilentSuccess(t *testing.T) {
	fset, f := parseSource(t, strings.Replace(strings.Replace(relayWiringFixture, "%s", "", 1), "%s", "", 1))
	if got := judgeRelayWiring(fset, f, nil); len(got.findings) == 0 {
		t.Fatal("пустой перечень целей дал зелёное")
	}
}
