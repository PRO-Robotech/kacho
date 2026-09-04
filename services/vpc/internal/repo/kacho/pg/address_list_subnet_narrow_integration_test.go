// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
	kachopg "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/pg"

	"github.com/PRO-Robotech/kacho/pkg/pgtest"
)

// Сужение списка адресов по подсети — что оно ОТБИРАЕТ и во что оно обходится.
//
// Предмет. Публичный список адресов принимает `subnet_id`, и вкладка адресов под
// подсетью — единственный её потребитель, у которого альтернативы нет: подсеть
// адреса лежит внутри jsonb и в ДВУХ семьях, поэтому выражением `filter` по имени
// колонки она не выражается вовсе. Пока сужение шло дизъюнкцией jsonb-выражений,
// верным было только первое из двух утверждений: «сужение есть» — да, «оно
// дешёвое» — нет. Ни один индекс такую дизъюнкцию не покрывает, то есть страница
// вкладки стоила чтения таблицы адресов ВСЕХ проектов, а при объявленной
// плотности (тысячи адресов в проекте) это цена на каждый тик опроса.
//
// Рядом, с первой миграции, лежит хранимая колонка `internal_subnet_id` (v4-подсеть,
// иначе v6-подсеть, иначе NULL) с внешним ключом на подсеть, а её однозначность
// закреплена проверкой `addresses_single_internal_family` (0025): строка с ДВУМЯ
// внутренними семьями невозможна, значит колонка и дизъюнкция отбирают одно и то
// же множество. Дочерний список подсети на неё уже переведён (0025) — публичный
// список остался на дизъюнкции, то есть один и тот же вопрос стоил в двух местах
// по-разному.
//
// Что утверждается ниже, тремя разными утверждениями:
//
//  1. ОТБОР — адрес находится по своей подсети в ОБЕИХ семьях (v4 и v6), чужой не
//     находится (отрицание), а без сужения видны все (второй положительный
//     контроль: без него «нашлось ровно своё» было бы верно и на пустой выдаче).
//  2. ЦЕНА, часть первая — план обязан идти индексом, а не читать таблицу.
//  3. ЦЕНА, часть вторая — страница подсети не оплачивается сортировкой ВСЕХ её
//     адресов: у плана не должно быть узла сортировки. Это отдельное утверждение,
//     а не придирка: индекс по одной колонке подсети отдаёт строки без порядка,
//     поэтому на подсети с тысячами адресов курсорная страница из 50 строк платит
//     сортировкой тысяч.
//
// Объём осмотренного печатается: «ноль находок» на пустой таблице неотличимо от
// работающего сужения, поэтому засеянное число строк — часть утверждения.

const (
	narrowProject = "prj_narrow_addr"
	// Плотность лежит В ЦЕЛЕВОЙ подсети, а не рядом с ней, и это не деталь
	// фикстуры. Утверждение «страница стоит страницу» имеет предмет только там,
	// где адресов подсети БОЛЬШЕ страницы: на двух строках сортировка бесплатна,
	// поэтому фикстура с плотностью у соседа мерила бы поведение в единственном
	// режиме, где дефект ничего не стоит.
	//
	// Соседние подсети нужны по второй, независимой причине: они делают предикат
	// подсети ИЗБИРАТЕЛЬНЫМ. Когда целевая подсеть — почти вся таблица, страницу
	// дёшево набрать и обходом по времени создания, поэтому гейт на такой
	// фикстуре не различал бы наличие индекса.
	narrowDenseRows    = 400
	narrowOtherSubnets = 4
	narrowForeignRows  = 3
	narrowPageSize     = 50
)

