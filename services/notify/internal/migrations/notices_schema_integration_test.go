// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// notices_schema_integration_test.go — пробы схемы извещений оператора, стадия S1
// (приёмка sub-phase-NTF-5-operator-notices-acceptance.md, Р4, Р5, Р8, Р10, Р14, Р19;
// замысел issue-2924 §6, З5 п.5, З6, З7, З9 п.1 и п.4, З21, З27).
//
// # Что утверждается
//
// Каждый инвариант, который схема берёт на себя, — отказом базы с ИМЕНЕМ
// ограничения: единственный дом отображения SQLSTATE службы переводит `23514` в
// закрытый текст приёмки по имени, поэтому безымянный или переименованный отказ
// — дефект, а не косметика.
//
// # Как построен каждый отрицательный кейс
//
// Строки собираются из базовых наборов столбцов (`row`), и отрицательный кейс
// меняет РОВНО ОДИН столбец одной строки против положительного близнеца, который
// стоит в той же таблице кейсов и обязан пройти. Так отказ доказывает предмет, а
// не поломку фикстуры: тот же набор без изменённого столбца базой принят.
package migrations_test

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/corelib/pgtest"

	"github.com/PRO-Robotech/kacho/services/notify/internal/migrations"
)

// Коды отказа, которых ждут пробы.
const (
	sqlStateCheck  = "23514"
	sqlStateFK     = "23503"
	sqlStateUnique = "23505"
)

// noticeTables — таблицы стадии S1, которые заводит цепочка (замысел §6, строка
// N1 маршрута). Кэша областей среди них нет: приёмка редакции 20 его снимает
// (Р10 «Кэша областей нет», Д131).
var noticeTables = []string{
	"notice_create_requests",
	"notices",
	"notice_audience",
	"notice_affected_resources",
	"notice_reminders",
	"notice_stage_events",
	"notice_counters",
}

// row — строка вставки: таблица и столбцы как SQL-выражения. Ключи
// упорядочиваются при сборке, поэтому текст оператора детерминирован.
type row struct {
	table string
	cols  map[string]string
}

// with — копия строки с одним изменённым столбцом (единственный изменённый факт
// отрицательного кейса). Значение "" снимает столбец из вставки.
func (r row) with(col, expr string) row {
	cols := make(map[string]string, len(r.cols)+1)
	for k, v := range r.cols {
		cols[k] = v
	}
	if expr == "" {
		delete(cols, col)
	} else {
		cols[col] = expr
	}
	return row{table: r.table, cols: cols}
}

