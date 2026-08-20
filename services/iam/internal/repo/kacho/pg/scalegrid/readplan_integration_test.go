// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package scalegrid_test

// ПРИБОР ОБЯЗАН ЧИТАТЬ ТЕМ ЖЕ ЗАХОДОМ, КАКИМ ЧИТАЕТ ИЗМЕРЯЕМОЕ.
//
// Ветвь выдач вердикта заходит в `access_binding_subjects` ПАРОЙ КОЛОНОК
// (`grantArmSQL`: bs.subject_type = sp.s_type AND bs.subject_id = sp.s_id), и
// ради этого захода заведён индекс
// `access_binding_subjects_subject_scope_idx (subject_type, subject_id,
// resource_type, resource_id)` (миграция 732001). Прибор, спрашивающий ту же
// таблицу СКЛЕЙКОЙ `subject_type || ':' || subject_id`, выводит колонки из-под
// этого индекса: вычисленное значение отбирает строки только ПОСЛЕ того, как
// они прочитаны.
//
// ПОЧЕМУ ЭТО НЕЛЬЗЯ ПРОВЕРИТЬ НИ ТЕКСТОМ, НИ ЧИСЛОМ. Обе формы возвращают ОДНО
// И ТО ЖЕ ЧИСЛО — и это здесь утверждается отдельно, потому что именно
// одинаковость числа делала расхождение тихим: перепись «не двигалась», а
// двигалась цена. Текст же запроса гейт по подстроке нашёл бы и в комментарии,
// объясняющем этот самый запрет. Спросить можно только план — у настоящей базы
// с настоящей схемой.
//
// ПРОБА НЕСЁТ СВОЙ ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ: рядом с формой прибора она гоняет
// склейку и ТРЕБУЕТ от неё сплошного чтения. Без него «индекса не видно» было
// бы неотличимо от «строк так мало, что планировщик выбрал бы сплошное чтение
// при любой форме», и проба зеленела бы на непосаженной фикстуре.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PRO-Robotech/kacho/internal/pgtest"
	"github.com/PRO-Robotech/kacho/services/iam/internal/repo/kacho/pg/scalegrid"
)

// readPlanRows — сколько строк таблицы прочитано и каким узлом.
type readPlanRows struct {
	nodes     []string
	scanned   float64
	usedIndex bool
	seqScan   bool
}

const (
	// subjectsSeeded — объём, при котором сплошное чтение отличимо от захода по
	// указателю. Число названо здесь, а не подобрано: положительный контроль
	// ниже ТРЕБУЕТ от склейки сплошного чтения, поэтому недосев уронит пробу, а
	// не позеленит её.
	subjectsSeeded = 4000
	// bindingSubjectsTable — имя таблицы, чьё чтение судит проба.
	bindingSubjectsTable = "access_binding_subjects"
	// wantIndex — индекс, ради которого заведена форма захода (732001).
	wantIndex = "access_binding_subjects_subject_scope_idx"
)

