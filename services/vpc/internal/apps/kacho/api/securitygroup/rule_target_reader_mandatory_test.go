// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package securitygroup

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io/fs"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/kachomock"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/repomock"
)

// Проверка «цель правила лежит в моей сети» была ОТКЛЮЧАЕМОЙ: порт чтения групп
// приезжал необязательным (builder `WithSGReader` у Create/Update, позиционный
// параметр, принимающий nil, у UpdateRules/UpdateRule), а сама проверка на
// непереданном порту возвращала «ок». То есть у защиты было состояние
// «не настроена = разрешено всё», и отличить его от «настроена и разрешила»
// вызывающий не мог: ответ одинаковый.
//
// Порт при этом не несёт НИ ОДНОГО факта сверх уже обязательного `Repo` —
// боевая провязка передаёт `cqrsadapter.NewSecurityGroup(kachoRepo)`, то есть
// адаптер, целиком выведенный из того же `Repo`. Поэтому «порт не передан»
// перестало быть представимым состоянием: конструктор выводит порт из `Repo`,
// а ветки сравнения порта с nil не осталось ни в одном пути.
//
// Пробы ниже держат три разных свойства, и ни одна не заменяет другую:
//   - поведенческие: цель из чужой сети отвергается на КАЖДОМ пути, собранном
//     БЕЗ порта (и законная цель на том же пути проходит — иначе отрицание
//     зеленело бы на проверке, отвергающей всё);
//   - перепись: у каждого use-case'а с полем порта порт после конструктора
//     ненулевой, и перечень таких use-case'ов ВЫВЕДЕН из дерева, а не выписан;
//   - гейт: в непробном коде пакета нет ни одного сравнения порта с nil,
//     поэтому «отключаемость» нельзя вернуть незаметно.

// ---- перепись непробных исходников пакета (общая для переписи и гейта) ----

// packageProdFiles — непробные .go пакета, разобранные в AST. Комментарии в
// дерево не попадают: гейт обязан читать исполняемую часть, иначе он нашёл бы
// собственное объяснение в комментарии и остался зелёным при снятой защите.
func packageProdFiles(t *testing.T) (*token.FileSet, map[string]*ast.File) {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, parser.SkipObjectResolution)
	require.NoError(t, err)
	require.Contains(t, pkgs, "securitygroup")
	files := pkgs["securitygroup"].Files
	require.NotEmpty(t, files, "перепись обязана быть непустой: ноль прочитанных файлов — не «ноль находок»")
	return fset, files
}

// ---- перепись: у каждого носителя порта порт ненулевой после конструктора ----

// TestSGTargetReader_EveryUseCaseCarryingThePortGetsItFromItsConstructor —
// перечень носителей порта выводится из дерева. Появится пятый use-case с полем
// порта и без строки в этой пробе — проба покраснеет, назвав его: послабление
// самоистекает, а не переживает своё основание.
func TestSGTargetReader_EveryUseCaseCarryingThePortGetsItFromItsConstructor(t *testing.T) {
	const portField = "sgReader"
	_, files := packageProdFiles(t)

	var carriers []string
	structs := 0
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			ts, ok := n.(*ast.TypeSpec)
			if !ok {
				return true
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				return true
			}
			structs++
			for _, f := range st.Fields.List {
				for _, name := range f.Names {
					if name.Name == portField {
						carriers = append(carriers, ts.Name.Name)
					}
				}
			}
			return true
		})
	}
	sort.Strings(carriers)
	t.Logf("осмотрено: непробных файлов %d, структур %d, носителей поля %q — %v",
		len(files), structs, portField, carriers)

	// Конструктор каждого носителя вызывается БЕЗ порта (Create/Update) либо с
	// явным nil (UpdateRules/UpdateRule) — исторически опасные формы вызова.
	repo := kachomock.NewRepository()
	ops := repomock.NewOpsRepo()
	covered := map[string]func() SecurityGroupReader{
		"CreateSecurityGroupUseCase": func() SecurityGroupReader {
			return NewCreateSecurityGroupUseCase(repo, repomock.NewNetworkRepo(),
				&repomock.ProjectClient{OK: true}, ops).sgReader
		},
		"UpdateSecurityGroupUseCase": func() SecurityGroupReader {
			return NewUpdateSecurityGroupUseCase(repo, ops).sgReader
		},
		"UpdateRulesUseCase": func() SecurityGroupReader {
			return NewUpdateRulesUseCase(repo, ops, nil).sgReader
		},
		"UpdateRuleUseCase": func() SecurityGroupReader {
			return NewUpdateRuleUseCase(repo, ops, nil).sgReader
		},
	}
	names := make([]string, 0, len(covered))
	for name := range covered {
		names = append(names, name)
	}
	sort.Strings(names)
	require.Equal(t, names, carriers,
		"перечень носителей порта в дереве и перечень проверенных здесь обязаны совпадать")

	for _, name := range names {
		reader := covered[name]()
		require.NotNil(t, reader, "%s: порт обязан быть выведен конструктором, а не остаться пустым", name)
		// Порт обязан РАБОТАТЬ, а не просто быть непустым: непустой порт,
		// возвращающий ошибку на любой ввод, — та же отключённая проверка.
		found, err := reader.GetMany(context.Background(), []string{"нет-такого"})
		require.NoError(t, err, "%s: выведенный порт обязан отвечать", name)
		assert.Empty(t, found, "%s: несуществующий id не резолвится", name)
	}
}