func (r row) sql() string {
	keys := make([]string, 0, len(r.cols))
	for k := range r.cols {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	vals := make([]string, 0, len(keys))
	for _, k := range keys {
		vals = append(vals, r.cols[k])
	}
	return "INSERT INTO " + r.table + " (" + strings.Join(keys, ", ") +
		") VALUES (" + strings.Join(vals, ", ") + ")"
}

// Базовые строки — положительные экземпляры, каждый принят схемой (кейс «база»).
const (
	maintID   = "'ntc-0000000000000000a'"
	outageID  = "'ntc-0000000000000000b'"
	suspendID = "'ntc-0000000000000000c'"
	t0        = "'2026-10-01 00:00:00Z'"
	t1        = "'2026-10-02 00:00:00Z'"
	t2        = "'2026-10-02 02:00:00Z'"
	t3        = "'2026-10-02 03:00:00Z'"
)

var (
	requestBase = row{table: "notice_create_requests", cols: map[string]string{
		"notice_id":            maintID,
		"operation_id":         "'nop0000000000000000a'",
		"kind":                 "'MAINTENANCE'",
		"starts_at":            t1,
		"ends_at":              t2,
		"audience_all":         "false",
		"audience_account_ids": "'{acc00000000000000001,acc00000000000000002}'",
		"audience_project_ids": "'{}'",
		"affected_types":       "'{vpc_network}'",
		"affected_ids":         "'{enp00000000000000001}'",
		"created_by":           "'user:usr-op'",
		"accepted_at":          t0,
	}}

	maintBase = row{table: "notices", cols: map[string]string{
		"id":           maintID,
		"kind":         "'MAINTENANCE'",
		"state":        "'SCHEDULED'",
		"starts_at":    t1,
		"ends_at":      t2,
		"audience_all": "false",
		"revision":     "1",
		"created_by":   "'user:usr-op'",
		"created_at":   t0,
		"updated_at":   t0,
	}}

	outageBase = row{table: "notices", cols: map[string]string{
		"id":           outageID,
		"kind":         "'OUTAGE'",
		"state":        "'IN_PROGRESS'",
		"starts_at":    t0,
		"started_at":   t0,
		"audience_all": "true",
		"revision":     "1",
		"created_by":   "'user:usr-op'",
		"created_at":   t0,
		"updated_at":   t0,
	}}

	suspendBase = row{table: "notices", cols: map[string]string{
		"id":           suspendID,
		"kind":         "'SUSPENSION'",
		"state":        "'IN_PROGRESS'",
		"starts_at":    t0,
		"started_at":   t0,
		"audience_all": "false",
		"revision":     "1",
		"created_by":   "'user:usr-op'",
		"created_at":   t0,
		"updated_at":   t0,
	}}

	audience0 = row{table: "notice_audience", cols: map[string]string{
		"notice_id":  maintID,
		"ordinal":    "0",
		"scope_type": "'account'",
		"scope_id":   "'acc00000000000000001'",
		"account_id": "'acc00000000000000001'",
	}}
	audience1 = row{table: "notice_audience", cols: map[string]string{
		"notice_id":  maintID,
		"ordinal":    "1",
		"scope_type": "'account'",
		"scope_id":   "'acc00000000000000002'",
		"account_id": "'acc00000000000000002'",
	}}
	suspendAudience = audience0.with("notice_id", suspendID)

	affectedBase = row{table: "notice_affected_resources", cols: map[string]string{
		"notice_id":     maintID,
		"notice_kind":   "'MAINTENANCE'",
		"ordinal":       "0",
		"resource_type": "'vpc_network'",
		"resource_id":   "'enp00000000000000001'",
	}}

	reminderBase = row{table: "notice_reminders", cols: map[string]string{
		"notice_id": maintID,
		"at":        "'2026-10-01 23:00:00Z'",
		"revision":  "1",
	}}

	eventBase = row{table: "notice_stage_events", cols: map[string]string{
		"notice_id": maintID,
		"stage":     "'scheduled'",
		"revision":  "1",
		"due_at":    t0,
	}}

	counterBase = row{table: "notice_counters", cols: map[string]string{
		"name":   "'notify_notice_letters_total'",
		"labels": "'kind=MAINTENANCE,stage=scheduled,outcome=SENT'",
		"n":      "1",
	}}
)

// maintSet — извещение MAINTENANCE со всеми зависимыми строками; основа кейсов,
// меняющих одну зависимую строку.
func maintSet(rows ...row) []row {
	return append([]row{maintBase, audience0, audience1}, rows...)
}

type schemaCase struct {
	name       string
	rows       []row
	extra      []string // операторы после вставок, в той же транзакции
	code       string   // "" — положительный близнец: транзакция обязана зафиксироваться
	constraint string
}

func schemaCases() []schemaCase {
	return []schemaCase{
		// --- заявка создания (Р4, Р10, З3, З27) ---
		{name: "заявка/база", rows: []row{requestBase}},
		{name: "заявка/две формы аудитории", rows: []row{requestBase.with("audience_project_ids", "'{prj00000000000000001}'")},
			code: sqlStateCheck, constraint: "notice_create_requests_audience_one_form_chk"},
		{name: "заявка/нет формы аудитории", rows: []row{requestBase.with("audience_account_ids", "'{}'")},
			code: sqlStateCheck, constraint: "notice_create_requests_audience_one_form_chk"},
		{name: "заявка/все аккаунты — одна форма", rows: []row{requestBase.with("audience_account_ids", "'{}'").with("audience_all", "true")}},
		{name: "заявка/101 аккаунт", rows: []row{requestBase.with("audience_account_ids", "array_fill('acc00000000000000001'::text, ARRAY[101])")},
			code: sqlStateCheck, constraint: "notice_create_requests_audience_items_chk"},
		{name: "заявка/100 аккаунтов", rows: []row{requestBase.with("audience_account_ids", "array_fill('acc00000000000000001'::text, ARRAY[100])")}},
		{name: "заявка/пустой элемент аудитории", rows: []row{requestBase.with("audience_account_ids", "'{acc00000000000000001,\"\"}'")},
			code: sqlStateCheck, constraint: "notice_create_requests_audience_items_chk"},
		{name: "заявка/все аккаунты у приостановки", rows: []row{requestBase.
			with("kind", "'SUSPENSION'").with("starts_at", "").with("ends_at", "").
			with("affected_types", "").with("affected_ids", "").
			with("audience_account_ids", "'{}'").with("audience_all", "true")},
			code: sqlStateCheck, constraint: "notice_create_requests_audience_all_kind_chk"},
		{name: "заявка/приостановка по аккаунтам", rows: []row{requestBase.
			with("kind", "'SUSPENSION'").with("starts_at", "").with("ends_at", "").
			with("affected_types", "").with("affected_ids", "")}},
		{name: "заявка/endsAt у вывода из эксплуатации", rows: []row{requestBase.with("kind", "'DECOMMISSION'")},
			code: sqlStateCheck, constraint: "notice_create_requests_ends_at_kind_chk"},
		{name: "заявка/вывод из эксплуатации без endsAt", rows: []row{requestBase.with("kind", "'DECOMMISSION'").with("ends_at", "")}},
		{name: "заявка/startsAt у сбоя", rows: []row{requestBase.with("kind", "'OUTAGE'").with("ends_at", "")},
			code: sqlStateCheck, constraint: "notice_create_requests_starts_at_kind_chk"},
		{name: "заявка/сбой без startsAt", rows: []row{requestBase.with("kind", "'OUTAGE'").with("ends_at", "").with("starts_at", "")}},
		{name: "заявка/endsAt не позже startsAt", rows: []row{requestBase.with("ends_at", t1)},
			code: sqlStateCheck, constraint: "notice_create_requests_ends_after_starts_chk"},
		{name: "заявка/тип ресурса вне перечня", rows: []row{requestBase.with("affected_types", "'{vpc_address_pool}'")},
			code: sqlStateCheck, constraint: "notice_create_requests_affected_chk"},
		{name: "заявка/ресурсы у приостановки", rows: []row{requestBase.
			with("kind", "'SUSPENSION'").with("starts_at", "").with("ends_at", "")},
			code: sqlStateCheck, constraint: "notice_create_requests_affected_chk"},
		{name: "заявка/51 ресурс", rows: []row{requestBase.
			with("affected_types", "array_fill('vpc_network'::text, ARRAY[51])").
			with("affected_ids", "array_fill('enp00000000000000001'::text, ARRAY[51])")},
			code: sqlStateCheck, constraint: "notice_create_requests_affected_chk"},
		{name: "заявка/50 ресурсов", rows: []row{requestBase.
			with("affected_types", "array_fill('vpc_network'::text, ARRAY[50])").
			with("affected_ids", "array_fill('enp00000000000000001'::text, ARRAY[50])")}},
		{name: "заявка/типов и id разное число", rows: []row{requestBase.with("affected_ids", "'{}'")},
			code: sqlStateCheck, constraint: "notice_create_requests_affected_chk"},
		{name: "заявка/аренда без срока", rows: []row{requestBase.with("lease_token", "gen_random_uuid()")},
			code: sqlStateCheck, constraint: "notice_create_requests_lease_pair_chk"},
		{name: "заявка/аренда со сроком", rows: []row{requestBase.with("lease_token", "gen_random_uuid()").with("lease_until", t1)}},
		{name: "заявка/операция занята", rows: []row{requestBase, requestBase.with("notice_id", outageID)},
			code: sqlStateUnique, constraint: "notice_create_requests_operation_uniq"},
		{name: "заявка/пустой создатель", rows: []row{requestBase.with("created_by", "''")},
			code: sqlStateCheck, constraint: "notice_create_requests_created_by_chk"},

		// --- извещение (Р4, Р5, З5 п.5) ---
		{name: "извещение/база", rows: maintSet()},
		{name: "извещение/вид вне перечня", rows: []row{outageBase.with("kind", "'BROWNOUT'")},
			code: sqlStateCheck, constraint: "notices_kind_chk"},
		{name: "извещение/состояние вне перечня", rows: []row{outageBase.with("state", "'PAUSED'")},
			code: sqlStateCheck, constraint: "notices_state_chk"},
		{name: "извещение/работы без endsAt", rows: []row{maintBase.with("ends_at", ""), audience0},
			code: sqlStateCheck, constraint: "notices_ends_at_kind_chk"},
		{name: "извещение/endsAt у сбоя", rows: []row{outageBase.with("ends_at", t3)},
			code: sqlStateCheck, constraint: "notices_ends_at_kind_chk"},
		{name: "извещение/endsAt не позже startsAt", rows: []row{maintBase.with("ends_at", t1), audience0},
			code: sqlStateCheck, constraint: "notices_ends_after_starts_chk"},
		{name: "извещение/сбой в SCHEDULED", rows: []row{outageBase.with("state", "'SCHEDULED'").with("started_at", "")},
			code: sqlStateCheck, constraint: "notices_born_in_progress_chk"},
		{name: "извещение/сбой: startsAt не момент создания", rows: []row{outageBase.with("starts_at", t1).with("started_at", t1).with("updated_at", t1)},
			code: sqlStateCheck, constraint: "notices_born_in_progress_chk"},
		{name: "извещение/сбой база", rows: []row{outageBase}},
		{name: "извещение/работы: startsAt не позже создания", rows: []row{maintBase.with("starts_at", t0), audience0},
			code: sqlStateCheck, constraint: "notices_starts_after_created_chk"},
		{name: "извещение/COMPLETED без completedAt", rows: []row{outageBase.with("state", "'COMPLETED'")},
			code: sqlStateCheck, constraint: "notices_state_moments_chk"},
		{name: "извещение/COMPLETED с completedAt", rows: []row{outageBase.with("state", "'COMPLETED'").with("completed_at", t1).with("updated_at", t1)}},
		{name: "извещение/startedAt в SCHEDULED", rows: []row{maintBase.with("started_at", t1), audience0},
			code: sqlStateCheck, constraint: "notices_state_moments_chk"},
		{name: "извещение/CANCELLED с cancelledAt", rows: []row{maintBase.with("state", "'CANCELLED'").with("cancelled_at", t0), audience0}},
		{name: "извещение/completedAt раньше startedAt", rows: []row{outageBase.with("state", "'COMPLETED'").with("completed_at", "'2026-09-30 00:00:00Z'")},
			code: sqlStateCheck, constraint: "notices_moments_order_chk"},
		{name: "извещение/ревизия 0", rows: []row{maintBase.with("revision", "0"), audience0},
			code: sqlStateCheck, constraint: "notices_revision_chk"},
		{name: "извещение/ревизия 2 у сбоя", rows: []row{outageBase.with("revision", "2")},
			code: sqlStateCheck, constraint: "notices_revision_chk"},
		{name: "извещение/ревизия 2 у работ", rows: []row{maintBase.with("revision", "2"), audience0}},
		{name: "извещение/все аккаунты у приостановки", rows: []row{suspendBase.with("audience_all", "true")},
			code: sqlStateCheck, constraint: "notices_audience_all_kind_chk"},

		// --- аудитория: форма проверяется при фиксации (Р10) ---
		{name: "аудитория/приостановка по аккаунту", rows: []row{suspendBase, suspendAudience}},
		{name: "аудитория/строки при «все аккаунты»", rows: []row{outageBase, audience0.with("notice_id", outageID)},
			code: sqlStateCheck, constraint: "notices_audience_form_chk"},
		{name: "аудитория/ни одной строки без «все аккаунты»", rows: []row{suspendBase},
			code: sqlStateCheck, constraint: "notices_audience_form_chk"},
		{name: "аудитория/аккаунт и проект в одной аудитории", rows: []row{suspendBase, suspendAudience,
			suspendAudience.with("ordinal", "1").with("scope_type", "'project'").with("scope_id", "'prj00000000000000001'")},
			code: sqlStateCheck, constraint: "notices_audience_form_chk"},
		{name: "аудитория/два проекта", rows: []row{suspendBase,
			suspendAudience.with("scope_type", "'project'").with("scope_id", "'prj00000000000000001'"),
			suspendAudience.with("ordinal", "1").with("scope_type", "'project'").with("scope_id", "'prj00000000000000002'")}},
		{name: "аудитория/пропуск порядкового номера", rows: []row{suspendBase, suspendAudience.with("ordinal", "1")},
			code: sqlStateCheck, constraint: "notices_audience_form_chk"},
		{name: "аудитория/аккаунт области-аккаунта не она сама", rows: []row{suspendBase, suspendAudience.with("account_id", "'acc00000000000000009'")},
			code: sqlStateCheck, constraint: "notice_audience_account_scope_chk"},
		{name: "аудитория/номер 100", rows: []row{suspendBase, suspendAudience.with("ordinal", "100")},
			code: sqlStateCheck, constraint: "notice_audience_ordinal_chk"},
		{name: "аудитория/извещения нет", rows: []row{audience0},
			code: sqlStateFK, constraint: "notice_audience_notice_fk"},
		{name: "аудитория/область дважды", rows: []row{suspendBase, suspendAudience, suspendAudience.with("ordinal", "1")},
			code: sqlStateUnique, constraint: "notice_audience_pkey"},

		// --- затронутые ресурсы (Р4) ---
		{name: "ресурсы/база", rows: maintSet(affectedBase)},
		{name: "ресурсы/у приостановки", rows: []row{suspendBase, suspendAudience,
			affectedBase.with("notice_id", suspendID).with("notice_kind", "'SUSPENSION'")},
			code: sqlStateCheck, constraint: "notice_affected_resources_kind_chk"},
		{name: "ресурсы/вид не совпал с извещением", rows: maintSet(affectedBase.with("notice_kind", "'OUTAGE'")),
			code: sqlStateFK, constraint: "notice_affected_resources_notice_fk"},
		{name: "ресурсы/тип вне перечня", rows: maintSet(affectedBase.with("resource_type", "'vpc_address_pool'")),
			code: sqlStateCheck, constraint: "notice_affected_resources_type_chk"},
		{name: "ресурсы/номер 50", rows: maintSet(affectedBase.with("ordinal", "50")),
			code: sqlStateCheck, constraint: "notice_affected_resources_ordinal_chk"},
		{name: "ресурсы/номер 49", rows: maintSet(affectedBase.with("ordinal", "49"))},
		{name: "ресурсы/пустой id", rows: maintSet(affectedBase.with("resource_id", "''")),
			code: sqlStateCheck, constraint: "notice_affected_resources_id_chk"},

		// --- напоминания (Р5, З7) ---
		{name: "напоминания/база", rows: maintSet(reminderBase)},
		{name: "напоминания/ревизия 0", rows: maintSet(reminderBase.with("revision", "0")),
			code: sqlStateCheck, constraint: "notice_reminders_revision_chk"},
		{name: "напоминания/тот же момент дважды", rows: maintSet(reminderBase, reminderBase.with("revision", "2")),
			code: sqlStateUnique, constraint: "notice_reminders_pkey"},

		// --- события этапа (Р8, З6, З9, З27) ---
		{name: "события/база", rows: maintSet(eventBase)},
		{name: "события/этап вне перечня", rows: maintSet(eventBase.with("stage", "'paused'")),
			code: sqlStateCheck, constraint: "notice_stage_events_stage_chk"},
		{name: "события/без dueAt", rows: maintSet(eventBase.with("due_at", "")),
			code: "23502", constraint: ""},
		{name: "события/то же событие дважды", rows: maintSet(eventBase, eventBase),
			code: sqlStateUnique, constraint: "notice_stage_events_event_uniq"},
		{name: "события/три напоминания одной ревизии", rows: maintSet(
			eventBase.with("stage", "'reminder'"),
			eventBase.with("stage", "'reminder'").with("due_at", t1),
			eventBase.with("stage", "'reminder'").with("due_at", t2))},
		{name: "события/позиция областей отрицательна", rows: maintSet(eventBase.with("scope_done", "-1")),
			code: sqlStateCheck, constraint: "notice_stage_events_scope_done_chk"},
		{name: "события/позиция областей 0", rows: maintSet(eventBase.with("scope_done", "0"))},
		{name: "события/пустой курсор", rows: maintSet(eventBase.with("page_token", "''")),
			code: sqlStateCheck, constraint: "notice_stage_events_page_token_chk"},
		{name: "события/курсор задан", rows: maintSet(eventBase.with("page_token", "'cursor-1'"))},
		{name: "события/две формы позиции", rows: maintSet(eventBase.with("page_token", "'cursor-1'").with("scope_done", "0")),
			code: sqlStateCheck, constraint: "notice_stage_events_position_chk"},
		{name: "события/срок аренды без токена", rows: maintSet(eventBase.with("lease_until", t1)),
			code: sqlStateCheck, constraint: "notice_stage_events_lease_pair_chk"},
		{name: "события/аренда целиком", rows: maintSet(eventBase.with("lease_until", t1).with("lease_token", "gen_random_uuid()"))},
		{name: "события/ревизия 0", rows: maintSet(eventBase.with("revision", "0")),
			code: sqlStateCheck, constraint: "notice_stage_events_revision_chk"},

		// --- счётчики (Р19, З21) ---
		{name: "счётчики/база", rows: []row{counterBase}},
		{name: "счётчики/имя вне перечня", rows: []row{counterBase.with("name", "'notify_notice_whatever_total'")},
			code: sqlStateCheck, constraint: "notice_counters_name_chk"},
		{name: "счётчики/отрицательное значение", rows: []row{counterBase.with("n", "-1")},
			code: sqlStateCheck, constraint: "notice_counters_n_chk"},
		{name: "счётчики/приращение одной функцией в порядке ключа", rows: []row{counterBase},
			extra: []string{`INSERT INTO notice_counters (name, labels, n)
SELECT * FROM unnest(
  ARRAY['notify_notice_letters_total','notify_notice_unaddressed_total']::text[],
  ARRAY['kind=MAINTENANCE,stage=scheduled,outcome=SENT','kind=MAINTENANCE,stage=scheduled']::text[],
  ARRAY[2,1]::bigint[]) ORDER BY 1, 2
ON CONFLICT (name, labels) DO UPDATE SET n = notice_counters.n + EXCLUDED.n`}},
	}
}

// TestNoticeSchemaHoldsItsInvariants — каждый инвариант схемы S1 отвергается
// базой с именем ограничения, а его положительный близнец фиксируется.
func TestNoticeSchemaHoldsItsInvariants(t *testing.T) {
	cases := schemaCases()
	var negatives, positives int
	for _, c := range cases {
		if c.code == "" {
			positives++
		} else {
			negatives++
		}
	}
	t.Logf("кейсов %d: отрицательных %d, положительных близнецов %d", len(cases), negatives, positives)
	require.Positive(t, negatives, "обход пуст — «ноль отказов» неотличим от «ничего не проверено»")
	require.Positive(t, positives)

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx := context.Background()
			conn := openNoticeDB(t)
			err := applyInOneTx(ctx, conn, c)
			if c.code == "" {
				require.NoError(t, err, "положительный близнец отвергнут — фикстура или схема сломана")
				return
			}
			var pgErr *pgconn.PgError
			require.Truef(t, errors.As(err, &pgErr),
				"ждали отказ базы %s (%s), получили %v", c.code, c.constraint, err)
			require.Equal(t, c.code, pgErr.Code, "SQLSTATE: %s", pgErr.Message)
			require.Equal(t, c.constraint, pgErr.ConstraintName, "имя ограничения: %s", pgErr.Message)
		})
	}
}

