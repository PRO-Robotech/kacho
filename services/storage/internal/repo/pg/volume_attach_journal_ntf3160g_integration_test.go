// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// volume_attach_journal_ntf3160g_integration_test.go — полоса RED S2-B3
// (issue-2918, NTF-3) для storage: привязка тома к машине и её снятие —
// записи в журналируемую таблицу `volume_attachments` — несут инициатора того,
// кто начал привязку, и дают по строке ленты `resource-event` на строку журнала.
//
// Сценарии приёмки NTF-3 (редакция 42) и заказы замысла issue-2918:
//   - NTF3-160 (г): `Attach` и `Detach` тома `vol-11` под принципалом `usr-A` —
//     строк `Volume UPDATED` по тому после P0 две, обе `user:usr-A`; строк журнала
//     после P0 с инициатором вида `service:` или `system:` — 0; каждой строке
//     журнала — ровно одна строка ленты с теми же пятью полями, лишних нет;
//   - близнец (г): то же с `vol-12` под `usr-B` — инициатор `user:usr-B`, строк
//     `user:usr-A` по `vol-12` — 0 (один изменённый факт — начавший);
//   - УК3-32 (CX3D-05): повтор `Attach`/`Detach` после зафиксированной записи
//     (обрыв ответа storage, compute повторяет) лишних строк журнала и ленты не
//     даёт; положительный близнец — первый вызов каждой пары даёт ровно одну.
//
// Пересылка от вызывающего доезжает до репозитория принципалом контекста:
// storage-листенер ставит в контекст личность, которую переслал доверенный
// отправитель (compute — `auth.PropagateOutgoing`), а репозиторий берёт
// инициатора ТОЛЬКО оттуда (journaltx). Поэтому предмет здесь — исход записи
// под заданным принципалом, а не текст заголовка.
//
// ДВА ПУЛА НА ОДНОЙ БАЗЕ (как в ntf357): фикстура заводит тома пулом с
// инициатором и выключенной лентой, предмет пишет пулом без умолчаний — всё,
// что в нём появится, принесёт транзакция репозитория.

package pg_test

import (
	"context"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/corelib/ids"
	"github.com/PRO-Robotech/corelib/journaltx"
	"github.com/PRO-Robotech/corelib/operations"
	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	"github.com/PRO-Robotech/kacho/services/storage/internal/repo/pg"
)

// ntf3160gFeedOn — Options писателя предмета: лента включена, иначе строки
// ленты не появятся и соответствие «журнал ↔ лента» вакуумно.
var ntf3160gFeedOn = journaltx.NewOptions(true)

// ntf3160gMark — позиция журнала и множество строк ленты до вызова When.
type ntf3160gMark struct {
	seq    int64
	feedID []string
}

// ntf3160gRow — пять полей, которые строка ленты обязана повторить за строкой журнала.
type ntf3160gRow struct {
	Kind, ResourceID, Change, Initiator, OccurredAt string
}

func (s *ntf357Stand) ntf3160gMarkNow(t *testing.T) ntf3160gMark {
	t.Helper()
	var m ntf3160gMark
	require.NoError(t, s.subject.QueryRow(context.Background(),
		`SELECT COALESCE(max(sequence_no), 0) FROM kacho_storage.storage_outbox`).Scan(&m.seq))
	require.NoError(t, s.subject.QueryRow(context.Background(),
		`SELECT COALESCE(array_agg(id), '{}') FROM kacho_storage.storage_notification_outbox`).Scan(&m.feedID))
	return m
}

// journalAfter — строки журнала ресурсов (без строки-будильника `notification`) после P0.
func (s *ntf357Stand) ntf3160gJournalAfter(t *testing.T, m ntf3160gMark) []ntf3160gRow {
	t.Helper()
	rows, err := s.subject.Query(context.Background(), `
		SELECT resource_kind, resource_id, event_type, initiator,
		       to_char(date_trunc('second', created_at) AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
		  FROM kacho_storage.storage_outbox
		 WHERE sequence_no > $1 AND resource_kind <> 'notification'
		 ORDER BY sequence_no`, m.seq)
	require.NoError(t, err)
	defer rows.Close()
	var out []ntf3160gRow
	for rows.Next() {
		var r ntf3160gRow
		require.NoError(t, rows.Scan(&r.Kind, &r.ResourceID, &r.Change, &r.Initiator, &r.OccurredAt))
		out = append(out, r)
	}
	require.NoError(t, rows.Err())
	return out
}

// feedAfter — строки ленты `resource-event`, появившиеся после P0.
func (s *ntf357Stand) ntf3160gFeedAfter(t *testing.T, m ntf3160gMark) []ntf3160gRow {
	t.Helper()
	rows, err := s.subject.Query(context.Background(), `
		SELECT attrs->>'kind', attrs->>'resource_id', attrs->>'change', attrs->>'initiator', attrs->>'occurred_at'
		  FROM kacho_storage.storage_notification_outbox
		 WHERE template = 'resource-event' AND NOT (id = ANY($1))`, m.feedID)
	require.NoError(t, err)
	defer rows.Close()
	var out []ntf3160gRow
	for rows.Next() {
		var r ntf3160gRow
		require.NoError(t, rows.Scan(&r.Kind, &r.ResourceID, &r.Change, &r.Initiator, &r.OccurredAt))
		out = append(out, r)
	}
	require.NoError(t, rows.Err())
	return out
}

