// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// model_absent_is_not_yes_test.go — «модели здесь нет» не есть «да».
//
// Эталон уже стоит в дереве, у соседа: storage AllowedOnObject на том же условии
// (`f == nil || !f.cfg.Enabled || f.cli == nil`) отказывает, и говорит почему —
// «Это состояние посадки, а не ответ модели, — поэтому отказ, а не „да"». Разница
// между «модель ответила нет» и «модели здесь нет» и есть предмет: второе не бывает
// разрешением.
//
// Списочные пути nlb помечены ScopeFiltered — per-RPC Check за них не задаётся
// ВОВСЕ, и откатываться при отсутствующем фильтре не на что. Отсечку безымянного
// вызывающего здесь уже сделали безусловной; отсечка ОТСУТСТВУЮЩЕГО ФИЛЬТРА осталась
// непроведённой — закрыли шумный подслучай, тихий выжил.
package authzfilter

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/pkg/operations"
)

func namedCaller() context.Context {
	return operations.WithPrincipal(context.Background(),
		operations.Principal{Type: "user", ID: "usr_alice"})
}

// TestFilterPage_AbsentModelRefuses — названный вызывающий и НЕТ фильтра: страница
// не отдаётся. За этими RPC нет второй линии, поэтому отсутствие фильтра означает
// отсутствие авторизации, а не «сужение выключено».
func TestFilterPage_AbsentModelRefuses(t *testing.T) {
	got, err := FilterPage(namedCaller(), nil,
		ResourceTypeLoadBalancer, ActionLoadBalancerList, []string{"nlb-a", "nlb-b"})
	require.Error(t, err, "фильтра нет — спросить негде; страницу отдавать не на каком основании")
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
	assert.Empty(t, got)
}

// TestFilterVisiblePage_AbsentModelRefuses — второй вход в тот же контракт. Пропустить
// его значило бы оставить дыру ровно для тех List'ов, что фильтруют записи, а не id.
func TestFilterVisiblePage_AbsentModelRefuses(t *testing.T) {
	type rec struct{ id string }
	got, err := FilterVisiblePage(namedCaller(), nil,
		ResourceTypeLoadBalancer, ActionLoadBalancerList,
		[]rec{{"nlb-a"}, {"nlb-b"}}, func(r rec) string { return r.id })
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
	assert.Empty(t, got)
}

// TestFilterPage_PresentModelStillAnswers — ПАРНЫЙ ПОЛОЖИТЕЛЬНЫЙ. Без него «отказано»
// неотличимо от «отказывает всегда», и отказ выше зеленел бы на полностью сломанном
// пути.
func TestFilterPage_PresentModelStillAnswers(t *testing.T) {
	flt := &fakeFilter{visible: []string{"nlb-a"}}
	got, err := FilterPage(namedCaller(), flt,
		ResourceTypeLoadBalancer, ActionLoadBalancerList, []string{"nlb-a", "nlb-b"})
	require.NoError(t, err, "модель на месте — ответ обязан быть получен")
	assert.Equal(t, []string{"nlb-a"}, got, "и он обязан быть СУЖЕНИЕМ, а не всей страницей")
	assert.Equal(t, 1, flt.calls)
}

// TestFilterVisiblePage_PresentModelStillAnswers — парный положительный для второго входа.
func TestFilterVisiblePage_PresentModelStillAnswers(t *testing.T) {
	type rec struct{ id string }
	flt := &fakeFilter{visible: []string{"nlb-b"}}
	got, err := FilterVisiblePage(namedCaller(), flt,
		ResourceTypeLoadBalancer, ActionLoadBalancerList,
		[]rec{{"nlb-a"}, {"nlb-b"}}, func(r rec) string { return r.id })
	require.NoError(t, err)
	assert.Equal(t, []rec{{"nlb-b"}}, got)
}

// TestAbsentModelAndUnnamedCallerAnswerTheSameWay — положение вызывающего не должно
// решаться тем, что оператор прописал в конфиге: безымянный получает отказ и с
// фильтром, и без него. Проверяется, что отказ ОТСУТСТВУЮЩЕЙ МОДЕЛИ не подменил собой
// отсечку анонимности (иначе один класс закрыл бы другой и спрятал его регрессию).
func TestAbsentModelAndUnnamedCallerAnswerTheSameWay(t *testing.T) {
	ids := []string{"nlb-a"}

	_, withFilter := FilterPage(context.Background(), &fakeFilter{visible: ids},
		ResourceTypeLoadBalancer, ActionLoadBalancerList, ids)
	_, withoutFilter := FilterPage(context.Background(), nil,
		ResourceTypeLoadBalancer, ActionLoadBalancerList, ids)

	require.Error(t, withFilter)
	require.Error(t, withoutFilter)
	assert.Equal(t, codes.Unauthenticated, status.Code(withFilter),
		"безымянный вызывающий — это UNAUTHENTICATED, а не «модели нет»")
	assert.Equal(t, codes.Unauthenticated, status.Code(withoutFilter),
		"и ответ ему один и тот же независимо от посадки фильтра")
}
