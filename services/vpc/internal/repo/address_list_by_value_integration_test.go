// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repo_test

// Сужение списка адресов по ЗНАЧЕНИЮ — замена снятому поиску по значению.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЗАЧЕМ ЗАМЕНА, А НЕ ПРОСТО СНЯТИЕ
//
// Снимаемый метод отвечал на вопрос «чей это адрес?». Вопрос законный, и его нельзя
// потерять вместе с методом: снятие без замены — не упрощение, а отнятая возможность.
//
// Почему метод всё же снимается: его внешняя ветвь была НЕАВТОРИЗУЕМА ПО ПОСТРОЕНИЮ.
// Область запроса бралась из подсети, а у ВНЕШНЕГО адреса подсети нет — значит для
// него не существовало объекта, про который можно задать вопрос о правах. Список с
// сужением берёт область из проекта, который вызывающий и так обязан назвать.
//
// ЧЕТЫРЕ ФОРМЫ ВЛАДЕНИЯ, ОДИН ВОПРОС. Адрес бывает внутренним (из подсети) и внешним
// (из пула), в двух семействах — итого четыре места хранения значения. Сужение,
// покрывающее только внутренние, отвечало бы «не найдено» на законный внешний адрес,
// то есть заменой не было бы. Проба проверяет все четыре и рядом — что чужое значение
// не находится.

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
	kachopg "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/pg"

	"github.com/PRO-Robotech/kacho/pkg/pgtest"
)

func TestIntegration_Address_ListByValue_CoversAllOwnershipForms(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool, err := coredb.NewPool(ctx, setupTestDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)
	r := kachopg.New(pool, nil)
	defer r.Close()

	const proj = "prj-list-by-value"
	// Подсеть внутри полезной нагрузки внутреннего адреса держится ВНЕШНИМ КЛЮЧОМ —
	// схема строже, чем кажется по виду JSONB. Значит сеть и подсеть обязаны
	// существовать; фикстура, обходящая это, была бы снисходительнее продукта и
	// делала бы ненаблюдаемым именно тот инвариант, который схема держит.
	const netID, subID = "netlistbyvalue000000", "sublistbyvalue000000"
	_, err = pool.Exec(ctx,
		`INSERT INTO kacho_vpc.networks (id, project_id, name) VALUES ($1,$2,$3)`,
		netID, proj, "net-list-by-value")
	require.NoError(t, err, "посев сети")
	_, err = pool.Exec(ctx,
		`INSERT INTO kacho_vpc.subnets (id, project_id, network_id, name, placement_type, zone_id)
		 VALUES ($1,$2,$3,$4,'ZONAL','zone-a')`,
		subID, proj, netID, "sub-list-by-value")
	require.NoError(t, err, "посев подсети")
	// Значения кладутся прямым INSERT: предмет пробы — СУЖЕНИЕ по значению, а не
	// путь выделения. Проходить через выделение значило бы сделать пробу зависимой
	// от пула и от IPAM, то есть от того, о чём она не утверждает.
	seed := []struct {
		id   string
		col  string
		body string
	}{
		{"adrintv400000000000a", "internal_ipv4", fmt.Sprintf(`{"address":"10.1.0.5","subnet_id":%q}`, subID)},
		{"adrintv600000000000b", "internal_ipv6", fmt.Sprintf(`{"address":"fd00::5","subnet_id":%q}`, subID)},
		{"adrextv400000000000c", "external_ipv4", `{"address":"203.0.113.5"}`},
		{"adrextv600000000000d", "external_ipv6", `{"address":"2001:db8::5"}`},
	}
	for _, s := range seed {
		_, ierr := pool.Exec(ctx,
			`INSERT INTO kacho_vpc.addresses (id, project_id, name, `+s.col+`)
			 VALUES ($1, $2, $3, $4::jsonb)`,
			s.id, proj, s.id, s.body)
		require.NoError(t, ierr, "посев %s", s.id)
	}

	rd, err := r.Reader(ctx)
	require.NoError(t, err)
	defer func() { _ = rd.Close() }()

	for _, tc := range []struct {
		value string
		want  string
	}{
		{"10.1.0.5", "adrintv400000000000a"},
		{"fd00::5", "adrintv600000000000b"},
		{"203.0.113.5", "adrextv400000000000c"},
		{"2001:db8::5", "adrextv600000000000d"},
	} {
		t.Run(tc.value, func(t *testing.T) {
			got, _, lerr := rd.Addresses().List(ctx,
				kacho.AddressFilter{ProjectID: proj, IPAddress: tc.value},
				kacho.Pagination{PageSize: 10})
			require.NoError(t, lerr)
			require.Len(t, got, 1,
				"сужение по значению обязано находить адрес В ЛЮБОЙ форме владения: "+
					"покрывающее только внутренние отвечало бы «не найдено» на законный внешний")
			require.Equal(t, tc.want, got[0].ID)
		})
	}

	// Отрицание в паре: чужое значение не находится. Без него утверждения выше
	// зеленели бы на реализации, которая сужение игнорирует и отдаёт всё подряд.
	t.Run("чужое значение не находится", func(t *testing.T) {
		got, _, lerr := rd.Addresses().List(ctx,
			kacho.AddressFilter{ProjectID: proj, IPAddress: "198.51.100.200"},
			kacho.Pagination{PageSize: 10})
		require.NoError(t, lerr)
		require.Empty(t, got, "сужение не применилось — список отдал посторонние строки")
	})

	// И второй контроль: БЕЗ сужения видны все четыре. Иначе «нашлось ровно одно»
	// было бы истинно и на реализации, которая теряет строки по другой причине.
	t.Run("без сужения видны все", func(t *testing.T) {
		got, _, lerr := rd.Addresses().List(ctx,
			kacho.AddressFilter{ProjectID: proj},
			kacho.Pagination{PageSize: 10})
		require.NoError(t, lerr)
		require.Len(t, got, len(seed))
	})
}
