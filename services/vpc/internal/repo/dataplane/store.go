// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package dataplane — адаптер проекции намерения для исполнителя датаплейна.
//
// Реализует порты, объявленные use-case'ом
// (`internal/apps/kacho/api/dataplane`): чтение курсора и тел, запись
// подтверждения применения, уплотнение снятых намерений.
//
// # Почему тела читаются в ОДНОМ снимке с курсором
//
// Курсор живёт в `kacho_vpc.dataplane_intent`, тела — в таблицах ресурсов.
// Прочитав их порознь, мы получили бы пару, которой ни в один момент не
// существовало: строка, объявленная живой курсором, могла быть удалена между
// двумя чтениями, и исполнителю поехало бы пустое тело под видом объявленного
// намерения. Поэтому обе стороны читаются в ОДНОЙ транзакции уровня
// REPEATABLE READ — снимок один, и рассогласоваться внутри него нечему.
package dataplane

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	uc "github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/api/dataplane"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/helpers"
	kachorepo "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
)

// Store — адаптер проекции намерения над пулом соединений.
type Store struct {
	pool *pgxpool.Pool
}

// New собирает адаптер.
func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Порты, которые этот адаптер обязан выполнять, — ОДНИМ перечнем: порт,
// потерявший реализацию, обязан ронять сборку здесь, а не там, где его первый
// раз позовут.
var (
	_ uc.IntentReader      = (*Store)(nil)
	_ uc.ApplyRecorder     = (*Store)(nil)
	_ uc.PublicStateReader = (*Store)(nil)
)

// boundsSQL — горизонт уплотнения и голова журнала ОДНИМ обращением.
//
// Голова берётся как наибольшее из «максимальной живой ревизии» и горизонта:
// уплотнение могло удалить строку, которая и несла максимум, и без этого
// исполнитель, стоящий ровно на ней, получил бы «такой позиции не бывает» на
// позицию, которую сам от нас же и получил.
const boundsSQL = `
SELECT h.revision AS horizon,
       GREATEST(COALESCE((SELECT max(revision) FROM kacho_vpc.dataplane_intent), 0), h.revision) AS head
  FROM kacho_vpc.dataplane_intent_horizon h
 WHERE h.only_row`

// Bounds возвращает границы журнала намерения.
func (s *Store) Bounds(ctx context.Context) (uc.Bounds, error) {
	var b uc.Bounds
	err := s.pool.QueryRow(ctx, boundsSQL).Scan(&b.Horizon, &b.Head)
	if err != nil {
		return uc.Bounds{}, fmt.Errorf("dataplane: границы журнала намерения: %w", err)
	}
	return b, nil
}

// cursorSQL — страница курсора.
//
// Условие про снятые намерения выражено ВЕЛИЧИНОЙ, а не флагом: при
// продолжении с ненулевой позиции вызывающий передаёт 0, и предикат
// `revision <= 0` ложен для любой строки — то есть не исключается ничто.
// Ветвления в тексте запроса нет, а значит нет и второй его формы, которая
// разошлась бы с первой.
const cursorSQL = `
SELECT revision, resource_id, kind, withdrawn
  FROM kacho_vpc.dataplane_intent
 WHERE revision > $1
   AND NOT (withdrawn AND revision <= $2)
 ORDER BY revision ASC
 LIMIT $3`

// intentTables — вид объекта → таблица, колонки и разбор строки.
//
// Колонки и разбор берутся у ОБЩИХ помощников репозитория, а не выписываются
// здесь: выписанные, они разошлись бы со схемой при первой же правке таблицы, и
// разошлись бы молча — намерение поехало бы с полем, которого уже нет, или без
// поля, которое появилось.
var intentTables = map[uc.Kind]struct {
	table string
	cols  string
}{
	uc.KindNetwork:          {"kacho_vpc.networks", helpers.NetworkCols},
	uc.KindSubnet:           {"kacho_vpc.subnets", helpers.SubnetCols},
	uc.KindNetworkInterface: {"kacho_vpc.network_interfaces", helpers.NICCols},
	uc.KindSecurityGroup:    {"kacho_vpc.security_groups", helpers.SGCols},
	uc.KindRouteTable:       {"kacho_vpc.route_tables", helpers.RouteTableCols},
	uc.KindGateway:          {"kacho_vpc.gateways", helpers.GatewayCols},
	uc.KindAddress:          {"kacho_vpc.addresses", helpers.AddressCols},
}

