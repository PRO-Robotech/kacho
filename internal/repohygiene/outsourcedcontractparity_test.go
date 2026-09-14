// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// outsourcedcontractparity_test.go — вердикт о НАСТОЯЩЕМ дереве.
//
// Способность падать доказывает не этот прогон, а инъекция
// (`outsourcedcontractparity_injection_test.go`, восемь утверждений, у каждой
// оси законный близнец): здесь только вердикт.
package repohygiene

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	subscriptionv1 "github.com/PRO-Robotech/corelib/api/corelib/subscription"
)

// productModulePrefix — корень путей модулей продукта.
const productModulePrefix = "github.com/PRO-Robotech/"

// contractParityRoot — корень дерева: каталог с `go.mod` корневого модуля.
func contractParityRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("рабочий каталог: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("корень дерева не найден от %s", dir)
		}
		dir = parent
	}
}

// productModulePins — модули продукта, пиненные ЭТИМ деревом.
//
// Читается текстом закоммиченного `go.mod`: ответ уже лежит в дереве, и сеть
// предикату не нужна.
func productModulePins(t *testing.T, root string) []string {
	t.Helper()
	f, err := os.Open(filepath.Join(root, "go.mod")) // #nosec G304 -- корень выведен обходом вверх
	if err != nil {
		t.Fatalf("чтение go.mod: %v", err)
	}
	defer func() { _ = f.Close() }()

	var out []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 || !strings.HasPrefix(fields[0], productModulePrefix) {
			continue
		}
		out = append(out, fields[0])
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("разбор go.mod: %v", err)
	}
	return out
}

// TestOutsourcedContractsAgreeWithTheEdge — контракт, по которому это дерево
// говорит с вынесенной службой, обязан лежать у обеих сторон по ОДНОМУ пути.
func TestOutsourcedContractsAgreeWithTheEdge(t *testing.T) {
	t.Parallel()
	root := contractParityRoot(t)

	// Свидетель берётся У СТАБА, а не выписывается строкой: выписанный разошёлся
	// бы с двоичным молча, и разошёлся бы ровно в день переезда контракта — то
	// есть тогда, когда гейт и нужен.
	witness := reflect.TypeOf(subscriptionv1.SubscriptionRequest{}).PkgPath()
	if witness == "" {
		t.Fatalf("путь пакета контракта не читается — свидетеля нет, вердикт беспредметен")
	}

	// Модуль фундамента ВЫВОДИТСЯ: это тот пиненный модуль продукта, чей путь
	// является приставкой свидетеля. Выписать его значило бы завести второе
	// место об одном предмете.
	modules := productModulePins(t, root)
	foundation := ""
	var pinned []string
	for _, m := range modules {
		if strings.HasPrefix(witness, m+"/") {
			foundation = m
			continue
		}
		pinned = append(pinned, m)
	}
	if foundation == "" {
		t.Fatalf("модуль фундамента не выведен: свидетель %q не лежит ни в одном из пиненных модулей %v",
			witness, modules)
	}

	// Каталоги пиненных модулей спрашиваются у сборщика, а не собираются из
	// GOMODCACHE и правил экранирования пути: у сборщика ответ авторитетный.
	dirs := map[string]string{}
	for _, m := range pinned {
		cmd := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", m) // #nosec G204 -- путь модуля прочитан из go.mod этого дерева
		cmd.Dir = root
		out, err := cmd.Output()
		if err != nil {
			// Модуль не скачан — ТРЕТЬЯ КАТЕГОРИЯ: о нём не известно ничего.
			// Объявить его нарушителем значило бы выдать незнание за вердикт.
			dirs[m] = ""
			continue
		}
		dirs[m] = strings.TrimSpace(string(out))
	}

	var log strings.Builder
	findings, census, err := AuditOutsourcedContractParity(OutsourcedContractParityOptions{
		OurRoots:      []string{root},
		Pinned:        dirs,
		FoundationAPI: foundation + "/api",
		LinkedWitness: witness,
	}, &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	t.Log(strings.TrimSpace(log.String()))

	// ПРЕМИСА обхода. Свидетель анализатор уже проверил сам; здесь остаётся то,
	// что обход обязан произвести на КАЖДОЙ стороне, — иначе «ноль находок»
	// достижимо пустым обходом.
	switch {
	case census.Modules == 0:
		t.Fatalf("пиненных модулей продукта ноль — перечень не выведен, вердикт беспредметен")
	case census.OurContracts < 2:
		t.Fatalf("контрактов фундамента у нас %d — обход прочитал не то дерево", census.OurContracts)
	case census.ModulesReachable == 0:
		// ОТКАЗ, а не пропуск. Дерево пина достаётся сборщиком и в этом дереве
		// доступно by construction — его требует сама сборка. Ноль доступных
		// означает, что обход не состоялся, и «ноль находок» тогда неотличимо
		// от «ноль прочитанного»; пропуск здесь молча выдал бы незнание за
		// исправность.
		t.Fatalf("ни одно дерево пина не досталось (модулей %d) — вердикт беспредметен: "+
			"о согласии контрактов не известно ничего", census.Modules)
	case census.SharedContracts == 0:
		t.Fatalf("общих контрактов ноль при %d наших и %d импортах у них — "+
			"сопоставление не состоялось, вердикт беспредметен",
			census.OurContracts, census.TheirImports)
	}

	for _, f := range findings {
		t.Errorf("контракт %q разошёлся между деревьями: мы линкуем %q, %s линкует %q (%s).\n"+
			"  Два двоичных, собранных от РАЗНЫХ версий фундамента, говорят разными именами: "+
			"вызов уходит по нашему имени, на сервере его нет, gRPC отвечает Unimplemented.\n"+
			"  Снимается ВЫПУСКОМ вынесенной части от ревизии, где контракт уже переехал, "+
			"и подъёмом ОБОИХ её пинов — модуля и образа.",
			f.Contract, f.Ours, f.Module, f.Theirs, f.Coord)
	}
}
