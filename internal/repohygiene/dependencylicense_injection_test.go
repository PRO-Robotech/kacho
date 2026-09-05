// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// dependencylicense_injection_test.go — доказательство, что гейт лицензий
// СПОСОБЕН упасть и СПОСОБЕН смолчать.
//
// Инъекция идёт по синтетическому кэшу в `t.TempDir()` и по синтетическому
// тексту go.mod: она не трогает ни дерево, ни настоящий кэш модулей, поэтому
// роняет ТОЛЬКО проверяемое. Каждая пара — дефект и ЗАКОННЫЙ БЛИЗНЕЦ той же
// формы, различающиеся ОДНИМ фактом: иначе красное могло бы прийти от соседа.

// fakeModuleCache — синтетический кэш: каталог модуля и файлы в его корне.
// Возвращает путь кэша, годный для DiskLicenseProbe.
func fakeModuleCache(t *testing.T, mods map[DirectDependency]map[string]string) string {
	t.Helper()
	cache := t.TempDir()
	for dep, files := range mods {
		dir := ModuleCacheDir(cache, dep)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
		for name, body := range files {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
				t.Fatalf("write %s: %v", name, err)
			}
		}
	}
	return cache
}

var (
	// depUnlicensed — модуль БЕЗ лицензии. Имя намеренно постороннее: гейт судит
	// отсутствие лицензии, а не членство имени в перечне.
	depUnlicensed = DirectDependency{Path: "example.com/Some-Vendor/toolkit", Version: "v1.2.3", Line: 7}
	// depLicensed — ЗАКОННЫЙ БЛИЗНЕЦ той же формы: тот же вид пути, та же
	// заглавная буква в пути, та же версия — различие ровно одно, файл лицензии.
	depLicensed = DirectDependency{Path: "example.com/Other-Vendor/toolkit", Version: "v1.2.3", Line: 8}
)

// ---- ось 1: файл лицензии ---------------------------------------------------

func TestDependencyLicenseGate_RedsOnAModuleWithoutALicense(t *testing.T) {
	cache := fakeModuleCache(t, map[DirectDependency]map[string]string{
		depUnlicensed: {"go.mod": "module example.com/Some-Vendor/toolkit\n", "README.md": "# toolkit\n"},
	})
	findings, census := ScanDependencyLicenses([]DirectDependency{depUnlicensed}, DiskLicenseProbe(cache))
	if len(findings) != 1 {
		t.Fatalf("находок %d, ожидалась 1\n%s", len(findings), census)
	}
	got := findings[0].String()
	for _, want := range []string{"go.mod:7", depUnlicensed.Path, depUnlicensed.Version} {
		if !strings.Contains(got, want) {
			t.Fatalf("находка не называет координату %q: %s", want, got)
		}
	}
	if census.Resolved != 1 || census.Licensed != 0 {
		t.Fatalf("перепись не сходится: %s", census)
	}
}

func TestDependencyLicenseGate_SilentOnTheLegalTwinWithALicenseFile(t *testing.T) {
	cache := fakeModuleCache(t, map[DirectDependency]map[string]string{
		depLicensed: {"go.mod": "module example.com/Other-Vendor/toolkit\n", "README.md": "# toolkit\n", "LICENSE": "MIT\n"},
	})
	findings, census := ScanDependencyLicenses([]DirectDependency{depLicensed}, DiskLicenseProbe(cache))
	if len(findings) != 0 {
		t.Fatalf("законный близнец объявлен находкой: %v\n%s", findings, census)
	}
	if census.Licensed != 1 {
		t.Fatalf("перепись не засчитала лицензию: %s", census)
	}
}

