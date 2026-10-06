// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// volume_feed_flag_uk361_integration_test.go — полоса RED S1-A4 issue-2918
// (NTF-3, Н3-Ф2) для storage: флаг ленты в транзакции писателя журнала равен
// значению, которое корень построил из ручки KACHO_STORAGE_NOTIFICATIONS_ENABLED.
//
// Сценарии (приёмка NTF-3, отпечаток ac1f9fc9…; замысел З4, З11, CX3M-02 (а)):
//   - УК3-61, близнец — «передан NewOptions(<ручка>) → собран, флаг транзакции
//     равен ручке»;
//   - NTF3-65 / NTF3-67 (часть транзакции) — при false и при true писатель тома
//     открывает транзакцию с настройкой `kacho_feed.enabled`, равной флагу
//     модуля; функция базы ленты читает ровно её (З10).
//
// Наблюдение — настройка транзакции в момент вставки строки журнала: на
// `kacho_storage.storage_outbox` пробы ставят свой триггер, переписывающий
// `current_setting('kacho_feed.enabled', true)` в таблицу захвата. Сначала
// КОНТРОЛЬ: транзакция помощника journaltx с NewOptions(true) и (false) пишет
// строку журнала, и захват видит ровно то, что выставлено, — иначе красное
// пробы предмета читалось бы как поломка захвата.
//
// Писатель строится отражением: проба обязана собираться и до, и после того,
// как конструктор получит позиционные Options.

package pg_test

import (
	"context"
	"reflect"
	"strconv"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	coredb "github.com/PRO-Robotech/corelib/db"
	"github.com/PRO-Robotech/corelib/ids"
	"github.com/PRO-Robotech/corelib/journaltx"
	"github.com/PRO-Robotech/corelib/operations"
	"github.com/PRO-Robotech/corelib/pgtest"
	"github.com/PRO-Robotech/kacho/services/storage/internal/repo/pg"
)

// uk361CaptureDDL — захват настройки флага ленты на каждой строке журнала.
const uk361CaptureDDL = `
CREATE TABLE public.uk361_feed_flag_capture (
    seq         bigserial PRIMARY KEY,
    resource_id text NOT NULL,
    flag        text
);
CREATE FUNCTION public.uk361_capture_feed_flag() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO public.uk361_feed_flag_capture (resource_id, flag)
    VALUES (NEW.resource_id, current_setting('kacho_feed.enabled', true));
    RETURN NEW;
END $$;
CREATE TRIGGER uk361_capture_feed_flag AFTER INSERT ON kacho_storage.storage_outbox
    FOR EACH ROW EXECUTE FUNCTION public.uk361_capture_feed_flag();`

type uk361Stand struct {
	pool *pgxpool.Pool
	ctx  context.Context
}

func newUK361Stand(t *testing.T) *uk361Stand {
	t.Helper()
	if testing.Short() {
		t.Skip("integration: testcontainers Postgres")
	}
	userID := ids.NewHyphenID(ids.PrefixUser)
	s := &uk361Stand{
		ctx: operations.WithPrincipal(context.Background(), operations.Principal{Type: "user", ID: userID}),
	}
	dsn := pgtest.NewDB(t)
	// Пул фикстуры — каталог и квоты, не предмет: инициатор параметром старта сессии.
	fixture, err := coredb.NewPool(context.Background(), ntf357WithStartOption(t, dsn, "kacho_journal.initiator=user:"+userID))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, fixture)
	seedFixtureQuotas(t, fixture)
	seedFixtureCatalog(t, fixture)

	s.pool, err = coredb.NewPool(context.Background(), dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, s.pool)
	_, err = s.pool.Exec(context.Background(), uk361CaptureDDL)
	require.NoError(t, err, "ФИКСТУРА: захват настройки флага ленты не установлен")
	return s
}

