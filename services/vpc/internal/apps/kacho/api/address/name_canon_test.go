// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package address

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/kachomock"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/repomock"
)

// Проверки единственной формы имени (#715) для Address: строка ресурса не может
// нести пустое имя, и снять имя правкой нельзя.

func nameCanonFixture(t *testing.T) (*CreateAddressUseCase, *repomock.OpsRepo) {
	t.Helper()
	kr := kachomock.NewRepository()
	sr := repomock.NewSubnetRepo()
	or := repomock.NewOpsRepo()
	return NewCreateAddressUseCase(kr, sr, &repomock.ProjectClient{OK: true}, or, nil), or
}

func createUnnamedAddress(t *testing.T, uc *CreateAddressUseCase,
	or *repomock.OpsRepo, ip string) *vpcv1.Address {
	t.Helper()
	op, err := uc.Execute(context.Background(), CreateInput{
		ProjectID:    "f1",
		ExternalSpec: &ExternalAddrSpec{Address: ip, ZoneID: "zone-a"},
	})
	require.NoError(t, err)
	saved := repomock.AwaitOpDone(t, or, op.ID)
	require.Nil(t, saved.Error, "создание без имени обязано пройти: пустое имя означает «назови сам»")
	var got vpcv1.Address
	require.NoError(t, saved.Response.UnmarshalTo(&got))
	return &got
}

// TestCreate_EmptyName_WritesIdDerivedDefault — создание без имени записывает
// имя, производное от идентификатора, а не пустую строку.
func TestCreate_EmptyName_WritesIdDerivedDefault(t *testing.T) {
	uc, or := nameCanonFixture(t)
	got := createUnnamedAddress(t, uc, or, "203.0.113.10")

	assert.NotEmpty(t, got.Name, "строка ресурса не может нести пустое имя")
	assert.Equal(t, got.Id, got.Name, "умолчание — сам идентификатор (pkg/validate.NameOrDefault)")
}

// TestCreate_TwoEmptyNames_BothSucceedWithDistinctNames — два безымянных
// создания в одном проекте проходят ОБА и получают РАЗНЫЕ имена. Умолчание,
// производное от чего-либо, кроме идентификатора, столкнулось бы на уникальности
// (project, name), и второе создание отвергалось бы.
func TestCreate_TwoEmptyNames_BothSucceedWithDistinctNames(t *testing.T) {
	uc, or := nameCanonFixture(t)
	first := createUnnamedAddress(t, uc, or, "203.0.113.10")
	second := createUnnamedAddress(t, uc, or, "203.0.113.11")

	assert.NotEqual(t, first.Name, second.Name,
		"два безымянных создания обязаны получить разные имена")
}

// TestCreate_NameStillValidated — положительный и отрицательный контроль формы.
func TestCreate_NameStillValidated(t *testing.T) {
	uc, or := nameCanonFixture(t)

	op, err := uc.Execute(context.Background(), CreateInput{
		ProjectID: "f1", Name: "addr-1",
		ExternalSpec: &ExternalAddrSpec{Address: "203.0.113.10", ZoneID: "zone-a"},
	})
	require.NoError(t, err, "законное имя обязано проходить")
	require.Nil(t, repomock.AwaitOpDone(t, or, op.ID).Error)

	_, err = uc.Execute(context.Background(), CreateInput{
		ProjectID: "f1", Name: "Bad_Name",
		ExternalSpec: &ExternalAddrSpec{Address: "203.0.113.11", ZoneID: "zone-a"},
	})
	require.Error(t, err, "заглавные и подчёркивание формой не приняты")
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

// TestUpdate_MaskNamesName_EmptyRejected — маска НАЗВАЛА имя, значение пусто:
// отказ с именем поля. Рядом — положительный контроль той же маской.
func TestUpdate_MaskNamesName_EmptyRejected(t *testing.T) {
	err := validateAddressUpdate(UpdateInput{UpdateMask: []string{"name"}, Name: ""})
	require.Error(t, err, "снять имя правкой нельзя — имени без значения не бывает")
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	assert.True(t, nameCanonRefusalNamesField(t, err, "name"), "отказ обязан называть поле")

	require.NoError(t, validateAddressUpdate(UpdateInput{
		UpdateMask: []string{"name"}, Name: "addr-2",
	}), "положительный контроль: та же маска с законным именем проходит")
}

// TestUpdate_EmptyMask_EmptyNameStillAccepted — полная правка без маски пустое
// имя НЕ отвергает: в proto3 «поле не прислано» и «поле пусто» неразличимы.
func TestUpdate_EmptyMask_EmptyNameStillAccepted(t *testing.T) {
	require.NoError(t, validateAddressUpdate(UpdateInput{Name: ""}))
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
// Предмет — дыра, пережившая проверку входа: проверка правильно пропускает
// пустое имя при пустой маске (в proto3 «не прислано» и «пусто» неразличимы), а
// применение записывало эту пустоту в строку. То есть ресурс всё равно мог
// остаться без имени — и после миграции 715001, поставившей на столбец
// ограничение формы, это уже не «странное имя», а отказ БАЗЫ на пути, где
// вызывающий не сделал ничего неверного.
func TestApply_EmptyMask_EmptyNameKeepsCurrent(t *testing.T) {
	cur := domain.Address{Name: "addr-current"}
	applyAddressMask(&cur, UpdateInput{Name: ""})
	assert.Equal(t, domain.RcNameVPC("addr-current"), cur.Name, "полная правка без имени обязана сохранить текущее имя")
	assert.NotEmpty(t, string(cur.Name), "ресурса без имени не бывает")
}

// TestApply_EmptyMask_NewNameApplied — положительный контроль рядом с отрицанием:
// та же полная правка с НЕПУСТЫМ именем имя меняет. Без этой половины первая
// проба зеленела бы и на применении, которое имя не трогает вовсе.
func TestApply_EmptyMask_NewNameApplied(t *testing.T) {
	cur := domain.Address{Name: "addr-current"}
	applyAddressMask(&cur, UpdateInput{Name: "addr-new"})
	assert.Equal(t, domain.RcNameVPC("addr-new"), cur.Name, "непустое имя при полной правке обязано примениться")
}

// TestApply_MaskNamesName_NewNameApplied — явная маска имя применяет (пустое до
// применения не доходит — его отвергает проверка входа).
func TestApply_MaskNamesName_NewNameApplied(t *testing.T) {
	cur := domain.Address{Name: "addr-current"}
	applyAddressMask(&cur, UpdateInput{UpdateMask: []string{"name"}, Name: "addr-new"})
	assert.Equal(t, domain.RcNameVPC("addr-new"), cur.Name, "маска, назвавшая имя, обязана его применить")
}
