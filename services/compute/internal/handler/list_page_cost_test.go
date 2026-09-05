// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package handler

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	computev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/compute/v1"
	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"

	"github.com/PRO-Robotech/kacho/services/compute/internal/apps/kacho/api/instance"
	"github.com/PRO-Robotech/kacho/services/compute/internal/domain"
	"github.com/PRO-Robotech/kacho/services/compute/internal/ports/portmock"
)

// ─────────────────────────────────────────────────────────────────────────────
// Видимость страницы спрашивается ПО СТРАНИЦЕ, а не перечислением типа.
//
// # Что здесь было и что из этого пережило снятие внешнего движка
//
// Прежняя редакция моделировала дефект чужого хранилища: перечисление объектов
// имело жёсткий серверный предел и не имело продолжения, поэтому ресурс сверх
// предела становился владельцу невидим навсегда при живых правах. Наблюдалось это
// счётчиком вызовов перечисления.
//
// Перечисления больше НЕТ: порт сужения списка (`pkg/listnarrow.AuthorizeClient`)
// несёт один метод — пообъектный вопрос о странице. Значит и счётчик вызовов
// перечисления остался бы БЕЗ ПРОИЗВОДИТЕЛЯ: утверждение «перечисления не было»
// стало бы истинным по построению типа и не могло бы покраснеть ни на чём. Такая
// проба хуже отсутствующей — она занимает место и отчитывается зелёным.
//
// # Что утверждается вместо
//
// Живое свойство то же самое, но меряется тем, у чего производитель ЕСТЬ:
// СТОИМОСТЬЮ. Число пообъектных вопросов обязано быть ограничено СТРАНИЦЕЙ, а не
// населением типа. Фикстура намеренно держит тысячу выданных идентификаторов при
// одной строке в проекте — ровно та форма, на которой перечисление стоило бы
// тысячу, а вопрос по странице стоит один.
//
// # Почему один источник истины у обоих ответов
//
// Дублёр отвечает из ОДНОГО набора выданных идентификаторов, поэтому проба не
// может пройти за счёт ослабления авторизации: идентификатор вне набора отвергнут
// на всех путях сразу.
// ─────────────────────────────────────────────────────────────────────────────

// pageCostAuthorizeClient — дублёр kaname AuthorizeService.
//
// ДУБЛЁР НЕ СНИСХОДИТЕЛЬНЕЕ НАСТОЯЩЕГО: предел размера партии он проверяет тем же
// отказом, что и служба. Дублёр, молча принимающий партию, которую настоящий
// отвергает, сделал бы невидимым именно тот дефект, ради которого его подставляют.
type pageCostAuthorizeClient struct {
	// granted — авторитетная истина: идентификаторы, которые субъекту действительно
	// видны.
	granted map[string]bool
	// batchCheckedIDs — наблюдаемая СТОИМОСТЬ: сколько пообъектных вопросов задано.
	batchCheckedIDs atomic.Int64
}

func newPageCostAuthorizeClient(granted ...string) *pageCostAuthorizeClient {
	g := make(map[string]bool, len(granted))
	for _, id := range granted {
		g[id] = true
	}
	return &pageCostAuthorizeClient{granted: g}
}

// BatchCheck отвечает по каждому (субъект, объект) ПРЯМО из того же набора —
// без перечисления. Порядок сохраняется, как требует контракт RPC: верный, но
// переставленный вердикт отфильтровал бы страницу чужим ответом.
func (c *pageCostAuthorizeClient) BatchCheck(_ context.Context, in *iamv1.BatchAuthorizeCheckRequest,
	_ ...grpc.CallOption) (*iamv1.BatchAuthorizeCheckResponse, error) {
	if n := len(in.GetChecks()); n > 100 {
		return nil, status.Errorf(codes.InvalidArgument, "Illegal argument checks: batch size %d > 100", n)
	}
	out := &iamv1.BatchAuthorizeCheckResponse{
		Responses: make([]*iamv1.AuthorizeCheckResponse, len(in.GetChecks())),
	}
	for i, chk := range in.GetChecks() {
		c.batchCheckedIDs.Add(1)
		out.Responses[i] = &iamv1.AuthorizeCheckResponse{
			Allowed: c.granted[chk.GetResource().GetId()],
		}
	}
	return out, nil
}

// crowdedTypePopulation — сколько объектов этого типа выдано субъекту помимо его
// единственной строки в проекте. Величина взята заведомо больше любой страницы:
// на ней перечисление типа и вопрос по странице расходятся на три порядка, и
// перепутать их нельзя.
const crowdedTypePopulation = 1000

