// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package routetable

// granular_route_refusal_test.go — гранулярная правка маршрутов отказывает
// ЯВНО, называя причину и рабочий путь.
//
// # Предмет
//
// `AddRoutes`/`RemoveRoutes`/`UpdateRoute` объявлены контрактом и не были
// переопределены над встроенной заглушкой: вызывающий получал `UNIMPLEMENTED`
// без единого слова о том, почему и что делать вместо. Сайт документации при
// этом описывал все три полностью — с примерами `curl` — и советовал ими
// пользоваться, чтобы не потерять маршруты при полной замене списка.
//
// Реализовать два из трёх нельзя: они адресуют маршрут по идентификатору,
// которого `StaticRoute` не несёт НИ в контракте, НИ в домене, НИ в хранилище
// (`static_routes jsonb` — массив объектов без ключа). Идентичность маршрута —
// отдельный инкремент со своей приёмкой (запись 26 в 07-known-divergences.md).
//
// # Что утверждает проба
//
// Отказ — ЧАСТЬ КОНТРАКТА, поэтому проверяется не только код, но и СООБЩЕНИЕ:
// унаследованный `UNIMPLEMENTED` и названный отказ дают ОДИН И ТОТ ЖЕ код, и
// проба, спрашивающая только код, зеленела бы на снятом переопределении — то
// есть ровно на возврате исходного дефекта.

import (
	"context"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
)

func TestGranularRouteEditingRefusesByName(t *testing.T) {
	h := &Handler{}
	ctx := context.Background()

	cases := []struct {
		what string
		call func() error
	}{
		{"AddRoutes", func() error {
			_, err := h.AddRoutes(ctx, &vpcv1.AddRouteTableRoutesRequest{RouteTableId: "rtb00000000000000001"})
			return err
		}},
		{"RemoveRoutes", func() error {
			_, err := h.RemoveRoutes(ctx, &vpcv1.RemoveRouteTableRoutesRequest{RouteTableId: "rtb00000000000000001"})
			return err
		}},
		{"UpdateRoute", func() error {
			_, err := h.UpdateRoute(ctx, &vpcv1.UpdateRouteTableRouteRequest{RouteTableId: "rtb00000000000000001"})
			return err
		}},
	}

	for _, c := range cases {
		t.Run(c.what, func(t *testing.T) {
			err := c.call()
			if err == nil {
				t.Fatalf("%s ответил успехом — метод не реализован, и успех здесь означает "+
					"молчаливую потерю запроса", c.what)
			}
			st, ok := status.FromError(err)
			if !ok || st.Code() != codes.Unimplemented {
				t.Fatalf("%s: код %v, ожидался Unimplemented", c.what, st.Code())
			}
			msg := st.Message()
			// Отказ обязан назвать ПРИЧИНУ: без неё он неотличим от
			// унаследованного молчаливого, ради снятия которого и написан.
			if !strings.Contains(msg, "static route id") {
				t.Errorf("%s: сообщение %q не называет отсутствующую идентичность маршрута — "+
					"вызывающий не узнаёт, почему отказано", c.what, msg)
			}
			// И РАБОЧИЙ ПУТЬ: отказ без альтернативы оставляет вызывающего ни с чем,
			// хотя правка маршрутов возможна полной заменой набора.
			if !strings.Contains(msg, "static_routes") {
				t.Errorf("%s: сообщение %q не указывает рабочий путь (Update с "+
					"update_mask static_routes)", c.what, msg)
			}
		})
	}
}

// TestInheritedUnimplementedWouldNotSatisfyTheContract — положительный контроль
// к пробе выше: тот же вызов на типе БЕЗ переопределения даёт тот же код, но
// пустое сообщение.
//
// Он существует, чтобы «зелено» выше относилось к переопределению, а не к коду
// ответа: без этой половины проба не отличала бы починку от исходного дефекта.
func TestInheritedUnimplementedWouldNotSatisfyTheContract(t *testing.T) {
	var bare vpcv1.UnimplementedRouteTableServiceServer
	_, err := bare.AddRoutes(context.Background(), &vpcv1.AddRouteTableRoutesRequest{})
	st, _ := status.FromError(err)
	if st.Code() != codes.Unimplemented {
		t.Fatalf("встроенная заглушка отвечает %v — предпосылка пробы изменилась", st.Code())
	}
	if strings.Contains(st.Message(), "static route id") {
		t.Fatal("встроенная заглушка уже называет причину — значит проба выше проверяет " +
			"не переопределение, а поведение сгенерированного кода")
	}
	t.Logf("контроль: встроенная заглушка отвечает %v с сообщением %q", st.Code(), st.Message())
}
