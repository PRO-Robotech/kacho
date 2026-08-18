// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// nameformdbprobe_test.go — ограничение формы имени, поставленное миграцией
// сервиса, обязано быть ДОКАЗАНО вставкой в живую базу.
//
// # Предмет
//
// «Миграция применилась» и «ограничение отвергает негодную строку» — разные
// утверждения. Первое видно по дереву, второе — только прогоном. Задача #721
// пришла из состояния, где форму имени ставили ПЯТЬ сервисов, а её действие
// доказывал ОДИН: расхождение накопилось молча и заметить его было нечем, потому
// что оба перечня жили в разных местах и никто их не сверял.
//
// Ограничение базы — последний рубеж канона: код может смениться, вызывающий
// может пойти мимо слоя домена, но оператор базы отвергнет негодную строку
// всегда. Незадоказанное ограничение выглядит ровно так же, как действующее.
//
// # Что гейт требует
//
// Сервис, чья миграция объявляет канон формы, обязан нести пробу, зовущую общий
// двигатель `internal/nameformdb`. Гейт про НАЛИЧИЕ доказательства; его СОДЕРЖАНИЕ
// (перечень таблиц, отказ именно от формы, положительный контроль) держит сам
// двигатель, а его способность упасть — инъекция у geo.
//
// # Почему форма читается из дерева, а не выписана сюда
//
// Выписанная копия канона — ровно то, что запрещает соседний гейт
// TestResourceNameFormIsDeclaredOnce, и она разошлась бы с каноном первой.
// Поэтому форма приезжает параметром из единственного объявления
// (`pkg/validate/nameform`), а гейт отказывается судить, если прочитать её не
// удалось: «канона не нашли» обязано быть отказом, а не тихим «находок нет».
//
// # Перепись
//
// Печатается всегда: прочитано миграций, прочитано файлов проб, сервисов под
// формой, сервисов с пробой. Пустой обход — провал.
package repohygiene

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestNameFormConstraintIsProvenWhereItIsDeclared(t *testing.T) {
	tt := newTrackedTree(t, repoRoot(t))
	canonPattern := readCanonPattern(t, tt.root)

	files := readNameFormDBGateCorpus(t, tt)
	cov := analyseNameFormDBCoverage(files, canonPattern)

	// Предпосылка. Гейт обоснован тем, что в дереве ЕСТЬ миграции и ЕСТЬ файлы
	// проб. Пустой обход обязан быть отказом: молчание на нём означало бы
	// «ничего не прочитано», а выглядело бы как «нарушений нет».
	if cov.MigrationsRead == 0 || cov.TestsRead == 0 {
		t.Fatalf("обход прочитал миграций %d, файлов проб %d — гейту нечего рассматривать; "+
			"молчаливый зелёный здесь означал бы «проверено»", cov.MigrationsRead, cov.TestsRead)
	}
	if len(cov.Constrained) == 0 {
		t.Fatalf("ни одна миграция дерева не объявляет форму имени (искали форму из %s) — "+
			"предпосылка гейта не выполняется, его молчание ничего не значит", canonNameFormPkgDir)
	}

	t.Logf("прочитано миграций %d, файлов проб %d; форму ставят сервисы: %v; пробу несут: %v",
		cov.MigrationsRead, cov.TestsRead, cov.Services(), probedServices(cov))

	for _, svc := range cov.Unproven() {
		t.Errorf("сервис %s ставит форму имени миграцией (%s), но действие ограничения "+
			"не доказано ни одной пробой: ни один файл `services/%s/**/*_test.go` не зовёт "+
			"общий двигатель. «Миграция применилась» и «ограничение отвергает негодную строку» — "+
			"разные утверждения, и проверяется только первое",
			svc, strings.Join(cov.Constrained[svc], ", "), svc)
	}
}

func probedServices(cov nameFormDBCoverage) []string {
	out := make([]string, 0, len(cov.Probed))
	for svc := range cov.Probed {
		out = append(out, svc)
	}
	sort.Strings(out)
	return out
}

// readNameFormDBGateCorpus читает то и только то, что гейт судит: миграции
// сервисов и файлы их проб.
func readNameFormDBGateCorpus(t *testing.T, tt *trackedTree) map[string]string {
	t.Helper()

	out := map[string]string{}
	for _, rel := range tt.SortedFiles() {
		rel = filepath.ToSlash(rel)
		if !strings.HasPrefix(rel, "services/") {
			continue
		}
		isMigration := strings.HasSuffix(rel, ".sql") && strings.Contains(rel, "/internal/migrations/")
		if !isMigration && !strings.HasSuffix(rel, "_test.go") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(tt.root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("%s: чтение: %v — гейт обязан отказаться судить дерево, которое не смог "+
				"прочитать целиком", rel, err)
		}
		out[rel] = string(body)
	}
	return out
}
