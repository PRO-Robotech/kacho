// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// system_role_segments_resolve_integration_test.go — ИСХОД миграции
// 20260901231022_system_role_rules_speak_the_verb_dictionary.
//
// Гейт дерева на глагольную половину сегмента живёт рядом
// (seed_rule_verb_resolvability_integration_test.go) и судит СВОЙСТВО дерева;
// здесь утверждается то, что миграция обязана произвести и обязана НЕ тронуть.
//
// Приёмка: services/iam/docs/engineering/acceptance/system-role-segments-resolve.md
// Сценарии IAM-SV-1-01, -04, -07, -12, -13, -14. Задача продукта kacho#1815.
package pg_test

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/internal/pgtest"

	"github.com/PRO-Robotech/kacho/services/iam/internal/authzmap"
	"github.com/PRO-Robotech/kacho/services/iam/internal/domain"
	"github.com/PRO-Robotech/kacho/services/iam/internal/migrations"
)

// segmentMigrationPrefix — префикс имени миграции-предмета. Тело её `Up`
// читается ИЗ ПОСТАВЛЯЕМОГО ФАЙЛА (migrations.FS), а не переписывается в
// пробу: копия текста была бы вторым местом об одном предмете и разошлась бы с
// оригиналом молча.
const segmentMigrationPrefix = "20260901231022"

