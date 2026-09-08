// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: Apache-2.0

// contractrootliteral_injection_test.go — способность гейта упасть и смолчать
// доказывается ИНЪЕКЦИЕЙ настоящим входом, а не прочтением.
//
// Каждая пара отличается РОВНО ОДНИМ фактом: чем именно отбирается популяция.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/contractroot"
)

// synthGoTree — синтетическое дерево из одного файла с заданным телом.
func synthGoTree(t *testing.T, body string) (root string, dirs []string) {
	t.Helper()
	root = t.TempDir()
	dir := filepath.Join(root, "internal", "probe")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("фикстура не собрана: %v", err)
	}
	src := "package probe\n\nimport \"strings\"\n\nfunc judge(s string) bool {\n\t" + body + "\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "probe.go"), []byte(src), 0o600); err != nil {
		t.Fatalf("фикстура не собрана: %v", err)
	}
	return root, []string{"internal/probe"}
}

// rootPrefix — приставка объявленного корня, СОБРАННАЯ, а не выписанная.
//
// Собранная намеренно: выписанный литерал `"kacho."` лежал бы в исходнике этого
// файла, а гейт обходит `internal/repohygiene` целиком — то есть краснел бы на
// фикстуре собственной инъекции. Тот же класс, который корпус ловит: проверка,
// считающая своё объяснение предметом.
func rootPrefix(sep string) string { return contractroot.Roots[0] + sep }

// TestInjection_RootPrefixLiteralIsAFinding — ИНЪЕКЦИЯ.
func TestInjection_RootPrefixLiteralIsAFinding(t *testing.T) {
	root, dirs := synthGoTree(t, `return strings.HasPrefix(s, "`+rootPrefix(".")+`")`)
	findings, census, err := AuditContractRootLiterals(root, dirs)
	if err != nil {
		t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: %v", err)
	}
	if census.PrefixCalls != 1 {
		t.Fatalf("распознаватель не увидел вызова: встречено %d (%s)", census.PrefixCalls, census)
	}
	if len(findings) != 1 {
		t.Fatalf("литерал приставки корня не найден: находок %d (%s) — гейт потерял "+
			"способность падать", len(findings), census)
	}
	got := findings[0].String()
	if !strings.Contains(got, "internal/probe/probe.go") || !strings.Contains(got, "contractroot") {
		t.Errorf("находка не называет ни координаты, ни того, чем чинить: %s", got)
	}
}

// TestInjection_PathFormOfTheRootPrefixIsAlsoAFinding — вторая ФОРМА записи той
// же приставки. Форма, о которой распознаватель не знает, даёт молчание.
func TestInjection_PathFormOfTheRootPrefixIsAlsoAFinding(t *testing.T) {
	root, dirs := synthGoTree(t, `return strings.HasPrefix(s, "`+rootPrefix("/")+`")`)
	findings, _, err := AuditContractRootLiterals(root, dirs)
	if err != nil {
		t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("путевая форма приставки корня не найдена: находок %d — вторая форма "+
			"осталась вне наблюдения", len(findings))
	}
}

// TestInjection_DomainPrefixIsLegitimateAndSilent — ЗАКОННЫЙ БЛИЗНЕЦ.
//
// Отличие от инъекции РОВНО ОДНО: у литерала есть продолжение, то есть он
// отбирает ДОМЕН, а не корень. Без этого утверждения гейт ловил бы форму, а не
// существо, и первый же ложный срабат его отключил бы.
func TestInjection_DomainPrefixIsLegitimateAndSilent(t *testing.T) {
	root, dirs := synthGoTree(t, `return strings.HasPrefix(s, "`+rootPrefix(".")+`cloud.compute")`)
	findings, census, err := AuditContractRootLiterals(root, dirs)
	if err != nil {
		t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: %v", err)
	}
	if census.LiteralArgs != 1 {
		t.Fatalf("литерал не осмотрен вовсе: %s — близнец вакуумен", census)
	}
	if len(findings) != 0 {
		t.Fatalf("отбор ДОМЕНА объявлен находкой: %v — гейт ловит форму, а не существо", findings)
	}
}

// TestInjection_DeclaredDictionaryIsSilent — ВТОРОЙ законный близнец: отбор взят
// у объявленного словаря, литерала нет вовсе.
func TestInjection_DeclaredDictionaryIsSilent(t *testing.T) {
	root, dirs := synthGoTree(t, `return len(s) > 0 && strings.HasPrefix(s, prefixFromDictionary())`)
	// Функции нет — файл не соберётся, но РАЗБОР его читает: гейт судит узлы, а
	// не сборку. Существенно, что аргумент не литерал.
	findings, census, err := AuditContractRootLiterals(root, dirs)
	if err != nil {
		t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: %v", err)
	}
	if census.PrefixCalls != 1 || census.LiteralArgs != 0 {
		t.Fatalf("перепись не различает литерал и вызов: %s", census)
	}
	if len(findings) != 0 {
		t.Fatalf("отбор у словаря объявлен находкой: %v", findings)
	}
}

// TestInjection_MissingWalkDirIsRefusedNotSilent — каталог обхода, которого нет,
// даёт ОТКАЗ, а не пустой зелёный.
func TestInjection_MissingWalkDirIsRefusedNotSilent(t *testing.T) {
	root := t.TempDir()
	_, _, err := AuditContractRootLiterals(root, []string{"internal/nosuchdir"})
	if err == nil {
		t.Fatal("несуществующий каталог обхода принят молча — «ноль находок» стало бы " +
			"неотличимо от «ноль прочитанного»")
	}
}
