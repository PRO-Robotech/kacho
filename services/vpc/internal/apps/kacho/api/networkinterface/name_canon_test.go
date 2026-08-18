// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package networkinterface

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	kachorepo "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/kachomock"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/repomock"
)

// Проверки единственной формы имени (#715) для NetworkInterface: строка ресурса
// не может нести пустое имя, и снять имя правкой нельзя.

const nameCanonSubnetID = "nc1sub1"

func nameCanonFixture(t *testing.T) (*CreateNetworkInterfaceUseCase, *UpdateNetworkInterfaceUseCase, *repomock.OpsRepo) {
	t.Helper()
	kr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	kr.SeedSubnet(&kachorepo.SubnetRecord{
		Subnet: domain.Subnet{ID: nameCanonSubnetID, ProjectID: "f1", Name: domain.RcNameVPC("sn")},
	})
	return NewCreateNetworkInterfaceUseCase(kr, &repomock.ProjectClient{OK: true}, or),
		NewUpdateNetworkInterfaceUseCase(kr, or), or
}

func createUnnamedNIC(t *testing.T, uc *CreateNetworkInterfaceUseCase,
	or *repomock.OpsRepo) *vpcv1.NetworkInterface {
	t.Helper()
	op, err := uc.Execute(context.Background(), CreateInput{
		NetworkInterface: domain.NetworkInterface{ProjectID: "f1", SubnetID: nameCanonSubnetID},
	})
	require.NoError(t, err)
	saved := repomock.AwaitOpDone(t, or, op.ID)
	require.Nil(t, saved.Error, "создание без имени обязано пройти: пустое имя означает «назови сам»")
	var got vpcv1.NetworkInterface
	require.NoError(t, saved.Response.UnmarshalTo(&got))
	return &got
}

// TestCreate_EmptyName_WritesIdDerivedDefault — создание без имени записывает
// имя, производное от идентификатора, а не пустую строку.
func TestCreate_EmptyName_WritesIdDerivedDefault(t *testing.T) {
	create, _, or := nameCanonFixture(t)
	got := createUnnamedNIC(t, create, or)

	assert.NotEmpty(t, got.Name, "строка ресурса не может нести пустое имя")
	assert.Equal(t, got.Id, got.Name, "умолчание — сам идентификатор (pkg/validate.NameOrDefault)")
}

// TestCreate_TwoEmptyNames_BothSucceedWithDistinctNames — два безымянных
// создания в одном проекте проходят ОБА и получают РАЗНЫЕ имена. Умолчание,
// производное от чего-либо, кроме идентификатора, столкнулось бы на уникальности
// (project, name), и второе создание отвергалось бы.
func TestCreate_TwoEmptyNames_BothSucceedWithDistinctNames(t *testing.T) {
	create, _, or := nameCanonFixture(t)
	first := createUnnamedNIC(t, create, or)
	second := createUnnamedNIC(t, create, or)

	assert.NotEqual(t, first.Name, second.Name,
		"два безымянных создания обязаны получить разные имена")
}