// seedNarrowFixture — сеть, подсети и адреса: внутренние v4 и v6 в целевой
// подсети плюс её плотность, столько же в каждой из соседних, немного в подсети
// для отрицания и внешний адрес без подсети вовсе.
//
// Всё лежит в ОДНОЙ таблице и одном проекте — иначе сужение выглядело бы
// работающим на выборке, где отбирать нечего.
func seedNarrowFixture(t *testing.T, ctx context.Context, r kacho.Repository) (target, foreign, v4ID, v6ID, extID string) {
	t.Helper()
	w, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w.Abort()

	net := newNetwork(narrowProject, "net-narrow")
	_, err = w.Networks().Insert(ctx, net)
	require.NoError(t, err)

	subA, err := w.Subnets().Insert(ctx, newSubnet(narrowProject, "sub-target", net.ID, "zone-a", []string{"10.30.0.0/16"}))
	require.NoError(t, err)
	subB, err := w.Subnets().Insert(ctx, newSubnet(narrowProject, "sub-foreign", net.ID, "zone-a", []string{"10.31.0.0/16"}))
	require.NoError(t, err)

	mkInternalV4 := func(name, subnetID, ip string) string {
		a := &domain.Address{
			ID:           ids.NewID(ids.PrefixAddress),
			ProjectID:    narrowProject,
			Name:         domain.RcNameVPC(name),
			Description:  domain.RcDescription(""),
			Labels:       domain.LabelsFromMap(nil),
			Type:         domain.AddressTypeInternal,
			IpVersion:    domain.IpVersionIPv4,
			InternalIpv4: &domain.InternalIpv4Spec{SubnetID: subnetID, Address: ip},
		}
		_, e := w.Addresses().Insert(ctx, a)
		require.NoError(t, e)
		return a.ID
	}

	v4ID = mkInternalV4("addr-v4-target", subA.ID, "10.30.0.10")

	v6 := &domain.Address{
		ID:           ids.NewID(ids.PrefixAddress),
		ProjectID:    narrowProject,
		Name:         domain.RcNameVPC("addr-v6-target"),
		Description:  domain.RcDescription(""),
		Labels:       domain.LabelsFromMap(nil),
		Type:         domain.AddressTypeInternal,
		IpVersion:    domain.IpVersionIPv6,
		InternalIpv6: &domain.InternalIpv6Spec{SubnetID: subA.ID, Address: "2001:db8:30::10"},
	}
	_, err = w.Addresses().Insert(ctx, v6)
	require.NoError(t, err)
	v6ID = v6.ID

	ext := newAddress(narrowProject, "addr-external", true)
	_, err = w.Addresses().Insert(ctx, ext)
	require.NoError(t, err)
	extID = ext.ID

	// Плотность целевой подсети — предмет утверждения о цене страницы. Третий
	// октет отсчитывается от 10, чтобы наполнитель не столкнулся по уникальности
	// (подсеть, адрес) с названными выше адресами: такое столкновение обрывает
	// засев в середине транзакции, и разбирать пришлось бы уже последствия.
	for i := 0; i < narrowDenseRows; i++ {
		mkInternalV4(fmt.Sprintf("dense-%d", i), subA.ID, fmt.Sprintf("10.30.%d.%d", i/250+10, i%250+1))
	}
	// Соседняя подсеть — тот сосед, который делает разницу между «сузили» и
	// «в таблице и так ничего чужого не было».
	for i := 0; i < narrowForeignRows; i++ {
		mkInternalV4(fmt.Sprintf("foreign-%d", i), subB.ID, fmt.Sprintf("10.31.0.%d", i+1))
	}
	// Прочие подсети того же проекта — избирательность предиката подсети.
	for s := 0; s < narrowOtherSubnets; s++ {
		other, oerr := w.Subnets().Insert(ctx, newSubnet(narrowProject,
			fmt.Sprintf("sub-other-%d", s), net.ID, "zone-a", []string{fmt.Sprintf("10.%d.0.0/16", 40+s)}))
		require.NoError(t, oerr)
		for i := 0; i < narrowDenseRows; i++ {
			mkInternalV4(fmt.Sprintf("other-%d-%d", s, i), other.ID,
				fmt.Sprintf("10.%d.%d.%d", 40+s, i/250, i%250+1))
		}
	}
	require.NoError(t, w.Commit())
	return subA.ID, subB.ID, v4ID, v6ID, extID
}

