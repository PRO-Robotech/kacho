// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: Apache-2.0

package feedjournaltest

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	subscriptionv1 "github.com/PRO-Robotech/corelib/api/corelib/subscription"
	"github.com/PRO-Robotech/corelib/auth"
	"github.com/PRO-Robotech/corelib/ids"
	"github.com/PRO-Robotech/corelib/journaltx"
	"github.com/PRO-Robotech/corelib/notify/feed"
	"github.com/PRO-Robotech/corelib/operations"
	"github.com/PRO-Robotech/corelib/subscription"
)

// ResourceEventStand — база модуля с полной цепочкой его миграций и то, что
// проба функции базы `resource-event` (NTF-3 З10) знает о модуле: журнал на
// Go при включённом флаге, таблица ленты, имя модуля строки сигнала и вид
// журнала, чьей строкой проба пишет.
type ResourceEventStand struct {
	Pool    *pgxpool.Pool
	Journal subscription.Journal
	// FeedTable — таблица строк ленты модуля, как её назвала миграция
	// `notifygen init -service`. Имя приходит от пробы модуля: в не-тестовом
	// дереве имена таблиц ленты производит только corelib (гейт NTF1-B19).
	FeedTable string
	// Module — имя модуля: объект строки сигнала `notification_feed:<Module>`.
	Module string
	// Kind — слово журнала вида с именем формы DNS-метки и якорем проекта.
	Kind string
}

// feedRow — строка ленты, поставленная функцией базы.
type feedRow struct {
	Template string
	Class    string
	State    string
	Attrs    map[string]string
}

// RequireResourceEventFeedRow — функция базы `resource-event` висит на журнале
// модуля и ставит строку ленты той же транзакцией, что строка журнала:
//
//   - флаг `true`: строка журнала снятия вида с именем даёт ровно одну строку
//     ленты шаблона `resource-event` с атрибутами строки журнала (вид,
//     идентификатор, якорь `project:<проект>`, род `DELETED`, инициатор
//     транзакции, имя из полезной нагрузки) и одну строку сигнала
//     `notification` с объектом модуля в журнале (NTF3-68 по пути функции
//     базы);
//   - близнец «флаг `false`» — ни строки ленты, ни строки сигнала (NTF3-65);
//   - близнец «откат» — та же вставка при флаге `true`, откаченная, не
//     оставляет строки ленты (NTF3-69).
//
// Транзакцию открывает помощник `journaltx` — тот же, что у писателей журнала
// модуля: он и ставит настройки инициатора и флага, которые читает функция.
func RequireResourceEventFeedRow(t *testing.T, s ResourceEventStand) {
	t.Helper()
	userID := ids.NewHyphenID(ids.PrefixUser)
	initiator, err := auth.InitiatorOf(operations.Principal{Type: "user", ID: userID})
	if err != nil {
		t.Fatalf("проба НЕ ИСПОЛНЯЛАСЬ: инициатор пробы не выражается: %v", err)
	}
	ctx := operations.WithPrincipal(context.Background(), operations.Principal{Type: "user", ID: userID})
	deleted := changeWord(t, s.Journal, subscriptionv1.SubscriptionEvent_DELETED)
	const project, name = "prj-feedprobe", "data-7"

	write := func(on bool, id string, commit bool) {
		t.Helper()
		tx, err := journaltx.Begin(ctx, s.Pool, journaltx.NewOptions(on))
		if err != nil {
			t.Fatalf("проба НЕ ИСПОЛНЯЛАСЬ: помощник journaltx не открыл транзакцию: %v", err)
		}
		defer func() { _ = tx.Rollback(ctx) }() // после Commit — пустой ход
		st := s.Journal.Storage
		payload, err := json.Marshal(map[string]string{subscription.NamePayloadKey: name})
		if err != nil {
			t.Fatalf("проба НЕ ИСПОЛНЯЛАСЬ: полезная нагрузка: %v", err)
		}
		q := fmt.Sprintf(`INSERT INTO %s (%s, %s, %s, %s, %s) VALUES ($1, $2, $3, $4, $5)`,
			st.Table, st.KindColumn, st.IDColumn, st.ChangeColumn, st.PayloadColumn, st.ProjectColumn)
		if _, err := tx.Exec(ctx, q, s.Kind, id, deleted, payload, project); err != nil {
			t.Fatalf("строка журнала %s %s (флаг %v) отвергнута: %v", s.Kind, id, on, err)
		}
		if !commit {
			return
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("транзакция строки журнала %s (флаг %v) не зафиксирована: %v", id, on, err)
		}
	}

	signalsBefore := s.signals(t)

	onID := "feedprobe-" + ids.NewHyphenID("on")
	write(true, onID, true)
	rows := s.feedRows(t, onID)
	if len(rows) != 1 {
		t.Fatalf("строка журнала %s %s при флаге true дала строк ленты %d, ожидалась 1 — функции resource-event на журнале %s нет либо она ставит не то",
			s.Kind, onID, len(rows), s.Journal.Storage.Table)
	}
	got := rows[0]
	want := feedRow{
		Template: "resource-event", Class: "notice", State: "pending",
		Attrs: map[string]string{
			"kind": s.Kind, "resource_id": onID, "scope": "project:" + project, "change": "DELETED",
			"initiator": initiator.String(), "name": name,
		},
	}
	occurred, ok := got.Attrs["occurred_at"]
	if !ok || occurred == "" {
		t.Errorf("строка ленты %s без атрибута occurred_at: %v", onID, got.Attrs)
	}
	delete(got.Attrs, "occurred_at")
	if !reflect.DeepEqual(got, want) {
		t.Errorf("строка ленты %s:\n got  %v\n want %v", onID, got, want)
	}
	if n := s.signals(t) - signalsBefore; n != 1 {
		t.Errorf("строка журнала при флаге true дала строк сигнала %s:%s %d, ожидалась 1", feed.JournalKey, s.Module, n)
	}

	offID := "feedprobe-" + ids.NewHyphenID("off")
	signalsBefore = s.signals(t)
	write(false, offID, true)
	if rows := s.feedRows(t, offID); len(rows) != 0 {
		t.Errorf("строка журнала при флаге false дала строк ленты %d, ожидалось 0 (NTF3-65)", len(rows))
	}
	if n := s.signals(t) - signalsBefore; n != 0 {
		t.Errorf("строка журнала при флаге false дала строк сигнала %d, ожидалось 0", n)
	}

	rbID := "feedprobe-" + ids.NewHyphenID("rb")
	write(true, rbID, false)
	if rows := s.feedRows(t, rbID); len(rows) != 0 {
		t.Errorf("откаченная строка журнала оставила строк ленты %d, ожидалось 0 (NTF3-69)", len(rows))
	}
}

