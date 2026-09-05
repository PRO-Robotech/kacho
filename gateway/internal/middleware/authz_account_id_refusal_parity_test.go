// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware_test

// authz_account_id_refusal_parity_test.go — КТО производит отказ формы на поле
// `accountId`, когда его спрашивают ДВА глагола.
//
// ПРЕДМЕТ. Приёмка `subject-grants-within-an-account.md` (IAM-AB-SIA-12)
// утверждала: «Производитель назван и он ОДИН на поле:
// `shared.ValidateResourceID(id, domain.PrefixAccount, "account")` — ровно та
// функция, которую на этом же поле зовёт `ListByAccount`», и требовала от двух
// глаголов ПОБАЙТОВО равных тел на негодном `accountId`.
//
// Производителей ДВА, и это свойство края, а не сервиса:
//
//   `List`          — `scope_filtered`: единого объекта для проверки у края нет,
//                     запрос доезжает до владельца, и отказ производит ВЛАДЕЛЕЦ,
//                     называя ресурс: `invalid account id '<X>'`;
//   `ListByAccount` — несёт `scope_extractor` на `account_id`, то есть поле здесь
//                     не фильтр, а ЦЕЛЬ АВТОРИЗАЦИИ. Край обязан судить её форму
//                     ДО обращения к модели прав, иначе отказ «пути нет»
//                     замаскировал бы 400 под 403 (authz.go, полоса 5b). Отказ
//                     производит КРАЙ, и он называет ресурс нейтрально:
//                     `invalid resource id '<X>'` — одинаково на всех своих
//                     полосах, потому что своего словаря имён у него нет.
//
// ЧТО ЭТА ПРОБА ДЕРЖИТ. Утверждение приёмки проверялось только сквозным
// прогоном — двадцать пять минут стенда на один байт текста, — и в дереве не
// проверялось ничем: расхождение прожило от заведения кейса до первого прогона.
// Здесь тот же вопрос задан за миллисекунды и НАСТОЯЩЕМУ стражу края с
// НАСТОЯЩИМИ записями каталога обоих глаголов.
//
// ПРОБА НЕ ОБЪЯВЛЯЕТ РАЗЛИЧИЕ ХОРОШИМ. Нейтральное имя ресурса на полосе края —
// предмет отдельной задачи #1932: край ЗНАЕТ конкретный тип, тот стоит в
// `object_type` той же записи каталога, — но полос у класса 210 из 346 и типов 25,
// то есть правка меняет тон отказа во всех семи сервисах и идёт своей линией.
// До тех пор проба закрепляет ФАКТ: сменится любой из двух текстов — она
// покраснеет и назовёт, какой.
//
// Записи каталога здесь не выписаны, а ВЗЯТЫ ИЗ ВСТРОЕННОГО КАТАЛОГА по имени
// метода: выписанная копия разошлась бы с ним молча — и разошлась бы ровно на той
// оси, о которой проба (наличие `scope_extractor`).

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// malformedAccountID — вход сценария IAM-AB-SIA-12 дословно.
const malformedAccountID = "not-an-id"

// wellFormedAccountID — положительный контроль: годная форма обязана ПРОЙТИ
// полосу формы и дойти до модели прав. Без него отрицание ниже зеленело бы на
// крае, отвергающем всякий вход.
const wellFormedAccountID = "acc00000000000wellfm"

// ownerRefusalOnAccountID — текст ВЛАДЕЛЬЦА поля: `shared.ValidateResourceID(id,
// domain.PrefixAccount, "account")` (services/iam/internal/apps/kaname/shared/ids.go).
const ownerRefusalOnAccountID = "invalid account id 'not-an-id'"

// edgeRefusalOnMalformedScopeID — текст КРАЯ на полосе формы цели авторизации
// (`invalidResourceIDMessage`, permission_denied_response.go).
const edgeRefusalOnMalformedScopeID = "invalid resource id 'not-an-id'"

// catalogEntryJSON — запись встроенного каталога по имени метода, отданная в той
// же форме, какую принимает buildCatalog. Берётся из дерева, а не выписывается.
func catalogEntryJSON(t *testing.T, fqn string) string {
	t.Helper()
	// Встроенный каталог — массив верхнего уровня. Форма прочитана из самого
	// файла, а не угадана: первая редакция этой пробы ждала объекта с полем
	// `entries` и краснела на СВОЁМ разборщике, а не на предмете.
	var entries []json.RawMessage
	require.NoError(t, json.Unmarshal(middleware.EmbeddedPermissionCatalogJSON(), &entries))
	require.NotEmpty(t, entries, "встроенный каталог пуст — проба беспредметна")
	for _, raw := range entries {
		var probe struct {
			FQN string `json:"fqn"`
		}
		require.NoError(t, json.Unmarshal(raw, &probe))
		if probe.FQN == fqn {
			return string(raw)
		}
	}
	t.Fatalf("во встроенном каталоге нет записи %q — проба беспредметна", fqn)
	return ""
}

