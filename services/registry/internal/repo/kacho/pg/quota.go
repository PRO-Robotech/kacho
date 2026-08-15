// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
)

// Доступ к строкам учёта числа ресурсов.
//
// Приёмка `docs/specs/sub-phase-quota-v2-materialised-usage-acceptance.md`
// (APPROVED, раунд 2), DoD S4 п.1.
//
// ЗДЕСЬ ДВА ГЛАГОЛА И НИ ОДНОГО ТРЕТЬЕГО. `QuotaAdmit` — совещательная полоса
// (чтение, ничего не решает); `MaterializeQuotas` — заведение строк учёта по
// ответу владельца величин. Списание и возврат ГЛАГОЛА НЕ ИМЕЮТ и иметь не
// должны: их делает триггер той же транзакцией, что вставку строки ресурса, и
// Go-метод «списать» означал бы второго писателя счётчика — ровно то, что гейт
// дерева `TestQuotaUsedIsWrittenOnlyByItsTrigger` запрещает.

// QuotaRow — одна строка учёта, какой её ЗАВОДИТ материализация.
//
// Потребления здесь нет НАМЕРЕННО: `used` принадлежит триггеру и никакому
// другому писателю. Структура, несущая `Used`, немедленно сделала бы «записать
// потребление» выразимым на Go-стороне — а невыразимость этого и есть то, чем
// счётчик защищён от расхождения с реальностью.
type QuotaRow struct {
	// CarrierType — тип носителя: `project` либо родительский тип модели прав
	// (`registry.registries`).
	CarrierType string
	// CarrierID — идентификатор носителя.
	CarrierID string
	// Kind — вид ресурса точечным токеном платформы.
	Kind string
	// Limit — разрешённая величина, как её РАЗРЕШИЛ владелец величин. Снимок, а
	// не второй авторитет.
	Limit int64
	// SourceScope — область, на которой величина победила.
	SourceScope string
	// SourceScopeID — объект победившей области; пуст, когда победило умолчание.
	SourceScopeID string
	// LimitRevision — ревизия величины на момент снимка.
	LimitRevision int64
	// AccountID — зеркало аккаунта проекта; пустым быть не может (схема
	// отвергает): строка без зеркала невидима аккаунтной дельте.
	AccountID string
}

// QuotaExecutor — то единственное, что нужно этим операторам от носителя: один
// оператор. Транзакция, пул, соединение фикстуры — подходит любое.
type QuotaExecutor interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// QuotaAdmit — совещательная полоса: есть ли место, НЕ занимая его.
//
// Отказ приезжает исключением единственного производителя
// (`kacho_quota_refuse`, миграция 0015), поэтому его текст и признак совпадают с
// отказом авторитетной полосы ПОБАЙТОВО — не потому, что их согласовали, а
// потому, что место одно.
//
// nil означает «место было в момент чтения», а не «место будет»: между этим
// ответом и вставкой помещается чужая запись. Решает атомарное списание триггера
// (ban #10); приёмка прямо запрещает считать эту полосу решением (§7.4).
func QuotaAdmit(ctx context.Context, ex QuotaExecutor, carrierType, carrierID, kind string) error {
	stmt := fmt.Sprintf(`SELECT %s.kacho_quota_admit($1, $2, $3)`, schema)
	if _, err := ex.Exec(ctx, stmt, carrierType, carrierID, kind); err != nil {
		return wrapPgErr(err, "Quota", "")
	}
	return nil
}

// MaterializeQuotas — ЕДИНСТВЕННЫЙ оператор заведения строк учёта.
//
// Экспортирован не ради удобства, а ради того, чтобы у него не появилось второго
// экземпляра: фикстура интеграционных проб обязана приводить базу в то же
// состояние, в каком её видит репозиторий на живом пути, а собственный INSERT
// фикстуры разошёлся бы с настоящим молча — и разошёлся бы именно на составе
// столбцов, то есть там, где расхождение не видно глазом.
//
// `ON CONFLICT DO NOTHING`, а не `UPSERT`: строка учёта несёт `used`, и
// перезапись снимка величиной из свежего резолва обнулила бы потребление у
// проекта, который уже что-то создал.
func MaterializeQuotas(ctx context.Context, ex QuotaExecutor, rows []QuotaRow) (int64, error) {
	if len(rows) == 0 {
		return 0, nil
	}
	stmt := fmt.Sprintf(`
		INSERT INTO %s.project_resource_quotas
		    (carrier_type, carrier_id, kind, used, limit_value,
		     source_scope, source_scope_id, limit_revision, account_id)
		SELECT * FROM unnest(
		    $1::text[], $2::text[], $3::text[], $4::bigint[], $5::bigint[],
		    $6::text[], $7::text[], $8::bigint[], $9::text[])
		ON CONFLICT (carrier_type, carrier_id, kind) DO NOTHING`, schema)

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
		return 0, wrapPgErr(err, "Quota", "")
	}
	return tag.RowsAffected(), nil
}

// MaterializeNestedDefaults заводит ПРОЕКТНЫЙ резолв вложенных видов — величину,
// из которой триггер берёт снимок, заводя строку учёта нового родителя.
//
// Отдельный оператор, потому что отдельная таблица: величина вложенного вида
// резолвится по проекту, а считается по родителю, поэтому проектного
// потребления у неё нет — и столбца, который бы его выдумывал, тоже.
func MaterializeNestedDefaults(ctx context.Context, ex QuotaExecutor, rows []QuotaRow) (int64, error) {
	if len(rows) == 0 {
		return 0, nil
	}
	stmt := fmt.Sprintf(`
		INSERT INTO %s.nested_quota_defaults
		    (project_id, kind, limit_value, source_scope, source_scope_id,
		     limit_revision, account_id)
		SELECT * FROM unnest(
		    $1::text[], $2::text[], $3::bigint[], $4::text[], $5::text[],
		    $6::bigint[], $7::text[])
		ON CONFLICT (project_id, kind) DO NOTHING`, schema)

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
		return 0, wrapPgErr(err, "Quota", "")
	}
	return tag.RowsAffected(), nil
}