// TestIntegration_AddressList_NarrowBySubnet_SelectsBothFamilies — что сужение
// ОТБИРАЕТ: обе семьи своей подсети, ничего чужого, и без сужения — всё.
func TestIntegration_AddressList_NarrowBySubnet_SelectsBothFamilies(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test (testcontainers); skipped in -short")
	}
	ctx := context.Background()
	pool, err := coredb.NewPool(ctx, setupTestDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)
	r := kachopg.New(pool, nil)
	defer r.Close()

	target, foreign, v4ID, v6ID, extID := seedNarrowFixture(t, ctx, r)

	rd, err := r.Reader(ctx)
	require.NoError(t, err)
	defer func() { _ = rd.Close() }()

	ids := func(f kacho.AddressFilter, size int64) []string {
		got, _, lerr := rd.Addresses().List(ctx, f, kacho.Pagination{PageSize: size})
		require.NoError(t, lerr)
		out := make([]string, 0, len(got))
		for _, a := range got {
			out = append(out, a.ID)
		}
		return out
	}

	t.Run("своя подсеть отдаёт ОБЕ семьи", func(t *testing.T) {
		got := ids(kacho.AddressFilter{ProjectID: narrowProject, SubnetID: target}, 1000)
		assert.Contains(t, got, v4ID,
			"сужение обязано быть ОБЪЕДИНЕНИЕМ по семьям: адрес принадлежит подсети, если ссылка стоит в любой из них")
		assert.Contains(t, got, v6ID, "вторая семья — тот же вопрос, а не отдельный ресурс")
		assert.Len(t, got, narrowDenseRows+2, "и ровно свои: плотность целевой подсети плюс обе семьи")
	})

	t.Run("чужая подсеть своих не отдаёт — отрицание", func(t *testing.T) {
		got := ids(kacho.AddressFilter{ProjectID: narrowProject, SubnetID: foreign}, 1000)
		assert.NotContains(t, got, v4ID)
		assert.NotContains(t, got, v6ID)
		assert.NotContains(t, got, extID, "внешний адрес подсети не имеет и не может принадлежать ничьей")
		// Положительная половина того же утверждения: чужая подсеть не пуста —
		// иначе «своих не отдаёт» было бы верно и при полностью сломанном сужении.
		assert.Len(t, got, narrowForeignRows, "у соседней подсети свои адреса на месте")
	})

	t.Run("без сужения список шире — второй положительный контроль", func(t *testing.T) {
		// Утверждается ОТНОШЕНИЕ, а не абсолютное число: без сужения страница
		// упирается в потолок размера (1000), поэтому «видны все» здесь не
		// проверяемо и проверялось бы неверно. Предмет контроля другой: сужение
		// действительно убирает строки, а не отдаёт то же, что и без него.
		wide := len(ids(kacho.AddressFilter{ProjectID: narrowProject}, 1000))
		narrowed := len(ids(kacho.AddressFilter{ProjectID: narrowProject, SubnetID: target}, 1000))
		assert.Equal(t, 1000, wide, "предпосылка: без сужения выдача упирается в потолок страницы")
		assert.Less(t, narrowed, wide, "сужение обязано убирать строки, а не отдавать то же самое")
		assert.Equal(t, narrowDenseRows+2, narrowed)
	})
}

// capturedStmt — оператор, который репозиторий реально отправил в базу.
type capturedStmt struct {
	sql  string
	args []any
}

// sqlCapture — pgx-трассировщик: производитель входа для гейта плана.
//
// Гейт, EXPLAIN'ящий собственную копию запроса, ничего не говорит о запросе
// продукта — он остаётся зелёным ровно тогда, когда репозиторий исполняет ДРУГОЙ
// предикат, то есть в единственном интересном случае. Поэтому вход захватывается:
// вызывается настоящий `List`, а планируется тот текст, который ушёл на сервер.
type sqlCapture struct {
	mu    sync.Mutex
	stmts []capturedStmt
}

func (c *sqlCapture) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stmts = append(c.stmts, capturedStmt{sql: data.SQL, args: data.Args})
	return ctx
}

func (c *sqlCapture) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

// pageOf — захваченные операторы, читающие страницу адресов.
func (c *sqlCapture) pageOf(table string) []capturedStmt {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []capturedStmt
	for _, s := range c.stmts {
		if strings.Contains(s.sql, "FROM "+table) && strings.Contains(s.sql, "ORDER BY created_at") {
			out = append(out, s)
		}
	}
	return out
}

