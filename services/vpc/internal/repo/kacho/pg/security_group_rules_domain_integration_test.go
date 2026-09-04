// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo"
	kachopg "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/pg"

	"github.com/PRO-Robotech/kacho/pkg/pgtest"
)

// Область значений правила SG на уровне БД.
//
// Синхронная проверка в use-case (`validateSGRulePorts`/`validateSGRuleProtocol`)
// ограничивает ОДИН запрос вызывающего. Инвариант «правило выразимо продуктом»
// обязан жить конструкцией БД (ban #10): она отвечает КАЖДОМУ писателю, включая
// тех, кто пришёл мимо use-case — inline-путь default-SG, будущий воркер,
// восстановление из дампа, ручной SQL.
//
// Тесты пишут JSONB напрямую (минуя use-case) и требуют 23514.

// sgRuleJSON собирает один элемент массива `security_groups.rules` в том виде,
// в каком его пишет repo (`json.Marshal([]domain.SecurityGroupRule)`): ключи —
// имена полей Go-структуры, тегов у неё нет.
func sgRuleJSON(t *testing.T, r domain.SecurityGroupRule) string {
	t.Helper()
	b, err := json.Marshal([]domain.SecurityGroupRule{r})
	require.NoError(t, err)
	return string(b)
}

// insertNetworkSQL — родитель для FK security_groups.network_id, напрямую SQL.
func insertNetworkSQL(t *testing.T, ctx context.Context, pool *pgxpool.Pool, name string) string {
	t.Helper()
	id := ids.NewID(ids.PrefixNetwork)
	_, err := pool.Exec(ctx, `
		INSERT INTO networks (id, project_id, name) VALUES ($1, $2, $3)`,
		id, "proj-sgdom", name)
	require.NoError(t, err)
	return id
}

// insertSGWithRules — прямой INSERT строки SG с заданным JSONB правил.
func insertSGWithRules(ctx context.Context, pool *pgxpool.Pool, netID, rules string) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO security_groups (id, project_id, network_id, name, rules)
		VALUES ($1, 'proj-sgdom', $2, $1, $3::jsonb)`,
		ids.NewID(ids.PrefixSecurityGroup), netID, rules)
	return err
}

// sqlStateOf — SQLSTATE ошибки pgx, либо её текст, если это не PgError.
func sqlStateOf(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return err.Error()
}

func legalRule() domain.SecurityGroupRule {
	return domain.SecurityGroupRule{
		ID:           "r-legal",
		Direction:    domain.SecurityGroupRuleDirectionIngress,
		FromPort:     22,
		ToPort:       22,
		ProtocolName: "tcp",
		V4CidrBlocks: []string{"10.0.0.0/8"},
	}
}

// VPC-SGDOM-1 — ограничение существует и названо.
func TestSGRulesDomain_ConstraintPresent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool, err := coredb.NewPool(ctx, setupTestDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)

	var n int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT count(*) FROM pg_constraint
		WHERE conrelid = 'kacho_vpc.security_groups'::regclass
		  AND contype = 'c'
		  AND conname = 'security_groups_rules_domain'`).Scan(&n))
	assert.Equal(t, 1, n, "CHECK security_groups_rules_domain must exist after migrations")
}

// VPC-SGDOM-2 — законные написания проходят (парная половина гейта: он обязан
// МОЛЧАТЬ на том, что продукт выразить умеет, иначе ловит форму, а не существо).
func TestSGRulesDomain_LegalShapesPass(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool, err := coredb.NewPool(ctx, setupTestDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)
	netID := insertNetworkSQL(t, ctx, pool, "net-sgdom-legal")

	cases := []struct {
		name string
		rule domain.SecurityGroupRule
	}{
		{"tcp/22", legalRule()},
		{"any-port -1/-1", domain.SecurityGroupRule{
			Direction: domain.SecurityGroupRuleDirectionEgress,
			FromPort:  -1, ToPort: -1, ProtocolName: "ANY", ProtocolNumber: -1,
		}},
		{"unset ports stored as 0/0", domain.SecurityGroupRule{
			Direction: domain.SecurityGroupRuleDirectionIngress, ProtocolName: "udp",
		}},
		{"boundary 0-65535", domain.SecurityGroupRule{
			Direction: domain.SecurityGroupRuleDirectionIngress,
			FromPort:  0, ToPort: 65535, ProtocolNumber: 255,
		}},
		{"alias name", domain.SecurityGroupRule{
			Direction: domain.SecurityGroupRuleDirectionIngress, ProtocolName: "ospf",
		}},
		{"upper-case name", domain.SecurityGroupRule{
			Direction: domain.SecurityGroupRuleDirectionIngress, ProtocolName: "TCP",
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.NoError(t, insertSGWithRules(ctx, pool, netID, sgRuleJSON(t, c.rule)))
		})
	}

	// Пустой набор и JSON-скаляр null — тоже законны (их пишет repo).
	assert.NoError(t, insertSGWithRules(ctx, pool, netID, `[]`))
	assert.NoError(t, insertSGWithRules(ctx, pool, netID, `null`))

	// Набор правил default-SG, который продукт пишет мимо use-case.
	b, err := json.Marshal(domain.NewDefaultSecurityGroupRules())
	require.NoError(t, err)
	assert.NoError(t, insertSGWithRules(ctx, pool, netID, string(b)))
}

