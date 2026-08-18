// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg

import (
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// dbVocabularyNLB — словарь хранилища. «violates check constraint» —
// формулировка Postgres, а не контракт Kachō (задача #718). nlb производил её
// тем же текстом, что и vpc, поэтому чинится тем же заходом: класс, а не
// экземпляр.
var dbVocabularyNLB = []string{"check constraint", "violates", "sqlstate", "23514"}

// TestMapPgErr_NameFormBackstop_IsInternal — имя ресурса nlb судит
// `domain.LbName.Validate` → `corevalidate.Name` → единая форма дерева, на
// обоих путях записи. Ограничение таблицы — защита последнего рубежа, и её
// срабатывание есть дефект сервиса, а не ввода.
func TestMapPgErr_NameFormBackstop_IsInternal(t *testing.T) {
	got := mapPgErr(&pgconn.PgError{
		Code:           "23514",
		TableName:      "load_balancers",
		ConstraintName: "load_balancers_name_check",
		Message:        `new row for relation "load_balancers" violates check constraint "load_balancers_name_check"`,
	}, "LoadBalancer", "lb-1")

	if !errors.Is(got, kacho.ErrInternal) {
		t.Fatalf("23514 на ограничении формы имени = %v; ожидался ErrInternal", got)
	}
	if errors.Is(got, kacho.ErrInvalidArg) {
		t.Fatalf("23514 на ограничении формы имени = %v; ErrInvalidArg обвиняет вызывающего", got)
	}
}

// TestMapPgErr_OtherCheck_StaysInvalidArgWithoutDBVocabulary — положительный
// контроль: ограничение, формой имени НЕ являющееся, остаётся отказом по вводу
// и говорит тоном контракта.
func TestMapPgErr_OtherCheck_StaysInvalidArgWithoutDBVocabulary(t *testing.T) {
	got := mapPgErr(&pgconn.PgError{
		Code:           "23514",
		TableName:      "listeners",
		ConstraintName: "listeners_port_check",
		Message:        `new row for relation "listeners" violates check constraint "listeners_port_check"`,
	}, "Listener", "lsn-1")

	if !errors.Is(got, kacho.ErrInvalidArg) {
		t.Fatalf("23514 на прочем ограничении = %v; ожидался ErrInvalidArg", got)
	}
	msg := strings.ToLower(got.Error())
	for _, w := range dbVocabularyNLB {
		if strings.Contains(msg, w) {
			t.Errorf("сообщение несёт язык СУБД (%q): %q", w, got.Error())
		}
	}
	if strings.Contains(got.Error(), "listeners_port_check") {
		t.Errorf("имя ограничения базы утекло вызывающему: %q", got.Error())
	}
}
