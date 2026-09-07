// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: AGPL-3.0-or-later

package platformtree_test

// platformtree_gate_test.go — почему пометка «требует дерева платформы» не
// является маской.
//
// Утверждение, ради которого написан весь пакет, ОДНО: в монорепо ветвь
// пропуска недостижима. Оно проверяется здесь, и из него следует «пропущенных
// проб ноль» — без переписи по каждой пробе и без доверия автору пометки.
//
// Инъекция меняет ровно один факт против своего близнеца: лежит ли модуль в
// каталоге модулей платформы.

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/PRO-Robotech/kaname/internal/testsupport/platformtree"
)

// TestInThisTreeTheSkipBranchIsUnreachableWhenPlatformIsPresent — НЕСУЩЕЕ
// утверждение и единственное, которое говорит о ДЕРЕВЕ ПРОГОНА.
//
// Прогон идёт либо в монорепо, либо в самостоятельном клоне, и оба исхода
// законны — но они обязаны РАЗЛИЧАТЬСЯ и быть НАЗВАННЫМИ. Проба печатает, что
// именно она увидела: молчаливое «зелено» здесь означало бы, что пометка может
// сработать где угодно и никто этого не заметит.
func TestInThisTreeTheSkipBranchIsUnreachableWhenPlatformIsPresent(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: %v", err)
	}

	moduleRoot, err := platformtree.ModuleRootFrom(wd)
	if err != nil {
		t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: корень модуля не установлен: %v", err)
	}

	root, rerr := platformtree.RootFrom(wd)
	switch {
	case rerr == nil:
		t.Logf("посадка: модуль внутри дерева платформы (%s) — ветвь пропуска НЕДОСТИЖИМА, "+
			"помеченных проб пропущено ноль", root)
		// Признак обязан быть непустым: корень, у которого нет каталога модулей,
		// объявил бы платформу там, где её нет.
		if st, serr := os.Stat(filepath.Join(root, "services")); serr != nil || !st.IsDir() {
			t.Fatalf("детектор назвал корнем платформы %s, но каталога модулей в нём нет: %v", root, serr)
		}
	case errors.Is(rerr, platformtree.ErrNoPlatformTree):
		t.Logf("посадка: самостоятельный клон (%s) — помеченные пробы пропускаются С НАЗВАННОЙ "+
			"предпосылкой, и это «условие не создано», а не находка", moduleRoot)
	default:
		t.Fatalf("детектор отказал по причине, не являющейся посадкой: %v", rerr)
	}
}

// --- инъекция в детектор: обе стороны на синтетических деревьях -------------

