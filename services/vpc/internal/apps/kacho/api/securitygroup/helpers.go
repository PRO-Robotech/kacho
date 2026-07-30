// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package securitygroup

import (
	"context"
	"fmt"
	"net/netip"

	"google.golang.org/protobuf/types/known/anypb"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/dto"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"

	// Blank-import регистрирует трансферы SecurityGroup/time через init().
	_ "github.com/PRO-Robotech/kacho/services/vpc/internal/dto/toproto"
)

// validateCIDRPrefix — host-bits = 0; используется в правилах SG (sync-валидация
// каждого V4/V6 CIDR-блока). Параллельный к `service.validateCIDRPrefix`.
func validateCIDRPrefix(field, value string) error {
	prefix, err := netip.ParsePrefix(value)
	if err != nil {
		return serviceerr.InvalidArg(field, field+" must be a valid CIDR (e.g. 10.0.0.0/24)")
	}
	if prefix.Masked() != prefix {
		return serviceerr.InvalidArg(field,
			field+" must have zero host-bits (use the network address "+prefix.Masked().String()+")")
	}
	return nil
}

// validateSGRule — sync-валидация правила.
//
// Direction-семантика, диапазон портов, протокол и CIDR host-bits — cross-field
// invariants, не newtype-level (description/labels валидируются через
// r.Validate() внутри SecurityGroup.Validate()).
//
// Каждый путь, на котором правило приходит ОТ ВЫЗЫВАЮЩЕГО, проходит здесь
// СИНХРОННО, до создания операции: `Create.rule_specs`, `Update.rule_specs`,
// `UpdateRules.addition_rule_specs`. `UpdateRule` меняет только
// description/labels, портов и протокола не касается (его known-set маски —
// ровно эти два поля).
//
// Есть ровно один путь ПОМИМО них: `api/network/default_sg.go` пишет
// `domain.NewDefaultSecurityGroupRules()` прямо в writer-TX создания сети, минуя
// эту функцию. Это не дыра — правила там не тенантские, их сочиняет сам продукт,
// — но и не «все пути проходят здесь». Инвариант держится тестом
// `TestValidateSGRule_DefaultSecurityGroupRulesPass`: то, что продукт пишет мимо
// проверки, обязано её проходить. Добавляя новый inline-путь записи правил,
// добавь его в этот список или проведи через validateSGRule.
func validateSGRule(field string, r domain.SecurityGroupRule) error {
	if r.Direction != domain.SecurityGroupRuleDirectionIngress && r.Direction != domain.SecurityGroupRuleDirectionEgress {
		return serviceerr.InvalidArg(field+".direction", "direction must be INGRESS or EGRESS")
	}
	if err := validateSGRulePorts(field, r.FromPort, r.ToPort); err != nil {
		return err
	}
	if err := validateSGRuleProtocol(field, r.ProtocolName, r.ProtocolNumber); err != nil {
		return err
	}
	if err := serviceerr.FromValidation(r.Description.Validate()); err != nil {
		return err
	}
	if err := serviceerr.FromValidation(domain.ValidateLabels(domain.LabelsFromMap(r.Labels))); err != nil {
		return err
	}
	for i, c := range r.V4CidrBlocks {
		if err := validateCIDRPrefix(fmt.Sprintf("%s.cidr_blocks.v4_cidr_blocks[%d]", field, i), c); err != nil {
			return err
		}
	}
	// v6 CIDR-блоки правила валидируются симметрично v4 — иначе
	// малформированный/host-bits v6 CIDR попадал бы в БД как мусор.
	for i, c := range r.V6CidrBlocks {
		if err := validateCIDRPrefix(fmt.Sprintf("%s.cidr_blocks.v6_cidr_blocks[%d]", field, i), c); err != nil {
			return err
		}
	}
	return nil
}