// VPC-SGDOM-3 — невыразимые значения отбиваются БАЗОЙ, а не use-case'ом.
func TestSGRulesDomain_UnexpressibleRejected(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool, err := coredb.NewPool(ctx, setupTestDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)
	netID := insertNetworkSQL(t, ctx, pool, "net-sgdom-bad")

	cases := []struct {
		name string
		rule domain.SecurityGroupRule
	}{
		{"from_port over 65535", domain.SecurityGroupRule{
			Direction: domain.SecurityGroupRuleDirectionIngress, FromPort: 65536, ToPort: 65536}},
		{"to_port over 65535", domain.SecurityGroupRule{
			Direction: domain.SecurityGroupRuleDirectionIngress, FromPort: 80, ToPort: 70000}},
		{"from_port below range", domain.SecurityGroupRule{
			Direction: domain.SecurityGroupRuleDirectionIngress, FromPort: -2, ToPort: 80}},
		{"half any-port", domain.SecurityGroupRule{
			Direction: domain.SecurityGroupRuleDirectionIngress, FromPort: -1, ToPort: 80}},
		{"inverted range", domain.SecurityGroupRule{
			Direction: domain.SecurityGroupRuleDirectionIngress, FromPort: 443, ToPort: 80}},
		{"unknown protocol name", domain.SecurityGroupRule{
			Direction: domain.SecurityGroupRuleDirectionIngress, ProtocolName: "klingon"}},
		{"protocol number over 255", domain.SecurityGroupRule{
			Direction: domain.SecurityGroupRuleDirectionIngress, ProtocolNumber: 999}},
		{"protocol number below range", domain.SecurityGroupRule{
			Direction: domain.SecurityGroupRuleDirectionIngress, ProtocolNumber: -2}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := insertSGWithRules(ctx, pool, netID, sgRuleJSON(t, c.rule))
			pgErr := mustPgError(t, err)
			assert.Equal(t, "23514", pgErr.Code)
			assert.Equal(t, "security_groups_rules_domain", pgErr.ConstraintName)
		})
	}

	// JSON-число, которое не помещается в целое: писателю мимо use-case никто
	// не мешает его записать. Ограничение обязано ответить тем же 23514, а не
	// возбудить исключение разбора — иначе вызывающий получает ошибку СОВСЕМ
	// другого класса, которую маппер репозитория относит к разбору
	// идентификатора, а обратное заполнение на такой строке просто падает.
	for _, raw := range []struct{ name, rules string }{
		{"fractional port", `[{"ID":"x","Direction":"INGRESS","FromPort":1.5,"ToPort":80}]`},
		{"integral-looking float port", `[{"ID":"x","Direction":"INGRESS","FromPort":1.0,"ToPort":80}]`},
		{"port beyond bigint", `[{"ID":"x","Direction":"INGRESS","FromPort":1e999,"ToPort":80}]`},
		{"fractional protocol number", `[{"ID":"x","Direction":"INGRESS","ProtocolNumber":6.5}]`},
	} {
		t.Run(raw.name, func(t *testing.T) {
			err := insertSGWithRules(ctx, pool, netID, raw.rules)
			pgErr := mustPgError(t, err)
			assert.Equal(t, "23514", pgErr.Code, "ожидался отказ ограничения, получен SQLSTATE %s", pgErr.Code)
			assert.Equal(t, "security_groups_rules_domain", pgErr.ConstraintName)
		})
	}
}

