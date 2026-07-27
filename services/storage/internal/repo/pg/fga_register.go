// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg

import (
	"context"
	"encoding/json"
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

// reEmitLabelMirror переэмитит register-intent ПОСЛЕ смены меток, в той же
// writer-TX, что и UPDATE строки.
//
// Зачем. Доступ, выданный через label-селектор, отзывается СНЯТИЕМ метки — но это
// работает, только если владелец прав видит новые метки, а видит он их через
// mirror-строку, которую кормит владелец ресурса. storage эмитил intent на Create
// и на Delete, и ничего между ними: зеркало застывало на моменте создания,
// селектор продолжал матчить метки, которых у ресурса уже нет, и грант переживал
// метку, через которую был выдан. kacho-vpc это уже делает (network update
// переэмитит, когда labels в маске) — здесь были пропущены три ресурса, а не
// введён новый контракт.
//
// labelsChanged=false → no-op: переименование селектору ничего не сообщает, а
// лишняя строка — трафик дренажа, который голова партиции обязана разгрести
// прежде настоящего intent'а.
//
// Полное снятие меток — UPSERT С ПУСТЫМИ метками, НИКОГДА не unregister: ресурс
// существует и сохраняет свой owner-tuple, он лишь перестаёт матчиться
// селектором. Unregister здесь снял бы доступ у самого владельца.
func reEmitLabelMirror(
	ctx context.Context,
	tx pgx.Tx,
	labelsChanged bool,
	labelsAfterJSON []byte,
	item func(labels map[string]string) fgaregister.Item,
) error {
	if !labelsChanged {
		return nil
	}
	labels := map[string]string{}
	if len(labelsAfterJSON) > 0 {
		if err := json.Unmarshal(labelsAfterJSON, &labels); err != nil {
			return fmt.Errorf("fga register intent: decode labels after update: %w", err)
		}
	}
	return emitFGARegister(ctx, tx, fgaregister.EventRegister, item(labels))
}

// splitFGAObject разбивает FGA-object "<kind>:<id>" на (kind, id) для
// трассировочных колонок resource_kind/resource_id. Объект без ':' → ("", object).
func splitFGAObject(object string) (kind, id string) {
	if i := strings.IndexByte(object, ':'); i >= 0 {
		return object[:i], object[i+1:]
	}
	return "", object
}