// validateSGRulePorts — диапазон портов правила.
//
// Каждая граница — либо `-1` («любой порт»), либо число из объявленного в
// `PortRange` интервала `0-65535`. `-1` принимается ТОЛЬКО на обеих границах
// сразу: «от любого до 80» не диапазон, а два разных утверждения в одном поле.
// Перевёрнутый диапазон (`from > to`) не описывает ни одного порта, поэтому
// отвергается там же, а не сохраняется как правило, которое ничего не значит.
//
// Отказ называет ту границу, из-за которой он произошёл, — вызывающий правит
// поле, а не гадает.
func validateSGRulePorts(field string, from, to int64) error {
	fromField, toField := field+".ports.from_port", field+".ports.to_port"
	anyFrom, anyTo := from == domain.AnyPort, to == domain.AnyPort
	switch {
	case anyFrom && anyTo:
		return nil
	case anyFrom:
		return serviceerr.InvalidArg(toField,
			fmt.Sprintf("from_port %d means any port; to_port must be %d as well", domain.AnyPort, domain.AnyPort))
	case anyTo:
		return serviceerr.InvalidArg(fromField,
			fmt.Sprintf("to_port %d means any port; from_port must be %d as well", domain.AnyPort, domain.AnyPort))
	}
	if from < domain.MinPort || from > domain.MaxPort {
		return serviceerr.InvalidArg(fromField,
			fmt.Sprintf("from_port must be %d (any) or within %d-%d", domain.AnyPort, domain.MinPort, domain.MaxPort))
	}
	if to < domain.MinPort || to > domain.MaxPort {
		return serviceerr.InvalidArg(toField,
			fmt.Sprintf("to_port must be %d (any) or within %d-%d", domain.AnyPort, domain.MinPort, domain.MaxPort))
	}
	if from > to {
		return serviceerr.InvalidArg(fromField, "from_port must not be greater than to_port")
	}
	return nil
}

// validateSGRuleProtocol — протокол правила.
//
// `protocol_name` объявлен как значение реестра IANA; неизвестное имя —
// InvalidArgument, а не тихо сохранённая строка. `protocol_number` — номер из
// того же реестра (`0-255`) либо `-1` («любой»). Обе ветки oneof пусты =
// «любой протокол» — это законно и проверять нечего.
func validateSGRuleProtocol(field, name string, number int64) error {
	if name != "" && !domain.IsKnownProtocolName(name) {
		return serviceerr.InvalidArg(field+".protocol_name",
			"protocol_name must be "+domain.AnyProtocolName+" or an IANA protocol keyword (for example tcp, udp, icmp)")
	}
	if number != domain.AnyProtocolNumber && (number < domain.MinProtocolNumber || number > domain.MaxProtocolNumber) {
		return serviceerr.InvalidArg(field+".protocol_number",
			fmt.Sprintf("protocol_number must be %d (any) or an IANA protocol number within %d-%d",
				domain.AnyProtocolNumber, domain.MinProtocolNumber, domain.MaxProtocolNumber))
	}
	return nil
}

// Тексты ошибок для same-network-валидации SG-target-правил.
const (
	errRuleCrossNetwork  = "security group rule can only reference a security group in the same network"
	errRuleTargetMissing = "security group rule references a non-existent security group"
)

// validateSGTargetSameNetwork проверяет, что каждое SG-target-правило (`oneof
// target = security_group_id`) ссылается на SecurityGroup из той же Network,
// что и редактируемая SG.
//
// `ownerNetworkID` — network_id редактируемой SG (на Create приходит из
// request'а; на UpdateRules/UpdateRule резолвится use-case'ом из самой SG).
// `fieldFor(i)` строит имя поля для BadRequest.field_violations (напр.
// `rule_specs[0].security_group_id` / `addition_rule_specs[0].security_group_id`).
//
// CIDR / predefined правила не затрагиваются (нет `SecurityGroupID`). Cross-network
// → InvalidArgument; несуществующая target-SG (`repo.ErrNotFound`) →
// InvalidArgument (единый класс с cross-network) — НЕ NotFound, НЕ wrapSGErr.
// Проверка не TOCTOU-prone (network_id immutable); удаление target-SG —
// грациозный dangling-ref либо negative InvalidArgument.
func validateSGTargetSameNetwork(
	ctx context.Context,
	reader SecurityGroupReader,
	ownerNetworkID string,
	rules []domain.SecurityGroupRule,
	fieldFor func(i int) string,
) error {
	if reader == nil {
		return nil
	}
	// Цели резолвятся ОДНИМ запросом на весь набор и с дедупликацией: длину
	// набора выбирает вызывающий, а одна и та же цель в тысяче правил — это
	// одна строка, а не тысяча обращений к БД. Порядок сообщений сохраняется:
	// решение по каждому правилу принимается в исходном порядке по уже
	// полученной карте.
	targets := make([]string, 0, len(rules))
	seen := make(map[string]struct{}, len(rules))
	for _, r := range rules {
		if r.SecurityGroupID == "" {
			continue // CIDR / predefined / no target — not an SG-target rule.
		}
		if _, dup := seen[r.SecurityGroupID]; dup {
			continue
		}
		seen[r.SecurityGroupID] = struct{}{}
		targets = append(targets, r.SecurityGroupID)
	}
	if len(targets) == 0 {
		return nil
	}
	found, err := reader.GetMany(ctx, targets)
	if err != nil {
		return serviceerr.MapRepoErr(err)
	}
	for i, r := range rules {
		if r.SecurityGroupID == "" {
			continue
		}
		target, ok := found[r.SecurityGroupID]
		if !ok {
			return serviceerr.InvalidArg(fieldFor(i), errRuleTargetMissing)
		}
		if target.NetworkID != ownerNetworkID {
			return serviceerr.InvalidArg(fieldFor(i), errRuleCrossNetwork)
		}
	}
	return nil
}