// TestIntegration_AddressList_NarrowBySubnet_PlanIsPagePriced — что сужение СТОИТ.
//
// План строится по ЗАХВАЧЕННОМУ тексту запроса репозитория, а не по копии,
// написанной в гейте.
func TestIntegration_AddressList_NarrowBySubnet_PlanIsPagePriced(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test (testcontainers); skipped in -short")
	}
	ctx := context.Background()
	dsn := setupTestDB(t)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)
	r := kachopg.New(pool, nil)
	defer r.Close()

	target, _, _, _, _ := seedNarrowFixture(t, ctx, r)

	// Время создания разведено по строкам: внутри одной транзакции `now()`
	// постоянно, поэтому без этого у всех адресов один и тот же ключ курсора —
	// порядок вырождается, и вопрос «сколько стоит упорядочить» теряет предмет.
	_, err = pool.Exec(ctx,
		`UPDATE addresses SET created_at = timestamptz '2026-01-01 00:00:00Z'
		    + (abs(hashtext(id)) % 1000000) * interval '1 second'`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `ANALYZE addresses`)
	require.NoError(t, err)

	var rows, inTarget int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM addresses`).Scan(&rows))
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM addresses WHERE internal_subnet_id = $1`, target).Scan(&inTarget))
	require.Greater(t, inTarget, narrowPageSize,
		"предпосылка: в целевой подсети адресов больше страницы — иначе утверждение о цене страницы беспредметно")
	require.Greater(t, rows, inTarget*3,
		"предпосылка: целевая подсеть — меньшинство таблицы, иначе страницу дёшево набрать и обходом по времени")

	// Захват: тот же репозиторий поверх пула с трассировщиком.
	capture := &sqlCapture{}
	cfg, err := pgxpool.ParseConfig(dsn)
	require.NoError(t, err)
	cfg.ConnConfig.Tracer = capture
	traced, err := pgxpool.NewWithConfig(ctx, cfg)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, traced)

	tr := kachopg.New(traced, nil)
	defer tr.Close()
	rd, err := tr.Reader(ctx)
	require.NoError(t, err)
	_, _, err = rd.Addresses().List(ctx,
		kacho.AddressFilter{ProjectID: narrowProject, SubnetID: target},
		kacho.Pagination{PageSize: narrowPageSize})
	require.NoError(t, err)
	require.NoError(t, rd.Close())

	stmts := capture.pageOf("addresses")
	require.Len(t, stmts, 1, "предпосылка: страницу адресов читает ровно один захваченный оператор")
	stmt := stmts[0]
	require.Contains(t, stmt.args, target,
		"предпосылка: сужение по подсети доехало до SQL — иначе планируется не тот вопрос")

	plan, nodes := explainAnalyze(t, ctx, pool, stmt)
	t.Logf("объём осмотренного: строк в addresses — %d, из них в целевой подсети — %d; "+
		"захваченных операторов — %d; узлов плана — %d", rows, inTarget, len(capture.stmts), len(nodes))

	// ИСХОД, а не форма плана: сколько строк запрос реально прочитал. Утверждение
	// про имя индекса ниже — про механизм; это — про свойство, ради которого
	// механизм заводится, и оно останется верным, если планировщик найдёт другой
	// путь той же цены.
	maxTouched := 0
	worst := ""
	for _, n := range nodes {
		if n.touched > maxTouched {
			maxTouched, worst = n.touched, n.nodeType
		}
	}
	assert.LessOrEqual(t, maxTouched, 2*(narrowPageSize+1),
		"страница обязана стоить страницу: самый жадный узел (%s) тронул %d строк при %d в подсети и %d в таблице\n%s",
		worst, maxTouched, inTarget, rows, plan)

	// Механизм, которым это достигается, — тоже утверждается: иначе «дёшево»
	// однажды окажется случайным следствием размера тестовой таблицы.
	//
	// Имя индекса сверяется РАВЕНСТВОМ, а не вхождением подстроки. Проба инъекции
	// это и показала: переименованный индекс `…_page_idx_DISABLED_FOR_INJECTION`
	// содержит искомую подстроку целиком, поэтому утверждение о вхождении осталось
	// зелёным на дереве, где предмета уже не было.
	used := make([]string, 0, len(nodes))
	subnetDriven := false
	for _, n := range nodes {
		if n.indexName != "" {
			used = append(used, fmt.Sprintf("%s[%s]", n.indexName, n.indexCond))
		}
		if n.indexName != "" && strings.Contains(n.indexCond, subnetColumn) {
			subnetDriven = true
		}
	}
	// Утверждается СВОЙСТВО спуска, а не имя индекса: подсеть обязана стоять в
	// УСЛОВИИ ИНДЕКСА, то есть вести спуск по дереву, а не отбираться фильтром
	// после него.
	//
	// Здесь стояло имя (#963) — под то дерево, где индекс с этим предикатом был
	// один. Посылка была верна про данные и неверна про планировщик: равенств в
	// запросе ДВА, и пока каждый индекс покрывал ровно одно, выбор между ними
	// решала статистика. Замер: восемь прогонов дали один красный, а конвейер —
	// семь красных на ветках, каталога vpc не касавшихся. Индекс своего
	// предиката возвращён миграцией 20260823080000, и индексов, ведущих спуск
	// подсетью, снова два.
	//
	// Возвращать «любое из двух имён» нельзя — на дереве с одним индексом такой
	// предикат вырождается в проверку существования, и это уже было названо
	// обходом. Условие индекса от числа индексов не зависит вовсе: оно ловит и
	// подмену на общий проектный (там подсеть уходит в `Filter`), и любой
	// будущий индекс, который подсетью спуск не ведёт.
	assert.Truef(t, subnetDriven,
		"сужение обязано вести спуск по подсети: ни у одного индексного узла %s не стоит в условии индекса; "+
			"план использовал %v:\n%s",
		subnetColumn, used, plan)
	for _, n := range nodes {
		assert.NotContains(t, n.nodeType, "Sort",
			"порядок обязан приходить из индекса, а не из сортировки страницы подсети: %s", plan)
		assert.NotContains(t, n.nodeType, "Seq Scan",
			"страница вкладки не читает таблицу адресов всех проектов: %s", plan)
	}
}

