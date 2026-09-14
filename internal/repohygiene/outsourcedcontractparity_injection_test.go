// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// outsourcedcontractparity_injection_test.go — СПОСОБНОСТЬ ПАДАТЬ И МОЛЧАТЬ,
// доказанная настоящим входом: синтетическим деревом, которое анализатор
// разбирает тем же кодом, что и рабочее.
//
// У КАЖДОЙ оси — ЗАКОННЫЙ БЛИЗНЕЦ. Без него анализатор ловил бы форму, а не
// существо, и первый же ложный срабат его отключил бы.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const injFoundation = "example.com/foundation/api"

// writeContractParityFile — файл синтетического дерева с объявленными импортами.
func writeContractParityFile(t *testing.T, dir, name string, imports ...string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("каталог %s: %v", dir, err)
	}
	var b strings.Builder
	b.WriteString("package synthetic\n\nimport (\n")
	for _, imp := range imports {
		b.WriteString("\t_ \"" + imp + "\"\n")
	}
	b.WriteString(")\n")
	if err := os.WriteFile(filepath.Join(dir, name), []byte(b.String()), 0o600); err != nil {
		t.Fatalf("файл %s: %v", name, err)
	}
}

// contractParityTree — пара деревьев: наше и пиненного модуля.
func contractParityTree(t *testing.T, ourImports, theirImports []string) OutsourcedContractParityOptions {
	t.Helper()
	root := t.TempDir()
	ourDir := filepath.Join(root, "ours")
	theirDir := filepath.Join(root, "theirs")
	writeContractParityFile(t, ourDir, "edge.go", ourImports...)
	writeContractParityFile(t, theirDir, "wiring.go", theirImports...)
	return OutsourcedContractParityOptions{
		OurRoots:      []string{ourDir},
		Pinned:        map[string]string{"example.com/service": theirDir},
		FoundationAPI: injFoundation,
	}
}

// TestInjectionContractMovedIsAFinding — ДЕФЕКТ: контракт переехал у одной
// стороны. Анализатор обязан НАЗВАТЬ его и координату.
func TestInjectionContractMovedIsAFinding(t *testing.T) {
	t.Parallel()
	var log strings.Builder
	findings, census, err := AuditOutsourcedContractParity(contractParityTree(t,
		[]string{injFoundation + "/foundation/subscription"},
		[]string{injFoundation + "/platform/cloud/subscription"},
	), &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("ожидалась ОДНА находка, получено %d: %s", len(findings), log.String())
	}
	f := findings[0]
	if f.Contract != "subscription" {
		t.Fatalf("находка не назвала контракт: %+v", f)
	}
	if f.Ours != injFoundation+"/foundation/subscription" || f.Theirs != injFoundation+"/platform/cloud/subscription" {
		t.Fatalf("находка не назвала ОБА пути: %+v", f)
	}
	if !strings.Contains(f.Coord, "wiring.go") {
		t.Fatalf("находка не назвала координату у них: %+v", f)
	}
	if census.SharedContracts != 1 {
		t.Fatalf("перепись общих контрактов: ожидалась 1, получено %d", census.SharedContracts)
	}
}

// TestInjectionSamePathIsSilent — ЗАКОННЫЙ БЛИЗНЕЦ: тот же контракт по тому же
// пути. Молчание обязано быть, иначе анализатор ловит форму, а не существо.
func TestInjectionSamePathIsSilent(t *testing.T) {
	t.Parallel()
	var log strings.Builder
	findings, census, err := AuditOutsourcedContractParity(contractParityTree(t,
		[]string{injFoundation + "/foundation/subscription"},
		[]string{injFoundation + "/foundation/subscription"},
	), &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("законный близнец объявлен находкой: %s", log.String())
	}
	if census.SharedContracts != 1 {
		t.Fatalf("близнец не осмотрен: общих контрактов %d, ожидалась 1", census.SharedContracts)
	}
}

