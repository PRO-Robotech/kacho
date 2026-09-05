// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repo_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/pgtest"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/api/network"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	kachopg "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/pg"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/repomock"
)

// journalRow — строка журнала в том виде, в каком её читает сервер потока.
type journalRow struct {
	seq               int64
	kind, id, change  string
	defaultSG         string
	defaultRouteTable string
}

// TestIntegration_NetworkCreate_OneJournalRowPerResource — одно создание сети
// объявляет КАЖДЫЙ созданный ресурс РОВНО ОДИН раз, и строка сети несёт её
// умолчания.
//
// # Что здесь предмет и почему проба идёт в базу
//
// Предмет — не порядок вызовов в коде, а то, ЧТО ПРОЧТЁТ подписчик: строки
// журнала. Их пишут ДВА производителя — код и триггеры базы (журнал vpc это
// прямо оговаривает), поэтому проба, считающая эмиссии подставного репозитория,
// о втором производителе не сказала бы ничего. Здесь настоящая схема, настоящая
// writer-транзакция и настоящая таблица журнала.
//
// # От чего защищает
//
// Прежде создание сети писало ПЯТЬ строк, три из них — о самой сети:
// `CREATED` без умолчаний, затем два `UPDATED` по мере того, как они
// достраивались в той же транзакции. Подписчик, ведущий состояние по потоку,
// принимал первую строку за факт и показывал сеть БЕЗ группы безопасности и БЕЗ
// таблицы маршрутов — состояние, которого арендатор не просил и которое
// перестало быть верным до конца того же действия.
//
// Число строк при этом было функцией нашей ВНУТРЕННЕЙ композиции: завёл бы vpc
// третье умолчание — подписчик получил бы четвёртое событие, ничего не сделав.
//
// # Почему проба утверждает ПОЛНОТУ, а не только число
//
// Одного «строк три» мало: этому удовлетворяет и починка, которая просто снимет
// два `UPDATED`, оставив `CREATED` с пустыми умолчаниями, — то есть сделает
// ложное состояние ЕДИНСТВЕННЫМ и потому уже неисправимым. Поэтому строка сети
// обязана нести оба идентификатора.
//
// # Положительный контроль здесь же
//
// Группа и таблица маршрутов — самостоятельные ресурсы, и их появление подписчик
// обязан узнать. Проба требует их строк наравне с сетью: иначе «одно создание —
// одна строка» зеленело бы на журнале, из которого выкинули всё.
func TestIntegration_NetworkCreate_OneJournalRowPerResource(t *testing.T) {
	if testing.Short() {
		t.Skip("интеграционная проба: нужна настоящая база")
	}
	ctx := context.Background()
	dsn := setupTestDB(t)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)
	r := kachopg.New(pool, nil)
	defer r.Close()

	uc := network.NewCreateNetworkUseCase(r, &repomock.ProjectClient{OK: true}, repomock.NewOpsRepo())
	op, err := uc.Execute(ctx, domain.Network{
		ProjectID:      "journalrowsproj",
		Name:           domain.RcNameVPC("journal-rows-net"),
		IPv4CidrBlocks: []string{"10.79.0.0/16"},
	})
	require.NoError(t, err)
	require.Nil(t, op.Error, "операция создания обязана быть успешной")

	rows := readVPCJournal(t, ctx, pool)

	byKind := map[string][]journalRow{}
	for _, row := range rows {
		byKind[row.kind] = append(byKind[row.kind], row)
	}

	if len(byKind["Network"]) != 1 {
		t.Errorf("о сети объявлено %d строк, ожидалась ОДНА.\n"+
			"Подписчик читает непустую нагрузку как ПОЛНОЕ состояние предмета, поэтому "+
			"каждая лишняя строка — это состояние, которое он записал как факт и которое "+
			"перестало быть верным внутри того же действия.\nЖурнал:\n%s",
			len(byKind["Network"]), formatJournal(rows))
	}
	if len(byKind["SecurityGroup"]) != 1 {
		t.Errorf("о группе по умолчанию объявлено %d строк, ожидалась ОДНА (положительный контроль).\n"+
			"Журнал:\n%s", len(byKind["SecurityGroup"]), formatJournal(rows))
	}
	if len(byKind["RouteTable"]) != 1 {
		t.Errorf("о таблице маршрутов по умолчанию объявлено %d строк, ожидалась ОДНА (положительный контроль).\n"+
			"Журнал:\n%s", len(byKind["RouteTable"]), formatJournal(rows))
	}
	if len(rows) != 3 {
		t.Errorf("строк журнала на одно создание сети %d, ожидалось 3 — по одной на созданный ресурс.\n"+
			"Число строк не имеет права быть функцией нашей внутренней композиции: заведём "+
			"третье умолчание — подписчик получит лишнее событие, ничего не сделав.\nЖурнал:\n%s",
			len(rows), formatJournal(rows))
	}

	if len(byKind["Network"]) == 1 {
		got := byKind["Network"][0]
		if got.change != "CREATED" {
			t.Errorf("род изменения строки сети %q, ожидался CREATED", got.change)
		}
		if got.defaultSG == "" {
			t.Errorf("строка сети не несёт группу безопасности по умолчанию.\n"+
				"Единственная строка обязана нести УСТОЯВШЕЕСЯ состояние: иначе ложное "+
				"состояние стало единственным и подписчику неоткуда узнать умолчания.\nЖурнал:\n%s",
				formatJournal(rows))
		}
		if got.defaultRouteTable == "" {
			t.Errorf("строка сети не несёт таблицу маршрутов по умолчанию.\nЖурнал:\n%s",
				formatJournal(rows))
		}
	}
}

// readVPCJournal читает журнал целиком в порядке позиции.
//
// Перечень читается ЦЕЛИКОМ, а не отбором по виду: «строк о сети одна» и «строк
// о сети одна, а прочих пятьдесят» — разные состояния журнала, и отбор сделал бы
// второе неотличимым от первого.
func readVPCJournal(t *testing.T, ctx context.Context, pool *pgxpool.Pool) []journalRow {
	t.Helper()
	q, err := pool.Query(ctx, `SELECT sequence_no, resource_kind, resource_id, event_type,
	       coalesce(payload->>'DefaultSecurityGroupID', ''),
	       coalesce(payload->>'DefaultRouteTableID', '')
	  FROM kacho_vpc.vpc_outbox ORDER BY sequence_no`)
	require.NoError(t, err)
	defer q.Close()
	var out []journalRow
	for q.Next() {
		var row journalRow
		require.NoError(t, q.Scan(&row.seq, &row.kind, &row.id, &row.change,
			&row.defaultSG, &row.defaultRouteTable))
		out = append(out, row)
	}
	require.NoError(t, q.Err())
	return out
}

// formatJournal печатает ОСМОТРЕННОЕ: без него «строк не столько» не отличить от
// «журнал пуст», и читатель отказа пошёл бы искать дефект не там.
func formatJournal(rows []journalRow) string {
	if len(rows) == 0 {
		return "  (журнал пуст — ни одной строки)"
	}
	var b strings.Builder
	for _, r := range rows {
		fmt.Fprintf(&b, "  %d %-14s %-8s %s sg=%q rt=%q\n",
			r.seq, r.kind, r.change, r.id, r.defaultSG, r.defaultRouteTable)
	}
	return b.String()
}
