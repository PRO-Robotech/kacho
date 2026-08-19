// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// nameformdb_test.go — шов, на который опирается диагностика `Run`.
//
// `Run` печатает перепись и уже найденное ДО того, как уронить прогон отказом
// чтения схемы. Это осмысленно ровно постольку, поскольку `Check` возвращает
// ПРИГОДНЫЙ отчёт ВМЕСТЕ с ошибкой. Утверждение проверяемо без базы и без
// подставного `*testing.T`, поэтому проверяется здесь, а не разглядыванием кода.
//
// Замечание, из которого выведено (рецензия #721, 2026-08-19): при отказе
// переписи `Run` ронял прогон сразу и терял оба — и перепись, и расхождения
// образцов с каноном, которые находятся ДО обращения к базе. Ложного зелёного
// не было; терялась диагностика, а она и есть то, ради чего пробу читают.
package nameformdb

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// failingExecer — база, которая не отвечает. Отказ отдаётся на ЧТЕНИИ (Query),
// потому что перепись идёт именно им; Exec на этом пути не зовётся, и его
// молчаливый успех выдал бы ложный вывод о том, какой путь исполнился.
type failingExecer struct{ err error }

func (f failingExecer) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	panic("Exec не должен зваться до успешной переписи — путь исполнился не тот, который проверяется")
}

func (f failingExecer) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, f.err
}

func TestCheck_ReportSurvivesTheError(t *testing.T) {
	ctx := context.Background()

	t.Run("перепись НЕ прочиталась — отчёт всё равно пригоден", func(t *testing.T) {
		boom := errors.New("соединение закрыто")
		p := Probe{
			Schema: "kacho_probe_seam",
			Tables: []Table{
				{Name: "widgets", Row: func(string, int) (string, []any) { return "", nil }},
			},
		}

		rep, err := p.Check(ctx, failingExecer{err: boom})
		if !errors.Is(err, boom) {
			t.Fatalf("отказ чтения обязан дойти до вызывающего, получено %v", err)
		}
		census := rep.Census()
		if !strings.Contains(census, "kacho_probe_seam") || !strings.Contains(census, "widgets") {
			t.Errorf("отчёт при отказе не несёт переписи — `Run` печатал бы пустоту: %q", census)
		}
		if rep.RejectedPerTable == 0 || rep.AcceptedPerTable == 0 {
			t.Errorf("отчёт при отказе не несёт объёма утверждений: отвергаемых %d, принимаемых %d",
				rep.RejectedPerTable, rep.AcceptedPerTable)
		}
	})

	t.Run("законный близнец: перечень таблиц ПУСТ — отказ, и он про другое", func(t *testing.T) {
		// Другой вход, а не копия предыдущего: здесь до базы дело не доходит
		// вовсе, поэтому исправная база подставляется намеренно — отказ обязан
		// прийти от предпосылки пробы, а не от чтения. Без этого подслучая
		// «отчёт пригоден при отказе» доказывалось бы на одном-единственном
		// виде отказа.
		p := Probe{Schema: "kacho_probe_seam"}

		rep, err := p.Check(ctx, failingExecer{err: errors.New("этой ошибки не должно быть видно")})
		if err == nil {
			t.Fatal("проба без таблиц обязана отказать: исполнять нечего, а пустой отчёт " +
				"читался бы как «находок нет»")
		}
		if strings.Contains(err.Error(), "этой ошибки не должно быть видно") {
			t.Errorf("отказ пришёл от чтения базы, хотя до неё дойти не должно было: %v", err)
		}
		if !strings.Contains(rep.Census(), "kacho_probe_seam") {
			t.Errorf("отчёт при отказе предпосылки не называет схему: %q", rep.Census())
		}
	})
}