// foreignInitiatorsAfter — число строк журнала после P0 (включая служебные) с
// инициатором вида `service:` или `system:`.
func (s *ntf357Stand) ntf3160gForeignInitiatorsAfter(t *testing.T, m ntf3160gMark) int {
	t.Helper()
	var n int
	require.NoError(t, s.subject.QueryRow(context.Background(), `
		SELECT count(*) FROM kacho_storage.storage_outbox
		 WHERE sequence_no > $1 AND (initiator LIKE 'service:%' OR initiator LIKE 'system:%')`, m.seq).Scan(&n))
	return n
}

func ntf3160gSorted(in []ntf3160gRow) []ntf3160gRow {
	out := append([]ntf3160gRow(nil), in...)
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.ResourceID != b.ResourceID {
			return a.ResourceID < b.ResourceID
		}
		if a.Change != b.Change {
			return a.Change < b.Change
		}
		if a.Initiator != b.Initiator {
			return a.Initiator < b.Initiator
		}
		return a.OccurredAt < b.OccurredAt
	})
	return out
}

// volumeUpdatedBy — инициаторы строк `Volume UPDATED` тома в выборке.
func ntf3160gVolumeUpdated(rows []ntf3160gRow, volumeID string) []string {
	var out []string
	for _, r := range rows {
		if r.Kind == "Volume" && r.ResourceID == volumeID && r.Change == "UPDATED" {
			out = append(out, r.Initiator)
		}
	}
	return out
}

func ntf3160gAttachment(v *domain.Volume, instanceID string) *domain.VolumeAttachment {
	return &domain.VolumeAttachment{
		VolumeID:     v.ID,
		InstanceID:   instanceID,
		InstanceName: "ins-ntf3160g",
		ProjectID:    v.ProjectID,
		ZoneID:       v.ZoneID,
		DeviceName:   "sdb",
		Mode:         domain.AttachmentModeReadWrite,
	}
}

// ntf3160gAttachDetachAs — сценарий (г) для одного начавшего: том готов до P0,
// затем Attach и Detach предметом под принципалом начавшего. Возвращает выборки
// журнала и ленты после P0 и число строк с чужим инициатором.
func ntf3160gAttachDetachAs(t *testing.T, s *ntf357Stand, starter operations.Principal, volName string) (string, []ntf3160gRow, []ntf3160gRow, int) {
	t.Helper()
	v := mkVolume(t, s.fixture, mustJournalWriter(pg.NewVolumeRepo(s.fixture, probeJournalOptions)), "prj-1", volName, 1<<30)
	instanceID := ids.NewHyphenID("ins")
	subject := mustJournalWriter(pg.NewVolumeRepo(s.subject, ntf3160gFeedOn))
	ctx := operations.WithPrincipal(context.Background(), starter)

	p0 := s.ntf3160gMarkNow(t)
	require.NoError(t, subject.Attach(ctx, ntf3160gAttachment(v, instanceID)),
		"NTF3-160 (г): привязка %s под принципалом %s:%s отвергнута", v.ID, starter.Type, starter.ID)
	require.NoError(t, subject.Detach(ctx, v.ID, instanceID),
		"NTF3-160 (г): снятие привязки %s под принципалом %s:%s отвергнуто", v.ID, starter.Type, starter.ID)
	return v.ID, s.ntf3160gJournalAfter(t, p0), s.ntf3160gFeedAfter(t, p0), s.ntf3160gForeignInitiatorsAfter(t, p0)
}

// TestVolume_NTF3160g_AttachDetachCarryStarterInitiatorAndFeed — NTF3-160 (г).
func TestVolume_NTF3160g_AttachDetachCarryStarterInitiatorAndFeed(t *testing.T) {
	s := newNTF357Stand(t)
	usrA := operations.Principal{Type: "user", ID: ids.NewHyphenID(ids.PrefixUser)}
	wantA := "user:" + usrA.ID

	volID, journal, feed, foreign := ntf3160gAttachDetachAs(t, s, usrA, "ntf3160g-vol-11")

	require.Equal(t, []string{wantA, wantA}, ntf3160gVolumeUpdated(journal, volID),
		"NTF3-160 (г): строк журнала Volume UPDATED по %s после P0 — две (привязка и её снятие), обе с инициатором начавшего", volID)
	require.Equal(t, 0, foreign,
		"NTF3-160 (г): строк журнала storage после P0 с инициатором service: или system: — 0")
	require.Equal(t, ntf3160gSorted(journal), ntf3160gSorted(feed),
		"NTF3-160 (г): каждой строке журнала после P0 — ровно одна строка resource-event с теми же kind, resource_id, change, initiator, occurred_at; лишних строк ленты нет")
	require.Equal(t, []string{wantA, wantA}, ntf3160gVolumeUpdated(feed, volID),
		"NTF3-160 (г): строк ленты kind=Volume resource_id=%s change=UPDATED initiator=%s — ровно две", volID, wantA)
}

