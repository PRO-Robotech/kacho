// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package loadbalancer

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	rpcstatus "google.golang.org/genproto/googleapis/rpc/status"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
	vpcclient "github.com/PRO-Robotech/kacho/services/nlb/internal/clients/vpc"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// АСИНХРОННАЯ полоса связанного адреса — та же, что и синхронная, и отвечать
// обязана так же.
//
// `linked_address_visibility_lane_test.go` развёл полосы на SYNC-предпроверке:
// адрес, только что созданный самим вызывающим, ещё не виден пообъектному
// authz → `FAILED_PRECONDITION` + `PEER_RESOURCE_MISSING` (переходная полоса,
// закрывается ограниченным повтором клиента), а настоящий mismatch остаётся
// терминальным `INVALID_ARGUMENT` без токена.
//
// Тот же вопрос задаётся ВТОРОЙ раз — воркером, на link-CAS
// (`InternalAddressService.SetAddressReference`), — и там ответ схлопывался
// обратно: `linkAcquireErr` отдавал ОДИН `FAILED_PRECONDITION "Illegal argument
// addressId"` и промаху, и отказу в правах, и негодной ссылке, и проигранному
// CAS. Полосы разошлись между собой: на один и тот же вход синхронная ветка
// отвечала различимо, асинхронная — нет.
//
// Цена именно у асинхронной: её ответ клиент читает из `Operation.error`, где
// разбор прозы невозможен by construction (тон стабилен, но не парсибелен), а
// токен — единственный машинный признак. Без него краснота сквозного прогона
// называет шаг и не называет причину.
//
// Проза во ВСЕХ полосах остаётся генерической — анти-oracle сохранён
// (положительные контроли ниже это и стерегут).

// opReason достаёт reason-token из деталей ошибки ОПЕРАЦИИ; "" — если его нет.
func opReason(t *testing.T, final *rpcstatus.Status) string {
	t.Helper()
	require.NotNil(t, final)
	for _, d := range status.FromProto(final).Details() {
		if ei, ok := d.(*errdetails.ErrorInfo); ok {
			return ei.GetReason()
		}
	}
	return ""
}

// linkWorkerOutcome прогоняет Create с BYO-ссылкой, чей link-CAS отвечает `peerErr`,
// и возвращает терминальную ошибку операции.
func linkWorkerOutcome(t *testing.T, peerErr error) *rpcstatus.Status {
	t.Helper()
	repo, opsRepo := newFakeRepo(), newFakeOpsRepo()
	addr := &fakeAddressClient{byoFunc: func(_ context.Context, _ vpcclient.AttachExistingRequest) (*vpcclient.AllocateResponse, error) {
		return nil, peerErr
	}}
	uc := newCreateUC(repo, opsRepo, createDeps{reader: &fakeAddressReader{}, addr: addr})
	req := baseCreateReq()
	req.Placement = lbv1.NetworkLoadBalancer_INTERNAL_REGIONAL
	req.V4Source = vipAddress(lbTestAddrInternal)

	op, err := uc.Execute(context.Background(), req)
	require.NoError(t, err)
	final := awaitOpDone(t, opsRepo, op.ID)
	require.NotNil(t, final.Error, "полоса обязана дойти до терминальной ошибки операции")
	assert.Empty(t, repo.lbs, "durable-ручка компенсирована на любой полосе отказа")
	return final.Error
}

// opCode / opMessage — код и проза терминальной ошибки операции. Отдельными
// функциями, чтобы каждое утверждение доставало их ОДНИМ способом: три разных
// способа достать одно и то же расходятся молча.
func opCode(final *rpcstatus.Status) codes.Code { return codes.Code(final.GetCode()) }
func opMessage(final *rpcstatus.Status) string  { return final.GetMessage() }

// Свой свежий адрес ещё не виден пообъектному authz на link-CAS → полоса
// peer-validate с машинным признаком, а не безымянный приговор.
func TestCreate_Worker_LinkedAddressInvisible_AnswersPeerMissLane(t *testing.T) {
	final := linkWorkerOutcome(t, fmt.Errorf("%w: address %s not found", domain.ErrNotFound, lbTestAddrInternal))

	assert.Equal(t, codes.FailedPrecondition, opCode(final),
		"нерезолвящийся чужой id — предусловие на чужой ресурс")
	assert.Equal(t, "Illegal argument addressId", opMessage(final),
		"проза остаётся генерической — ownership/placement чужого адреса не подтверждаем")
	assert.Equal(t, "PEER_RESOURCE_MISSING", opReason(t, final),
		"из Operation.error прозу не парсят: токен — единственный машинный признак полосы")
}

// Положительный контроль №1: негодная по мнению владельца ссылка остаётся
// ТЕРМИНАЛЬНОЙ и токена переходной полосы не носит.
func TestCreate_Worker_LinkedAddressMalformed_StaysTerminalWithoutPeerMissToken(t *testing.T) {
	final := linkWorkerOutcome(t, fmt.Errorf("%w: vpc set address reference: bad referrer", domain.ErrInvalidArg))

	assert.Equal(t, codes.InvalidArgument, opCode(final),
		"то, что повтором не лечится, отвечает полосой аргумента")
	assert.Equal(t, "Illegal argument addressId", opMessage(final))
	assert.Empty(t, opReason(t, final),
		"терминальный отказ не вправе носить токен переходной полосы — иначе клиентский повтор залипнет")
}

// Положительный контроль №2: проигранный CAS (адрес уже занят) — состояние
// чужого ресурса. Код тот же, что у переходной полосы, поэтому различает их
// РОВНО токен; без этого контроля утверждение о токене выше зеленело бы на
// маппере, который вешает токен на всё подряд.
func TestCreate_Worker_LinkConflict_StaysFailedPreconditionWithoutPeerMissToken(t *testing.T) {
	final := linkWorkerOutcome(t, domain.ErrFailedPrecondition)

	assert.Equal(t, codes.FailedPrecondition, opCode(final))
	assert.Equal(t, "Illegal argument addressId", opMessage(final))
	assert.Empty(t, opReason(t, final),
		"занятый адрес не станет свободным от повтора — это не полоса промаха")
}

// Положительный контроль №3: недоступность владельца — единственная полоса, где
// он не установил НИЧЕГО; она была верна и до правки и обязана её пережить.
func TestCreate_Worker_LinkedAddressPeerUnavailable_StaysTransientWithoutPeerMissToken(t *testing.T) {
	final := linkWorkerOutcome(t, fmt.Errorf("%w: vpc down", domain.ErrUnavailable))

	assert.Equal(t, codes.Unavailable, opCode(final))
	assert.Equal(t, "address lookup unavailable", opMessage(final))
	assert.Empty(t, opReason(t, final))
}
