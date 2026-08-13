// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/PRO-Robotech/kacho/pkg/filter"
	"github.com/PRO-Robotech/kacho/pkg/validate"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/helpers"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
)

// routeTableReader — Get/List поверх произвольной pgx.Tx (read-only или RW).
// RouteTable построен на CQRS, чтобы Network.Delete мог в одной writer-TX
// проверять child-RT и/или (опционально) удалять — единообразно с Network/SG.
// SQL-семантика опирается на общие shim'ы: `helpers.RouteTableCols` /
// `helpers.ScanRouteTable` / `helpers.MarshalStaticRoutes`.
//
// Здесь стояло предупреждение про DB-триггер, который на вставке таблицы
// «усыновлял» подсети сети без таблицы. Такого триггера в дереве НЕТ: он снят
// миграцией 0019 — именно потому, что давал второй, невидимый в API способ
// выбрать таблицу подсети, зависящий от порядка создания. Остался один: привязка
// ставится на создании ПОДСЕТИ (явный `route_table_id` либо
// `networks.default_route_table_id`).
type routeTableReader struct {
	tx pgx.Tx
}

// Get — well-formed-but-absent → NotFound с "Route table <id> not found".
func (r *routeTableReader) Get(ctx context.Context, id string) (*kacho.RouteTableRecord, error) {
	q := fmt.Sprintf(`SELECT %s FROM route_tables WHERE id = $1`, helpers.RouteTableCols)
	row := r.tx.QueryRow(ctx, q, id)
	rt, err := helpers.ScanRouteTable(row)
	if err != nil {
		return nil, helpers.WrapPgErr(err, "Route table", id)
	}
	return rt, nil
}

// List — cursor-based pagination + filter.Parse (whitelist `["name"]`).
func (r *routeTableReader) List(ctx context.Context, f kacho.RouteTableFilter, p kacho.Pagination) ([]*kacho.RouteTableRecord, string, error) {
	pageSize, err := validate.PageSize("page_size", p.PageSize)
	if err != nil {
		return nil, "", err
	}

	args := []any{}
	conditions := []string{}
	argIdx := 1

	if f.ProjectID != "" {
		conditions = append(conditions, fmt.Sprintf("project_id = $%d", argIdx))
		args = append(args, f.ProjectID)
		argIdx++
	}
	if f.NetworkID != "" {
		conditions = append(conditions, fmt.Sprintf("network_id = $%d", argIdx))
		args = append(args, f.NetworkID)
		argIdx++
	}
	if f.Name != "" {
		conditions = append(conditions, fmt.Sprintf("name = $%d", argIdx))
		args = append(args, f.Name)
		argIdx++
	}
	if f.Filter != "" {
		// `network_id` — контрактное поле и настоящая колонка. Оно ОБЯЗАТЕЛЬНО:
		// без него снятие `NetworkService.ListRouteTables` отняло бы возможность
		// «таблицы этой сети», не дав замены. У подсети и группы правил такое
		// сужение уже есть, у таблиц маршрутизации его не было.
		ast, perr := filter.Parse(f.Filter, []string{"name", "network_id"})
		if perr != nil {
			return nil, "", helpers.InvalidFilterErr(perr)
		}
		if ast != nil {
			frag, fargs := ast.ToSQL(argIdx)
			conditions = append(conditions, frag)
			args = append(args, fargs...)
			argIdx += len(fargs)
		}
	}
	if p.PageToken != "" {
		ts, id, derr := helpers.DecodePageToken(p.PageToken)
		if derr != nil {
			return nil, "", helpers.InvalidPageTokenErr(derr)
		}
		conditions = append(conditions, fmt.Sprintf("(created_at, id) > ($%d, $%d)", argIdx, argIdx+1))
		args = append(args, ts, id)
		argIdx += 2
	}

	var where string
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}
	q := fmt.Sprintf(`SELECT %s FROM route_tables %s ORDER BY created_at ASC, id ASC LIMIT $%d`, helpers.RouteTableCols, where, argIdx)
	args = append(args, pageSize+1)

	rows, err := r.tx.Query(ctx, q, args...)
	if err != nil {
		return nil, "", helpers.WrapPgErr(err, "Route table", "")
	}
	defer rows.Close()

	var result []*kacho.RouteTableRecord
	for rows.Next() {
		rt, err := helpers.ScanRouteTable(rows)
		if err != nil {
			return nil, "", helpers.WrapPgErr(err, "Route table", "")
		}
		result = append(result, rt)
	}
	if err := rows.Err(); err != nil {
		return nil, "", helpers.WrapPgErr(err, "Route table", "")
	}

	var nextToken string
	if int64(len(result)) > pageSize {
		last := result[pageSize-1]
		nextToken = helpers.EncodePageToken(last.CreatedAt, last.ID)
		result = result[:pageSize]
	}
	return result, nextToken, nil
}