// TestVolume_NTF3160gTwin_OtherStarterOwnInitiator — близнец (г): тот же путь
// начал `usr-B` (единственный изменённый факт).
func TestVolume_NTF3160gTwin_OtherStarterOwnInitiator(t *testing.T) {
	s := newNTF357Stand(t)
	usrA := operations.Principal{Type: "user", ID: ids.NewHyphenID(ids.PrefixUser)}
	usrB := operations.Principal{Type: "user", ID: ids.NewHyphenID(ids.PrefixUser)}
	wantB := "user:" + usrB.ID

	volID, journal, feed, foreign := ntf3160gAttachDetachAs(t, s, usrB, "ntf3160g-vol-12")

	require.Equal(t, []string{wantB, wantB}, ntf3160gVolumeUpdated(journal, volID),
		"NTF3-160 (г) близнец: строк Volume UPDATED по %s — две, обе с инициатором %s", volID, wantB)
	for _, r := range journal {
		require.NotEqual(t, "user:"+usrA.ID, r.Initiator, "NTF3-160 (г) близнец: строк с инициатором usr-A по %s — 0", volID)
	}
	require.Equal(t, 0, foreign, "NTF3-160 (г) близнец: строк с инициатором service: или system: — 0")
	require.Equal(t, ntf3160gSorted(journal), ntf3160gSorted(feed),
		"NTF3-160 (г) близнец: строке журнала — ровно одна строка ленты, лишних нет")
}

// TestVolume_UK332_ReplayAfterCommittedWriteAddsNoRows — УК3-32 (CX3D-05):
// повтор Attach и Detach после зафиксированной записи — тот самый повтор, который
// делает compute (retry.OnUnavailable) при обрыве ответа storage, — лишних строк
// журнала и ленты не даёт. Положительный близнец в той же пробе: первый вызов
// каждой пары даёт ровно одну строку, иначе «ноль лишних» был бы вакуумен.
func TestVolume_UK332_ReplayAfterCommittedWriteAddsNoRows(t *testing.T) {
	s := newNTF357Stand(t)
	usr := operations.Principal{Type: "user", ID: ids.NewHyphenID(ids.PrefixUser)}
	want := "user:" + usr.ID
	ctx := operations.WithPrincipal(context.Background(), usr)

	v := mkVolume(t, s.fixture, mustJournalWriter(pg.NewVolumeRepo(s.fixture, probeJournalOptions)), "prj-1", "uk332-vol", 1<<30)
	instanceID := ids.NewHyphenID("ins")
	subject := mustJournalWriter(pg.NewVolumeRepo(s.subject, ntf3160gFeedOn))
	a := ntf3160gAttachment(v, instanceID)
	p0 := s.ntf3160gMarkNow(t)

	// Attach: первый — одна строка (положительный близнец), повтор — ни одной.
	require.NoError(t, subject.Attach(ctx, a), "УК3-32: первая привязка")
	require.Equal(t, []string{want}, ntf3160gVolumeUpdated(s.ntf3160gJournalAfter(t, p0), v.ID),
		"УК3-32 (близнец): первая привязка пишет ровно одну строку Volume UPDATED")
	require.NoError(t, subject.Attach(ctx, a), "УК3-32: повтор привязки после зафиксированной записи — идемпотентный успех")
	require.Equal(t, []string{want}, ntf3160gVolumeUpdated(s.ntf3160gJournalAfter(t, p0), v.ID),
		"УК3-32: повтор привязки после зафиксированной записи — лишних строк журнала 0")

	// Detach: первый — одна строка, повтор — ни одной.
	require.NoError(t, subject.Detach(ctx, v.ID, instanceID), "УК3-32: первое снятие")
	require.Equal(t, []string{want, want}, ntf3160gVolumeUpdated(s.ntf3160gJournalAfter(t, p0), v.ID),
		"УК3-32 (близнец): первое снятие пишет ровно одну строку Volume UPDATED")
	require.NoError(t, subject.Detach(ctx, v.ID, instanceID), "УК3-32: повтор снятия — идемпотентный успех")
	journal := s.ntf3160gJournalAfter(t, p0)
	require.Equal(t, []string{want, want}, ntf3160gVolumeUpdated(journal, v.ID),
		"УК3-32: повтор снятия после зафиксированной записи — лишних строк журнала 0")
	require.Equal(t, ntf3160gSorted(journal), ntf3160gSorted(s.ntf3160gFeedAfter(t, p0)),
		"УК3-32: строк ленты ровно столько же, сколько строк журнала, — лишних строк ленты 0")
}
