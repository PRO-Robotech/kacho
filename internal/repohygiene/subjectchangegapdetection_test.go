// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"
)

// TestSubjectChangeJournalDetectsAGapOnBothSides — журнал смены субъекта
// обнаруживает пропуск, и обнаруживает его на ОБЕИХ сторонах шва (задача #1712).
//
// Разбор предмета, трёх звеньев и предпосылок — в шапке
// `subjectchangegapdetection.go`; здесь он не пересказывается, иначе два места об
// одном предмете разошлись бы молча.
func TestSubjectChangeJournalDetectsAGapOnBothSides(t *testing.T) {
	root := repoRoot(t)
	var log strings.Builder

	findings, census, err := AuditSubjectChangeGapDetection(SubjectChangeGapDetectionOptions{
		Root:          root,
		ReaderPackage: "pkg/subjectchange",
	}, &log)
	if err != nil {
		t.Fatalf("обход дерева: %v", err)
	}
	t.Log(strings.TrimSpace(log.String()))

	for _, f := range findings {
		t.Errorf("НАХОДКА: %s", f.What)
	}
	if len(findings) > 0 {
		t.Log("Пропуск в журнале смены субъекта fail-open by design: снятая строка означает " +
			"непогашенный кэш вердиктов края, то есть неприменённый отзыв доступа, молча. " +
			"Разбор — services/iam/docs/engineering/architecture/journal-retention-is-a-policy.md")
	}

	// Объём осмотренного утверждается ОТДЕЛЬНО: «ноль находок» обязано быть
	// отличимо от «ноль прочитанного». Предпосылки (файлы, окна) анализатор
	// роняет сам ошибкой; здесь — вторая половина шва, которую он объявляет
	// находкой, а не отказом обхода.
	if len(census.WindowsAskFloor) != len(census.Windows) {
		t.Errorf("окон чтения %d, спрашивают пол %d — расхождение обязано быть находкой выше",
			len(census.Windows), len(census.WindowsAskFloor))
	}
	if len(census.Producers) == 0 || len(census.Parsers) == 0 {
		t.Errorf("шов разомкнут: производителей отказа %d, разборщиков у читателя %d",
			len(census.Producers), len(census.Parsers))
	}
}