// ListByNetwork — узкий read для checkNetworkEmpty / ListRouteTables.
// Реализован поверх List с filter NetworkID — экономит дублирование SQL.
func (r *routeTableReader) ListByNetwork(ctx context.Context, networkID string, p kacho.Pagination) ([]*kacho.RouteTableRecord, string, error) {
	return r.List(ctx, kacho.RouteTableFilter{NetworkID: networkID}, p)
}

// routeTableWriter — DML над route_tables через writer-TX. Embeds
// routeTableReader (writer видит свои writes).
//
// Writer НЕ emit'ит outbox самостоятельно: после успешного DML caller (use-case)
// вызывает `RepositoryWriter.Outbox().Emit(...)` — outbox-write виден прямо в
// use-case-коде, а не «из глубины» repo.
//
// Вставка таблицы подсети НЕ трогает (триггер-«усыновитель» снят миграцией 0019 —
// см. комментарий у routeTableReader). Единственное, что writer делает помимо
// самой строки, — приводит в соответствие нормализованные ссылки «маршрут →
// шлюз» (`syncGatewayRefs`), потому что набор маршрутов и его ссылки обязаны
// писаться одним вызывающим в одной TX.
type routeTableWriter struct {
	routeTableReader
}

// Insert — INSERT route_tables RETURNING. CreatedAt проставляется явно (UTC) для
// детерминизма в тестах. outbox-write — в use-case'е.
func (w *routeTableWriter) Insert(ctx context.Context, rt *domain.RouteTable) (*kacho.RouteTableRecord, error) {
	labelsJSON, err := helpers.MarshalJSONB(domain.LabelsToMap(rt.Labels), "RouteTable.labels")
	if err != nil {
		return nil, err
	}
	routesJSON, err := helpers.MarshalStaticRoutes(rt.StaticRoutes)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	q := fmt.Sprintf(`
		INSERT INTO route_tables (id, project_id, created_at, name, description, labels, network_id, static_routes)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING %s`, helpers.RouteTableCols)

	row := w.tx.QueryRow(ctx, q,
		rt.ID, rt.ProjectID, now, string(rt.Name), string(rt.Description), labelsJSON,
		rt.NetworkID, routesJSON,
	)
	result, err := helpers.ScanRouteTable(row)
	if err != nil {
		return nil, helpers.WrapPgErr(err, "Route table", string(rt.Name))
	}
	// Ссылки «маршрут → шлюз» пишутся тем же вызывающим и в той же writer-TX, что
	// сам набор маршрутов: иначе набор в JSONB и нормализованные ссылки — два места
	// об одном факте, которые разойдутся при первой же ошибке на полпути.
	if err := w.syncGatewayRefs(ctx, rt.ID, rt.StaticRoutes); err != nil {
		return nil, err
	}
	return result, nil
}

// Update — UPDATE route_tables RETURNING. Мутирует name/description/labels/
// static_routes; network_id immutable.
//
// outbox-write — в use-case'е (см. Insert).
func (w *routeTableWriter) Update(ctx context.Context, rt *domain.RouteTable) (*kacho.RouteTableRecord, error) {
	labelsJSON, err := helpers.MarshalJSONB(domain.LabelsToMap(rt.Labels), "RouteTable.labels")
	if err != nil {
		return nil, err
	}
	routesJSON, err := helpers.MarshalStaticRoutes(rt.StaticRoutes)
	if err != nil {
		return nil, err
	}

	q := fmt.Sprintf(`
		UPDATE route_tables SET name=$2, description=$3, labels=$4, static_routes=$5
		WHERE id=$1
		RETURNING %s`, helpers.RouteTableCols)
	row := w.tx.QueryRow(ctx, q,
		rt.ID, string(rt.Name), string(rt.Description), labelsJSON, routesJSON,
	)
	result, err := helpers.ScanRouteTable(row)
	if err != nil {
		return nil, helpers.WrapPgErr(err, "Route table", rt.ID)
	}
	if err := w.syncGatewayRefs(ctx, rt.ID, rt.StaticRoutes); err != nil {
		return nil, err
	}
	return result, nil
}

