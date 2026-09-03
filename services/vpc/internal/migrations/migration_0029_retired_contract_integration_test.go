// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// migration_0029_retired_contract_integration_test.go — upgrade-регрессия миграции
// 0029 на НЕПУСТОМ дереве: снятое с контракта пережило свои строки.
//
// Три предмета одной миграции, и все три — «контракта нет, а строка есть»:
//
//  1. `subnets.dhcp_options` — колонка baseline. Контракт снят (номера и имя
//     зарезервированы), Go-типа больше нет, читателя у значения нет ни одного.
//  2. `networks.route_distinguisher` — колонка baseline. Читателя нет ни одного;
//     публичная поверхность её и не несла (это не `vrf_id`, который жив и
//     internal-only — его 0029 НЕ трогает, и здесь это утверждается).
//  3. `security_groups.rules` — правила с ключом снятой ветви цели. После снятия
//     такое правило проецируется БЕЗ цели, по закрытой модели не разрешает ничего,
//     и — хуже — блокирует правку группы: вызывающий, прочитав группу и записав её
//     обратно, получает отказ «цель обязательна» на правиле, которого он не писал.
//
// Пробы идут ДО 0029 и РОВНО на неё (`goose.UpTo`), а не «до конца цепочки»: голый
// `goose.Up` + один `Down` выражал бы «0029 — вершина», а не содержание 0029, и
// краснел бы на любой следующей миграции, ничего не сообщая о снятом.
//
// Дом пробы — пакет МИГРАЦИЙ, а не репозитория: предмет здесь цепочка, и её TestMain
// уже раздаёт ПУСТЫЕ базы (шаблон не мигрирован — см. `testmain_pgtest_test.go`),
// что и требуется, чтобы стартовать раньше головы. В пакете репозитория пришлось бы
// брать пустую базу в обход его собственного предмигрированного шаблона.
package migrations_test

import (
	"database/sql"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/internal/pgtest"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/migrations"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/helpers"
)

// preRetiredContractVersion — последняя миграция ДО 0029.
const preRetiredContractVersion int64 = 28

// retiredContractVersion — сама 0029.
const retiredContractVersion int64 = 29

// noticeSink собирает NOTICE-сообщения соединения.
//
// Наблюдаемость этой миграции — не украшение, а требование: правило, которое она
// удаляет, восстановить неоткуда, поэтому «ноль изменённых» обязано быть отличимо
// от «ноль осмотренных». Утверждать это можно только прочитав то, что миграция
// печатает, — иначе печать остаётся заявлением о себе самой.
type noticeSink struct {
	mu    sync.Mutex
	lines []string
}

func (s *noticeSink) add(n *pgconn.Notice) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lines = append(s.lines, n.Message)
}

// text — все собранные NOTICE одной строкой, для утверждений по содержанию.
func (s *noticeSink) text() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.Join(s.lines, "\n")
}

// openChainAt выдаёт пробе собственную ПУСТУЮ базу на контейнере пакета, применяет
// цепочку РОВНО до version и возвращает открытое соединение вместе с приёмником
// NOTICE.
//
// Пустая база, а не клон предмигрированного шаблона: проба обязана стартовать
// раньше текущей головы цепочки, чтобы вписать строки, которые 0029 будет править.
func openChainAt(t testing.TB, version int64) (*sql.DB, *noticeSink) {
	t.Helper()
	dsn := pgtest.NewEmptyDB(t)

	cfg, err := pgx.ParseConfig(dsn)
	require.NoError(t, err)
	sink := &noticeSink{}
	cfg.OnNotice = func(_ *pgconn.PgConn, n *pgconn.Notice) { sink.add(n) }
	name := stdlib.RegisterConnConfig(cfg)
	t.Cleanup(func() { stdlib.UnregisterConnConfig(name) })

	db, err := sql.Open("pgx", name)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	// Одно соединение на пробу: NOTICE приходит по тому соединению, которое
	// исполняло DO-блок, а пул мог бы отдать миграции одно, а утверждению другое.
	db.SetMaxOpenConns(1)

	goose.SetBaseFS(migrations.FS)
	require.NoError(t, goose.SetDialect("postgres"))
	goose.SetLogger(goose.NopLogger())
	require.NoError(t, goose.UpTo(db, ".", version))
	return db, sink
}

