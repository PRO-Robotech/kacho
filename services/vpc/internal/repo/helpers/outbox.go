// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package helpers

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"

	"github.com/PRO-Robotech/kacho/pkg/outbox"
)

// VPCOutboxTable — имя таблицы outbox в kacho_vpc DB.
const VPCOutboxTable = "vpc_outbox"

// NoProjectAnchor — якорь предмета, у которого проектного измерения НЕТ вовсе.
//
// Пустая строка здесь — НАЗВАННОЕ решение, а не пропуск, и потому у неё есть имя:
// пустой литерал на месте якоря неотличим от забытого аргумента ни на чтение, ни
// на перепись, а забытый якорь делает событие невидимым подписчику с осью проекта
// — тихо.
//
// Законных предметов ровно два, и оба админские, уровня кластера: `AddressPool`
// (пул адресов заводит администратор платформы, проекта у него нет по определению
// ресурса) и `AddressPoolNetworkDefault` (привязка пула к сети — предмет того же
// админского ресурса). Оба НЕ значатся в словаре видов подписки: типа объекта в
// модели прав у них нет, поэтому вопрос «вправе ли вызывающий это видеть» задать
// нечем, и строка арендатору не доставляется. Это и требуется: инфраструктурный
// предмет живёт только на внутренней поверхности.
const NoProjectAnchor = ""

// EmitVPC — обертка над outbox.EmitAnchored с фиксированной таблицей vpc_outbox.
// Должна вызываться внутри той же tx, что и INSERT/UPDATE/DELETE на ресурсной
// таблице (атомарность). Trigger vpc_outbox_notify_trg на каждый INSERT
// автоматически шлет pg_notify('vpc_outbox', sequence_no::text).
//
// payload — нужно передавать произвольную map (например, snapshot domain-объекта),
// либо nil (тогда payload = `{}`). Для DELETED-event payload может содержать
// только {"id": "..."} как tombstone.
//
// # Почему якорь проекта стоит ОТДЕЛЬНЫМ аргументом, а не берётся из нагрузки
//
// Якорь АВТОРИЗУЮЩИЙ: по нему сервер потока решает, кому показать событие, не
// обращаясь к предмету. У снятия нагрузка несёт один идентификатор, поэтому
// вывести якорь из неё нельзя — а пустой якорь по контракту формы означает
// «предмет уровня аккаунта», то есть утверждение, ложное для всякого проектного
// предмета vpc. Подписка с осью проекта такие события не пропускала бы, и
// потребитель НИКОГДА не узнавал бы об удалении. Отказ тихий.
//
// Отдельный аргумент делает якорь ВИДИМЫМ решением на каждом месте эмиссии:
// пропустить его нельзя — не соберётся. Пустая строка законна ровно для предметов
// без проектного измерения (`AddressPool` и его привязка к сети — админские
// ресурсы уровня кластера), и там она стоит явно.
func EmitVPC(ctx context.Context, tx pgx.Tx, kind, id, projectID, eventType string, payload map[string]any) error {
	if payload == nil {
		payload = map[string]any{}
	}
	return outbox.EmitAnchored(ctx, tx, VPCOutboxTable, kind, id, projectID, eventType, payload)
}

// DomainToMap конвертирует произвольный domain-объект в map[string]any через
// JSON round-trip. Используется для формирования payload outbox-события.
// При ошибке возвращает пустую map (lenient — outbox event важнее, чем
// content корректности).
func DomainToMap(v any) map[string]any {
	b, err := json.Marshal(v)
	if err != nil {
		return map[string]any{}
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return map[string]any{}
	}
	return m
}
