// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package identityvendor_test

import (
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/identityvendor"
)

// Сверка регистра — не педантизм: читатели сравнивают ПОНИЖЕННЫЙ текст с
// отметкой, и отметка с заглавной буквой не совпала бы ни с чем. Распознаватель
// ослеп бы молча, и «находок ноль» стало бы ложью на каждом дереве.
func TestMarksAreLowercaseAndNonEmpty(t *testing.T) {
	marks := identityvendor.Marks()
	if len(marks) == 0 {
		t.Fatal("отметок ноль — читатели ведомости судили бы пустым словарём и молчали на всём")
	}
	for _, m := range marks {
		if m == "" || m != strings.ToLower(m) {
			t.Errorf("отметка %q пуста либо не в нижнем регистре — понижённый текст с ней не совпадёт", m)
		}
	}
	if b := identityvendor.Brand(); b == "" || b != strings.ToLower(b) {
		t.Errorf("бренд %q пуст либо не в нижнем регистре", b)
	}
	t.Logf("перепись: отметок %d · бренд %d", len(marks), 1)
}

// Ведомость отдаётся КОПИЕЙ: вызывающий, поправивший срез, не меняет словарь
// соседнего читателя.
func TestMarksAreACopy(t *testing.T) {
	a := identityvendor.Marks()
	want := a[0]
	a[0] = "подмена"
	if got := identityvendor.Marks()[0]; got != want {
		t.Fatalf("правка возвращённого среза изменила словарь: %q вместо %q", got, want)
	}
}
