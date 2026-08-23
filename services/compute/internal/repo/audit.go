// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repo

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/operations"
)

// auditPrefix — префикс идентификатора записи журнала. Тип читается по
// префиксу, как у любого ресурса продукта; ограничение формы стоит в схеме.
const auditPrefix = "aud"

// AuditEvent — одна запись журнала.
//
// # Два лица, а не одно
//
// Actor — кто фактически выполнил. OnBehalfOf — от чьего имени. Для синхронного
// пути они совпадают; для асинхронного продолжения рабочая функция исполняется
// под личностью инициатора, захваченной в момент запроса.
//
// Записать только исполнителя — потерять ответственность: журнал скажет «это
// сделала рабочая функция», а кто её попросил, останется неизвестным. Записать
// только инициатора — потерять, кто фактически выполнил. Поэтому пишутся оба, и
// схема запрещает заполнить половину второго лица.
type AuditEvent struct {
	EventType    string
	ResourceType string
	ResourceID   string
	ProjectID    string
	Actor        operations.Principal
	OnBehalfOf   operations.Principal
	Payload      map[string]any
}

// emitAudit пишет запись журнала В ТОЙ ЖЕ транзакции, что и мутация.
//
// # Почему параметром идёт транзакция, а не пул
//
// Функция, берущая пул, физически не может писать в чужую транзакцию — и
// вызывающий, передавший пул, получил бы запись, которая переживает откат
// мутации. Тип параметра здесь и есть то, что делает свойство невозможным
// нарушить: чтобы записать аудит мимо транзакции, придётся изменить сигнатуру,
// а это видно в обзоре.
//
// # Чего эта функция НЕ делает
//
// Не отправляет наружу и не решает, куда отправлять. Запись ложится в очередь;
// доставку ведёт вывоз (`pkg/audit`, поднят в композиционном корне службы), у
// него свои повторы и своя пауза перед ними. Смешать запись с доставкой значило
// бы поставить успех мутации в зависимость от доступности приёмника журнала.
func emitAudit(ctx context.Context, tx pgx.Tx, ev AuditEvent) error {
	if ev.EventType == "" || ev.ResourceType == "" || ev.ResourceID == "" {
		return fmt.Errorf("audit: event_type, resource_type and resource_id are required")
	}
	// Актор обязателен: запись без него не отвечает на вопрос, ради которого
	// журнал существует. Пустой актор — не «неизвестно», а «мы не посмотрели»,
	// и различить это потом будет нечем.
	if ev.Actor.Type == "" || ev.Actor.ID == "" {
		return fmt.Errorf("audit: actor is required for %s on %s", ev.EventType, ev.ResourceID)
	}

	payload := ev.Payload
	if payload == nil {
		payload = map[string]any{}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("audit: marshal payload: %w", err)
	}

	const q = `INSERT INTO audit_outbox
		(id, event_type, resource_type, resource_id, project_id,
		 actor_type, actor_id, on_behalf_of_type, on_behalf_of_id, payload)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`
	_, err = tx.Exec(ctx, q,
		ids.NewID(auditPrefix),
		ev.EventType,
		ev.ResourceType,
		ev.ResourceID,
		ev.ProjectID,
		ev.Actor.Type,
		ev.Actor.ID,
		ev.OnBehalfOf.Type,
		ev.OnBehalfOf.ID,
		raw,
	)
	return err
}

// auditPrincipals читает из контекста пару лиц для записи журнала.
//
// Инициатор берётся из контекста запроса; исполнитель для синхронного пути —
// он же. Асинхронное продолжение передаёт инициатора явно, и тогда исполнитель
// отличается — это не олицетворение, а передача контекста: рабочая функция
// доделывает начатое, а не действует по своему усмотрению.
func auditPrincipals(ctx context.Context) (actor, onBehalfOf operations.Principal) {
	p := operations.PrincipalFromContext(ctx)
	return p, operations.Principal{}
}
