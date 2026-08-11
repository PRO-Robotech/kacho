// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package handler

// declared_but_absent_refusal_test.go — RPC, объявленные контрактом и не
// реализованные, отказывают ЯВНО: с причиной и с рабочим путём.
//
// # Предмет
//
// Семь методов `InstanceService` не были переопределены над встроенной
// заглушкой. Страница документации ресурса причину называет честно — но
// вызывающий по gRPC её не видит: ему приходит `method X not implemented`, из
// чего не следует ни почему, ни что делать вместо. Между «читатель сайта» и
// «клиент API» разница существенная: второй узнаёт об ограничении в момент
// отказа, и отказ — единственное место, где ему можно об этом сказать.
//
// # Почему проба утверждает СООБЩЕНИЕ
//
// Унаследованный отказ и названный дают ОДИН И ТОТ ЖЕ код `UNIMPLEMENTED`.
// Проба, спрашивающая только код, зеленела бы на снятом переопределении — то
// есть ровно на возврате исходного состояния.

import (
	"context"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	accesspb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/access"
	computev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/compute/v1"
)

func TestDeclaredButAbsentRPCsRefuseByName(t *testing.T) {
	h := &InstanceHandler{}
	ctx := context.Background()

	cases := []struct {
		what string
		// owner — имя владельца возможности, которое отказ обязан назвать;
		// вызывающему нужен адрес, а не только «нельзя».
		owner string
		call  func() error
	}{
		{"AddOneToOneNat", "vpc", func() error {
			_, err := h.AddOneToOneNat(ctx, &computev1.AddInstanceOneToOneNatRequest{})
			return err
		}},
		{"RemoveOneToOneNat", "vpc", func() error {
			_, err := h.RemoveOneToOneNat(ctx, &computev1.RemoveInstanceOneToOneNatRequest{})
			return err
		}},
		{"UpdateNetworkInterface", "vpc", func() error {
			_, err := h.UpdateNetworkInterface(ctx, &computev1.UpdateInstanceNetworkInterfaceRequest{})
			return err
		}},
		{"Relocate", "storage", func() error {
			_, err := h.Relocate(ctx, &computev1.RelocateInstanceRequest{})
			return err
		}},
		{"ListAccessBindings", "iam", func() error {
			_, err := h.ListAccessBindings(ctx, &accesspb.ListAccessBindingsRequest{})
			return err
		}},
		{"SetAccessBindings", "iam", func() error {
			_, err := h.SetAccessBindings(ctx, &accesspb.SetAccessBindingsRequest{})
			return err
		}},
		{"UpdateAccessBindings", "iam", func() error {
			_, err := h.UpdateAccessBindings(ctx, &accesspb.UpdateAccessBindingsRequest{})
			return err
		}},
	}

	for _, c := range cases {
		t.Run(c.what, func(t *testing.T) {
			err := c.call()
			if err == nil {
				t.Fatalf("%s ответил успехом — метода нет, и успех означает молчаливую "+
					"потерю запроса", c.what)
			}
			st, ok := status.FromError(err)
			if !ok || st.Code() != codes.Unimplemented {
				t.Fatalf("%s: код %v, ожидался Unimplemented", c.what, st.Code())
			}
			msg := st.Message()
			if strings.Contains(msg, "not implemented") && !strings.Contains(msg, c.owner) {
				t.Fatalf("%s: сообщение %q — унаследованное от заглушки: причина не названа, "+
					"вызывающий не узнаёт, где живёт возможность", c.what, msg)
			}
			if !strings.Contains(msg, c.owner) {
				t.Errorf("%s: сообщение %q не называет владельца возможности (%s) — отказ без "+
					"адреса оставляет вызывающего ни с чем", c.what, msg, c.owner)
			}
		})
	}
	t.Logf("осмотрено: объявленных-и-отсутствующих RPC %d", len(cases))
}