// TestCreate_NameStillValidated — положительный и отрицательный контроль формы.
func TestCreate_NameStillValidated(t *testing.T) {
	create, _, or := nameCanonFixture(t)

	op, err := create.Execute(context.Background(), CreateInput{
		NetworkInterface: domain.NetworkInterface{
			ProjectID: "f1", SubnetID: nameCanonSubnetID, Name: domain.RcNameVPC("nic-1"),
		},
	})
	require.NoError(t, err, "законное имя обязано проходить")
	require.Nil(t, repomock.AwaitOpDone(t, or, op.ID).Error)

	_, err = create.Execute(context.Background(), CreateInput{
		NetworkInterface: domain.NetworkInterface{
			ProjectID: "f1", SubnetID: nameCanonSubnetID, Name: domain.RcNameVPC("Bad_Name"),
		},
	})
	require.Error(t, err, "заглавные и подчёркивание формой не приняты")
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

// TestUpdate_MaskNamesName_EmptyRejected — маска НАЗВАЛА имя, значение пусто:
// отказ с именем поля, СИНХРОННО. Рядом — положительный контроль той же маской:
// он обязателен, иначе отрицание зеленело бы и на реализации, отвергающей всё.
func TestUpdate_MaskNamesName_EmptyRejected(t *testing.T) {
	_, update, _ := nameCanonFixture(t)
	nicID := ids.NewID(ids.PrefixNetworkInterface)

	_, err := update.Execute(context.Background(), UpdateInput{
		NetworkInterfaceID: nicID,
		UpdateMask:         []string{"name"},
		NetworkInterface:   domain.NetworkInterface{Name: ""},
	})
	require.Error(t, err, "снять имя правкой нельзя — имени без значения не бывает")
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	assert.Contains(t, violationFieldsOf(t, err), "name", "отказ обязан называть поле")

	// Положительный контроль: та же маска с законным именем синхронную проверку
	// проходит. Дальше путь упирается в отсутствие самого интерфейса — это уже
	// ДРУГОЙ отказ, и именно он доказывает, что проверка имени пропустила ввод.
	_, err = update.Execute(context.Background(), UpdateInput{
		NetworkInterfaceID: nicID,
		UpdateMask:         []string{"name"},
		NetworkInterface:   domain.NetworkInterface{Name: "nic-2"},
	})
	if err != nil {
		assert.NotEqual(t, codes.InvalidArgument, status.Code(err),
			"законное имя не может быть отвергнуто как негодный ввод: %v", err)
	}
}

// TestUpdate_EmptyMask_EmptyNameNotRefusedAsInvalidArgument — полная правка без
// маски пустое имя как негодный ввод НЕ отвергает: в proto3 «поле не прислано»
// и «поле пусто» неразличимы, и отказ здесь сломал бы всякого, кто правит объект
// целиком, не трогая имя.
func TestUpdate_EmptyMask_EmptyNameNotRefusedAsInvalidArgument(t *testing.T) {
	_, update, _ := nameCanonFixture(t)

	_, err := update.Execute(context.Background(), UpdateInput{
		NetworkInterfaceID: ids.NewID(ids.PrefixNetworkInterface),
		NetworkInterface:   domain.NetworkInterface{Name: ""},
	})
	if err != nil {
		assert.NotEqual(t, codes.InvalidArgument, status.Code(err),
			"пустое имя при полной правке не является негодным вводом: %v", err)
	}
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
	cur := domain.NetworkInterface{Name: "nic-current"}
	applyNICMask(&cur, UpdateInput{NetworkInterface: domain.NetworkInterface{Name: ""}})
	assert.Equal(t, domain.RcNameVPC("nic-current"), cur.Name, "полная правка без имени обязана сохранить текущее имя")
	assert.NotEmpty(t, string(cur.Name), "ресурса без имени не бывает")
}

// TestApply_EmptyMask_NewNameApplied — положительный контроль рядом с отрицанием:
// та же полная правка с НЕПУСТЫМ именем имя меняет. Без этой половины первая
// проба зеленела бы и на применении, которое имя не трогает вовсе.
func TestApply_EmptyMask_NewNameApplied(t *testing.T) {
	cur := domain.NetworkInterface{Name: "nic-current"}
	applyNICMask(&cur, UpdateInput{NetworkInterface: domain.NetworkInterface{Name: "nic-new"}})
	assert.Equal(t, domain.RcNameVPC("nic-new"), cur.Name, "непустое имя при полной правке обязано примениться")
}

// TestApply_MaskNamesName_NewNameApplied — явная маска имя применяет (пустое до
// применения не доходит — его отвергает проверка входа).
func TestApply_MaskNamesName_NewNameApplied(t *testing.T) {
	cur := domain.NetworkInterface{Name: "nic-current"}
	applyNICMask(&cur, UpdateInput{UpdateMask: []string{"name"}, NetworkInterface: domain.NetworkInterface{Name: "nic-new"}})
	assert.Equal(t, domain.RcNameVPC("nic-new"), cur.Name, "маска, назвавшая имя, обязана его применить")
}
