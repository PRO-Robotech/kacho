// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// volume_journal_initiator_ntf357_integration_test.go — полоса RED S1-A2
// (issue-2918, NTF-3) для storage: записи тома, сделанные глаголом арендатора,
// несут инициатора принципала контекста. Строку журнала
// `kacho_storage.storage_outbox` пишет функция базы на таблицах `volumes` и
// `volume_attachments`; колонку инициатора она заполняет умолчанием из настройки
// транзакции, которую выставляет только помощник `journaltx`.
//
// Сценарии приёмки NTF-3 (отпечаток ac1f9fc9…):
//   - NTF3-57, ветка storage (`VolumeService.Create` от имени пользователя);
//   - близнец NTF3-58: то же изменение тома, сделанное глаголом арендатора
//     (правка), несёт `user:`;
//   - М4 замысла issue-2918: снятие привязки диска (`Detach`) — автокоммитная
//     запись в журналируемую таблицу — идёт через помощника и несёт инициатора.
//
// ДВА ПУЛА НА ОДНОЙ БАЗЕ. Пул фикстуры выставляет инициатора параметром старта
// сессии: им заводятся том и привязка, которые предметом пробы не являются.
// Пул предмета инициатора не выставляет: всё, что в нём появится, принесёт
// транзакция репозитория. Пул фикстуры — заодно КОНТРОЛЬ: на нём та же вставка
// тома проходит, и красное пробы предмета читается как «транзакция репозитория
// инициатора не выставила», а не как поломка фикстуры (репозиторий отдаёт отказ
// классом, без текста базы).

package pg_test

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	coredb "github.com/PRO-Robotech/corelib/db"
	"github.com/PRO-Robotech/corelib/ids"
	"github.com/PRO-Robotech/corelib/operations"
	"github.com/PRO-Robotech/corelib/pgtest"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/volume"
	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	"github.com/PRO-Robotech/kacho/services/storage/internal/repo/pg"
)

// ntf357Stand — база пробы с двумя пулами и пользователем.
type ntf357Stand struct {
	fixture *pgxpool.Pool // инициатор выставлен параметром старта сессии
	subject *pgxpool.Pool // инициатора не выставляет никто, кроме репозитория
	ctx     context.Context
	want    string
}

func newNTF357Stand(t *testing.T) *ntf357Stand {
	t.Helper()
	if testing.Short() {
		t.Skip("integration: testcontainers Postgres")
	}
	userID := ids.NewHyphenID(ids.PrefixUser)
	s := &ntf357Stand{
		ctx:  operations.WithPrincipal(context.Background(), operations.Principal{Type: "user", ID: userID}),
		want: "user:" + userID,
	}
	dsn := pgtest.NewDB(t)
	var err error
	s.fixture, err = coredb.NewPool(context.Background(), ntf357WithStartOption(t, dsn, "kacho_journal.initiator="+s.want))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, s.fixture)
	s.subject, err = coredb.NewPool(context.Background(), dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, s.subject)

	var n int
	require.NoError(t, s.subject.QueryRow(context.Background(), `
		SELECT count(*) FROM information_schema.columns
		 WHERE table_schema = 'kacho_storage' AND table_name = 'storage_outbox' AND column_name = 'initiator'`).Scan(&n))
	require.Equal(t, 1, n, "ФИКСТУРА: у kacho_storage.storage_outbox нет колонки initiator — миграция S1-A1 не применена")
	seedFixtureQuotas(t, s.fixture)
	seedFixtureCatalog(t, s.fixture)
	return s
}

func ntf357NewVolume(name string) *domain.Volume {
	return &domain.Volume{
		ID:         ids.NewID(domain.PrefixVolume),
		ProjectID:  "prj-1",
		Name:       name,
		ZoneID:     "region-1-a",
		DiskTypeID: seededDiskType,
		SizeBytes:  1 << 30,
	}
}

// journalInitiators — инициаторы строк журнала тома по виду события, по порядку записи.
func (s *ntf357Stand) journalInitiators(t *testing.T, volumeID, eventType string) []string {
	t.Helper()
	rows, err := s.subject.Query(context.Background(), `
		SELECT initiator FROM kacho_storage.storage_outbox
		 WHERE resource_kind = 'Volume' AND resource_id = $1 AND event_type = $2 ORDER BY sequence_no`, volumeID, eventType)
	require.NoError(t, err)
	defer rows.Close()
	var out []string
	for rows.Next() {
		var v string
		require.NoError(t, rows.Scan(&v))
		out = append(out, v)
	}
	require.NoError(t, rows.Err())
	return out
}

