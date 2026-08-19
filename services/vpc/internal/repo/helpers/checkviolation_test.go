// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package helpers

import (
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// dbVocabulary — слова, которыми о своём отказе говорит СУБД. Ни одно из них не
// принадлежит контракту продукта, и ни одно не должно доезжать до вызывающего:
// «violates check constraint» — формулировка Postgres, а не Kachō (задача #718).
var dbVocabulary = []string{"check constraint", "violates", "SQLSTATE", "23514", "constraint"}

func assertNoDBVocabulary(t *testing.T, err error) {
	t.Helper()
	msg := err.Error()
	for _, w := range dbVocabulary {
		if strings.Contains(strings.ToLower(msg), strings.ToLower(w)) {
			t.Errorf("сообщение наружу несёт язык СУБД (%q): %q", w, msg)
		}
	}
}

// TestWrapPgErr_NameFormBackstop_IsInternal — нарушение ограничения ФОРМЫ ИМЕНИ
// есть дефект СЕРВИСА, а не ввода вызывающего.
//
// Форму имени vpc проверяет сам, доменным newtype (`domain.RcNameVPC.Validate`
// → `nameform.OK`), на обоих путях записи. Ограничение таблицы, поставленное
// миграцией 715001, — защита последнего рубежа. Значит его срабатывание
// означает, что негодное значение прошло мимо проверки; отвечать на это
// `INVALID_ARGUMENT` — обвинять вызывающего в нашей ошибке.
func TestWrapPgErr_NameFormBackstop_IsInternal(t *testing.T) {
	pgErr := &pgconn.PgError{
		Code:           "23514",
		TableName:      "networks",
		ConstraintName: "networks_name_check",
		Message:        `new row for relation "networks" violates check constraint "networks_name_check"`,
	}

	got := WrapPgErr(pgErr, "Network", "net-1")

	if !errors.Is(got, ErrInternal) {
		t.Fatalf("23514 на ограничении формы имени = %v; ожидался ErrInternal", got)
	}
	if errors.Is(got, ErrInvalidArg) {
		t.Fatalf("23514 на ограничении формы имени = %v; ErrInvalidArg обвиняет вызывающего", got)
	}
}

// TestWrapPgErr_OtherCheck_StaysInvalidArgWithoutDBVocabulary — положительный
// контроль к предыдущей пробе: ограничение, формой имени НЕ являющееся,
// остаётся отказом по вводу — но говорит тоном контракта, а не СУБД.
//
// Без этой пары отрицание было бы неотличимо от «схлопнули весь 23514 в
// INTERNAL»: одна проба на «стало INTERNAL» зеленеет и в этом случае тоже.
func TestWrapPgErr_OtherCheck_StaysInvalidArgWithoutDBVocabulary(t *testing.T) {
	pgErr := &pgconn.PgError{
		Code:           "23514",
		TableName:      "networks",
		ConstraintName: "networks_labels_valid",
		Message:        `new row for relation "networks" violates check constraint "networks_labels_valid"`,
	}

	got := WrapPgErr(pgErr, "Network", "net-1")

	if !errors.Is(got, ErrInvalidArg) {
		t.Fatalf("23514 на прочем ограничении = %v; ожидался ErrInvalidArg", got)
	}
	assertNoDBVocabulary(t, got)
	if strings.Contains(got.Error(), "networks_labels_valid") {
		t.Errorf("имя ограничения базы утекло вызывающему: %q", got.Error())
	}
}

// Язык СУБД на ПРОВОДЕ проверяется отдельной пробой — в слое, который и решает,
// что увидит вызывающий (`shared/serviceerr`, checkviolation_test.go). Здесь его
// проверять нельзя: полоса внутреннего дефекта НАМЕРЕННО сохраняет исходную
// ошибку в цепочке — она нужна оператору в журнале, — а наружу её сворачивает
// отображение в статус. Проба, запретившая бы сохранение здесь, потребовала бы
// выбросить причину и оставить оператора без единой строки о SQLSTATE.
