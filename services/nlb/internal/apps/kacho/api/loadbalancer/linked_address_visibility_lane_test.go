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

	vpcclient "github.com/PRO-Robotech/kacho/services/nlb/internal/clients/vpc"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// Линия «связанный адрес не резолвится у владельца» — ПЕРЕХОДНАЯ, а не приговор
// аргументу.
//
// `resolveLinkedAddress` читает чужой (vpc-owned) `v4Source/v6Source.addressId`
// под личностью вызывающего. Когда адрес только что создан этим же вызывающим,
// vpc отвечает hide-existence NOT_FOUND / PERMISSION_DENIED, пока пообъектный
// owner-tuple свежего `vpc_address:<id>` не материализовался (data-integrity.md:
// материализация eventually-consistent — outbox → drainer → iam → FGA). Ровно
// эту пару кодов сосед по пакету объявляет невидимостью собственного ресурса —
// `clients/vpc/internal_address_client.go` §ownResourceInvisible — и ретраит её
// на reference-CAS полосе.
//
// Get-precheck той же ссылки этого не знал: NOT_FOUND/PERMISSION_DENIED
// схлопывались в `domain.ErrInvalidArg`, и вызывающий получал ТЕРМИНАЛЬНЫЙ
// `INVALID_ARGUMENT "Illegal argument addressId"` — утверждение «твой аргумент
// незаконен» про well-formed id существующего ресурса. Клиенту нечего было
// различать: bounded read-your-writes retry (api-conventions.md: «создал→сразу
// мутирую» закрывается ретраем КЛИЕНТА, не серверным барьером — ban #9) не
// отличал эту полосу от настоящего mismatch'а и не срабатывал ни разу.
//
// Полоса задана api-conventions.md §By-lane code-split: чужой id не существует
// у владельца → `FAILED_PRECONDITION` + `ErrorInfo.reason =
// PEER_RESOURCE_MISSING`. Проза сообщения остаётся ГЕНЕРИЧЕСКОЙ — анти-oracle
// сохранён (см. positive control ниже).

// peerMissReason достаёт reason-token из деталей ошибки; "" — если его нет.
// Клиент различает полосы машинно по нему, а не разбором прозы (api-conventions).
func peerMissReason(t *testing.T, err error) string {
	t.Helper()
	for _, d := range status.Convert(err).Details() {
		if ei, ok := d.(*errdetails.ErrorInfo); ok {
			return ei.GetReason()
		}
	}
	return ""
}

// Собственный свежий адрес ещё не виден пообъектному authz → полоса
// peer-validate, а НЕ терминальный приговор аргументу.
func TestCreateLB_LinkedAddress_OwnLaneInvisible_AnswersPeerMissLane(t *testing.T) {
	t.Parallel()
	const fresh = "adr7tp1q22pfqey44m4m"
	ar := &countingAddressReader{fn: func(_ context.Context, id string) (*vpcclient.Address, error) {
		// Ровно то, что отдаёт адаптер, когда vpc прячет существование.
		return nil, fmt.Errorf("%w: address %s not found", domain.ErrNotFound, id)
	}}
	uc := newCreateUC(newFakeRepo(), newFakeOpsRepo(), createDeps{reader: ar})
	req := internalRegionalReq()
	req.V4Source = vipAddress(fresh)

	_, err := uc.Execute(context.Background(), req)

	require.Error(t, err)
	assert.Equal(t, []string{fresh}, ar.calls, "существование решает владелец")
	assert.Equal(t, codes.FailedPrecondition, status.Code(err),
		"нерезолвящийся ЧУЖОЙ id — предусловие на чужой ресурс, не незаконный аргумент")
	assert.Equal(t, "Illegal argument addressId", status.Convert(err).Message(),
		"проза остаётся генерической — placement/ownership чужого адреса не подтверждаем")
	assert.Equal(t, "PEER_RESOURCE_MISSING", peerMissReason(t, err),
		"клиент обязан различать полосу машинно, иначе bounded retry не за что зацепиться")
}

// Positive control — то, ради чего генерическая проза и заводилась. Настоящий
// mismatch (адрес чужого проекта) обязан остаться ТЕРМИНАЛЬНЫМ и НЕ нести
// peer-miss-token: иначе клиентский ретрай залипнет на отказе, который не
// станет успехом никогда, а различающая проба перестанет различать.
func TestCreateLB_LinkedAddress_ForeignProject_StaysTerminalWithoutPeerMissToken(t *testing.T) {
	t.Parallel()
	ar := &fakeAddressReader{projectID: "prj-b"}
	uc := newCreateUC(newFakeRepo(), newFakeOpsRepo(), createDeps{reader: ar})
	req := internalRegionalReq()
	req.V4Source = vipAddress("adr-foreign")

	_, err := uc.Execute(context.Background(), req)

	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err),
		"ownership-mismatch клиент не исправит повтором — полоса терминальная")
	assert.Equal(t, "Illegal argument addressId", status.Convert(err).Message())
	assert.Empty(t, peerMissReason(t, err),
		"терминальный отказ не вправе носить token переходной полосы")
}

// Malformed id остаётся терминальным INVALID_ARGUMENT и до владельца не едет —
// синтаксический гейт VIP-источников (08-known-divergences) этой правкой не
// затронут.
func TestCreateLB_LinkedAddress_Malformed_StaysTerminalWithoutPeerMissToken(t *testing.T) {
	t.Parallel()
	ar := &countingAddressReader{}
	uc := newCreateUC(newFakeRepo(), newFakeOpsRepo(), createDeps{reader: ar})
	req := internalRegionalReq()
	req.V4Source = vipAddress("garbage!!")

	_, err := uc.Execute(context.Background(), req)

	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	assert.Equal(t, "invalid address id 'garbage!!'", status.Convert(err).Message())
	assert.Empty(t, ar.calls, "явный мусор не оплачивается вызовом к соседу")
	assert.Empty(t, peerMissReason(t, err))
}

// Владелец недоступен — по-прежнему UNAVAILABLE (fail-closed, retryable), и это
// НЕ peer-miss: различать «соседа нет» и «ссылки у соседа нет» обязаны обе
// стороны.
func TestCreateLB_LinkedAddress_OwnerUnavailable_StaysUnavailable(t *testing.T) {
	t.Parallel()
	ar := &countingAddressReader{fn: func(context.Context, string) (*vpcclient.Address, error) {
		return nil, fmt.Errorf("%w: vpc down", domain.ErrUnavailable)
	}}
	uc := newCreateUC(newFakeRepo(), newFakeOpsRepo(), createDeps{reader: ar})
	req := internalRegionalReq()
	req.V4Source = vipAddress("adr7tp1q22pfqey44m4m")

	_, err := uc.Execute(context.Background(), req)

	require.Error(t, err)
	assert.Equal(t, codes.Unavailable, status.Code(err))
	assert.Empty(t, peerMissReason(t, err))
}
