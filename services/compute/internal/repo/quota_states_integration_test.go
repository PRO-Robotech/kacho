// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

//go:build integration

package repo

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/pkg/quota/quotaread"
)

// Чтение строк учёта — общий оператор поверх схемы ЭТОГО владельца.
//
// ПОЧЕМУ ЭТО ИНТЕГРАЦИОННАЯ ПРОБА, А НЕ ЮНИТ. Предмет здесь ровно один и он
// свойство ДЕРЕВА, а не кода: попадает ли общий оператор в ту таблицу, в которую
// пишет продукт. Имя схемы у этого владельца — `public` (миграции создают таблицы
// без квалификации), и ошибиться в нём нельзя ни компилятором, ни подставным
// носителем: оператор соберётся, выполнится и не найдёт ничего — то есть ответит
// «строк учёта нет», неотличимо от свежего проекта. Арендатор увидел бы полный
// набор с нулевым потреблением на проекте, где ресурсы есть.
func TestQuotaStates_ReadTheSameTableTheProductWritesTo(t *testing.T) {
	pool := auditTestPool(t)
	ctx := context.Background()

	const project = "prj-states-compute"
	quotaSeed(t, pool, project, "compute.instance", 32)
	quotaSeed(t, pool, project, "compute.guestAccessKey", 10)

	repo := NewQuotaRepo(pool)

	states, err := repo.ListStates(ctx, quotaread.CarrierProject, project)
	require.NoError(t, err)
	require.Len(t, states, 2,
		"общий оператор не нашёл строк, которые фикстура завела тем же путём, что продукт — "+
			"почти наверняка он читает ЧУЖУЮ схему, и это неотличимо от свежего проекта")

	// Порядок — по КОЛОНКЕ ПОРЯДКА, а не по времени вставки: строки заводит одна
	// транзакция, метки времени у них совпадают, и сортировка по ним разрешалась
	// бы идентификатором, то есть случайной строкой.
	require.Equal(t, "compute.guestAccessKey", states[0].Kind)
	require.Equal(t, "compute.instance", states[1].Kind)

	require.EqualValues(t, 32, states[1].Limit)
	require.Equal(t, "DEFAULT", states[1].SourceScope)
	require.Equal(t, quotaread.CarrierProject, states[1].CarrierType)
	require.Equal(t, project, states[1].CarrierID)

	// Отрицание рядом с положительным: чужой носитель отдаёт ПУСТО. Без него
	// «нашлись строки» зеленело бы и на операторе, который игнорирует адресацию.
	other, err := repo.ListStates(ctx, quotaread.CarrierProject, project+"-other")
	require.NoError(t, err)
	require.Empty(t, other, "оператор отдал строки чужого носителя: адресация не применяется")
}

// Потребление, записанное ТРИГГЕРОМ, доезжает до чтения.
//
// Отдельная проба потому, что предмет другой: выше проверялось, что читается та
// же таблица; здесь — что читается тот же СТОЛБЕЦ, который двигает списание.
// Оператор, выбирающий не то поле, ответил бы нулём на живом проекте, и
// арендатор прочитал бы «ничего не создано».
func TestQuotaStates_ShowTheUsageTheTriggerWrote(t *testing.T) {
	pool := auditTestPool(t)
	ctx := context.Background()

	const project = "prj-states-usage"
	quotaSeed(t, pool, project, "compute.placementGroup", 16)

	// Вставка идёт в ТУ ЖЕ таблицу, в которую пишет продукт: списание держится
	// триггером, то есть свойством связки, а не вызовом.
	_, err := pool.Exec(ctx,
		`INSERT INTO placement_groups (id, project_id, name, strategy, placement_type, zone_id)
		 VALUES ($1, $2, 'pg-states', 'SPREAD', 'ZONAL', 'ru-central1-a')`,
		plgID(9101), project)
	require.NoError(t, err)

	states, err := NewQuotaRepo(pool).ListStates(ctx, quotaread.CarrierProject, project)
	require.NoError(t, err)
	require.Len(t, states, 1)
	require.EqualValues(t, 1, states[0].Used,
		"потребление, записанное триггером, до чтения не доехало — читается не тот столбец")
	require.EqualValues(t, quotaUsed(t, pool, project, "compute.placementGroup"), states[0].Used)
}
