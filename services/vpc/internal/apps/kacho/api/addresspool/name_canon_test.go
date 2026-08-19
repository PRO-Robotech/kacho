// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package addresspool

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	kachorepo "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/kachomock"
)

// Проверки единственной формы имени (#715) для AddressPool: строка ресурса не
// может нести пустое имя, и снять имя правкой нельзя.
//
// Создание пула синхронно и разделено на «решить» и «записать»; идентификатор
// чеканит первая половина, поэтому и подстановка умолчания живёт там же — здесь
// проверяется именно её результат.

func nameCanonCreateReq(name string, cidr string) CreatePoolReq {
	return CreatePoolReq{
		Name:         name,
		Kind:         domain.AddressPoolKindExternalPublic,
		V4CIDRBlocks: []string{cidr},
	}
}

// TestCreate_EmptyName_WritesIdDerivedDefault — создание без имени даёт имя,
// производное от идентификатора, а не пустую строку.
func TestCreate_EmptyName_WritesIdDerivedDefault(t *testing.T) {
	uc := NewCreateAddressPoolUseCase(kachomock.NewRepository(), nil)

	p, err := uc.Validate(context.Background(), nameCanonCreateReq("", "203.0.113.0/24"))
	require.NoError(t, err, "создание без имени обязано пройти: пустое имя означает «назови сам»")
	assert.NotEmpty(t, string(p.Name), "строка ресурса не может нести пустое имя")
	assert.Equal(t, p.ID, string(p.Name), "умолчание — сам идентификатор (pkg/validate.NameOrDefault)")
}

// TestCreate_TwoEmptyNames_BothSucceedWithDistinctNames — два безымянных
// создания проходят ОБА и получают РАЗНЫЕ имена. Умолчание, производное от
// чего-либо, кроме идентификатора, столкнулось бы на уникальности (project,
// name), и второе создание отвергалось бы.
func TestCreate_TwoEmptyNames_BothSucceedWithDistinctNames(t *testing.T) {
	uc := NewCreateAddressPoolUseCase(kachomock.NewRepository(), nil)

	first, err := uc.Validate(context.Background(), nameCanonCreateReq("", "203.0.113.0/24"))
	require.NoError(t, err)
	second, err := uc.Validate(context.Background(), nameCanonCreateReq("", "198.51.100.0/24"))
	require.NoError(t, err)

	assert.NotEqual(t, string(first.Name), string(second.Name),
		"два безымянных создания обязаны получить разные имена")
}

