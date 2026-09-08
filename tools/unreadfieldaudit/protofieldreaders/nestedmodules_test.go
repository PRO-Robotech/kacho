// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package protofieldreaders_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/tools/unreadfieldaudit/protofieldreaders"
)

var reModuleLine = regexp.MustCompile(`(?m)^module\s+(\S+)`)

// TestIndexWalksEveryModuleUnderTheWalkedTrees — прод-код, живущий во ВЛОЖЕННОМ
// Go-модуле, обязан быть в индексе.
//
// ПРЕДМЕТ — форма записи читателя, о которой распознаватель не знал. Индекс
// перечисляет пакеты одним `go list` в корне репозитория, а `go list` в модуль,
// у которого есть СВОЙ `go.mod`, не спускается by construction. Значит всякое
// чтение поля, записанное в таком модуле, оказывается не «непрочитанным» и не
// «находкой», а НЕВИДИМЫМ: предикат «поле публичного запроса имеет читателя»
// молчит о нём в обе стороны.
//
// Молчание при этом не остаётся молчанием: у типа запроса пропадает и
// обработчик, поэтому весь его домен уезжает в полосу «RPC-НЕ-РЕАЛИЗОВАН» —
// корзину, которая по построению ничего не утверждает. Со стороны это выглядит
// как «не находка», то есть как исправное дерево.
//
// Контроль в ОБЕ стороны:
//
//	(1) КРАСНОЕ  — модуль, чей `go.mod` лежит ПОД обойдённым деревом, обязан быть
//	    в индексе своими пакетами (перечень модулей выводится из дерева, а не
//	    выписывается: следующий вынесенный сервис унаследовал бы ту же немоту);
//	(2) МОЛЧАНИЕ — модуль, чей `go.mod` лежит ВНЕ обойдённого дерева, в индекс не
//	    попадает. Без этой половины «обойти всё» было бы неотличимо от «обойти
//	    заведомо лишнее», и обход стоил бы компиляции всего репозитория.
func TestIndexWalksEveryModuleUnderTheWalkedTrees(t *testing.T) {
	t.Chdir("../../..")

	nested := nestedModules(t, "services")
	if len(nested) == 0 {
		t.Skip("под services/ нет ни одного вложенного модуля — предмета различения " +
			"в дереве нет, и тест ничего не утверждал бы. Появится вынесенный сервис — " +
			"утверждение вернётся само")
	}

	ix, err := protofieldreaders.Build("./services/...")
	if err != nil {
		t.Fatalf("индекс не построился: %v", err)
	}
	if len(ix.Errors) > 0 {
		t.Fatalf("предпосылка не выполнена — %d пакетов не протипизировано: %s",
			len(ix.Errors), strings.Join(ix.Errors, "; "))
	}
	if n := ix.FileCount(); n == 0 {
		t.Fatal("прочитано 0 файлов — обход не состоялся, тест ничего не утверждает")
	}

	seen := map[string]int{}
	for _, p := range ix.Packages {
		for path := range nested {
			if p.Path == path || strings.HasPrefix(p.Path, path+"/") {
				seen[path]++
			}
		}
	}
	// (1) положительная сторона.
	for path, dir := range nested {
		if seen[path] == 0 {
			t.Errorf("модуль %s (%s) лежит под обойденным деревом ./services/..., но в "+
				"индексе нет ни одного его пакета: чтения полей запроса, записанные в "+
				"нём, НЕВИДИМЫ — ни находка, ни закрытие", path, dir)
		}
	}
	// (3) перепись обязана называть модули порознь, иначе «обошли оба дерева»
	// неотличимо от «обошли одно».
	if len(ix.Modules) < 1+len(nested) {
		t.Errorf("перепись индекса называет %d модулей, а обойдено должно быть %d "+
			"(корневой + %d вложенных): объём осмотренного печатается порознь, иначе "+
			"пустой обход одного из деревьев не отличить от полного",
			len(ix.Modules), 1+len(nested), len(nested))
	}

	// (2) законный близнец: то же построение на дереве, ПОД которым вложенных
	// модулей нет, их пакетов не приносит.
	other, err := protofieldreaders.Build("./gateway/...")
	if err != nil {
		t.Fatalf("индекс ./gateway/... не построился: %v", err)
	}
	for _, p := range other.Packages {
		for path, dir := range nested {
			if p.Path == path || strings.HasPrefix(p.Path, path+"/") {
				t.Errorf("обход ./gateway/... принёс пакет %s вложенного модуля %s (%s) — "+
					"обход шире запрошенного дерева", p.Path, path, dir)
			}
		}
	}
	t.Logf("вложенных модулей под services/: %d; их пакетов в индексе: %v; "+
		"модулей в переписи: %d", len(nested), seen, len(ix.Modules))
}

// nestedModules — {путь модуля: каталог} для каждого `go.mod` СТРОГО под dir.
//
// Выводится из дерева, а не выписывается: рукописный перечень разошёлся бы с
// деревом молча — ровно так и завелась слепая зона, которую тест стережёт.
func nestedModules(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			switch info.Name() {
			case ".git", "node_modules", "vendor", "testdata":
				return filepath.SkipDir
			}
			return nil
		}
		if info.Name() != "go.mod" {
			return nil
		}
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		m := reModuleLine.FindSubmatch(b)
		if m == nil {
			t.Fatalf("%s не объявляет module — предпосылка обхода не выполнена", p)
		}
		out[string(m[1])] = filepath.Dir(p)
		return nil
	})
	if err != nil {
		t.Fatalf("обход %s: %v", dir, err)
	}
	return out
}
