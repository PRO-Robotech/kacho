// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/PRO-Robotech/kacho/services/storage/internal/fgaregister"
)

// emitFGARegister вставляет ОДНУ строку fga_register_outbox (register/unregister
// intent owner-tuple) в переданной writer-TX — атомарно с доменным INSERT/DELETE
// ресурса (один commit, без dual-write; orphan-tuple исключён by construction).
//
// source_version штампуется БД-часами (now()) прямо в INSERT через jsonb_set —
// внутри writer-TX, монотонно per-object (позднейшая TX коммитится позже → её now()
// строго больше). resource_kind/resource_id извлекаются из tuple.Object
// ("<kind>:<id>") для трассировки/reconciler'а — drainer их НЕ читает.
//
// После INSERT срабатывает trigger pg_notify('kacho_storage_fga_register_outbox',
// NEW.id) — будит register-drainer.
func emitFGARegister(ctx context.Context, tx pgx.Tx, eventType string, item fgaregister.Item) error {
	payload, err := fgaregister.Encode(fgaregister.Payload{
		Tuple:           item.Tuple,
		Labels:          item.Labels,
		ParentProjectID: item.ParentProjectID,
	})
	if err != nil {
		return fmt.Errorf("fga register intent marshal: %w", err)
	}
	kind, id := splitFGAObject(item.Tuple.Object)
	if _, err := tx.Exec(ctx,
		`INSERT INTO kacho_storage.fga_register_outbox
		   (event_type, resource_kind, resource_id, payload, created_at)
		 VALUES ($1, $2, $3,
		         jsonb_set($4::jsonb, '{source_version}', to_jsonb(now())),
		         now())`,
		eventType, kind, id, payload); err != nil {
		return fmt.Errorf("fga register intent insert: %w", err)
	}
	return nil
}

// splitFGAObject разбивает FGA-object "<kind>:<id>" на (kind, id) для
// трассировочных колонок resource_kind/resource_id. Объект без ':' → ("", object).
func splitFGAObject(object string) (kind, id string) {
	if i := strings.IndexByte(object, ':'); i >= 0 {
		return object[:i], object[i+1:]
	}
	return "", object
}
