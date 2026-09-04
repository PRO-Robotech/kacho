// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repo_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/ids"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
	kachopg "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/pg"

	"github.com/PRO-Robotech/kacho/pkg/pgtest"
)

// DB-уровень привязки Subnet→RouteTable после VPC-1 F3/F8.
//
// Редизайн сделал `network.defaultRouteTableId°` ЕДИНСТВЕННЫМ механизмом выбора
// RT для подсети (Network.Create провижнит системную RT в своей writer-TX,
// Subnet.Create подставляет её id, если тенант не задал свой). Оба legacy-
// триггера, которые выбирали RT «за» клиента, сняты:
//
//   - `subnet_auto_pick_rt_trg` (BEFORE INSERT ON subnets, «самая ранняя RT
//     сети») — снят миграцией 0017;
//   - `rt_auto_assoc_subnets_trg` (AFTER INSERT ON route_tables, «усыновить все
//     подсети с route_table_id IS NULL») — снят миграцией 0019.
//
// Проверяем 3 поведения:
//  1. БД сама RT НЕ подставляет и НЕ переклеивает: ни INSERT подсети без
//     route_table_id, ни INSERT новой RT в сеть не меняют привязку. Явное
//     значение сохраняется как есть.
//  2. FK subnets.route_table_id → route_tables(id) ON DELETE SET NULL.
//  3. AFTER UPDATE OF route_table_id ON subnets → outbox-эмит Subnet.UPDATED
//     с payload.auto_association=true (единственный оставшийся DB-driven путь
//     смены route_table_id — FK SET NULL при RT.Delete).
//
// Кто именно становится дефолтом — лочит
// TestIntegration_Subnet_VPC_1_37_AutoAssocUsesDeclaredDefault.
//
// Тестируем напрямую через repo (без service-слоя) — это DB-level гарантия.

func setupAssocRepo(t *testing.T) (kacho.Repository, *pgxpool.Pool, func()) {
	ctx := context.Background()
	dsn := setupTestDB(t)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	r := kachopg.New(pool, nil)
	return r, pool, func() {
		r.Close()
		pool.Close()
	}
}

