// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// migratordbopen_test.go — гейт: шаг «открыть базу, дождаться готовности,
// настроить goose» объявлен один раз.
//
// Предмет, требования, замер цены и граница разобраны в шапке
// migratordbopen.go — здесь они не пересказываются, чтобы не завести двух мест
// об одном предмете.
//
// Доказательство способности упасть, смолчать и ронять ТОЛЬКО своё —
// migratordbopen_injection_test.go.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

func TestMigratorDatabaseOpeningIsDeclaredOnce(t *testing.T) {
	root := repoRoot(t)

	census, findings, err := auditMigratorDBOpen(root)
	if err != nil {
		t.Fatalf("%v", err)
	}
	t.Log(census.String())

	// Страж собственной предпосылки. Обход, ничего не прочитавший, обязан
	// ронять прогон: иначе «ноль находок» неотличимо от «ноль прочитанного».
	if census.TractFiles == 0 {
		t.Fatalf("обход пуст: файлов тракта наката прочитано 0 — вердикт беспредметен. %s",
			census.String())
	}

	// ПОЛОЖИТЕЛЬНАЯ половина. Без неё отрицание ниже стало бы вакуумным в тот
	// день, когда шаг переименуют или унесут: искать было бы нечего, и молчание
	// выглядело бы исправной работой.
	if !census.SharedOpen || !census.SharedGoose || !census.SharedSpec {
		t.Errorf("общий пакет %s не объявляет шаг целиком "+
			"(открытие базы %v, настройка goose %v, метаданные диалекта %v). "+
			"Отрицание ниже без него проверяет пустое множество — почини премису, "+
			"а не перечень. Целевая форма — %s",
			migratorSharedTractHome,
			census.SharedOpen, census.SharedGoose, census.SharedSpec,
			migratorTractDecisionDoc)
	}
	if census.MarkersDeclared != len(migratorDBOpenMarkers) {
		t.Errorf("общий пакет %s объявляет %d текстов отказа из %d: остальные либо "+
			"переименованы, либо унесены. Оператор читает эти строки в логе "+
			"init-контейнера, и разные редакции одного отказа — разные строки для "+
			"скрипта, который их разбирает",
			migratorSharedTractHome, census.MarkersDeclared, len(migratorDBOpenMarkers))
	}

	// ОТРИЦАТЕЛЬНАЯ половина.
	for _, text := range sortedDBOpenFindingTexts(findings) {
		t.Error(text)
	}
}

// auditMigratorDBOpen читает корпус и возвращает перепись с находками.
// Вынесен из пробы, чтобы инъекция звала ТО ЖЕ, что и гейт.
func auditMigratorDBOpen(root string) (migratorDBOpenCensus, []migratorTractFinding, error) {
	var (
		census   migratorDBOpenCensus
		findings []migratorTractFinding
		declared = map[string]struct{}{}
	)

	for _, dir := range []string{"pkg", "services"} {
		paths, err := treecorpus.UnderWithSuffix(filepath.Join(root, dir), ".go")
		if err != nil {
			return census, nil, err
		}
		for _, path := range paths {
			rel, rerr := filepath.Rel(root, path)
			if rerr != nil {
				return census, nil, rerr
			}
			rel = filepath.ToSlash(rel)

			// Пробы из корпуса исключены намеренно: проба, утверждающая текст
			// отказа, обязана его называть — иначе она не проверяет ничего. И
			// проба, поднимающая свой Postgres, вправе звать sql.Open.
			if strings.HasSuffix(rel, "_test.go") {
				continue
			}
			shared := migratorTractIsShared(rel)
			if !shared && !migratorTractIsEntryPoint(rel) {
				continue
			}

			src, rerr := os.ReadFile(path)
			if rerr != nil {
				return census, nil, rerr
			}
			census.FilesRead++
			if shared {
				census.SharedFiles++
			} else {
				census.TractFiles++
			}

			facts, ferr := readMigratorDBOpenSource(rel, string(src))
			if ferr != nil {
				return census, nil, ferr
			}

			if shared {
				census.SharedOpen = census.SharedOpen || facts.DeclaresOpen
				census.SharedGoose = census.SharedGoose || facts.DeclaresSetup
				census.SharedSpec = census.SharedSpec || facts.DeclaresSpec
				for _, m := range facts.Markers {
					declared[m] = struct{}{}
				}
				continue
			}

			if facts.OpensDB {
				census.OwnOpen++
			}
			if facts.WaitsReady {
				census.OwnReadyWait++
			}
			if facts.SetsUpGoose {
				census.OwnGooseSetup++
			}
			if facts.DeclaresSpec {
				census.OwnSpecDecl++
			}
			census.Redeclarations += len(dedupSortedMarkers(facts.Markers))
			findings = append(findings, migratorDBOpenFindings(rel, facts)...)
		}
	}
	census.MarkersDeclared = len(declared)
	return census, findings, nil
}
