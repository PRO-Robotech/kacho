// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package quota_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/kachomock"
)

// Чтение квот арендатором — задача #365, сценарии приёмки QV2-17, QV2-18, QV2-21
// (`docs/specs/sub-phase-quota-v2-materialised-usage-acceptance.md`, APPROVED).
//
// Предмет этих проб — НЕ «метод отвечает», а два свойства, которые ломаются тихо:
// ответ не бывает пустым по умолчанию, и источник победившей величины различим.

// TestStates_FreshProjectReadsFullSetNotEmptyArray — проект, ещё ничего не
// создавший, читается ПОЛНЫМ набором видов с нулевым потреблением.
//
// Пустой массив здесь был бы утверждением, которого владелец не делал: арендатор
// прочитал бы «квот нет» и заключил, что он безлимитен. Строк учёта у свежего
// проекта нет by construction — их заводит первая мутация, — поэтому полный набор
// собирается резолвом у владельца величин синхронно, БЕЗ записи: чтение остаётся
// чтением (`api-conventions.md` §«Пустое значение обязано означать „пусто“»).
func TestStates_FreshProjectReadsFullSetNotEmptyArray(t *testing.T) {
	t.Parallel()

	repo := kachomock.NewRepository()
	res := &fakeResolver{limits: eightKinds()}
	acc := &fakeAccounts{account: "acc-1"}
	g := newGuard(t, repo, res, acc)

	states, err := g.States(context.Background(), "prj-fresh")
	require.NoError(t, err)

	require.Len(t, states, 8, "свежий проект обязан читаться полным набором видов домена, а не пустым массивом")
	for _, st := range states {
		require.Zero(t, st.Used, "у проекта без ресурсов потребление нулевое: %s", st.Kind)
		require.NotZero(t, st.Limit, "непустой предел обязан приехать резолвом: %s", st.Kind)
		require.Equal(t, "project", st.CarrierType)
		require.Equal(t, "prj-fresh", st.CarrierID)
	}

	// Чтение НЕ записывает: строк учёта после него по-прежнему ноль.
	require.Empty(t, repo.QuotaRows(),
		"чтение квот завело строки учёта — тогда это уже не чтение")
}

// TestStates_ReadsMaterialisedRowsWithoutAskingThePeer — когда строки учёта уже
// есть, ответ берётся локально и сосед не спрашивается.
//
// Утверждается ИСХОД (число обращений к владельцу величин), а не факт вызова:
// «мы позвали резолв» осталось бы зелёным и при лишнем обращении на каждое
// чтение, а именно оно и есть цена, которую правило запрещает платить молча.
func TestStates_ReadsMaterialisedRowsWithoutAskingThePeer(t *testing.T) {
	t.Parallel()

	repo := kachomock.NewRepository()
	res := &fakeResolver{limits: eightKinds()}
	acc := &fakeAccounts{account: "acc-1"}
	g := newGuard(t, repo, res, acc)

	// Первое обращение материализует строки — это живой путь, а не посев литералом.
	require.NoError(t, g.Admit(context.Background(), "prj-1", "vpc.network"))
	callsAfterMaterialise := res.callCount

	states, err := g.States(context.Background(), "prj-1")
	require.NoError(t, err)
	require.Len(t, states, 8)
	require.Equal(t, callsAfterMaterialise, res.callCount,
		"чтение уже материализованного проекта обратилось к владельцу величин — это лишний вызов на каждое чтение")
}

// TestStates_OrderIsByKindNotByInsertion — порядок ответа задан видом.
//
// Строки заводит материализация ОДНОЙ транзакцией, поэтому метка времени у них
// совпадает и сортировка по ней разрешалась бы идентификатором, то есть
// случайной строкой. Клиент, ведущий состояние по индексу, прочитал бы
// перестановку как изменение (`api-conventions.md` §«Порядок повторяющегося
// поля»). Проба на трёх и более элементах — на одном она не различает ничего.
func TestStates_OrderIsByKindNotByInsertion(t *testing.T) {
	t.Parallel()

	repo := kachomock.NewRepository()
	res := &fakeResolver{limits: eightKinds()}
	acc := &fakeAccounts{account: "acc-1"}
	g := newGuard(t, repo, res, acc)
	require.NoError(t, g.Admit(context.Background(), "prj-1", "vpc.network"))

	states, err := g.States(context.Background(), "prj-1")
	require.NoError(t, err)
	require.Greater(t, len(states), 2, "порядок проверяется на трёх и более элементах")

	for i := 1; i < len(states); i++ {
		require.Less(t, states[i-1].Kind, states[i].Kind,
			"виды обязаны идти по возрастанию: %s перед %s", states[i-1].Kind, states[i].Kind)
	}
}

// TestStates_SourceScopeDistinguishesOverrideFromDefault — источник победившей
// величины различим (QV2-21).
//
// Без него «вы на 16 из 16» не говорит арендатору, чей это предел — его
// собственный, аккаунта или платформенный, — а значит не говорит и к кому идти
// его поднимать.
func TestStates_SourceScopeDistinguishesOverrideFromDefault(t *testing.T) {
	t.Parallel()

	limits := eightKinds()
	limits[0].SourceScope, limits[0].SourceScopeID = "PROJECT", "prj-1"
	limits[1].SourceScope, limits[1].SourceScopeID = "ACCOUNT", "acc-1"

	repo := kachomock.NewRepository()
	res := &fakeResolver{limits: limits}
	acc := &fakeAccounts{account: "acc-1"}
	g := newGuard(t, repo, res, acc)
	require.NoError(t, g.Admit(context.Background(), "prj-1", "vpc.network"))

	states, err := g.States(context.Background(), "prj-1")
	require.NoError(t, err)

	byKind := make(map[string]struct{ scope, scopeID string }, len(states))
	for _, st := range states {
		byKind[st.Kind] = struct{ scope, scopeID string }{st.SourceScope, st.SourceScopeID}
	}

	require.Equal(t, "PROJECT", byKind[limits[0].Kind].scope, "личное перекрытие обязано быть названо")
	require.Equal(t, "prj-1", byKind[limits[0].Kind].scopeID)
	require.Equal(t, "ACCOUNT", byKind[limits[1].Kind].scope, "аккаунтное перекрытие обязано быть названо")
	require.Equal(t, "acc-1", byKind[limits[1].Kind].scopeID)

	// Положительный контроль на третью область: остальные виды несут умолчание с
	// ПУСТЫМ объектом области. Без него проба зеленела бы на реализации,
	// проставляющей PROJECT всем подряд.
	require.Equal(t, "DEFAULT", byKind[limits[2].Kind].scope)
	require.Empty(t, byKind[limits[2].Kind].scopeID,
		"у умолчания объекта области нет, и пустота здесь — часть контракта")
}

// TestStates_PeerUnavailableIsRefusalNotEmptyAnswer — недоступность владельца
// величин на свежем проекте есть ОТКАЗ, а не пустой ответ.
//
// Иначе кратковременная недоступность соседа читалась бы арендатором как «квот
// нет» — то есть как «предела нет», и это худшее из возможных прочтений.
func TestStates_PeerUnavailableIsRefusalNotEmptyAnswer(t *testing.T) {
	t.Parallel()

	repo := kachomock.NewRepository()
	res := &fakeResolver{err: context.DeadlineExceeded}
	acc := &fakeAccounts{account: "acc-1"}
	g := newGuard(t, repo, res, acc)

	states, err := g.States(context.Background(), "prj-fresh")
	require.Error(t, err, "недоступность владельца величин обязана быть отказом")
	require.Empty(t, states, "при отказе набор не отдаётся частично")
}
