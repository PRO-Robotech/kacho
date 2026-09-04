// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/migrations"

	"github.com/PRO-Robotech/kacho/pkg/pgtest"
)

// Обратное заполнение правил SG, записанных ДО того, как область значений стала
// проверяться.
//
// Ограничение (миграция 0027) добавляется на таблицу, где уже могут лежать
// правила с невыразимым портом или несуществующим именем протокола: их
// принимали, пока проверки не было. Без обратного заполнения `ALTER TABLE ADD
// CONSTRAINT` вообще не применился бы (он валидирует существующие строки), а
// применившись частично — сделал бы такие группы неисправимыми через API:
// любая последующая запись строки (переименование правила, добавление или
// удаление соседнего) отбивалась бы ограничением.
//
// Решение продукта: правило, которое продукт выразить не может, УДАЛЯЕТСЯ из
// набора. Правила группы — разрешающие (`Allows ingress/egress traffic`), и
// удаление разрешающего правила ничего не расширяет; любая иная нормализация
// (подтянуть порт к границе, заменить протокол на «любой») ПРИДУМАЛА бы
// намерение и в случае «любого» расширила бы доступ.

const beforeRulesDomainMigration = 26

// rulesDomainMigration — сама 0027. Проба идёт ДО неё и РОВНО на неё, а не «до
// конца цепочки», и это не оформление.
//
// Здесь стоял голый `goose.Up`, то есть проба закрепляла «0027 — вершина», а не
// своё содержание. Она покраснела на первой же следующей миграции, тронувшей ту же
// колонку: 0029 судит правило по ЕЩЁ ОДНОМУ измерению — по ЦЕЛИ, — и все правила
// этой фикстуры цели не несут, поэтому к голове набор пуст. Это верный исход 0029
// (правило без цели не разрешает ничего и блокирует правку группы), но под именем
// «обратное заполнение области значений» он утверждал бы чужое.
//
// Предмет 0027 — область значений (порт, протокол); предмет 0029 — цель, и он
// проверяется своей пробой в `services/vpc/internal/migrations/`.
const rulesDomainMigration = 27

// VPC-SGBF-1 — строки, записанные до правки, приводятся к выразимому виду, и
// после этого ограничение к ним применимо.
func TestSGRulesBackfill_LegacyRowsNormalised(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	dsn := setupTestDBUpTo(t, beforeRulesDomainMigration)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)

	netID := insertNetworkSQL(t, ctx, pool, "net-sgbf")

	// Группа, записанная до правки: одно выразимое правило и три невыразимых.
	mixedID := ids.NewID(ids.PrefixSecurityGroup)
	_, err = pool.Exec(ctx, `
		INSERT INTO security_groups (id, project_id, network_id, name, rules)
		VALUES ($1, 'proj-sgbf', $2, '', $3::jsonb)`, mixedID, netID, `[
			{"ID":"keep","Direction":"INGRESS","FromPort":22,"ToPort":22,"ProtocolName":"tcp"},
			{"ID":"drop-port","Direction":"INGRESS","FromPort":65536,"ToPort":65536,"ProtocolName":"tcp"},
			{"ID":"drop-proto","Direction":"INGRESS","FromPort":80,"ToPort":80,"ProtocolName":"klingon"},
			{"ID":"drop-number","Direction":"EGRESS","FromPort":-1,"ToPort":-1,"ProtocolNumber":999}
		]`)
	require.NoError(t, err, "до миграции такие значения принимались — иначе предпосылка теста ложна")

	// Группа целиком из невыразимых правил — вырождается в пустой набор.
	allBadID := ids.NewID(ids.PrefixSecurityGroup)
	_, err = pool.Exec(ctx, `
		INSERT INTO security_groups (id, project_id, network_id, name, rules)
		VALUES ($1, 'proj-sgbf', $2, '', $3::jsonb)`, allBadID, netID, `[
			{"ID":"x","Direction":"INGRESS","FromPort":-1,"ToPort":80}
		]`)
	require.NoError(t, err)

	// Группа, которую трогать не за что — обязана остаться побайтово прежней.
	untouchedID := ids.NewID(ids.PrefixSecurityGroup)
	const untouchedRules = `[{"ID": "ok", "ToPort": -1, "Labels": null, "FromPort": -1, "Direction": "EGRESS", "ProtocolName": "ANY", "ProtocolNumber": -1}]`
	_, err = pool.Exec(ctx, `
		INSERT INTO security_groups (id, project_id, network_id, name, rules)
		VALUES ($1, 'proj-sgbf', $2, '', $3::jsonb)`, untouchedID, netID, untouchedRules)
	require.NoError(t, err)
	var before string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT rules::text FROM security_groups WHERE id = $1`, untouchedID).Scan(&before))

	// Догоняем РОВНО до 0027 — обратное заполнение + ограничение (см.
	// rulesDomainMigration о том, почему не до конца цепочки).
	db, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	goose.SetBaseFS(migrations.FS)
	require.NoError(t, goose.SetDialect("postgres"))
	require.NoError(t, goose.UpTo(db, ".", rulesDomainMigration),
		"миграция обязана примениться на строках, записанных до правки")

	var kept []string
	rows, err := pool.Query(ctx,
		`SELECT jsonb_array_elements(rules) ->> 'ID' FROM security_groups WHERE id = $1`, mixedID)
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var id string
		require.NoError(t, rows.Scan(&id))
		kept = append(kept, id)
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, []string{"keep"}, kept, "остаться обязано ровно выразимое правило")

	var n int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT jsonb_array_length(rules) FROM security_groups WHERE id = $1`, allBadID).Scan(&n))
	assert.Equal(t, 0, n, "группа из одних невыразимых правил вырождается в пустой набор, а не в NULL")

	var after string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT rules::text FROM security_groups WHERE id = $1`, untouchedID).Scan(&after))
	assert.Equal(t, before, after, "выразимый набор обязан остаться нетронутым")

	// И главное: строка, пережившая заполнение, снова редактируема — чтение,
	// правка, запись проходит ограничение.
	_, err = pool.Exec(ctx, `
		UPDATE security_groups
		   SET rules = rules || '[{"ID":"added","Direction":"INGRESS","FromPort":443,"ToPort":443,"ProtocolName":"tcp"}]'::jsonb
		 WHERE id = $1`, mixedID)
	assert.NoError(t, err, "после заполнения группа обязана снова принимать правки")
}
