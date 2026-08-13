// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repo

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/PRO-Robotech/kacho/services/compute/internal/ports"
)

// ErrGuestKeyNotInProject — ключ не принадлежит проекту машины (или его нет).
//
// Оба случая отвечают одинаково, и это намеренно: ответ «ключ существует, но
// чужой» отличался бы от «ключа нет», то есть сообщал бы про чужой проект то,
// чего вызывающий знать не должен.
var ErrGuestKeyNotInProject = fmt.Errorf(
	"%w: one or more guestAccessKeyIds do not name a key of this project", ports.ErrFailedPrecondition)

// ErrGuestKeyInUse — ключ привязан к живым машинам и потому не снимается.
//
// Несёт ПЕРЕЧЕНЬ машин: арендатор обязан видеть радиус ДО отказа, а не выяснять
// его перебором. Без перечня отказ читается как «где-то используется» и не
// подсказывает ни одного следующего шага.
type ErrGuestKeyInUse struct {
	KeyID       string
	InstanceIDs []string
	// Truncated — перечень усечён пределом. Признак ОБЯЗАТЕЛЕН: молчаливая
	// обрезка читается как полный список, и арендатор, сняв названные машины,
	// упёрся бы в тот же отказ, считая его ошибкой продукта.
	Truncated bool
}

func (e *ErrGuestKeyInUse) Error() string {
	tail := ""
	if e.Truncated {
		tail = " and more"
	}
	return fmt.Sprintf("guest access key %s is attached to instances: %s%s",
		e.KeyID, strings.Join(e.InstanceIDs, ", "), tail)
}

// Unwrap делает отказ снятия отображаемым общей таблицей отказов: состояние
// ресурса не позволяет операцию. Без него отказ упал бы в фиксированный
// внутренний — и перечень машин, ради которого он написан, не доехал бы.
func (e *ErrGuestKeyInUse) Unwrap() error { return ports.ErrFailedPrecondition }

// maxNamedHolders — сколько машин называется в отказе. Отказ адресован человеку;
// перечень из тысячи идентификаторов ему ничего не сообщает.
const maxNamedHolders = 10

// replaceGuestKeyBindings приводит набор ключей машины к заданному.
//
// # Почему замена целиком, а не «добавить/снять»
//
// Два частичных глагола на одном списке ссылок дали бы два способа сказать одно
// и разошлись бы на первом же одновременном вызове. Полный набор выразим и для
// «добавить один», и для «снять все», и он идемпотентен: повтор той же команды
// оставляет то же состояние.
//
// # Почему принадлежность проекту проверяется ВНУТРИ вставки
//
// Отдельная проверка перед вставкой защищает ровно тот путь, который через неё
// проходит, и разъезжается с записью под конкуренцией: между «спросил» и
// «вставил» ключ можно снять. Условие стоит в самой вставке, поэтому строка
// либо появляется законной, либо не появляется вовсе.
func replaceGuestKeyBindings(ctx context.Context, tx pgx.Tx, instanceID, projectID string, keyIDs []string) error {
	if _, err := tx.Exec(ctx,
		`DELETE FROM instance_guest_access_keys WHERE instance_id = $1`, instanceID); err != nil {
		return wrapPgErr(err, "Instance", instanceID)
	}
	if len(keyIDs) == 0 {
		return nil
	}

	// Вставка идёт выборкой из входа с условием принадлежности проекту, поэтому
	// чужой (или несуществующий) ключ не даёт строки. Сравнение числа вставленных
	// строк с числом РАЗЛИЧНЫХ запрошенных отличает «все законны» от «часть
	// отсеялась» — и делает это одним обменом с базой.
	tag, err := tx.Exec(ctx, `
		INSERT INTO instance_guest_access_keys (instance_id, guest_access_key_id)
		SELECT $1, k.id
		  FROM guest_access_keys k
		 WHERE k.id = ANY($2::text[])
		   AND k.project_id = $3
		ON CONFLICT DO NOTHING`, instanceID, keyIDs, projectID)
	if err != nil {
		return wrapPgErr(err, "Instance", instanceID)
	}

	distinct := map[string]struct{}{}
	for _, id := range keyIDs {
		distinct[id] = struct{}{}
	}
	if tag.RowsAffected() != int64(len(distinct)) {
		return ErrGuestKeyNotInProject
	}
	return nil
}

// guestKeyIDsOf возвращает набор ключей машины в устойчивом порядке.
//
// Порядок задан явно, а не оставлен на усмотрение планировщика: ответ, чей
// порядок меняется от запроса к запросу, невозможно ни сравнить, ни закрепить
// пробой, и разница читается как изменение состава.
func guestKeyIDsOf(ctx context.Context, q pgx.Tx, instanceID string) ([]string, error) {
	rows, err := q.Query(ctx,
		`SELECT guest_access_key_id FROM instance_guest_access_keys
		  WHERE instance_id = $1 ORDER BY guest_access_key_id`, instanceID)
	if err != nil {
		return nil, wrapPgErr(err, "Instance", instanceID)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if serr := rows.Scan(&id); serr != nil {
			return nil, wrapPgErr(serr, "Instance", instanceID)
		}
		out = append(out, id)
	}
	if rows.Err() != nil {
		return nil, wrapPgErr(rows.Err(), "Instance", instanceID)
	}
	return out, nil
}

// instancesHoldingGuestKey — машины, которые держат ключ.
//
// Читается перед отказом на снятии, чтобы вызывающий получил радиус, а не
// намёк. Перечень ограничен: отказ адресован человеку, и список из тысячи
// идентификаторов ему ничего не сообщает — поэтому дальше первых нескольких
// возвращается признак усечения, а не молчаливая обрезка.
func instancesHoldingGuestKey(ctx context.Context, q pgx.Tx, keyID string, limit int) ([]string, bool, error) {
	rows, err := q.Query(ctx,
		`SELECT instance_id FROM instance_guest_access_keys
		  WHERE guest_access_key_id = $1 ORDER BY instance_id LIMIT $2`, keyID, limit+1)
	if err != nil {
		return nil, false, wrapPgErr(err, "GuestAccessKey", keyID)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if serr := rows.Scan(&id); serr != nil {
			return nil, false, wrapPgErr(serr, "GuestAccessKey", keyID)
		}
		out = append(out, id)
	}
	if rows.Err() != nil {
		return nil, false, wrapPgErr(rows.Err(), "GuestAccessKey", keyID)
	}
	if len(out) > limit {
		return out[:limit], true, nil
	}
	return out, false, nil
}
