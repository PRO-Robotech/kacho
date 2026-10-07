// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package migratorapply_test

import (
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/treecorpus"
)

// TestServiceOfManifestIsDerivedOnlyForAServiceInTheIndex — чья форма вызова
// (Д77 (а)): две раскладки пути вне подчартов зонта — `deploy/helm/<чарт>/…`
// и `deploy/helm/umbrella/templates/<файл>` (служба — первый сегмент имени
// файла до `-` или `.`) — дают службу, только если каталог `services/<служба>`
// есть в индексе git. Иначе путь службы не даёт, и действует прежний порядок
// (ключ подчарта, затем отказ «ЧЬЮ — не выводится»).
func TestServiceOfManifestIsDerivedOnlyForAServiceInTheIndex(t *testing.T) {
	tree, err := treecorpus.NewTree(repoRoot(t))
	if err != nil {
		t.Fatalf("индекс git не прочитан: %v", err)
	}
	template := strings.Split("apiVersion: apps/v1\nkind: Deployment\nspec:\n"+
		"          command: [\"/usr/local/bin/kacho-migrator\", \"up\"]\n", "\n")
	const at = 3
	cases := []struct {
		rel, want string
	}{
		// Инъекция: чарт без каталога службы — служба не выводится.
		{"deploy/helm/zzz/templates/x.yaml", ""},
		// Близнец: тот же файл под чартом службы, чей каталог есть.
		{"deploy/helm/notify/templates/x.yaml", "notify"},
		// Шаблон зонта: служба — первый сегмент имени файла.
		{"deploy/helm/umbrella/templates/notify-probe.yaml", "notify"},
		{"deploy/helm/umbrella/templates/notify.yaml", "notify"},
		{"deploy/helm/umbrella/templates/zzz-probe.yaml", ""},
		// Прежние раскладки — без изменений.
		{"services/vpc/deploy/templates/deployment.yaml", "vpc"},
		{"deploy/helm/umbrella/charts/kacho-geo/templates/deployment.yaml", "geo"},
	}
	for _, tc := range cases {
		if got := serviceForForm(tree, tc.rel, template, at); got != tc.want {
			t.Errorf("%s: служба %q, ожидалась %q", tc.rel, got, tc.want)
		}
	}
	// Наложение значений зонта по-прежнему решает ключ подчарта: каталога
	// services/umbrella нет, и путь службы не даёт.
	values := strings.Split("kacho-geo:\n  migrate:\n    command: [\"/usr/local/bin/kacho-migrator\", \"up\"]\n", "\n")
	if got := serviceForForm(tree, "deploy/helm/umbrella/values.yaml", values, 2); got != "geo" {
		t.Errorf("наложение значений зонта: служба %q, ожидалась geo", got)
	}
}