// captured — значения настройки флага на строках журнала ресурса, по порядку.
func (s *uk361Stand) captured(t *testing.T, resourceID string) []string {
	t.Helper()
	rows, err := s.pool.Query(context.Background(),
		`SELECT coalesce(flag, '<не выставлена>') FROM public.uk361_feed_flag_capture WHERE resource_id = $1 ORDER BY seq`, resourceID)
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

// newVolumeRepoWith строит писателя тома с opts на месте позиционного
// journaltx.Options; ok == false — у конструктора такого параметра нет.
func uk361NewVolumeRepoWith(t *testing.T, pool *pgxpool.Pool, opts journaltx.Options) (*pg.VolumeRepo, bool) {
	t.Helper()
	fn := reflect.ValueOf(pg.NewVolumeRepo)
	typ := fn.Type()
	optsType := reflect.TypeOf(journaltx.Options{})
	poolType := reflect.TypeOf(pool)
	args := make([]reflect.Value, 0, typ.NumIn())
	found := false
	for i := 0; i < typ.NumIn(); i++ {
		switch typ.In(i) {
		case optsType:
			args = append(args, reflect.ValueOf(opts))
			found = true
		case poolType:
			args = append(args, reflect.ValueOf(pool))
		default:
			args = append(args, reflect.Zero(typ.In(i)))
		}
	}
	if !found {
		return nil, false
	}
	out := fn.Call(args)
	if last := out[len(out)-1]; len(out) > 1 && !last.IsNil() {
		t.Fatalf("УК3-61 (близнец): pg.NewVolumeRepo отверг построенные Options: %v", last.Interface())
	}
	repo, ok := out[0].Interface().(*pg.VolumeRepo)
	require.True(t, ok, "ФИКСТУРА: pg.NewVolumeRepo вернул %s", out[0].Type())
	return repo, true
}

// TestUK361_StorageVolumeWriterCarriesTheRootFlagIntoItsTransaction — флаг
// транзакции писателя тома равен Options, построенным корнем (близнецы true, false).
func TestUK361_StorageVolumeWriterCarriesTheRootFlagIntoItsTransaction(t *testing.T) {
	s := newUK361Stand(t)

	// КОНТРОЛЬ: захват видит настройку, выставленную помощником.
	for _, on := range []bool{true, false} {
		id := "vol-uk361ctl" + strconv.FormatBool(on)
		tx, err := journaltx.Begin(s.ctx, s.pool, journaltx.NewOptions(on))
		require.NoError(t, err, "ФИКСТУРА: помощник journaltx не открыл транзакцию")
		_, err = tx.Exec(s.ctx, `INSERT INTO kacho_storage.storage_outbox (resource_kind, resource_id, project_id, event_type, payload)
		                         VALUES ('Volume', $1, 'prj-1', 'CREATED', '{}'::jsonb)`, id)
		require.NoError(t, err, "ФИКСТУРА: строка журнала контроля не вставлена")
		require.NoError(t, tx.Commit(s.ctx))
		require.Equal(t, []string{strconv.FormatBool(on)}, s.captured(t, id),
			"ФИКСТУРА: захват не видит настройку kacho_feed.enabled, выставленную NewOptions(%v)", on)
	}

	for _, on := range []bool{true, false} {
		repo, ok := uk361NewVolumeRepoWith(t, s.pool, journaltx.NewOptions(on))
		if !ok {
			t.Fatalf("УК3-61: storage: конструктор pg.NewVolumeRepo %s не принимает journaltx.Options позиционно — "+
				"флаг транзакции писателя тома не может быть равен ручке модуля", reflect.TypeOf(pg.NewVolumeRepo))
		}
		in := ntf357NewVolume("uk361-" + strconv.FormatBool(on))
		_, _, err := repo.Insert(s.ctx, in, "")
		require.NoError(t, err, "УК3-61: вставка тома писателем с NewOptions(%v) отвергнута", on)
		require.Equal(t, []string{strconv.FormatBool(on)}, s.captured(t, in.ID),
			"УК3-61: строка журнала тома, вставленная писателем с NewOptions(%v), записана в транзакции с иным флагом ленты", on)
	}
}