// crowdedInstanceScenario строит долгоживущий кластер: тысяча выданных объектов
// типа при ОДНОЙ строке в проекте арендатора.
//
// Это реальная форма: горстка строк в проекте, тысячи объектов типа в кластере.
func crowdedInstanceScenario(t *testing.T) (*InstanceHandler, *pageCostAuthorizeClient, string) {
	t.Helper()

	const realID = "ins-zzzowned"

	granted := make([]string, 0, crowdedTypePopulation+1)
	for i := 0; i < crowdedTypePopulation; i++ {
		granted = append(granted, fmt.Sprintf("ins-fill%06d", i))
	}
	granted = append(granted, realID)

	cli := newPageCostAuthorizeClient(granted...)
	h, insRepo := newInstanceHandlerWithFilter(t, newFilter(t, cli))
	insRepo.Seed(&domain.Instance{
		ID: realID, ProjectID: "proj", Name: "own",
		Status: domain.InstanceStatusRunning,
	})
	return h, cli, realID
}

// Положительный контроль: своя выданная существующая строка ВИДНА в списке.
//
// Без него отрицание ниже зеленело бы на фильтре, который не показывает ничего
// никогда.
func TestInstanceList_OwnResourceIsVisibleInACrowdedType(t *testing.T) {
	h, _, realID := crowdedInstanceScenario(t)

	resp, err := h.List(ctxWithSubject("user:usr_alice"),
		&computev1.ListInstancesRequest{ProjectId: "proj"})

	require.NoError(t, err)
	ids := make([]string, 0, len(resp.GetInstances()))
	for _, im := range resp.GetInstances() {
		ids = append(ids, im.GetId())
	}
	assert.Contains(t, ids, realID, "своя, выданная, существующая машина обязана быть в списке; "+
		"её отсутствие означает, что страница фильтруется не по себе, а по населению типа")
}

// Стоимость: видимость резолвится по СТРАНИЦЕ, а не по населению типа.
func TestInstanceList_PageCostDoesNotFollowTypePopulation(t *testing.T) {
	h, cli, _ := crowdedInstanceScenario(t)

	_, err := h.List(ctxWithSubject("user:usr_alice"), &computev1.ListInstancesRequest{ProjectId: "proj"})
	require.NoError(t, err)

	assert.LessOrEqual(t, cli.batchCheckedIDs.Load(), int64(1),
		"вопросов о доступе задано больше, чем строк на странице (посеяна одна): стоимость "+
			"страницы пошла за населением типа — тысяча выданных объектов рядом именно для того, "+
			"чтобы эти два числа нельзя было перепутать")
}

// Пустая страница не стоит НИ ОДНОГО вопроса.
//
// Это вторая половина того же свойства и она нужна отдельно: фильтр, который
// спрашивает про население типа, задаст вопросы и тогда, когда показывать нечего,
// — а проба выше на пустой странице молчала бы.
func TestAllPublicLists_EmptyPageAsksNothing(t *testing.T) {
	t.Run("instance", func(t *testing.T) {
		cli := newPageCostAuthorizeClient()
		insSvc := instance.NewInstanceService(
			portmock.NewInstanceRepo(), portmock.NewMachineTypeRepo(), portmock.NewZoneRegistry(),
			portmock.NewSubnetRegistry(), &portmock.ProjectClient{OK: true},
			portmock.NewNicClient(), portmock.NewStorageClient(), portmock.NewOpsRepo(),
		)
		h := NewInstanceHandler(insSvc, newFilter(t, cli))
		_, err := h.List(ctxWithSubject("user:usr_alice"), &computev1.ListInstancesRequest{ProjectId: "proj"})
		require.NoError(t, err)
		assert.Zero(t, cli.batchCheckedIDs.Load(),
			"на пустой странице задан вопрос о доступе — спрашивают не про страницу")
	})
}

// Без ослабления: существующая строка, которая субъекту не выдана, в списке не
// появляется. Это страж, не дающий «починить» видимость показом всего подряд.
func TestInstanceList_UngrantedResourceStaysInvisible(t *testing.T) {
	cli := newPageCostAuthorizeClient("ins-granted")
	h, insRepo := newInstanceHandlerWithFilter(t, newFilter(t, cli))
	insRepo.Seed(&domain.Instance{
		ID: "ins-granted", ProjectID: "proj", Name: "granted",
		Status: domain.InstanceStatusRunning,
	})
	insRepo.Seed(&domain.Instance{
		ID: "ins-secret", ProjectID: "proj", Name: "secret",
		Status: domain.InstanceStatusRunning,
	})

	resp, err := h.List(ctxWithSubject("user:usr_alice"), &computev1.ListInstancesRequest{ProjectId: "proj"})
	require.NoError(t, err)
	ids := make([]string, 0, len(resp.GetInstances()))
	for _, im := range resp.GetInstances() {
		ids = append(ids, im.GetId())
	}
	assert.Contains(t, ids, "ins-granted")
	assert.NotContains(t, ids, "ins-secret", "невыданная машина не должна попадать в список")
}