// syncGatewayRefs приводит нормализованные ссылки «маршрут → шлюз» к набору
// маршрутов, который только что записан, и ЭТИМ ЖЕ решает, законен ли набор.
//
// Почему ссылка нормализована, а не читается из JSONB на месте: существование
// шлюза обязан держать внешний ключ. Коррелированный подзапрос на `gateways`
// строк не лочит, поэтому «прочитал шлюз → записал маршрут» и параллельное
// удаление шлюза оба закоммитились бы, оставив маршрут, указывающий в никуда. FK
// эту гонку закрывает by construction и даёт обратное направление: шлюз,
// названный живым маршрутом, не удаляется.
//
// Набор заменяется целиком (снять всё → вставить заново), потому что у
// статического маршрута нет собственной идентичности: аддитивного глагола у
// набора нет, Create и Update несут ИТОГ. Позиция в наборе — то единственное,
// чем маршрут адресуется, и она же стоит в тексте отказа.
//
// Вставка идёт ПО ОДНОМУ маршруту, а не одним оператором на весь набор: только
// так отказ адресуем — вызывающий получает индекс СВОЕГО маршрута, а не «где-то в
// наборе». Число операторов ограничено потолком набора (domain.MaxStaticRoutes),
// и платит за него мутация, а не чтение.
func (w *routeTableWriter) syncGatewayRefs(ctx context.Context, rtID string, routes []domain.StaticRoute) error {
	if _, err := w.tx.Exec(ctx,
		`DELETE FROM route_table_gateway_refs WHERE route_table_id = $1`, rtID); err != nil {
		return helpers.WrapPgErr(err, "Route table", rtID)
	}
	for i, r := range routes {
		if r.GatewayID == "" {
			continue
		}
		family := helpers.PrefixFamily(r.DestinationPrefix)
		if family == 0 {
			// Сюда не доходит вход, прошедший validateStaticRoutes (назначение
			// проверено как CIDR раньше). Но writer вправе вызываться и другим
			// путём, а ссылка без семейства не сверяется с видом шлюза — то есть
			// проверка стала бы тождественно истинной. Отказ, а не пропуск.
			return fmt.Errorf("%w: static_routes[%d].destination_prefix must be a valid CIDR",
				helpers.ErrInvalidArg, i)
		}
		if err := w.insertGatewayRef(ctx, rtID, i, r.GatewayID, family); err != nil {
			return err
		}
	}
	return nil
}

// insertGatewayRef — одна ссылка, один оператор, и он же — ВСЯ проверка.
//
// Предикат отсекает строку ровно в трёх случаях, и порядок ветвей повторён в
// классификаторе сообщения:
//
//   - якорь шлюза лежит в ДРУГОЙ сети, чем эта таблица маршрутизации;
//   - вид шлюза не обслуживает семейство назначения маршрута;
//   - размещение не когерентно: и подсеть-якорь шлюза, и хотя бы одна подсеть,
//     пользующаяся этой таблицей, ЗОНАЛЬНЫ, и зоны у них разные. Региональная
//     (anycast) подсеть с любой стороны зоны не несёт, поэтому из зональной сверки
//     исключена BY CONSTRUCTION — сравнивать не с чем, а не «сравнение пропущено».
//
// Отсутствующий шлюз предикатом НЕ отсекается намеренно: внутренний EXISTS на нём
// ложен, строка вставляется, и её отвергает внешний ключ. Так существование
// держит FK, а не проверка перед записью.
func (w *routeTableWriter) insertGatewayRef(ctx context.Context, rtID string, idx int, gwID string, family int) error {
	tag, err := w.tx.Exec(ctx, `
		INSERT INTO route_table_gateway_refs (route_table_id, route_index, gateway_id, destination_family)
		SELECT $1, $2, $3, $4
		 WHERE NOT EXISTS (
		   SELECT 1
		     FROM gateways g
		     JOIN subnets gs ON gs.id = g.subnet_id
		    WHERE g.id = $3
		      AND ( gs.network_id <> (SELECT network_id FROM route_tables WHERE id = $1)
		         -- Литералы семейства приведены к типу колонки, а не наоборот: без
		         -- этого один и тот же параметр выводится как smallint в списке
		         -- вставки и как integer в сравнении, и планировщик отказывается
		         -- (42P08) — то есть отказ был бы у КАЖДОГО входа, включая законный.
		         OR ($4 = 4::smallint AND g.gateway_type <> 'NAT')
		         OR ($4 = 6::smallint AND g.gateway_type <> 'EGRESS_ONLY')
		         OR EXISTS ( SELECT 1 FROM subnets s
		                      WHERE s.route_table_id = $1
		                        AND s.placement_type = 'ZONAL'
		                        AND gs.placement_type = 'ZONAL'
		                        AND s.zone_id <> gs.zone_id ) )
		 )`, rtID, idx, gwID, family)
	if err != nil {
		if helpers.IsFKViolationOn(err, helpers.RouteRefGatewayFKConstraint) {
			return fmt.Errorf("%w: Gateway %s not found", helpers.ErrNotFound, gwID)
		}
		return helpers.WrapPgErr(err, "Route table", rtID)
	}
	if tag.RowsAffected() == 1 {
		return nil
	}
	return w.classifyGatewayRefRejection(ctx, rtID, idx, gwID, family)
}

