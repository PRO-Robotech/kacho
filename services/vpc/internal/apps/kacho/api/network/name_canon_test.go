// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package network

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/kachomock"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/repomock"
)

// Проверки единственной формы имени (#715) для Network.
//
// Предмет — не форма сама по себе, а два её следствия, которые прежде не
// проверялись ничем: строка ресурса не может нести пустое имя, и снять имя
// правкой нельзя.

// TestCreate_EmptyName_WritesIdDerivedDefault — создание без имени записывает
// имя, производное от идентификатора, а не пустую строку.
func TestCreate_EmptyName_WritesIdDerivedDefault(t *testing.T) {
	kr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	uc := NewCreateNetworkUseCase(kr, &repomock.ProjectClient{OK: true}, or)

	op, err := uc.Execute(context.Background(), domain.Network{ProjectID: "p1"})
	require.NoError(t, err)
	saved := repomock.AwaitOpDone(t, or, op.ID)
	require.Nil(t, saved.Error, "создание без имени обязано пройти: пустое имя означает «назови сам»")

	rows := kr.Networks()
	require.Len(t, rows, 1)
	assert.NotEmpty(t, string(rows[0].Name), "строка ресурса не может нести пустое имя")
	assert.Equal(t, rows[0].ID, string(rows[0].Name),
		"умолчание — сам идентификатор (pkg/validate.NameOrDefault)")
}

// TestCreate_TwoEmptyNames_BothSucceedWithDistinctNames — два создания без имени
// в одном проекте проходят ОБА и получают РАЗНЫЕ имена.
//
// Проба стоит здесь потому, что умолчание, производное от чего-либо, кроме
// идентификатора, столкнулось бы на уникальности (project, name) и второе
// создание отвергалось бы.
func TestCreate_TwoEmptyNames_BothSucceedWithDistinctNames(t *testing.T) {
	kr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	uc := NewCreateNetworkUseCase(kr, &repomock.ProjectClient{OK: true}, or)

	for i := 0; i < 2; i++ {
		op, err := uc.Execute(context.Background(), domain.Network{ProjectID: "p1"})
		require.NoError(t, err)
		saved := repomock.AwaitOpDone(t, or, op.ID)
		require.Nil(t, saved.Error, "создание %d без имени обязано пройти", i)
	}

	rows := kr.Networks()
	require.Len(t, rows, 2)
	assert.NotEqual(t, string(rows[0].Name), string(rows[1].Name),
		"два безымянных создания обязаны получить разные имена")
}

// TestCreate_NameStillValidated — положительный и отрицательный контроль формы
// на создании: законное имя проходит, негодное отвергается синхронно.
func TestCreate_NameStillValidated(t *testing.T) {
	kr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	uc := NewCreateNetworkUseCase(kr, &repomock.ProjectClient{OK: true}, or)

	op, err := uc.Execute(context.Background(), domain.Network{ProjectID: "p1", Name: "net-1"})
	require.NoError(t, err, "законное имя обязано проходить")
	require.Nil(t, repomock.AwaitOpDone(t, or, op.ID).Error)

	_, err = uc.Execute(context.Background(), domain.Network{ProjectID: "p1", Name: "Bad_Name"})
	require.Error(t, err, "заглавные и подчёркивание формой не приняты")
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

// TestUpdate_MaskNamesName_EmptyRejected — маска НАЗВАЛА имя, а значение пусто:
// отказ с именем поля. Рядом — положительный контроль той же маской.
func TestUpdate_MaskNamesName_EmptyRejected(t *testing.T) {
	err := validateNetworkUpdate(UpdateInput{
		UpdateMask: []string{"name"},
		Network:    domain.Network{Name: ""},
	})
	require.Error(t, err, "снять имя правкой нельзя — имени без значения не бывает")
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.True(t, refusalNamesField(t, err, "name"), "отказ обязан называть поле")

	require.NoError(t, validateNetworkUpdate(UpdateInput{
		UpdateMask: []string{"name"},
		Network:    domain.Network{Name: "net-2"},
	}), "положительный контроль: та же маска с законным именем проходит")
}

// TestUpdate_EmptyMask_EmptyNameStillAccepted — полная правка без маски пустое
// имя НЕ отвергает: в proto3 «поле не прислано» и «поле пусто» неразличимы,
// поэтому отказ здесь сломал бы всякого, кто правит объект целиком.
func TestUpdate_EmptyMask_EmptyNameStillAccepted(t *testing.T) {
	require.NoError(t, validateNetworkUpdate(UpdateInput{
		Network: domain.Network{Name: ""},
	}))
}

// refusalNamesField — назвал ли отказ поле по имени. Утверждать надо ИМЕННО это,
// а не только код: `InvalidArgument` возвращает вся проверка входа, и вызывающий
// по одному коду не узнает, что именно он прислал неверно.
func refusalNamesField(t *testing.T, err error, field string) bool {
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
	cur := domain.Network{Name: "net-current"}
	applyNetworkMask(&cur, UpdateInput{Network: domain.Network{Name: ""}})
	assert.Equal(t, domain.RcNameVPC("net-current"), cur.Name, "полная правка без имени обязана сохранить текущее имя")
	assert.NotEmpty(t, string(cur.Name), "ресурса без имени не бывает")
}

// TestApply_EmptyMask_NewNameApplied — положительный контроль рядом с отрицанием:
// та же полная правка с НЕПУСТЫМ именем имя меняет. Без этой половины первая
// проба зеленела бы и на применении, которое имя не трогает вовсе.
func TestApply_EmptyMask_NewNameApplied(t *testing.T) {
	cur := domain.Network{Name: "net-current"}
	applyNetworkMask(&cur, UpdateInput{Network: domain.Network{Name: "net-new"}})
	assert.Equal(t, domain.RcNameVPC("net-new"), cur.Name, "непустое имя при полной правке обязано примениться")
}

// TestApply_MaskNamesName_NewNameApplied — явная маска имя применяет (пустое до
// применения не доходит — его отвергает проверка входа).
func TestApply_MaskNamesName_NewNameApplied(t *testing.T) {
	cur := domain.Network{Name: "net-current"}
	applyNetworkMask(&cur, UpdateInput{UpdateMask: []string{"name"}, Network: domain.Network{Name: "net-new"}})
	assert.Equal(t, domain.RcNameVPC("net-new"), cur.Name, "маска, назвавшая имя, обязана его применить")
}