// mkModule — модуль (каталог с go.mod) по пути rel внутри base.
func mkModule(t *testing.T, base, rel string) string {
	t.Helper()
	dir := filepath.Join(base, filepath.FromSlash(rel))
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestDetector_ModuleAmongPlatformSiblingsIsPlatform — положительный близнец.
func TestDetector_ModuleAmongPlatformSiblingsIsPlatform(t *testing.T) {
	base := t.TempDir()
	mod := mkModule(t, base, "services/iam")

	root, err := platformtree.RootFrom(mod)
	if err != nil {
		t.Fatalf("модуль в каталоге модулей платформы не опознан: %v", err)
	}
	if root != base {
		t.Fatalf("корнем платформы назван %s, ожидался %s", root, base)
	}
}

// TestDetector_StandaloneCloneIsNotPlatform — инъекция. Отличается от близнеца
// выше РОВНО ОДНИМ фактом: модуль лежит не в каталоге модулей платформы.
func TestDetector_StandaloneCloneIsNotPlatform(t *testing.T) {
	base := t.TempDir()
	mod := mkModule(t, base, "kaname-0.1.0")

	_, err := platformtree.RootFrom(mod)
	if !errors.Is(err, platformtree.ErrNoPlatformTree) {
		t.Fatalf("самостоятельный клон принят за дерево платформы: %v", err)
	}
	if err != nil && !containsAll(err.Error(), "ожидался признак", "самостоятельный клон") {
		t.Fatalf("отказ не назвал предпосылку словами: %v", err)
	}
}

// TestDetector_ForeignServicesDirAboveIsNotOurPlatform — вторая инъекция, и она
// про ту самую цену, ради которой резолв ограничен.
//
// Каталог с именем services может оказаться ВЫШЕ по чужому дереву. Детектор,
// ищущий его подъёмом, приписал бы нас к чужой платформе и вынес бы вердикт о
// ней. Здесь модуль лежит НЕ в нём, и ответ обязан быть «не платформа».
func TestDetector_ForeignServicesDirAboveIsNotOurPlatform(t *testing.T) {
	base := t.TempDir()
	if err := os.MkdirAll(filepath.Join(base, "services", "чужой"), 0o750); err != nil {
		t.Fatal(err)
	}
	mod := mkModule(t, base, "где-то/ещё/kaname")

	if _, err := platformtree.RootFrom(mod); !errors.Is(err, platformtree.ErrNoPlatformTree) {
		t.Fatalf("чужой каталог модулей выше по дереву принят за нашу платформу: %v", err)
	}
}

// TestDetector_NoModuleMarkerIsNotRun — третий исход представим отдельно:
// «корня модуля нет» не выдаётся ни за «платформа есть», ни за «её нет».
func TestDetector_NoModuleMarkerIsNotRun(t *testing.T) {
	deep := filepath.Join(t.TempDir(), "а", "б")
	if err := os.MkdirAll(deep, 0o750); err != nil {
		t.Fatal(err)
	}
	_, err := platformtree.RootFrom(deep)
	if !errors.Is(err, platformtree.ErrModuleRootUnknown) {
		t.Fatalf("отсутствие корня модуля выдано за вердикт о посадке: %v", err)
	}
}

// TestModuleDirIn_IsDerivedNotComposed — путь модуля в дереве выводится.
func TestModuleDirIn_IsDerivedNotComposed(t *testing.T) {
	for _, c := range []struct{ root, mod, want string }{
		{"/д", "/д/services/iam", filepath.FromSlash("services/iam")},
		{"/д", "/д", "."},
	} {
		got, err := platformtree.ModuleDirIn(c.root, c.mod)
		if err != nil {
			t.Fatalf("%s → %s: %v", c.root, c.mod, err)
		}
		if got != c.want {
			t.Fatalf("%s в %s: получено %q, ожидалось %q", c.mod, c.root, got, c.want)
		}
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		found := false
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// --- координата модуля НЕ ГНИЁТ: выписанное сверяется с выведенным -----------

// TestModuleCoordinateIsDerivedWhereItCanBeDerived — единственное, что оправдывает
// выписанную координату `services/iam`.
//
// В дереве платформы она ВЫВОДИМА, и здесь выведенное сравнивается с выписанным.
// Разъедутся (модуль переехал, каталог модулей переименован) — красное, а не
// тихий промах мимо каталога. В клоне выводить не из чего, и проба это говорит.
func TestModuleCoordinateIsDerivedWhereItCanBeDerived(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: %v", err)
	}
	moduleRoot, err := platformtree.ModuleRootFrom(wd)
	if err != nil {
		t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: %v", err)
	}
	root, rerr := platformtree.RootFrom(wd)
	if errors.Is(rerr, platformtree.ErrNoPlatformTree) {
		t.Skipf("УСЛОВИЕ НЕ СОЗДАНО (не находка): координату выводить не из чего — "+
			"платформы рядом нет (%s)", moduleRoot)
	}
	if rerr != nil {
		t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: %v", rerr)
	}
	derived, err := platformtree.ModuleDirIn(root, moduleRoot)
	if err != nil {
		t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: %v", err)
	}
	if derived != filepath.FromSlash(platformtree.ModuleDirInPlatform()) {
		t.Fatalf("выписанная координата модуля %q разошлась с выведенной %q — "+
			"пути, собранные по выписанной, промахиваются мимо каталога МОЛЧА",
			platformtree.ModuleDirInPlatform(), derived)
	}
	t.Logf("координата модуля сверена: выписано %q, выведено %q", platformtree.ModuleDirInPlatform(), derived)
}

// --- PathOf: три исхода, каждый со своим близнецом --------------------------

func TestPathOf_InPlatformTreeThePathIsTakenAsWritten(t *testing.T) {
	base := t.TempDir()
	mod := mkModule(t, base, "services/iam")

	got, err := platformtree.PathOf(mod, "services/iam/internal/migrations")
	if err != nil {
		t.Fatalf("путь в дереве платформы не разрешён: %v", err)
	}
	if want := filepath.Join(base, "services", "iam", "internal", "migrations"); got != want {
		t.Fatalf("получено %s, ожидалось %s", got, want)
	}
}

// TestPathOf_InCloneThePrefixIsStripped — инъекция посадкой: тот же путь, но
// модуль стоит клоном. Отличается от близнеца выше РОВНО ОДНИМ фактом.
func TestPathOf_InCloneThePrefixIsStripped(t *testing.T) {
	base := t.TempDir()
	mod := mkModule(t, base, "kaname")

	got, err := platformtree.PathOf(mod, "services/iam/internal/migrations")
	if err != nil {
		t.Fatalf("путь внутри модуля не разрешён в клоне: %v", err)
	}
	if want := filepath.Join(mod, "internal", "migrations"); got != want {
		t.Fatalf("получено %s, ожидалось %s", got, want)
	}
}

// TestPathOf_InCloneAPathOutsideTheModuleIsNotAMiss — третий исход, и он несущий:
// то, что в поставку не входит, обязано давать «условие не создано», а не путь,
// которого нет.
func TestPathOf_InCloneAPathOutsideTheModuleIsNotAMiss(t *testing.T) {
	base := t.TempDir()
	mod := mkModule(t, base, "kaname")

	for _, rel := range []string{
		"proto/kaname/cloud/iam/v1/fga_model.fga",
		"services/vpc/manifest.yaml",
		"deploy/helm/umbrella",
	} {
		if _, err := platformtree.PathOf(mod, rel); !errors.Is(err, platformtree.ErrNoPlatformTree) {
			t.Fatalf("%s: свойство поставки выдано за отсутствие файла: %v", rel, err)
		}
	}
}

// TestPathOf_TheSamePathsResolveInThePlatformTree — законный близнец предыдущей:
// те же пути в дереве платформы разрешаются, а не отвергаются. Без него отказ
// выше зеленел бы и на исправном дереве.
func TestPathOf_TheSamePathsResolveInThePlatformTree(t *testing.T) {
	base := t.TempDir()
	mod := mkModule(t, base, "services/iam")

	for _, rel := range []string{
		"proto/kaname/cloud/iam/v1/fga_model.fga",
		"services/vpc/manifest.yaml",
		"deploy/helm/umbrella",
	} {
		if _, err := platformtree.PathOf(mod, rel); err != nil {
			t.Fatalf("%s: путь дерева платформы отвергнут в дереве платформы: %v", rel, err)
		}
	}
}

// TestPathOf_ModuleRootItselfIsAddressable — координата самого модуля.
func TestPathOf_ModuleRootItselfIsAddressable(t *testing.T) {
	base := t.TempDir()
	mod := mkModule(t, base, "kaname")

	got, err := platformtree.PathOf(mod, "services/iam")
	if err != nil {
		t.Fatalf("корень модуля не адресуем: %v", err)
	}
	if got != mod {
		t.Fatalf("получено %s, ожидалось %s", got, mod)
	}
}

// TestPathOf_EscapingRelIsRefused — путь, выходящий вверх, координатой не является.
func TestPathOf_EscapingRelIsRefused(t *testing.T) {
	base := t.TempDir()
	mod := mkModule(t, base, "kaname")

	if _, err := platformtree.PathOf(mod, "../соседнее/дерево"); !errors.Is(err, platformtree.ErrModuleRootUnknown) {
		t.Fatalf("путь вверх принят за координату дерева: %v", err)
	}
}
