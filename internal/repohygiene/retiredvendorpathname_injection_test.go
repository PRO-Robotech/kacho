// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// retiredvendorpathname_injection_test.go — доказательство способности гейта
// «наш путь не несёт имени снятого издателя» упасть И смолчать.
//
// Каждый отрицательный кейс отличается от своего законного близнеца ОДНИМ
// фактом — тем, что кейс называет. Имя издателя в тексте проб не выписано
// литералом: оно берётся из `foreignIDPWords`, единственного дома словаря, —
// иначе сама проба добавляла бы строки к убывающему потолку привязок.
package repohygiene

import (
	"errors"
	"strings"
	"testing"
)

// vendorPathWords — длинное слово словаря (узнаётся вхождением) и трёхбуквенное
// (узнаётся только равенством). Выбираются по СВОЙСТВУ, а не по номеру: перестановка
// словаря не должна молча подменить предмет проб.
func vendorPathWords(t *testing.T) (long, short string) {
	t.Helper()
	for _, w := range foreignIDPWords {
		if long == "" && len(w) > 3 {
			long = w
		}
		if short == "" && len(w) == 3 {
			short = w
		}
	}
	if long == "" || short == "" {
		t.Fatalf("предпосылка проб: в словаре нет длинного либо трёхбуквенного слова: %v", foreignIDPWords)
	}
	return long, short
}

// vendorPathChart — Chart.yaml зонта: одна своя зависимость (`file://`) и
// перечисленные внешние, каждая парой «имя, версия».
func vendorPathChart(external ...[2]string) []byte {
	var b strings.Builder
	b.WriteString("apiVersion: v2\nname: kacho-umbrella\nversion: 2.0.0\ndependencies:\n")
	b.WriteString("  - name: api-gateway\n    version: \">= 0.0.0\"\n    repository: file://../../../gateway/deploy/helm/api-gateway\n")
	for _, d := range external {
		b.WriteString("  - name: " + d[0] + "\n    version: " + d[1] + "\n    repository: https://charts.example.invalid/helm\n")
	}
	return []byte(b.String())
}

func judgeVendorPaths(t *testing.T, chart []byte, paths ...string) RetiredVendorPathVerdict {
	t.Helper()
	v, err := JudgeRetiredVendorPaths(paths, chart)
	if err != nil {
		t.Fatalf("вердикт не вынесен: %v", err)
	}
	if v.Walked != len(paths) {
		t.Fatalf("перепись обязана считать каждый путь: обойдено %d из %d", v.Walked, len(paths))
	}
	return v
}

// ОДИН ФАКТ: чьё слово стоит в имени файла.
func TestRetiredVendorPathName_OurPathIsFoundWithItsCoordinate(t *testing.T) {
	t.Parallel()
	long, _ := vendorPathWords(t)

	defect := "gateway/internal/middleware/" + long + "_session.go"
	v := judgeVendorPaths(t, vendorPathChart(), "go.mod", defect)
	if len(v.Findings) != 1 {
		t.Fatalf("наш путь с именем издателя обязан краснеть: находок %d (%v)", len(v.Findings), v.Findings)
	}
	f := v.Findings[0]
	if !strings.HasPrefix(f, defect+":") || !strings.Contains(f, `"`+long+`"`) {
		t.Fatalf("находка обязана назвать путь и слово, которым он узнан: %s", f)
	}
	if !strings.Contains(f, "переименовать") {
		t.Fatalf("находка обязана назвать исход, а не симптом: %s", f)
	}

	twin := "gateway/internal/middleware/session_carrier.go"
	if v := judgeVendorPaths(t, vendorPathChart(), "go.mod", twin); len(v.Findings) != 0 || v.Named != 0 {
		t.Fatalf("наше имя в том же месте пути обязано молчать: находок %v", v.Findings)
	}
}

