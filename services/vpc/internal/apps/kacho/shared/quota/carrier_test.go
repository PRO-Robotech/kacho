// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package quota_test

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/kachomock"
)

// Строка учёта ложится под тот носитель, который НАЗВАЛ владелец величин.
//
// Задача `PRO-Robotech/kacho#401`.
//
// # Предмет
//
// Владелец величин отдаёт домену vpc двенадцать видов; четыре из них считаются
// в родительском РЕСУРСЕ, а не в проекте. Владелец типа проставлял всем
// двенадцати носителем проект — константой, потому что узнать носитель было
// неоткуда. Отсюда две неправды арендатору сразу: носитель назван неверно, а
// потребление таких строк не наполнится НИКОГДА, потому что списание идёт по
// настоящему носителю.
//
// # Почему предмет закрывается здесь, а не сужением ответа резолва
//
// Сузить ответ владельца величин — значит решить за ВСЕХ потребителей. У nlb и
// registry вложенные виды реализованы: они кладут их в свою таблицу умолчаний
// родителя, и сужение отняло бы у них работающий механизм. Носитель едет по
// проводу, и каждый потребитель раскладывает по нему сам — это и есть разница
// между «резолв знает про всех» и «каждый знает про себя».

// nestedKindTokens — вложенные виды домена, выписанные для утверждений.
var nestedKindTokens = []string{
	"vpc.network.subnet",
	"vpc.network.routeTable",
	"vpc.network.securityGroup",
	"vpc.subnet.networkInterface",
}

// TestMaterialise_NestedKindsDoNotBecomeProjectRows — вид, считаемый в родителе,
// не заводится строкой проекта.
func TestMaterialise_NestedKindsDoNotBecomeProjectRows(t *testing.T) {
	ctx := context.Background()
	repoMock := kachomock.NewRepository()
	res := &fakeResolver{limits: twelveKindsWithNested()}
	acc := &fakeAccounts{account: "acc-1"}

	require.NoError(t, newGuard(t, repoMock, res, acc).Admit(ctx, "prj-1", "vpc.network"))

	rows := repoMock.QuotaRows()
	// Положительный контроль: плоские виды ЗАВЕДЕНЫ. Без него проба зеленела бы
	// и на реализации, не заводящей ничего вовсе.
	require.Len(t, rows, 8, "заведены ровно те виды, что считаются в проекте")
	require.Contains(t, rows, "project/prj-1/vpc.gateway")

	for _, k := range nestedKindTokens {
		require.NotContains(t, rows, "project/prj-1/"+k,
			"вид %s считается в родительском ресурсе — строкой проекта он быть не может", k)
	}

	// И ни одна заведённая строка не носит чужого носителя: у владельца типа
	// сегодня есть только проектная полоса.
	for key := range rows {
		require.True(t, strings.HasPrefix(key, "project/prj-1/"),
			"строка %s заведена под носителем, которого у этого владельца нет", key)
	}
}

// TestStates_NestedKindsAreNotShownAsProjectQuotas — арендатор, спросивший про
// ПРОЕКТ, не получает видов, считаемых в родителе.
//
// У такого вида единственного значения на уровне проекта нет by construction:
// подсетей столько-то в КАЖДОЙ сети. Строка с носителем «проект» была бы ответом
// на вопрос, которого никто не задавал.
func TestStates_NestedKindsAreNotShownAsProjectQuotas(t *testing.T) {
	ctx := context.Background()
	repoMock := kachomock.NewRepository()
	res := &fakeResolver{limits: twelveKindsWithNested()}
	acc := &fakeAccounts{account: "acc-1"}

	states, err := newGuard(t, repoMock, res, acc).States(ctx, "prj-1")
	require.NoError(t, err)

	require.Len(t, states, 8, "восемь видов проекта — и ни одного чужого носителя")
	for _, st := range states {
		require.Equal(t, "project", st.CarrierType,
			"каждая отданная строка обязана называть носителем то, чем она и считается")
		require.NotContains(t, nestedKindTokens, st.Kind,
			"вложенный вид на этой полосе не отвечается")
	}
}

// TestStates_CompositionIsTheSameOnBothReadingLanes — состав ответа не зависит
// от того, создавал ли проект хоть один ресурс.
//
// Ровно признак из задачи: свежий проект читался одним набором,
// материализованный — другим, и состав менялся без единого действия арендатора
// над квотами.
func TestStates_CompositionIsTheSameOnBothReadingLanes(t *testing.T) {
	ctx := context.Background()

	// Полоса 1: строк учёта ещё нет — набор собирается резолвом.
	fresh := kachomock.NewRepository()
	freshStates, err := newGuard(t, fresh,
		&fakeResolver{limits: twelveKindsWithNested()},
		&fakeAccounts{account: "acc-1"}).States(ctx, "prj-1")
	require.NoError(t, err)

	// Полоса 2: тот же проект после материализации — набор читается из базы.
	warm := kachomock.NewRepository()
	g2 := newGuard(t, warm,
		&fakeResolver{limits: twelveKindsWithNested()},
		&fakeAccounts{account: "acc-1"})
	require.NoError(t, g2.Admit(ctx, "prj-1", "vpc.network"))
	warmStates, err := g2.States(ctx, "prj-1")
	require.NoError(t, err)

	require.NotEmpty(t, kindSet(freshStates),
		"иначе равенство ниже было бы равенством двух пустот")
	require.Equal(t, kindSet(freshStates), kindSet(warmStates),
		"состав ответа обязан совпадать на обеих полосах чтения")
}

func kindSet(states []kacho.QuotaState) []string {
	out := make([]string, 0, len(states))
	for _, st := range states {
		out = append(out, st.Kind)
	}
	sort.Strings(out)
	return out
}
