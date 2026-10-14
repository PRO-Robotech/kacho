// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// external_route_gate_wiring_test.go — гейт корня: сторож маршрута внешнего
// слушателя (restmux.Mux.ExternalRouteGate, kacho#3053) стоит В ЦЕПОЧКЕ и
// СНАРУЖИ слоя аутентификации, а значит и ступени подтверждения, и проверки
// прав.
//
// Поведение сторожа держат пробы пакета restmux и сквозная проба
// gateway/internal/e2e; сборку цепочки они не видят — она живёт здесь
// присваиваниями в одну переменную (каждое следующее оборачивает предыдущее).
// Порядок поэтому судится по синтаксическому дереву: упоминание в комментарии за
// присваивание не считается.
//
// Почему СНАРУЖИ аутентификации, а не за ней, как отказ маршрута на нативной
// поверхности (route_refusal_wiring_test.go). Там отказ ставится только
// внутренним методам, и перед аутентификацией он отличал бы их от прочих. Здесь
// «маршрута нет» получает КАЖДАЯ необслуживаемая координата — внутренняя и
// несуществующая одним ответом, — поэтому внутреннее от несуществующего
// неотличимо в любом состоянии вызывающего, а слои за сторожем внутренних путей
// не видят вовсе.
package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"
)

// gateWiring — позиции звеньев цепочки, интересующих гейт.
type gateWiring struct {
	gate, authn, dpop, authz, bodyCap int
	gateOwn, gateRecv, gateNext       string
}

func readGateWiring(t *testing.T, rel string) gateWiring {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(gatewayTreeRootForWiring(t), rel))
	if err != nil {
		t.Fatalf("чтение %s: %v", rel, err)
	}
	return gateWiringOf(t, rel, body)
}

func gateWiringOf(t *testing.T, rel string, body []byte) gateWiring {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), rel, body, 0)
	if err != nil {
		t.Fatalf("разбор %s: %v", rel, err)
	}
	var w gateWiring
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		recv, _ := sel.X.(*ast.Ident)
		pos := int(call.Pos())
		switch {
		case sel.Sel.Name == "ExternalRouteGate" && len(call.Args) == 2:
			w.gate = pos
			if recv != nil {
				w.gateRecv = recv.Name
			}
			if id, isID := call.Args[0].(*ast.Ident); isID {
				w.gateOwn = id.Name
			}
			if id, isID := call.Args[1].(*ast.Ident); isID {
				w.gateNext = id.Name
			}
		case sel.Sel.Name == "HTTP" && recv != nil && recv.Name == "authInterceptor":
			w.authn = pos
		case sel.Sel.Name == "Wrap" && recv != nil && recv.Name == "dpopMiddleware":
			w.dpop = pos
		case sel.Sel.Name == "HTTP" && recv != nil && recv.Name == "authzMW":
			w.authz = pos
		case sel.Sel.Name == "HTTPMaxBodyBytes":
			w.bodyCap = pos
		}
		return true
	})
	return w
}

// judgeGateWiring — находки о сборке; пусто — сборка верна.
func judgeGateWiring(w gateWiring) []string {
	var out []string
	if w.gate == 0 {
		return append(out, "сторож маршрута (ExternalRouteGate) в цепочке не смонтирован: внутренний путь на внешнем "+
			"слушателе отвечает слоем аутентификации (401) или прав (403 с именем внутреннего метода), а не «маршрута нет»")
	}
	if w.gateRecv != "restHandler" {
		out = append(out, "сторож вызван не у REST-фасада restmux.NewMux (получатель «"+w.gateRecv+"»): его таблица маршрутов — не та, что обслуживает слушатель")
	}
	if w.gateOwn != "httpMux" {
		out = append(out, "сторожу передан не httpMux (аргумент «"+w.gateOwn+"»): свои пути края он сочтёт необслуживаемыми")
	}
	if w.gateNext != "inner" {
		out = append(out, "сторож оборачивает не накопленную цепочку (аргумент «"+w.gateNext+"»)")
	}
	for _, link := range []struct {
		name string
		pos  int
	}{{"аутентификации", w.authn}, {"ступени подтверждения", w.dpop}, {"проверки прав", w.authz}} {
		if link.pos != 0 && w.gate < link.pos {
			out = append(out, "сторож смонтирован ВНУТРИ слоя "+link.name+": тот ответит на внутренний путь раньше сторожа")
		}
	}
	return out
}

// TestExternalRouteGateIsMountedOutsideAuthentication — гейт корня.
func TestExternalRouteGateIsMountedOutsideAuthentication(t *testing.T) {
	const rel = "cmd/api-gateway/main.go"
	w := readGateWiring(t, rel)
	// «Ноль находок» обязано отличаться от «ноль прочитанного».
	if w.authn == 0 || w.dpop == 0 || w.authz == 0 || w.bodyCap == 0 {
		t.Fatalf("гейт не узнал звенья цепочки в %s (аутентификация %d, ступень %d, права %d, потолок тела %d) — "+
			"сборка цепочки изменилась, уточни распознавание", rel, w.authn, w.dpop, w.authz, w.bodyCap)
	}
	t.Logf("цепочка распознана: права@%d, ступень@%d, аутентификация@%d, сторож@%d, потолок тела@%d",
		w.authz, w.dpop, w.authn, w.gate, w.bodyCap)
	for _, f := range judgeGateWiring(w) {
		t.Errorf("%s: %s", rel, f)
	}
}

// TestExternalRouteGateWiringJudgeFindsTheMisplacedGate — инъекция в обе
// стороны на синтетике: сторож внутри аутентификации и отсутствующий сторож
// краснеют; законный близнец (сторож снаружи) молчит.
func TestExternalRouteGateWiringJudgeFindsTheMisplacedGate(t *testing.T) {
	const head = "package main\nfunc f() {\n\tvar inner http.Handler = httpMux\n\tinner = authzMW.HTTP(inner)\n\tinner = dpopMiddleware.Wrap(inner)\n"
	cases := []struct {
		name  string
		tail  string
		wantN int
	}{
		{"законный близнец: снаружи", "\tinner = authInterceptor.HTTP(inner)\n\tinner = restHandler.ExternalRouteGate(httpMux, inner)\n\tinner = middleware.HTTPMaxBodyBytes(1)(inner)\n}\n", 0},
		{"внутри аутентификации", "\tinner = restHandler.ExternalRouteGate(httpMux, inner)\n\tinner = authInterceptor.HTTP(inner)\n\tinner = middleware.HTTPMaxBodyBytes(1)(inner)\n}\n", 1},
		{"не смонтирован", "\tinner = authInterceptor.HTTP(inner)\n\tinner = middleware.HTTPMaxBodyBytes(1)(inner)\n}\n", 1},
		{"чужой own", "\tinner = authInterceptor.HTTP(inner)\n\tinner = restHandler.ExternalRouteGate(nil, inner)\n\tinner = middleware.HTTPMaxBodyBytes(1)(inner)\n}\n", 1},
	}
	for _, c := range cases {
		got := judgeGateWiring(gateWiringOf(t, "synthetic.go", []byte(head+c.tail)))
		if len(got) != c.wantN {
			t.Errorf("%s: находок %d, ожидалось %d: %v", c.name, len(got), c.wantN, got)
		}
	}
}
