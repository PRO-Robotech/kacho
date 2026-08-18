// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg

// pgmaperr_unavailable_test.go — отказ ДОСТУПА к базе читается как
// недоступность, а не как поломка платформы (#666).
//
// Предмет. Пул строится лениво: соединения открываются на первом `Acquire`, а
// не на старте. В загрузочной буре (все домены поднимаются разом, тянущий
// величин ходит к владельцу через 30–100 мс после его листенера) быстрый
// транзиторный отказ открытия ОЖИДАЕМ ПО ПОСТРОЕНИЮ. Классифицированный как
// `Internal`, он не повторяется никем: `retry.OnUnavailable` повторяет только
// `Unavailable`.
//
// Отрицание идёт В ПАРЕ с положительным контролем: без «настоящая ошибка
// осталась настоящей» проба зеленела бы на мосте, объявляющем недоступностью всё
// подряд, — а это было бы хуже исходного дефекта, потому что прятало бы дефекты
// схемы за retryable-кодом.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	iamerr "github.com/PRO-Robotech/kacho/services/iam/internal/errors"
)

func TestWrapPgErr_ConnectionFailureIsUnavailable(t *testing.T) {
	unavailable := []struct {
		name string
		err  error
	}{
		{"53300 too_many_connections — пул ещё не разошёлся на bring-up",
			&pgconn.PgError{Code: "53300", Message: "sorry, too many clients already"}},
		{"57P03 cannot_connect_now — сервер проигрывает WAL",
			&pgconn.PgError{Code: "57P03", Message: "the database system is starting up"}},
		{"57P01 admin_shutdown — rolling restart",
			&pgconn.PgError{Code: "57P01", Message: "terminating connection due to administrator command"}},
		{"57P02 crash_shutdown — перезапуск после сбоя бэкенда",
			&pgconn.PgError{Code: "57P02", Message: "terminating connection because of crash"}},
		{"08006 connection_failure — класс 08 целиком",
			&pgconn.PgError{Code: "08006", Message: "connection failure"}},
		{"не-PgError: порт закрыт — сосед ещё не поднялся",
			&net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}},
		{"не-PgError: имя не резолвится",
			&net.DNSError{Err: "no such host", Name: "kacho-iam-db"}},
		{"не-PgError: соединение оборвалось на середине",
			fmt.Errorf("read tcp: %w", io.ErrUnexpectedEOF)},
	}

	for _, tc := range unavailable {
		t.Run(tc.name, func(t *testing.T) {
			got := wrapPgErr(tc.err, "", "")
			require.True(t, errors.Is(got, iamerr.ErrUnavailable),
				"отказ доступа к базе обязан быть повторяемым: %T → %v", tc.err, got)
		})
	}

	// ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ. Настоящая ошибка недоступностью НЕ становится:
	// мост, объявляющий retryable всё подряд, прятал бы дефекты схемы и прав за
	// вечным повтором.
	real := []struct {
		name string
		err  error
		want error
	}{
		{"28P01 неверный пароль — конфигурация, а не мигание",
			&pgconn.PgError{Code: "28P01", Message: "password authentication failed"}, iamerr.ErrInternal},
		{"3D000 нет такой базы — конфигурация",
			&pgconn.PgError{Code: "3D000", Message: "database does not exist"}, iamerr.ErrInternal},
		{"42501 нет права — конфигурация",
			&pgconn.PgError{Code: "42501", Message: "permission denied for table"}, iamerr.ErrInternal},
		{"23505 нарушение уникальности — ответ арендатору, а не сбой",
			&pgconn.PgError{Code: "23505", Message: "duplicate key"}, iamerr.ErrAlreadyExists},
		{"KQ001 отказ учёта — контракт квоты, не сбой базы",
			&pgconn.PgError{Code: "KQ001", Message: "prj-x has reached its limit of 5 iam.account"},
			iamerr.ErrQuotaExceeded},
	}

	for _, tc := range real {
		t.Run(tc.name, func(t *testing.T) {
			got := wrapPgErr(tc.err, "", "")
			require.False(t, errors.Is(got, iamerr.ErrUnavailable),
				"настоящая ошибка не должна становиться повторяемой: %v", got)
			require.ErrorIs(t, got, tc.want)
		})
	}

	// Отмена вызывающего недоступностью не является: бюджет кончился у НАС.
	t.Run("отмена контекста — не недоступность соседа", func(t *testing.T) {
		got := wrapPgErr(context_Canceled(), "", "")
		require.False(t, errors.Is(got, iamerr.ErrUnavailable),
			"отменённый нами вызов не есть недоступность базы")
	})
}

func context_Canceled() error { return fmt.Errorf("query: %w", context.Canceled) }
