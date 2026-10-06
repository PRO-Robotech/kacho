// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// outbox_emitter_foundation_s1a7_integration_test.go — полоса RED S1-A7
// (issue-2918, NTF-3, Н3-Ф2): эмиттер журнала nlb пишет строку ФУНКЦИЕЙ
// ФУНДАМЕНТА с дескриптором таблицы (`corelib/subscription.Journal.Emit`), а не
// литеральной вставкой.
//
// Основание — замысел issue-2918: З5 (функция журнала получает дескриптор из
// `Mapping` модуля; перевод вида и действия — один источник; значение вне
// закрытого набора строки не даёт, подстановки нет — CX3C-04 (в)), З6 (строка
// nlb `repo/kacho/pg/outbox_emitter.go:37`: «литеральная вставка → обёртка вызова
// функции фундамента с дескриптором nlb»); держатели — NTF3-160 (б), NTF3-70
// (пара (в): литерал — красный, вызов функции фундамента — зелёный), УК3-30.
//
// # Что именно различает эту пробу
//
// Наблюдаемое отличие функции фундамента от литеральной вставки — ГДЕ стоит
// словарь. У литеральной вставки словарь один — ограничение базы
// `nlb_outbox_resource_type_check` (словарь целиком: `nlb_load_balancer`,
// `nlb_listener`, `nlb_target_group`, `notification`); у функции фундамента —
// объявление владельца (`subscriptionjournal.Journal(false).Mapping`), и запись вне
// него отвергается `subscription.ErrEntryRefused` ДО оператора. Последнее слово
// ограничение базы принимает (миграция
// `20261004170100_journal_kind_admits_notification.sql` — ключ строки сигнала
// ленты), а словарь видов владельца — нет: строку сигнала
// пишет `feed.Put` своим дескриптором, а не ресурсный эмиттер модуля. Поэтому
// это слово — единственный вход, на котором два писателя расходятся, и проба
// стоит ровно на нём.
//
// Близнец — тот же вызов с видом `nlb_load_balancer`: меняется ОДИН факт (слово
// вида), прочее (действие, идентификатор, проект, нагрузка, принципал) одно.
//
// Порядок проверок несущий: сперва вся фикстура (колонка инициатора, слово в
// ограничении базы, слова НЕТ в словаре владельца, близнец записался ровно одной
// строкой), и только потом — возможность испытуемого.

package pg_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	coredb "github.com/PRO-Robotech/corelib/db"
	"github.com/PRO-Robotech/corelib/ids"
	"github.com/PRO-Robotech/corelib/operations"
	"github.com/PRO-Robotech/corelib/pgtest"
	"github.com/PRO-Robotech/corelib/subscription"

	kachorepo "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
	kachopg "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho/pg"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/subscriptionjournal"
)

// s1a7SignalKind — слово, которое ограничение базы принимает, а словарь видов
// владельца — нет (ключ строки сигнала ленты, `corelib/notify/feed.JournalKey`).
const s1a7SignalKind = "notification"

func s1a7Rows(t *testing.T, pool *pgxpool.Pool, kind, id string) int {
	t.Helper()
	var n int
	require.NoError(t, pool.QueryRow(context.Background(), `
		SELECT count(*) FROM kacho_nlb.nlb_outbox WHERE resource_type = $1 AND resource_id = $2`,
		kind, id).Scan(&n))
	return n
}

// s1a7Emit — один вызов эмиттера модуля в своей транзакции репозитория:
// успех коммитится (строка ложится), отказ откатывается.
func s1a7Emit(t *testing.T, ctx context.Context, repo *kachopg.Repository, kind, id, projectID string) error {
	t.Helper()
	w, err := repo.Writer(ctx)
	require.NoError(t, err, "ФИКСТУРА: транзакция репозитория не открылась")
	emitErr := w.Outbox().Emit(ctx, kind, id, projectID, kachorepo.OutboxActionCreated,
		map[string]any{"id": id, "name": "lb-s1a7"})
	if emitErr != nil {
		w.Abort()
		return emitErr
	}
	require.NoError(t, w.Commit(), "ФИКСТУРА: коммит после успешного Emit")
	return nil
}

