// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// Инъекция для гейта области уникальности имени — В ОБЕ СТОРОНЫ.
//
// Гейт, чью способность падать не доказали, неотличим от вечно-зелёного: на
// чистом дереве он выглядит точно так же. Поэтому здесь: (а) возвращённый дефект
// обязан ДАТЬ находку и НАЗВАТЬ координату; (б) законный близнец той же формы
// обязан МОЛЧАТЬ; (в) пустой обход обязан быть отличим от «нарушений нет».
//
// Дефект возвращается НАСТОЯЩИЙ — тот самый текст, что стоял в контракте до
// задачи продукта #1597 («unique within the project» у имени слушателя при
// `listeners_lb_name_uniq (load_balancer_id, name)` в базе).

// mustSyntheticTree — состав СИНТЕТИЧЕСКОГО дерева инъекции.
//
// Такое дерево репозиторием не является, индекса у него нет, и обход файловой
// системы здесь — не откат, а единственный возможный авторитет. Конструктор
// отдельный намеренно: молчаливый откат внутри NewTree был бы невидим, а
// отдельное имя вызывающий выбирает осознанно (см. godoc treecorpus.SyntheticTree).
func mustSyntheticTree(t *testing.T, root string) *treecorpus.Tree {
	t.Helper()
	tree, err := treecorpus.SyntheticTree(root)
	if err != nil {
		t.Fatalf("синтетическое дерево %s: %v", root, err)
	}
	return tree
}

// nlbScopeFixture — синтетическое дерево: миграция (авторитет), контракт и
// страница справочника. Значения подставляются, чтобы одна и та же форма
// проверялась и как дефект, и как законный близнец.
type nlbScopeFixture struct {
	listenerScopeInProto string // "project" | "parent load balancer"
	listenerScopeInDoc   string // "балансировщика" | "проекта"
	lbScopeInProto       string // законный близнец: у балансировщика область — проект
}

func writeNlbScopeTree(t *testing.T, f nlbScopeFixture) string {
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

	// АВТОРИТЕТ: у слушателя область — балансировщик, у балансировщика — проект.
	mk("services/nlb/internal/migrations/0001_initial.sql", `
-- +goose Up
CREATE UNIQUE INDEX listeners_lb_name_uniq ON kacho_nlb.listeners (load_balancer_id, name);
CREATE UNIQUE INDEX load_balancers_project_name_uniq ON kacho_nlb.load_balancers (project_id, name);
-- +goose Down
DROP INDEX kacho_nlb.listeners_lb_name_uniq;
`)

	mk("proto/kacho/cloud/loadbalancer/v1/listener_service.proto", `
syntax = "proto3";
message CreateListenerRequest {
  // Name of the Listener. Must be a DNS label and is unique within the `+
		f.listenerScopeInProto+`. Empty on Create
  // makes the server derive the name from the assigned id.
  string name = 2;
}
`)

	mk("proto/kacho/cloud/loadbalancer/v1/network_load_balancer_service.proto", `
syntax = "proto3";
message CreateNetworkLoadBalancerRequest {
  // Name of the NetworkLoadBalancer. Must be a DNS label and is unique within
  // the `+f.lbScopeInProto+`. Empty on Create makes the server derive the name.
  string name = 2;
}
`)

	mk("services/nlb/docs/content/api/listener.mdx", `
# Listener
<tr><td><code>name</code></td><td>Имя. Уникально в рамках `+f.listenerScopeInDoc+`</td></tr>
`)

	return root
}