// TestIAMSV101_RuleRefOrphanTraceIsGone — IAM-SV-1-01: следа объявления не
// остаётся.
//
// `role_grant_orphan` со `source = 'rule_ref'` — запись о ПЕРЕСЕЛЕНИИ
// объявления, а не о выдаче. Запись, которой больше нечего описывать, —
// находка: она утверждала бы, что объявление живо, при том что его нет.
func TestIAMSV101_RuleRefOrphanTraceIsGone(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: требует Postgres")
	}
	ctx := context.Background()

	pool, err := pgxpool.New(ctx, setupTestDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)

	// Перепись — до вердикта, и по ОБЕИМ популяциям следа: «ноль строк
	// объявления» обязано быть отличимо от «таблица пуста целиком».
	rows, err := pool.Query(ctx, `
		SELECT source, count(*) FROM kacho_iam.role_grant_orphan GROUP BY source ORDER BY source`)
	require.NoError(t, err)
	bySource := map[string]int{}
	for rows.Next() {
		var src string
		var n int
		require.NoError(t, rows.Scan(&src, &n))
		bySource[src] = n
	}
	require.NoError(t, rows.Err())
	t.Logf("след сирот: role_verb=%d, rule_ref=%d", bySource["role_verb"], bySource["rule_ref"])

	// Предпосылка: таблица следа существует и адресуема. Проверяется явно —
	// иначе «ноль строк» приходило бы и от несуществующего предмета.
	var relKind string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT relkind FROM pg_class WHERE oid = 'kacho_iam.role_grant_orphan'::regclass`).Scan(&relKind),
		"предпосылка нарушена: таблицы следа нет — «ноль строк» получено даром")
	require.Equal(t, "r", relKind)

	detail, err := pool.Query(ctx, `
		SELECT object_type, verb, count(*)
		  FROM kacho_iam.role_grant_orphan
		 WHERE source = 'rule_ref'
		 GROUP BY 1, 2 ORDER BY 1, 2`)
	require.NoError(t, err)
	var left []string
	for detail.Next() {
		var objectType, verb string
		var n int
		require.NoError(t, detail.Scan(&objectType, &verb, &n))
		left = append(left, fmt.Sprintf("%s.%s ×%d", objectType, verb, n))
	}
	require.NoError(t, detail.Err())

	require.Emptyf(t, left,
		"объявленные сегменты, не резолвящиеся ни в одно право, остались в следе: %s.\n"+
			"Их снимает миграция %s вместе с их предметом — глаголом правила; "+
			"остаток означает, что снят не весь предмет либо предикат снятия несимметричен "+
			"предикату постановки (kacho#1815, §2.7 приёмки).", strings.Join(left, ", "), segmentMigrationPrefix)
}

// TestIAMSV107_RuleRefProjectionEqualsWhatRulesDeclare — IAM-SV-1-07: проекция
// объявленных сегментов равна тому, что правила ОБЪЯВЛЯЮТ.
//
// Почему это красное ДО миграции: обратное заполнение 1030001 клало в проекцию
// только резолвящееся, а `domain.RuleRefsOf` даёт ВСЕ объявленные сегменты.
// Разница — ровно те двадцать. После приведения правил к словарю обе стороны
// говорят об одном.
func TestIAMSV107_RuleRefProjectionEqualsWhatRulesDeclare(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: требует Postgres")
	}
	ctx := context.Background()

	pool, err := pgxpool.New(ctx, setupTestDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)

	declared := map[string]map[string]bool{}
	roleName := map[string]string{}
	rows, err := pool.Query(ctx, `SELECT id, name, rules FROM kacho_iam.roles ORDER BY id`)
	require.NoError(t, err)
	var roles, refs int
	for rows.Next() {
		var id, name string
		var raw []byte
		require.NoError(t, rows.Scan(&id, &name, &raw))
		roles++
		roleName[id] = name
		if len(raw) == 0 {
			continue
		}
		rules, derr := domain.DecodeRules(raw)
		require.NoErrorf(t, derr, "роль %s (%s): rules не декодируются", id, name)
		set := map[string]bool{}
		for _, ref := range domain.RuleRefsOf(rules) {
			set[ref.Module+"."+ref.Resource+"."+ref.Verb] = true
			refs++
		}
		if len(set) > 0 {
			declared[id] = set
		}
	}
	require.NoError(t, rows.Err())

	projected := map[string]map[string]bool{}
	prows, err := pool.Query(ctx, `
		SELECT role_id, module, resource, COALESCE(verb, '') FROM kacho_iam.role_rule_ref`)
	require.NoError(t, err)
	var projectedRows int
	for prows.Next() {
		var roleID, module, resource, verb string
		require.NoError(t, prows.Scan(&roleID, &module, &resource, &verb))
		projectedRows++
		if projected[roleID] == nil {
			projected[roleID] = map[string]bool{}
		}
		projected[roleID][module+"."+resource+"."+verb] = true
	}
	require.NoError(t, prows.Err())

	t.Logf("осмотрено: ролей=%d, объявленных сегментов=%d, строк проекции=%d", roles, refs, projectedRows)
	require.NotZerof(t, roles, "предпосылка нарушена: ни одной роли")
	require.NotZerof(t, refs, "предпосылка нарушена: ни одного объявленного сегмента — "+
		"равенство множеств было бы получено даром")
	require.NotZerof(t, projectedRows, "предпосылка нарушена: проекция пуста")

	var diff []string
	for id, want := range declared {
		got := projected[id]
		for seg := range want {
			if !got[seg] {
				diff = append(diff, fmt.Sprintf("объявлено, но не спроецировано: роль %q, сегмент %s", roleName[id], seg))
			}
		}
	}
	for id, got := range projected {
		want := declared[id]
		for seg := range got {
			if !want[seg] {
				diff = append(diff, fmt.Sprintf("спроецировано, но не объявлено: роль %q, сегмент %s", roleName[id], seg))
			}
		}
	}
	sort.Strings(diff)
	require.Emptyf(t, diff, "проекция объявленных сегментов разошлась с правилами (%d расхождений):\n%s",
		len(diff), strings.Join(diff, "\n"))
}

// TestIAMSV104_VerdictProjectionIsUnchanged — IAM-SV-1-04: ХАРАКТЕРИЗУЮЩИЙ
// ЗАМОК, а не RED. Дерево уже даёт это поведение, и проба обязана ПЕРЕЖИТЬ
// изменение: требовать от неё красноты запрещено.
//
// Утверждается то, ради чего вся правка защитима: набор глаголов, который
// РЕАЛЬНО получает арендатор, до и после приведения правил один и тот же.
// Приведение схлопывается в снятие второго имени того, что уже названо.
//
// Почему не через таблицу `role_verb`: в фикстуре, применяющей только миграции,
// она пуста — её наполняет досев на старте. Утверждение через неё было бы
// `0 = 0`, то есть вакуумным. Граница названа, а не обойдена.
func TestIAMSV104_VerdictProjectionIsUnchanged(t *testing.T) {
	cases := []struct {
		fgaType string
		before  []string
		after   []string
	}{
		{
			fgaType: "vpc_network",
			before:  []string{"read", "list", "get"},
			after:   []string{"get", "list"},
		},
		{
			fgaType: "compute_instance",
			before:  []string{"read", "list", "get"},
			after:   []string{"get", "list"},
		},
		{
			fgaType: "nlb_target_group",
			before:  []string{"addTargets", "removeTargets", "get", "list", "listOperations"},
			after:   []string{"addTargets", "removeTargets", "get", "list"},
		},
		{
			fgaType: "nlb_network_load_balancer",
			before:  []string{"getTargetStates", "listOperations", "get", "list"},
			after:   []string{"get", "list"},
		},
		{
			fgaType: "nlb_listener",
			before:  []string{"get", "list", "listOperations"},
			after:   []string{"get", "list"},
		},
	}
	for _, c := range cases {
		t.Run(c.fgaType, func(t *testing.T) {
			typeVerbs := authzmap.VerbsOfType(c.fgaType)
			require.NotEmptyf(t, typeVerbs, "контроль: тип %q обязан объявлять глаголы — "+
				"на пустом наборе обе стороны дали бы nil и равенство прошло бы даром", c.fgaType)

			was := authzmap.GrantedVerbs(c.fgaType, c.before, typeVerbs)
			now := authzmap.GrantedVerbs(c.fgaType, c.after, typeVerbs)
			require.NotEmptyf(t, was, "контроль: набор «до» пуст — сравнение вакуумно")
			sort.Strings(was)
			sort.Strings(now)
			require.Equal(t, was, now,
				"проекция вердикта изменилась: арендатор теряет действие. "+
					"Приведение правил обязано быть снятием ВТОРОГО ИМЕНИ того, что уже названо, "+
					"а не отнятием права (kacho#1815, §2.6 приёмки)")
			t.Logf("%s: до=%v после=%v → выдача %v", c.fgaType, c.before, c.after, now)
		})
	}
}

// applyMigrationUpBodyInTx — тело `Up` поставляемой миграции, исполненное В
// ОДНОЙ ТРАНЗАКЦИИ, как его исполняет goose; возвращает ПЕРЕПИСЬ, которую
// миграция печатает через RAISE NOTICE.
//
// Отличается от applyMigrationUpBody (migration_helpers_test.go) двумя вещами, и
// обе несущие. ТРАНЗАКЦИЯ: миграция-предмет держит предикат снятия во временной
// таблице `ON COMMIT DROP`, и вне транзакции та исчезла бы после первого же
// оператора. ПЕРЕПИСЬ: «ноль снятых» обязано быть отличимо от «ноль
// прочитанных», а сказать это может только сама миграция — поэтому её NOTICE
// перехватываются и утверждаются, а не остаются прозой в комментарии.
//
// Текст миграции читается из migrations.FS: копии её SQL здесь нет и быть не
// должно — она разошлась бы с оригиналом молча.
func applyMigrationUpBodyInTx(t *testing.T, ctx context.Context, dsn, prefix string) ([]string, error) {
	t.Helper()
	entries, err := migrations.FS.ReadDir(".")
	require.NoError(t, err)
	var file string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), prefix+"_") {
			file = e.Name()
			break
		}
	}
	require.NotEmptyf(t, file, "миграции с префиксом %q в поставке нет — "+
		"проба утверждала бы о тексте, которого не существует", prefix)
	raw, err := migrations.FS.ReadFile(file)
	require.NoError(t, err)
	body := string(raw)
	if i := strings.Index(body, "-- +goose Down"); i >= 0 {
		body = body[:i]
	}
	body = strings.Replace(body, "-- +goose Up", "", 1)

	cfg, err := pgx.ParseConfig(dsn)
	require.NoError(t, err)
	// Простой протокол: операторов с параметрами здесь нет, а тела DO несут `$$`.
	cfg.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	var notices []string
	cfg.OnNotice = func(_ *pgconn.PgConn, n *pgconn.Notice) {
		notices = append(notices, n.Message)
	}
	conn, err := pgx.ConnectConfig(ctx, cfg)
	require.NoError(t, err)
	defer func() { _ = conn.Close(ctx) }()

	tx, err := conn.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()

	for _, stmt := range splitGooseStatements(body) {
		if strings.TrimSpace(stmt) == "" {
			continue
		}
		if _, execErr := tx.Exec(ctx, stmt); execErr != nil {
			return notices, execErr
		}
	}
	if commitErr := tx.Commit(ctx); commitErr != nil {
		return notices, commitErr
	}
	return notices, nil
}

// plantSystemRule — системная роль с заданным набором правил, заведённая ПРЯМОЙ
// вставкой, как её заводит миграция: путь `ReplaceRuleRefs` (а с ним и ключ
// `role_rule_ref_verb_fk`) системную половину не судит, и фикстура обязана
// повторять именно это, а не быть снисходительнее продукта.
//
// Область копируется с уже посеянной системной роли: `roles_definition_tier_xor`
// требует ровно один якорь, а `is_system` с 0056 — GENERATED-деривация
// `cluster_id IS NOT NULL` (вставка значения в неё даёт 428C9). Выдумывать
// идентификатор кластера литералом было бы вторым местом, знающим посев.
func plantSystemRule(t *testing.T, ctx context.Context, pool *pgxpool.Pool, name, rules string) {
	t.Helper()
	_, err := pool.Exec(ctx, `
		INSERT INTO kacho_iam.roles (id, name, description, permissions, cluster_id, rules)
		SELECT 'rol' || substr(md5($1), 1, 17), $1, '', '[]'::jsonb, s.cluster_id, $2::jsonb
		  FROM kacho_iam.roles s
		 WHERE s.is_system AND s.cluster_id IS NOT NULL
		 LIMIT 1`, name, rules)
	require.NoErrorf(t, err, "фикстура не завелась: роль %q", name)

	var planted string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT rules::text FROM kacho_iam.roles WHERE name = $1`, name).Scan(&planted),
		"фикстура обязана быть НАБЛЮДАЕМА: без этого «миграция отказала» неотличимо от "+
			"«фикстуры не было»")
	t.Logf("посажено: роль %q, правила %s", name, planted)
}