// ---- гейт: сравнения порта с nil в непробном коде пакета быть не должно ----

// TestSGTargetReader_NoNilComparisonRemainsInProductionCode — «отключаемость»
// нельзя вернуть незаметно. Гейт читает AST, поэтому не сработает на
// комментарии, объясняющем этот же запрет, и покраснеет ровно на коде.
//
// Предпосылка гейта проверяется тут же: перечень имён портов ВЫВЕДЕН из полей
// структур пакета, а не выписан, — иначе новый порт унаследовал бы слепую зону.
func TestSGTargetReader_NoNilComparisonRemainsInProductionCode(t *testing.T) {
	fset, files := packageProdFiles(t)

	// Имена, за которыми гейт следит: поля-порты use-case'ов (по типу
	// интерфейса, объявленного в этом же пакете) плюс имя параметра, под которым
	// порт приходит в проверку.
	watched := map[string]struct{}{"reader": {}}
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			st, ok := n.(*ast.StructType)
			if !ok {
				return true
			}
			for _, f := range st.Fields.List {
				ident, ok := f.Type.(*ast.Ident)
				if !ok || (ident.Name != "SecurityGroupReader" && ident.Name != "NetworkReader") {
					continue
				}
				for _, name := range f.Names {
					watched[name.Name] = struct{}{}
				}
			}
			return true
		})
	}
	watchedNames := make([]string, 0, len(watched))
	for name := range watched {
		watchedNames = append(watchedNames, name)
	}
	sort.Strings(watchedNames)
	require.Greater(t, len(watchedNames), 1, "гейт без выведенных из дерева имён портов не следит ни за чем")

	render := func(e ast.Expr) string {
		var sb strings.Builder
		require.NoError(t, printer.Fprint(&sb, fset, e))
		return sb.String()
	}
	isWatched := func(e ast.Expr) bool {
		text := render(e)
		if i := strings.LastIndex(text, "."); i >= 0 {
			text = text[i+1:]
		}
		_, ok := watched[text]
		return ok
	}

	var findings []string
	comparisons := 0
	for name, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			be, ok := n.(*ast.BinaryExpr)
			if !ok || (be.Op != token.EQL && be.Op != token.NEQ) {
				return true
			}
			comparisons++
			left, right := render(be.X), render(be.Y)
			if (left == "nil" && isWatched(be.Y)) || (right == "nil" && isWatched(be.X)) {
				findings = append(findings, fmt.Sprintf("%s:%d: %s %s %s",
					name, fset.Position(be.Pos()).Line, left, be.Op, right))
			}
			return true
		})
	}
	sort.Strings(findings)
	t.Logf("осмотрено: непробных файлов %d, сравнений на равенство %d, имён портов под надзором %v",
		len(files), comparisons, watchedNames)
	require.Greater(t, comparisons, 0, "ноль осмотренных сравнений — это не «ноль находок»")
	assert.Empty(t, findings,
		"порт не может быть отключаемым: сравнение порта с nil означает «не настроен = разрешено»")
}

// ---- поведенческие: каждый путь отвергает цель из чужой сети без порта ----

