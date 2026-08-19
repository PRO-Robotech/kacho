// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package serviceerr_test

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/helpers"
)

// Слова, которыми о своём отказе говорит хранилище. Вызывающему они не
// адресованы: «violates check constraint» — формулировка Postgres, а не
// контракт Kachō (задача #718). Проба стоит на ПРОВОДЕ, а не в репозитории:
// внутрь цепочки исходная ошибка кладётся намеренно — она нужна оператору.
var wireDBVocabulary = []string{"check constraint", "violates", "sqlstate", "23514", "pq:", "pgx"}

func assertWireClean(t *testing.T, err error) {
	t.Helper()
	msg := strings.ToLower(status.Convert(err).Message())
	for _, w := range wireDBVocabulary {
		if strings.Contains(msg, w) {
			t.Errorf("на проводе язык СУБД (%q): %q", w, status.Convert(err).Message())
		}
	}
}

// TestMapRepoErr_NameFormBackstop_IsInternalOnTheWire — вызывающий, чьё имя
// умерло на ограничении таблицы, получает внутренний отказ, а не обвинение.
//
// Довод — в `helpers.WrapPgErr`: форму имени vpc проверяет сам, до вставки,
// поэтому срабатывание ограничения означает дефект сервиса. Проба утверждает
// ОБА наблюдаемых свойства: код и текст. Утверждение одного кода оставило бы
// зелёным пересказ СУБД, а утверждение одного текста — смену кода.
func TestMapRepoErr_NameFormBackstop_IsInternalOnTheWire(t *testing.T) {
	repoErr := helpers.WrapPgErr(&pgconn.PgError{
		Code:           "23514",
		TableName:      "addresses",
		ConstraintName: "addresses_name_check",
		Message:        `new row for relation "addresses" violates check constraint "addresses_name_check"`,
	}, "Address", "adr-1")

	got := serviceerr.MapRepoErr(repoErr)

	if code := status.Code(got); code != codes.Internal {
		t.Fatalf("код = %s; ожидался Internal", code)
	}
	if msg := status.Convert(got).Message(); msg != "internal database error" {
		t.Fatalf("текст = %q; ожидался фиксированный «internal database error»", msg)
	}
	assertWireClean(t, got)
	if strings.Contains(status.Convert(got).Message(), "addresses_name_check") {
		t.Errorf("имя ограничения базы уехало вызывающему")
	}
}

// TestMapRepoErr_OtherCheck_StaysInvalidArgumentOnTheWire — положительный
// контроль: ограничение, формой имени НЕ являющееся, по-прежнему отвечает
// отказом по вводу. Без него отрицание выше было бы неотличимо от «схлопнули
// весь 23514 в Internal».
func TestMapRepoErr_OtherCheck_StaysInvalidArgumentOnTheWire(t *testing.T) {
	repoErr := helpers.WrapPgErr(&pgconn.PgError{
		Code:           "23514",
		TableName:      "addresses",
		ConstraintName: "addresses_labels_valid",
		Message:        `new row for relation "addresses" violates check constraint "addresses_labels_valid"`,
	}, "Address", "adr-1")

	got := serviceerr.MapRepoErr(repoErr)

	if code := status.Code(got); code != codes.InvalidArgument {
		t.Fatalf("код = %s; ожидался InvalidArgument", code)
	}
	assertWireClean(t, got)
	if strings.Contains(status.Convert(got).Message(), "addresses_labels_valid") {
		t.Errorf("имя ограничения базы уехало вызывающему: %q", status.Convert(got).Message())
	}
}

// TestMapRepoErrLeakSafe_CheckViolation_SpeaksTheSameOnBothListeners — тот же
// отказ на внутреннем слушателе читается так же.
//
// Полоса, теряющая код или тон на одном из портов, давала бы один отказ,
// читаемый по-разному в зависимости от того, куда постучались.
func TestMapRepoErrLeakSafe_CheckViolation_SpeaksTheSameOnBothListeners(t *testing.T) {
	for _, c := range []struct {
		name       string
		constraint string
		want       codes.Code
	}{
		{"форма имени — дефект сервиса", "address_pools_name_check", codes.Internal},
		{"прочее ограничение — ввод", "address_pools_kind_chk", codes.InvalidArgument},
	} {
		t.Run(c.name, func(t *testing.T) {
			repoErr := helpers.WrapPgErr(&pgconn.PgError{
				Code:           "23514",
				TableName:      "address_pools",
				ConstraintName: c.constraint,
			}, "AddressPool", "apl-1")

			got := serviceerr.MapRepoErrLeakSafe(repoErr, "address pool admin error")

			if code := status.Code(got); code != c.want {
				t.Fatalf("код = %s; ожидался %s", code, c.want)
			}
			assertWireClean(t, got)
		})
	}
}
