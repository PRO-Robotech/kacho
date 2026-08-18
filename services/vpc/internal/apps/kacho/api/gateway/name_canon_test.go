// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package gateway

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

// Проверки единственной формы имени (#715) для Gateway.
//
// Шлюз конвертировали на канон ПЕРВЫМ — как образец для остальных восьми, — и
// именно он единственный остался без проб. Обнаружилось это не обзором, а
// инъекцией: снятие механизма покраснило все восемь пакетов и НЕ покраснило
// шлюз. Образец, за которым ничего не следит, переживает то, что им
// обозначалось.

func nameCanonFixture(t *testing.T) (*CreateGatewayUseCase, *repomock.OpsRepo) {
	t.Helper()
	kr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	return NewCreateGatewayUseCase(kr, &repomock.ProjectClient{OK: true}, or), or
}

func createUnnamedGateway(t *testing.T, uc *CreateGatewayUseCase, or *repomock.OpsRepo) *vpcv1.Gateway {
	t.Helper()
	op, err := uc.Execute(context.Background(), domain.Gateway{
		ProjectID:   "f1",
		GatewayType: domain.GatewayTypeEgressOnly,
		SubnetID:    seedSubnetID,
	})
	require.NoError(t, err)
	saved := repomock.AwaitOpDone(t, or, op.ID)
	require.Nil(t, saved.Error, "создание без имени обязано пройти: пустое имя означает «назови сам»")
	var got vpcv1.Gateway
	require.NoError(t, saved.Response.UnmarshalTo(&got))
	return &got
}

// TestCreate_EmptyName_WritesIdDerivedDefault — создание без имени записывает
// имя, производное от идентификатора, а не пустую строку.
func TestCreate_EmptyName_WritesIdDerivedDefault(t *testing.T) {
	uc, or := nameCanonFixture(t)
	got := createUnnamedGateway(t, uc, or)

	assert.NotEmpty(t, got.Name, "строка ресурса не может нести пустое имя")
	assert.Equal(t, got.Id, got.Name, "умолчание — сам идентификатор (pkg/validate.NameOrDefault)")
}

// TestCreate_TwoEmptyNames_BothSucceedWithDistinctNames — два безымянных
// создания проходят ОБА и получают РАЗНЫЕ имена.
func TestCreate_TwoEmptyNames_BothSucceedWithDistinctNames(t *testing.T) {
	uc, or := nameCanonFixture(t)
	first := createUnnamedGateway(t, uc, or)
	second := createUnnamedGateway(t, uc, or)

	assert.NotEqual(t, first.Name, second.Name,
		"два безымянных создания обязаны получить разные имена")
}

// TestCreate_NameStillValidated — положительный и отрицательный контроль формы.
func TestCreate_NameStillValidated(t *testing.T) {
	uc, or := nameCanonFixture(t)

	op, err := uc.Execute(context.Background(), domain.Gateway{
		ProjectID: "f1", Name: domain.RcNameVPC("gw-1"),
		GatewayType: domain.GatewayTypeEgressOnly, SubnetID: seedSubnetID,
	})
	require.NoError(t, err, "законное имя обязано проходить")
	require.Nil(t, repomock.AwaitOpDone(t, or, op.ID).Error)

	_, err = uc.Execute(context.Background(), domain.Gateway{
		ProjectID: "f1", Name: domain.RcNameVPC("Bad_Name"),
		GatewayType: domain.GatewayTypeEgressOnly, SubnetID: seedSubnetID,
	})
	require.Error(t, err, "заглавные и подчёркивание формой не приняты")
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

// TestUpdate_MaskNamesName_EmptyRejected — маска НАЗВАЛА имя, значение пусто:
// отказ с именем поля. Рядом — положительный контроль той же маской.
func TestUpdate_MaskNamesName_EmptyRejected(t *testing.T) {
	err := validateGatewayUpdate(UpdateInput{
		UpdateMask: []string{"name"},
		Gateway:    domain.Gateway{Name: ""},
	})
	require.Error(t, err, "снять имя правкой нельзя — имени без значения не бывает")
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	assert.True(t, nameCanonRefusalNamesField(t, err, "name"), "отказ обязан называть поле")

	require.NoError(t, validateGatewayUpdate(UpdateInput{
		UpdateMask: []string{"name"},
		Gateway:    domain.Gateway{Name: "gw-2"},
	}), "положительный контроль: та же маска с законным именем проходит")
}

// TestUpdate_EmptyMask_EmptyNameStillAccepted — полная правка без маски пустое
// имя НЕ отвергает.
//
// Это ПОЧИНКА, а не подтверждение: до неё шлюз раскрывал пустую маску в
// `{name, description, labels}` и звал строгую проверку, поэтому полная правка
// без имени отвергалась — у восьми остальных ресурсов такого поведения нет.
// Двух контрактов у одного глагола не бывает.
func TestUpdate_EmptyMask_EmptyNameStillAccepted(t *testing.T) {
	require.NoError(t, validateGatewayUpdate(UpdateInput{Gateway: domain.Gateway{Name: ""}}))
}

// TestApply_EmptyMask_EmptyNameKeepsCurrent — полная правка, НЕ назвавшая имя,
// имя не стирает.
func TestApply_EmptyMask_EmptyNameKeepsCurrent(t *testing.T) {
	cur := domain.Gateway{Name: "gw-current"}
	applyGatewayMask(&cur, UpdateInput{Gateway: domain.Gateway{Name: ""}})
	assert.Equal(t, domain.RcNameVPC("gw-current"), cur.Name,
		"полная правка без имени обязана сохранить текущее имя")
	assert.NotEmpty(t, string(cur.Name), "ресурса без имени не бывает")
}

// TestApply_EmptyMask_NewNameApplied — положительный контроль рядом.
func TestApply_EmptyMask_NewNameApplied(t *testing.T) {
	cur := domain.Gateway{Name: "gw-current"}
	applyGatewayMask(&cur, UpdateInput{Gateway: domain.Gateway{Name: "gw-new"}})
	assert.Equal(t, domain.RcNameVPC("gw-new"), cur.Name,
		"непустое имя при полной правке обязано примениться")
}

// TestApply_MaskNamesName_NewNameApplied — явная маска имя применяет.
func TestApply_MaskNamesName_NewNameApplied(t *testing.T) {
	cur := domain.Gateway{Name: "gw-current"}
	applyGatewayMask(&cur, UpdateInput{
		UpdateMask: []string{"name"}, Gateway: domain.Gateway{Name: "gw-new"},
	})
	assert.Equal(t, domain.RcNameVPC("gw-new"), cur.Name,
		"маска, назвавшая имя, обязана его применить")
}

// nameCanonRefusalNamesField — назвал ли отказ поле по имени.
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
