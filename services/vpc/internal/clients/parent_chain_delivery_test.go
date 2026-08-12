// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package clients

// parent_chain_delivery_test.go — регистрация ресурса vpc называет цепь предков
// ОБОИМИ путями доставки, и повторная регистрация её не опустошает.
//
// # Предмет
//
// Принимающая сторона держит предков объекта отдельными рёбрами и заменяет их
// набор ЦЕЛИКОМ на каждой применённой регистрации. Доставка, не назвавшая цепь,
// поэтому не «ничего не меняет», а СТИРАЕТ уже записанных предков и не ставит
// новых. Дальше вопрос о доступе поднимается по цепи, цепи нет, ответ «нет» — и
// он неотличим от честного отказа.
//
// # Почему повтор — отдельный сценарий, а не следствие первого
//
// Первая доставка могла бы записать цепь и «доказать» свойство, а вторая —
// стереть её и остаться незамеченной: обе возвращают успех. Идемпотентность
// здесь измеряется НАБОРОМ, который останется после второй доставки, а не тем,
// что вторая не упала.

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/fgaregister"
)

// TestRegisterDeliveryNamesParentChain_Durable — путь очереди.
func TestRegisterDeliveryNamesParentChain_Durable(t *testing.T) {
	f := &fakeIAMRegisterClient{}
	apply := NewIAMRegisterApplier(f)

	p := fgaregister.Payload{
		Tuple:           fgaregister.ProjectHierarchy("prj-P", "vpc_subnet", "sub-1"),
		ParentProjectID: "prj-P",
		SourceVersion:   time.Now().UTC(),
	}
	require.NoError(t, apply(context.Background(), fgaregister.EventRegister, p))

	require.Len(t, f.registerCalls, 1)
	assert.Equal(t, []string{"project:prj-P"}, f.registerCalls[0].GetParentChain(),
		"регистрация подсети не назвала предка: принимающая сторона заменяет набор "+
			"рёбер целиком, поэтому неназванная цепь стирает уже записанных предков")
}

// TestRegisterDeliveryNamesParentChain_Sync — путь синхронной доставки.
func TestRegisterDeliveryNamesParentChain_Sync(t *testing.T) {
	f := &fakeIAMRegisterClient{}
	reg, err := NewSyncRegistrar(f)
	require.NoError(t, err)

	require.NoError(t, reg.Register(context.Background(), []fgaregister.Item{
		fgaregister.ProjectHierarchyItem("prj-P", "vpc_network", "net-1", nil),
	}, time.Now()))

	require.Len(t, f.registerCalls, 1)
	assert.Equal(t, []string{"project:prj-P"}, f.registerCalls[0].GetParentChain())
}

// TestReRegistrationDoesNotEmptyParentChain — ПОВТОРНАЯ регистрация того же
// объекта несёт ту же непустую цепь.
//
// Это и есть суть дефекта: правка меток перерегистрирует объект, и вторая
// доставка с пустой цепью снимала бы предков, записанных первой.
func TestReRegistrationDoesNotEmptyParentChain(t *testing.T) {
	f := &fakeIAMRegisterClient{}
	apply := NewIAMRegisterApplier(f)

	first := fgaregister.Payload{
		Tuple:           fgaregister.ProjectHierarchy("prj-P", "vpc_network", "net-1"),
		Labels:          map[string]string{"env": "prod"},
		ParentProjectID: "prj-P",
		SourceVersion:   time.Now().UTC(),
	}
	second := first
	second.Labels = map[string]string{"env": "stage"}
	second.SourceVersion = first.SourceVersion.Add(time.Second)

	require.NoError(t, apply(context.Background(), fgaregister.EventRegister, first))
	require.NoError(t, apply(context.Background(), fgaregister.EventRegister, second))

	require.Len(t, f.registerCalls, 2)
	assert.Equal(t, f.registerCalls[0].GetParentChain(), f.registerCalls[1].GetParentChain(),
		"перерегистрация изменила цепь предков")
	assert.NotEmpty(t, f.registerCalls[1].GetParentChain(),
		"вторая доставка того же объекта пришла без предков — набор рёбер объекта "+
			"будет опустошён, и объект замолчит на каждом вопросе о доступе")
}
