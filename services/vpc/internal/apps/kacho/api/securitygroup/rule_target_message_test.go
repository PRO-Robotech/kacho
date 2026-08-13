// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package securitygroup

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/kachomock"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/repomock"
)

// Резолв цели правила («oneof target = security_group_id») имеет ДВА исхода, по
// которым вызывающий не имеет права их различить: цели нет вовсе и цель есть, но
// в чужой сети. Раньше у них были два РАЗНЫХ текста, поэтому по тексту отказа
// устанавливалось существование чужой группы: «references a non-existent
// security group» означало «такой группы нет», «in the same network» —
// «группа есть, она просто не ваша». Это существование-оракул: скрытие
// обязано быть побайтово неотличимо от настоящего промаха
// (`security.md` §Hardening-инварианты п.6).
//
// Проба ниже сравнивает не с литералом, выписанным здесь, а с формой, которую
// производит САМ слой хранения (`repo/helpers`.`WrapSGErr`): выписанный литерал
// разошёлся бы с продуктом молча, а извлечённая форма краснеет, как только
// продукт сменит тон.

// sgTargetNotFoundForm — контракт-тон промаха SecurityGroup, извлечённый из
// производителя. Читается по AST, а не грепом: тот же текст стоит в шапке
// функции, и текстовый поиск нашёл бы комментарий, объясняющий тон, при
// изменившемся коде. Печатает объём осмотренного, чтобы «нашёл одну форму»
// было отличимо от «прочитал ноль литералов».
func sgTargetNotFoundForm(t *testing.T) string {
	t.Helper()
	const producer = "../../../../repo/helpers/sg.go"
	fset := token.NewFileSet()
	// parser.SkipObjectResolution, без ParseComments — комментарии в дерево не
	// попадают вовсе, поэтому совпадение возможно только с исполняемой частью.
	file, err := parser.ParseFile(fset, producer, nil, parser.SkipObjectResolution)
	require.NoError(t, err, "производитель контракт-тона обязан быть читаем: %s", producer)

	var literals, matched []string
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		val, uerr := strconv.Unquote(lit.Value)
		if uerr != nil {
			return true
		}
		literals = append(literals, val)
		if strings.HasPrefix(val, "%w: ") && strings.Contains(val, "not found") {
			matched = append(matched, strings.TrimPrefix(val, "%w: "))
		}
		return true
	})
	t.Logf("осмотрено: %s — строковых литералов %d, из них not-found-форм %d",
		producer, len(literals), len(matched))
	require.Len(t, matched, 1,
		"в %s обязана быть ровно одна not-found-форма SecurityGroup; найдено %v", producer, matched)
	require.Contains(t, matched[0], "%s", "форма обязана нести id — иначе скрытие теряет id")
	return matched[0]
}

// sgTargetRefusal — прогоняет Create с правилом, ссылающимся на targetID, и
// возвращает сообщение синхронного отказа. `seedTargetInOtherNetwork`
// переключает состояние хранилища между двумя исходами резолва: цели нет вовсе
// и цель есть, но в другой сети. ID один и тот же в обоих прогонах — именно
// поэтому сравнение сообщений может быть побайтовым.
func sgTargetRefusal(t *testing.T, targetID string, seedTargetInOtherNetwork bool) error {
	t.Helper()
	sgr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	nr := repomock.NewNetworkRepo()
	netA := ids.NewID(ids.PrefixNetwork)
	netB := ids.NewID(ids.PrefixNetwork)
	_, err := nr.Insert(context.Background(), &domain.Network{ID: netA, ProjectID: "P", Name: "net-A"})
	require.NoError(t, err)
	_, err = nr.Insert(context.Background(), &domain.Network{ID: netB, ProjectID: "P", Name: "net-B"})
	require.NoError(t, err)
	if seedTargetInOtherNetwork {
		seedMockSGWithID(t, sgr, targetID, "P", netB, "sg-target-elsewhere")
	}

	uc := NewCreateSecurityGroupUseCase(sgr, nr, &repomock.ProjectClient{OK: true}, or)
	_, err = uc.Execute(context.Background(), domain.SecurityGroup{
		ProjectID: "P", NetworkID: netA, Name: domain.RcNameVPC("sg-probe"),
		Rules: []domain.SecurityGroupRule{sgTargetRule(targetID)},
	})
	return err
}

// seedMockSGWithID — как seedMockSG, но с заданным id: обе ветки пробы обязаны
// говорить об ОДНОМ идентификаторе, иначе побайтовое сравнение сообщений
// невозможно by construction.
func seedMockSGWithID(t *testing.T, sgr *kachomock.Repository, id, projectID, networkID, name string) {
	t.Helper()
	w, err := sgr.Writer(context.Background())
	require.NoError(t, err)
	_, err = w.SecurityGroups().Insert(context.Background(), &domain.SecurityGroup{
		ID: id, ProjectID: projectID, NetworkID: networkID, Name: domain.RcNameVPC(name),
	})
	require.NoError(t, err)
	require.NoError(t, w.Commit())
}

// Оба исхода резолва цели отдают ПОБАЙТОВО одно сообщение, и это сообщение —
// контракт-тон настоящего промаха, взятый у его производителя.
func TestSGRuleTarget_MissingAndCrossNetworkAreIndistinguishable(t *testing.T) {
	form := sgTargetNotFoundForm(t)
	targetID := ids.NewID(ids.PrefixSecurityGroup)

	errMissing := sgTargetRefusal(t, targetID, false)
	errCrossNet := sgTargetRefusal(t, targetID, true)

	require.Error(t, errMissing, "цель, которой нет, обязана быть отвергнута")
	require.Error(t, errCrossNet, "цель из чужой сети обязана быть отвергнута")

	stMissing, _ := status.FromError(errMissing)
	stCrossNet, _ := status.FromError(errCrossNet)
	assert.Equal(t, codes.InvalidArgument, stMissing.Code())
	assert.Equal(t, codes.InvalidArgument, stCrossNet.Code())

	assert.Equal(t, stMissing.Message(), stCrossNet.Message(),
		"по тексту отказа НЕЛЬЗЯ установить, существует ли чужая группа")
	assert.Equal(t, fmt.Sprintf(form, targetID), stMissing.Message(),
		"текст обязан быть побайтово равен настоящему промаху SecurityGroup")
}

// Положительный контроль к пробе выше: отказ не выдаётся на законную цель, иначе
// «оба исхода неотличимы» зеленело бы и на проверке, отвергающей всё.
func TestSGRuleTarget_SameNetworkTargetAccepted(t *testing.T) {
	sgr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	nr := repomock.NewNetworkRepo()
	netA := ids.NewID(ids.PrefixNetwork)
	_, err := nr.Insert(context.Background(), &domain.Network{ID: netA, ProjectID: "P", Name: "net-A"})
	require.NoError(t, err)
	sgA := seedMockSG(t, sgr, "P", netA, "sg-target-same-net")

	uc := NewCreateSecurityGroupUseCase(sgr, nr, &repomock.ProjectClient{OK: true}, or)
	op, err := uc.Execute(context.Background(), domain.SecurityGroup{
		ProjectID: "P", NetworkID: netA, Name: domain.RcNameVPC("sg-ok"),
		Rules: []domain.SecurityGroupRule{sgTargetRule(sgA)},
	})
	require.NoError(t, err, "цель в своей сети обязана проходить синхронно")
	saved := repomock.AwaitOpDone(t, or, op.ID)
	assert.True(t, saved.Done)
	assert.Nil(t, saved.Error, "цель в своей сети обязана проходить и в воркере")
}