// seedNetwork — родитель для FK security_groups.network_id / subnets.network_id.
func seedNetwork(t *testing.T, db *sql.DB, name string) string {
	t.Helper()
	id := ids.NewID(ids.PrefixNetwork)
	_, err := db.Exec(
		`INSERT INTO networks (id, project_id, name) VALUES ($1, 'prj-r29', $2)`, id, name)
	require.NoError(t, err)
	return id
}

// seedSGWithRules — прямой INSERT группы с заданным JSONB правил (минуя use-case:
// строки, которые правит 0029, писались тогда, когда проверки не было).
func seedSGWithRules(t *testing.T, db *sql.DB, netID, name, rules string) string {
	t.Helper()
	id := ids.NewID(ids.PrefixSecurityGroup)
	_, err := db.Exec(`
		INSERT INTO security_groups (id, project_id, network_id, name, rules)
		VALUES ($1, 'prj-r29', $2, $3, $4::jsonb)`, id, netID, name, rules)
	require.NoError(t, err, "схема 0001..0028 принимает такое правило — это и есть строка под нормализацию")
	return id
}

// rulesOf читает набор правил группы как разобранный JSON.
func rulesOf(t *testing.T, db *sql.DB, sgID string) []map[string]any {
	t.Helper()
	var raw []byte
	require.NoError(t, db.QueryRow(`SELECT rules FROM security_groups WHERE id = $1`, sgID).Scan(&raw))
	var out []map[string]any
	require.NoError(t, json.Unmarshal(raw, &out))
	return out
}

// rulesRawOf читает набор правил группы как текст — для утверждения «не тронуто».
func rulesRawOf(t *testing.T, db *sql.DB, sgID string) string {
	t.Helper()
	var raw string
	require.NoError(t, db.QueryRow(`SELECT rules::text FROM security_groups WHERE id = $1`, sgID).Scan(&raw))
	return raw
}

func columnExists(t *testing.T, db *sql.DB, table, column string) bool {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRow(`
		SELECT count(*) FROM information_schema.columns
		 WHERE table_schema = 'kacho_vpc' AND table_name = $1 AND column_name = $2`,
		table, column).Scan(&n))
	return n == 1
}

func ruleByID(rules []map[string]any, id string) map[string]any {
	for _, r := range rules {
		if r["ID"] == id {
			return r
		}
	}
	return nil
}

func ruleIDs(rules []map[string]any) []string {
	out := make([]string, 0, len(rules))
	for _, r := range rules {
		s, _ := r["ID"].(string)
		out = append(out, s)
	}
	return out
}