// TestLB_S1A7_EmitterRefusesAKindOutsideTheOwnersDictionary — З5, CX3C-04 (в),
// NTF3-70 (в): эмиттер журнала nlb судит запись объявлением владельца —
// вид вне словаря видов отвергается `subscription.ErrEntryRefused`, строки нет;
// близнец (вид из словаря) — строка одна.
func TestLB_S1A7_EmitterRefusesAKindOutsideTheOwnersDictionary(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: testcontainers Postgres")
	}
	bg := context.Background()
	pool, err := coredb.NewPool(bg, setupTestDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)

	// --- ФИКСТУРА ---------------------------------------------------------
	var n int
	require.NoError(t, pool.QueryRow(bg, `
		SELECT count(*) FROM information_schema.columns
		 WHERE table_schema = 'kacho_nlb' AND table_name = 'nlb_outbox' AND column_name = 'initiator'`).Scan(&n))
	require.Equal(t, 1, n, "ФИКСТУРА: у kacho_nlb.nlb_outbox нет колонки initiator — миграция S1-A1 не применена")

	var def string
	require.NoError(t, pool.QueryRow(bg, `
		SELECT pg_get_constraintdef(c.oid) FROM pg_constraint c
		  JOIN pg_class r ON r.oid = c.conrelid
		  JOIN pg_namespace s ON s.oid = r.relnamespace
		 WHERE s.nspname = 'kacho_nlb' AND r.relname = 'nlb_outbox'
		   AND c.conname = 'nlb_outbox_resource_type_check'`).Scan(&def))
	require.Contains(t, def, "'"+s1a7SignalKind+"'",
		"ФИКСТУРА: ограничение базы не принимает слово %q — вход, на котором словарь владельца и словарь базы расходятся, не построен (определение: %s)",
		s1a7SignalKind, def)

	journal := subscriptionjournal.Journal(false)
	_, inOwner := journal.Mapping.Kinds[s1a7SignalKind]
	require.False(t, inOwner,
		"ФИКСТУРА: слово %q стоит в словаре видов владельца — расхождения словарей нет, проба беспредметна", s1a7SignalKind)
	_, twinInOwner := journal.Mapping.Kinds[kachorepo.OutboxResourceLoadBalancer]
	require.True(t, twinInOwner, "ФИКСТУРА: вида близнеца %q нет в словаре владельца", kachorepo.OutboxResourceLoadBalancer)

	userID := ids.NewHyphenID(ids.PrefixUser)
	ctx := operations.WithPrincipal(bg, operations.Principal{Type: "user", ID: userID})
	repo := mustJournalWriter(kachopg.New(pool, nil, probeJournalOptions))
	projectID := "prj01ABC"

	twinID := ids.NewID(ids.PrefixLoadBalancer)
	require.NoError(t, s1a7Emit(t, ctx, repo, kachorepo.OutboxResourceLoadBalancer, twinID, projectID),
		"ФИКСТУРА (близнец): эмиттер отверг вид из словаря владельца")
	require.Equal(t, 1, s1a7Rows(t, pool, kachorepo.OutboxResourceLoadBalancer, twinID),
		"ФИКСТУРА (близнец): строка вида из словаря владельца легла не одна")

	// --- ПРЕДМЕТ ----------------------------------------------------------
	refusedID := ids.NewID(ids.PrefixLoadBalancer)
	emitErr := s1a7Emit(t, ctx, repo, s1a7SignalKind, refusedID, projectID)
	landed := s1a7Rows(t, pool, s1a7SignalKind, refusedID)
	if emitErr == nil {
		t.Fatalf("S1-A7 (З5, CX3C-04 (в)): эмиттер журнала nlb принял вид %q вне словаря владельца — "+
			"строка легла (строк: %d); запись идёт мимо функции фундамента с дескриптором "+
			"(subscription.Journal.Emit), словарь у неё — только ограничение базы", s1a7SignalKind, landed)
	}
	require.Truef(t, errors.Is(emitErr, subscription.ErrEntryRefused),
		"S1-A7 (З5): отказ эмиттера — не отказ объявления владельца (subscription.ErrEntryRefused): %v", emitErr)
	require.Truef(t, strings.Contains(emitErr.Error(), s1a7SignalKind),
		"S1-A7 (З5): отказ не называет вид: %v", emitErr)
	require.Equal(t, 0, landed, "S1-A7 (З5): отказанная запись оставила строку журнала")
}