// ОДИН ФАКТ на строку: форма записи слова в пути. Каждая форма — со своим
// близнецом той же формы, где на месте слова издателя стоит наше.
func TestRetiredVendorPathName_EveryLawfulFormIsRecognised(t *testing.T) {
	t.Parallel()
	long, short := vendorPathWords(t)
	upper := func(s string) string { return strings.ToUpper(s) }
	title := func(s string) string { return strings.ToUpper(s[:1]) + s[1:] }

	cases := []struct {
		form, defect, twin, word string
	}{
		{"kebab в имени файла", "deploy/tests/helm/" + long + "-ui-test.sh", "deploy/tests/helm/identity-ui-test.sh", long},
		{"snake в имени файла", "gateway/internal/x/" + long + "_admin.go", "gateway/internal/x/identity_admin.go", long},
		{"camelCase в имени файла", "gateway/internal/x/auth" + title(long) + "Session.go", "gateway/internal/x/authIdentitySession.go", long},
		{"склейка строчными", "deploy/helm/x/" + long + "admin.yaml", "deploy/helm/x/identityadmin.yaml", long},
		{"сегмент КАТАЛОГА", "deploy/helm/umbrella/charts/" + long + "-selfservice-ui/values.yaml", "deploy/helm/umbrella/charts/identity-selfservice-ui/values.yaml", long},
		{"трёхбуквенное словом между точкой и дефисом", "deploy/helm/umbrella/values.fe3455-" + short + "-posture.yaml", "deploy/helm/umbrella/values.fe3455-identity-posture.yaml", short},
		{"трёхбуквенное прописными", "docs/README-" + upper(short) + ".md", "docs/README-IDENTITY.md", short},
		{"трёхбуквенное за знаком вне словаря разделителей", "docs/a+" + short + ".txt", "docs/a+identity.txt", short},
	}
	for _, c := range cases {
		t.Run(c.form, func(t *testing.T) {
			t.Parallel()
			seg, w := RetiredVendorPathWord(c.defect)
			if w != c.word {
				t.Fatalf("%s: форма не узнана — слово %q вместо %q (сегмент %q)", c.defect, w, c.word, seg)
			}
			if !strings.Contains(c.defect, seg) || seg == "" || strings.Contains(seg, "/") {
				t.Fatalf("%s: координатой обязан быть сегмент пути, получено %q", c.defect, seg)
			}
			if _, w := RetiredVendorPathWord(c.twin); w != "" {
				t.Fatalf("%s: законный близнец той же формы обязан молчать, узнано %q", c.twin, w)
			}
		})
	}

	// Каждое слово словаря — поимённо: слово, о котором распознаватель не знает,
	// дало бы не красное и не зелёное, а молчание.
	for _, w := range foreignIDPWords {
		rel := "deploy/helm/x/" + w + "-thing.yaml"
		if _, got := RetiredVendorPathWord(rel); got != w {
			t.Errorf("слово словаря %q в пути %s не узнано (узнано %q)", w, rel, got)
		}
	}
}

// ОДИН ФАКТ: чужой референт тех же букв. Слово — одно и другое.
func TestRetiredVendorPathName_ForeignReferentStaysSilent(t *testing.T) {
	t.Parallel()
	silent := []string{
		"services/vpc/internal/repo/repository.go",
		"internal/memory/factory.go",
		"docs/directory-history.md",
		"deploy/helm/x/category-inventory.yaml",
		"docs/advisory/mandatory.md",
		"ui-future/shared/src/lib/use-is-hydrated.ts",
	}
	v := judgeVendorPaths(t, vendorPathChart(), silent...)
	if v.Named != 0 || len(v.Findings) != 0 {
		t.Fatalf("чужой референт тех же букв обязан молчать: несут имя %d, находки %v", v.Named, v.Findings)
	}
}