// VPC-R29-1 — предмет нормализации: правила со снятой ветвью цели.
//
// Каждый исход утверждается отдельно, и рядом стоят ДВА положительных контроля
// (правило с блоками адресов и правило с целью-группой). Без них проба зеленела бы
// на миграции, которая перезаписывает все правила подряд: «невыразимое исчезло»
// верно и для набора, вычищенного целиком.
func TestMigration0029_SGRuleTargets_NormalizesRetiredTargetAndSparesLegalRules(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test (testing.Short)")
	}
	db, sink := openChainAt(t, preRetiredContractVersion)

	netID := seedNetwork(t, db, "r29net")
	peerID := seedSGWithRules(t, db, netID, "r29peer", `[]`)

	// Порядок правил значим (контракт), поэтому он и утверждается ниже.
	stored := `[
	  {"ID":"r-self","Direction":1,"FromPort":-1,"ToPort":-1,"ProtocolName":"any","ProtocolNumber":-1,
	   "PredefinedTarget":"self_security_group"},
	  {"ID":"r-lbhc","Direction":1,"FromPort":80,"ToPort":80,"ProtocolName":"tcp","ProtocolNumber":0,
	   "PredefinedTarget":"loadbalancer_healthchecks"},
	  {"ID":"r-cidr","Direction":1,"FromPort":22,"ToPort":22,"ProtocolName":"tcp","ProtocolNumber":0,
	   "V4CidrBlocks":["10.0.0.0/8"]},
	  {"ID":"r-sg","Direction":2,"FromPort":-1,"ToPort":-1,"ProtocolName":"any","ProtocolNumber":-1,
	   "SecurityGroupID":"` + peerID + `"},
	  {"ID":"r-both","Direction":1,"FromPort":443,"ToPort":443,"ProtocolName":"tcp","ProtocolNumber":0,
	   "V6CidrBlocks":["2001:db8::/32"],"PredefinedTarget":"self_security_group"},
	  {"ID":"r-none","Direction":1,"FromPort":53,"ToPort":53,"ProtocolName":"udp","ProtocolNumber":0}
	]`
	sgID := seedSGWithRules(t, db, netID, "r29main", stored)

	require.NoError(t, goose.UpTo(db, ".", retiredContractVersion),
		"апгрейд обязан пройти на непустом наборе правил")

	rules := rulesOf(t, db, sgID)

	// Исход по каждому виду, и порядок сохранён.
	assert.Equal(t, []string{"r-self", "r-cidr", "r-sg", "r-both"}, ruleIDs(rules),
		"невыразимое правило снято, выразимое сохранено, порядок не переставлен")

	self := ruleByID(rules, "r-self")
	require.NotNil(t, self)
	assert.Equal(t, sgID, self["SecurityGroupID"],
		"`self_security_group` означает «цель — сама эта группа» и выразим живой ветвью: id самой группы")
	assert.NotContains(t, self, "PredefinedTarget", "снятый ключ не остаётся в строке")

	both := ruleByID(rules, "r-both")
	require.NotNil(t, both)
	assert.NotContains(t, both, "PredefinedTarget", "мёртвый ключ снят")
	assert.Equal(t, []any{"2001:db8::/32"}, both["V6CidrBlocks"],
		"живая цель правила НЕ переписывается на самоссылку — она уже была выразима")
	assert.NotContains(t, both, "SecurityGroupID",
		"вторая цель не дописывается: правило с двумя целями не выразимо контрактом")

	// Положительные контроли: законные правила побайтово те же.
	cidr := ruleByID(rules, "r-cidr")
	require.NotNil(t, cidr)
	assert.Equal(t, []any{"10.0.0.0/8"}, cidr["V4CidrBlocks"])
	assert.Equal(t, float64(22), cidr["FromPort"])
	assert.Equal(t, "tcp", cidr["ProtocolName"])
	sg := ruleByID(rules, "r-sg")
	require.NotNil(t, sg)
	assert.Equal(t, peerID, sg["SecurityGroupID"])
	assert.Equal(t, float64(2), sg["Direction"])

	// Соседняя группа с пустым набором не задета.
	assert.Equal(t, `[]`, rulesRawOf(t, db, peerID))

	// Наблюдаемость: миграция назвала И объём осмотренного, И объём изменённого.
	notices := sink.text()
	assert.Contains(t, notices, "security_groups retired rule target",
		"миграция обязана назвать предмет в NOTICE")
	assert.Contains(t, notices, "examined", "объём осмотренного назван числом")
	assert.Contains(t, notices, "dropped 2", "снятые правила посчитаны (r-lbhc + r-none)")
	assert.Contains(t, notices, "self-target 1", "нормализованные к самоссылке посчитаны")
	assert.Contains(t, notices, sgID, "группа, потерявшая правило, названа по id")
}

// VPC-R29-2 — положительный контроль на уровне МИГРАЦИИ: набор, весь выразимый,
// не переписывается вовсе, а печать всё равно называет объём осмотренного.
//
// Это и есть проверка «ноль изменённых отличимо от ноль осмотренных»: без неё
// молчащая миграция была бы неотличима от миграции, которую не применили.
func TestMigration0029_SGRuleTargets_LeavesExpressibleRulesByteIdentical(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test (testing.Short)")
	}
	db, sink := openChainAt(t, preRetiredContractVersion)

	netID := seedNetwork(t, db, "r29clean")
	stored := `[{"ID":"ok-1","Direction":1,"FromPort":443,"ToPort":443,"ProtocolName":"tcp","ProtocolNumber":0,"V4CidrBlocks":["192.0.2.0/24"]}]`
	sgID := seedSGWithRules(t, db, netID, "r29cleansg", stored)
	before := rulesRawOf(t, db, sgID)

	require.NoError(t, goose.UpTo(db, ".", retiredContractVersion))

	assert.Equal(t, before, rulesRawOf(t, db, sgID),
		"выразимый набор не переписывается — ни порядок, ни ключи, ни пробелы")

	notices := sink.text()
	assert.Contains(t, notices, "examined 1", "осмотренное названо числом даже когда менять нечего")
	assert.Contains(t, notices, "rewritten 0", "ноль изменённых объявлен, а не подразумевается")
}