// VPC-SGDOM-4 — repo-путь (writer.Insert, минуя use-case) маппит 23514 в
// ErrInvalidArg и НЕ течёт SQL-текстом наружу.
func TestSGRulesDomain_RepoInsertMapsAndDoesNotLeak(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool, err := coredb.NewPool(ctx, setupTestDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)

	r := kachopg.New(pool, nil)
	w, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w.Abort()
	net := insertNetworkInTx(t, ctx, w, "proj-sgdom-repo", "net-sgdom-repo")
	sg := newDefaultSG(net.ProjectID, net.ID)
	sg.Rules = []domain.SecurityGroupRule{{
		ID:        "r-bad",
		Direction: domain.SecurityGroupRuleDirectionIngress,
		FromPort:  65536, ToPort: 65536,
	}}
	_, err = w.SecurityGroups().Insert(ctx, sg)
	require.Error(t, err)
	assert.True(t, errors.Is(err, repo.ErrInvalidArg), "23514 → repo.ErrInvalidArg, got %v", err)
	assertNoSQLLeak(t, err)
	// Общий помощник перечисляет токены СОСЕДНЕГО ресурса (`address_pools`,
	// `_chk`), поэтому на этой ошибке он молчал бы и при утечке: ни имя таблицы
	// групп, ни имя нового ограничения, ни имена его функций в его список не
	// входят, а новое ограничение к тому же не оканчивается на `_chk`. Проверка
	// без предмета — не проверка, поэтому предмет назван здесь явно.
	for _, leak := range []string{
		"security_groups", "security_groups_rules_domain",
		"kacho_sg_rule_expressible", "kacho_sg_rules_domain_valid", "kacho_sg_protocol_name_valid",
		"jsonb", "SQLSTATE", "23514",
	} {
		assert.NotContains(t, err.Error(), leak,
			"текст ошибки не должен нести внутренности базы (%q)", leak)
	}
}

