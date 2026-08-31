// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Инъекция для гейта показанных маршрутов — В ОБЕ СТОРОНЫ.
//
// Дефект возвращается НАСТОЯЩИЙ: тот самый снятый глагол, который быстрый старт
// показывал шестым шагом из семи (задача продукта #1617).

func writeNlbRouteTree(t *testing.T, docRoutes ...string) string {
	t.Helper()
	root := t.TempDir()
	mk := func(rel, body string) {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	// Контракт производит ровно два маршрута: чтение и один :verb.
	mk("proto/kacho/cloud/loadbalancer/v1/network_load_balancer_service.proto", `
syntax = "proto3";
service NetworkLoadBalancerService {
  rpc Get(GetNetworkLoadBalancerRequest) returns (NetworkLoadBalancer) {
    option (google.api.http) = {get: "/nlb/v1/networkLoadBalancers/{network_load_balancer_id}"};
  }
  rpc Move(MoveNetworkLoadBalancerRequest) returns (operation.Operation) {
    option (google.api.http) = {
      post: "/nlb/v1/networkLoadBalancers/{network_load_balancer_id}:move"
      body: "*"
    };
  }
}
`)
	var b strings.Builder
	b.WriteString("# Быстрый старт\n")
	for _, r := range docRoutes {
		b.WriteString("curl -X POST 'http://localhost:18080" + r + "'\n")
	}
	mk("services/nlb/docs/content/getting-started.mdx", b.String())
	return root
}

func TestNlbDocumentedRoutesGateInjection(t *testing.T) {
	t.Run("КОНТРОЛЬ: показаны только живые маршруты — гейт молчит", func(t *testing.T) {
		root := writeNlbRouteTree(t,
			"/nlb/v1/networkLoadBalancers/nlb...",
			"/nlb/v1/networkLoadBalancers/nlb...:move")
		c, err := collectNlbDocumentedRoutes(mustSyntheticTree(t, root))
		if err != nil {
			t.Fatalf("обход: %v", err)
		}
		if len(c.Claims) != 2 {
			t.Fatalf("собрано %d маршрутов вместо 2 — инъекция беспредметна: %+v", len(c.Claims), c.Claims)
		}
		if f := nlbDocumentedRouteFindings(c); len(f) != 0 {
			t.Errorf("гейт краснеет на исправной документации: %v", f)
		}
	})

	t.Run("ДЕФЕКТ: #1617 возвращён — снятый глагол показан как живой", func(t *testing.T) {
		root := writeNlbRouteTree(t,
			"/nlb/v1/networkLoadBalancers/nlb...:move",
			"/nlb/v1/networkLoadBalancers/nlb...:attachTargetGroup")
		c, err := collectNlbDocumentedRoutes(mustSyntheticTree(t, root))
		if err != nil {
			t.Fatalf("обход: %v", err)
		}
		f := nlbDocumentedRouteFindings(c)
		if len(f) != 1 {
			t.Fatalf("ожидалась одна находка, получено %d: %v", len(f), f)
		}
		if !strings.Contains(f[0], "attachTargetGroup") {
			t.Errorf("находка не называет снятый глагол: %s", f[0])
		}
		if !strings.Contains(f[0], "getting-started.mdx") {
			t.Errorf("находка не называет координату: %s", f[0])
		}
	})

	t.Run("ЗАКОННЫЙ БЛИЗНЕЦ: тот же ресурс без :verb — молчание", func(t *testing.T) {
		// Отличие от дефекта ровно в одном сегменте — :verb. Близнец обязан быть
		// ПРОЧИТАН, иначе его молчание ничего не доказывает.
		root := writeNlbRouteTree(t, "/nlb/v1/networkLoadBalancers/{id}")
		c, err := collectNlbDocumentedRoutes(mustSyntheticTree(t, root))
		if err != nil {
			t.Fatalf("обход: %v", err)
		}
		if len(c.Claims) != 1 {
			t.Fatalf("близнец не прочитан: %+v", c.Claims)
		}
		if f := nlbDocumentedRouteFindings(c); len(f) != 0 {
			t.Errorf("живой маршрут объявлен находкой: %v", f)
		}
	})

	t.Run("ПОДСТАНОВКИ трёх форм записи распознаются одинаково", func(t *testing.T) {
		// Форма, о которой распознаватель не знает, — не редкость, а невидимость.
		// Все три записи одного живого маршрута обязаны молчать.
		root := writeNlbRouteTree(t,
			"/nlb/v1/networkLoadBalancers/{id}",
			"/nlb/v1/networkLoadBalancers/&#123;id&#125;",
			"/nlb/v1/networkLoadBalancers/nlb...")
		c, err := collectNlbDocumentedRoutes(mustSyntheticTree(t, root))
		if err != nil {
			t.Fatalf("обход: %v", err)
		}
		if len(c.Claims) != 3 {
			t.Fatalf("прочитано %d записей вместо 3: %+v", len(c.Claims), c.Claims)
		}
		if f := nlbDocumentedRouteFindings(c); len(f) != 0 {
			t.Errorf("одна из форм записи живого маршрута объявлена находкой: %v", f)
		}
	})

	t.Run("ПУСТОЙ ОБХОД отличим от «нарушений нет»", func(t *testing.T) {
		c, err := collectNlbDocumentedRoutes(mustSyntheticTree(t, t.TempDir()))
		if err != nil {
			t.Fatalf("обход: %v", err)
		}
		if c.ProtoFiles != 0 || len(c.Claims) != 0 {
			t.Fatalf("пустое дерево дало непустую перепись: %+v", c)
		}
	})
}
