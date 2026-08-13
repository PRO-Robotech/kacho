// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package cidrgroup

import (
	"context"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/kachomock"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/repomock"
)

// seedGroup создаёт набор через use-case и возвращает его идентификатор.
func seedGroup(t *testing.T, kr *kachomock.Repository, or *repomock.OpsRepo) string {
	t.Helper()
	op, err := NewCreateCidrGroupUseCase(kr, &repomock.ProjectClient{OK: true}, or).
		Execute(context.Background(), domain.CidrGroup{
			ProjectID:    "prj-b3n7k1x9q2m5t8",
			Name:         domain.RcNameVPC("office-egress"),
			V4CidrBlocks: []string{"203.0.113.0/24"},
		})
	if err != nil {
		t.Fatalf("посев набора не удался: %v", err)
	}
	if op.Error != nil {
		t.Fatalf("операция посева завершилась ошибкой: %v", op.Error)
	}
	var g vpcv1.CidrGroup
	if uerr := op.Response.UnmarshalTo(&g); uerr != nil {
		t.Fatalf("тело операции посева не разбирается: %v", uerr)
	}
	return g.Id
}

// TestUpdate_ImmutableAndCompositionFieldsAreRefusedByName — маска правки.
//
// Проба закрепляет ДВА разных отказа, и различие между ними несущее:
//
//   - `id` / `project_id` / `created_at` НЕИЗМЕНЯЕМЫ — они адресуют ресурс и его
//     владение на всю жизнь ресурса, операции смены не существует;
//   - `v4_cidr_blocks` / `v6_cidr_blocks` изменяемы, но НЕ ЗДЕСЬ — у состава свои
//     глаголы, и отказ обязан отправить вызывающего к ним, а не объявлять состав
//     вечным.
//
// Обе ветви стоят ДО проверки маски по известному набору: иначе поле отверглось
// бы родовым «unknown field», и вызывающий не узнал бы ни что оно неизменяемо,
// ни куда идти за правкой состава.
func TestUpdate_ImmutableAndCompositionFieldsAreRefusedByName(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	kr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	id := seedGroup(t, kr, or)
	uc := NewUpdateCidrGroupUseCase(kr, or)

	t.Run("косметическая правка проходит (положительный контроль)", func(t *testing.T) {
		op, err := uc.Execute(ctx, UpdateInput{
			CidrGroupID: id,
			CidrGroup:   domain.CidrGroup{Name: domain.RcNameVPC("renamed")},
			UpdateMask:  []string{"name"},
		})
		if err != nil {
			t.Fatalf("правка имени отвергнута — отрицания ниже ничего не доказывают: %v", err)
		}
		if op.Error != nil {
			t.Fatalf("операция правки имени завершилась ошибкой: %v", op.Error)
		}
	})

	immutable := []string{"id", "project_id", "created_at", "cidr_block_count", "used_by"}
	for _, field := range immutable {
		t.Run("неизменяемое поле "+field, func(t *testing.T) {
			_, err := uc.Execute(ctx, UpdateInput{CidrGroupID: id, UpdateMask: []string{field}})
			if err == nil {
				t.Fatalf("поле %q принято маской правки", field)
			}
			if got := status.Code(err); got != codes.InvalidArgument {
				t.Fatalf("код отказа %v, ожидался INVALID_ARGUMENT", got)
			}
			want := field + " is immutable after CidrGroup.Create"
			if msg := status.Convert(err).Message(); msg != want {
				t.Fatalf("текст отказа %q, ожидался конвенционный %q", msg, want)
			}
		})
	}

	for _, field := range []string{"v4_cidr_blocks", "v6_cidr_blocks"} {
		t.Run("состав правится глаголами, поле "+field, func(t *testing.T) {
			_, err := uc.Execute(ctx, UpdateInput{CidrGroupID: id, UpdateMask: []string{field}})
			if err == nil {
				t.Fatalf("поле состава %q принято маской правки — появился второй способ менять набор", field)
			}
			if got := status.Code(err); got != codes.InvalidArgument {
				t.Fatalf("код отказа %v, ожидался INVALID_ARGUMENT", got)
			}
			msg := status.Convert(err).Message()
			if !strings.Contains(msg, "use AddCidrBlocks/RemoveCidrBlocks") {
				t.Fatalf("отказ %q не называет глаголы состава — вызывающему некуда идти", msg)
			}
			if strings.Contains(msg, "immutable") {
				t.Fatalf("отказ %q объявляет состав неизменяемым, хотя он изменяем глаголами", msg)
			}
		})
	}

	t.Run("неизвестное поле маски отвергается", func(t *testing.T) {
		_, err := uc.Execute(ctx, UpdateInput{CidrGroupID: id, UpdateMask: []string{"no_such_field"}})
		if err == nil {
			t.Fatal("неизвестное поле принято маской")
		}
		if got := status.Code(err); got != codes.InvalidArgument {
			t.Fatalf("код отказа %v, ожидался INVALID_ARGUMENT", got)
		}
	})
}

// TestUpdate_MalformedIDIsRefusedFirstStatement — малформированный
// идентификатор отвергается ФОРМАТОМ, а не промахом.
//
// Разница видна вызывающему: «не найдено» на строке, которая идентификатором
// быть не может, — утверждение об отсутствии ресурса, а не о неверном вводе.
func TestUpdate_MalformedIDIsRefusedFirstStatement(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	kr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	uc := NewUpdateCidrGroupUseCase(kr, or)

	_, err := uc.Execute(ctx, UpdateInput{CidrGroupID: "not-an-id", UpdateMask: []string{"name"}})
	if err == nil {
		t.Fatal("малформированный идентификатор принят")
	}
	if got := status.Code(err); got != codes.InvalidArgument {
		t.Fatalf("код отказа %v, ожидался INVALID_ARGUMENT", got)
	}

	// Положительный контроль: well-formed идентификатор несуществующего набора
	// формат проходит — значит отказ выше про ФОРМУ, а не про «всё отвергаем».
	op, err := uc.Execute(ctx, UpdateInput{
		CidrGroupID: "cdg-0123456789abcdefg",
		CidrGroup:   domain.CidrGroup{Name: domain.RcNameVPC("x")},
		UpdateMask:  []string{"name"},
	})
	if err != nil {
		t.Fatalf("корректный по форме идентификатор отвергнут синхронно: %v", err)
	}
	if op.Error == nil {
		t.Fatal("правка несуществующего набора завершилась успехом")
	}
}