// TestNoticeDeleteCascadesToItsRows — удаление извещения (подметальщик Р14, З24
// шаг 7) снимает аудиторию, ресурсы, напоминания и события той же базой; чужого
// извещения и заявки оно не трогает.
func TestNoticeDeleteCascadesToItsRows(t *testing.T) {
	ctx := context.Background()
	conn := openNoticeDB(t)
	require.NoError(t, applyInOneTx(ctx, conn, schemaCase{
		rows: append(maintSet(affectedBase, reminderBase, eventBase), suspendBase, suspendAudience, requestBase.with("notice_id", "'ntc-0000000000000000z'")),
	}))

	tag, err := conn.Exec(ctx, "DELETE FROM notices WHERE id = "+maintID)
	require.NoError(t, err)
	require.EqualValues(t, 1, tag.RowsAffected())

	for _, tbl := range []string{"notice_audience", "notice_affected_resources", "notice_reminders", "notice_stage_events"} {
		var n int
		require.NoError(t, conn.QueryRow(ctx, "SELECT count(*) FROM "+tbl+" WHERE notice_id = "+maintID).Scan(&n))
		require.Zero(t, n, "%s: строки удалённого извещения остались", tbl)
	}
	var left int
	require.NoError(t, conn.QueryRow(ctx, "SELECT count(*) FROM notice_audience WHERE notice_id = "+suspendID).Scan(&left))
	require.Equal(t, 1, left, "каскад задел чужое извещение")
	require.NoError(t, conn.QueryRow(ctx, "SELECT count(*) FROM notice_create_requests").Scan(&left))
	require.Equal(t, 1, left, "заявка не связана с извещением ключом и каскадом не снимается")
}

