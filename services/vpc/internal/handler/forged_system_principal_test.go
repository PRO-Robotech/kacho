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
	"github.com/PRO-Robotech/kacho/pkg/authz"
	"github.com/PRO-Robotech/kacho/pkg/operations"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/services/nicinternal"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/check"
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

// Тот же вопрос на уровне звена решения о доступе: предъявлена ли личность.
//
// # Что здесь изменилось при переводе на носитель контура — и это НЕ косметика
//
// До перевода у vpc стоял СВОЙ страж (`handler.AuthNUnaryInterceptor`), который
// отвергал ЛЮБОЙ принципал типа `system` — и делал это только в боевом режиме.
// Носитель такого звена не ставит: цепочку принести нельзя, а решение о доступе
// он принимает одним звеном (`pkg/authz`), чей предикат анонимности — общий на
// платформу (`operations.Principal.IsAnonymous`) и сужает ИМЕННО анонимность:
// пустую пару и зарезервированное слово `anonymous` в любом заявленном типе.
//
// Итог по полосам, названный прямо, а не сведённый к «всё то же самое»:
//
//   - метка анонимности края (`{system, anonymous}`) и отсутствие принципала
//     вовсе → отказ ДО обращения к модели, и теперь БЕЗУСЛОВНО, в любом режиме,
//     а не только в боевом. Это строже прежнего;
//   - прочий заявленный `system` (`{system, bootstrap}`, `{system, <что угодно>}`)
//     → доезжает до модели субъектом `user:<id>` и отвергается ОТСУТСТВИЕМ
//     ВЫДАЧИ, а не свойством цепочки. Прежний страж отвергал его по построению.
//     Дополнительной границы это не снимает: заявить тип и идентификатор может
//     ТОЛЬКО пир из круга законных отправителей (пара извлечения личности
//     проверяет сертификат), а такой пир и без того вправе прислать
//     `{user, <идентификатор жертвы>}` — то есть ровно тот же субъект. Граница
//     здесь одна, и её держит круг, а не отброшенный страж.
//
// Проба утверждает обе полосы на НАБЛЮДАЕМОМ: дошёл ли вызов до обработчика и
// О КОМ спросили модель. Вторая половина обязательна — без неё «отказ» первой
// полосы был бы неотличим от «модель ответила нет».
func decisionOutcome(t *testing.T, p operations.Principal, modelSays bool) (reached bool, err error, asked []string) {
	t.Helper()
	intr := authz.NewInterceptor(authz.InterceptorOptions{
		Cache:       authz.NewCache(0),
		ServiceName: "kacho-vpc-test",
		Map:         check.PermissionMap(),
		Client: authz.CheckClientFunc(func(_ context.Context, subject, _, _ string) (bool, error) {
			asked = append(asked, subject)
			return modelSays, nil
		}),
	})
	ctx := context.Background()
	if p.Type != "" || p.ID != "" {
		ctx = operations.WithPrincipal(ctx, p)
	}
	info := &grpc.UnaryServerInfo{FullMethod: "/kacho.cloud.vpc.v1.NetworkService/List"}
	_, err = intr.Unary()(ctx, &vpcv1.ListNetworksRequest{ProjectId: "prj_x"}, info,
		func(context.Context, any) (any, error) { reached = true; return nil, nil })
	return reached, err, asked
}

// Анонимность личностью не является — и модель об этом даже не спрашивают.
//
// Модель здесь говорит «да» кому угодно: если бы отказ приходил от неё, проба
// была бы зелёной по неверной причине. Утверждается именно то, что до неё не
// дошли.
func TestDecisionLink_AnonymityIsRefusedBeforeTheModelIsAsked(t *testing.T) {
	for name, p := range map[string]operations.Principal{
		"метка анонимности края":    {Type: "system", ID: "anonymous"},
		"личность не предъявлена":   {},
		"анонимность с чужим типом": {Type: "user", ID: "anonymous"},
	} {
		t.Run(name, func(t *testing.T) {
			reached, err, asked := decisionOutcome(t, p, true)
			require.Error(t, err, "анонимность не является предъявленной личностью")
			assert.Equal(t, codes.PermissionDenied, status.Code(err))
			assert.False(t, reached)
			assert.Empty(t, asked, "модель не должна опрашиваться о безымянном вызывающем")
		})
	}
}

// Заявленный `system` с непустым идентификатором решает МОДЕЛЬ, а не цепочка.
//
// Проба закрепляет расхождение со снятым стражем именно как факт, а не как
// «всё по-прежнему»: субъект собирается как `user:<id>` и уезжает в модель.
// Изменится отображение принципала в субъект — проба покраснеет, и расхождение
// не переживёт правку молча.
func TestDecisionLink_DeclaredSystemPrincipalIsDecidedByTheModel(t *testing.T) {
	for name, tc := range map[string]struct {
		principal operations.Principal
		subject   string
	}{
		"fallback без auth":  {operations.Principal{Type: "system", ID: "bootstrap"}, "user:bootstrap"},
		"третье значение id": {operations.Principal{Type: "system", ID: "sva_attacker"}, "user:sva_attacker"},
	} {
		t.Run(name, func(t *testing.T) {
			reached, err, asked := decisionOutcome(t, tc.principal, false)
			require.Error(t, err, "выдачи на этот субъект нет — модель отвечает отказом")
			assert.Equal(t, codes.PermissionDenied, status.Code(err))
			assert.False(t, reached)
			assert.Equal(t, []string{tc.subject}, asked,
				"полоса обязана быть модельной: субъект назван и спрошен")
		})
	}
}

// Контроль: настоящая личность по-прежнему проходит. Без него оба отрицания выше
// зеленели бы и от «сломали передачу личности вообще».
func TestDecisionLink_RealPrincipalReachesTheHandler(t *testing.T) {
	reached, err, asked := decisionOutcome(t,
		operations.Principal{Type: "user", ID: "usr_alice"}, true)
	require.NoError(t, err)
	assert.True(t, reached)
	assert.Equal(t, []string{"user:usr_alice"}, asked)
}