// requireTriggerAbsent — снятый триггер обязан отсутствовать в каталоге, а не
// «просто не срабатывать»: пока функция+триггер живы, следующая миграция/
// рефактор может нечаянно вернуть DB-выбор RT в обход явного дефолта.
func requireTriggerAbsent(t *testing.T, pool *pgxpool.Pool, name string) {
	t.Helper()
	var cnt int
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT count(*) FROM pg_trigger WHERE tgname = $1`, name).Scan(&cnt))
	require.Zero(t, cnt, "%s обязан быть снят: выбор RT живёт только в Subnet.Create", name)
}

// TestIntegration_VPC_RouteTableInsert_NeverRebindsSubnets — 0019 снял
// `rt_auto_assoc_subnets_trg`: INSERT RouteTable больше НЕ переклеивает подсети
// сети. Привязка ставится ровно один раз, на Subnet.Create, из явного
// `network.defaultRouteTableId°` (VPC-1 F8), и дальше меняется только явной
// мутацией тенанта (Subnet.Update mask=route_table_id) либо FK SET NULL.
//
// Почему усыновление «подсетей с route_table_id IS NULL» пришлось снять, а не
// оставить «безобидным backstop'ом»: оно — второй, невидимый в API механизм
// выбора RT. Достижим он через RT.Delete (FK SET NULL обнуляет и привязку
// подсети, и default сети) — после этого следующий RouteTable.Create молча
// привязывал бы осиротевшие подсети к себе, хотя `defaultRouteTableId°` сети
// пуст. Два конкурирующих механизма выбора RT спека запрещает прямо (F8).
func TestIntegration_VPC_RouteTableInsert_NeverRebindsSubnets(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	r, pool, cleanup := setupAssocRepo(t)
	defer cleanup()

	requireTriggerAbsent(t, pool, "rt_auto_assoc_subnets_trg")
	requireTriggerAbsent(t, pool, "subnet_auto_pick_rt_trg")

	withTx := func(t *testing.T, fn func(kacho.RepositoryWriter) error) error {
		t.Helper()
		w, err := r.Writer(ctx)
		require.NoError(t, err)
		if err := fn(w); err != nil {
			w.Abort()
			return err
		}
		return w.Commit()
	}

	net := &domain.Network{
		ID: ids.NewID(ids.PrefixNetwork), ProjectID: "f-assoc-a", Name: domain.RcNameVPC("net-assoc-a"),
	}
	require.NoError(t, withTx(t, func(w kacho.RepositoryWriter) error {
		_, e := w.Networks().Insert(ctx, net)
		return e
	}))

	// Подсеть-сирота: вставлена напрямую через repo, без route_table_id (так
	// выглядит подсеть, чью RT удалили — FK SET NULL).
	subOrphan := &domain.Subnet{
		ID: ids.NewID(ids.PrefixSubnet), ProjectID: "f-assoc-a", Name: domain.RcNameVPC("sub-assoc-a"), NetworkID: net.ID, PlacementType: domain.PlacementZonal, ZoneID: "zone-a",
		V4CidrBlocks: []string{"10.71.0.0/24"},
	}
	require.NoError(t, withTx(t, func(w kacho.RepositoryWriter) error {
		_, e := w.Subnets().Insert(ctx, subOrphan)
		return e
	}))

	subnetGet := func(id string) *kacho.SubnetRecord {
		rd, err := r.Reader(ctx)
		require.NoError(t, err)
		defer rd.Close()
		got, err := rd.Subnets().Get(ctx, id)
		require.NoError(t, err)
		return got
	}

	require.Empty(t, subnetGet(subOrphan.ID).RouteTableID, "БД сама RT не подставляет")

	rtFirst := &domain.RouteTable{
		ID: ids.NewID(ids.PrefixRouteTable), ProjectID: "f-assoc-a", Name: domain.RcNameVPC("rt-explicit"), NetworkID: net.ID,
	}
	require.NoError(t, withTx(t, func(w kacho.RepositoryWriter) error {
		_, e := w.RouteTables().Insert(ctx, rtFirst)
		return e
	}))

	require.Empty(t, subnetGet(subOrphan.ID).RouteTableID,
		"INSERT RouteTable не усыновляет подсети с NULL route_table_id (0019)")

	// Явно привязанная подсеть тоже не переклеивается следующей RT.
	subBound := &domain.Subnet{
		ID: ids.NewID(ids.PrefixSubnet), ProjectID: "f-assoc-a", Name: domain.RcNameVPC("sub-assoc-bound"), NetworkID: net.ID, PlacementType: domain.PlacementZonal, ZoneID: "zone-a",
		V4CidrBlocks: []string{"10.76.0.0/24"}, RouteTableID: rtFirst.ID,
	}
	require.NoError(t, withTx(t, func(w kacho.RepositoryWriter) error {
		_, e := w.Subnets().Insert(ctx, subBound)
		return e
	}))

	rt2 := &domain.RouteTable{
		ID: ids.NewID(ids.PrefixRouteTable), ProjectID: "f-assoc-a", Name: domain.RcNameVPC("rt-explicit-2"), NetworkID: net.ID,
	}
	require.NoError(t, withTx(t, func(w kacho.RepositoryWriter) error {
		_, e := w.RouteTables().Insert(ctx, rt2)
		return e
	}))

	require.Equal(t, rtFirst.ID, subnetGet(subBound.ID).RouteTableID,
		"existing route_table_id не должен перетираться при INSERT новой RT")
	require.Empty(t, subnetGet(subOrphan.ID).RouteTableID,
		"вторая RT тоже не усыновляет сироту")
}

// TestIntegration_VPC_AutoAssociation_Subnet_NoDBAutoPick — 0017 снял
// `subnet_auto_pick_rt_trg`: INSERT подсети без route_table_id в сети, где RT
// ЕСТЬ, обязан оставить поле пустым (раньше триггер молча подставлял «самую
// раннюю» RT). Выбор дефолта переехал в Subnet.Create и опирается на явный
// `network.defaultRouteTableId°` — недетерминированный DB-выбор ретирован.
// Явно заданный route_table_id по-прежнему сохраняется как есть.
func TestIntegration_VPC_AutoAssociation_Subnet_NoDBAutoPick(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	r, _, cleanup := setupAssocRepo(t)
	defer cleanup()

	withTx := func(t *testing.T, fn func(kacho.RepositoryWriter) error) error {
		t.Helper()
		w, err := r.Writer(ctx)
		require.NoError(t, err)
		if err := fn(w); err != nil {
			w.Abort()
			return err
		}
		return w.Commit()
	}

	net := &domain.Network{
		ID: ids.NewID(ids.PrefixNetwork), ProjectID: "f-assoc-b", Name: domain.RcNameVPC("net-assoc-b"),
	}
	require.NoError(t, withTx(t, func(w kacho.RepositoryWriter) error {
		_, e := w.Networks().Insert(ctx, net)
		return e
	}))

	rtEarly := &domain.RouteTable{
		ID: ids.NewID(ids.PrefixRouteTable), ProjectID: "f-assoc-b", Name: domain.RcNameVPC("rt-early"), NetworkID: net.ID,
	}
	require.NoError(t, withTx(t, func(w kacho.RepositoryWriter) error {
		_, e := w.RouteTables().Insert(ctx, rtEarly)
		return e
	}))

	rtLate := &domain.RouteTable{
		ID: ids.NewID(ids.PrefixRouteTable), ProjectID: "f-assoc-b", Name: domain.RcNameVPC("rt-late"), NetworkID: net.ID,
	}
	require.NoError(t, withTx(t, func(w kacho.RepositoryWriter) error {
		_, e := w.RouteTables().Insert(ctx, rtLate)
		return e
	}))

	sub := &domain.Subnet{
		ID: ids.NewID(ids.PrefixSubnet), ProjectID: "f-assoc-b", Name: domain.RcNameVPC("sub-autopick"), NetworkID: net.ID, PlacementType: domain.PlacementZonal, ZoneID: "zone-a",
		V4CidrBlocks: []string{"10.72.0.0/24"},
	}
	require.NoError(t, withTx(t, func(w kacho.RepositoryWriter) error {
		_, e := w.Subnets().Insert(ctx, sub)
		return e
	}))

	rd, err := r.Reader(ctx)
	require.NoError(t, err)
	subGot, err := rd.Subnets().Get(ctx, sub.ID)
	require.NoError(t, rd.Close())
	require.NoError(t, err)
	require.Empty(t, subGot.RouteTableID,
		"0017: БД больше не подставляет RT сама — auto-pick-триггер снят, выбор делает Subnet.Create")
	_ = rtLate

	subExplicit := &domain.Subnet{
		ID: ids.NewID(ids.PrefixSubnet), ProjectID: "f-assoc-b", Name: domain.RcNameVPC("sub-explicit-late"), NetworkID: net.ID, PlacementType: domain.PlacementZonal, ZoneID: "zone-a",
		V4CidrBlocks: []string{"10.73.0.0/24"}, RouteTableID: rtLate.ID,
	}
	require.NoError(t, withTx(t, func(w kacho.RepositoryWriter) error {
		_, e := w.Subnets().Insert(ctx, subExplicit)
		return e
	}))

	rd2, err := r.Reader(ctx)
	require.NoError(t, err)
	subExplicitGot, err := rd2.Subnets().Get(ctx, subExplicit.ID)
	require.NoError(t, rd2.Close())
	require.NoError(t, err)
	require.Equal(t, rtLate.ID, subExplicitGot.RouteTableID,
		"явно заданный route_table_id сохраняется как есть")
}

func TestIntegration_VPC_AutoAssociation_RT_Delete_FK_SetNull(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	r, _, cleanup := setupAssocRepo(t)
	defer cleanup()

	withTx := func(t *testing.T, fn func(kacho.RepositoryWriter) error) error {
		t.Helper()
		w, err := r.Writer(ctx)
		require.NoError(t, err)
		if err := fn(w); err != nil {
			w.Abort()
			return err
		}
		return w.Commit()
	}

	net := &domain.Network{
		ID: ids.NewID(ids.PrefixNetwork), ProjectID: "f-assoc-c", Name: domain.RcNameVPC("net-assoc-c"),
	}
	require.NoError(t, withTx(t, func(w kacho.RepositoryWriter) error {
		_, e := w.Networks().Insert(ctx, net)
		return e
	}))

	rt := &domain.RouteTable{
		ID: ids.NewID(ids.PrefixRouteTable), ProjectID: "f-assoc-c", Name: domain.RcNameVPC("rt-tobedeleted"), NetworkID: net.ID,
	}
	require.NoError(t, withTx(t, func(w kacho.RepositoryWriter) error {
		_, e := w.RouteTables().Insert(ctx, rt)
		return e
	}))

	// route_table_id задаём ЯВНО: с 0017 БД сама RT не подставляет (auto-pick
	// снят), а предмет этого теста — FK ON DELETE SET NULL, не выбор дефолта.
	sub := &domain.Subnet{
		ID: ids.NewID(ids.PrefixSubnet), ProjectID: "f-assoc-c", Name: domain.RcNameVPC("sub-fk-setnull"), NetworkID: net.ID, PlacementType: domain.PlacementZonal, ZoneID: "zone-a",
		V4CidrBlocks: []string{"10.74.0.0/24"}, RouteTableID: rt.ID,
	}
	require.NoError(t, withTx(t, func(w kacho.RepositoryWriter) error {
		_, e := w.Subnets().Insert(ctx, sub)
		return e
	}))

	rd, err := r.Reader(ctx)
	require.NoError(t, err)
	subBefore, err := rd.Subnets().Get(ctx, sub.ID)
	require.NoError(t, rd.Close())
	require.NoError(t, err)
	require.Equal(t, rt.ID, subBefore.RouteTableID, "precondition: подсеть ссылается на RT")

	// Удаляем RT — FK ON DELETE SET NULL обнулит subnet.route_table_id.
	require.NoError(t, withTx(t, func(w kacho.RepositoryWriter) error {
		return w.RouteTables().Delete(ctx, rt.ID)
	}))

	rd2, err := r.Reader(ctx)
	require.NoError(t, err)
	subAfter, err := rd2.Subnets().Get(ctx, sub.ID)
	require.NoError(t, rd2.Close())
	require.NoError(t, err)
	require.Empty(t, subAfter.RouteTableID,
		"FK ON DELETE SET NULL: subnet.route_table_id должен обнулиться после RT.Delete")
}

// TestIntegration_VPC_AutoAssociation_OutboxEmit_OnTriggeredUpdate —
// `subnets_outbox_emit_route_table_change_trg` (AFTER UPDATE OF route_table_id)
// остаётся: watch-клиент обязан увидеть Subnet.UPDATED и тогда, когда
// route_table_id меняет не service-слой, а сама БД. После 0019 такой путь ровно
// один — FK ON DELETE SET NULL при RouteTable.Delete (усыновление на INSERT RT
// снято), поэтому эмит проверяем именно на нём.
func TestIntegration_VPC_AutoAssociation_OutboxEmit_OnTriggeredUpdate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	dsn := setupTestDB(t)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)
	r := kachopg.New(pool, nil)
	defer r.Close()

	withTx := func(t *testing.T, fn func(kacho.RepositoryWriter) error) error {
		t.Helper()
		w, err := r.Writer(ctx)
		require.NoError(t, err)
		if err := fn(w); err != nil {
			w.Abort()
			return err
		}
		return w.Commit()
	}

	net := &domain.Network{
		ID: ids.NewID(ids.PrefixNetwork), ProjectID: "f-assoc-d", Name: domain.RcNameVPC("net-assoc-d"),
	}
	require.NoError(t, withTx(t, func(w kacho.RepositoryWriter) error {
		_, e := w.Networks().Insert(ctx, net)
		return e
	}))

	rt := &domain.RouteTable{
		ID: ids.NewID(ids.PrefixRouteTable), ProjectID: "f-assoc-d", Name: domain.RcNameVPC("rt-outbox"), NetworkID: net.ID,
	}
	require.NoError(t, withTx(t, func(w kacho.RepositoryWriter) error {
		_, e := w.RouteTables().Insert(ctx, rt)
		return e
	}))

	sub := &domain.Subnet{
		ID: ids.NewID(ids.PrefixSubnet), ProjectID: "f-assoc-d", Name: domain.RcNameVPC("sub-outbox"), NetworkID: net.ID, PlacementType: domain.PlacementZonal, ZoneID: "zone-a",
		V4CidrBlocks: []string{"10.75.0.0/24"}, RouteTableID: rt.ID,
	}
	require.NoError(t, withTx(t, func(w kacho.RepositoryWriter) error {
		_, e := w.Subnets().Insert(ctx, sub)
		return e
	}))

	// snapshot outbox seq до удаления RT.
	var seqBefore int64
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COALESCE(MAX(sequence_no), 0) FROM vpc_outbox`).Scan(&seqBefore))

	require.NoError(t, withTx(t, func(w kacho.RepositoryWriter) error {
		return w.RouteTables().Delete(ctx, rt.ID)
	}))

	// Проверяем что в outbox есть Subnet.UPDATED с auto_association=true.
	var (
		kind, resID, evtType string
		payload              map[string]any
	)
	row := pool.QueryRow(ctx, `
		SELECT resource_kind, resource_id, event_type, payload
		  FROM vpc_outbox
		 WHERE sequence_no > $1
		   AND resource_kind = 'Subnet'
		   AND resource_id = $2
		   AND event_type = 'UPDATED'
		 ORDER BY sequence_no DESC
		 LIMIT 1`, seqBefore, sub.ID)
	require.NoError(t, scanOutboxRow(row, &kind, &resID, &evtType, &payload))
	require.Equal(t, "Subnet", kind)
	require.Equal(t, sub.ID, resID)
	require.Equal(t, "UPDATED", evtType)
	require.Nil(t, payload["route_table_id"],
		"FK SET NULL: в payload уезжает уже обнулённая привязка")
	require.Equal(t, true, payload["auto_association"],
		"triggered emit ставит auto_association=true маркер")

	// ЯКОРЬ ПРОЕКТА у строки, рождённой ТРИГГЕРОМ БАЗЫ, а не кодом.
	//
	// У журнала vpc два производителя, и второй — сама база: этот триггер пишет
	// `Subnet`/`UPDATED` при авто-привязке. Подписка с осью `project_id`
	// отбирает КОЛОНКОЙ, поэтому строка без якоря до подписчика не доедет —
	// тихо, без отказа и без пропуска в нумерации. Обратное заполнение прошлого
	// этого не закрывает: оно закрывает прошлое, а триггер рождает будущее.
	var anchor string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT project_id
		  FROM vpc_outbox
		 WHERE sequence_no > $1
		   AND resource_kind = 'Subnet'
		   AND resource_id = $2
		   AND event_type = 'UPDATED'
		 ORDER BY sequence_no DESC
		 LIMIT 1`, seqBefore, sub.ID).Scan(&anchor))
	require.Equal(t, "f-assoc-d", anchor,
		"строку рождает триггер базы, и якорь проекта обязан стоять колонкой: без него "+
			"подписка с осью project_id это событие не пропустит, и потребитель, снявший "+
			"опрос, не узнает о смене привязки")
}

func scanOutboxRow(row interface {
	Scan(dest ...any) error
}, kind, resID, evtType *string, payload *map[string]any) error {
	var payloadJSON []byte
	if err := row.Scan(kind, resID, evtType, &payloadJSON); err != nil {
		return err
	}
	return json.Unmarshal(payloadJSON, payload)
}
