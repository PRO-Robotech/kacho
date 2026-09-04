// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg

import (
	"context"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ExistenceProbe — read-only проверка существования object-scoped vpc-ресурса по
// его FGA-объекту. Используется на deny-пути authz-Check'а для existence-hiding
// (object-scoped deny на отсутствующий объект → 404 вместо 403). Легкий
// `SELECT EXISTS`, без полного чтения row.
type ExistenceProbe struct {
	pool *pgxpool.Pool
}

// NewExistenceProbe собирает probe поверх пула, который передаёт composition root,
// и этот пул обязан быть МАСТЕРОМ.
//
// Реплика здесь была бы дефектом, а не оптимизацией. Probe отвечает на вопрос
// «объекта не существует?», и ответ «да» превращает отказ в правах в 404. На
// реплике только что созданный объект какое-то время не виден, поэтому владелец,
// обратившийся к собственному свежему ресурсу, получил бы «не существует» вместо
// нормального разрешения — и, что хуже, тот же ответ получил бы посторонний,
// которому объект недоступен, то есть отсутствие и недоступность перестали бы
// различаться в пользу первого именно в момент, когда объект уже создан.
// Задержка репликации не ограничена сверху и не наблюдаема отсюда, поэтому
// «обычно свежая реплика» защитой не является: авторитетным должен быть тот же
// пул, в который шла запись.
//
// Привязка проверяется тестом композиции (cmd/vpc/existence_probe_wiring_test.go),
// чтобы утверждение выше не могло разойтись с проводкой.
func NewExistenceProbe(pool *pgxpool.Pool) *ExistenceProbe {
	return &ExistenceProbe{pool: pool}
}

// objectTypeTable — whitelist FGA-тип → таблица в схеме kacho_vpc. Имя таблицы
// берется ТОЛЬКО из этой константной карты (никогда из request'а) — инъекция
// невозможна.
var objectTypeTable = map[string]string{
	"vpc_network":           "networks",
	"vpc_subnet":            "subnets",
	"vpc_address":           "addresses",
	"vpc_route_table":       "route_tables",
	"vpc_security_group":    "security_groups",
	"vpc_gateway":           "gateways",
	"vpc_network_interface": "network_interfaces",
	"vpc_cidr_group":        "cidr_groups",
}

// ObjectExists возвращает true, если строка с данным id есть в таблице,
// соответствующей object-типу. Неизвестный тип → ошибка (caller трактует как
// «не могу подтвердить отсутствие» → оставляет deny, fail-closed).
func (p *ExistenceProbe) ObjectExists(ctx context.Context, objectType, objectID string) (bool, error) {
	table, ok := objectTypeTable[objectType]
	if !ok {
		return false, fmt.Errorf("existence probe: unprobeable object type %q", objectType)
	}
	var exists bool
	// table — из константного whitelist выше; objectID — bound-параметр.
	if err := p.pool.QueryRow(ctx,
		"SELECT EXISTS(SELECT 1 FROM kacho_vpc."+table+" WHERE id = $1)", objectID,
	).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

// ProbeableTypes — типы, о которых проба умеет ответить.
//
// Перечень обязан покрывать ВСЕ пообъектные типы карты прав сервиса; сверяет
// это носитель на старте (`servicehost`, О5в) сравнением с
// `catalogderive.ObjectScopedTypes` — обе стороны выведены, ни одна не
// выписана в месте сравнения. Тип, попавший в карту и не попавший сюда,
// отвечает на отказе ошибкой пробы, а она fail-closed: `403` там, где соседний
// тип того же сервиса отвечает `404` (задача продукта #1931).
//
// Возвращается свежий срез: носитель читает перечень и не вправе зависеть от
// того, что за ним стоит карта пакета.
func (p *ExistenceProbe) ProbeableTypes() []string {
	out := make([]string, 0, len(objectTypeTable))
	for ot := range objectTypeTable {
		out = append(out, ot)
	}
	sort.Strings(out)
	return out
}
