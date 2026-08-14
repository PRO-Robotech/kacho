// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

//go:build integration

package repo

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	computev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/compute/v1"
)

// TestObservedState_ContractAndSchemaAgreeBothWays — множество наблюдаемых
// состояний в контракте и в ограничении схемы совпадает ПОЭЛЕМЕНТНО.
//
// # Почему в обе стороны, а не «схема принимает всё, что шлёт код»
//
// Одностороннее утверждение пропускает оба реальных расхождения, и они разные:
//
//   - значение есть в коде, нет в схеме — отчёт агента упрётся в отказ
//     хранилища без имени поля: сигнал, по которому нельзя понять, что не так;
//   - значение есть в схеме, нет в коде — наблюдаемое становится свободной
//     строкой, которую никто не сможет ни сгруппировать, ни отфильтровать, и
//     первый же чужой отчёт запишет в него что угодно.
//
// Поэтому сравниваются МНОЖЕСТВА, а не вхождение одного в другое.
//
// # Почему проба читает схему живой базы, а не текст миграции
//
// Текст миграции — намерение; ограничение живой базы — то, что действительно
// применится. Между ними помещается опечатка, откат и миграция, которую забыли
// применить.
func TestObservedState_ContractAndSchemaAgreeBothWays(t *testing.T) {
	pool := auditTestPool(t)
	ctx := context.Background()

	// Контракт: все значения перечисления, кроме нулевого. Нулевое означает
	// «не задано» и в колонку не пишется — его отсутствие в схеме законно.
	var fromContract []string
	for v, name := range computev1.ObservedState_name {
		if v == 0 {
			continue
		}
		fromContract = append(fromContract, name)
	}
	sort.Strings(fromContract)
	require.NotEmpty(t, fromContract, "перечисление контракта пусто — проба не читает то, что думает")

	// Схема: значения из текста ограничения живой базы.
	var clause string
	err := pool.QueryRow(ctx, `
		SELECT pg_get_constraintdef(oid)
		  FROM pg_constraint
		 WHERE conname = 'instances_observed_state_check'`).Scan(&clause)
	require.NoError(t, err, "ограничение наблюдаемого состояния отсутствует в схеме")

	var fromSchema []string
	for _, name := range computev1.ObservedState_name {
		if name != "OBSERVED_STATE_UNSPECIFIED" && strings.Contains(clause, "'"+name+"'") {
			fromSchema = append(fromSchema, name)
		}
	}
	// Отдельно: в ограничении не должно быть значений СВЕРХ контракта. Считаем
	// кавычки-литералы и сверяем количество — если схема несёт лишнее, число
	// разойдётся с длиной множества контракта.
	literals := strings.Count(clause, "'::text") // Postgres печатает литералы с приведением типа
	if literals == 0 {
		literals = strings.Count(clause, "'") / 2
	}
	sort.Strings(fromSchema)

	require.Equal(t, fromContract, fromSchema,
		"значение контракта без места в схеме даёт отказ хранилища без имени поля")
	require.Equal(t, len(fromContract), literals,
		"значение схемы без места в контракте делает наблюдаемое свободной строкой")
}

// TestObservedState_PartialTripleIsRejected — три поля наблюдения заполняются
// вместе либо не заполняются вовсе.
//
// Состояние без номера события не с чем сравнить на свежесть; номер без
// состояния ничего не описывает. Частично заполненная тройка выглядит
// осведомлённой и не является ею — поэтому свойство держит база, а не проверка
// в коде: проверка защищает только тот путь, который через неё проходит.
func TestObservedState_PartialTripleIsRejected(t *testing.T) {
	pool := auditTestPool(t)
	ctx := context.Background()

	seed := func(t *testing.T, id string) {
		t.Helper()
		_, err := pool.Exec(ctx, `INSERT INTO instances
			(id, project_id, name, zone_id, status, instance_kind, bs_type, bs_id)
			VALUES ($1, 'prj-obs', $1, 'ru-central1-a', 'PROVISIONING', 1, 'storage.image', 'img-x')`, id)
		require.NoError(t, err)
	}

	// (−) половина тройки отвергается
	seed(t, "ins-obs-partial")
	_, err := pool.Exec(ctx,
		`UPDATE instances SET observed_state = 'OBSERVED_RUNNING' WHERE id = 'ins-obs-partial'`)
	require.Error(t, err, "состояние без номера события и времени обязано отвергаться")

	// (+) полная тройка проходит — иначе отрицание выше зеленело бы на любом
	// сломанном обновлении
	seed(t, "ins-obs-full")
	_, err = pool.Exec(ctx, `UPDATE instances
		   SET observed_state = 'OBSERVED_RUNNING',
		       observed_sequence_no = 42,
		       observed_at = now()
		 WHERE id = 'ins-obs-full'`)
	require.NoError(t, err, "полная тройка обязана приниматься")
}
