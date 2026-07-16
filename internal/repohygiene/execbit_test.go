// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// execbit_test.go — гейт исполняемости скриптов.
//
// Ловит два класса, которые НЕ ловит ни компилятор, ни линтер, ни `go test`, и которые
// проявляются только в CI/чистом клоне («у меня работало»):
//
//  1. Потерянный бит исполнения. Скрипт с shebang'ом, закоммиченный как 100644, в чистом
//     клоне просто не запустится: `./kind/create-cluster.sh: Permission denied`.
//     Реальный инцидент 2026-07-16: вставка SPDX-хедеров писала через
//     `printf … > "$f.tmp" && mv` — временный файл создаётся с umask-правами 644, и mv
//     затирал режим. Пострадали 27 скриптов; на диске у автора всё работало, потому что
//     режим правился отдельно, а git хранил 100644.
//
//  2. Shebang не на первой строке. `#!/usr/bin/env python3` обязан быть ПЕРВОЙ строкой —
//     ядро читает его только оттуда. Тот же хедер-скрипт спец-обрабатывал лишь *.sh и у
//     .py воткнул копирайт ПЕРЕД shebang'ом: файл синтаксически валиден (и `py_compile`
//     это пропустил!), но как исполняемый мёртв.
//
// Проверяется ИНДЕКС git (`ls-files -s`), а не режим на диске: в CI/клоне работает
// именно он, и расхождение диск-vs-индекс — самая частая форма этого бага.
package repohygiene

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitLsFiles возвращает `git ls-files -s`-строки: "<mode> <oid> <stage>\t<path>".
func gitLsFiles(t *testing.T, root string) []string {
	t.Helper()
	cmd := exec.Command("git", "ls-files", "-s")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Skipf("git ls-files недоступен (%v) — гейт пропущен", err)
	}
	return strings.Split(strings.TrimRight(string(out), "\n"), "\n")
}

// hasShebang — файл начинается с `#!` (ПЕРВАЯ строка).
func hasShebang(t *testing.T, path string) bool {
	t.Helper()
	f, err := os.Open(path) //nolint:gosec // путь из обхода репо в тесте
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	if !sc.Scan() {
		return false
	}
	return strings.HasPrefix(sc.Text(), "#!")
}

// findShebangLine возвращает 1-based номер строки с shebang'ом (0 — нет вовсе).
func findShebangLine(t *testing.T, path string) int {
	t.Helper()
	f, err := os.Open(path) //nolint:gosec // путь из обхода репо в тесте
	if err != nil {
		return 0
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for i := 1; i <= 10 && sc.Scan(); i++ {
		if strings.HasPrefix(strings.TrimSpace(sc.Text()), "#!") {
			return i
		}
	}
	return 0
}

// TestShebangScriptsAreExecutable — каждый файл с shebang'ом обязан лежать в индексе как
// 100755. Иначе в чистом клоне/CI он не запустится.
func TestShebangScriptsAreExecutable(t *testing.T) {
	root := repoRoot(t)
	var broken []string
	for _, line := range gitLsFiles(t, root) {
		mode, path, ok := parseLsFiles(line)
		if !ok {
			continue
		}
		abs := filepath.Join(root, path)
		if !hasShebang(t, abs) {
			continue
		}
		if mode != "100755" {
			broken = append(broken, path+" (mode="+mode+", ожидался 100755)")
		}
	}
	if len(broken) > 0 {
		t.Errorf("%d файл(ов) с shebang'ом НЕ исполняемы в индексе git — в чистом клоне не запустятся:\n%s\n\nпочинить: git add --chmod=+x <файл>",
			len(broken), strings.Join(broken, "\n"))
	}
}

// TestShebangIsFirstLine — shebang обязан быть ПЕРВОЙ строкой. Если он есть, но ниже
// (напр. под вставленным копирайт-хедером) — файл как исполняемый мёртв, хотя
// синтаксически валиден и тесты/компиляторы молчат.
func TestShebangIsFirstLine(t *testing.T) {
	root := repoRoot(t)
	var broken []string
	for _, line := range gitLsFiles(t, root) {
		_, path, ok := parseLsFiles(line)
		if !ok {
			continue
		}
		switch filepath.Ext(path) {
		case ".sh", ".py", ".bash":
		default:
			continue
		}
		abs := filepath.Join(root, path)
		if n := findShebangLine(t, abs); n > 1 {
			broken = append(broken, path+" (shebang на строке "+itoa(n)+", должен быть на 1-й)")
		}
	}
	if len(broken) > 0 {
		t.Errorf("%d файл(ов) с shebang'ом НЕ на первой строке — ядро его не увидит:\n%s",
			len(broken), strings.Join(broken, "\n"))
	}
}

// parseLsFiles разбирает строку `git ls-files -s`: "<mode> <oid> <stage>\t<path>".
func parseLsFiles(line string) (mode, path string, ok bool) {
	tab := strings.IndexByte(line, '\t')
	if tab < 0 {
		return "", "", false
	}
	meta, path := line[:tab], line[tab+1:]
	sp := strings.IndexByte(meta, ' ')
	if sp < 0 {
		return "", "", false
	}
	return meta[:sp], path, true
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
