// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// dependencylicense_test.go — ГЕЙТ: каждый модуль, закреплённый go.mod, несёт
// лицензию.
//
// Разбор предмета, границы и способа доказательства — в шапке
// `dependencylicense.go`; здесь он не пересказывается, чтобы два места об одном
// предмете не разошлись.
//
// Что делает именно этот файл: добывает КАТАЛОГ КЭША и текст go.mod РЕАЛЬНОГО
// дерева и предъявляет их разбору. Способность разбора падать и молчать
// доказывается отдельно — `dependencylicense_injection_test.go`.

// moduleCacheDir — каталог кэша модулей. Спрашивается у самого `go`, а не
// собирается из переменных окружения: `GOMODCACHE` бывает не задан, и тогда
// значение выводится из `GOPATH`, а он тоже бывает не задан.
func moduleCacheDir(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("go", "env", "GOMODCACHE").Output()
	if err != nil {
		t.Fatalf("go env GOMODCACHE: %v", err)
	}
	dir := strings.TrimSpace(string(out))
	if dir == "" {
		t.Fatal("go env GOMODCACHE пуст — каталог кэша модулей не установлен")
	}
	return dir
}

func TestEveryPinnedModuleCarriesALicense(t *testing.T) {
	root := repoRoot(t)
	body, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatalf("чтение go.mod: %v", err)
	}

	deps := ParseGoModRequires(string(body))
	if len(deps) == 0 {
		t.Fatal("обход пуст: в go.mod не разобрано ни одной записи require — " +
			"вердикт беспредметен, чинить надо разбор, а не дерево")
	}

	findings, census := ScanDependencyLicenses(deps, DiskLicenseProbe(moduleCacheDir(t)))
	t.Log(census.String())

	if census.Resolved == 0 {
		t.Fatalf("не прочитано НИ ОДНОГО каталога модуля из %d — условие не создано "+
			"(кэш модулей не наполнен: `go mod download`); «находок 0» здесь означало бы "+
			"«не прочитано ничего»\n%s", census.Requires, census.String())
	}

	if len(findings) == 0 {
		return
	}

	var b strings.Builder
	b.WriteString("модулей без лицензии: ")
	b.WriteString(strconv.Itoa(len(findings)))
	b.WriteString("\n")
	for _, f := range findings {
		b.WriteString("  ")
		b.WriteString(f.String())
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(census.String())
	b.WriteString("\n\nОтсутствие лицензии означает «все права защищены»: разрешения по\n")
	b.WriteString("умолчанию не существует, поэтому каждый образ и каждый клон этого\n")
	b.WriteString("ПУБЛИЧНОГО репозитория распространяет чужой код без разрешения.\n")
	b.WriteString("Исходов три: снять зависимость · получить лицензию от владельца ·\n")
	b.WriteString("заменить своим кодом. Прощённых записей у гейта нет и не заводится:\n")
	b.WriteString("запись «этому лицензия не нужна» есть утверждение о чужом праве.\n")
	b.WriteString("Где импортируется: git grep -n <путь модуля> -- '*.go'\n")
	t.Fatal(b.String())
}
