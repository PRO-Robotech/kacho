// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// pkgvardecl_injection_test.go — ДОКАЗАТЕЛЬСТВО того, что разрешение объявления
// по пакету находит предмет там, где он есть, и отказывает там, где его нет.
//
// Стенд синтетический: настоящий пакет нельзя ни сломать, ни вернуть, а вердикт
// о нём о способности отказывать не говорит ничего.
//
// Инъекции вносятся ПО ОДНОЙ, и к каждой приложен законный близнец той же формы,
// обязанный молчать. Без близнеца «отказал» доказывало бы лишь то, что
// разрешение отказывает всегда.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pkgDeclStand — синтетический пакет из ДВУХ не-тестовых файлов и одного
// тестового. Объявление лежит во втором файле: именно эту раскладку прежний
// разрешатель по имени файла и не переживал.
type pkgDeclStand struct{ root string }

func newPkgDeclStand(t *testing.T) *pkgDeclStand {
	t.Helper()
	s := &pkgDeclStand{root: t.TempDir()}
	// Файл, где объявление ЖИЛО РАНЬШЕ: осталась только проза, называющая имя.
	// Разрешатель, читающий текст вместо узлов, нашёл бы «объявление» здесь.
	s.write(t, "svc/authzmap/fga_types.go", `package authzmap

// objectTypes переехал в порождённый файл; здесь остался разбор переноса.
// var objectTypes = map[string]string{"ghost.one": "ghost_one"}
func unrelated() {}
`)
	// Файл, где объявление ЛЕЖИТ СЕЙЧАС.
	s.write(t, "svc/authzmap/tables_gen.go", `package authzmap

var objectTypes = map[string]string{
	"alpha.one": "alpha_one",
	"beta.one":  "beta_one",
}
`)
	// Тестовый файл пакета держит СВОЙ литерал того же имени. Он не читается —
	// иначе предмет стал бы функцией числа проб, а здесь ещё и объявлений
	// оказалось бы два.
	s.write(t, "svc/authzmap/tables_gen_test.go", `package authzmap

var objectTypes = map[string]string{"synthetic.one": "synthetic_one"}
`)
	return s
}