// ОДИН ФАКТ: объявлена ли зависимость, чей архив лежит в дереве.
func TestRetiredVendorPathName_VendorGivenArchiveIsSilentOnlyWhileDeclared(t *testing.T) {
	t.Parallel()
	long, _ := vendorPathWords(t)
	archive := "deploy/helm/umbrella/charts/" + long + "-0.62.1.tgz"

	declared := judgeVendorPaths(t, vendorPathChart([2]string{long, "0.62.1"}), "go.mod", archive)
	if len(declared.Findings) != 0 || len(declared.Given) != 1 || declared.Given[0] != archive {
		t.Fatalf("архив объявленной внешней зависимости — имя поставщика, находкой не является: "+
			"находки %v, прощено %v", declared.Findings, declared.Given)
	}
	if !strings.Contains(declared.Census(), archive) {
		t.Fatalf("прощённое обязано печататься поимённо: %s", declared.Census())
	}

	// Зависимость снята, архив остался: прощение истекает вместе с предметом.
	undeclared := judgeVendorPaths(t, vendorPathChart(), "go.mod", archive)
	if len(undeclared.Findings) != 1 || len(undeclared.Given) != 0 {
		t.Fatalf("архив без объявления больше ничьим именем не назван — обязан краснеть: находки %v",
			undeclared.Findings)
	}
}

// ОДИН ФАКТ на кейс: чем архив расходится с объявлением.
func TestRetiredVendorPathName_ArchiveOutsideItsDeclarationIsOurs(t *testing.T) {
	t.Parallel()
	long, _ := vendorPathWords(t)
	chart := vendorPathChart([2]string{long, "0.62.1"})

	cases := map[string]string{
		"версия не та, что объявлена": "deploy/helm/umbrella/charts/" + long + "-0.63.0.tgz",
		"не в каталоге charts/ зонта": "deploy/helm/other/charts/" + long + "-0.62.1.tgz",
	}
	for name, rel := range cases {
		if v := judgeVendorPaths(t, chart, rel); len(v.Findings) != 1 {
			t.Errorf("%s (%s): обязан краснеть, находок %d", name, rel, len(v.Findings))
		}
	}

	// Своя зависимость (`file://`) имени поставщика не даёт: её имя выбрали мы.
	own := []byte("apiVersion: v2\nname: kacho-umbrella\nversion: 2.0.0\ndependencies:\n" +
		"  - name: " + long + "-ui\n    version: 1.0.0\n    repository: file://./x\n")
	if v := judgeVendorPaths(t, own, "deploy/helm/umbrella/charts/"+long+"-ui-1.0.0.tgz"); len(v.Findings) != 1 {
		t.Fatalf("архив СВОЕЙ зависимости с именем издателя — наш путь, обязан краснеть: находок %d",
			len(v.Findings))
	}
}

// ЦЕЛЬ — НЕ ОТКАЗ: дерево без путей издателя и зонт без единой внешней
// зависимости дают ноль находок и вердикт, а не «проверять нечего».
func TestRetiredVendorPathName_GoalStateIsAVerdict(t *testing.T) {
	t.Parallel()
	chart := []byte("apiVersion: v2\nname: kacho-umbrella\nversion: 2.0.0\n")
	v := judgeVendorPaths(t, chart, "go.mod", "deploy/helm/umbrella/Chart.yaml")
	if len(v.Findings) != 0 || v.External != 0 || v.Walked != 2 {
		t.Fatalf("целевое состояние обязано проходить с переписью: %s", v.Census())
	}
}

// ПРЕДПОСЫЛКА: пустой обход и неразборный Chart.yaml — не вердикт.
func TestRetiredVendorPathName_PremiseFailureIsNotGreen(t *testing.T) {
	t.Parallel()
	if _, err := JudgeRetiredVendorPaths(nil, vendorPathChart()); !errors.Is(err, errRetiredVendorPathEmptyWalk) {
		t.Fatalf("пустой обход обязан быть отказом, а не «находок нет»: %v", err)
	}
	if _, err := JudgeRetiredVendorPaths([]string{"go.mod"}, []byte("dependencies: [\n")); err == nil {
		t.Fatal("неразборный Chart.yaml обязан быть отказом: имя поставщика от нашего отличить не по чему")
	}
}
