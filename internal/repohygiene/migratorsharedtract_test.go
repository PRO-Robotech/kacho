// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// migratorsharedtract_test.go — гейт: половина тракта наката, проверяемая без
// базы, объявлена один раз.
//
// Предмет, требования, перечень признаков и граница разобраны в шапке
// migratorsharedtract.go — здесь они не пересказываются, чтобы не завести двух
// мест об одном предмете.
//
// Доказательство способности упасть и смолчать — в
// migratorsharedtract_injection_test.go.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

func TestMigratorDbFreeTractIsDeclaredOnce(t *testing.T) {
	root := repoRoot(t)

	census, findings, err := auditMigratorSharedTract(root)
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
	// день, когда тексты переименуют: искать было бы нечего, гейт молчал бы, и
	// молчание выглядело бы исправной работой.
	if census.MarkersDeclared != len(migratorRefusalMarkers) {
		t.Errorf("общий пакет %s объявляет %d текстов отказа из %d: "+
			"остальные либо переименованы, либо унесены. Отрицание ниже без них "+
			"проверяет пустое множество — почини премису, а не перечень",
			migratorSharedTractHome, census.MarkersDeclared, len(migratorRefusalMarkers))
	}

	// ОТРИЦАТЕЛЬНАЯ половина.
	for _, text := range sortedFindingTexts(findings) {
		t.Error(text)
	}
}

// auditMigratorSharedTract читает корпус и возвращает перепись с находками.
// Вынесен из пробы, чтобы инъекция звала ТО ЖЕ, что и гейт: доказательство,
// проверяющее свою копию разбора, не доказывает ничего о гейте.
func auditMigratorSharedTract(root string) (migratorTractCensus, []migratorTractFinding, error) {
	var (
		census   migratorTractCensus
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
			// отказа, обязана его называть — иначе она не проверяет ничего.
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

			lits, lerr := stringLiteralsOfGoSource(rel, string(src))
			if lerr != nil {
				return census, nil, lerr
			}
			for _, marker := range migratorRefusalMarkers {
				for _, lit := range lits {
					if !strings.Contains(lit, marker) {
						continue
					}
					if shared {
						declared[marker] = struct{}{}
						break
					}
					census.Redeclarations++
					findings = append(findings, migratorTractFinding{
						Rel:  rel,
						What: "заново объявляет текст отказа предусловий " + quotedMarker(marker),
					})
					break
				}
			}

			if shared {
				continue
			}
			own, oerr := declaresOwnTargetParser(rel, string(src))
			if oerr != nil {
				return census, nil, oerr
			}
			for _, what := range own {
				census.OwnTargetParser++
				findings = append(findings, migratorTractFinding{Rel: rel, What: what})
			}
		}
	}
	census.MarkersDeclared = len(declared)
	return census, findings, nil
}

func quotedMarker(s string) string { return `"` + s + `"` }
