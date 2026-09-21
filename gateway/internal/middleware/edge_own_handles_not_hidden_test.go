// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// edge_own_handles_not_hidden_test.go — укрытие внешнего слушателя не смеет
// накрыть СОБСТВЕННЫЕ ручки края.
//
// # Почему эта проба существует
//
// `phaseUnservedOnThisListener` укрывает всё, чего внешний слушатель не
// обслуживает, и признак «не обслуживает» берёт из ИМЕНИ МЕТОДА: имя службы
// `Internal*` наружу не выставляется. Для маршрутов, выведенных из контракта,
// это верно.
//
// Для собственных ручек края — неверно, и именно так этот дефект и был заведён
// в первой редакции починки. Ручка потока изменений ресурсов висит на внешнем
// слушателе (её вешает композиционный корень, `httpMux.Handle`), а имя метода у
// неё НАСТОЯЩЕЕ — внутренней службы фундамента,
// `corelib.subscription.InternalSubscriptionService/Subscribe` (см. довод в
// шапке rest_route_edge.go: второе имя объявляло бы право, которого не
// спрашивает ни один вызов). Предикат по одному имени объявил бы ручку
// необслуживаемой, и поток изменений исчез бы для консоли целиком — а снаружи
// это выглядело бы как «стало безопаснее».
//
// # Почему обходом, а не перечнем
//
// Перечень ручек здесь разошёлся бы с объявлением ровно тогда, когда в
// объявление добавят вторую: новая ручка получила бы укрытие, а проба осталась
// бы зелёной. Предмет берётся из `edgeRestRoutes` — того же объявления, из
// которого строятся и сами маршруты.
package middleware

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/PRO-Robotech/kacho/gateway/internal/listenerorigin"
)

// TestEdgeOwnHandlesAreNotHiddenOnTheExternalListener — ни одна собственная
// ручка края не признаётся необслуживаемой на внешнем слушателе.
func TestEdgeOwnHandlesAreNotHiddenOnTheExternalListener(t *testing.T) {
	if len(edgeRestRoutes) == 0 {
		t.Fatal("объявление собственных ручек края пусто — пустой обход вердиктом не является")
	}
	rr := NewRestRouter()

	// Премиса: предикат вообще СПОСОБЕН сказать «не обслуживается». Без неё
	// зелёное ниже значило бы лишь, что он истинен на всём.
	if servedOnExternalListener("kacho.cloud.vpc.v1.InternalAddressPoolService/Get") {
		t.Fatal("предикат признал обслуживаемым метод внутренней службы контракта — " +
			"он не различает ничего, и его согласие на ручках края ничего не значит")
	}

	for _, rt := range edgeRestRoutes {
		// Маршрут ручки обязан разбираться в её же имя: полоса прав ищет запись
		// каталога по имени, и путь, который в него не разбирается, промахивается
		// мимо каталога.
		fqn, ok := rr.Resolve(rt.Method, rt.Template)
		if !ok || fqn != rt.FQN {
			t.Errorf("%s %s разбирается в %q (ok=%v), а объявлен как %q",
				rt.Method, rt.Template, fqn, ok, rt.FQN)
			continue
		}
		if !servedOnExternalListener(fqn) {
			t.Errorf("собственная ручка края %s %s (%s) признана НЕОБСЛУЖИВАЕМОЙ на внешнем "+
				"слушателе — укрытие накрыло бы её, и наружу она исчезла бы целиком",
				rt.Method, rt.Template, fqn)
		}
	}
	t.Logf("перепись: собственных ручек края %d", len(edgeRestRoutes))
}

// TestEdgeOwnHandleSurvivesTheUnservedPhase — то же свойство на уровне
// НАБЛЮДАЕМОГО: запрос к собственной ручке края проходит фазу укрытия и идёт
// дальше, к записи каталога, а не получает «маршрута нет».
func TestEdgeOwnHandleSurvivesTheUnservedPhase(t *testing.T) {
	if len(edgeRestRoutes) == 0 {
		t.Fatal("объявление собственных ручек края пусто — предмета нет")
	}
	mw := &AuthzMiddleware{
		cfg: AuthzMiddlewareConfig{
			RestRouter: NewRestRouter(),
			Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		},
		metrics: NewAuthzMetrics(),
	}

	for _, rt := range edgeRestRoutes {
		// Внешнее происхождение — умолчание fail-closed, маркера нет.
		if _, handled := mw.phaseUnservedOnThisListener(decisionRequest{
			FQN:     rt.FQN,
			HTTPReq: httptest.NewRequest(rt.Method, rt.Template, nil),
		}); handled {
			t.Errorf("фаза укрытия перехватила собственную ручку края %s %s (%s)",
				rt.Method, rt.Template, rt.FQN)
		}
	}

	// ЗАКОННЫЙ БЛИЗНЕЦ: путь внутренней службы КОНТРАКТА та же фаза обязана
	// перехватить. Без него молчание выше не отличить от фазы, которая не
	// перехватывает ничего.
	twin := httptest.NewRequest(http.MethodPost, "/compute/v1/internal/machineTypes", nil)
	if _, handled := mw.phaseUnservedOnThisListener(decisionRequest{
		FQN:     "kacho.cloud.compute.v1.InternalMachineTypeService/Create",
		HTTPReq: twin,
	}); !handled {
		t.Fatal("фаза укрытия пропустила путь внутренней службы контракта — " +
			"её молчание на ручках края получено даром")
	}

	// И ВТОРОЙ БЛИЗНЕЦ: на ВНУТРЕННЕМ происхождении тот же путь фаза не трогает.
	internal := httptest.NewRequest(http.MethodPost, "/compute/v1/internal/machineTypes", nil)
	internal = internal.WithContext(listenerorigin.WithInternal(internal.Context()))
	if _, handled := mw.phaseUnservedOnThisListener(decisionRequest{
		FQN:     "kacho.cloud.compute.v1.InternalMachineTypeService/Create",
		HTTPReq: internal,
	}); handled {
		t.Fatal("фаза укрытия сработала на ВНУТРЕННЕМ слушателе — там различимость законна")
	}
}
