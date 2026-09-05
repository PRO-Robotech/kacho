// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// vendorednotice_test.go — держатель класса «вендоренный чужой файл без
// уведомления первоисточника». Разбор — vendorednotice.go, доказательство
// падучести — vendorednotice_injection_test.go.
//
// Состав дерева берётся из ИНДЕКСА git, а не с диска: на диске лежат
// gitignored-артефакты, которых в репозитории нет, а предмет гейта — то, что
// уезжает в чистый клон и раздаётся получателю. Именно раздача и есть предмет
// Apache-2.0 §4.
package repohygiene

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// licenseCopyNames — имена, под которыми копия лицензии признаётся лежащей в
// корне пространства. Перечень имён ФАЙЛА, а не путей: он не растёт с числом
// вендоренных пространств.
var licenseCopyNames = []string{"LICENSE", "LICENSE.txt", "LICENSE.md", "COPYING"}

// readLicenseAt — копия лицензии в корне пространства; пустая строка означает,
// что её нет.
func readLicenseAt(root, repo string) string {
	for _, name := range licenseCopyNames {
		b, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(root), name))
		if err == nil && len(b) > 0 {
			return string(b)
		}
	}
	return ""
}

func TestVendoredContractsCarryTheirUpstreamNotice(t *testing.T) {
	root := repoRoot(t)

	var files []VendoredFile
	indexed := 0
	for _, line := range gitLsFiles(t, root) {
		_, rel, ok := parseLsFiles(line)
		if !ok {
			continue
		}
		indexed++
		if filepath.Ext(rel) != ".proto" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("чтение %s: %v", rel, err)
		}
		files = append(files, VendoredFile{Rel: rel, Source: string(b)})
	}

	findings, census := ScanVendoredNotices(files, func(vendorRoot string) string {
		return readLicenseAt(vendorRoot, root)
	})

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		return findings[i].Kind < findings[j].Kind
	})

	// Перепись печатается ВСЕГДА: «ноль находок» обязано быть отличимо от «ноль
	// прочитанного», а «ось не вынесена» — от «ось вынесена и молчит».
	t.Logf("осмотрено: записей индекса %d, файлов контракта %d "+
		"(наших %d, вендоренных %d, без объявления пакета %d); "+
		"корней вендоренных пространств %d (выведено по сегменту пути %d, взято каталогом файла %d); "+
		"уведомлений найдено %d; сверка названия лицензии вынесена %d, НЕ вынесена %d; находок %d",
		indexed, len(files), census.Ours, census.Vendored, census.PackageUndeclared,
		census.VendorRoots, census.RootsDerivedFromPath, census.RootsFellBackToDir,
		census.NoticesFound, census.MismatchChecked, census.LicenseTitleUnknown, len(findings))

	if indexed == 0 {
		t.Fatal("индекс git пуст — обход не дошёл ни до одного файла. Это отказ, а не чистота")
	}
	if len(files) == 0 {
		t.Fatal("файлов контракта не прочитано НИ ОДНОГО: отбор по расширению разошёлся " +
			"с деревом. Это отказ, а не чистота")
	}
	// Классификатор судит обе стороны, поэтому проверяется он тоже с обеих: наших
	// контрактов в этом дереве сотни, и ноль здесь означает, что разбор объявления
	// пакета умер, а не что дерево чисто. Ноль ВЕНДОРЕННЫХ отказом не является —
	// дерево без чужого кода есть законная цель, а не поломка; перепись это скажет.
	if census.Ours == 0 {
		t.Fatal("наших контрактов не опознано НИ ОДНОГО: разбор объявления пакета разошёлся " +
			"с деревом. Это отказ, а не чистота")
	}

	if len(findings) > 0 {
		var b strings.Builder
		for _, f := range findings {
			b.WriteString("\n  " + f.File + " [" + f.Kind + "] пакет " + f.Package +
				", корень пространства " + f.VendorRoot + ": " + f.Detail)
		}
		t.Errorf("вендоренных нарушений уведомления: %d%s", len(findings), b.String())
	}
}