// Page возвращает страницу намерения вместе с телами.
func (s *Store) Page(ctx context.Context, after, skipWithdrawnUpTo int64, limit int) ([]uc.IntentRow, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return nil, fmt.Errorf("dataplane: снимок журнала намерения: %w", err)
	}
	// Транзакция только читает; откат — её штатное завершение.
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, cursorSQL, after, skipWithdrawnUpTo, limit)
	if err != nil {
		return nil, fmt.Errorf("dataplane: курсор журнала намерения: %w", err)
	}
	out := make([]uc.IntentRow, 0, limit)
	byKind := map[uc.Kind][]string{}
	for rows.Next() {
		var r uc.IntentRow
		var kind string
		if err := rows.Scan(&r.Revision, &r.ResourceID, &kind, &r.Withdrawn); err != nil {
			rows.Close()
			return nil, fmt.Errorf("dataplane: разбор строки курсора: %w", err)
		}
		r.Kind = uc.Kind(kind)
		out = append(out, r)
		if !r.Withdrawn {
			byKind[r.Kind] = append(byKind[r.Kind], r.ResourceID)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("dataplane: обход курсора: %w", err)
	}

	bodies, err := s.loadBodies(ctx, tx, byKind)
	if err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].Withdrawn {
			continue
		}
		attach(&out[i], bodies)
	}
	return out, nil
}

// bodySet — прочитанные тела, по виду и идентификатору.
type bodySet struct {
	networks map[string]*kachorepo.NetworkRecord
	subnets  map[string]*kachorepo.SubnetRecord
	nics     map[string]*kachorepo.NetworkInterfaceRecord
	sgs      map[string]*kachorepo.SecurityGroupRecord
	rts      map[string]*kachorepo.RouteTableRecord
	gws      map[string]*kachorepo.GatewayRecord
	addrs    map[string]*kachorepo.AddressRecord
}

