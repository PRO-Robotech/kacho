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

// gatewayReader — Get/List поверх произвольной pgx.Tx (read-only или RW).
// Не имеет своего state кроме tx. Это read-сторона CQRS-разделения Gateway-репо;
// SQL/scan-семантика вынесена в helpers (helpers.GatewayCols / helpers.ScanGateway /
// helpers.WrapGatewayErr / helpers.MarshalJSONB).
type gatewayReader struct {
	tx pgx.Tx
}

// Get — well-formed-но-нет → NotFound с "Gateway <id> not found".
func (r *gatewayReader) Get(ctx context.Context, id string) (*kacho.GatewayRecord, error) {
	q := fmt.Sprintf(`SELECT %s FROM gateways WHERE id = $1`, helpers.GatewayCols)
	row := r.tx.QueryRow(ctx, q, id)
	g, err := helpers.ScanGateway(row)
	if err != nil {
		return nil, helpers.WrapGatewayErr(err, id)
	}
	return g, nil
}

// List — cursor-based pagination + filter.Parse с whitelist allowedFields=["name"].
func (r *gatewayReader) List(ctx context.Context, f kacho.GatewayFilter, p kacho.Pagination) ([]*kacho.GatewayRecord, string, error) {
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
	if f.Name != "" {
		conditions = append(conditions, fmt.Sprintf("name = $%d", argIdx))
		args = append(args, f.Name)
		argIdx++
	}
	if f.Filter != "" {
		// Список остаётся при одном поле ОСОЗНАННО, а не по отставанию. Плоский
		// контракт шлюза несёт только id, project_id, created_at, name и
		// description: сужать больше нечем. Колонка `gateway_type` существует, но
		// полем КОНТРАКТА не является (тот же дефект отдельно чинится в маске
		// обновления) — вынести её в публичный фильтр значило бы завести
		// неконтрактное имя на публичной поверхности.
		ast, perr := filter.Parse(f.Filter, []string{"name"})
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
	q := fmt.Sprintf(`SELECT %s FROM gateways %s ORDER BY created_at ASC, id ASC LIMIT $%d`, helpers.GatewayCols, where, argIdx)
	args = append(args, pageSize+1)

	rows, err := r.tx.Query(ctx, q, args...)
	if err != nil {
		return nil, "", helpers.WrapGatewayErr(err, "")
	}
	defer rows.Close()

	var result []*kacho.GatewayRecord
	for rows.Next() {
		g, err := helpers.ScanGateway(rows)
		if err != nil {
			return nil, "", helpers.WrapGatewayErr(err, "")
		}
		result = append(result, g)
	}
	if err := rows.Err(); err != nil {
		return nil, "", helpers.WrapGatewayErr(err, "")
	}

	var nextToken string
	if int64(len(result)) > pageSize {
		last := result[pageSize-1]
		nextToken = helpers.EncodePageToken(last.CreatedAt, last.ID)
		result = result[:pageSize]
	}
	return result, nextToken, nil
}

// gatewayWriter — DML над gateways через writer-TX. Встраивает gatewayReader, чтобы
// writer видел собственные writes.
//
// Особенность CQRS: writer НЕ emit-ит outbox самостоятельно — caller (use-case)
// делает RepositoryWriter.Outbox().Emit(...) явно после успешного DML. Это
// гарантирует, что outbox-write идет в той же pgx.Tx и что последовательность
// outbox-событий — явное решение use-case-а, а не «из глубины» repo.
type gatewayWriter struct {
	gatewayReader
}

// Insert — INSERT gateways RETURNING. CreatedAt явно проставляется в UTC, хотя
// БД-колонка имеет DEFAULT now() — это нужно для детерминированности тестов и для
// возврата RETURNING без второго round-trip.
//
// outbox-write — не здесь, а в use-case-е через writer.Outbox().Emit(...).
func (w *gatewayWriter) Insert(ctx context.Context, g *domain.Gateway) (*kacho.GatewayRecord, error) {
	labelsJSON, err := helpers.MarshalJSONB(domain.LabelsToMap(g.Labels), "Gateway.labels")
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	// Якорь размещения проверяется ВНУТРИ вставки, а не отдельным чтением до неё:
	// «прочитал подсеть → вставил шлюз» под READ COMMITTED допускает удаление или
	// правку подсети между двумя операторами, и второй писатель молча победил бы
	// (ban #10). Здесь один оператор, и он решает всё:
	//
	//   * подсети НЕТ (или она чужого проекта — см. ниже) → внутренний EXISTS
	//     ложен, значит строка вставляется и её отвергает внешний ключ
	//     `gateways_subnet_fk` (23503). Это НЕ обход проверки: существование
	//     держит именно FK, потому что коррелированный подзапрос строк не лочит, а
	//     FK берёт разделяемую блокировку на строку-referent;
	//   * подсеть есть, но НЕ несёт семейства, которое обслуживает выбранный вид
	//     шлюза, ЛИБО принадлежит другому проекту → предикат отсекает строку, ноль
	//     строк, классификация ниже.
	//
	// Чужой проект отсекается предикатом (а не FK) намеренно: ответ обязан быть
	// БАЙТ-ИДЕНТИЧЕН настоящему промаху — «Subnet <id> not found». Различимый
	// ответ был бы оракулом существования чужой подсети.
	// `external_address_id` кладётся ВНУТРИ этой же вставки, а не отдельным
	// UPDATE следом: биусловие `gateways_nat_has_address_chk` (0038) связывает
	// каждую записываемую строку, поэтому «сначала вставим шлюз, потом припишем
	// адрес» не прошло бы вовсе — промежуточного состояния «NAT без адреса» не
	// существует. Пустая строка означает «адреса нет» и обязана лечь NULL'ом:
	// частичный UNIQUE считал бы пустые строки равными и допустил бы ровно один
	// безадресный шлюз на всю таблицу.
	q := fmt.Sprintf(`
		INSERT INTO gateways (id, project_id, created_at, name, description, labels, gateway_type, subnet_id, external_address_id)
		SELECT $1, $2, $3, $4, $5, $6, $7, $8, NULLIF($9, '')
		 WHERE NOT EXISTS (
		   SELECT 1 FROM subnets s
		    WHERE s.id = $8
		      AND ( s.project_id <> $2
		         OR NOT ( ($7 = 'NAT'         AND coalesce(array_length(s.v4_cidr_blocks, 1), 0) >= 1)
		               OR ($7 = 'EGRESS_ONLY' AND coalesce(array_length(s.v6_cidr_blocks, 1), 0) >= 1) ) )
		 )
		RETURNING %s`, helpers.GatewayCols)

	row := w.tx.QueryRow(ctx, q,
		g.ID, g.ProjectID, now, string(g.Name), string(g.Description), labelsJSON,
		string(g.GatewayType), g.SubnetID, g.ExternalAddressID,
	)
	result, err := helpers.ScanGateway(row)
	if err != nil {
		if helpers.IsFKViolationOn(err, helpers.GatewaySubnetFKConstraint) {
			return nil, fmt.Errorf("%w: Subnet %s not found", helpers.ErrNotFound, g.SubnetID)
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, w.classifyAnchorRejection(ctx, &g.ProjectID, g.SubnetID, g.GatewayType)
		}
		return nil, helpers.WrapGatewayErr(err, string(g.Name))
	}
	return result, nil
}

// classifyAnchorRejection — СООБЩЕНИЕ для отвергнутой вставки шлюза; решение уже
// принято оператором выше, здесь только выясняется, что именно сказать.
//
// Порядок ветвей повторяет порядок предиката вставки. Ветвь «причина больше не
// видна» — не корзина «прочее»: под READ COMMITTED следующий оператор той же
// транзакции получает новый снимок, поэтому подсеть могла измениться между
// вставкой и этим чтением. Тогда правдиво сказать ровно то, что известно:
// предусловие не выполнено, — и не выдумывать причину.
func (w *gatewayWriter) classifyAnchorRejection(
	ctx context.Context, projectID *string, subnetID string, gtype domain.GatewayType,
) error {
	var hasV4, hasV6 bool
	err := w.tx.QueryRow(ctx, `
		SELECT coalesce(array_length(v4_cidr_blocks, 1), 0) >= 1,
		       coalesce(array_length(v6_cidr_blocks, 1), 0) >= 1
		  FROM subnets WHERE id = $1 AND project_id = $2`, subnetID, *projectID).Scan(&hasV4, &hasV6)
	if errors.Is(err, pgx.ErrNoRows) {
		// Подсети нет ЛИБО она чужого проекта — один и тот же ответ, дословно
		// равный настоящему промаху.
		return fmt.Errorf("%w: Subnet %s not found", helpers.ErrNotFound, subnetID)
	}
	if err != nil {
		return helpers.WrapPgErr(err, "Subnet", subnetID)
	}
	switch {
	case gtype == domain.GatewayTypeNat && !hasV4:
		return fmt.Errorf("%w: Subnet %s has no IPv4 CIDR block", helpers.ErrFailedPrecondition, subnetID)
	case gtype == domain.GatewayTypeEgressOnly && !hasV6:
		return fmt.Errorf("%w: Subnet %s has no IPv6 CIDR block", helpers.ErrFailedPrecondition, subnetID)
	}
	return fmt.Errorf("%w: Subnet %s can not anchor this gateway", helpers.ErrFailedPrecondition, subnetID)
}

// Update — UPDATE gateways RETURNING name/description/labels.
//
// `gateway_type` и `subnet_id` в SET НЕ входят: вид шлюза и его якорь размещения
// выбираются на Create и неизменяемы (`update_mask` с их именами отвергается
// синхронно с конвенционным тоном). Держать их в SET значило бы иметь путь
// записи у поля, которое контракт объявляет неизменяемым, — расхождение, которое
// однажды кто-нибудь «починит», добавив ветвь в маску.
//
// outbox-write — в use-case-е (см. Insert).
func (w *gatewayWriter) Update(ctx context.Context, g *domain.Gateway) (*kacho.GatewayRecord, error) {
	labelsJSON, err := helpers.MarshalJSONB(domain.LabelsToMap(g.Labels), "Gateway.labels")
	if err != nil {
		return nil, err
	}

	q := fmt.Sprintf(`
		UPDATE gateways SET name=$2, description=$3, labels=$4
		WHERE id=$1
		RETURNING %s`, helpers.GatewayCols)

	row := w.tx.QueryRow(ctx, q,
		g.ID, string(g.Name), string(g.Description), labelsJSON,
	)
	result, err := helpers.ScanGateway(row)
	if err != nil {
		return nil, helpers.WrapGatewayErr(err, g.ID)
	}
	return result, nil
}

// GetForUpdate — Get с row-lock (`FOR UPDATE`) в writer-TX. Сериализует
// конкурентный read-modify-write в Update: второй concurrent Update блокируется на
// GetForUpdate до commit первого, затем читает уже обновленный row и применяет свою
// маску поверх — lost-update исключен.
func (w *gatewayWriter) GetForUpdate(ctx context.Context, id string) (*kacho.GatewayRecord, error) {
	q := fmt.Sprintf(`SELECT %s FROM gateways WHERE id = $1 FOR UPDATE`, helpers.GatewayCols)
	g, err := helpers.ScanGateway(w.tx.QueryRow(ctx, q, id))
	if err != nil {
		return nil, helpers.WrapPgErr(err, "Gateway", id)
	}
	return g, nil
}

// Delete — DELETE gateways WHERE id = $1. FK violation (gateway в использовании)
// → ErrFailedPrecondition с текстом "gateway is in use". row not affected →
// ErrNotFound "Gateway <id> not found".
//
// У ветви FK появился ПРОИЗВОДИТЕЛЬ: до миграции 0030 на `gateways` не ссылался
// ни один внешний ключ, поэтому ветвь была недостижима и защищала от того, чего
// не было. Теперь на шлюз ссылается `route_table_gateway_refs.gateway_id`
// (ON DELETE RESTRICT) — шлюз, названный живым статическим маршрутом, не
// удаляется, и именно этот отказ здесь классифицируется.
//
// outbox-write (DELETED tombstone) — в use-case-е.
func (w *gatewayWriter) Delete(ctx context.Context, id string) error {
	tag, err := w.tx.Exec(ctx, `DELETE FROM gateways WHERE id = $1`, id)
	if err != nil {
		if helpers.IsFKViolation(err) {
			return fmt.Errorf("%w: gateway is in use", helpers.ErrFailedPrecondition)
		}
		return helpers.WrapGatewayErr(err, id)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: Gateway %s not found", helpers.ErrNotFound, id)
	}
	return nil
}
