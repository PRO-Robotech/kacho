// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package securitygroup

import (
	"context"
	"testing"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/operations"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/kachomock"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/repomock"
)

// Диапазон портов и протокол правила — предмет контракта, а не свободный текст.
//
// `PortRange.from_port`/`to_port` объявлены в proto как `0-65535`, а доменная
// модель документирует `-1` как «любой порт». `protocol_name` объявлен как
// значение из реестра номеров протоколов IANA, `protocol_number` — как номер
// оттуда же. До этих тестов ни одно из четырёх полей не читалось ни одной
// проверкой: правило с портом вне диапазона, с перевёрнутым диапазоном или с
// несуществующим именем протокола принималось и сохранялось как есть — то есть
// вызывающий получал успех на правило, которого продукт выразить не может.
//
// Проверки живут в service-слое (`validateSGRule`), потому что это cross-field
// инварианты; каждый пишущий путь правила (`Create.rule_specs`,
// `Update.rule_specs`, `UpdateRules.addition_rule_specs`) проходит через них
// синхронно, до создания операции.

// fieldViolation достаёт имя поля из BadRequest-детали ошибки. Отказ обязан
// называть поле — иначе вызывающий знает только «что-то не так».
func fieldViolation(t *testing.T, err error) string {
	t.Helper()
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("not a gRPC status: %v", err)
	}
	for _, d := range st.Details() {
		if br, ok := d.(*errdetails.BadRequest); ok && len(br.GetFieldViolations()) > 0 {
			return br.GetFieldViolations()[0].GetField()
		}
	}
	t.Fatalf("no BadRequest field violation in %v", err)
	return ""
}

func ruleWithPorts(from, to int64) domain.SecurityGroupRule {
	return domain.SecurityGroupRule{
		Direction:    domain.SecurityGroupRuleDirectionIngress,
		FromPort:     from,
		ToPort:       to,
		ProtocolName: "tcp",
		V4CidrBlocks: []string{"0.0.0.0/0"},
	}
}

