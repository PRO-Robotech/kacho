// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package handler_test

// Кем себя объявил вызывающий — не основание доверять ему всё.
//
// Тип принципала приезжает в метаданных запроса (`x-kacho-principal-type`) и
// целиком назначается тем, кто звонит. Значение `system` вдобавок служит на
// платформе ЯРЛЫКОМ АНОНИМНОСТИ: край ставит запросу без удостоверения ровно
// `{system, anonymous}`. Поэтому «тип равен system» не может означать «полное
// доверие» — иначе полное доверие получает и подделавший заголовок, и вообще
// неаутентифицированный вызывающий.
//
// Цена именно на этом RPC максимальна: `ListByInstance` авторизуется НА УРОВНЕ
// ДАННЫХ (ScopeFiltered), то есть единичного per-RPC Check за него не задаётся
// вовсе — фильтр видимости в use-case'е и есть весь гейт. Обойдя фильтр, вызывающий
// получает привязки интерфейсов ЛЮБЫХ названных им инстансов: идентификатор
// интерфейса, подсеть, адреса, группы безопасности, MAC — из чужих проектов и
// аккаунтов.
//
// Тесты ниже утверждают на НАБЛЮДАЕМОМ — на том, что получает вызывающий из
// gRPC-ответа, а не на том, «вызвана ли функция».

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/pkg/operations"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/services/nicinternal"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/handler"
	kachorepo "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/kachomock"

	"github.com/PRO-Robotech/kacho/pkg/listnarrow/narrowtest"
)

func seedVictimNIC(t *testing.T, kr *kachomock.Repository, nicID, instanceID string) {
	t.Helper()
	ctx := context.Background()
	w, err := kr.Writer(ctx)
	require.NoError(t, err)
	_, err = w.NetworkInterfaces().Insert(ctx, &domain.NetworkInterface{
		ID:        nicID,
		ProjectID: "prj_victim",
		Name:      domain.RcNameVPC("nic-" + nicID),
		SubnetID:  "e9b_victim",
		Status:    domain.NIStatusAvailable,
	})
	require.NoError(t, err)
	_, err = w.NetworkInterfaces().AttachToInstance(ctx, kachorepo.AttachNICParams{
		NICID:      nicID,
		InstanceID: instanceID,
	})
	require.NoError(t, err)
	require.NoError(t, w.Commit())
}

func victimHandler(t *testing.T) (*handler.InternalNetworkInterfaceHandler, *narrowtest.Peer) {
	t.Helper()
	kr := kachomock.NewRepository()
	seedVictimNIC(t, kr, "nic_victim", "ins_victim")
	f, peer := narrowtest.Recording()
	return handler.NewInternalNetworkInterfaceHandler(nicinternal.NewService(kr).WithListFilter(f)), peer
}

// Принципал, чей ТИП вызывающий написал себе сам, не открывает чужие привязки.
//
// `{system, anonymous}` — ровно то, что край подставляет запросу БЕЗ удостоверения,
// то есть эту форму может предъявить кто угодно, ничего не подделывая.
func TestListByInstance_ForgedSystemPrincipalSeesNothing(t *testing.T) {
	for name, p := range map[string]operations.Principal{
		"метка анонимности края": {Type: "system", ID: "anonymous"},
		"подделанный system":     {Type: "system", ID: "sva_attacker", DisplayName: "attacker"},
		"system с пустым id":     {Type: "system"},
	} {
		t.Run(name, func(t *testing.T) {
			h, peer := victimHandler(t)
			ctx := operations.WithPrincipal(context.Background(), p)

			resp, err := h.ListByInstance(ctx, &vpcv1.ListNetworkInterfacesByInstanceRequest{
				InstanceIds: []string{"ins_victim"},
			})

			require.Error(t, err,
				"объявленный вызывающим тип принципала не может открывать привязки "+
					"интерфейсов чужих инстансов")
			assert.Equal(t, codes.Unauthenticated, status.Code(err),
				"ответ обязан быть про личность: «пусто» неотличимо от «личность потеряна по дороге»")
			assert.Empty(t, resp.GetNetworkInterfaces())
			assert.Zero(t, peer.Calls,
				"личность не предъявлена — спрашивать модель не о ком; отдавать всё тем более не за что")
		})
	}
}

// Симметричный контроль: настоящая личность по-прежнему доходит до модели, и ответ
// модели по-прежнему решает. Без этого случая предыдущий тест зеленел бы и от
// «сломали RPC целиком».
func TestListByInstance_RealPrincipalStillAsksTheModel(t *testing.T) {
	h, peer := victimHandler(t)
	ctx := operations.WithPrincipal(context.Background(),
		operations.Principal{Type: "user", ID: "usr_alice"})

	resp, err := h.ListByInstance(ctx, &vpcv1.ListNetworkInterfacesByInstanceRequest{
		InstanceIds: []string{"ins_victim"},
	})

	require.NoError(t, err)
	assert.Empty(t, resp.GetNetworkInterfaces(), "модель не разрешила — отдавать нечего")
	assert.Equal(t, 1, peer.Calls, "у настоящей личности видимость обязана спрашиваться у модели")
}

// Тот же вопрос на уровне AuthN-стража: предъявлена ли личность.
//
// Страж отвечал на него, перечисляя ДВА известных значения идентификатора
// (`anonymous`, `bootstrap`), — то есть любое ТРЕТЬЕ значение при том же типе
// проходило как «личность предъявлена». Признак же не в идентификаторе: тип
// `system` целиком назначается вызывающим и служит на платформе ярлыком
// анонимности, поэтому им не может называться никто. Ни один законный путь этот
// тип не шлёт: служебные вызовы несут `user:system.<сервис>-<роль>`.
func TestAuthN_DeclaredSystemTypeIsNotAnIdentity(t *testing.T) {
	for name, p := range map[string]operations.Principal{
		"метка анонимности края": {Type: "system", ID: "anonymous"},
		"fallback без auth":      {Type: "system", ID: "bootstrap"},
		"третье значение id":     {Type: "system", ID: "sva_attacker"},
	} {
		t.Run(name, func(t *testing.T) {
			intr := handler.AuthNUnaryInterceptor(true)
			reached := false
			_, err := intr(
				operations.WithPrincipal(context.Background(), p),
				nil, &grpc.UnaryServerInfo{FullMethod: "/kacho.cloud.vpc.v1.NetworkService/List"},
				func(context.Context, any) (any, error) { reached = true; return nil, nil })

			require.Error(t, err, "объявленный вызывающим тип не является предъявленной личностью")
			assert.Equal(t, codes.PermissionDenied, status.Code(err))
			assert.False(t, reached)
		})
	}
}

// Контроль: настоящая личность по-прежнему проходит.
func TestAuthN_RealPrincipalPasses(t *testing.T) {
	intr := handler.AuthNUnaryInterceptor(true)
	reached := false
	_, err := intr(
		operations.WithPrincipal(context.Background(),
			operations.Principal{Type: "user", ID: "usr_alice"}),
		nil, &grpc.UnaryServerInfo{FullMethod: "/kacho.cloud.vpc.v1.NetworkService/List"},
		func(context.Context, any) (any, error) { reached = true; return nil, nil })
	require.NoError(t, err)
	assert.True(t, reached)
}
