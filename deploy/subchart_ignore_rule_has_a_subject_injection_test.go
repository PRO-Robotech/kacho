// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// subchart_ignore_rule_has_a_subject_injection_test.go — инъекция в ОБЕ стороны
// для гейта правил игнорирования подчартов.
//
// Корпус — СИНТЕТИКА в t.TempDir(), а не живой `deploy/.gitignore`: фикстура,
// привязанная к живой строке, истекает вместе с ней, и в день, когда правило
// снимут, инъекция покраснела бы на верной работе. Судья при этом зовётся ТОТ
// ЖЕ, что на дереве (`readSubchartIgnoreRules` + `umbrellaDeclaredNames` +
// `judgeSubchartIgnoreRules`), иначе доказывалась бы копия.
//
// Инъекция роняет РОВНО СВОЙ предмет: каждый случай меняет против контроля один
// факт — имя в правиле, источник имени или наличие правил вовсе.
package deploy_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// injTree — синтетическое дерево: файл игнорирования, Chart.yaml зонта и
// каталоги `charts/`. Возвращает корень.
func injTree(t *testing.T, ignore string, chart string, chartDirs ...string) string {
	t.Helper()
	root := t.TempDir()
	umbrella := filepath.Join(root, "helm", "umbrella")
	if err := os.MkdirAll(filepath.Join(umbrella, "charts"), 0o755); err != nil {
		t.Fatalf("синтетика не построена: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(ignore), 0o644); err != nil {
		t.Fatalf("синтетика не построена: %v", err)
	}
	if err := os.WriteFile(filepath.Join(umbrella, "Chart.yaml"), []byte(chart), 0o644); err != nil {
		t.Fatalf("синтетика не построена: %v", err)
	}
	for _, d := range chartDirs {
		if err := os.MkdirAll(filepath.Join(umbrella, "charts", d), 0o755); err != nil {
			t.Fatalf("синтетика не построена: %v", err)
		}
	}
	return root
}

// injJudge — тот же путь, что на дереве.
func injJudge(t *testing.T, root string) ([]subchartIgnoreFinding, subchartIgnoreCensus) {
	t.Helper()
	rules, lines, err := readSubchartIgnoreRules(filepath.Join(root, ".gitignore"))
	if err != nil {
		t.Fatalf("синтетика не прочитана: %v", err)
	}
	declared, dirs, err := umbrellaDeclaredNames(filepath.Join(root, "helm", "umbrella"))
	if err != nil {
		t.Fatalf("синтетика не прочитана: %v", err)
	}
	return judgeSubchartIgnoreRules(rules, declared, lines, dirs)
}

const injChart = `apiVersion: v2
name: kacho-umbrella
dependencies:
  - name: ingress-nginx
    version: 4.15.1
  - name: postgresql
    alias: pg-iam
    version: 13.4.4
`

const injIgnoreLawful = `# правило каталога — предмет объявлен зависимостью
helm/umbrella/charts/ingress-nginx/
`

// TestSubchartIgnoreInjection_LawfulCorpusIsSilent — КОНТРОЛЬ: правило на
// объявленную зависимость находкой не является.
func TestSubchartIgnoreInjection_LawfulCorpusIsSilent(t *testing.T) {
	t.Parallel()
	findings, census := injJudge(t, injTree(t, injIgnoreLawful, injChart))
	if census.Rules != 1 {
		t.Fatalf("предпосылка контроля сломана: правил %d, ожидалось 1 (%s)", census.Rules, census)
	}
	if len(findings) != 0 {
		t.Fatalf("контроль не молчит: %v", findings)
	}
}

// TestSubchartIgnoreInjection_RuleWithoutASubjectIsFound — ИНЪЕКЦИЯ: правило на
// имя, которого зонт не объявляет, найдено, и находка называет ИМЯ, а не только
// номер строки.
func TestSubchartIgnoreInjection_RuleWithoutASubjectIsFound(t *testing.T) {
	t.Parallel()
	const injected = injIgnoreLawful + "helm/umbrella/charts/retired-thing/\n"
	findings, census := injJudge(t, injTree(t, injected, injChart))
	if census.Rules != 2 {
		t.Fatalf("инъекция не приземлилась: правил %d, ожидалось 2 (%s)", census.Rules, census)
	}
	if len(findings) != 1 {
		t.Fatalf("инъекция не найдена: находок %d, ожидалась 1 (%s)", len(findings), census)
	}
	if findings[0].Name != "retired-thing" {
		t.Fatalf("находка называет не свой предмет: %q", findings[0].Name)
	}
	if !strings.Contains(findings[0].String(), "retired-thing") {
		t.Fatalf("текст находки не называет имя: %s", findings[0])
	}
}

// TestSubchartIgnoreInjection_SourceDirectoryIsASubject — ЗАКОННЫЙ БЛИЗНЕЦ:
// чарт, лежащий исходником в `charts/` и намеренно НЕ объявленный зависимостью,
// предметом является. Без этого плеча гейт краснел бы на kaname, kacho-geo и
// kratos-selfservice-ui.
func TestSubchartIgnoreInjection_SourceDirectoryIsASubject(t *testing.T) {
	t.Parallel()
	const twin = injIgnoreLawful + "helm/umbrella/charts/source-only/\n"
	findings, census := injJudge(t, injTree(t, twin, injChart, "source-only"))
	if census.Dirs != 1 {
		t.Fatalf("предпосылка близнеца сломана: каталогов-исходников %d, ожидался 1 (%s)",
			census.Dirs, census)
	}
	if len(findings) != 0 {
		t.Fatalf("законный близнец объявлен находкой: %v", findings)
	}
}

// TestSubchartIgnoreInjection_AliasIsASubject — ЗАКОННЫЙ БЛИЗНЕЦ: имя из
// псевдонима зависимости (`alias: pg-iam`) предметом является.
func TestSubchartIgnoreInjection_AliasIsASubject(t *testing.T) {
	t.Parallel()
	const twin = injIgnoreLawful + "helm/umbrella/charts/pg-iam/\n"
	findings, _ := injJudge(t, injTree(t, twin, injChart))
	if len(findings) != 0 {
		t.Fatalf("псевдоним не признан предметом: %v", findings)
	}
}

// TestSubchartIgnoreInjection_VendoredArchiveRecordIsNotARule — ГРАНИЦА: запись
// вендоренного АРХИВА не читается правилом каталога. У неё другой владелец
// (assert-vendored-external-charts.py), и второе место об одном предмете
// разошлось бы с первым молча.
func TestSubchartIgnoreInjection_VendoredArchiveRecordIsNotARule(t *testing.T) {
	t.Parallel()
	const withArchive = injIgnoreLawful +
		"helm/umbrella/charts/*.tgz\n" +
		"!helm/umbrella/charts/retired-thing-0.1.0.tgz\n"
	findings, census := injJudge(t, injTree(t, withArchive, injChart))
	if census.Rules != 1 {
		t.Fatalf("запись архива прочитана как правило каталога: правил %d (%s)", census.Rules, census)
	}
	if len(findings) != 0 {
		t.Fatalf("граница нарушена: %v", findings)
	}
}

// TestSubchartIgnoreInjection_EmptyWalkIsNotAVerdict — ПУСТОЙ ОБХОД: файл
// игнорирования без единого правила каталога даёт НЕ «находок ноль», а отказ.
// Здесь это утверждается на переписи — ровно её читает страж предпосылки на
// дереве.
func TestSubchartIgnoreInjection_EmptyWalkIsNotAVerdict(t *testing.T) {
	t.Parallel()
	findings, census := injJudge(t, injTree(t, "# ни одного правила каталога\n.idea/\n", injChart))
	if len(findings) != 0 {
		t.Fatalf("на пустом обходе появились находки: %v", findings)
	}
	if census.Rules != 0 {
		t.Fatalf("предпосылка случая сломана: правил %d, ожидалось 0 (%s)", census.Rules, census)
	}
	if census.LinesRead == 0 {
		t.Fatalf("перепись не отличает «ноль правил» от «ноль прочитанного»: %s", census)
	}
}

// TestSubchartIgnoreInjection_CommentedRuleIsNotARule — ГРАНИЦА: правило,
// закомментированное объяснением, исполняемой частью не является.
func TestSubchartIgnoreInjection_CommentedRuleIsNotARule(t *testing.T) {
	t.Parallel()
	const commented = injIgnoreLawful + "# helm/umbrella/charts/retired-thing/\n"
	findings, census := injJudge(t, injTree(t, commented, injChart))
	if census.Rules != 1 {
		t.Fatalf("комментарий прочитан как правило: правил %d (%s)", census.Rules, census)
	}
	if len(findings) != 0 {
		t.Fatalf("гейт прочитал комментарий: %v", findings)
	}
}
