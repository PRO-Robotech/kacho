// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package domain

// Builders для inline-собираемых domain-сущностей вокруг Network. Держат подальше
// от service-слоя inline-литералы с magic-константами, инкапсулируя:
//   * имя default-SG (formula: "default-sg-" + TruncateID(networkID)),
//   * описание default-SG,
//   * default-правила (INGRESS+EGRESS, protocol ANY, 0.0.0.0/0).

// DefaultSGName возвращает имя default-SG для сети по формуле
// `default-sg-<first 8 chars of network id>`.
func DefaultSGName(networkID string) string {
	return "default-sg-" + TruncateID(networkID)
}

// DefaultSGDescription — описание автосоздаваемого default-SG.
const DefaultSGDescription = "Default security group (auto-created by kacho-vpc)"

// NewDefaultSecurityGroupRules возвращает набор правил, который получает каждая
// автосозданная группа по умолчанию: разрешить весь ИСХОДЯЩИЙ трафик в обоих
// семействах адресов и НИ ОДНОГО входящего правила.
//
// # Посадка, а не оформление (решение владельца)
//
// Модель fail-closed в ОБЕ стороны: нет правила — значит не разрешено. Послабление,
// без которого арендатор не работает, объявлено РЕСУРСОМ, а не спрятано в
// умолчании: разница между «модель открыта» и «модель закрыта, а послабление
// объявлено ресурсом» в том, что второе ВИДНО в ответе `Get` и правится обычной
// правкой группы, а первое обнаруживается по последствиям.
//
// Почему исходящее вообще разрешено: безопасность требует закрытой модели,
// работоспособность — исходящего доступа. Арендатор, чья машина не может дойти до
// репозитория пакетов, заводит обращение в первый час.
//
// Почему В ОБОИХ СЕМЕЙСТВАХ: обещание симметрии семейств не сужается. Асимметрия
// была бы скрытой дырой наоборот — правило защищает одно семейство и не защищает
// другое, а арендатор об этом не знает.
//
// Что было до: два правила, разрешавшие весь ВХОД и весь выход, и только в IPv4.
// То есть вход был открыт у каждой сети по умолчанию, а IPv6 не покрыт вовсе.
//
// Это builder, а не глобальная переменная — каждый вызов отдаёт свежий слайс
// (вызывающий вправе мутировать без побочных эффектов у следующего). Направление —
// перечисление, а не голый строковый литерал.
func NewDefaultSecurityGroupRules() []SecurityGroupRule {
	return []SecurityGroupRule{
		{
			Direction:      SecurityGroupRuleDirectionEgress,
			ProtocolName:   "ANY",
			ProtocolNumber: -1,
			V4CidrBlocks:   []string{"0.0.0.0/0"},
		},
		{
			Direction:      SecurityGroupRuleDirectionEgress,
			ProtocolName:   "ANY",
			ProtocolNumber: -1,
			V6CidrBlocks:   []string{"::/0"},
		},
	}
}

// NewDefaultSecurityGroup собирает domain.SecurityGroup для default-SG сети.
// Чистый value-builder: `id` минтит use-case/repo-слой (ids.NewID(PrefixSecurityGroup))
// и передаёт сюда — domain не тянет infra-утилиту и остаётся детерминированным
// (stdlib+proto-only, dependency-rule). CreatedAt сюда не входит (DB-managed);
// caller (репозиторий) выставит время в Insert. Name/Description — newtypes
// (RcNameVPC / RcDescription).
//
// Используется service-слоем в worker'е Network.Create при
// KACHO_VPC_DEFAULT_SG_INLINE=true.
func NewDefaultSecurityGroup(id string, net Network) SecurityGroup {
	return SecurityGroup{
		ID:                id,
		ProjectID:         net.ProjectID,
		NetworkID:         net.ID,
		Name:              RcNameVPC(DefaultSGName(net.ID)),
		Description:       RcDescription(DefaultSGDescription),
		DefaultForNetwork: true,
		Rules:             NewDefaultSecurityGroupRules(),
	}
}
