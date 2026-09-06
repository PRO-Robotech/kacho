// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package clients_test

// parent_chain_delivery_test.go — регистрация ресурса compute называет цепь
// предков, и повторная регистрация её не опустошает.
//
// Предмет тот же, что у одноимённой пробы каждого потребителя: принимающая
// сторона заменяет набор рёбер объекта ЦЕЛИКОМ, поэтому доставка без цепи не
// «ничего не меняет», а стирает уже записанных предков и не ставит новых. Проба
// стоит у КАЖДОГО потребителя отдельно, потому что цепь собирает он сам:
// свойство одного сервиса о четырёх остальных не утверждает ничего.

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kaname/cloud/iam/v1"

	"github.com/PRO-Robotech/kacho/services/compute/internal/clients"
	"github.com/PRO-Robotech/kacho/services/compute/internal/fgaintent"
)

// recordingRegisterClient — двойник, записывающий каждый запрос регистрации.
type recordingRegisterClient struct {
	register []*iamv1.RegisterResourceRequest
}

func (c *recordingRegisterClient) RegisterResource(
	_ context.Context, in *iamv1.RegisterResourceRequest, _ ...grpc.CallOption,
) (*iamv1.RegisterResourceResponse, error) {
	c.register = append(c.register, in)
	return &iamv1.RegisterResourceResponse{}, nil
}

func (c *recordingRegisterClient) UnregisterResource(
	_ context.Context, _ *iamv1.UnregisterResourceRequest, _ ...grpc.CallOption,
) (*iamv1.UnregisterResourceResponse, error) {
	return &iamv1.UnregisterResourceResponse{}, nil
}

func computeIntent(version time.Time, labels map[string]string) fgaintent.Payload {
	tuple, ok := fgaintent.ProjectHierarchyTuple("Instance", "ins-aaaaaaaaaaaaaaaaa", "prj-prod000000000000")
	if !ok {
		panic("проба построена на неотображаемом виде ресурса — предмет пробы отсутствует")
	}
	return fgaintent.Payload{
		Tuples:          []fgaintent.Tuple{tuple},
		Labels:          labels,
		ParentProjectID: "prj-prod000000000000",
		SourceVersion:   version,
	}
}

// TestRegisterDeliveryNamesParentChain_Durable — путь очереди.
func TestRegisterDeliveryNamesParentChain_Durable(t *testing.T) {
	rec := &recordingRegisterClient{}
	applier := clients.NewIAMRegisterApplierWithClient(rec)

	require.NoError(t, applier.Apply(context.Background(), fgaintent.EventRegister,
		computeIntent(time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC), nil)))

	require.Len(t, rec.register, 1)
	assert.Equal(t, []string{"project:prj-prod000000000000"}, rec.register[0].GetParentChain(),
		"регистрация машины не назвала предка: принимающая сторона заменяет набор "+
			"рёбер целиком, поэтому неназванная цепь стирает уже записанных предков")
}

// TestReRegistrationDoesNotEmptyParentChain — вторая доставка того же объекта
// (правка меток) несёт ту же непустую цепь.
func TestReRegistrationDoesNotEmptyParentChain(t *testing.T) {
	rec := &recordingRegisterClient{}
	applier := clients.NewIAMRegisterApplierWithClient(rec)

	base := time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC)
	require.NoError(t, applier.Apply(context.Background(), fgaintent.EventRegister,
		computeIntent(base, map[string]string{"tier": "critical"})))
	require.NoError(t, applier.Apply(context.Background(), fgaintent.EventRegister,
		computeIntent(base.Add(time.Second), map[string]string{"tier": "normal"})))

	require.Len(t, rec.register, 2)
	assert.Equal(t, rec.register[0].GetParentChain(), rec.register[1].GetParentChain(),
		"перерегистрация изменила цепь предков")
	assert.NotEmpty(t, rec.register[1].GetParentChain(),
		"вторая доставка пришла без предков — набор рёбер объекта будет опустошён, "+
			"и объект замолчит на каждом вопросе о доступе")
}