func TestNlbNameScopeGateInjection(t *testing.T) {
	t.Run("КОНТРОЛЬ: согласованное дерево — гейт молчит", func(t *testing.T) {
		root := writeNlbScopeTree(t, nlbScopeFixture{
			listenerScopeInProto: "parent load balancer",
			listenerScopeInDoc:   "балансировщика",
			lbScopeInProto:       "project",
		})
		c, err := collectNlbNameScope(mustSyntheticTree(t, root))
		if err != nil {
			t.Fatalf("обход: %v", err)
		}
		if got := len(c.Claims); got == 0 {
			t.Fatal("утверждений не собрано — инъекция беспредметна, молчание ничего не доказывает")
		}
		if f := nlbNameScopeFindings(c); len(f) != 0 {
			t.Errorf("гейт краснеет на исправном дереве: %v", f)
		}
	})

	t.Run("ДЕФЕКТ В КОНТРАКТЕ: #1597 возвращён — находка с координатой", func(t *testing.T) {
		root := writeNlbScopeTree(t, nlbScopeFixture{
			listenerScopeInProto: "project", // ← ровно то, что стояло до фикса
			listenerScopeInDoc:   "балансировщика",
			lbScopeInProto:       "project",
		})
		c, err := collectNlbNameScope(mustSyntheticTree(t, root))
		if err != nil {
			t.Fatalf("обход: %v", err)
		}
		f := nlbNameScopeFindings(c)
		if len(f) != 1 {
			t.Fatalf("ожидалась ровно одна находка, получено %d: %v", len(f), f)
		}
		joined := strings.Join(f, "\n")
		if !strings.Contains(joined, "listener_service.proto") {
			t.Errorf("находка не называет координату дефекта: %s", joined)
		}
		if !strings.Contains(joined, "project_id") || !strings.Contains(joined, "load_balancer_id") {
			t.Errorf("находка не называет ОБЕ области — по ней нельзя понять, что чинить: %s", joined)
		}
	})

	t.Run("ДЕФЕКТ В ДОКУМЕНТАЦИИ: зеркальная сторона тоже ловится", func(t *testing.T) {
		root := writeNlbScopeTree(t, nlbScopeFixture{
			listenerScopeInProto: "parent load balancer",
			listenerScopeInDoc:   "проекта", // документация разошлась с базой
			lbScopeInProto:       "project",
		})
		c, err := collectNlbNameScope(mustSyntheticTree(t, root))
		if err != nil {
			t.Fatalf("обход: %v", err)
		}
		f := nlbNameScopeFindings(c)
		if len(f) != 1 {
			t.Fatalf("ожидалась ровно одна находка, получено %d: %v", len(f), f)
		}
		if !strings.Contains(f[0], "listener.mdx") {
			t.Errorf("находка не называет страницу: %s", f[0])
		}
	})

	t.Run("ЗАКОННЫЙ БЛИЗНЕЦ: у балансировщика область ДЕЙСТВИТЕЛЬНО проект — молчание", func(t *testing.T) {
		root := writeNlbScopeTree(t, nlbScopeFixture{
			listenerScopeInProto: "parent load balancer",
			listenerScopeInDoc:   "балансировщика",
			lbScopeInProto:       "project",
		})
		c, err := collectNlbNameScope(mustSyntheticTree(t, root))
		if err != nil {
			t.Fatalf("обход: %v", err)
		}
		// Близнец обязан быть ПРОЧИТАН, иначе его молчание — не свидетельство.
		var seenLB bool
		for _, cl := range c.Claims {
			if cl.table == "load_balancers" && cl.column == "project_id" {
				seenLB = true
			}
		}
		if !seenLB {
			t.Fatal("утверждение балансировщика не прочитано — молчание по нему ничего не доказывает")
		}
		if f := nlbNameScopeFindings(c); len(f) != 0 {
			t.Errorf("законный близнец объявлен нарушением: %v", f)
		}
	})

	t.Run("ПУСТОЙ ОБХОД отличим от «нарушений нет»", func(t *testing.T) {
		c, err := collectNlbNameScope(mustSyntheticTree(t, t.TempDir()))
		if err != nil {
			t.Fatalf("обход: %v", err)
		}
		if c.MigrationFiles != 0 || len(c.Claims) != 0 {
			t.Fatalf("пустое дерево дало непустую перепись: %+v", c)
		}
		// Находок ноль И перепись ноль — именно эту пару и различает проверка
		// предпосылки в самом гейте (t.Fatalf на пустом обходе).
		if f := nlbNameScopeFindings(c); len(f) != 0 {
			t.Errorf("на пустом дереве найдены нарушения: %v", f)
		}
	})
}