func (s *pkgDeclStand) write(t *testing.T, rel, body string) {
	t.Helper()
	p := filepath.Join(s.root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestPkgVarDecl_FindsTheDeclarationWhereverInThePackageItLives — КОНТРОЛЬ и
// одновременно предмет задачи #1944: объявление найдено во втором файле пакета.
func TestPkgVarDecl_FindsTheDeclarationWhereverInThePackageItLives(t *testing.T) {
	s := newPkgDeclStand(t)
	lit, census, err := findPackageVarLiteral(
		clientTruthSyntheticTree(t, s.root), "svc/authzmap", "objectTypes")
	if err != nil {
		t.Fatalf("объявление лежит в пакете, а разрешение отказало: %v", err)
	}
	if census.DeclFile != "svc/authzmap/tables_gen.go" {
		t.Errorf("объявление найдено в %q, ожидался порождённый файл", census.DeclFile)
	}
	// Объём осмотренного: ДВА не-тестовых файла, тестовый не читан.
	if census.PkgFiles != 2 {
		t.Errorf("файлов пакета прочитано %d, ожидалось 2 (тестовый не читается)", census.PkgFiles)
	}
	keys := pkgVarLiteralStringKeys(lit)
	if len(keys) != 2 || keys[0] != "alpha.one" || keys[1] != "beta.one" {
		t.Errorf("ключи прочитаны как %v — взят не тот литерал", keys)
	}
}

// TestPkgVarDecl_RefusesWhenTheDeclarationIsGone — инъекция: объявление снято из
// пакета целиком. Проза о нём в соседнем файле остаётся, и разрешатель по тексту
// принял бы её за объявление.
func TestPkgVarDecl_RefusesWhenTheDeclarationIsGone(t *testing.T) {
	s := newPkgDeclStand(t)
	if err := os.Remove(filepath.Join(s.root, "svc/authzmap/tables_gen.go")); err != nil {
		t.Fatal(err)
	}
	_, census, err := findPackageVarLiteral(
		clientTruthSyntheticTree(t, s.root), "svc/authzmap", "objectTypes")
	if err == nil {
		t.Fatal("объявления в пакете нет, а разрешение вернуло успех — " +
			"проза о нём принята за него самого")
	}
	// Отказ обязан назвать объём прочитанного: иначе «не найдено» неотличимо от
	// «не читано».
	if !strings.Contains(err.Error(), "прочитано 1") {
		t.Errorf("отказ не называет объём осмотренного: %v", err)
	}
	if census.PkgFiles != 1 {
		t.Errorf("файлов прочитано %d, ожидался 1", census.PkgFiles)
	}
}

// TestPkgVarDecl_RefusesWhenTheNameWasRenamed — вторая половина той же премисы:
// пакет на месте, имя другое. Молчаливый ноль означал бы гейт, переживший
// переименование своего предмета.
func TestPkgVarDecl_RefusesWhenTheNameWasRenamed(t *testing.T) {
	s := newPkgDeclStand(t)
	if _, _, err := findPackageVarLiteral(
		clientTruthSyntheticTree(t, s.root), "svc/authzmap", "renamedTypes"); err == nil {
		t.Fatal("объявления с таким именем нет, а разрешение вернуло успех")
	}
}

// TestPkgVarDecl_RefusesOnAnEmptyPackage — обход пуст: каталога нет вовсе.
func TestPkgVarDecl_RefusesOnAnEmptyPackage(t *testing.T) {
	s := newPkgDeclStand(t)
	if _, _, err := findPackageVarLiteral(
		clientTruthSyntheticTree(t, s.root), "svc/nosuchpkg", "objectTypes"); err == nil {
		t.Fatal("пакета нет, а разрешение вернуло успех — пустой обход неотличим от находки")
	}
}

// TestPkgVarDecl_RefusesOnTwoDeclarations — два места об одном предмете: каталог
// несёт файл ВТОРОГО пакета (`_test`-суффикс имени пакета — законная форма Go),
// и в нём то же имя. Взять первое молча значило бы вынести вердикт о половине.
func TestPkgVarDecl_RefusesOnTwoDeclarations(t *testing.T) {
	s := newPkgDeclStand(t)
	s.write(t, "svc/authzmap/fga_types.go", `package authzmap

var objectTypes = map[string]string{"gamma.one": "gamma_one"}
`)
	_, _, err := findPackageVarLiteral(
		clientTruthSyntheticTree(t, s.root), "svc/authzmap", "objectTypes")
	if err == nil {
		t.Fatal("объявлений два, а разрешение вернуло успех")
	}
	for _, want := range []string{"fga_types.go", "tables_gen.go"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("отказ не называет оба места (%s): %v", want, err)
		}
	}
}

// TestPkgVarDecl_IgnoresNestedPackages — законный близнец пустого обхода:
// вложенный каталог — ДРУГОЙ пакет, и его объявление здешним не считается.
// Без этой пробы разрешение по каталогу тихо расширилось бы на поддерево.
func TestPkgVarDecl_IgnoresNestedPackages(t *testing.T) {
	s := newPkgDeclStand(t)
	s.write(t, "svc/authzmap/nested/other.go", `package nested

var objectTypes = map[string]string{"nested.one": "nested_one"}
`)
	lit, census, err := findPackageVarLiteral(
		clientTruthSyntheticTree(t, s.root), "svc/authzmap", "objectTypes")
	if err != nil {
		t.Fatalf("вложенный пакет не должен мешать разрешению: %v", err)
	}
	if census.PkgFiles != 2 {
		t.Errorf("файлов прочитано %d — обход спустился во вложенный пакет", census.PkgFiles)
	}
	if keys := pkgVarLiteralStringKeys(lit); len(keys) != 2 {
		t.Errorf("ключей %d, ожидалось 2 — взят литерал вложенного пакета", len(keys))
	}
}