// TestVolume_NTF357_ControlFixturePoolAdmitsTheInsert — КОНТРОЛЬ: на пуле с
// инициатором та же вставка тома проходит и строка журнала его несёт.
func TestVolume_NTF357_ControlFixturePoolAdmitsTheInsert(t *testing.T) {
	s := newNTF357Stand(t)
	v, _, err := mustJournalWriter(pg.NewVolumeRepo(s.fixture, probeJournalOptions)).Insert(s.ctx, ntf357NewVolume("ntf357-control"), "")
	require.NoError(t, err, "КОНТРОЛЬ: вставка тома при выставленном инициаторе")
	require.Equal(t, []string{s.want}, s.journalInitiators(t, v.ID, "CREATED"))
}

// TestVolume_NTF357_CreatedByUserCarriesUserInitiator — NTF3-57 (storage).
func TestVolume_NTF357_CreatedByUserCarriesUserInitiator(t *testing.T) {
	s := newNTF357Stand(t)
	in := ntf357NewVolume("ntf357")
	_, _, err := mustJournalWriter(pg.NewVolumeRepo(s.subject, probeJournalOptions)).Insert(s.ctx, in, "")
	require.NoError(t, err,
		"NTF3-57: вставка тома под принципалом %s отвергнута (контроль с тем же вызовом и выставленным инициатором — зелёный)", s.want)
	require.Equal(t, []string{s.want}, s.journalInitiators(t, in.ID, "CREATED"),
		"NTF3-57: CREATED несёт инициатора принципала контекста")
}

// TestVolume_NTF358Twin_UpdatedByUserCarriesUserInitiator — близнец NTF3-58:
// изменение тома глаголом арендатора несёт `user:`.
func TestVolume_NTF358Twin_UpdatedByUserCarriesUserInitiator(t *testing.T) {
	s := newNTF357Stand(t)
	v := mkVolumeCreating(t, mustJournalWriter(pg.NewVolumeRepo(s.fixture, probeJournalOptions)), "prj-1", "ntf358-twin", 1<<30)
	before := len(s.journalInitiators(t, v.ID, "UPDATED"))

	desc := "ntf3-58 twin"
	_, _, err := mustJournalWriter(pg.NewVolumeRepo(s.subject, probeJournalOptions)).Update(s.ctx, v.ID, volume.VolumeUpdate{Description: &desc})
	require.NoError(t, err,
		"NTF3-58 (близнец): правка тома под принципалом %s отвергнута", s.want)
	got := s.journalInitiators(t, v.ID, "UPDATED")
	require.Len(t, got, before+1, "NTF3-58 (близнец): правка пишет ровно одну строку UPDATED")
	require.Equal(t, s.want, got[len(got)-1], "NTF3-58 (близнец): UPDATED глагола арендатора несёт user:")
}

// TestVolume_M4_DetachCarriesPrincipalInitiator — М4 замысла issue-2918: снятие
// привязки — запись в журналируемую таблицу `volume_attachments` — несёт
// инициатора принципала контекста.
func TestVolume_M4_DetachCarriesPrincipalInitiator(t *testing.T) {
	s := newNTF357Stand(t)
	v := mkVolume(t, s.fixture, mustJournalWriter(pg.NewVolumeRepo(s.fixture, probeJournalOptions)), "prj-1", "m4-detach", 1<<30)
	instanceID := ids.NewHyphenID("ins")
	attach(t, s.fixture, v.ID, instanceID)
	before := len(s.journalInitiators(t, v.ID, "UPDATED"))

	err := mustJournalWriter(pg.NewVolumeRepo(s.subject, probeJournalOptions)).Detach(s.ctx, v.ID, instanceID)
	require.NoError(t, err, "М4: снятие привязки под принципалом %s отвергнуто", s.want)
	got := s.journalInitiators(t, v.ID, "UPDATED")
	require.Len(t, got, before+1, "М4: снятие привязки пишет ровно одну строку UPDATED тома")
	require.Equal(t, s.want, got[len(got)-1], "М4: строка снятия привязки несёт инициатора принципала контекста")
}

// ntf357WithStartOption дописывает `-c <настройка>` к параметру старта `options`
// строки подключения, сохраняя уже стоящие там (`search_path` выдающего базу):
// второй параметр `options` заменил бы первый, и контроль молча шёл бы без
// инициатора.
func ntf357WithStartOption(t *testing.T, dsn, setting string) string {
	t.Helper()
	u, err := url.Parse(dsn)
	require.NoError(t, err)
	q := u.Query()
	q.Set("options", strings.TrimSpace(q.Get("options")+" -c "+setting))
	u.RawQuery = q.Encode()
	return u.String()
}