// Пара выше различается ОДНИМ фактом. Проба ниже утверждает это дословно:
// один и тот же модуль краснеет без файла и молчит с ним.
func TestDependencyLicenseGate_TheOnlyDifferenceIsTheLicenseFile(t *testing.T) {
	files := map[string]string{"go.mod": "module m\n", "README.md": "# m\n"}
	cache := fakeModuleCache(t, map[DirectDependency]map[string]string{depUnlicensed: files})
	if findings, c := ScanDependencyLicenses([]DirectDependency{depUnlicensed}, DiskLicenseProbe(cache)); len(findings) != 1 {
		t.Fatalf("без файла лицензии находок %d, ожидалась 1\n%s", len(findings), c)
	}
	dir := ModuleCacheDir(cache, depUnlicensed)
	if err := os.WriteFile(filepath.Join(dir, "LICENSE.md"), []byte("Apache-2.0\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if findings, c := ScanDependencyLicenses([]DirectDependency{depUnlicensed}, DiskLicenseProbe(cache)); len(findings) != 0 {
		t.Fatalf("с файлом лицензии всё ещё находка: %v\n%s", findings, c)
	}
}

// Основы имени — все четыре, каждая своей пробой: иначе распознаватель мог бы
// знать одну форму записи предмета и молчать о трёх остальных.
func TestDependencyLicenseGate_KnowsEveryConventionalFileName(t *testing.T) {
	for _, name := range []string{"LICENSE", "LICENSE.txt", "LICENCE", "COPYING", "COPYING.LESSER", "NOTICE", "license", "License-MIT"} {
		t.Run(name, func(t *testing.T) {
			cache := fakeModuleCache(t, map[DirectDependency]map[string]string{
				depLicensed: {"go.mod": "module m\n", name: "текст лицензии\n"},
			})
			findings, c := ScanDependencyLicenses([]DirectDependency{depLicensed}, DiskLicenseProbe(cache))
			if len(findings) != 0 {
				t.Fatalf("%s не признан лицензией: %v\n%s", name, findings, c)
			}
		})
	}
}

// ---- ось 2: заголовок SPDX вместо файла ------------------------------------

func TestDependencyLicenseGate_SilentOnAModuleDeclaringItsLicenseBySpdxHeader(t *testing.T) {
	cache := fakeModuleCache(t, map[DirectDependency]map[string]string{
		depLicensed: {"go.mod": "module m\n", "doc.go": "// " + spdxTag + ": MIT\npackage m\n"},
	})
	findings, census := ScanDependencyLicenses([]DirectDependency{depLicensed}, DiskLicenseProbe(cache))
	if len(findings) != 0 {
		t.Fatalf("модуль с заголовком SPDX объявлен находкой: %v\n%s", findings, census)
	}
	if census.Licensed != 1 {
		t.Fatalf("перепись не засчитала заголовок: %s", census)
	}
}

// Законный близнец предыдущей: тот же корневой .go, но БЕЗ заголовка. Без этой
// пробы «молчит на SPDX» было бы неотличимо от «молчит на любом .go в корне».
func TestDependencyLicenseGate_RedsOnTheSameFileWithoutTheSpdxHeader(t *testing.T) {
	cache := fakeModuleCache(t, map[DirectDependency]map[string]string{
		depUnlicensed: {"go.mod": "module m\n", "doc.go": "// пакет без объявления лицензии\npackage m\n"},
	})
	findings, census := ScanDependencyLicenses([]DirectDependency{depUnlicensed}, DiskLicenseProbe(cache))
	if len(findings) != 1 {
		t.Fatalf("находок %d, ожидалась 1\n%s", len(findings), census)
	}
}

// ---- ось 3: непрочитанный каталог НЕ засчитывается в проверенные ------------

func TestDependencyLicenseGate_UnresolvedIsNeitherFindingNorPass(t *testing.T) {
	cache := t.TempDir() // каталога модуля нет вовсе
	findings, census := ScanDependencyLicenses([]DirectDependency{depUnlicensed}, DiskLicenseProbe(cache))
	if len(findings) != 0 {
		t.Fatalf("непрочитанный каталог объявлен находкой: %v", findings)
	}
	if census.Licensed != 0 || census.Resolved != 0 {
		t.Fatalf("непрочитанный каталог засчитан проверенным: %s", census)
	}
	if len(census.Unresolved) != 1 || !strings.Contains(census.String(), depUnlicensed.Path) {
		t.Fatalf("перепись не назвала непрочитанный модуль по имени: %s", census)
	}
}

// ---- ось 4: разбор go.mod --------------------------------------------------

func TestDependencyLicenseGate_ParsesBothRequireForms(t *testing.T) {
	body := "module example.com/tree\n\ngo 1.25\n\n" +
		"require example.com/single v0.1.0\n\n" +
		"require (\n\texample.com/block v0.2.0\n\texample.com/indirect v0.3.0 // indirect\n)\n"
	deps := ParseGoModRequires(body)
	if len(deps) != 3 {
		t.Fatalf("разобрано %d записей, ожидалось 3: %+v", len(deps), deps)
	}
	byPath := map[string]DirectDependency{}
	for _, d := range deps {
		byPath[d.Path] = d
	}
	// Косвенная запись разбирается наравне с прямой: распространяется всё, что
	// дерево закрепляет, — она приезжает в клон и в образ так же.
	if _, ok := byPath["example.com/indirect"]; !ok {
		t.Fatal("косвенная запись потеряна разбором — она распространяется наравне с прямой")
	}
	if got := byPath["example.com/single"].Line; got != 5 {
		t.Fatalf("однострочная форма: строка %d, ожидалась 5", got)
	}
	if got := byPath["example.com/block"].Version; got != "v0.2.0" {
		t.Fatalf("блочная форма: версия %q", got)
	}
}

func TestDependencyLicenseGate_EmptyParseIsAnEmptyTraversal(t *testing.T) {
	deps := ParseGoModRequires("module example.com/tree\n\ngo 1.25\n")
	if len(deps) != 0 {
		t.Fatalf("на go.mod без require разобрано %d записей", len(deps))
	}
	_, census := ScanDependencyLicenses(deps, DiskLicenseProbe(t.TempDir()))
	if census.Requires != 0 {
		t.Fatalf("перепись не объявила обход пустым: %s", census)
	}
	// Гейт на таком обходе обязан падать — это его строка `t.Fatal` про пустой
	// обход; здесь утверждается ВХОД в неё, чтобы «находок 0» не читалось как
	// «прочитано 0».
}

// ---- ось 5: кодировка пути модуля ------------------------------------------

func TestDependencyLicenseGate_EscapesUppercaseInTheModulePath(t *testing.T) {
	// Путь синтетический намеренно: имя снятого модуля, оставленное здесь
	// «просто как данные пробы», пережило бы свой предмет.
	if got := EscapeModulePath("example.com/H-BF/ToolKit"); got != "example.com/!h-!b!f/!tool!kit" {
		t.Fatalf("кодировка пути: %q", got)
	}
	if got := EscapeModulePath("golang.org/x/sync"); got != "golang.org/x/sync" {
		t.Fatalf("путь без заглавных изменён: %q", got)
	}
}

// ---- ось 6: НАСТОЯЩЕЕ дерево, обе стороны ----------------------------------

// chainProbe — свидетельство из НЕСКОЛЬКИХ источников: первый, который смог
// прочитать каталог, и отвечает. Нужен ровно для инъекции по настоящему дереву:
// подставной модуль живёт во временном кэше, все остальные — в настоящем.
func chainProbe(probes ...LicenseProbe) LicenseProbe {
	return func(dep DirectDependency) LicenseEvidence {
		for _, probe := range probes {
			if ev := probe(dep); ev.Resolved {
				return ev
			}
		}
		return LicenseEvidence{}
	}
}

// Возврат дефекта в НАСТОЯЩИЙ текст go.mod: гейт обязан покраснеть и назвать
// координату — номер строки, путь и версию.
//
// Подставной модуль СИНТЕТИЧЕСКИЙ, а не снятый: имя снятого пришлось бы держать
// в дереве ради самой пробы, и оно пережило бы свой предмет — тот самый класс,
// который гейт и стережёт. Сверх того снятый модуль уходит из кэша вместе с
// пином, и проба, опирающаяся на его каталог, стала бы беспредметной там, где
// снятие как раз и удалось.
func TestDependencyLicenseGate_RedsWhenAnUnlicensedModuleIsPutIntoTheRealGoMod(t *testing.T) {
	root := repoRoot(t)
	body, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatalf("чтение go.mod: %v", err)
	}
	injected := strings.Replace(string(body), "require (",
		"require (\n\t"+depUnlicensed.Path+" "+depUnlicensed.Version, 1)

	fake := fakeModuleCache(t, map[DirectDependency]map[string]string{
		depUnlicensed: {"go.mod": "module " + depUnlicensed.Path + "\n", "README.md": "# toolkit\n"},
	})
	deps := ParseGoModRequires(injected)
	findings, census := ScanDependencyLicenses(deps,
		chainProbe(DiskLicenseProbe(moduleCacheDir(t)), DiskLicenseProbe(fake)))

	if census.Resolved == 0 {
		t.Fatalf("условие не создано: кэш модулей не наполнен\n%s", census)
	}
	if len(findings) != 1 {
		t.Fatalf("находок %d, ожидалась ровно 1 — внесённая\n%s", len(findings), census)
	}
	got := findings[0].String()
	for _, want := range []string{"go.mod:", depUnlicensed.Path, depUnlicensed.Version} {
		if !strings.Contains(got, want) {
			t.Fatalf("находка не называет координату %q: %s", want, got)
		}
	}
}

// Законный близнец предыдущей на ТОМ ЖЕ настоящем дереве: go.mod как есть,
// после снятия зависимости. Без неё «краснеет на возвращённом дефекте» было бы
// неотличимо от «краснеет всегда».
func TestDependencyLicenseGate_SilentOnTheRealTreeAsItStands(t *testing.T) {
	root := repoRoot(t)
	body, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatalf("чтение go.mod: %v", err)
	}
	deps := ParseGoModRequires(string(body))
	findings, census := ScanDependencyLicenses(deps, DiskLicenseProbe(moduleCacheDir(t)))
	if len(findings) != 0 {
		t.Fatalf("дерево несёт модули без лицензии: %v\n%s", findings, census)
	}
}