func TestValidateSGRule_PortRange(t *testing.T) {
	cases := []struct {
		name      string
		from, to  int64
		wantField string // "" = принимается
	}{
		{"any/any", -1, -1, ""},
		{"lower bound", 0, 0, ""},
		{"upper bound", 65535, 65535, ""},
		{"ordinary range", 80, 443, ""},
		{"below any", -2, 22, "rule.ports.from_port"},
		{"to below any", 22, -2, "rule.ports.to_port"},
		{"above max", 65536, 65536, "rule.ports.from_port"},
		{"to above max", 22, 65536, "rule.ports.to_port"},
		{"inverted range", 443, 80, "rule.ports.from_port"},
		// Полудиапазон отвергается, и отказ называет ту границу, которую нужно
		// поменять, чтобы диапазон снова стал одним утверждением.
		{"half-any from", -1, 80, "rule.ports.to_port"},
		{"half-any to", 80, -1, "rule.ports.from_port"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateSGRule("rule", ruleWithPorts(tc.from, tc.to))
			if tc.wantField == "" {
				if err != nil {
					t.Fatalf("ports %d-%d must be accepted, got %v", tc.from, tc.to, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ports %d-%d must be refused", tc.from, tc.to)
			}
			if got := status.Code(err); got != codes.InvalidArgument {
				t.Fatalf("want INVALID_ARGUMENT, got %v (%v)", got, err)
			}
			if got := fieldViolation(t, err); got != tc.wantField {
				t.Fatalf("refusal must name %q, named %q", tc.wantField, got)
			}
		})
	}
}

func TestValidateSGRule_ProtocolName(t *testing.T) {
	accepted := []string{"tcp", "TCP", "udp", "icmp", "ipv6-icmp", "esp", "ah", "gre", "sctp", "any", "ANY", ""}
	for _, name := range accepted {
		r := ruleWithPorts(80, 80)
		r.ProtocolName = name
		if err := validateSGRule("rule", r); err != nil {
			t.Fatalf("protocol %q must be accepted, got %v", name, err)
		}
	}
	refused := []string{"klingon", "tcp/udp", "TCP ", "42"}
	for _, name := range refused {
		r := ruleWithPorts(80, 80)
		r.ProtocolName = name
		err := validateSGRule("rule", r)
		if err == nil {
			t.Fatalf("protocol %q must be refused", name)
		}
		if got := status.Code(err); got != codes.InvalidArgument {
			t.Fatalf("protocol %q: want INVALID_ARGUMENT, got %v (%v)", name, got, err)
		}
		if got := fieldViolation(t, err); got != "rule.protocol_name" {
			t.Fatalf("protocol %q: refusal must name rule.protocol_name, named %q", name, got)
		}
	}
}

func TestValidateSGRule_ProtocolNumber(t *testing.T) {
	for _, num := range []int64{-1, 0, 6, 17, 255} {
		r := ruleWithPorts(80, 80)
		r.ProtocolName = ""
		r.ProtocolNumber = num
		if err := validateSGRule("rule", r); err != nil {
			t.Fatalf("protocol number %d must be accepted, got %v", num, err)
		}
	}
	for _, num := range []int64{-2, 256, 99999} {
		r := ruleWithPorts(80, 80)
		r.ProtocolName = ""
		r.ProtocolNumber = num
		err := validateSGRule("rule", r)
		if err == nil {
			t.Fatalf("protocol number %d must be refused", num)
		}
		if got := status.Code(err); got != codes.InvalidArgument {
			t.Fatalf("protocol number %d: want INVALID_ARGUMENT, got %v (%v)", num, got, err)
		}
		if got := fieldViolation(t, err); got != "rule.protocol_number" {
			t.Fatalf("protocol number %d: refusal must name rule.protocol_number, named %q", num, got)
		}
	}
}

// Правила, которые продукт создаёт сам (default-SG каждой сети), обязаны
// проходить собственную проверку — иначе сеть с default-SG стала бы
// нередактируемой через публичный API.
func TestValidateSGRule_DefaultSecurityGroupRulesPass(t *testing.T) {
	for i, r := range domain.NewDefaultSecurityGroupRules() {
		if err := validateSGRule("rule_specs[0]", r); err != nil {
			t.Fatalf("default rule %d must pass its own validation, got %v", i, err)
		}
	}
}

// Отказ приходит СИНХРОННО, ДО создания операции — это и есть разница между
// «400 с именем поля» и «200 с операцией, которая тихо примет неверное правило».
//
// Каждый из трёх тестов ниже гоняет НАСТОЯЩИЙ use-case и после отказа требует,
// чтобы в репозитории операций не осталось ни одной строки. Проверять это на
// самой `validateSGRule` бессмысленно: она про порядок вызовов ничего не знает,
// и такой тест остался бы зелёным, если перенести валидацию за создание
// операции — то есть ровно при том дефекте, который он называет.
func assertRefusedBeforeOperation(t *testing.T, ops *repomock.OpsRepo, err error, wantField string) {
	t.Helper()
	if err == nil {
		t.Fatal("bad rule must be refused synchronously")
	}
	if got := status.Code(err); got != codes.InvalidArgument {
		t.Fatalf("want INVALID_ARGUMENT, got %v (%v)", got, err)
	}
	if got := fieldViolation(t, err); got != wantField {
		t.Fatalf("refusal must name %q, named %q", wantField, got)
	}
	created, _, lerr := ops.List(context.Background(), operations.ListFilter{})
	if lerr != nil {
		t.Fatalf("list operations: %v", lerr)
	}
	if len(created) != 0 {
		t.Fatalf("refusal must happen BEFORE the operation is created, found %d operation(s)", len(created))
	}
}

func TestUpdateRules_BadPortRefusedBeforeOperation(t *testing.T) {
	sgr := kachomock.NewRepository()
	ops := repomock.NewOpsRepo()
	netA := ids.NewID(ids.PrefixNetwork)
	sg := seedMockSG(t, sgr, "P", netA, "sg-ports")

	uc := NewUpdateRulesUseCase(sgr, ops, newSGReaderMock(sgr))
	_, err := uc.Execute(context.Background(), UpdateRulesInput{
		SecurityGroupID:   sg,
		AdditionRuleSpecs: []domain.SecurityGroupRule{ruleWithPorts(65536, 65536)},
	})
	assertRefusedBeforeOperation(t, ops, err, "addition_rule_specs[0].ports.from_port")
}

func TestCreate_UnknownProtocolRefusedBeforeOperation(t *testing.T) {
	sgr := kachomock.NewRepository()
	ops := repomock.NewOpsRepo()
	nr := repomock.NewNetworkRepo()
	netA := ids.NewID(ids.PrefixNetwork)
	_, _ = nr.Insert(context.Background(), &domain.Network{ID: netA, ProjectID: "P", Name: "net-a"})

	bad := ruleWithPorts(80, 80)
	bad.ProtocolName = "klingon"
	uc := NewCreateSecurityGroupUseCase(sgr, nr, &repomock.ProjectClient{OK: true}, ops).
		WithSGReader(newSGReaderMock(sgr))
	_, err := uc.Execute(context.Background(), domain.SecurityGroup{
		ProjectID: "P", NetworkID: netA, Name: domain.RcNameVPC("sg-proto"),
		Rules: []domain.SecurityGroupRule{bad},
	})
	assertRefusedBeforeOperation(t, ops, err, "rule_specs[0].protocol_name")
}

func TestUpdate_InvertedPortRangeRefusedBeforeOperation(t *testing.T) {
	sgr := kachomock.NewRepository()
	ops := repomock.NewOpsRepo()
	netA := ids.NewID(ids.PrefixNetwork)
	sg := seedMockSG(t, sgr, "P", netA, "sg-upd")

	uc := NewUpdateSecurityGroupUseCase(sgr, ops)
	_, err := uc.Execute(context.Background(), UpdateInput{
		SecurityGroupID: sg,
		SecurityGroup: domain.SecurityGroup{
			ID: sg, ProjectID: "P", NetworkID: netA, Name: domain.RcNameVPC("sg-upd"),
			Rules: []domain.SecurityGroupRule{ruleWithPorts(443, 80)},
		},
		UpdateMask: []string{"rule_specs"},
	})
	assertRefusedBeforeOperation(t, ops, err, "rule_specs[0].ports.from_port")
}
