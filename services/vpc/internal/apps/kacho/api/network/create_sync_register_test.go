// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package network

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/fgaregister"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/kachomock"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/repomock"
)

// recordingRegistrar — фейковый синхронный owner-tuple registrar (Decision 2).
type recordingRegistrar struct {
	mu    sync.Mutex
	calls [][]fgaregister.Item
	err   error
}

func (r *recordingRegistrar) Register(_ context.Context, items []fgaregister.Item) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := make([]fgaregister.Item, len(items))
	copy(cp, items)
	r.calls = append(r.calls, cp)
	return r.err
}

func (r *recordingRegistrar) snapshot() [][]fgaregister.Item {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([][]fgaregister.Item, len(r.calls))
	copy(out, r.calls)
	return out
}

// Decision 2 GWT-2.1: после успешного Commit ресурса use-case СИНХРОННО
// регистрирует owner-tuple через registrar — до того, как Operation станет done.
// Грант доступен сразу (без гонки с async drainer'ом).
func TestCreateUseCase_SyncRegister_OwnerTuple(t *testing.T) {
	kr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	reg := &recordingRegistrar{}
	uc := NewCreateNetworkUseCase(kr, &repomock.ProjectClient{OK: true}, or, false).WithRegistrar(reg)

	op, err := uc.Execute(context.Background(), domain.Network{
		ProjectID: "f1",
		Name:      domain.RcNameVPC("net-sync"),
	})
	require.NoError(t, err)

	saved := repomock.AwaitOpDone(t, or, op.ID)
	require.True(t, saved.Done)
	require.Nil(t, saved.Error)

	calls := reg.snapshot()
	require.Len(t, calls, 1, "sync RegisterResource должен быть вызван ровно один раз в create-flow")
	require.NotEmpty(t, calls[0])
	var sawNetwork bool
	for _, it := range calls[0] {
		if strings.HasPrefix(it.Tuple.Object, "vpc_network:") {
			sawNetwork = true
		}
	}
	assert.True(t, sawNetwork, "owner-tuple для vpc_network должен быть зарегистрирован синхронно")

	// Тот же набор Item'ов должен быть и в outbox-intent (backstop): sync + async
	// несут одинаковую регистрацию (идемпотентно).
	require.GreaterOrEqual(t, len(kr.FGARegisterEvents()), 1, "outbox-intent остается backstop'ом")
}

// Отказ синхронной регистрации НЕ проваливает мутацию. Это заменяет прежний
// «Decision 2 GWT-2.2: fail-closed», и обоснование — ровно та посылка, которая
// стояла в его же комментарии: «ресурс закоммичен, outbox-intent durable —
// backstop drainer дорегистрирует». Если посылка верна, то отказ операции не
// защищает ничего: строка уже есть и никуда не денется, а клиент получает
// ошибку на существующий ресурс — фантом (повтор ловит AlreadyExists по
// занятому имени). Sync-registrar — оптимизация окна видимости гранта поверх
// at-least-once дренажа, а не условие успеха Create.
//
// Здесь registrar отдаёт ПЛОСКУЮ (не gRPC-status) ошибку: она дополняет
// create_register_denied_test.go, где отказ несёт чужой gRPC-код и служебную
// обёртку. Оба вида отказа обязаны оставлять операцию успешной.
func TestCreateUseCase_SyncRegisterFailure_DoesNotFailTheMutation(t *testing.T) {
	kr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	reg := &recordingRegistrar{err: errors.New("iam unavailable")}
	uc := NewCreateNetworkUseCase(kr, &repomock.ProjectClient{OK: true}, or, false).WithRegistrar(reg)

	op, err := uc.Execute(context.Background(), domain.Network{
		ProjectID: "f1",
		Name:      domain.RcNameVPC("net-register-failure"),
	})
	require.NoError(t, err)

	saved := repomock.AwaitOpDone(t, or, op.ID)
	require.True(t, saved.Done)
	require.Len(t, reg.snapshot(), 1, "registrar must have been invoked on the create path")
	require.Nil(t, saved.Error,
		"registration runs after the durable commit — its failure must not fail the mutation")

	require.Len(t, kr.Networks(), 1, "resource is durable: commit happened before registration")
	require.GreaterOrEqual(t, len(kr.FGARegisterEvents()), 1,
		"durable register-intent is the at-least-once backstop that makes swallowing safe")
}

// Без registrar (nil) поведение прежнее: Create проходит, sync-register
// пропускается (dev/no-iam), outbox-intent остается единственным путем.
func TestCreateUseCase_NilRegistrar_BackCompat(t *testing.T) {
	kr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	uc := NewCreateNetworkUseCase(kr, &repomock.ProjectClient{OK: true}, or, false)

	op, err := uc.Execute(context.Background(), domain.Network{ProjectID: "f1", Name: domain.RcNameVPC("net-nil")})
	require.NoError(t, err)
	saved := repomock.AwaitOpDone(t, or, op.ID)
	require.True(t, saved.Done)
	require.Nil(t, saved.Error)
}
