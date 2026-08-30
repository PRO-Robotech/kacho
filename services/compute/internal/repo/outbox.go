// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repo

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/PRO-Robotech/kacho/pkg/outbox"
	"github.com/PRO-Robotech/kacho/pkg/ownerregister"
	"github.com/PRO-Robotech/kacho/services/compute/internal/domain"
	"github.com/PRO-Robotech/kacho/services/compute/internal/fgaintent"
)

// computeOutboxTable — имя таблицы outbox в kacho_compute DB.
const computeOutboxTable = "compute_outbox"

// fgaRegisterOutboxTable — таблица FGA-register-intent (миграция 0010).
const fgaRegisterOutboxTable = "compute_fga_register_outbox"

// emitCompute — обёртка над outbox.EmitAnchored с фиксированной таблицей compute_outbox.
// Должна вызываться внутри той же tx, что и INSERT/UPDATE/DELETE на ресурсной
// таблице (атомарность). Trigger compute_outbox_notify_trg на каждый INSERT
// шлёт pg_notify('compute_outbox', sequence_no::text). kind ∈ {Instance} — блочное
// хранение из compute снято (миграция 0021 дропнула disks/images/snapshots), и Disk /
// Image / Snapshot в этом перечислении больше не значатся.
//
// # projectID — ЯКОРЬ, а не украшение
//
// Он уезжает в СВОЮ колонку, а не в нагрузку, потому что по нему подписка
// решает, кому показать событие, не обращаясь к предмету. Для снятия это
// несущее: обращаться не к чему, а нагрузка снятия несёт один идентификатор.
// Оставь якорь только в нагрузке — и события удаления машины уходили бы с пустым
// якорем, то есть с утверждением «предмет уровня аккаунта»; подписчик, снявший
// опрос, об удалении не узнавал бы НИКОГДА. Разбор — у миграции
// `..._compute_outbox_project_anchor` и в объявлении журнала
// (`internal/subscriptionjournal`).
//
// Пустой якорь остаётся ЗАКОННЫМ входом и здесь не отвергается: отвергать его
// значило бы решать за вызывающего судьбу его транзакции ради поля, которого у
// исторических строк и так нет. Наблюдаемость этого — на стороне подписки: строка
// без якоря не отбирается осью проекта, и это сказано в миграции вслух.
func emitCompute(ctx context.Context, tx pgx.Tx, kind, id, projectID, eventType string, payload map[string]any) error {
	if payload == nil {
		payload = map[string]any{}
	}
	return outbox.EmitAnchored(ctx, tx, computeOutboxTable, kind, id, projectID, eventType, payload)
}

// domainToMap конвертирует произвольный domain-объект в map[string]any через
// JSON round-trip. При ошибке возвращает пустую map (lenient — outbox event
// важнее content-корректности).
func domainToMap(v any) map[string]any {
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

// emitFGARegisterIntent writes one FGA-register/unregister intent row into
// compute_fga_register_outbox IN THE SAME tx as the resource Insert/Update/Delete
// (transactional outbox — no dual-write). event ∈ {fga.register,
// fga.unregister}; kind ∈ {Instance} (block storage left compute in migration 0021,
// so Disk / Image / Snapshot are no longer among the kinds). The payload carries
// the project-hierarchy owner-tuple set AND the owner's labels + parent-scope
// (project) so the register-drainer can feed IAM resource_mirror.
// labels may be nil/empty (graceful — empty mirror labels). parent_account_id is
// left empty: compute does not resolve project→account on the resource hot-path
// (IAM handles an empty parent gracefully). Unknown kind / empty id / empty
// projectID → no row is written (caller's resource still commits; an unmappable
// kind simply has no FGA hierarchy to register — fail-safe, never an orphan
// intent). An INSERT here fires the NOTIFY trigger waking the register-drainer;
// if the surrounding tx aborts, the intent rolls back atomically.
//
// ВОЗВРАЩАЕТ ГОТОВУЮ СТРОКУ ДОСТАВКИ со штампом, ПРОЧИТАННЫМ ЧЕРЕЗ RETURNING.
// Синхронная доставка обязана нести ИМЕННО ЕГО: обе доставки одной регистрации
// (синхронная и дренаж этой же строки) приходят к владельцу прав, и он гасит
// повторную строгим монотонным сравнением версий — при одном значении гашение
// срабатывает, какая бы ни пришла первой. Здесь стояли часы момента доставки,
// отчего гашение работало только в одном порядке, а в обратном заставляло
// пересчитывать материализацию заново на горячем пути создания.
// Неотображаемый kind / пустой id → нулевая строка (регистрировать нечего).
func emitFGARegisterIntent(ctx context.Context, tx pgx.Tx, event, kind, resourceID, projectID string, labels map[string]string) (ownerregister.Registration, error) {
	tuple, ok := fgaintent.ProjectHierarchyTuple(kind, resourceID, projectID)
	if !ok {
		return ownerregister.Registration{}, nil
	}
	b, err := fgaintent.Encode(fgaintent.Payload{
		Tuples:          []fgaintent.Tuple{tuple},
		Labels:          labels,
		ParentProjectID: projectID,
	})
	if err != nil {
		return ownerregister.Registration{}, fmt.Errorf("encode fga intent: %w", err)
	}
	// Stamp the β-hardening monotonic source_version into the payload from the DB
	// clock (now()) AT INSERT TIME, inside this writer-tx — the exact instant the
	// source-state is recorded. jsonb_set merges it into the encoded payload so the
	// register-drainer forwards it to IAM.RegisterResource.source_version
	// (last-source-state-wins). Compute has no per-row updated_at; the intent-emit
	// now() is the correct, least-invasive per-object marker and matches the row's
	// own created_at default (same transaction_timestamp()).
	var stamped time.Time
	if err = tx.QueryRow(ctx,
		fmt.Sprintf(`INSERT INTO %s (event_type, resource_kind, resource_id, payload)
		             VALUES ($1, $2, $3, jsonb_set($4::jsonb, '{source_version}', to_jsonb(now())))
		             RETURNING (payload->>'source_version')::timestamptz`, fgaRegisterOutboxTable),
		event, kind, resourceID, b).Scan(&stamped); err != nil {
		return ownerregister.Registration{}, fmt.Errorf("emit fga register intent: %w", err)
	}
	return ownerregister.Registration{
		Tuple: ownerregister.Tuple{
			SubjectID: tuple.SubjectID,
			Relation:  tuple.Relation,
			Object:    tuple.Object,
		},
		TraceID:         resourceID,
		Labels:          labels,
		ParentProjectID: projectID,
		// Цепь предков — та же, что на пути очереди: обе доставки одного
		// намерения обязаны нести одно содержание, иначе повтор стирает то,
		// что записала первая.
		ParentChain:   ownerregister.ParentChain(nil, projectID, ""),
		SourceVersion: stamped,
	}, nil
}

func instancePayload(in *domain.Instance) map[string]any { return domainToMap(in) }

// registrationsOf — набор доставки из одной строки. Нулевая строка (kind не
// отображается, метки не менялись) даёт ПУСТОЙ набор, а не набор с пустой
// строкой: общий регистратор нулевую версию отвергает, и молчаливо подсунуть ему
// «ничего» под видом «чего-то» значило бы завести отказ там, где доставлять
// действительно нечего.
func registrationsOf(reg ownerregister.Registration) []ownerregister.Registration {
	if reg.SourceVersion.IsZero() {
		return nil
	}
	return []ownerregister.Registration{reg}
}