func TestCensusReadsBindingSubjectsThroughTheIndexTheVerdictUses(t *testing.T) {
	ctx := context.Background()
	pool := seedBindingSubjects(t, ctx)

	speakers := []string{
		"user:usr-0000000042",
		"group:grp-000042",
		"group:grp-000042#member", // форма членства — её не несёт ни одна строка
		"user:*",
	}

	// Склейка — та самая форма, из-под которой индекс не работает. Держится
	// здесь ДОСЛОВНО как положительный контроль объёма фикстуры.
	const concatenated = `SELECT count(*)::bigint
		   FROM kacho_iam.access_binding_subjects bs
		  WHERE bs.subject_type || ':' || bs.subject_id = ANY($1::text[])`

	t.Run("перепись выдач: число обеих форм совпадает", func(t *testing.T) {
		got := scalarOf(t, ctx, pool, scalegrid.BindingsNamingSubjectSQL, speakers)
		want := scalarOf(t, ctx, pool, concatenated, speakers)
		if got != want {
			t.Fatalf("прибор считает %d, склейка — %d: правка захода ИЗМЕНИЛА перепись, "+
				"а обязана была изменить только её цену", got, want)
		}
		if got == 0 {
			t.Fatalf("перепись выдач нулевая — условие пробы не создано, а не свойство нарушено")
		}
	})

	t.Run("положительный контроль: склейка читает таблицу сплошь", func(t *testing.T) {
		p := explain(t, ctx, pool, concatenated, speakers)
		if !p.seqScan {
			t.Fatalf("склейка НЕ дала сплошного чтения (узлы: %s) — фикстура мала либо "+
				"планировщик изменился; отрицательное утверждение ниже на такой фикстуре "+
				"зеленело бы при любой форме и ничего бы не значило", strings.Join(p.nodes, ", "))
		}
		if p.scanned < subjectsSeeded {
			t.Fatalf("склейка прочитала %.0f строк при %d посаженных — посев неполон",
				p.scanned, subjectsSeeded)
		}
	})

	t.Run("прибор: заход по индексу, а не сплошное чтение", func(t *testing.T) {
		p := explain(t, ctx, pool, scalegrid.BindingsNamingSubjectSQL, speakers)
		assertIndexRead(t, p, len(speakers))
	})
}

func TestStrengthCensusReadsSpeakerScopeThroughTheIndexTheVerdictUses(t *testing.T) {
	ctx := context.Background()
	pool := seedBindingSubjects(t, ctx)

	speakers := []string{"user:usr-0000000042", "group:grp-000042", "user:*"}
	const scopeType, scopeID = "project", "prj-000042"

	const concatenated = `SELECT count(*)::bigint
		   FROM kacho_iam.access_binding_subjects bs
		   JOIN kacho_iam.access_bindings b ON b.id = bs.binding_id
		  WHERE bs.subject_type || ':' || bs.subject_id = ANY($1::text[])
		    AND bs.resource_type = $2 AND bs.resource_id = $3
		    AND b.status = 'ACTIVE' AND b.revoked_at IS NULL`

	t.Run("перепись пар: число обеих форм совпадает", func(t *testing.T) {
		got := scalarOf(t, ctx, pool, scalegrid.SpeakerScopeRowsSQL, speakers, scopeType, scopeID)
		want := scalarOf(t, ctx, pool, concatenated, speakers, scopeType, scopeID)
		if got != want {
			t.Fatalf("прибор считает %d, склейка — %d: правка захода изменила перепись", got, want)
		}
	})

	t.Run("положительный контроль: склейка читает таблицу сплошь", func(t *testing.T) {
		p := explain(t, ctx, pool, concatenated, speakers, scopeType, scopeID)
		if !p.seqScan {
			t.Fatalf("склейка НЕ дала сплошного чтения (узлы: %s) — фикстура мала",
				strings.Join(p.nodes, ", "))
		}
	})

	t.Run("прибор: заход по индексу, а не сплошное чтение", func(t *testing.T) {
		p := explain(t, ctx, pool, scalegrid.SpeakerScopeRowsSQL, speakers, scopeType, scopeID)
		assertIndexRead(t, p, len(speakers))
	})
}

// assertIndexRead — общее утверждение обеих проб: таблица не читается сплошь,
// заход идёт тем индексом, ради которого форма и заведена, а прочитано не
// больше, чем говорящих (по одному обращению на каждого).
func assertIndexRead(t *testing.T, p readPlanRows, speakers int) {
	t.Helper()
	if p.seqScan {
		t.Errorf("прибор читает %s СПЛОШЬ (%.0f строк): склейка колонок выводит их "+
			"из-под индекса %s, и прибор мерит НЕ ТОТ заход, каким читает вердикт.\n"+
			"  узлы плана: %s", bindingSubjectsTable, p.scanned, wantIndex,
			strings.Join(p.nodes, ", "))
	}
	if !p.usedIndex {
		t.Errorf("в плане нет обращения к %s — заход, ради которого заведён индекс "+
			"миграцией 732001, не состоялся.\n  узлы плана: %s", wantIndex,
			strings.Join(p.nodes, ", "))
	}
	if p.scanned > float64(speakers) {
		t.Errorf("прочитано %.0f строк %s при %d говорящих: заход обязан стоить одно "+
			"обращение на говорящего", p.scanned, bindingSubjectsTable, speakers)
	}
}