// changeWord — единственное слово журнала, которое словарь переводит в род c.
func changeWord(t *testing.T, j subscription.Journal, c subscriptionv1.SubscriptionEvent_Change) string {
	t.Helper()
	var words []string
	for w, v := range j.Mapping.Changes {
		if v == c {
			words = append(words, w)
		}
	}
	if len(words) != 1 {
		t.Fatalf("проба НЕ ИСПОЛНЯЛАСЬ: слов журнала %s рода %s %d (%v), нужно одно", j.Storage.Table, c, len(words), words)
	}
	return words[0]
}

// feedRows — строки ленты модуля по идентификатору ресурса.
func (s ResourceEventStand) feedRows(t *testing.T, resourceID string) []feedRow {
	t.Helper()
	q := fmt.Sprintf(`SELECT template, class, state, attrs FROM %s WHERE attrs->>'resource_id' = $1`, s.FeedTable)
	rows, err := s.Pool.Query(context.Background(), q, resourceID)
	if err != nil {
		t.Fatalf("лента модуля %s не читается: %v", s.FeedTable, err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (feedRow, error) {
		var (
			f   feedRow
			raw []byte
		)
		if err := r.Scan(&f.Template, &f.Class, &f.State, &raw); err != nil {
			return f, err
		}
		return f, json.Unmarshal(raw, &f.Attrs)
	})
	if err != nil {
		t.Fatalf("лента модуля %s не читается: %v", s.FeedTable, err)
	}
	return out
}

// signals — строки сигнала ленты модуля в журнале.
func (s ResourceEventStand) signals(t *testing.T) int {
	t.Helper()
	st := s.Journal.Storage
	var n int
	q := fmt.Sprintf(`SELECT count(*) FROM %s WHERE %s = $1 AND %s = $2`, st.Table, st.KindColumn, st.IDColumn)
	if err := s.Pool.QueryRow(context.Background(), q, feed.JournalKey, s.Module).Scan(&n); err != nil {
		t.Fatalf("журнал %s не читается: %v", st.Table, err)
	}
	return n
}
