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

func TestEnvPrefixFollowsTheSameNameAsTheChart(t *testing.T) {
	// Приставка окружения выводится из имени части, а не из имени платформы.
	// Положительный контроль обеих форм: без него проверка зеленела бы на
	// выводе, который не различает переименованную часть и любую другую.
	for _, c := range []struct{ svc, want string }{
		{"iam", "KANAME"},
		{"vpc", "KACHO_VPC"},
		{"api-gateway", "KACHO_API_GATEWAY"},
	} {
		if got := productnaming.EnvPrefix(c.svc); got != c.want {
			t.Errorf("EnvPrefix(%q) = %q, ожидалось %q", c.svc, got, c.want)
		}
	}
}

func TestMetricNamespaceFollowsTheSameNameAsTheChart(t *testing.T) {
	for _, c := range []struct{ svc, want string }{
		{"iam", "kaname"},
		{"vpc", "kacho_vpc"},
		{"api-gateway", "kacho_api_gateway"},
	} {
		if got := productnaming.MetricNamespace(c.svc); got != c.want {
			t.Errorf("MetricNamespace(%q) = %q, ожидалось %q", c.svc, got, c.want)
		}
	}
}

func TestProductNameSeparatesTheProductFromThePart(t *testing.T) {
	// Обе стороны разом, иначе проверка зеленела бы на выводе, который
	// продукт от части не отличает: у переименованной части они совпадают,
	// у остальных — расходятся, и различие это и есть предмет.
	for _, c := range []struct{ svc, product, chart string }{
		{"iam", "kaname", "kaname"},
		{"vpc", "kacho", "kacho-vpc"},
		{"api-gateway", "kacho", "kacho-api-gateway"},
	} {
		if got := productnaming.ProductName(c.svc); got != c.product {
			t.Errorf("ProductName(%q) = %q, ожидалось %q", c.svc, got, c.product)
		}
		if got := productnaming.ChartName(c.svc); got != c.chart {
			t.Errorf("ChartName(%q) = %q, ожидалось %q", c.svc, got, c.chart)
		}
		if c.svc != "iam" && productnaming.ProductName(c.svc) == productnaming.ChartName(c.svc) {
			t.Errorf("ProductName(%q) совпало с ChartName — у части БЕЗ своего имени "+
				"продукт и часть обязаны различаться, иначе вывод не различает их вовсе",
				c.svc)
		}
	}
}

func TestMigratorBinaryFollowsTheProductNotThePart(t *testing.T) {
	// Имя накатчика ОДНО НА ПРОДУКТ, а не на службу: шесть служб платформы
	// делят одно имя, Kaname несёт своё.
	for _, c := range []struct{ svc, want string }{
		{"iam", "kaname-migrator"},
		{"vpc", "kacho-migrator"},
		{"compute", "kacho-migrator"},
		{"geo", "kacho-migrator"},
	} {
		if got := productnaming.MigratorBinary(c.svc); got != c.want {
			t.Errorf("MigratorBinary(%q) = %q, ожидалось %q", c.svc, got, c.want)
		}
	}
	// Отрицательная половина: имя накатчика Kaname не совпадает с платформенным.
	// Без неё проверка выше осталась бы зелёной на выводе, который приставку
	// продукта не читает вовсе.
	if productnaming.MigratorBinary("iam") == productnaming.MigratorBinary("vpc") {
		t.Error("накатчик Kaname назван так же, как накатчик платформы — " +
			"переименование не различает продукты")
	}
}

// TestPartOfLineStopsAtTheFirstTopLevelKey — подъём останавливается на ПЕРВОМ
// ключе верхнего уровня, а не на первом ПОХОЖЕМ НА ПОДЧАРТ.
//
// Три случая, и каждый отличается от соседа РОВНО ОДНИМ фактом:
//
//	строка под ключом подчарта          → часть названа;
//	строка под общим блоком             → часть НЕ названа (прежде приписывалась
//	                                      предыдущему подчарту — соседу);
//	строка под ключом, чьё имя записано
//	иначе (`opaSidecar`)                → тоже НЕ названа: узкий образец такой
//	                                      ключ не видел вовсе.
//
// Без второго и третьего первый зеленел бы и на подъёме, который вообще не
// останавливается: строка любого блока после `kaname:` приписывалась бы Kaname.
func TestPartOfLineStopsAtTheFirstTopLevelKey(t *testing.T) {
	// Наложение зонта: путь части НЕ называет, её называет ключ над строкой.
	const rel = "deploy/helm/umbrella/values.dev.yaml"
	lines := []string{
		"kaname:",             // 0
		"  image:",            // 1
		"    tag: dev",        // 2  ← подчарт Kaname
		"opaSidecar:",         // 3
		"  image:",            // 4
		"    repo: kacho/opa", // 5  ← общий блок, часть НЕ названа
		"security:",           // 6
		"  mode: production",  // 7  ← общий блок, часть НЕ названа
		"kacho-nlb:",          // 8
		"  replicas: 2",       // 9  ← подчарт платформы
	}

	for _, tc := range []struct {
		at      int
		want    string
		wantOK  bool
		because string
	}{
		{2, "iam", true, "строка внутри блока подчарта Kaname"},
		{5, "", false, "общий блок `opaSidecar` — его имя записано иначе, и узкий образец его не видел"},
		{7, "", false, "общий блок `security` — образцу подходит, но частью продукта не является"},
		{9, "nlb", true, "строка внутри блока платформенного подчарта"},
	} {
		got, ok := productnaming.PartOfLine(rel, lines, tc.at)
		if ok != tc.wantOK || got != tc.want {
			t.Errorf("строка %d (%s): PartOfLine дал (%q, %v), ждали (%q, %v). "+
				"Приписать строку соседу хуже, чем остановиться: покрытой оказалась "+
				"бы не та часть продукта",
				tc.at, tc.because, got, ok, tc.want, tc.wantOK)
		}
	}
}
