// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package clients

// parent_chain_delivery_test.go — регистрация ресурса storage называет цепь
// предков, и повторная регистрация её не опустошает.
//
// Предмет тот же, что у одноимённой пробы каждого потребителя: принимающая
// сторона заменяет набор рёбер объекта ЦЕЛИКОМ, поэтому доставка без цепи не
// «ничего не меняет», а стирает уже записанных предков и не ставит новых.

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/storage/internal/fgaregister"
)

func storagePayload(version time.Time, labels map[string]string) fgaregister.Payload {
	return fgaregister.Payload{
		Tuple:           fgaregister.StorageVolume("prj-prod000000000000", "vol-aaaaaaaaaaaaaaaaa"),
		Labels:          labels,
		ParentProjectID: "prj-prod000000000000",
		SourceVersion:   version,
	}
}

// TestRegisterDeliveryNamesParentChain_Durable — путь очереди.
func TestRegisterDeliveryNamesParentChain_Durable(t *testing.T) {
	f := &fakeIAMRegister{}
	apply := NewIAMRegisterApplier(f)

	require.NoError(t, apply(context.Background(), fgaregister.EventRegister,
		storagePayload(time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC), nil)))

	require.Len(t, f.registerCalls, 1)
	assert.Equal(t, []string{"project:prj-prod000000000000"}, f.registerCalls[0].GetParentChain(),
		"регистрация тома не назвала предка: принимающая сторона заменяет набор "+
			"рёбер целиком, поэтому неназванная цепь стирает уже записанных предков")
}

// TestReRegistrationDoesNotEmptyParentChain — вторая доставка того же объекта
// (правка меток) несёт ту же непустую цепь.
func TestReRegistrationDoesNotEmptyParentChain(t *testing.T) {
	f := &fakeIAMRegister{}
	apply := NewIAMRegisterApplier(f)

	base := time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC)
	require.NoError(t, apply(context.Background(), fgaregister.EventRegister,
		storagePayload(base, map[string]string{"tier": "critical"})))
	require.NoError(t, apply(context.Background(), fgaregister.EventRegister,
		storagePayload(base.Add(time.Second), map[string]string{"tier": "normal"})))

	require.Len(t, f.registerCalls, 2)
	assert.Equal(t, f.registerCalls[0].GetParentChain(), f.registerCalls[1].GetParentChain(),
		"перерегистрация изменила цепь предков")
	assert.NotEmpty(t, f.registerCalls[1].GetParentChain(),
		"вторая доставка пришла без предков — набор рёбер объекта будет опустошён, "+
			"и объект замолчит на каждом вопросе о доступе")
}
