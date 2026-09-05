// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// decisionsuccessor_test.go — ГЕЙТ: все поверхности решения об удалении проекта
// называют ОДНУ задачу-преемника, и она объявлена в одном месте.
//
// Разбор и его границы — `decisionsuccessor.go`.
package repohygiene

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// projectDeletionDecisionDoc — документ решения. Координата ОДНА и здесь
// выписана намеренно: это и есть тот единственный вход, из которого гейт выводит
// всё остальное. Переедет документ — гейт скажет об этом отказом, а не молчанием.
const projectDeletionDecisionDoc = "docs/architecture/project-deletion-and-live-resources.md"

// TestProjectDeletionSurfacesNameTheSameSuccessor — сам гейт.
func TestProjectDeletionSurfacesNameTheSameSuccessor(t *testing.T) {
	root := repoRoot(t)

	doc, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(projectDeletionDecisionDoc)))
	if err != nil {
		t.Fatalf("документ решения %s не прочитан: %v — сверять поверхности не с чем, "+
			"и молчание гейта было бы сказано ни о чём", projectDeletionDecisionDoc, err)
	}

	successor := DeclaredSuccessor(doc)
	if successor == 0 {
		t.Fatalf("документ решения %s не объявляет задачу-преемника строкой %q.\n\n"+
			"Решение, принятое и не реализованное, обязано называть держателя остатка. "+
			"Без объявления каждая поверхность называет номер сама, и они расходятся "+
			"молча — в сторону, которая успокаивает: задача, при которой решение "+
			"принималось, закрывается, а ссылки продолжают вести к ней.",
			projectDeletionDecisionDoc, SuccessorMarker)
	}

	coords := DeclaredCoordinates(doc)
	census := DecisionCensus{Successor: successor, Coordinates: len(coords)}

	var (
		findings []string
		missing  []string
	)
	for _, rel := range coords {
		src, rerr := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if rerr != nil {
			// Координата, названная документом и НЕ существующая, — находка сама
			// по себе: документ посылает читателя туда, где ничего нет.
			missing = append(missing, rel)
			continue
		}
		census.Surfaces++
		cites, found := CitesSuccessor(src, successor)
		census.Citations += len(found)
		if !cites {
			findings = append(findings, SuccessorFinding(
				DecisionSurface{Path: rel, Cites: cites, Found: found}, successor))
		}
	}

	t.Logf("перепись: документ решения %s; объявленная задача-преемник #%d; "+
		"координат прочитано %d, поверхностей разобрано %d, ссылок на задачи встречено %d; "+
		"находок %d, ненайденных координат %d",
		projectDeletionDecisionDoc, census.Successor, census.Coordinates,
		census.Surfaces, census.Citations, len(findings), len(missing))

	// Предпосылка: поверхности вообще есть. Ноль означает, что документ перестал
	// называть координаты, и суждение выполняется тождественно.
	if census.Surfaces == 0 {
		t.Fatalf("документ решения не назвал НИ ОДНОЙ существующей поверхности "+
			"(координат прочитано %d) — гейт судил бы ни о чём", census.Coordinates)
	}

	if len(missing) > 0 {
		sort.Strings(missing)
		t.Errorf("документ решения называет координаты, которых в дереве нет:\n%s",
			strings.Join(missing, "\n"))
	}
	if len(findings) > 0 {
		sort.Strings(findings)
		t.Fatalf("поверхностей решения, не называющих задачу-преемника #%d: %d\n%s\n\n"+
			"Читатель такой поверхности идёт к задаче, которую она называет. Если та "+
			"закрыта, он читает «закрыта» как «сделано» — то есть ссылка лжёт в сторону, "+
			"которая успокаивает. Номер объявляется ОДИН раз (%s в документе решения), "+
			"остальные поверхности обязаны его содержать; историческая ссылка на задачу, "+
			"при которой решение принималось, при этом законна и гейтом не запрещена.",
			successor, len(findings), strings.Join(findings, "\n"), SuccessorMarker)
	}
}
