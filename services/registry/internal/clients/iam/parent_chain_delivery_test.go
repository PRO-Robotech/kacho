// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package iam

// parent_chain_delivery_test.go — регистрация ресурса registry называет цепь
// предков КАЖДОГО своего вида объекта, и повторная регистрация её не опустошает.
//
// # Почему проба нужна и здесь, у сервиса, который цепь уже слал
//
// Цепь слал ОДИН вид объекта — репозиторий, у которого иерархия глубже проекта
// (репозиторий → реестр → проект). Сам реестр регистрировался без цепи: его
// предок — проект, а он выражался отдельным полем области. С точки зрения
// принимающей стороны разницы нет: набор рёбер заменяется целиком, поэтому
// регистрация реестра стирала его рёбра ровно так же, как регистрация из
// четырёх остальных сервисов.
//
// Отсюда форма пробы: два вида объекта рядом. Глубокая цепь обязана остаться
// НЕТРОНУТОЙ (её из области не вывести), а объект, чей единственный предок —
// проект, обязан перестать молчать о нём.

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/registry/internal/domain"
)

func versioned(i domain.RegisterIntent, at time.Time) domain.RegisterIntent {
	i.SourceVersion = domain.SourceVersion{Time: at}
	return i
}

// TestRegisterDeliveryNamesParentChain_RegistryObject — у реестра предок один:
// проект. Он обязан быть назван цепью, а не только полем области.
func TestRegisterDeliveryNamesParentChain_RegistryObject(t *testing.T) {
	f := &scriptedRegisterClient{}
	apply := NewRegisterApplier(f)

	intent := versioned(domain.RegisterIntentForCreate(
		&domain.Registry{ID: "reg-aaaaaaaaaaaaaaaaa", ProjectID: "prj-prod000000000000"},
		"", "",
	), time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC))

	require.NoError(t, apply(context.Background(), domain.FGAEventRegister, intent))

	require.Len(t, f.registerReqs, 1)
	assert.Equal(t, []string{"project:prj-prod000000000000"}, f.registerReqs[0].GetParentChain(),
		"регистрация реестра не назвала предка: принимающая сторона заменяет набор "+
			"рёбер целиком, поэтому неназванная цепь стирает уже записанных предков")
}

// TestRegisterDeliveryKeepsDeepChainVerbatim — цепь, названную владельцем,
// доставка не подменяет выводом из области.
//
// Это положительный контроль к пробе выше: вывод из области знает только проект
// и аккаунт, и подмени он названную цепь — промежуточное звено (реестр над
// репозиторием) исчезло бы, а объект оказался бы подчинён проекту напрямую.
func TestRegisterDeliveryKeepsDeepChainVerbatim(t *testing.T) {
	f := &scriptedRegisterClient{}
	apply := NewRegisterApplier(f)

	intent := versioned(domain.RegisterIntentForRepoPush(
		"reg-aaaaaaaaaaaaaaaaa", "app/api", "prj-prod000000000000", "",
	), time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC))

	require.NoError(t, apply(context.Background(), domain.FGAEventRegister, intent))

	require.Len(t, f.registerReqs, 1)
	assert.Equal(t,
		[]string{"registry_registry:reg-aaaaaaaaaaaaaaaaa", "project:prj-prod000000000000"},
		f.registerReqs[0].GetParentChain(),
		"глубокая цепь подменена выводом из области — промежуточное звено потеряно")
}

// TestReRegistrationDoesNotEmptyParentChain — перерегистрация реестра (правка
// меток) несёт ту же непустую цепь.
func TestReRegistrationDoesNotEmptyParentChain(t *testing.T) {
	f := &scriptedRegisterClient{}
	apply := NewRegisterApplier(f)

	base := time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC)
	reg := &domain.Registry{
		ID:        "reg-aaaaaaaaaaaaaaaaa",
		ProjectID: "prj-prod000000000000",
		Labels:    map[string]string{"tier": "critical"},
	}
	require.NoError(t, apply(context.Background(), domain.FGAEventRegister,
		versioned(domain.RegisterIntentForCreate(reg, "", ""), base)))

	reg.Labels = map[string]string{"tier": "normal"}
	require.NoError(t, apply(context.Background(), domain.FGAEventRegister,
		versioned(domain.RegisterIntentForUpdate(reg), base.Add(time.Second))))

	require.Len(t, f.registerReqs, 2)
	assert.Equal(t, f.registerReqs[0].GetParentChain(), f.registerReqs[1].GetParentChain(),
		"перерегистрация изменила цепь предков")
	assert.NotEmpty(t, f.registerReqs[1].GetParentChain(),
		"вторая доставка пришла без предков — набор рёбер объекта будет опустошён, "+
			"и объект замолчит на каждом вопросе о доступе")
}