// TestCreate_NameStillValidated — положительный и отрицательный контроль формы.
func TestCreate_NameStillValidated(t *testing.T) {
	uc := NewCreateAddressPoolUseCase(kachomock.NewRepository(), nil)

	_, err := uc.Validate(context.Background(), nameCanonCreateReq("pool-1", "203.0.113.0/24"))
	require.NoError(t, err, "законное имя обязано проходить")

	_, err = uc.Validate(context.Background(), nameCanonCreateReq("Bad_Name", "203.0.113.0/24"))
	require.Error(t, err, "заглавные и подчёркивание формой не приняты")
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

// TestUpdate_MaskNamesName_EmptyRejected — маска НАЗВАЛА имя, значение пусто:
// отказ с именем поля. Рядом — положительный контроль той же маской.
func TestUpdate_MaskNamesName_EmptyRejected(t *testing.T) {
	uc := NewUpdateAddressPoolUseCase(kachomock.NewRepository())

	err := uc.Validate(UpdatePoolReq{ID: "apl-x", UpdateMask: []string{"name"}, Name: ""})
	require.Error(t, err, "снять имя правкой нельзя — имени без значения не бывает")
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	assert.True(t, nameCanonRefusalNamesField(t, err, "name"), "отказ обязан называть поле")

	require.NoError(t, uc.Validate(UpdatePoolReq{
		ID: "apl-x", UpdateMask: []string{"name"}, Name: "pool-2",
	}), "положительный контроль: та же маска с законным именем проходит")
}

// TestUpdate_EmptyMask_EmptyNameStillAccepted — полная правка без маски пустое
// имя на этой ступени НЕ отвергает: в proto3 «поле не прислано» и «поле пусто»
// неразличимы. Форму применённой записи проверяет `cur.Validate()` в Execute.
func TestUpdate_EmptyMask_EmptyNameStillAccepted(t *testing.T) {
	uc := NewUpdateAddressPoolUseCase(kachomock.NewRepository())
	require.NoError(t, uc.Validate(UpdatePoolReq{ID: "apl-x", Name: ""}))
}

// nameCanonRefusalNamesField — назвал ли отказ поле по имени. Утверждать надо
// ИМЕННО это, а не только код: `InvalidArgument` возвращает вся проверка входа,
// и по одному коду вызывающий не узнает, что именно прислал неверно.
func nameCanonRefusalNamesField(t *testing.T, err error, field string) bool {
	t.Helper()
	st, ok := status.FromError(err)
	require.True(t, ok, "ошибка обязана быть gRPC-статусом")
	for _, d := range st.Details() {
		br, isBR := d.(*errdetails.BadRequest)
		if !isBR {
			continue
		}
		for _, v := range br.GetFieldViolations() {
			if v.GetField() == field {
				return true
			}
		}
	}
	return false
}

// TestApply_EmptyMask_EmptyNameKeepsCurrent — полная правка, НЕ назвавшая имя,
// имя не стирает.
//
// У пула применение маски живёт ВНУТРИ `Execute`, а не отдельной функцией,
// поэтому проба идёт через `Execute` на засеянной строке — иначе она проверяла
// бы не тот код.
func TestApply_EmptyMask_EmptyNameKeepsCurrent(t *testing.T) {
	kr, poolID := seedPoolForNameCanon(t, "pool-current")
	uc := NewUpdateAddressPoolUseCase(kr)

	got, err := uc.Execute(context.Background(), UpdatePoolReq{ID: poolID, Description: "d"})
	require.NoError(t, err, "полная правка без имени обязана проходить")
	assert.Equal(t, domain.RcNameVPC("pool-current"), got.Name,
		"полная правка без имени обязана сохранить текущее имя")
	assert.NotEmpty(t, string(got.Name), "ресурса без имени не бывает")
}

// TestApply_EmptyMask_NewNameApplied — положительный контроль рядом: та же
// полная правка с НЕПУСТЫМ именем имя меняет. Без этой половины первая проба
// зеленела бы и на реализации, которая имя не применяет вовсе.
func TestApply_EmptyMask_NewNameApplied(t *testing.T) {
	kr, poolID := seedPoolForNameCanon(t, "pool-current")
	uc := NewUpdateAddressPoolUseCase(kr)

	got, err := uc.Execute(context.Background(), UpdatePoolReq{ID: poolID, Name: "pool-new"})
	require.NoError(t, err)
	assert.Equal(t, domain.RcNameVPC("pool-new"), got.Name,
		"непустое имя при полной правке обязано примениться")
}

// TestApply_MaskNamesName_NewNameApplied — явная маска имя применяет.
func TestApply_MaskNamesName_NewNameApplied(t *testing.T) {
	kr, poolID := seedPoolForNameCanon(t, "pool-current")
	uc := NewUpdateAddressPoolUseCase(kr)

	got, err := uc.Execute(context.Background(), UpdatePoolReq{
		ID: poolID, UpdateMask: []string{"name"}, Name: "pool-new",
	})
	require.NoError(t, err)
	assert.Equal(t, domain.RcNameVPC("pool-new"), got.Name,
		"маска, назвавшая имя, обязана его применить")
}

// seedPoolForNameCanon кладёт готовую строку пула прямо в state mock-репозитория:
// предмет проб — применение маски, а не путь создания.
func seedPoolForNameCanon(t *testing.T, name string) (*kachomock.Repository, string) {
	t.Helper()
	kr := kachomock.NewRepository()
	now := time.Now().UTC()
	p := domain.AddressPool{
		ID:           ids.NewID(ids.PrefixAddressPool),
		Name:         domain.RcNameVPC(name),
		Kind:         domain.AddressPoolKindExternalPublic,
		V4CIDRBlocks: []string{"203.0.113.0/24"},
	}
	kr.SeedAddressPool(&kachorepo.AddressPoolRecord{AddressPool: p, CreatedAt: now, ModifiedAt: now})
	return kr, p.ID
}
