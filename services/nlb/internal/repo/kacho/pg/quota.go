// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// Доступ к строкам учёта числа ресурсов.
//
// Приёмка `docs/specs/sub-phase-quota-v2-materialised-usage-acceptance.md`
// (APPROVED, раунд 2), DoD S4 п.1.
//
// ЗДЕСЬ ДВА ГЛАГОЛА И НИ ОДНОГО ТРЕТЬЕГО. `Admit` — совещательная полоса
// (чтение, ничего не решает); `Materialize` — заведение строк учёта по ответу
// владельца величин. Списание и возврат ГЛАГОЛА НЕ ИМЕЮТ и иметь не должны: их
// делает триггер той же транзакцией, что вставку строки ресурса, и Go-метод
// «списать» означал бы второго писателя счётчика — ровно то, что гейт дерева
// `TestQuotaUsedIsWrittenOnlyByItsTrigger` запрещает.

// quotaReader — совещательная полоса поверх read-TX.
type quotaReader struct {
	tx pgx.Tx
}

// Admit спрашивает, есть ли место, и НЕ занимает его.
//
// Отказ приезжает исключением единственного производителя
// (`kacho_quota_refuse`, миграция 0032), поэтому его текст и признак совпадают с
// отказом авторитетной полосы ПОБАЙТОВО — не потому, что их согласовали, а
// потому, что место одно.
//
// nil означает «место было в момент чтения», а не «место будет».
func (q *quotaReader) Admit(ctx context.Context, carrierType, carrierID, kind string) error {
	const stmt = `SELECT kacho_nlb.kacho_quota_admit($1, $2, $3)`
	if _, err := q.tx.Exec(ctx, stmt, carrierType, carrierID, kind); err != nil {
		return mapPgErr(err, "Quota", "")
	}
	return nil
}

// quotaWriter — материализация поверх write-TX.
type quotaWriter struct {
	quotaReader
	tx pgx.Tx
}

// Materialize заводит строки учёта по всем видам, которые назвал владелец
// величин, и НЕ трогает уже заведённые.
func (q *quotaWriter) Materialize(ctx context.Context, rows []kacho.QuotaRow) (int64, error) {
	return MaterializeQuotas(ctx, q.tx, rows)
}

// QuotaExecutor — то единственное, что нужно материализации от носителя: один
// оператор. Транзакция, соединение из пула, соединение фикстуры — подходит любое.
type QuotaExecutor interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// MaterializeQuotas — ЕДИНСТВЕННЫЙ оператор заведения строк учёта.
//
// Экспортирован не ради удобства, а ради того, чтобы у него не появилось второго
// экземпляра. Кроме writer'а его зовёт фикстура интеграционных проб: она обязана
// приводить базу в то же состояние, в каком её видит репозиторий на живом пути,
// а собственный INSERT фикстуры разошёлся бы с настоящим молча — и разошёлся бы
// именно на составе столбцов, то есть там, где расхождение не видно глазом.
//
// `ON CONFLICT DO NOTHING`, а не `UPSERT`: строка учёта несёт `used`, и
// перезапись снимка величиной из свежего резолва обнулила бы потребление у
// проекта, который уже что-то создал. Догоняющее обновление величины — предмет
// синхронизатора (дельта по ревизии), а не материализации.
func MaterializeQuotas(ctx context.Context, ex QuotaExecutor, rows []kacho.QuotaRow) (int64, error) {
	if len(rows) == 0 {
		return 0, nil
	}
	const stmt = `
		INSERT INTO kacho_nlb.project_resource_quotas
		    (carrier_type, carrier_id, kind, used, limit_value,
		     source_scope, source_scope_id, limit_revision, account_id)
		SELECT * FROM unnest(
		    $1::text[], $2::text[], $3::text[], $4::bigint[], $5::bigint[],
		    $6::text[], $7::text[], $8::bigint[], $9::text[])
		ON CONFLICT (carrier_type, carrier_id, kind) DO NOTHING`

	n := len(rows)
	carrierTypes := make([]string, n)
	carrierIDs := make([]string, n)
	kinds := make([]string, n)
	used := make([]int64, n)
	limits := make([]int64, n)
	scopes := make([]string, n)
	scopeIDs := make([]string, n)
	revisions := make([]int64, n)
	accounts := make([]string, n)
	for i, r := range rows {
		carrierTypes[i] = r.CarrierType
		carrierIDs[i] = r.CarrierID
		kinds[i] = r.Kind
		used[i] = 0 // новая строка учёта не несёт потребления: его создаёт триггер
		limits[i] = r.Limit
		scopes[i] = r.SourceScope
		scopeIDs[i] = r.SourceScopeID
		revisions[i] = r.LimitRevision
		accounts[i] = r.AccountID
	}

	tag, err := ex.Exec(ctx, stmt,
		carrierTypes, carrierIDs, kinds, used, limits, scopes, scopeIDs, revisions, accounts)
	if err != nil {
		return 0, mapPgErr(err, "Quota", "")
	}
	return tag.RowsAffected(), nil
}

// MaterializeNestedDefaults заводит ПРОЕКТНЫЙ резолв вложенных видов.
func (q *quotaWriter) MaterializeNestedDefaults(ctx context.Context, rows []kacho.QuotaRow) (int64, error) {
	return MaterializeNestedDefaults(ctx, q.tx, rows)
}

// MaterializeNestedDefaults — ЕДИНСТВЕННЫЙ оператор заведения проектного
// резолва вложенных видов.
//
// Отдельная таблица, а не строка учёта: величина вложенного вида резолвится по
// проекту, а считается по родителю, поэтому проектного потребления у неё нет — и
// столбца `used`, который бы его выдумывал, тоже. Отсюда триггер жизненного
// цикла берёт снимок, заводя строку учёта нового родителя.
func MaterializeNestedDefaults(ctx context.Context, ex QuotaExecutor, rows []kacho.QuotaRow) (int64, error) {
	if len(rows) == 0 {
		return 0, nil
	}
	const stmt = `
		INSERT INTO kacho_nlb.nested_quota_defaults
		    (project_id, kind, limit_value, source_scope, source_scope_id,
		     limit_revision, account_id)
		SELECT * FROM unnest(
		    $1::text[], $2::text[], $3::bigint[], $4::text[], $5::text[],
		    $6::bigint[], $7::text[])
		ON CONFLICT (project_id, kind) DO NOTHING`

	n := len(rows)
	projects := make([]string, n)
	kinds := make([]string, n)
	limits := make([]int64, n)
	scopes := make([]string, n)
	scopeIDs := make([]string, n)
	revisions := make([]int64, n)
	accounts := make([]string, n)
	for i, r := range rows {
		projects[i] = r.CarrierID
		kinds[i] = r.Kind
		limits[i] = r.Limit
		scopes[i] = r.SourceScope
		scopeIDs[i] = r.SourceScopeID
		revisions[i] = r.LimitRevision
		accounts[i] = r.AccountID
	}

	tag, err := ex.Exec(ctx, stmt, projects, kinds, limits, scopes, scopeIDs, revisions, accounts)
	if err != nil {
		return 0, mapPgErr(err, "Quota", "")
	}
	return tag.RowsAffected(), nil
}
