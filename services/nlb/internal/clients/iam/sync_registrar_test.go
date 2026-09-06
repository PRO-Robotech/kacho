// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// sync_registrar_test.go — пробы ПЕРЕВОДА намерения nlb в общую форму доставки.
//
// # Что здесь проверяется и чего здесь БОЛЬШЕ НЕТ
//
// Регистратор nlb перестал быть самостоятельной реализацией: цикл по tuple'ам,
// срок вызова, сборка запроса, поведение при отказе и проброс личности живут
// теперь в ОДНОМ месте (pkg/ownerregister) и проверяются его собственными
// пробами. Здесь остаётся ровно то, что принадлежит nlb, — ПЕРЕВОД: какие поля
// намерения куда легли и какая версия поехала.
//
// Прежние шесть проб этого файла (forward полей, всплытие терминального и
// временного отказа, отсутствие короткого замыкания, продолжение после
// временного) переехали в пробы общей формы вместе со своим предметом. Седьмая
// — «нулевой клиент → пустая операция» — НЕ переехала, а ПЕРЕПИСАНА: общая
// форма такой клиент ОТВЕРГАЕТ. Прежнее поведение делало ускоритель, который
// никогда ничего не ускорял, неотличимым от исправного; проба ниже закрепляет
// новое, и это осознанная смена контракта, а не потеря проверки.
package iam_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	iampb "github.com/PRO-Robotech/kacho/pkg/api/kaname/cloud/iam/v1"
	"github.com/PRO-Robotech/kacho/pkg/ownerregister"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/clients/iam"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// scriptedRegisterClient — RegisterResourceClient-двойник: пишет каждый
// RegisterResource-запрос (для assert'ов forward'а) + флаг наличия deadline,
// возвращает scripted-ошибку по relation.
type scriptedRegisterClient struct {
	mu            sync.Mutex
	register      []*iampb.RegisterResourceRequest
	hadDeadline   []bool
	errByRelation map[string]error
}

func (c *scriptedRegisterClient) RegisterResource(
	ctx context.Context, in *iampb.RegisterResourceRequest, _ ...grpc.CallOption,
) (*iampb.RegisterResourceResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.register = append(c.register, in)
	_, ok := ctx.Deadline()
	c.hadDeadline = append(c.hadDeadline, ok)
	if c.errByRelation != nil {
		if err := c.errByRelation[in.GetRelation()]; err != nil {
			return nil, err
		}
	}
	return &iampb.RegisterResourceResponse{}, nil
}

func (c *scriptedRegisterClient) UnregisterResource(
	_ context.Context, _ *iampb.UnregisterResourceRequest, _ ...grpc.CallOption,
) (*iampb.UnregisterResourceResponse, error) {
	return &iampb.UnregisterResourceResponse{}, nil
}

// calls — снятые запросы. Возвращается копия: проба читает их после вызова, а
// дублёр мог бы дописывать из другой горутины.
func (c *scriptedRegisterClient) calls() []*iampb.RegisterResourceRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]*iampb.RegisterResourceRequest, len(c.register))
	copy(out, c.register)
	return out
}

func (c *scriptedRegisterClient) relations() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.register))
	for i, r := range c.register {
		out[i] = r.GetRelation()
	}
	return out
}

const (
	testLBID   = "nlb-aaaaaaaaaaaaaaaaa"
	testProjID = "prj-prod000000000000"
)

// lbUserIntent — LB owner-intent аутентифицированного user'а: project-tuple
// ПЕРВЫМ (containment, iam-proxy accepts) + creator(admin) вторым (iam-proxy
// rejects — не registrable relation).
func lbUserIntent() domain.FGARegisterIntent {
	return domain.FGARegisterIntent{
		Kind:            "NetworkLoadBalancer",
		ResourceID:      testLBID,
		Labels:          map[string]string{"tier": "critical"},
		ParentProjectID: testProjID,
		ParentAccountID: "acc-aaaaaaaaaaaaaaaa",
		Tuples: []domain.FGATuple{
			domain.FGAProjectTuple(domain.FGAObjectTypeLoadBalancer, testLBID, testProjID),
			// A relation the iam proxy refuses (privilege relations belong to the
			// AccessBinding flow). Production intents no longer emit one — this fixture
			// keeps exercising the registrar's "do not short-circuit on a rejection".
			{SubjectID: "user:usr-1", Relation: domain.FGARelationAdmin,
				Object: domain.FGAObjectRef(domain.FGAObjectTypeLoadBalancer, testLBID)},
		},
	}
}

// TestSyncRegistrar_RegistersEachTuple_ForwardsMirrorFields — happy path:
// RegisterResource вызывается по одному разу на tuple; forward'ит mirror-поля
// (labels/parent) + монотонный source_version (stamped registrar'ом) + несёт
// per-call deadline. Возвращает nil.
// TestVersionOfTheWriterTxTravelsVerbatim — версия, которой БД проштамповала
// durable-намерение, доезжает до владельца прав БЕЗ ИЗМЕНЕНИЙ, и ею помечается
// КАЖДЫЙ tuple намерения.
//
// Это предмет всей унификации. Утверждается РАВЕНСТВО исходному штампу, а не
// «версия не пуста»: «не пуста» зеленело бы и на часах момента доставки — ровно
// на том, что здесь и стояло.
func TestVersionOfTheWriterTxTravelsVerbatim(t *testing.T) {
	cli := &scriptedRegisterClient{}
	reg, err := iam.NewSyncRegistrar(cli)
	require.NoError(t, err)

	stamp := time.Date(2026, 8, 10, 11, 22, 33, 456789000, time.UTC)
	intent := domain.FGARegisterIntent{
		Kind:            "NetworkLoadBalancer",
		ResourceID:      "nlb-1",
		Tuples:          []domain.FGATuple{{SubjectID: "project:prj-1", Relation: "project", Object: "nlb_load_balancer:nlb-1"}},
		Labels:          map[string]string{"env": "prod"},
		ParentProjectID: "prj-1",
		ParentAccountID: "acc-1",
	}
	require.NoError(t, reg.Register(context.Background(), intent, stamp))

	got := cli.calls()
	require.Len(t, got, 1)
	assert.True(t, got[0].GetSourceVersion().AsTime().Equal(stamp),
		"версия изменилась в пути: отправлено %s, штамп writer-транзакции %s",
		got[0].GetSourceVersion().AsTime(), stamp)
	// Поля зеркала — вместе с версией: недосланное поле не роняет ничего сразу,
	// оно молча обедняет зеркало, по которому резолвится принадлежность объекта.
	assert.Equal(t, "nlb-1", got[0].GetTraceId())
	assert.Equal(t, "prj-1", got[0].GetParentProjectId())
	assert.Equal(t, "acc-1", got[0].GetParentAccountId())
	assert.Equal(t, "prod", got[0].GetLabels()["env"])
}

// TestNilClientRefusesInsteadOfSilentlyDoingNothing — нулевой клиент есть ОТКАЗ.
//
// Прежняя редакция объявляла его пустой операцией и это проверяла. Пустая
// операция неотличима от исправно работающего ускорителя, который никогда
// ничего не ускорял: ни отказа, ни строки в логе, ни одного срабатывания за всю
// жизнь.
func TestNilClientRefusesInsteadOfSilentlyDoingNothing(t *testing.T) {
	_, err := iam.NewSyncRegistrar(nil)
	require.ErrorIs(t, err, ownerregister.ErrNoClient)
}
