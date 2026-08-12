// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package iam_test

// parent_chain_delivery_test.go — регистрация ресурса nlb называет цепь предков
// обоими путями доставки, и повторная регистрация её не опустошает.
//
// Предмет тот же, что у одноимённой пробы каждого потребителя: принимающая
// сторона заменяет набор рёбер объекта ЦЕЛИКОМ, поэтому доставка без цепи не
// «ничего не меняет», а стирает уже записанных предков. Проба стоит у КАЖДОГО
// потребителя отдельно, потому что цепь собирает он сам: свойство одного
// сервиса о четырёх остальных не утверждает ничего.

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/clients/iam"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

func chainIntent(version time.Time, labels map[string]string) domain.FGARegisterIntent {
	return domain.FGARegisterIntent{
		Kind:            "TargetGroup",
		ResourceID:      "tgr-aaaaaaaaaaaaaaaaa",
		Labels:          labels,
		ParentProjectID: "prj-prod000000000000",
		ParentAccountID: "acc-aaaaaaaaaaaaaaaa",
		SourceVersion:   version,
		Tuples: []domain.FGATuple{
			domain.FGAProjectTuple(domain.FGAObjectTypeTargetGroup, "tgr-aaaaaaaaaaaaaaaaa", "prj-prod000000000000"),
		},
	}
}

// TestRegisterDeliveryNamesParentChain_Durable — путь очереди.
func TestRegisterDeliveryNamesParentChain_Durable(t *testing.T) {
	rec := &recordingRegisterClient{}
	apply := iam.NewRegisterApplier(rec)

	require.NoError(t, apply(context.Background(), domain.FGAEventRegister,
		chainIntent(time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC), nil)))

	require.Len(t, rec.register, 1)
	assert.Equal(t,
		[]string{"project:prj-prod000000000000", "account:acc-aaaaaaaaaaaaaaaa"},
		rec.register[0].GetParentChain(),
		"регистрация не назвала предков: цепь идёт от ближайшего к дальнему, и "+
			"неназванная стирает уже записанных")
}

// TestRegisterDeliveryNamesParentChain_Sync — путь синхронной доставки.
func TestRegisterDeliveryNamesParentChain_Sync(t *testing.T) {
	rec := &scriptedRegisterClient{}
	reg, err := iam.NewSyncRegistrar(rec)
	require.NoError(t, err)

	require.NoError(t, reg.Register(context.Background(),
		chainIntent(time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC), nil),
		time.Date(2026, 8, 12, 10, 0, 1, 0, time.UTC)))

	got := rec.calls()
	require.Len(t, got, 1)
	assert.Equal(t,
		[]string{"project:prj-prod000000000000", "account:acc-aaaaaaaaaaaaaaaa"},
		got[0].GetParentChain())
}

// TestReRegistrationDoesNotEmptyParentChain — вторая доставка того же объекта
// (правка меток) несёт ту же непустую цепь.
func TestReRegistrationDoesNotEmptyParentChain(t *testing.T) {
	rec := &recordingRegisterClient{}
	apply := iam.NewRegisterApplier(rec)

	base := time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC)
	require.NoError(t, apply(context.Background(), domain.FGAEventRegister,
		chainIntent(base, map[string]string{"tier": "critical"})))
	require.NoError(t, apply(context.Background(), domain.FGAEventRegister,
		chainIntent(base.Add(time.Second), map[string]string{"tier": "normal"})))

	require.Len(t, rec.register, 2)
	assert.Equal(t, rec.register[0].GetParentChain(), rec.register[1].GetParentChain(),
		"перерегистрация изменила цепь предков")
	assert.NotEmpty(t, rec.register[1].GetParentChain(),
		"вторая доставка пришла без предков — набор рёбер объекта будет опустошён, "+
			"и объект замолчит на каждом вопросе о доступе")
}
