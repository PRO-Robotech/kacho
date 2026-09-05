// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package productnaming_test

// productnaming_test.go — ведомость собственных имён обязана иметь ОБЕ стороны
// в дереве, а вывод по приставке — оставаться верным для всех остальных частей.
//
// Проверяется не «правильно ли названо» (это решение владельца), а два свойства,
// которые машина проверить может:
//
//  1. САМОИСТЕЧЕНИЕ. Запись ведомости, у которой в дереве нет каталога
//     исходников либо нет чарта, — находка. Иначе запись переживёт свой предмет
//     и будет молча уводить проверки к координате, которой нет.
//  2. ПОЛНОТА РАСПОЗНАВАНИЯ. Всякая часть, у которой есть чарт в умбрелле,
//     обязана быть распознана как часть ПРОДУКТА. Распознаватель, ослепший на
//     одном имени, не краснеет — он молчит, и это худший исход.
//
// Перепись печатается всегда: «ноль находок» обязано быть отличимо от «ноль
// прочитанного».

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/productnaming"
)

// repoRoot — корень дерева. Пакет лежит на два уровня ниже корня.
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("корень дерева не разрешается (%v) — предпосылка проверки исчезла", err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("в %s нет go.mod — корень дерева разрешён неверно, вердикт был бы о чужом дереве", root)
	}
	return root
}

// ledgerFindings — находки ведомости: запись, у которой в дереве нет каталога
// исходников либо нет чарта. Вынесена отдельно и принимает корень с ведомостью,
// чтобы доказательство способности упасть подавало ей НАСТОЯЩИЙ вход, а не
// повторяло её логику своей копией.
func ledgerFindings(root string, ledger map[string]string) []string {
	var out []string
	for dir, name := range ledger {
		if st, err := os.Stat(filepath.Join(root, "services", dir)); err != nil || !st.IsDir() {
			out = append(out, fmt.Sprintf(
				"запись %q → %q: каталога исходников services/%s в дереве нет — "+
					"запись пережила свой предмет", dir, name, dir))
		}
		chart := filepath.Join(root, "deploy", "helm", "umbrella", "charts", name)
		if st, err := os.Stat(chart); err != nil || !st.IsDir() {
			out = append(out, fmt.Sprintf(
				"запись %q → %q: чарта deploy/helm/umbrella/charts/%s в дереве нет — "+
					"запись пережила свой предмет", dir, name, name))
		}
	}
	sort.Strings(out)
	return out
}

func TestRenamedServicesLedgerHasBothSidesInTheTree(t *testing.T) {
	root := repoRoot(t)
	ledger := productnaming.RenamedServices()

	if len(ledger) == 0 {
		t.Log("перепись: записей собственных имён 0 — ведомость пуста, и это законно: " +
			"пустая ведомость означает, что своё имя не получила ни одна часть")
	}
	for _, f := range ledgerFindings(root, ledger) {
		t.Error(f)
	}
	t.Logf("перепись: записей ведомости %d, у каждой сверены обе стороны (каталог исходников и чарт)",
		len(ledger))
}

func TestEveryUmbrellaChartResolvesToAPartOfTheProduct(t *testing.T) {
	root := repoRoot(t)
	chartsDir := filepath.Join(root, "deploy", "helm", "umbrella", "charts")
	entries, err := os.ReadDir(chartsDir)
	if err != nil {
		t.Fatalf("каталог чартов умбреллы не читается (%v) — предпосылка проверки исчезла", err)
	}

	seen, ours, foreign := 0, 0, 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		seen++
		if _, ok := productnaming.ServiceDir(e.Name()); ok {
			ours++
			continue
		}
		// Чужой вендоренный чарт — законный случай, и распознаватель обязан
		// отвечать о нём «не наш», а не молчать.
		foreign++
	}
	if seen == 0 {
		t.Fatal("каталогов чартов прочитано ноль — обход пуст, вердикт беспредметен")
	}
	if ours == 0 {
		t.Errorf("ни один из %d чартов не распознан как часть продукта — распознаватель "+
			"ослеп целиком, а молчание неотличимо от чистоты", seen)
	}
	t.Logf("перепись: каталогов чартов %d, распознано частями продукта %d, чужих (вендоренных) %d",
		seen, ours, foreign)
}

func TestChartNameAndServiceDirRoundTrip(t *testing.T) {
	cases := []struct {
		svc, chart string
	}{
		{"iam", "kaname"},    // собственное имя продукта
		{"vpc", "kacho-vpc"}, // вывод по приставке платформы
		{"nlb", "kacho-nlb"},
	}
	for _, c := range cases {
		if got := productnaming.ChartName(c.svc); got != c.chart {
			t.Errorf("ChartName(%q) = %q, ожидалось %q", c.svc, got, c.chart)
		}
		got, ok := productnaming.ServiceDir(c.chart)
		if !ok || got != c.svc {
			t.Errorf("ServiceDir(%q) = %q,%v, ожидалось %q,true", c.chart, got, ok, c.svc)
		}
	}
}

func TestIsProductImageRepoSeesBothNamingForms(t *testing.T) {
	// Положительный контроль ОБЕИХ форм — без него отрицание зеленело бы на
	// распознавателе, который не признаёт ничего.
	for _, repo := range []string{
		"docker.io/prorobotech/kacho-vpc",
		"docker.io/prorobotech/kaname",
		"kaname",
		"kacho-api-gateway",
	} {
		if !productnaming.IsProductImageRepo(repo) {
			t.Errorf("IsProductImageRepo(%q) = false — образ продукта не распознан", repo)
		}
	}
	// Отрицательный: чужие образы частями продукта не считаются.
	for _, repo := range []string{
		"bitnamilegacy/postgresql",
		"axllent/mailpit",
		"oryd/hydra",
	} {
		if productnaming.IsProductImageRepo(repo) {
			t.Errorf("IsProductImageRepo(%q) = true — чужой образ зачтён нашим", repo)
		}
	}
}