// validateSGRulesCardinality — потолок числа правил, синхронно и ДО любого
// резолва целей. Без потолка стоимость одного запроса линейна по величине,
// которую выбирает вызывающий, и платится дважды (синхронно и в воркере).
// Проверяется ИТОГОВЫЙ набор, иначе он растёт серией формально законных
// запросов. DB-CHECK security_groups_rules_cardinality — атомарный backstop.
func validateSGRulesCardinality(field string, rules []domain.SecurityGroupRule) error {
	if len(rules) > domain.MaxSecurityGroupRules {
		return serviceerr.InvalidArg(field,
			fmt.Sprintf("at most %d rules per security group", domain.MaxSecurityGroupRules))
	}
	return nil
}

// assignRuleIDs присваивает каждому rule UID если он пустой.
func assignRuleIDs(rules []domain.SecurityGroupRule) []domain.SecurityGroupRule {
	out := make([]domain.SecurityGroupRule, len(rules))
	for i, r := range rules {
		if r.ID == "" {
			r.ID = ids.NewID(ids.PrefixSecurityGroup)
		}
		out[i] = r
	}
	return out
}

// marshalSecurityGroupRecord конвертирует repo-entity SG в *anypb.Any через
// DTO-реестр. Используется worker'ами Create/Update/UpdateRules/UpdateRule/Move
// для упаковки результата в Operation.response.
func marshalSecurityGroupRecord(rec *kacho.SecurityGroupRecord) (*anypb.Any, error) {
	var dst *vpcv1.SecurityGroup
	if err := dto.Transfer(dto.FromTo(*rec, &dst)); err != nil {
		return nil, fmt.Errorf("dto.Transfer SecurityGroup: %w", err)
	}
	return anypb.New(dst)
}

// ruleSpecFromProto конвертирует proto SecurityGroupRuleSpec → domain SecurityGroupRule.
// Description — newtype RcDescription, Direction — enum SecurityGroupRuleDirection.
// Labels на rule-уровне остаются map[string]string (JSONB-friendly, см.
// domain/security_group.go).
func ruleSpecFromProto(rs *vpcv1.SecurityGroupRuleSpec) domain.SecurityGroupRule {
	r := domain.SecurityGroupRule{
		Description: domain.RcDescription(rs.Description),
		Labels:      rs.Labels,
	}
	switch rs.Direction {
	case vpcv1.SecurityGroupRule_INGRESS:
		r.Direction = domain.SecurityGroupRuleDirectionIngress
	case vpcv1.SecurityGroupRule_EGRESS:
		r.Direction = domain.SecurityGroupRuleDirectionEgress
	}
	if rs.Ports != nil {
		r.FromPort = rs.Ports.FromPort
		r.ToPort = rs.Ports.ToPort
	}
	if name := rs.GetProtocolName(); name != "" {
		r.ProtocolName = name
	}
	if num := rs.GetProtocolNumber(); num != 0 {
		r.ProtocolNumber = num
	}
	if cb := rs.GetCidrBlocks(); cb != nil {
		r.V4CidrBlocks = cb.V4CidrBlocks
		r.V6CidrBlocks = cb.V6CidrBlocks
	}
	if sgID := rs.GetSecurityGroupId(); sgID != "" {
		r.SecurityGroupID = sgID
	}
	if pred := rs.GetPredefinedTarget(); pred != "" {
		r.PredefinedTarget = pred
	}
	return r
}
