// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package securitygroup

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/operations"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/kachomock"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/repomock"
)

// Выбранная ветка `oneof protocol` против нулевого значения её домена.
//
// `SecurityGroupRuleSpec.protocol` — oneof из двух ветвей. Прочитанная
// геттером, ветка `protocol_number` отдаёт `0` и когда вызывающий выбрал её со
// значением 0 (это HOPOPT, номер 0 реестра IANA), и когда не выбирал вовсе.
// Конверсия сравнивала результат геттера с нулём, поэтому «протокол 0» и
// «протокол не задан» сходились в одно, а правило сохранялось как «любой
// протокол» — то есть ШИРЕ, чем просил вызывающий. Симметрично для
// `protocol_name`: выбранная ветка с пустой строкой тоже уезжала в «любой».
//
// Оба случая — «принято-и-проигнорировано»: успех на параметр, которого продукт
// не применил. Исход выбран второй из трёх законных — отвергать явно, называя
// поле. Реализовать (0 = HOPOPT) значило бы менять представление в хранилище:
// «протокол не задан» лежит там нулём, и отличить его от протокола 0 нечем без
// миграции каждого сохранённого правила. Выразимость при этом не теряется —
// HOPOPT адресуется именем (`protocol_name: "hopopt"` входит в набор), и отказ
// прямо на это указывает.

// baseSpec — правило без ветки `protocol`: сама по себе это законная форма
// («любой протокол»). Ветка выставляется в каждом кейсе отдельно — тип
// oneof-обёртки не экспортируется, поэтому параметром её не передать.
func baseSpec() *vpcv1.SecurityGroupRuleSpec {
	return &vpcv1.SecurityGroupRuleSpec{
		Direction: vpcv1.SecurityGroupRule_INGRESS,
		Ports:     &vpcv1.PortRange{FromPort: 80, ToPort: 80},
		Target: &vpcv1.SecurityGroupRuleSpec_CidrBlocks{
			CidrBlocks: &vpcv1.CidrBlocks{V4CidrBlocks: []string{"0.0.0.0/0"}},
		},
	}
}

func specWithProtocolName(name string) *vpcv1.SecurityGroupRuleSpec {
	s := baseSpec()
	s.Protocol = &vpcv1.SecurityGroupRuleSpec_ProtocolName{ProtocolName: name}
	return s
}

func specWithProtocolNumber(num int64) *vpcv1.SecurityGroupRuleSpec {
	s := baseSpec()
	s.Protocol = &vpcv1.SecurityGroupRuleSpec_ProtocolNumber{ProtocolNumber: num}
	return s
}

// Выбранная ветка с нулевым значением домена — отказ, называющий поле.
func TestRuleSpecFromProto_ProtocolNumberZeroRefused(t *testing.T) {
	_, err := ruleSpecFromProto("rule_specs[0]",
		specWithProtocolNumber(0))
	if err == nil {
		t.Fatal("protocol_number 0 must be refused, not silently widened to any protocol")
	}
	if got := status.Code(err); got != codes.InvalidArgument {
		t.Fatalf("want INVALID_ARGUMENT, got %v (%v)", got, err)
	}
	if got := fieldViolation(t, err); got != "rule_specs[0].protocol_number" {
		t.Fatalf("refusal must name rule_specs[0].protocol_number, named %q", got)
	}
}

func TestRuleSpecFromProto_EmptyProtocolNameRefused(t *testing.T) {
	_, err := ruleSpecFromProto("rule_specs[0]",
		specWithProtocolName(""))
	if err == nil {
		t.Fatal("an explicitly selected empty protocol_name must be refused")
	}
	if got := status.Code(err); got != codes.InvalidArgument {
		t.Fatalf("want INVALID_ARGUMENT, got %v (%v)", got, err)
	}
	if got := fieldViolation(t, err); got != "rule_specs[0].protocol_name" {
		t.Fatalf("refusal must name rule_specs[0].protocol_name, named %q", got)
	}
}

// Парная половина: невыбранная ветка — законное «любой протокол», и она обязана
// проходить. Без неё гейт ловил бы форму (нулевое значение), а не существо
// (выбранную ветку с нулевым значением).
func TestRuleSpecFromProto_UnsetProtocolAccepted(t *testing.T) {
	r, err := ruleSpecFromProto("rule_specs[0]", baseSpec())
	if err != nil {
		t.Fatalf("unset protocol means any protocol and must be accepted, got %v", err)
	}
	if r.ProtocolName != "" || r.ProtocolNumber != 0 {
		t.Fatalf("unset protocol must stay unset, got name=%q number=%d", r.ProtocolName, r.ProtocolNumber)
	}
}

