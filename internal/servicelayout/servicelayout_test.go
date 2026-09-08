// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// servicelayout_test.go — объявленный сегмент СОГЛАСЕН С ДЕРЕВОМ.
//
// Карта имён — то же, что ведомость исключений: она верна ровно до тех пор,
// пока у каждой записи есть предмет. Служба, назвавшая свои каталоги иначе и
// не вписанная сюда, не роняет ничего — обходчики МОЛЧА идут не в тот каталог
// и отдают «ноль находок» там, где не прочитано ничего.
//
// Поэтому проверка судит не саму карту, а её согласие с деревом, и делает это
// в ОБЕ стороны: объявленный сегмент обязан существовать, а сегмент платформы —
// не существовать у той службы, что назвалась своим именем. Односторонняя
// проверка зеленела бы на дереве, где остались оба каталога сразу.
package servicelayout

import (
	"os"
	"path/filepath"
	"testing"
)

const servicesRoot = "../../services"

func TestDeclaredSegmentAgreesWithTheTree(t *testing.T) {
	entries, err := os.ReadDir(servicesRoot)
	if err != nil {
		t.Fatalf("каталог служб не читается: %v", err)
	}

	var seen, own, platform int
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		svc := e.Name()
		// Слой use-case есть не у всякой службы: у части он лежит в
		// `internal/handler`. Такая служба к предмету карты не относится.
		appsDir := filepath.Join(servicesRoot, svc, "internal", "apps")
		if _, statErr := os.Stat(appsDir); statErr != nil {
			continue
		}
		seen++

		seg := UseCaseSegment(svc)
		if _, statErr := os.Stat(filepath.Join(appsDir, seg)); statErr != nil {
			t.Errorf("%s: объявлен сегмент %q, но каталога `internal/apps/%s/` нет (%v) — "+
				"обходчики служб молча пойдут не туда и отдадут ноль находок",
				svc, seg, seg, statErr)
			continue
		}
		if _, isOwn := ownSegments[svc]; isOwn {
			own++
			// Обратная сторона: у назвавшейся своим именем каталога платформы
			// быть НЕ должно, иначе переименование сделано наполовину.
			if _, statErr := os.Stat(filepath.Join(appsDir, platformSegment)); statErr == nil {
				t.Errorf("%s: рядом с %q лежит и каталог платформы %q — "+
					"переименование сделано наполовину, и какой из двух живой, не решал никто",
					svc, seg, platformSegment)
			}
			continue
		}
		platform++
	}

	t.Logf("перепись: служб со слоем `internal/apps/` %d · назвавшихся своим именем %d · "+
		"наследующих имя платформы %d · записей в карте %d",
		seen, own, platform, len(ownSegments))

	if seen == 0 {
		t.Fatal("обход пуст — вердикт беспредметен: ни одной службы со слоем `internal/apps/`")
	}
	if own == 0 {
		t.Fatalf("положительный контроль пуст: ни одна служба не названа своим именем, "+
			"при том что карта несёт %d записей — значит карта пережила свой предмет", len(ownSegments))
	}
	if platform == 0 {
		t.Fatal("контроль пуст: ни одной службы, наследующей имя платформы — " +
			"проверка не отличила бы карту, возвращающую своё имя всем")
	}
}
