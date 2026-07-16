// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package repohygiene — репо-широкие гигиенические гейты. Живёт в КОРНЕ, а не внутри
// services/compute: гейт проверяет ВЕСЬ репозиторий, и его прописка в одном из сервисов
// была рудиментом polyrepo (в kacho-compute он был локальным).
//
// license_test.go — каждый source-файл репозитория
// обязан нести SPDX-копирайт-хедер, а в корне должен лежать файл LICENSE.
package repohygiene

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const spdxMarker = "SPDX-License-Identifier: BUSL-1.1"

// repoRoot — поднимаемся от каталога теста до каталога с go.mod (корень репо).
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod not found above %s", dir)
		}
		dir = parent
	}
}

// skipPath — пути вне области покрытия хедерами: VCS, синканная AI-оснастка,
// docs-site, вендоренное и build-артефакты. Принимает REL-путь (обход идёт по индексу
// git, где имена каталогов отдельно не приходят).
func skipPath(rel string) bool {
	for _, seg := range strings.Split(rel, "/") {
		switch seg {
		case ".git", ".claude", "docs-site", "node_modules", "vendor", "bin":
			return true
		}
	}
	return false
}

// inScope — файлы, обязанные нести SPDX-хедер. Markdown/JSON/lock/Dockerfile и
// сгенерированный код (proto/gen) — вне области (см. licensing-and-comments.md).
func inScope(rel string) bool {
	base := filepath.Base(rel)
	if base == "Makefile" {
		return true
	}
	switch filepath.Ext(rel) {
	case ".go", ".sql", ".sh", ".py", ".yaml", ".yml":
		return true
	}
	return false
}

// isGenerated — файл произведён генератором (protoc/buf/mockgen/…), поэтому SPDX-хедер
// с него не требуется: его пишет генератор, а не человек.
//
// Детект — по КАНОНИЧНОМУ Go-маркеру (`^// Code generated .* DO NOT EDIT\.$`,
// https://go.dev/s/generatedcode), а НЕ по пути. Прежде исключение было захардкожено
// как префикс `proto/gen/` — путь polyrepo. При переезде в монорепу он протух МОЛЧА
// (стабы теперь в pkg/api/), и гейт вывалил 78 генерённых .pb.gw.go. Маркер переживает
// любую смену раскладки; путь — нет.
func isGenerated(t *testing.T, path string) bool {
	t.Helper()
	if filepath.Ext(path) != ".go" {
		return false
	}
	f, err := os.Open(path) //nolint:gosec // путь получен обходом дерева репо в тесте
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	// Маркер обязан стоять до объявления package — хватит первых строк.
	for i := 0; i < 10 && sc.Scan(); i++ {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "// Code generated") && strings.HasSuffix(line, "DO NOT EDIT.") {
			return true
		}
		if strings.HasPrefix(line, "package ") {
			return false
		}
	}
	return false
}

func hasHeader(t *testing.T, path string) bool {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	head := b
	if len(head) > 1024 {
		head = head[:1024]
	}
	return strings.Contains(string(head), spdxMarker)
}

func TestLicenseFileExists(t *testing.T) {
	root := repoRoot(t)
	if _, err := os.Stat(filepath.Join(root, "LICENSE")); err != nil {
		t.Fatalf("root LICENSE missing: %v", err)
	}
}

func TestSPDXHeadersPresent(t *testing.T) {
	root := repoRoot(t)
	var missing []string
	// Ходим по ИНДЕКСУ git, а не по диску (filepath.WalkDir). Причина: на диске лежат
	// gitignored-файлы, которых в репозитории нет и быть не должно — напр.
	// values.fe3455-ory.yaml (креды кластера, локальный артефакт). Обход диска требовал
	// бы от НИХ SPDX-хедер, что бессмысленно: гейт про содержимое РЕПОЗИТОРИЯ.
	// Индекс — ровно то, что уедет в чистый клон и в CI.
	for _, line := range gitLsFiles(t, root) {
		_, rel, ok := parseLsFiles(line)
		if !ok {
			continue
		}
		if skipPath(rel) || !inScope(rel) {
			continue
		}
		abs := filepath.Join(root, rel)
		if isGenerated(t, abs) {
			continue
		}
		if !hasHeader(t, abs) {
			missing = append(missing, rel)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("%d source file(s) missing SPDX header:\n%s",
			len(missing), strings.Join(missing, "\n"))
	}
}
