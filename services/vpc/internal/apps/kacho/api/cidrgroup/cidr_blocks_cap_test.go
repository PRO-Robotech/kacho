// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package cidrgroup

import (
	"context"
	"fmt"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/kachomock"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/repomock"
)

// blocks — n законных префиксов одного семейства.
func blocks(n int) []string {
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, fmt.Sprintf("10.%d.0.0/24", i))
	}
	return out
}

// TestCreate_CardinalityCapIsSyncAndPerFamily — потолок входа проверяется
// СИНХРОННО, ДО создания операции, и считается ПО СЕМЕЙСТВАМ.
//
// Синхронность — не деталь: отказ, приехавший ошибкой операции, вызывающий видит
// только после опроса, а к этому моменту идентификатор несозданного ресурса уже
// уехал ему в метаданные.
func TestCreate_CardinalityCapIsSyncAndPerFamily(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	newUC := func() (*CreateCidrGroupUseCase, *kachomock.Repository) {
		repo := kachomock.NewRepository()
		return NewCreateCidrGroupUseCase(repo, &repomock.ProjectClient{OK: true}, repomock.NewOpsRepo()), repo
	}

	t.Run("предел на семейство проходит (положительный контроль)", func(t *testing.T) {
		t.Parallel()
		uc, _ := newUC()
		op, err := uc.Execute(ctx, domain.CidrGroup{
			ProjectID:    "prj-1",
			V4CidrBlocks: blocks(domain.MaxCidrGroupBlocks),
		})
		if err != nil {
			t.Fatalf("ровно предел отвергнут: %v", err)
		}
		if op == nil {
			t.Fatal("операция не создана")
		}
	})

	t.Run("на один больше предела отвергается синхронно", func(t *testing.T) {
		t.Parallel()
		uc, _ := newUC()
		_, err := uc.Execute(ctx, domain.CidrGroup{
			ProjectID:    "prj-1",
			V4CidrBlocks: blocks(domain.MaxCidrGroupBlocks + 1),
		})
		if err == nil {
			t.Fatal("вход сверх предела принят")
		}
		if got := status.Code(err); got != codes.InvalidArgument {
			t.Fatalf("код отказа %v, ожидался INVALID_ARGUMENT (это формат запроса, а не состояние ресурса)", got)
		}
	})

	t.Run("семейства считаются по отдельности", func(t *testing.T) {
		t.Parallel()
		uc, _ := newUC()
		// Обоих по пределу — сумма вдвое больше, и это законно: потолок объявлен
		// НА СЕМЕЙСТВО. Проба ловит проверку, посчитавшую сумму.
		if _, err := uc.Execute(ctx, domain.CidrGroup{
			ProjectID:    "prj-1",
			V4CidrBlocks: blocks(domain.MaxCidrGroupBlocks),
			V6CidrBlocks: []string{"2001:db8::/32"},
		}); err != nil {
			t.Fatalf("потолок посчитан по обоим семействам сразу: %v", err)
		}
	})
}

// TestAddCidrBlocks_CapAndFamilyAreCheckedBeforeTheOperation — тот же потолок на
// глаголе состава, плюс отказ на члене чужого семейства.
func TestAddCidrBlocks_CapAndFamilyAreCheckedBeforeTheOperation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := kachomock.NewRepository()
	uc := NewAddCidrBlocksUseCase(repo, repomock.NewOpsRepo())
	const id = "cdg-0123456789abcdefg"

	t.Run("предел входа проходит (положительный контроль)", func(t *testing.T) {
		// Набора нет — ответ придёт ошибкой операции, а не отказом на входе;
		// предмет пробы в том, что СИНХРОННОГО отказа тут нет.
		if _, err := uc.Execute(ctx, id, blocks(domain.MaxCidrGroupBlocks), nil); err != nil {
			t.Fatalf("вход по пределу отвергнут синхронно: %v", err)
		}
	})

	t.Run("сверх предела — синхронный отказ", func(t *testing.T) {
		_, err := uc.Execute(ctx, id, blocks(domain.MaxCidrGroupBlocks+1), nil)
		if err == nil {
			t.Fatal("вход сверх предела принят")
		}
		if got := status.Code(err); got != codes.InvalidArgument {
			t.Fatalf("код отказа %v, ожидался INVALID_ARGUMENT", got)
		}
	})

	t.Run("член чужого семейства отвергается с именем поля", func(t *testing.T) {
		_, err := uc.Execute(ctx, id, []string{"2001:db8::/32"}, nil)
		if err == nil {
			t.Fatal("v6-префикс принят в поле v4 — смешанный набор стал выразимым")
		}
		if got := status.Code(err); got != codes.InvalidArgument {
			t.Fatalf("код отказа %v, ожидался INVALID_ARGUMENT", got)
		}
		if msg := status.Convert(err).Message(); msg == "" {
			t.Fatal("отказ без текста — вызывающему нечего править")
		}
	})

	t.Run("пустой запрос отвергается", func(t *testing.T) {
		if _, err := uc.Execute(ctx, id, nil, nil); err == nil {
			t.Fatal("глагол без единого члена принят — операция без предмета")
		}
	})
}
