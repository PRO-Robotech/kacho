// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package securitygroup

import (
	"strings"
	"testing"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
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

// Отказ приходит СИНХРОННО, первым стейтментом, до создания операции: e2e видит
// 400 с кодом 3, а не 200 с операцией, которая тихо примет неверное правило.
func TestUpdateRules_BadPortRefusedSynchronously(t *testing.T) {
	err := validateSGRule("addition_rule_specs[0]", ruleWithPorts(65536, 65536))
	if err == nil {
		t.Fatal("out-of-range port must be refused")
	}
	if !strings.HasPrefix(fieldViolation(t, err), "addition_rule_specs[0].") {
		t.Fatalf("refusal must name the offending element, named %q", fieldViolation(t, err))
	}
}