// ---------------------------------------------------------------------------

func seedBindingSubjects(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	dsn := pgtest.NewDB(t)
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("пул: %v", err)
	}
	pgtest.ClosePoolAtEnd(t, pool)

	stmts := []string{
		// Субъекты выдач обязаны существовать: их наличие сторожит триггер.
		// Посев идёт ЧЕРЕЗ него, а не в обход, иначе фикстура была бы
		// снисходительнее продукта.
		`INSERT INTO kacho_iam.accounts (id, name, owner_user_id)
		 VALUES ('acc-probe', 'acc-readplan-probe', 'usr-0000000001')
		 ON CONFLICT DO NOTHING`,
		`INSERT INTO kacho_iam.users (id, external_id, email, account_id)
		 SELECT 'usr-' || lpad(i::text, 10, '0'), 'ext-' || i::text,
		        'u' || i::text || '@probe.invalid', 'acc-probe'
		   FROM generate_series(1, $1::int) i`,
		`INSERT INTO kacho_iam.groups (id, account_id, name)
		 SELECT 'grp-' || lpad(i::text, 6, '0'), 'acc-probe',
		        'g-' || lpad(i::text, 6, '0')
		   FROM generate_series(0, 499) i`,
		// Роль берётся ЗАСЕЯННАЯ миграциями, а не заводится своя: форма
		// permissions сторожится CHECK'ом, и собственная роль пробы
		// проверяла бы ЕЁ, а не предмет.
		//
		// Область своя у каждой строки: частичный UNIQUE
		// access_bindings_active_grant_uniq (0003) держит пятёрку
		// (субъект, роль, область) и иначе отверг бы повторы группы.
		`INSERT INTO kacho_iam.access_bindings
		   (id, resource_type, resource_id, role_id, subject_type, subject_id, status)
		 SELECT 'acb-' || lpad(i::text, 10, '0'), 'project',
		        'prj-' || lpad(i::text, 6, '0'),
		        (SELECT r.id FROM kacho_iam.roles r ORDER BY r.id LIMIT 1),
		        CASE WHEN i % 3 = 0 THEN 'group' ELSE 'user' END,
		        CASE WHEN i % 3 = 0 THEN 'grp-' || lpad((i % 500)::text, 6, '0')
		             ELSE 'usr-' || lpad(i::text, 10, '0') END,
		        'ACTIVE'
		   FROM generate_series(1, $1::int) i`,
		// Зеркало субъекта — тем же составом колонок, каким его пишет продукт
		// (область на строке субъекта, миграция 732001). ON CONFLICT нужен
		// потому, что миграции сеют собственную выдачу, и её строка уже здесь.
		`INSERT INTO kacho_iam.access_binding_subjects
		   (binding_id, subject_type, subject_id, ordinal, resource_type, resource_id)
		 SELECT b.id, b.subject_type, b.subject_id, 0, b.resource_type, b.resource_id
		   FROM kacho_iam.access_bindings b
		 ON CONFLICT DO NOTHING`,
	}
	// ОДНОЙ ТРАНЗАКЦИЕЙ, и это не оформление: accounts.owner_user_id и
	// users.account_id ссылаются друг на друга, а их внешние ключи объявлены
	// DEFERRABLE INITIALLY DEFERRED — проверка приходит на COMMIT. Каждым
	// стейтментом порознь (autocommit) посев неразрешим ни в каком порядке.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("транзакция посева: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, s := range stmts {
		var args []any
		if strings.Contains(s, "$1") {
			args = append(args, subjectsSeeded)
		}
		if _, err := tx.Exec(ctx, s, args...); err != nil {
			t.Fatalf("посев (%s…): %v", firstWords(s), err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("фиксация посева: %v", err)
	}
	// Статистика — ПОСЛЕ фиксации: без неё планировщик судит по умолчаниям, и
	// «какой план выбран» перестаёт быть свойством формы запроса.
	for _, a := range []string{
		`ANALYZE kacho_iam.access_bindings`,
		`ANALYZE kacho_iam.access_binding_subjects`,
	} {
		if _, err := pool.Exec(ctx, a); err != nil {
			t.Fatalf("сбор статистики (%s): %v", a, err)
		}
	}

	var n int64
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM kacho_iam.access_binding_subjects`).Scan(&n); err != nil {
		t.Fatalf("перепись посева: %v", err)
	}
	if n < subjectsSeeded {
		t.Fatalf("посеяно %d строк из %d — УСЛОВИЕ ПРОБЫ НЕ СОЗДАНО (третий исход), "+
			"а не свойство нарушено", n, subjectsSeeded)
	}
	t.Logf("посеяно строк %s: %d", bindingSubjectsTable, n)
	return pool
}

func scalarOf(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := pool.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
		t.Fatalf("счёт (%s…): %v", firstWords(sql), err)
	}
	return n
}

// explain — план ЗАПРОСА ПРИБОРА, а не его пересказа: sql приходит константой
// пакета, проба его не переписывает.
func explain(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) readPlanRows {
	t.Helper()
	var raw []byte
	q := "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) " + sql
	if err := pool.QueryRow(ctx, q, args...).Scan(&raw); err != nil {
		t.Fatalf("EXPLAIN (%s…): %v", firstWords(sql), err)
	}
	var plans []struct {
		Plan map[string]any `json:"Plan"`
	}
	if err := json.Unmarshal(raw, &plans); err != nil {
		t.Fatalf("разбор плана: %v", err)
	}
	if len(plans) == 0 {
		t.Fatalf("EXPLAIN вернул пустой план — вердикта у пробы нет")
	}
	var p readPlanRows
	walkPlan(plans[0].Plan, &p)
	if len(p.nodes) == 0 {
		t.Fatalf("в плане нет ни одного узла по %s — проба не прочитала того, о чём судит",
			bindingSubjectsTable)
	}
	t.Logf("план по %s: %s; строк прочитано %.0f", bindingSubjectsTable,
		strings.Join(p.nodes, ", "), p.scanned)
	return p
}

// walkPlan — узлы, читающие ИМЕННО ту таблицу, о которой проба судит. Соседние
// узлы (обращение к access_bindings по ключу) в счёт не идут: иначе «прочитано
// строк» смешало бы две разные величины.
func walkPlan(node map[string]any, out *readPlanRows) {
	rel, _ := node["Relation Name"].(string)
	alias, _ := node["Alias"].(string)
	if rel == bindingSubjectsTable || alias == "bs" {
		nodeType, _ := node["Node Type"].(string)
		idx, _ := node["Index Name"].(string)
		label := nodeType
		if idx != "" {
			label += " using " + idx
		}
		out.nodes = append(out.nodes, label)
		if strings.Contains(nodeType, "Seq Scan") {
			out.seqScan = true
		}
		if idx == wantIndex {
			out.usedIndex = true
		}
		rows, _ := node["Actual Rows"].(float64)
		loops, _ := node["Actual Loops"].(float64)
		if loops < 1 {
			loops = 1
		}
		// Сплошное чтение считается ОДНИМ проходом на цикл; заход по указателю —
		// по обращению на говорящего. Множитель на циклы обязателен: без него
		// четыре обращения по одной строке выглядели бы как одна.
		if strings.Contains(nodeType, "Seq Scan") {
			removed, _ := node["Rows Removed by Filter"].(float64)
			out.scanned += (rows + removed) * loops
		} else {
			out.scanned += rows * loops
		}
	}
	for _, key := range []string{"Plans"} {
		kids, _ := node[key].([]any)
		for _, k := range kids {
			if m, ok := k.(map[string]any); ok {
				walkPlan(m, out)
			}
		}
	}
}

func firstWords(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if len(s) > 60 {
		s = s[:60]
	}
	return s
}

var _ = pgx.ErrNoRows
var _ = fmt.Sprintf
