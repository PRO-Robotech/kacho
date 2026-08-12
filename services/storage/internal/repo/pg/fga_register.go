// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/PRO-Robotech/kacho/pkg/ownerregister"
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
//
// ВОЗВРАЩАЕТСЯ ГОТОВАЯ СТРОКА ДОСТАВКИ со ШТАМПОМ, ПРОЧИТАННЫМ ЧЕРЕЗ RETURNING,
// а не вычисленным на стороне приложения. Две причины, обе про уже случившееся:
//
//   - ШТАМП. Синхронная доставка обязана нести ИМЕННО ЭТО значение. Обе доставки
//     одной регистрации (эта и дренаж той же строки) приходят к владельцу прав, и
//     он гасит повторную строгим монотонным сравнением версий: при одном значении
//     гашение срабатывает, КАКАЯ БЫ ни пришла первой. Часы момента доставки —
//     что здесь и стояло — делают синхронный вызов строго новее, и когда дренаж
//     успевает первым, тот заставляет пересчитывать материализацию заново, на
//     самом горячем пути создания ресурса;
//   - СОДЕРЖИМОЕ. Прежде use-case собирал строку ЗАНОВО
//     (`fgaregister.VolumeItem(res.ProjectID, res.ID, res.Labels)`) — два места об
//     одном предмете, которые разошлись бы молча: durable-намерение и синхронная
//     доставка описывали бы разные tuple'ы, и заметить это можно было бы только
//     по последствиям.
func emitFGARegister(ctx context.Context, tx pgx.Tx, eventType string, item fgaregister.Item) (ownerregister.Registration, error) {
	payload, err := fgaregister.Encode(fgaregister.Payload{
		Tuple:           item.Tuple,
		Labels:          item.Labels,
		ParentProjectID: item.ParentProjectID,
	})
	if err != nil {
		return ownerregister.Registration{}, fmt.Errorf("fga register intent marshal: %w", err)
	}
	kind, id := splitFGAObject(item.Tuple.Object)
	var stamped time.Time
	if err := tx.QueryRow(ctx,
		`INSERT INTO kacho_storage.fga_register_outbox
		   (event_type, resource_kind, resource_id, payload, created_at)
		 VALUES ($1, $2, $3,
		         jsonb_set($4::jsonb, '{source_version}', to_jsonb(now())),
		         now())
		 RETURNING (payload->>'source_version')::timestamptz`,
		eventType, kind, id, payload).Scan(&stamped); err != nil {
		return ownerregister.Registration{}, fmt.Errorf("fga register intent insert: %w", err)
	}
	return ownerregister.Registration{
		Tuple: ownerregister.Tuple{
			SubjectID: item.Tuple.SubjectID,
			Relation:  item.Tuple.Relation,
			Object:    item.Tuple.Object,
		},
		TraceID:         id,
		Labels:          item.Labels,
		ParentProjectID: item.ParentProjectID,
		// Цепь предков — та же, что на пути очереди: обе доставки одного
		// намерения обязаны нести одно содержание, иначе повтор стирает то,
		// что записала первая.
		ParentChain:   ownerregister.ParentChain(nil, item.ParentProjectID, ""),
		SourceVersion: stamped,
	}, nil
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
// Нулевая возвращённая строка означает «строки не было» (метки не менялись): её
// SourceVersion нулевой, и доставлять нечего. Отличать это состояние обязан
// вызывающий — общий регистратор нулевую версию ОТВЕРГАЕТ, а не додумывает.
func reEmitLabelMirror(
	ctx context.Context,
	tx pgx.Tx,
	labelsChanged bool,
	labelsAfterJSON []byte,
	item func(labels map[string]string) fgaregister.Item,
) (ownerregister.Registration, error) {
	if !labelsChanged {
		return ownerregister.Registration{}, nil
	}
	labels := map[string]string{}
	if len(labelsAfterJSON) > 0 {
		if err := json.Unmarshal(labelsAfterJSON, &labels); err != nil {
			return ownerregister.Registration{}, fmt.Errorf("fga register intent: decode labels after update: %w", err)
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