// planNode — узел плана, из которого гейт читает ИСХОД.
//
// `touched` — строки, которые узел ТРОНУЛ, то есть выданные плюс отброшенные
// фильтром. Одних выданных мало: фильтр применяется внутри узла, поэтому обход,
// прочитавший пол-таблицы ради пятидесяти строк, отчитывается пятьюдесятью
// «Actual Rows», а его настоящая цена лежит в «Rows Removed by Filter». Проба
// инъекции это и показала: без индекса утверждение о цене оставалось зелёным.
type planNode struct {
	nodeType  string
	indexName string
	// indexCond — условие ИНДЕКСА узла, то есть то, чем ведётся спуск по дереву.
	// Отличать его от `Filter` обязательно: обе строки называют одни и те же
	// колонки, но первая сужает обход, а вторая отбрасывает уже прочитанное —
	// и именно в этой разнице лежит цена страницы.
	indexCond  string
	actualRows int
	touched    int
}

// subnetColumn — колонка, которой обязан вестись спуск при сужении по подсети.
const subnetColumn = "internal_subnet_id"

// explainAnalyze исполняет захваченный оператор под EXPLAIN ANALYZE и возвращает
// печатный план (для сообщения об отказе) и его узлы (для утверждений).
func explainAnalyze(t *testing.T, ctx context.Context, pool *pgxpool.Pool, stmt capturedStmt) (string, []planNode) {
	t.Helper()

	var raw []map[string]any
	require.NoError(t, pool.QueryRow(ctx,
		"EXPLAIN (ANALYZE, FORMAT JSON) "+stmt.sql, stmt.args...).Scan(&raw))
	require.NotEmpty(t, raw, "предпосылка: план прочитан")

	var nodes []planNode
	var walk func(m map[string]any)
	walk = func(m map[string]any) {
		nt, _ := m["Node Type"].(string)
		ar, _ := m["Actual Rows"].(float64)
		ix, _ := m["Index Name"].(string)
		ic, _ := m["Index Cond"].(string)
		rr, _ := m["Rows Removed by Filter"].(float64)
		ri, _ := m["Rows Removed by Index Recheck"].(float64)
		if nt != "" {
			nodes = append(nodes, planNode{
				nodeType: nt, indexName: ix, indexCond: ic,
				actualRows: int(ar), touched: int(ar + rr + ri),
			})
		}
		if kids, ok := m["Plans"].([]any); ok {
			for _, k := range kids {
				if km, ok := k.(map[string]any); ok {
					walk(km)
				}
			}
		}
	}
	root, ok := raw[0]["Plan"].(map[string]any)
	require.True(t, ok, "предпосылка: корень плана разобран")
	walk(root)
	require.NotEmpty(t, nodes, "предпосылка: у плана есть узлы — иначе утверждения ниже беспредметны")

	// Печатная форма — только для сообщения об отказе; утверждения читают узлы.
	var text string
	planRows, err := pool.Query(ctx, "EXPLAIN (ANALYZE, FORMAT TEXT) "+stmt.sql, stmt.args...)
	require.NoError(t, err)
	for planRows.Next() {
		var line string
		require.NoError(t, planRows.Scan(&line))
		text += line + "\n"
	}
	planRows.Close()
	require.NoError(t, planRows.Err())
	return text, nodes
}