// TestAccountIDMalformedRefusalNamesItsProducerPerVerb — на негодном `accountId`
// у двух глаголов ДВА производителя, и проба называет каждого поимённо.
func TestAccountIDMalformedRefusalNamesItsProducerPerVerb(t *testing.T) {
	t.Run("List — край пропускает, отказ производит владелец", func(t *testing.T) {
		checker := &fakeChecker{allowed: false, reasons: []string{"no path"}}
		mw := buildAuthzMiddleware(t,
			buildCatalog(t, catalogEntryJSON(t, "kacho.cloud.iam.v1.AccessBindingService/List")),
			checker)
		reached := false
		_, err := mw.Unary()(withTokenMD("usr_x", "user"),
			&iamv1.ListAccessBindingsRequest{AccountId: malformedAccountID},
			&grpc.UnaryServerInfo{FullMethod: "/kacho.cloud.iam.v1.AccessBindingService/List"},
			func(ctx context.Context, req any) (any, error) {
				reached = true
				// Здесь стоит владелец: его отказ и есть тело ответа.
				return nil, status.Error(codes.InvalidArgument, ownerRefusalOnAccountID)
			})
		require.True(t, reached,
			"край перехватил форму поля-ФИЛЬТРА — тогда производитель не владелец, и приёмка называет не того")
		st, _ := status.FromError(err)
		require.Equal(t, codes.InvalidArgument, st.Code())
		require.Equal(t, ownerRefusalOnAccountID, st.Message())
	})

	t.Run("ListByAccount — отказ производит край, владелец не вызывается", func(t *testing.T) {
		checker := &fakeChecker{allowed: false, reasons: []string{"no path"}}
		mw := buildAuthzMiddleware(t,
			buildCatalog(t, catalogEntryJSON(t, "kacho.cloud.iam.v1.AccessBindingService/ListByAccount")),
			checker)
		_, err := mw.Unary()(withTokenMD("usr_x", "user"),
			&iamv1.ListAccessBindingsByAccountRequest{AccountId: malformedAccountID},
			&grpc.UnaryServerInfo{FullMethod: "/kacho.cloud.iam.v1.AccessBindingService/ListByAccount"},
			func(ctx context.Context, req any) (any, error) {
				t.Fatal("владелец вызван: край перестал судить форму ЦЕЛИ АВТОРИЗАЦИИ, " +
					"и отказ «пути нет» снова замаскирует 400 под 403")
				return nil, nil
			})
		st, _ := status.FromError(err)
		require.Equal(t, codes.InvalidArgument, st.Code(),
			"негодная цель авторизации обязана оставаться 400, а не 403")
		require.Equal(t, edgeRefusalOnMalformedScopeID, st.Message(),
			"текст края на полосе формы сменился — сверить с IAM-AB-SIA-12 и с задачей #1932")
		require.Zero(t, checker.calls.Load(), "модель прав не спрашивается о негодной цели")
	})

	t.Run("положительный контроль: годная форма проходит полосу формы", func(t *testing.T) {
		checker := &fakeChecker{allowed: false, reasons: []string{"no path"}}
		mw := buildAuthzMiddleware(t,
			buildCatalog(t, catalogEntryJSON(t, "kacho.cloud.iam.v1.AccessBindingService/ListByAccount")),
			checker)
		_, err := mw.Unary()(withTokenMD("usr_x", "user"),
			&iamv1.ListAccessBindingsByAccountRequest{AccountId: wellFormedAccountID},
			&grpc.UnaryServerInfo{FullMethod: "/kacho.cloud.iam.v1.AccessBindingService/ListByAccount"},
			func(ctx context.Context, req any) (any, error) { return "ok", nil })
		st, _ := status.FromError(err)
		require.Equal(t, codes.PermissionDenied, st.Code(),
			"годная форма обязана дойти до модели прав: иначе отрицание выше зеленеет на крае, отвергающем всё")
		require.Positive(t, checker.calls.Load(), "модель прав не спрошена о годной цели")
	})
}