// seedMockSGRules — SG с заданным набором правил (seedMockSG правил не несёт, а
// UpdateRule валидирует ИМЕННО уже сохранённое правило).
func seedMockSGRules(t *testing.T, sgr *kachomock.Repository, projectID, networkID, name string,
	rules []domain.SecurityGroupRule) *kacho.SecurityGroupRecord {
	t.Helper()
	w, err := sgr.Writer(context.Background())
	require.NoError(t, err)
	rec, err := w.SecurityGroups().Insert(context.Background(), &domain.SecurityGroup{
		ID: ids.NewID(ids.PrefixSecurityGroup), ProjectID: projectID, NetworkID: networkID,
		Name: domain.RcNameVPC(name), Rules: assignRuleIDs(rules),
	})
	require.NoError(t, err)
	require.NoError(t, w.Commit())
	return rec
}

// twoNetworksWithForeignTarget — сеть-владелец, чужая сеть и группа в чужой сети.
func twoNetworksWithForeignTarget(t *testing.T, sgr *kachomock.Repository) (ownerNet, foreignTarget string) {
	t.Helper()
	ownerNet = ids.NewID(ids.PrefixNetwork)
	foreignNet := ids.NewID(ids.PrefixNetwork)
	return ownerNet, seedMockSG(t, sgr, "P", foreignNet, "sg-foreign-"+foreignNet[:6])
}

// assertCrossNetworkRefusal — отказ обязан быть контракт-тоном промаха и назвать
// поле; код — InvalidArgument.
func assertCrossNetworkRefusal(t *testing.T, err error, form, targetID, wantField string) {
	t.Helper()
	require.Error(t, err, "цель из чужой сети обязана быть отвергнута")
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Equal(t, fmt.Sprintf(form, targetID), st.Message())
	assert.Equal(t, wantField, fieldViolation(t, err))
}

// Create, собранный БЕЗ порта: цель из чужой сети отвергается синхронно.
func TestSGTargetReader_CreateWithoutInjectedPortStillRefusesCrossNetwork(t *testing.T) {
	form := sgTargetNotFoundForm(t)
	sgr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	nr := repomock.NewNetworkRepo()
	ownerNet, foreign := twoNetworksWithForeignTarget(t, sgr)
	_, err := nr.Insert(context.Background(), &domain.Network{ID: ownerNet, ProjectID: "P", Name: "net-own"})
	require.NoError(t, err)

	uc := NewCreateSecurityGroupUseCase(sgr, nr, &repomock.ProjectClient{OK: true}, or)
	_, err = uc.Execute(context.Background(), domain.SecurityGroup{
		ProjectID: "P", NetworkID: ownerNet, Name: domain.RcNameVPC("sg-create-noport"),
		Rules: []domain.SecurityGroupRule{sgTargetRule(foreign)},
	})
	assertCrossNetworkRefusal(t, err, form, foreign, "rule_specs[0].security_group_id")
}

// Update (full-replace rule_specs), собранный БЕЗ порта: цель из чужой сети
// отвергается воркером — проверка живёт в writer-TX, поэтому исход приходит
// через Operation.error.
func TestSGTargetReader_UpdateWithoutInjectedPortStillRefusesCrossNetwork(t *testing.T) {
	form := sgTargetNotFoundForm(t)
	sgr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	ownerNet, foreign := twoNetworksWithForeignTarget(t, sgr)
	owner := seedMockSG(t, sgr, "P", ownerNet, "sg-update-noport")

	uc := NewUpdateSecurityGroupUseCase(sgr, or)
	op, err := uc.Execute(context.Background(), UpdateInput{
		SecurityGroupID: owner,
		SecurityGroup: domain.SecurityGroup{
			Rules: []domain.SecurityGroupRule{sgTargetRule(foreign)},
		},
		UpdateMask: []string{"rule_specs"},
	})
	require.NoError(t, err)
	saved := repomock.AwaitOpDone(t, or, op.ID)
	require.NotNil(t, saved.Error, "цель из чужой сети обязана уронить операцию")
	assert.Equal(t, int32(codes.InvalidArgument), saved.Error.Code)
	assert.Equal(t, fmt.Sprintf(form, foreign), saved.Error.Message)
}

