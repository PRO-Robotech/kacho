// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package shared

// errors_detail_named_test.go — подробность отказа НАЗЫВАЕТСЯ в тот момент,
// когда перестаёт существовать (#666).
//
// Предмет. Два комментария на этом пути обещали журнал («Detail stays in the
// error chain for logging»), а `grep -c "slog\|logger"` по обоим файлам давал
// ноль. Обещание читалось как факт, и когда тянущий величины отказал на каждом
// подъёме каждого домена дважды подряд, причину назвать было НЕЧЕМ.
//
// Утверждается ПОВЕДЕНИЕ (что записано в журнал), а не наличие вызова: проба на
// «в файле есть slog» осталась бы зелёной при записи в никуда.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iamerr "github.com/PRO-Robotech/kacho/services/iam/internal/errors"
)

// captureLog подменяет журнал процесса на время пробы.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func TestMapRepoErr_DiscardedDetailIsNamed(t *testing.T) {
	t.Run("SQLSTATE неразобранного кода попадает в журнал", func(t *testing.T) {
		buf := captureLog(t)

		pgErr := &pgconn.PgError{Code: "42P01", Message: "relation \"limits\" does not exist"}
		got := MapRepoErr(fmt.Errorf("list changed since: %w", iamerr.Wrapf(iamerr.ErrInternal, "%w", pgErr)))

		require.Equal(t, codes.Internal, status.Code(got))
		require.Equal(t, "internal error", status.Convert(got).Message(),
			"наружу по-прежнему уезжает фиксированный текст — журнал этого не меняет")

		require.Contains(t, buf.String(), "42P01",
			"SQLSTATE обязан быть назван: без него причина отказа не восстановима ничем")
	})

	t.Run("тип ошибки называется и без SQLSTATE", func(t *testing.T) {
		buf := captureLog(t)

		got := MapRepoErr(fmt.Errorf("pull: %w", errors.New("some transport failure")))

		require.Equal(t, codes.Internal, status.Code(got))
		require.Contains(t, buf.String(), "error_type",
			"у отказа без SQLSTATE обязан быть назван хотя бы тип — иначе «почему» не отвечает ничто")
	})

	// ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ ТИШИНЫ. Ожидаемые исходы журнал НЕ засоряют: строка,
	// печатающаяся на каждый промах чтения, перестаёт читаться на третий день, и
	// тогда её отсутствие в нужный момент никто не заметит.
	t.Run("ожидаемый исход журнал не засоряет", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			err  error
			code codes.Code
		}{
			{"не найдено", iamerr.Wrapf(iamerr.ErrNotFound, "Project prj-x not found"), codes.NotFound},
			{"уже существует", iamerr.Wrapf(iamerr.ErrAlreadyExists, "taken"), codes.AlreadyExists},
			{"недоступно", iamerr.Wrapf(iamerr.ErrUnavailable, "database unavailable"), codes.Unavailable},
		} {
			t.Run(tc.name, func(t *testing.T) {
				buf := captureLog(t)
				got := MapRepoErr(tc.err)
				require.Equal(t, tc.code, status.Code(got))
				require.Empty(t, strings.TrimSpace(buf.String()),
					"ожидаемый исход не пишется в журнал: шумная строка перестаёт читаться, "+
						"и её отсутствие в нужный момент становится незаметным")
			})
		}
	})

	// Персональные данные в журнал не идут — свободный текст ошибки не пишется
	// ВОВСЕ, поэтому запрет держится по построению, а не вниманием автора.
	t.Run("свободный текст ошибки в журнал не попадает", func(t *testing.T) {
		buf := captureLog(t)

		const secret = "tenant.person@example.com"
		got := MapRepoErr(fmt.Errorf("insert: %w",
			iamerr.Wrapf(iamerr.ErrInternal, "user %s already staged", secret)))

		require.Equal(t, codes.Internal, status.Code(got))
		require.NotContains(t, buf.String(), secret,
			"обёрнутая ошибка вправе нести значения полей арендатора; в журнал они не идут")

		// И запись при этом состоялась — иначе предыдущее утверждение зеленело бы
		// на пустом журнале, то есть на утрате подробности вместо её сокрытия.
		var rec map[string]any
		require.NoError(t, json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &rec),
			"строка обязана быть — и быть разбираемой")
		require.Equal(t, "repo error surfaced as INTERNAL", rec["msg"])
	})

	_ = context.Background
}