// VPC-R29-3 — колонки без контракта сняты, а живой internal-только идентификатор
// НЕ снят. Предпосылка пробы утверждается ДО апгрейда: без этого «колонки нет»
// зеленело бы и на схеме, где её никогда не было.
func TestMigration0029_DropsRetiredColumnsAndKeepsVrfId(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test (testing.Short)")
	}
	db, _ := openChainAt(t, preRetiredContractVersion)

	require.True(t, columnExists(t, db, "subnets", "dhcp_options"),
		"предпосылка: до 0029 колонка есть")
	require.True(t, columnExists(t, db, "networks", "route_distinguisher"),
		"предпосылка: до 0029 колонка есть")

	require.NoError(t, goose.UpTo(db, ".", retiredContractVersion))

	assert.False(t, columnExists(t, db, "subnets", "dhcp_options"))
	assert.False(t, columnExists(t, db, "networks", "route_distinguisher"))
	assert.True(t, columnExists(t, db, "networks", "vrf_id"),
		"vrf_id — ДРУГОЙ предмет: живой internal-only идентификатор, 0029 его не касается")
}

// VPC-R29-4 — список колонок, которым читает репозиторий, разрешается на схеме
// ПОСЛЕ снятия.
//
// Это тот отказ, который иначе увидел бы арендатор, а не прогон: колонка ушла из
// схемы, а `SubnetCols` продолжает её называть — каждый INSERT/SELECT подсети
// отвечает 42703. Утверждается исполнением запроса, а не чтением константы:
// прочитанная константа говорит о тексте, исполненный запрос — о схеме.
func TestMigration0029_RepoColumnListsResolveAfterDrop(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test (testing.Short)")
	}
	db, _ := openChainAt(t, retiredContractVersion)

	_, err := db.Exec(`SELECT ` + helpers.SubnetCols + ` FROM subnets WHERE false`)
	require.NoError(t, err, "SubnetCols обязан разрешаться на схеме после снятия dhcp_options")
	_, err = db.Exec(`SELECT ` + helpers.NetworkCols + ` FROM networks WHERE false`)
	require.NoError(t, err, "NetworkCols обязан разрешаться на схеме после снятия route_distinguisher")

	// Зеркальная проверка: снятые имена в списках не остались. Без неё запрос выше
	// прошёл бы и на списке, который назвал колонку и получил её из другой таблицы.
	assert.NotContains(t, helpers.SubnetCols, "dhcp_options")
	assert.NotContains(t, helpers.NetworkCols, "route_distinguisher")
}

// VPC-R29-5 — Down исполним и возвращает форму схемы.
//
// Неисполнимый Down заклинил бы откат всей цепочки. Значения при этом НЕ
// восстанавливаются, и это утверждается прямо: колонка возвращается пустой.
func TestMigration0029_DownIsRunnableAndRestoresShapeNotValues(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test (testing.Short)")
	}
	db, _ := openChainAt(t, retiredContractVersion)

	netID := seedNetwork(t, db, "r29down")

	require.NoError(t, goose.Down(db, "."), "Down 0029 обязан исполняться")
	version, err := goose.GetDBVersion(db)
	require.NoError(t, err)
	require.Equal(t, preRetiredContractVersion, version, "откат снимает ровно 0029")

	assert.True(t, columnExists(t, db, "subnets", "dhcp_options"))
	assert.True(t, columnExists(t, db, "networks", "route_distinguisher"))

	var rd string
	require.NoError(t, db.QueryRow(
		`SELECT route_distinguisher FROM networks WHERE id = $1`, netID).Scan(&rd))
	assert.Equal(t, "", rd, "форма вернулась, значения — нет: восстанавливать их неоткуда")
}