// TestInjectionVersionSuffixDoesNotSplitTheKey — ЗАКОННЫЙ БЛИЗНЕЦ: версионный
// хвост не разводит один контракт на два ключа, а РАЗНЫЕ корни при одном хвосте
// остаются находкой.
func TestInjectionVersionSuffixDoesNotSplitTheKey(t *testing.T) {
	t.Parallel()
	var log strings.Builder
	findings, _, err := AuditOutsourcedContractParity(contractParityTree(t,
		[]string{injFoundation + "/foundation/quota/v1"},
		[]string{injFoundation + "/platform/cloud/quota/v1"},
	), &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	if len(findings) != 1 || findings[0].Contract != "quota" {
		t.Fatalf("версионный хвост не свёлся к одному ключу: %+v; %s", findings, log.String())
	}

	var log2 strings.Builder
	same, _, err := AuditOutsourcedContractParity(contractParityTree(t,
		[]string{injFoundation + "/foundation/quota/v1"},
		[]string{injFoundation + "/foundation/quota/v1"},
	), &log2)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	if len(same) != 0 {
		t.Fatalf("КОНТРОЛЬ: одинаковый версионный путь объявлен находкой: %s", log2.String())
	}
}

// TestInjectionContractOnlyOneSideIsSilent — ЗАКОННЫЙ БЛИЗНЕЦ: контракт,
// названный ТОЛЬКО одной стороной, разговором не является. Требовать от службы
// импортировать всё, что импортируем мы, значило бы объявить нарушением
// нормальное положение дел.
func TestInjectionContractOnlyOneSideIsSilent(t *testing.T) {
	t.Parallel()
	var log strings.Builder
	findings, census, err := AuditOutsourcedContractParity(contractParityTree(t,
		[]string{injFoundation + "/foundation/subscription", injFoundation + "/foundation/registry"},
		[]string{injFoundation + "/foundation/subscription"},
	), &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("односторонний контракт объявлен находкой: %s", log.String())
	}
	if census.OurContracts != 2 || census.SharedContracts != 1 {
		t.Fatalf("перепись не различила односторонний контракт: наших %d, общих %d",
			census.OurContracts, census.SharedContracts)
	}
}

// TestInjectionImportInCommentIsNotAnImport — ЗАКОННЫЙ БЛИЗНЕЦ: путь, названный
// в КОММЕНТАРИИ, импортом не является. Предикат по слову краснел бы на
// собственном объяснении — вот на этом файле, например.
func TestInjectionImportInCommentIsNotAnImport(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	ourDir, theirDir := filepath.Join(root, "ours"), filepath.Join(root, "theirs")
	writeContractParityFile(t, ourDir, "edge.go", injFoundation+"/foundation/subscription")
	if err := os.MkdirAll(theirDir, 0o755); err != nil {
		t.Fatalf("каталог: %v", err)
	}
	body := "package synthetic\n\n" +
		"// Прежде здесь стоял импорт \"" + injFoundation + "/platform/cloud/subscription\",\n" +
		"// и разбор по слову счёл бы это объявлением.\n" +
		"import _ \"" + injFoundation + "/foundation/subscription\"\n"
	if err := os.WriteFile(filepath.Join(theirDir, "wiring.go"), []byte(body), 0o600); err != nil {
		t.Fatalf("файл: %v", err)
	}

	var log strings.Builder
	findings, _, err := AuditOutsourcedContractParity(OutsourcedContractParityOptions{
		OurRoots:      []string{ourDir},
		Pinned:        map[string]string{"example.com/service": theirDir},
		FoundationAPI: injFoundation,
	}, &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("путь из комментария принят за импорт: %s", log.String())
	}
}

// TestInjectionTestFileIsNotTheWire — ЗАКОННЫЙ БЛИЗНЕЦ: импорт в ПРОБЕ о
// разговоре двух двоичных не утверждает ничего и находкой быть не может.
func TestInjectionTestFileIsNotTheWire(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	ourDir, theirDir := filepath.Join(root, "ours"), filepath.Join(root, "theirs")
	writeContractParityFile(t, ourDir, "edge.go", injFoundation+"/foundation/subscription")
	writeContractParityFile(t, theirDir, "wiring.go", injFoundation+"/foundation/subscription")
	writeContractParityFile(t, theirDir, "legacy_test.go", injFoundation+"/platform/cloud/subscription")

	var log strings.Builder
	findings, _, err := AuditOutsourcedContractParity(OutsourcedContractParityOptions{
		OurRoots:      []string{ourDir},
		Pinned:        map[string]string{"example.com/service": theirDir},
		FoundationAPI: injFoundation,
	}, &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("импорт пробы принят за разговор: %s", log.String())
	}
}

// TestInjectionEmptyWalkIsNotGreen — ПУСТОЙ ОБХОД обязан быть ОТКАЗОМ, а не
// зелёным: «ноль находок» неотличимо от «ноль прочитанного» ровно здесь.
func TestInjectionEmptyWalkIsNotGreen(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	ourDir, theirDir := filepath.Join(root, "ours"), filepath.Join(root, "theirs")
	if err := os.MkdirAll(ourDir, 0o755); err != nil {
		t.Fatalf("каталог: %v", err)
	}
	writeContractParityFile(t, theirDir, "wiring.go", injFoundation+"/platform/cloud/subscription")

	_, census, err := AuditOutsourcedContractParity(OutsourcedContractParityOptions{
		OurRoots:      []string{ourDir},
		Pinned:        map[string]string{"example.com/service": theirDir},
		FoundationAPI: injFoundation,
		LinkedWitness: injFoundation + "/foundation/subscription",
	}, nil)
	if err == nil {
		t.Fatalf("пустой обход дал вердикт вместо отказа; перепись: %+v", census)
	}
	if !strings.Contains(err.Error(), "вердикт беспредметен") {
		t.Fatalf("отказ не назвал причину: %v", err)
	}
}

// TestInjectionWitnessMismatchIsRefused — обход, прочитавший НЕ ТО дерево
// (контракт найден, но по другому пути, чем линкует двоичное), обязан быть
// отказом: иначе он судил бы чужое дерево от нашего имени.
func TestInjectionWitnessMismatchIsRefused(t *testing.T) {
	t.Parallel()
	opts := contractParityTree(t,
		[]string{injFoundation + "/platform/cloud/subscription"},
		[]string{injFoundation + "/platform/cloud/subscription"},
	)
	opts.LinkedWitness = injFoundation + "/foundation/subscription"
	if _, _, err := AuditOutsourcedContractParity(opts, nil); err == nil {
		t.Fatalf("обход не того дерева дал вердикт вместо отказа")
	}
}

// TestInjectionUnreachablePinIsNotAFinding — модуль без скачанного дерева —
// ТРЕТЬЯ КАТЕГОРИЯ, а не находка: о нём не известно ничего, и объявлять его
// нарушителем значит выдавать незнание за вердикт.
func TestInjectionUnreachablePinIsNotAFinding(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	ourDir := filepath.Join(root, "ours")
	writeContractParityFile(t, ourDir, "edge.go", injFoundation+"/foundation/subscription")

	var log strings.Builder
	findings, census, err := AuditOutsourcedContractParity(OutsourcedContractParityOptions{
		OurRoots:      []string{ourDir},
		Pinned:        map[string]string{"example.com/service": ""},
		FoundationAPI: injFoundation,
	}, &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("недоступный пин объявлен находкой: %s", log.String())
	}
	if census.Modules != 1 || census.ModulesReachable != 0 {
		t.Fatalf("перепись не отличила недоступный пин: модулей %d, доступных %d",
			census.Modules, census.ModulesReachable)
	}
}