// loadBodies читает тела всех видов ОДНИМ запросом на вид, в той же
// транзакции, что и курсор.
func (s *Store) loadBodies(ctx context.Context, tx pgx.Tx, byKind map[uc.Kind][]string) (*bodySet, error) {
	set := &bodySet{
		networks: map[string]*kachorepo.NetworkRecord{},
		subnets:  map[string]*kachorepo.SubnetRecord{},
		nics:     map[string]*kachorepo.NetworkInterfaceRecord{},
		sgs:      map[string]*kachorepo.SecurityGroupRecord{},
		rts:      map[string]*kachorepo.RouteTableRecord{},
		gws:      map[string]*kachorepo.GatewayRecord{},
		addrs:    map[string]*kachorepo.AddressRecord{},
	}
	for kind, ids := range byKind {
		spec, ok := intentTables[kind]
		if !ok {
			// Вид без таблицы означает, что словарь базы разошёлся с этим
			// перечнем. Отказ, а не пропуск: пропустив, мы отдали бы исполнителю
			// страницу, о полноте которой нечего сказать.
			return nil, fmt.Errorf("dataplane: у вида %q нет таблицы ресурса", kind)
		}
		q := "SELECT " + spec.cols + " FROM " + spec.table + " WHERE id = ANY($1)"
		rows, err := tx.Query(ctx, q, ids)
		if err != nil {
			return nil, fmt.Errorf("dataplane: чтение тел вида %q: %w", kind, err)
		}
		for rows.Next() {
			if err := scanInto(set, kind, rows); err != nil {
				rows.Close()
				return nil, err
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("dataplane: обход тел вида %q: %w", kind, err)
		}
	}
	return set, nil
}

func scanInto(set *bodySet, kind uc.Kind, row helpers.Scannable) error {
	switch kind {
	case uc.KindNetwork:
		rec, err := helpers.ScanNetwork(row)
		if err != nil {
			return err
		}
		set.networks[rec.ID] = rec
	case uc.KindSubnet:
		rec, err := helpers.ScanSubnet(row)
		if err != nil {
			return err
		}
		set.subnets[rec.ID] = rec
	case uc.KindNetworkInterface:
		rec, err := helpers.ScanNI(row)
		if err != nil {
			return err
		}
		set.nics[rec.ID] = rec
	case uc.KindSecurityGroup:
		rec, err := helpers.ScanSG(row)
		if err != nil {
			return err
		}
		set.sgs[rec.ID] = rec
	case uc.KindRouteTable:
		rec, err := helpers.ScanRouteTable(row)
		if err != nil {
			return err
		}
		set.rts[rec.ID] = rec
	case uc.KindGateway:
		rec, err := helpers.ScanGateway(row)
		if err != nil {
			return err
		}
		set.gws[rec.ID] = rec
	case uc.KindAddress:
		rec, err := helpers.ScanAddress(row)
		if err != nil {
			return err
		}
		set.addrs[rec.ID] = rec
	default:
		return fmt.Errorf("dataplane: у вида %q нет разбора строки", kind)
	}
	return nil
}

// attach привязывает прочитанное тело к строке.
//
// Тело, которого не нашлось, оставляет строку БЕЗ тела — и такая строка не
// пройдёт проверку формы у отправителя. Синтезировать вместо неё снятие
// намерения было бы куда хуже: рассогласование проекции (состояние, которого
// при штамповке триггером в одной транзакции с мутацией не бывает) заставило бы
// исполнителя СНЕСТИ живую сеть арендатора.
func attach(row *uc.IntentRow, set *bodySet) {
	switch row.Kind {
	case uc.KindNetwork:
		row.Network = set.networks[row.ResourceID]
	case uc.KindSubnet:
		row.Subnet = set.subnets[row.ResourceID]
	case uc.KindNetworkInterface:
		row.NetworkInterface = set.nics[row.ResourceID]
	case uc.KindSecurityGroup:
		row.SecurityGroup = set.sgs[row.ResourceID]
	case uc.KindRouteTable:
		row.RouteTable = set.rts[row.ResourceID]
	case uc.KindGateway:
		row.Gateway = set.gws[row.ResourceID]
	case uc.KindAddress:
		row.Address = set.addrs[row.ResourceID]
	}
}

// recordSQL — подтверждение применения ОДНИМ стейтментом.
//
// Ни одной проверки «прочитал → сравнил → записал» здесь нет и быть не может:
// два подтверждения по одному объекту приходят одновременно, и раздельные
// чтение и запись дали бы победу последнему писателю независимо от ревизий.
// Условие `a.applied_revision <= EXCLUDED.applied_revision` вычисляется базой
// на заблокированной строке, поэтому старее записанного не запишется никогда, а
// повторный отчёт о ТОЙ ЖЕ ревизии проходит — исполнитель вправе повторить свой
// доклад, и второй раз он может нести уже другой исход.
const recordSQL = `
WITH cur AS (
    SELECT revision FROM kacho_vpc.dataplane_intent WHERE resource_id = $1
), ins AS (
    INSERT INTO kacho_vpc.dataplane_apply AS a (resource_id, applied_revision, outcome, reason, reported_at)
    SELECT $1, $2, $3, $4, now() FROM cur WHERE cur.revision >= $2
    ON CONFLICT (resource_id) DO UPDATE
       SET applied_revision = EXCLUDED.applied_revision,
           outcome          = EXCLUDED.outcome,
           reason           = EXCLUDED.reason,
           reported_at      = EXCLUDED.reported_at
     WHERE a.applied_revision <= EXCLUDED.applied_revision
    RETURNING applied_revision
)
SELECT (SELECT revision FROM cur), (SELECT applied_revision FROM ins)`

// Record записывает подтверждение применения.
func (s *Store) Record(ctx context.Context, rep uc.ApplyReport) (uc.ApplyRecord, error) {
	var current, recorded *int64
	err := s.pool.QueryRow(ctx, recordSQL,
		rep.ResourceID, rep.Revision, string(rep.Outcome), string(rep.Reason),
	).Scan(&current, &recorded)
	if err != nil {
		return uc.ApplyRecord{}, fmt.Errorf("dataplane: запись подтверждения: %w", err)
	}
	switch {
	case current == nil:
		return uc.ApplyRecord{}, uc.ErrIntentUnknown
	case *current < rep.Revision:
		return uc.ApplyRecord{}, uc.ErrRevisionNotIssued
	case recorded == nil:
		// Строка есть, ревизия выдавалась, а запись не состоялась — значит уже
		// записанное подтверждение свежее. Это не отказ, а факт: исполнитель
		// отстал, и ему возвращается действующая ревизия.
		return uc.ApplyRecord{Recorded: false, CurrentRevision: *current}, nil
	default:
		return uc.ApplyRecord{Recorded: true, CurrentRevision: *current}, nil
	}
}

// compactSQL — уплотнение снятых намерений с подъёмом горизонта.
//
// Удаление и подъём горизонта — ОДИН стейтмент: разнеси их, и между ними
// найдётся окно, в котором след уже стёрт, а горизонт ещё не поднят. Поток,
// открытый в этом окне, получил бы «продолжать можно» на позицию, с которой
// продолжать уже нечем, — то есть молча потерял бы снятия.
const compactSQL = `
WITH gone AS (
    DELETE FROM kacho_vpc.dataplane_intent
     WHERE withdrawn
       AND stamped_at < now() - make_interval(secs => $1::double precision)
    RETURNING revision
), bumped AS (
    UPDATE kacho_vpc.dataplane_intent_horizon h
       SET revision = GREATEST(h.revision, COALESCE((SELECT max(revision) FROM gone), 0))
     WHERE h.only_row
    RETURNING h.revision
)
SELECT (SELECT count(*) FROM gone), (SELECT revision FROM bumped)`

// Compact удаляет снятые намерения старше retention и поднимает горизонт.
func (s *Store) Compact(ctx context.Context, retention time.Duration) (int64, int64, error) {
	if retention <= 0 {
		// Нулевой срок хранения стёр бы надгробия в тот же миг, когда они
		// появились, — то есть каждый поток получал бы «начни сначала».
		return 0, 0, errors.New("dataplane: срок хранения снятых намерений обязан быть положительным")
	}
	var removed, horizon int64
	err := s.pool.QueryRow(ctx, compactSQL, retention.Seconds()).Scan(&removed, &horizon)
	if err != nil {
		return 0, 0, fmt.Errorf("dataplane: уплотнение журнала намерения: %w", err)
	}
	return removed, horizon, nil
}