// VPC-SGDOM-5 — параллельные писатели по одной строке: ровно один коммитится,
// остальные получают ожидаемый признак.
//
// Половина претендентов несёт невыразимое значение — их отбивает ограничение
// (23514) независимо от того, кто выиграл гонку за строку. Из законных
// побеждает ровно один (row-lock + повторный CAS-предикат по xmin).
func TestSGRulesDomain_ConcurrentWritersExactlyOneWins(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool, err := coredb.NewPool(ctx, setupTestDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)
	netID := insertNetworkSQL(t, ctx, pool, "net-sgdom-conc")

	sgID := ids.NewID(ids.PrefixSecurityGroup)
	_, err = pool.Exec(ctx, `
		INSERT INTO security_groups (id, project_id, network_id, name, rules)
		VALUES ($1, 'proj-sgdom', $2, $1, '[]'::jsonb)`, sgID, netID)
	require.NoError(t, err)

	// Претенденты бьются в одну строку БЕЗ CAS-предиката: их UPDATE
	// сериализует row-lock, и ограничение вычисляется у КАЖДОГО. Вариант с
	// CAS по xmin (как в repo.UpdateRules) для этого свойства не годится:
	// проигравший CAS не задевает ни одной строки, ограничение на нём не
	// исполняется вовсе, и невыразимое значение остаётся неопровергнутым —
	// зелёное, которое не про то. Гонку самого CAS покрывает
	// security_group_occ_integration_test.go.
	const (
		legal   = 1
		illegal = 11
	)
	type outcome struct {
		committed bool
		sqlstate  string
	}
	results := make([]outcome, legal+illegal)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < legal+illegal; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rule := legalRule()
			rule.ID = fmt.Sprintf("r-%d", i)
			rule.FromPort, rule.ToPort = int64(1000+i), int64(1000+i)
			if i >= legal {
				rule.FromPort, rule.ToPort = 65536, 65536 // невыразимо
			}
			b, merr := json.Marshal([]domain.SecurityGroupRule{rule})
			if merr != nil {
				results[i] = outcome{sqlstate: "marshal:" + merr.Error()}
				return
			}
			<-start
			tag, uerr := pool.Exec(ctx, `
				UPDATE security_groups SET rules = $2::jsonb WHERE id = $1`, sgID, string(b))
			if uerr != nil {
				results[i] = outcome{sqlstate: sqlStateOf(uerr)}
				return
			}
			results[i] = outcome{committed: tag.RowsAffected() == 1}
		}(i)
	}
	close(start)
	wg.Wait()

	wins, checkViolations := 0, 0
	for i, o := range results {
		switch {
		case o.committed:
			wins++
			assert.Less(t, i, legal, "коммитится обязан законный писатель, а не %d", i)
		case o.sqlstate == "23514":
			checkViolations++
			assert.GreaterOrEqual(t, i, legal, "23514 обязан прийти невыразимому, а не законному %d", i)
		default:
			t.Errorf("писатель %d не получил ни коммита, ни 23514: %+v", i, o)
		}
	}
	assert.Equal(t, legal, wins, "ровно один писатель коммитится, получено %d (%v)", wins, results)
	assert.Equal(t, illegal, checkViolations,
		"каждый невыразимый писатель обязан получить 23514, получено %d (%v)", checkViolations, results)

	// И итог строки — от законного писателя, а не от кого-то из отбитых.
	var stored string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT rules::text FROM security_groups WHERE id = $1`, sgID).Scan(&stored))
	assert.Contains(t, stored, `"FromPort": 1000`, "в строке обязано остаться выразимое правило")
}

// VPC-SGDOM-6 — паритет наборов: имя, которое принимает код, обязана принимать
// БАЗА, и наоборот. Иначе два источника истины разъедутся молча, а обратное
// заполнение и ограничение начнут спорить друг с другом.
func TestSGRulesDomain_ProtocolNameSetParityWithCode(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool, err := coredb.NewPool(ctx, setupTestDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)

	codeNames := domain.KnownProtocolNames()
	require.NotEmpty(t, codeNames, "перепись: набор имён кода не должен быть пуст")

	// Набор базы ПЕРЕЧИСЛЯЕТСЯ, а не опрашивается по одному: опрос доказывает
	// только «код ⊆ база». Имя, добавленное в базу и отсутствующее в коде,
	// сделало бы ограничение ШИРЕ продукта — ровно то, ради предотвращения чего
	// оно и заведено, — и осталось бы незамеченным.
	rows, err := pool.Query(ctx, `SELECT unnest(kacho_vpc.kacho_sg_protocol_names())`)
	require.NoError(t, err)
	defer rows.Close()
	dbSet := map[string]struct{}{}
	for rows.Next() {
		var n string
		require.NoError(t, rows.Scan(&n))
		dbSet[n] = struct{}{}
	}
	require.NoError(t, rows.Err())
	require.NotEmpty(t, dbSet, "перепись: набор имён базы не должен быть пуст")
	t.Logf("сверено имён: код %d, база %d", len(codeNames), len(dbSet))

	codeSet := map[string]struct{}{}
	for _, n := range codeNames {
		codeSet[n] = struct{}{}
	}
	var onlyInCode, onlyInDB []string
	for n := range codeSet {
		if _, ok := dbSet[n]; !ok {
			onlyInCode = append(onlyInCode, n)
		}
	}
	for n := range dbSet {
		if _, ok := codeSet[n]; !ok {
			onlyInDB = append(onlyInDB, n)
		}
	}
	sort.Strings(onlyInCode)
	sort.Strings(onlyInDB)
	assert.Empty(t, onlyInCode, "код принимает имена, которых нет в базе")
	assert.Empty(t, onlyInDB, "база принимает имена, которых нет в коде — ограничение шире продукта")
	assert.Equal(t, len(codeSet), len(dbSet), "размеры наборов кода и базы обязаны совпадать")

	// Предикат базы обязан отвечать по этому набору и делать это
	// регистронезависимо — как и предикат кода.
	for _, n := range codeNames {
		var ok bool
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT kacho_vpc.kacho_sg_protocol_name_valid($1)`, n).Scan(&ok))
		assert.True(t, ok, "имя набора %q обязано приниматься предикатом базы", n)
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT kacho_vpc.kacho_sg_protocol_name_valid($1)`, strings.ToUpper(n)).Scan(&ok))
		assert.True(t, ok, "код регистронезависим на %q, база — нет", n)
	}

	// Отрицательная половина: то, что код НЕ принимает, база тоже не принимает.
	for _, n := range []string{"klingon", "tcp6", "", " tcp", "tcp ", "any-host-internal"} {
		var ok bool
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT kacho_vpc.kacho_sg_protocol_name_valid($1)`, n).Scan(&ok))
		assert.Equal(t, domain.IsKnownProtocolName(n), ok,
			"расхождение кода и базы на %q", n)
	}
}