func TestRuleSpecFromProto_SelectedBranchesCarried(t *testing.T) {
	r, err := ruleSpecFromProto("rule_specs[0]",
		specWithProtocolNumber(17))
	if err != nil {
		t.Fatalf("protocol_number 17 must be accepted, got %v", err)
	}
	if r.ProtocolNumber != 17 || r.ProtocolName != "" {
		t.Fatalf("protocol_number 17 → number=17 name=\"\", got number=%d name=%q", r.ProtocolNumber, r.ProtocolName)
	}
	r, err = ruleSpecFromProto("rule_specs[0]",
		specWithProtocolName("hopopt"))
	if err != nil {
		t.Fatalf("protocol_name hopopt must be accepted, got %v", err)
	}
	if r.ProtocolName != "hopopt" {
		t.Fatalf("protocol_name hopopt → name=hopopt, got %q", r.ProtocolName)
	}
	// `-1` — собственное написание продукта «любой протокол», его пишет builder
	// группы по умолчанию; выбранная ветка с ним законна.
	r, err = ruleSpecFromProto("rule_specs[0]",
		specWithProtocolNumber(domain.AnyProtocolNumber))
	if err != nil {
		t.Fatalf("protocol_number -1 (any) must be accepted, got %v", err)
	}
	if r.ProtocolNumber != domain.AnyProtocolNumber {
		t.Fatalf("protocol_number -1 → number=-1, got %d", r.ProtocolNumber)
	}
}

// Отказ приходит СИНХРОННО и ДО создания операции: проверяется настоящим
// handler'ом поверх настоящего use-case'а, после отказа в репозитории операций
// не должно остаться ни одной строки.
func TestHandlerCreate_ProtocolNumberZeroRefusedBeforeOperation(t *testing.T) {
	sgr := kachomock.NewRepository()
	ops := repomock.NewOpsRepo()
	nr := repomock.NewNetworkRepo()
	netA := ids.NewID(ids.PrefixNetwork)
	if _, err := nr.Insert(context.Background(),
		&domain.Network{ID: netA, ProjectID: "P", Name: "net-A"}); err != nil {
		t.Fatalf("seed network: %v", err)
	}
	create := NewCreateSecurityGroupUseCase(sgr, nr, &repomock.ProjectClient{OK: true}, ops).
		WithSGReader(newSGReaderMock(sgr))
	h := NewHandler(create, nil, nil, nil, nil, nil, nil, nil)

	_, err := h.Create(context.Background(), &vpcv1.CreateSecurityGroupRequest{
		ProjectId: "P", NetworkId: netA, Name: "sg-proto-zero",
		RuleSpecs: []*vpcv1.SecurityGroupRuleSpec{
			specWithProtocolNumber(0),
		},
	})
	assertRefusedBeforeOperation(t, ops, err, "rule_specs[0].protocol_number")
}

func TestHandlerUpdateRules_ProtocolNumberZeroRefusedBeforeOperation(t *testing.T) {
	sgr := kachomock.NewRepository()
	ops := repomock.NewOpsRepo()
	netA := ids.NewID(ids.PrefixNetwork)
	sgID := seedMockSG(t, sgr, "P", netA, "sg-proto-zero-upd")

	get := NewGetSecurityGroupUseCase(sgr)
	updateRules := NewUpdateRulesUseCase(sgr, ops, newSGReaderMock(sgr))
	h := NewHandler(nil, nil, updateRules, nil, nil, get, nil, nil)

	_, err := h.UpdateRules(context.Background(), &vpcv1.UpdateSecurityGroupRulesRequest{
		SecurityGroupId: sgID,
		AdditionRuleSpecs: []*vpcv1.SecurityGroupRuleSpec{
			specWithProtocolNumber(0),
		},
	})
	assertRefusedBeforeOperation(t, ops, err, "addition_rule_specs[0].protocol_number")
}

// Ещё одно следствие, ради которого правка и делается: правило, о котором
// просили как о протоколе 0, НЕ становится правилом «любой протокол».
func TestHandlerCreate_ProtocolNumberZeroDoesNotBecomeAnyProtocol(t *testing.T) {
	sgr := kachomock.NewRepository()
	ops := repomock.NewOpsRepo()
	nr := repomock.NewNetworkRepo()
	netA := ids.NewID(ids.PrefixNetwork)
	if _, err := nr.Insert(context.Background(),
		&domain.Network{ID: netA, ProjectID: "P", Name: "net-B"}); err != nil {
		t.Fatalf("seed network: %v", err)
	}
	create := NewCreateSecurityGroupUseCase(sgr, nr, &repomock.ProjectClient{OK: true}, ops).
		WithSGReader(newSGReaderMock(sgr))
	h := NewHandler(create, nil, nil, nil, nil, nil, nil, nil)

	if _, err := h.Create(context.Background(), &vpcv1.CreateSecurityGroupRequest{
		ProjectId: "P", NetworkId: netA, Name: "sg-widen",
		RuleSpecs: []*vpcv1.SecurityGroupRuleSpec{
			specWithProtocolNumber(0),
		},
	}); err == nil {
		t.Fatal("protocol_number 0 must not produce a rule at all")
	}
	created, _, err := ops.List(context.Background(), operations.ListFilter{})
	if err != nil {
		t.Fatalf("list operations: %v", err)
	}
	if len(created) != 0 {
		t.Fatalf("no security group must be created for an unappliable protocol, got %d operation(s)", len(created))
	}
}