// classifyGatewayRefRejection — СООБЩЕНИЕ для отвергнутой ссылки. Решение принял
// оператор вставки; здесь выясняется, что сказать вызывающему, и адрес отказа —
// индекс его собственного маршрута.
//
// Ветвь «причина больше не видна» — не корзина «прочее»: следующий оператор той
// же транзакции получает новый снимок (READ COMMITTED), поэтому подсеть могла
// отцепиться от таблицы между вставкой и этим чтением. Тогда честно сказать
// ровно то, что известно, и не выдумывать причину.
func (w *routeTableWriter) classifyGatewayRefRejection(
	ctx context.Context, rtID string, idx int, gwID string, family int,
) error {
	field := fmt.Sprintf("static_routes[%d].gateway_id", idx)
	var sameNetwork bool
	var gwZone, gwType string
	var conflictingZone *string
	err := w.tx.QueryRow(ctx, `
		SELECT gs.network_id = (SELECT network_id FROM route_tables WHERE id = $1),
		       gs.zone_id,
		       g.gateway_type,
		       (SELECT s.zone_id FROM subnets s
		         WHERE s.route_table_id = $1
		           AND s.placement_type = 'ZONAL'
		           AND gs.placement_type = 'ZONAL'
		           AND s.zone_id <> gs.zone_id
		         LIMIT 1)
		  FROM gateways g
		  JOIN subnets gs ON gs.id = g.subnet_id
		 WHERE g.id = $2`, rtID, gwID).Scan(&sameNetwork, &gwZone, &gwType, &conflictingZone)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: Gateway %s not found", helpers.ErrNotFound, gwID)
	}
	if err != nil {
		return helpers.WrapPgErr(err, "Gateway", gwID)
	}
	switch {
	case !sameNetwork:
		return fmt.Errorf("%w: %s: Gateway %s is attached to another network",
			helpers.ErrFailedPrecondition, field, gwID)
	case domain.GatewayType(gwType).IPFamily() != family:
		return fmt.Errorf("%w: %s: Gateway %s does not serve IPv%d destinations",
			helpers.ErrFailedPrecondition, field, gwID, family)
	case conflictingZone != nil:
		return fmt.Errorf("%w: %s: Gateway is in zone %s, route table subnet zone is %s",
			helpers.ErrFailedPrecondition, field, gwZone, *conflictingZone)
	}
	return fmt.Errorf("%w: %s: Gateway %s can not serve this route",
		helpers.ErrFailedPrecondition, field, gwID)
}

// GetForUpdate — Get с row-lock (`FOR UPDATE`) в writer-TX. Сериализует
// конкурентный read-modify-write в Update: второй concurrent Update блокируется
// на GetForUpdate до commit первого, затем читает уже обновленный row и применяет
// свою маску поверх — lost-update исключен.
func (w *routeTableWriter) GetForUpdate(ctx context.Context, id string) (*kacho.RouteTableRecord, error) {
	q := fmt.Sprintf(`SELECT %s FROM route_tables WHERE id = $1 FOR UPDATE`, helpers.RouteTableCols)
	rt, err := helpers.ScanRouteTable(w.tx.QueryRow(ctx, q, id))
	if err != nil {
		return nil, helpers.WrapPgErr(err, "Route table", id)
	}
	return rt, nil
}

// Delete — DELETE route_tables WHERE id = $1. FK violation (например, NetworkInterface
// или Subnet ссылается на RT) → ErrFailedPrecondition "route table is in use".
// row not affected → ErrNotFound "Route table <id> not found".
//
// `subnets.route_table_id → route_tables(id) ON DELETE SET NULL` (baseline-схема)
// — Delete RT обнуляет `route_table_id` у привязанных подсетей в той же операции,
// FK не блокирует. Ссылки «маршрут → шлюз» снимаются каскадом
// (`route_table_gateway_refs.route_table_id ON DELETE CASCADE`), поэтому шлюз,
// который называла только эта таблица, становится удаляемым — как и должно быть.
//
// outbox-write (DELETED tombstone) — в use-case'е.
func (w *routeTableWriter) Delete(ctx context.Context, id string) error {
	tag, err := w.tx.Exec(ctx, `DELETE FROM route_tables WHERE id = $1`, id)
	if err != nil {
		if helpers.IsFKViolation(err) {
			return fmt.Errorf("%w: route table is in use", helpers.ErrFailedPrecondition)
		}
		return helpers.WrapPgErr(err, "Route table", id)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: Route table %s not found", helpers.ErrNotFound, id)
	}
	return nil
}