// TestIntegration_AddressList_NarrowBySubnet_MatchesChildList — два пути чтения
// отвечают на один вопрос одинаково.
//
// У сравнения два независимых производителя: дочерний список подсети
// (`SubnetReader.AddressesBySubnet`) и публичный список с сужением
// (`AddressReader.List`). Пока публичный ходил дизъюнкцией jsonb-выражений, а
// дочерний — хранимой колонкой, совпадение держалось на договорённости; тест
// делает его проверяемым, и он покраснеет, если один из путей уедет.
func TestIntegration_AddressList_NarrowBySubnet_MatchesChildList(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test (testcontainers); skipped in -short")
	}
	ctx := context.Background()
	pool, err := coredb.NewPool(ctx, setupTestDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)
	r := kachopg.New(pool, nil)
	defer r.Close()

	target, foreign, v4ID, v6ID, _ := seedNarrowFixture(t, ctx, r)

	rd, err := r.Reader(ctx)
	require.NoError(t, err)
	defer func() { _ = rd.Close() }()

	collect := func(recs []*kacho.AddressRecord) []string {
		out := make([]string, 0, len(recs))
		for _, a := range recs {
			out = append(out, a.ID)
		}
		return out
	}

	child, _, err := rd.Subnets().AddressesBySubnet(ctx, target, kacho.Pagination{PageSize: 1000})
	require.NoError(t, err)
	public, _, err := rd.Addresses().List(ctx,
		kacho.AddressFilter{ProjectID: narrowProject, SubnetID: target}, kacho.Pagination{PageSize: 1000})
	require.NoError(t, err)
	assert.ElementsMatch(t, collect(child), collect(public),
		"дочерний список подсети и публичный список с сужением обязаны отдавать одно множество")
	assert.Len(t, collect(child), narrowDenseRows+2,
		"положительный контроль: множество не пусто, иначе совпадение выше тривиально")
	assert.Contains(t, collect(child), v4ID)
	assert.Contains(t, collect(child), v6ID)

	// Отрицательная половина: на ЧУЖОЙ подсети оба пути тоже совпадают, и это
	// другое множество — иначе «совпадают» было бы верно и при сужении, которое
	// ничего не отбирает.
	childF, _, err := rd.Subnets().AddressesBySubnet(ctx, foreign, kacho.Pagination{PageSize: 1000})
	require.NoError(t, err)
	publicF, _, err := rd.Addresses().List(ctx,
		kacho.AddressFilter{ProjectID: narrowProject, SubnetID: foreign}, kacho.Pagination{PageSize: 1000})
	require.NoError(t, err)
	assert.ElementsMatch(t, collect(childF), collect(publicF))
	assert.NotEqual(t, len(child), len(childF), "две подсети обязаны отличаться содержимым")
}
