// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package outbox реализует транзакционный outbox-паттерн: каждое мутирующее
// действие на ресурс пишет строку в per-service outbox-таблицу в ТОЙ ЖЕ
// транзакции (см. Emit), а trigger pg_notify будит stream subscribers. Единый
// writer — outbox.Emit; drainer/reconciler читают backlog по sequence_no.
package outbox

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// SanitizeTable квотирует имя таблицы (опц. схема-квалифицированное
// "schema.table") через pgx.Identifier — идентификатор экранируется библиотекой
// независимо от дисциплины вызывающего. Даже при контрактe «caller передаёт
// literal» это defense-in-depth: имя таблицы больше не может стать вектором
// statement-injection при interpolation в `INSERT INTO %s`.
//
// Единый source-of-truth для всех outbox-подпакетов (reconciler/metrics), чтобы
// политика квотирования имён не расходилась между ними.
func SanitizeTable(table string) string {
	return pgx.Identifier(strings.Split(table, ".")).Sanitize()
}

// Emit вставляет одну outbox-строку в произвольную таблицу с фиксированной
// схемой: (sequence_no BIGSERIAL PK, resource_kind TEXT, resource_id TEXT,
// event_type TEXT, payload JSONB, created_at TIMESTAMPTZ DEFAULT now()).
//
// table — имя таблицы (например "vpc_outbox").
// kind — тип ресурса ("Network", "Subnet", "Address", "RouteTable", "SecurityGroup").
// id — ID ресурса (TEXT, поддерживает любой формат — UUID, короткий непрозрачный id и т.д.).
// eventType — "CREATED" | "UPDATED" | "DELETED".
// payload — произвольная map (сериализуется в JSONB).
//
// Должна вызываться внутри pgx.Tx, в которой выполняется INSERT/UPDATE/DELETE
// целевой ресурсной таблицы — это обеспечивает атомарность outbox-write.
//
// На каждый INSERT срабатывает trigger pg_notify('<channel>', sequence_no),
// который будит подписанных stream subscribers.
func Emit(ctx context.Context, tx pgx.Tx, table, kind, id, eventType string, payload map[string]any) error {
	return emit(ctx, tx, table, kind, id, "", eventType, payload, false)
}

// EmitAnchored — то же, но с ПРОЕКТНЫМ ЯКОРЕМ в колонке `project_id`.
//
// # Почему отдельная функция, а не параметр у Emit
//
// Якорь — свойство ЖУРНАЛА, а не вызова: колонка либо есть в таблице, либо нет,
// и вставка в несуществующую колонку — отказ хранилища, а не пустое значение.
// Единая сигнатура заставила бы владельцев журналов без колонки передавать
// что-нибудь, и «что-нибудь» уезжало бы в SQL, который у них не исполнится.
//
// # Зачем якорь нужен КОЛОНКОЙ
//
// Общая форма подписки (`pkg/subscription`) несёт проектный якорь полем
// оболочки события и принимает по нему решение о показе — не обращаясь к
// предмету. Для события снятия это несущее: обращаться не к чему, а нагрузка
// снятия несёт один идентификатор. Владелец, у которого якорь только в нагрузке,
// отдаёт у снятий пустой якорь — то есть утверждение «предмет уровня аккаунта», —
// и подписка с осью проекта такие события молча не пропускает. Потребитель,
// снявший опрос, об удалении не узнаёт никогда.
//
// # Кто её зовёт сегодня и куда это сходится
//
// Журналов с колонкой якоря в дереве два: `nlb_outbox` (своим кодом, колонка с
// базовой схемы) и `compute_outbox` (этой функцией, колонка заведена миграцией
// `..._compute_outbox_project_anchor`). Остальные ресурсные журналы — `vpc`,
// `geo` — колонки не несут и зовут [Emit]; их сведение к одной форме заведено
// предметом задачей продукта о своде журналов вместе с провязкой их подписки. До тех пор
// две функции — не два языка об одном предмете, а две РАЗНЫЕ формы хранения,
// каждая названная вслух.
func EmitAnchored(ctx context.Context, tx pgx.Tx, table, kind, id, projectID, eventType string, payload map[string]any) error {
	return emit(ctx, tx, table, kind, id, projectID, eventType, payload, true)
}

func emit(
	ctx context.Context,
	tx pgx.Tx,
	table, kind, id, projectID, eventType string,
	payload map[string]any,
	anchored bool,
) error {
	if table == "" {
		return fmt.Errorf("outbox.Emit: table name required")
	}
	bp, err := json.Marshal(payload)
	if err != nil {
		// Не должно случаться для разумных payload-ов, но не молчим.
		return fmt.Errorf("outbox.Emit: marshal payload: %w", err)
	}
	if anchored {
		q := fmt.Sprintf(
			`INSERT INTO %s (resource_kind, resource_id, project_id, event_type, payload) VALUES ($1, $2, $3, $4, $5)`,
			SanitizeTable(table),
		)
		if _, err = tx.Exec(ctx, q, kind, id, projectID, eventType, bp); err != nil {
			return fmt.Errorf("outbox.Emit: insert into %s: %w", table, err)
		}
		return nil
	}
	// table инжектится в SQL как идентификатор → квотируем через
	// pgx.Identifier.Sanitize (см. SanitizeTable). Значения по-прежнему идут
	// параметрами $1..$4. Контракт «caller передаёт literal» остаётся, но
	// sanitize снимает риск statement-injection через имя таблицы.
	q := fmt.Sprintf(
		`INSERT INTO %s (resource_kind, resource_id, event_type, payload) VALUES ($1, $2, $3, $4)`,
		SanitizeTable(table),
	)
	_, err = tx.Exec(ctx, q, kind, id, eventType, bp)
	if err != nil {
		return fmt.Errorf("outbox.Emit: insert into %s: %w", table, err)
	}
	return nil
}
