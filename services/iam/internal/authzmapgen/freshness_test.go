// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package authzmapgen_test

// freshness_test.go — ПОРОЖДЁННЫЙ ФАЙЛ СВЕЖ, а перепись в нём не зависит от
// дерева вне манифестов (задача #1092).
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ЗДЕСЬ СУДИТСЯ
//
// Вывод снял второе место об одном предмете, но завёл СВОЁ: производителя и его
// продукт. Продукт, отставший от производителя, есть ровно тот класс, ради
// снятия которого вывод и заводился, — только теперь расхождение прячется за
// словом «сгенерировано» и выглядит доверенным.
//
// Поэтому сверка ПОБАЙТОВАЯ: таблица, совпавшая по составу и разошедшаяся
// отступом, всё равно означает, что файл писал не этот производитель.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ВТОРАЯ ПРОБА ПРО ПЕРЕПИСЬ — НЕ ПРИДИРКА
//
// Перепись, вошедшая в файл, становится частью сверяемых байт. Возьми она
// величину, зависящую от всего дерева (сколько путей осмотрел обход), — и гейт
// краснел бы на всякой правке где угодно, называя предметом таблицы. Прокси,
// ломающийся раньше того, что он стережёт, снимают как непонятный, а вместе с
// ним уходит и настоящая проверка.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/services/iam/internal/authzmapgen"
)

// repoRoot — корень репозитория от каталога этого пакета.
const repoRoot = "../../../.."

func renderTree(t *testing.T, root string) ([]byte, authzmapgen.Census) {
	t.Helper()
	tables, err := authzmapgen.Collect(root)
	if err != nil {
		t.Fatalf("обход манифестов %s не состоялся (%v) — предпосылка гейта исчезла, "+
			"а не дерево стало чистым", root, err)
	}
	body, err := authzmapgen.Render(tables)
	if err != nil {
		t.Fatalf("рендер: %v", err)
	}
	return body, tables.Census
}

// TestGeneratedTablesAreFresh — файл в дереве побайтово равен тому, что даёт
// производитель СЕГОДНЯ.
func TestGeneratedTablesAreFresh(t *testing.T) {
	census, err := authzmapgen.CheckFresh(repoRoot)
	t.Logf("осмотрено: %s", census.Summary())
	if census.Resources == 0 {
		t.Fatal("ресурсов ноль — «файл свеж» здесь означало бы «сверять было нечего»")
	}
	if err != nil {
		t.Fatal(err)
	}
}

// TestGeneratedCensusDoesNotDependOnTheRestOfTheTree — посторонний файл в дереве
// НЕ меняет ни одного байта продукта.
//
// Законный близнец к пробе выше: без него «файл свеж» было бы неотличимо от
// «файл свеж, пока никто не тронул дерево», и первый же не относящийся к делу
// коммит покрасил бы гейт.
func TestGeneratedCensusDoesNotDependOnTheRestOfTheTree(t *testing.T) {
	before, _ := renderTree(t, repoRoot)

	decoy := filepath.Join(repoRoot, "services", "iam",
		"zz_authzmapgen_decoy_"+t.Name()+".txt")
	if err := os.WriteFile(decoy, []byte("посторонний файл: манифестом не является\n"), 0o600); err != nil {
		t.Fatalf("подложить посторонний файл не удалось: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(decoy) })

	after, census := renderTree(t, repoRoot)
	if string(before) != string(after) {
		t.Fatalf("посторонний файл изменил продукт — перепись в нём считает дерево, а не "+
			"манифесты; гейт свежести краснел бы на правке, к таблицам отношения не имеющей "+
			"(%s)", census.Summary())
	}
	if !strings.Contains(string(after), "// Перепись производителя:") {
		t.Fatal("продукт не несёт переписи вовсе — «порождено из шести манифестов» стало бы " +
			"неотличимо от «порождено из одного»")
	}
}