// Положительный контроль к пробе выше: законная цель проходит тем же путём.
func TestSGTargetReader_UpdateWithoutInjectedPortAcceptsSameNetwork(t *testing.T) {
	sgr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	ownerNet := ids.NewID(ids.PrefixNetwork)
	owner := seedMockSG(t, sgr, "P", ownerNet, "sg-update-ok")
	sameNet := seedMockSG(t, sgr, "P", ownerNet, "sg-target-ok")

	uc := NewUpdateSecurityGroupUseCase(sgr, or)
	op, err := uc.Execute(context.Background(), UpdateInput{
		SecurityGroupID: owner,
		SecurityGroup: domain.SecurityGroup{
			Rules: []domain.SecurityGroupRule{sgTargetRule(sameNet)},
		},
		UpdateMask: []string{"rule_specs"},
	})
	require.NoError(t, err)
	saved := repomock.AwaitOpDone(t, or, op.ID)
	assert.Nil(t, saved.Error, "цель в своей сети обязана проходить")
}

// UpdateRules с явным nil вместо порта: цель из чужой сети отвергается
// синхронно, до создания операции.
func TestSGTargetReader_UpdateRulesWithNilPortStillRefusesCrossNetwork(t *testing.T) {
	form := sgTargetNotFoundForm(t)
	sgr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	ownerNet, foreign := twoNetworksWithForeignTarget(t, sgr)
	owner := seedMockSG(t, sgr, "P", ownerNet, "sg-rules-nilport")

	uc := NewUpdateRulesUseCase(sgr, or, nil)
	_, err := uc.Execute(context.Background(), UpdateRulesInput{
		SecurityGroupID:   owner,
		AdditionRuleSpecs: []domain.SecurityGroupRule{sgTargetRule(foreign)},
	})
	assertCrossNetworkRefusal(t, err, form, foreign, "addition_rule_specs[0].security_group_id")
}

// Положительный контроль: законное добавление тем же путём проходит.
func TestSGTargetReader_UpdateRulesWithNilPortAcceptsSameNetwork(t *testing.T) {
	sgr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	ownerNet := ids.NewID(ids.PrefixNetwork)
	owner := seedMockSG(t, sgr, "P", ownerNet, "sg-rules-ok")
	sameNet := seedMockSG(t, sgr, "P", ownerNet, "sg-rules-target-ok")

	uc := NewUpdateRulesUseCase(sgr, or, nil)
	op, err := uc.Execute(context.Background(), UpdateRulesInput{
		SecurityGroupID:   owner,
		AdditionRuleSpecs: []domain.SecurityGroupRule{sgTargetRule(sameNet)},
	})
	require.NoError(t, err)
	saved := repomock.AwaitOpDone(t, or, op.ID)
	assert.Nil(t, saved.Error, "добавление цели из своей сети обязано проходить")
}

// UpdateRule с явным nil вместо порта: унаследованная цель из чужой сети
// отвергается синхронно.
func TestSGTargetReader_UpdateRuleWithNilPortStillRefusesCrossNetwork(t *testing.T) {
	form := sgTargetNotFoundForm(t)
	sgr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	ownerNet, foreign := twoNetworksWithForeignTarget(t, sgr)
	owner := seedMockSGRules(t, sgr, "P", ownerNet, "sg-rule-nilport",
		[]domain.SecurityGroupRule{sgTargetRule(foreign)})
	require.Len(t, owner.Rules, 1)

	uc := NewUpdateRuleUseCase(sgr, or, nil)
	_, err := uc.Execute(context.Background(), UpdateRuleInput{
		SecurityGroupID: owner.ID,
		RuleID:          owner.Rules[0].ID,
		Description:     "правка описания",
		UpdateMask:      []string{"description"},
	})
	assertCrossNetworkRefusal(t, err, form, foreign, "security_group_id")
}

// Положительный контроль: правило с законной целью правится тем же путём.
func TestSGTargetReader_UpdateRuleWithNilPortAcceptsSameNetwork(t *testing.T) {
	sgr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	ownerNet := ids.NewID(ids.PrefixNetwork)
	sameNet := seedMockSG(t, sgr, "P", ownerNet, "sg-rule-target-ok")
	owner := seedMockSGRules(t, sgr, "P", ownerNet, "sg-rule-ok",
		[]domain.SecurityGroupRule{sgTargetRule(sameNet)})
	require.Len(t, owner.Rules, 1)

	uc := NewUpdateRuleUseCase(sgr, or, nil)
	op, err := uc.Execute(context.Background(), UpdateRuleInput{
		SecurityGroupID: owner.ID,
		RuleID:          owner.Rules[0].ID,
		Description:     "правка описания",
		UpdateMask:      []string{"description"},
	})
	require.NoError(t, err, "правило с законной целью обязано правиться")
	saved := repomock.AwaitOpDone(t, or, op.ID)
	assert.Nil(t, saved.Error)
}