// TestNoticeChainAppliesOnEmptyDatabaseAndRollsBack — цепочка kacho_notify
// накатывается на ПУСТУЮ базу, откатывается до нуля и накатывается снова; после
// наката каждая таблица S1 существует, после отката — ни одной.
func TestNoticeChainAppliesOnEmptyDatabaseAndRollsBack(t *testing.T) {
	ctx := context.Background()
	dsn := pgtest.NewEmptyDB(t)
	db, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	goose.SetBaseFS(migrations.FS)
	require.NoError(t, goose.SetDialect("postgres"))

	require.NoError(t, goose.Up(db, "."), "накат на пустую базу")
	requireTables(ctx, t, db, true)
	require.NoError(t, goose.DownTo(db, ".", 0), "откат до нуля")
	requireTables(ctx, t, db, false)
	require.NoError(t, goose.Up(db, "."), "повторный накат после отката")
	requireTables(ctx, t, db, true)
}

func requireTables(ctx context.Context, t *testing.T, db *sql.DB, present bool) {
	t.Helper()
	for _, tbl := range noticeTables {
		var exists bool
		require.NoError(t, db.QueryRowContext(ctx, "SELECT to_regclass($1) IS NOT NULL", tbl).Scan(&exists))
		require.Equalf(t, present, exists, "таблица %s", tbl)
	}
}

// openNoticeDB — собственная база пробы: клон шаблона с цепочкой до головы.
func openNoticeDB(t *testing.T) *pgx.Conn {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, pgtest.NewDB(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	return conn
}

// applyInOneTx исполняет вставки кейса и его операторы одной транзакцией и
// фиксирует её: ограничения, отложенные до фиксации (форма аудитории), судятся
// именно здесь. Первый отказ возвращается как есть.
func applyInOneTx(ctx context.Context, conn *pgx.Conn, c schemaCase) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, r := range c.rows {
		if _, err := tx.Exec(ctx, r.sql()); err != nil {
			return err
		}
	}
	for _, s := range c.extra {
		if _, err := tx.Exec(ctx, s); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