// TestIAMSV114_MigrationIsIdempotent — IAM-SV-1-14: повторное применение тела
// `Up` на уже приведённой базе ничего не меняет.
//
// Тело читается ИЗ ПОСТАВКИ (migrations.FS): копия SQL в пробе была бы вторым
// местом об одном предмете и разошлась бы с оригиналом молча.
func TestIAMSV114_MigrationIsIdempotent(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: требует Postgres")
	}
	ctx := context.Background()
	dsn := setupTestDB(t)

	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)

	before := rulesSnapshot(t, ctx, pool)
	require.NotEmpty(t, before, "предпосылка нарушена: посев пуст — «ничего не изменилось» даром")

	notices, err := applyMigrationUpBodyInTx(t, ctx, dsn, segmentMigrationPrefix)
	require.NoError(t, err, "повторное применение обязано проходить: миграция идемпотентна")

	// Перепись печатается ВСЕГДА, а не при находке: «ноль снятых» обязано быть
	// отличимо от «ноль прочитанных». На повторном применении она и есть нули —
	// и это её штатный, а не вырожденный исход.
	census := strings.Join(notices, "\n")
	t.Logf("перепись миграции на повторном применении:\n%s", census)
	require.Contains(t, census, "глаголов снято 0",
		"перепись обязана назвать ноль снятых — иначе повторное применение неотличимо "+
			"от прогона, который ничего не читал")
	require.Contains(t, census, "строк следа снято 0")
	require.Regexp(t, `правил системных ролей осмотрено [1-9]`, census,
		"перепись обязана назвать НЕНУЛЕВОЙ объём осмотренного рядом с нулём снятых")

	require.Equal(t, before, rulesSnapshot(t, ctx, pool),
		"повторное применение изменило правила — предикат снятия не идемпотентен")

	var trace int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM kacho_iam.role_grant_orphan WHERE source = 'rule_ref'`).Scan(&trace))
	require.Zero(t, trace)
}

// TestIAMSV112_MigrationRefusesRatherThanGuess — IAM-SV-1-12 и IAM-SV-1-13:
// миграция ОТКАЗЫВАЕТ там, где снятие означало бы решение, которого никто не
// принимал, и отказ НАЗЫВАЕТ роль.
//
// # Отступление от §7.3 приёмки — названо, а не внесено молча
//
// Приёмка объявила держателем этих двух сценариев прогон на подготовленной
// копии ДО посадки, а автоматическую регрессию отвергла доводом «воспроизвести
// отказ пробой можно, только исполнив её текст второй раз из теста, а это
// второе место об одном предмете». Довод опровергается деревом: копии текста
// здесь НЕТ — тело `Up` читается из migrations.FS тем же способом, каким его
// читает уже существующий applyMigrationUpBody
// (services/iam/internal/repo/kacho/pg/migration_helpers_test.go). Предикат:
// `grep -n 'func applyMigrationUpBody' services/iam/internal/repo/kacho/pg/migration_helpers_test.go`.
// Своего у пробы ровно одно — ТРАНЗАКЦИЯ, которой у того помощника нет, а
// миграции она нужна (временная таблица предиката живёт `ON COMMIT DROP`).
//
// Прогон на подготовленной копии §8.5 при этом остаётся сделанным — он и есть
// эта проба; отличие в том, что он повторяем и переживёт сессию.
func TestIAMSV112_MigrationRefusesRatherThanGuess(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: требует Postgres")
	}
	ctx := context.Background()

	run := func(t *testing.T, name, rules string) error {
		t.Helper()
		dsn := setupTestDB(t)
		pool, err := pgxpool.New(ctx, dsn)
		require.NoError(t, err)
		pgtest.ClosePoolAtEnd(t, pool)
		plantSystemRule(t, ctx, pool, name, rules)

		_, err = applyMigrationUpBodyInTx(t, ctx, dsn, segmentMigrationPrefix)
		return err
	}

	t.Run("IAM-SV-1-12 снятие опустошило бы правило — отказ", func(t *testing.T) {
		err := run(t, "inj.empty.probe",
			`[{"module":"vpc","resources":["network"],"verbs":["read"]}]`)
		require.Error(t, err, "правило, у которого снимаются ВСЕ глаголы, — другой предмет: "+
			"миграция обязана отказать, а не решить его молча")
		require.Contains(t, err.Error(), "inj.empty.probe", "отказ обязан НАЗЫВАТЬ роль")
		require.Contains(t, err.Error(), "IAM-SV-1-12")
		t.Logf("отказ: %v", err)
	})

	t.Run("IAM-SV-1-12 контроль: тот же вход плюс канонический глагол — проходит", func(t *testing.T) {
		dsn := setupTestDB(t)
		pool, err := pgxpool.New(ctx, dsn)
		require.NoError(t, err)
		pgtest.ClosePoolAtEnd(t, pool)
		plantSystemRule(t, ctx, pool, "inj.kept.probe",
			`[{"module":"vpc","resources":["network"],"verbs":["read","get"]}]`)

		_, err = applyMigrationUpBodyInTx(t, ctx, dsn, segmentMigrationPrefix)
		require.NoError(t, err, "законный близнец обязан ПРОХОДИТЬ — иначе отрицание выше вакуумно")

		var got string
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT rules::text FROM kacho_iam.roles WHERE name = 'inj.kept.probe'`).Scan(&got))
		require.Contains(t, got, `"get"`)
		require.NotContains(t, got, `"read"`, "снимается ВТОРОЕ имя того, что уже названо")
	})

	t.Run("IAM-SV-1-13 полуподстановка — отказ", func(t *testing.T) {
		err := run(t, "inj.half.probe",
			`[{"module":"vpc","resources":["*"],"verbs":["read","get"]}]`)
		require.Error(t, err, "у `конкретное.*` нет ни полосы каталога, ни полосы объединения")
		require.Contains(t, err.Error(), "inj.half.probe", "отказ обязан НАЗЫВАТЬ роль")
		require.Contains(t, err.Error(), "IAM-SV-1-13")
		t.Logf("отказ: %v", err)
	})

	t.Run("IAM-SV-1-13 контроль: ПОЛНАЯ подстановка с тем же набором — проходит", func(t *testing.T) {
		dsn := setupTestDB(t)
		pool, err := pgxpool.New(ctx, dsn)
		require.NoError(t, err)
		pgtest.ClosePoolAtEnd(t, pool)
		plantSystemRule(t, ctx, pool, "inj.full.probe",
			`[{"module":"*","resources":["*"],"verbs":["read","get"]}]`)

		_, err = applyMigrationUpBodyInTx(t, ctx, dsn, segmentMigrationPrefix)
		require.NoError(t, err, "полная подстановка судится полосой объединения и обязана проходить")

		var got string
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT rules::text FROM kacho_iam.roles WHERE name = 'inj.full.probe'`).Scan(&got))
		require.Contains(t, got, `"get"`)
		require.NotContains(t, got, `"read"`)
	})

	t.Run("контроль полосы: глагол ЧУЖОГО типа снимается, СВОЕГО — остаётся", func(t *testing.T) {
		dsn := setupTestDB(t)
		pool, err := pgxpool.New(ctx, dsn)
		require.NoError(t, err)
		pgtest.ClosePoolAtEnd(t, pool)
		// Инъекция и её законный близнец В ОДНОЙ роли: если бы конкретная полоса
		// спрашивала ОБЪЕДИНЕНИЕ, `addTargets` на vpc.network уцелел бы; если бы
		// сравнение шло без приведения регистра, `addTargets` на своём типе был бы
		// снят как незнакомый. Оба исхода различимы здесь одним прогоном.
		plantSystemRule(t, ctx, pool, "inj.lane.probe", `[
			{"module":"vpc","resources":["network"],"verbs":["addTargets","get"]},
			{"module":"loadbalancer","resources":["targetGroups"],"verbs":["addTargets","removeTargets","get"]}]`)

		notices, err := applyMigrationUpBodyInTx(t, ctx, dsn, segmentMigrationPrefix)
		require.NoError(t, err)
		t.Logf("перепись: %s", strings.Join(notices, " | "))

		var got string
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT rules::text FROM kacho_iam.roles WHERE name = 'inj.lane.probe'`).Scan(&got))
		t.Logf("после правки: %s", got)
		require.Contains(t, got, `"verbs": ["get"]`,
			"vpc.network: addTargets обязан быть снят — его объявляет СОСЕДНИЙ тип")
		require.Contains(t, got, `"addTargets", "removeTargets", "get"`,
			"loadbalancer.targetGroups: верблюжьи addTargets/removeTargets обязаны УЦЕЛЕТЬ — "+
				"каталог хранит их строчными, и сравнение без приведения сняло бы живое право")
	})
}

// rulesSnapshot — правила всех ролей как сравнимое значение.
func rulesSnapshot(t *testing.T, ctx context.Context, pool *pgxpool.Pool) map[string]string {
	t.Helper()
	rows, err := pool.Query(ctx, `SELECT id, rules::text FROM kacho_iam.roles`)
	require.NoError(t, err)
	out := map[string]string{}
	for rows.Next() {
		var id, rules string
		require.NoError(t, rows.Scan(&id, &rules))
		out[id] = rules
	}
	require.NoError(t, rows.Err())
	return out
}
