// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	corequota "github.com/PRO-Robotech/kacho/pkg/quota"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/helpers"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
)

// quotaSchema — схема, в которой у этого владельца лежит таблица учёта.
const quotaSchema = "kacho_vpc"

// Доступ к строкам учёта числа ресурсов.
//
// Приёмка `docs/specs/sub-phase-quota-v2-materialised-usage-acceptance.md`
// (APPROVED, раунд 2), DoD S2 п.3 и п.5.
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
// (`kacho_quota_refuse`), поэтому его текст и признак совпадают с отказом
// авторитетной полосы ПОБАЙТОВО — не потому, что их согласовали, а потому, что
// место одно. Классификация SQLSTATE'а в sentinel — общая
// (`helpers.WrapPgErr`), та же, что на пути вставки.
//
// nil означает «место было в момент чтения», а не «место будет». Между этим
// ответом и вставкой помещается чужая запись; решение принимает атомарный
// `UPDATE … WHERE used < limit_value` триггера (ban #10). Приёмка прямо
// запрещает считать совещательную полосу решением (§7.4).
func (q *quotaReader) Admit(ctx context.Context, carrierType, carrierID, kind string) error {
	const stmt = `SELECT kacho_vpc.kacho_quota_admit($1, $2, $3)`
	if _, err := q.tx.Exec(ctx, stmt, carrierType, carrierID, kind); err != nil {
		return helpers.WrapPgErr(err, "Quota", "")
	}
	return nil
}

// ListStates отдаёт строки учёта носителя — то, что арендатор читает как свои
// квоты.
//
// `ORDER BY kind` — по КОЛОНКЕ ПОРЯДКА, а не по времени вставки. Строки заводит
// материализация одной транзакцией, поэтому метка времени у них совпадает, и
// сортировка по ней разрешалась бы идентификатором, то есть случайной строкой:
// ответ переставлял бы виды от прогона к прогону, а клиент, ведущий состояние по
// индексу, читал бы перестановку как изменение (`api-conventions.md` §«Порядок
// повторяющегося поля — часть контракта либо его нет»).
//
// Пустой срез здесь означает «строк учёта ещё нет» и НИЧЕГО не говорит о
// пределах: различать это состояние и отвечать арендатору полным набором обязан
// вызывающий. Репозиторий сообщает только то, что видит в своей таблице.
// Оператор ОБЩИЙ (`pkg/quota.ListStates`): таблица у всех владельцев одна и та
// же с точностью до имени схемы. Прежде он стоял здесь своей копией, и это было
// верно ровно до появления второго владельца — дальше пять копий одного запроса
// расходились бы на составе столбцов или на порядке, то есть там, где
// расхождение не ломает сборку и не видно глазом.
func (q *quotaReader) ListStates(ctx context.Context, carrierType, carrierID string) ([]kacho.QuotaState, error) {
	return corequota.ListStates(ctx, q.tx, quotaSchema, carrierType, carrierID)
}

// quotaWriter — материализация поверх write-TX.
type quotaWriter struct {
	quotaReader
	tx pgx.Tx
}

// Materialize заводит строки учёта по всем видам, которые назвал владелец
// величин, и НЕ трогает уже заведённые.
//
// `ON CONFLICT DO NOTHING`, а не `UPSERT`: строка учёта несёт `used`, и перезапись
// снимка величиной из свежего резолва обнулила бы потребление у проекта, который
// уже что-то создал. Догоняющее обновление величины — предмет синхронизатора
// (дельта по ревизии), а не материализации; здесь заводится ТОЛЬКО отсутствующее.
//
// Идемпотентна by construction: повторный вызов на полностью материализованном
// проекте меняет ноль строк и стоит одного оператора. Это несущее свойство —
// материализация зовётся на промахе, а промах под конкуренцией случается у
// нескольких запросов сразу.
//
// Число заведённых строк возвращается, чтобы «ноль заведённых» было отличимо от
// «не звали»: механизм, не заведший ни одной строки за всю свою жизнь, — это
// находка, а не тишина.
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
func MaterializeQuotas(ctx context.Context, ex QuotaExecutor, rows []kacho.QuotaRow) (int64, error) {
	if len(rows) == 0 {
		return 0, nil
	}
	// `used` НЕ приходит из Go и не может: единственный, кто знает потребление, —
	// сама база. Прежняя редакция ставила здесь ноль, объясняя это тем, что
	// потребление создаёт триггер; утверждение верно ровно для проекта, у
	// которого на момент заведения строки ресурсов НЕТ, и ложно для всякого
	// другого. Затравка считается тем же отображением «вид → таблица», которым
	// ведёт списание, — оно читается у самих триггеров, поэтому разойтись со
	// списанием не может.
	//
	// Гонки здесь нет by construction: пока строки учёта нет, вставка строки
	// ресурса отвергается, значит множество, которое считают, между счётом и
	// вставкой не меняется. `ON CONFLICT DO NOTHING` при этом сохраняет прежний
	// смысл — потребление уже заведённой строки не переписывается.
	//
	// `COALESCE(…, 0)` покрывает вид, который не списывается НИЧЕМ: у такого вида
	// потребления не существует, и ноль здесь — не догадка, а единственное
	// возможное значение. Само же наличие таких видов — предмет отдельной
	// задачи про виды без производителя списания, а не материализации.
	const stmt = `
		INSERT INTO kacho_vpc.project_resource_quotas
		    (carrier_type, carrier_id, kind, used, limit_value,
		     source_scope, source_scope_id, limit_revision, account_id)
		SELECT s.carrier_type, s.carrier_id, s.kind,
		       COALESCE(kacho_vpc.kacho_quota_used_actual(
		           s.carrier_type, s.carrier_id, s.kind), 0),
		       s.limit_value, s.source_scope, s.source_scope_id,
		       s.limit_revision, s.account_id
		  FROM unnest(
		      $1::text[], $2::text[], $3::text[], $4::bigint[],
		      $5::text[], $6::text[], $7::bigint[], $8::text[])
		      AS s(carrier_type, carrier_id, kind, limit_value,
		           source_scope, source_scope_id, limit_revision, account_id)
		ON CONFLICT (carrier_type, carrier_id, kind) DO NOTHING`

	n := len(rows)
	carrierTypes := make([]string, n)
	carrierIDs := make([]string, n)
	kinds := make([]string, n)
	limits := make([]int64, n)
	scopes := make([]string, n)
	scopeIDs := make([]string, n)
	revisions := make([]int64, n)
	accounts := make([]string, n)
	for i, r := range rows {
		carrierTypes[i] = r.CarrierType
		carrierIDs[i] = r.CarrierID
		kinds[i] = r.Kind
		limits[i] = r.Limit
		scopes[i] = r.SourceScope
		scopeIDs[i] = r.SourceScopeID
		revisions[i] = r.LimitRevision
		accounts[i] = r.AccountID
	}

	tag, err := ex.Exec(ctx, stmt,
		carrierTypes, carrierIDs, kinds, limits, scopes, scopeIDs, revisions, accounts)
	if err != nil {
		return 0, helpers.WrapPgErr(err, "Quota", "")
	}
	return tag.RowsAffected(), nil
}
