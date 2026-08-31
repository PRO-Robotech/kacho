// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/pkg/authz"
)

// Окно отзыва края — четвёртая ось того же контура, что снятая проверка прав,
// открытый исход и режим входа. Три первые оси возвращают «refuse to start»;
// величина окна до этой пробы лишь ПЕЧАТАЛАСЬ в самоотчёте старта (`cache_ttl_s`
// в строке `authz-mw wired`) и не оценивалась — то есть контроль был виден и не
// судился.
//
// Пары ниже парные намеренно: отрицание («выше потолка — отказ») без
// положительного контроля («в пределах потолка — молчит») зеленело бы на страже,
// отвергающем любую величину.

func ceilingBase(env string) (string, AuthzMiddlewareConfig) {
	return env, AuthzMiddlewareConfig{
		Enabled:   true,
		FailOpen:  false,
		AuthNMode: "production-strict",
	}
}

func TestWindowAboveTheCeilingRefusesStart(t *testing.T) {
	env, cfg := ceilingBase("production")
	cfg.RevocationWindow = authz.RevocationPolicy.Ceiling + time.Second

	err := validateProductionAuthzConfig(env, cfg)
	if err == nil {
		t.Fatalf("окно %v выше потолка политики %v, а старт разрешён: "+
			"величина видна в самоотчёте и не судится",
			cfg.RevocationWindow, authz.RevocationPolicy.Ceiling)
	}
	// Текст отказа читает оператор: он обязан назвать ручку и обе величины,
	// иначе стенд остаётся неподнятым и непонятным.
	for _, want := range []string{
		"KACHO_API_GATEWAY_AUTHZ_CACHE_TTL_SECONDS",
		authz.RevocationPolicy.Ceiling.String(),
		"refuse to start",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("текст отказа не называет %q: %v", want, err)
		}
	}
}

func TestWindowWithinTheCeilingStartsSilently(t *testing.T) {
	env, cfg := ceilingBase("production")
	cfg.RevocationWindow = authz.RevocationPolicy.Ceiling

	if err := validateProductionAuthzConfig(env, cfg); err != nil {
		t.Fatalf("окно ровно в потолок политики законно, а старт отвергнут: %v", err)
	}
}

// Потолок судится под ЛЮБОЙ пометкой посадки, включая стендовую.
//
// Три прежние оси стендовую пометку пропускают, и проба, написанная против неё,
// зеленела бы вхолостую — доказательства способности упасть не было бы. Здесь
// пометка выбрана стендовой намеренно: потолок есть обещание платформы про
// отзыв гранта, а пометку, которая давала бы изъятие, выбирает тот же, кто
// пишет профиль развёртывания, — то есть изъятие на ней не барьер. Тот же довод
// уже применён к общему ключу подписи в этом файле.
func TestCeilingBindsUnderTheStandLabelToo(t *testing.T) {
	for _, env := range []string{"dev", "local", "test"} {
		cfg := AuthzMiddlewareConfig{
			Enabled:          true,
			AuthNMode:        "production-strict",
			RevocationWindow: authz.RevocationPolicy.Ceiling + time.Second,
		}
		if err := validateProductionAuthzConfig(env, cfg); err == nil {
			t.Errorf("посадка %q: окно выше потолка принято — потолок отменяется пометкой, "+
				"которую выбирает автор профиля", env)
		}
	}
	// Положительный контроль на той же полосе: законная величина под стендовой
	// пометкой молчит. Без него отрицание выше зеленело бы на страже, который
	// под этими пометками отвергает всё.
	for _, env := range []string{"dev", "local", "test"} {
		cfg := AuthzMiddlewareConfig{
			Enabled:          true,
			AuthNMode:        "production-strict",
			RevocationWindow: authz.RevocationPolicy.Default,
		}
		if err := validateProductionAuthzConfig(env, cfg); err != nil {
			t.Errorf("посадка %q: законное окно %v отвергнуто: %v",
				env, cfg.RevocationWindow, err)
		}
	}
}

// Незаданная ручка означает «беру умолчание политики», и судить надо ту
// величину, с которой процесс будет работать, — иначе страж и звено читают
// одно объявление по-разному. Разрешение обеих сторон делает ОДНА функция
// политики; второй её копии в дереве быть не должно.
func TestUnsetKnobIsJudgedAsThePolicyDefault(t *testing.T) {
	env, cfg := ceilingBase("production")
	cfg.RevocationWindow = 0

	if err := validateProductionAuthzConfig(env, cfg); err != nil {
		t.Fatalf("незаданная ручка берёт умолчание политики (%v ≤ потолок %v), "+
			"а старт отвергнут: %v",
			authz.RevocationPolicy.Default, authz.RevocationPolicy.Ceiling, err)
	}
	if got := authz.RevocationPolicy.Resolve(0); got != authz.RevocationPolicy.Default {
		t.Fatalf("Resolve(0) = %v, а умолчание политики %v", got, authz.RevocationPolicy.Default)
	}
	if got := authz.RevocationPolicy.Resolve(time.Second); got != time.Second {
		t.Fatalf("Resolve(1s) = %v: положительная величина обязана доезжать как есть", got)
	}
}

// Провязка судится обходом, а не чтением диффа: страж, которому не подали
// величину, судит нулевое поле — то есть всегда умолчание политики — и остаётся
// зелёным при любой ручке чарта.
func TestRevocationWindowReachesTheGuard(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "main.go", nil, 0)
	if err != nil {
		t.Fatalf("main.go не разбирается: %v", err)
	}

	guardCalls, windowSupplied, knobRead := 0, 0, 0
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		id, ok := call.Fun.(*ast.Ident)
		if !ok || id.Name != "validateProductionAuthzConfig" {
			return true
		}
		guardCalls++
		for _, arg := range call.Args {
			lit, ok := arg.(*ast.CompositeLit)
			if !ok {
				continue
			}
			for _, el := range lit.Elts {
				kv, ok := el.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				key, ok := kv.Key.(*ast.Ident)
				if !ok || key.Name != "RevocationWindow" {
					continue
				}
				windowSupplied++
				ast.Inspect(kv.Value, func(inner ast.Node) bool {
					sel, ok := inner.(*ast.SelectorExpr)
					if ok && sel.Sel.Name == "AuthZCacheTTLSeconds" {
						knobRead++
					}
					return true
				})
			}
		}
		return true
	})

	t.Logf("перепись: вызовов стража %d · из них с величиной окна %d · читают ручку %d",
		guardCalls, windowSupplied, knobRead)

	if guardCalls == 0 {
		t.Fatal("обход беспредметен: корень не зовёт стража вовсе")
	}
	if windowSupplied != guardCalls {
		t.Errorf("величина окна подана %d раз(а) из %d вызовов стража: страж судит нулевое поле, "+
			"то есть умолчание политики, при любой ручке чарта", windowSupplied, guardCalls)
	}
	if knobRead != windowSupplied {
		t.Errorf("величина окна взята не из ручки посадки (%d из %d): судится не то, "+
			"с чем работает процесс", knobRead, windowSupplied)
	}
}
