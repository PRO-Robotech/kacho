// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package domain

// Network — сетевой ресурс.
//
// Семантически-нагруженные поля (Name/Description/Labels) — newtypes со
// встроенным Validate(). `CreatedAt` сюда НЕ входит — это DB-managed
// (DEFAULT now()) и живет в repo-сущности `NetworkRecord` (см.
// internal/repo/kacho/entity_network.go).
//
// `ID` / `ProjectID` / `DefaultSecurityGroupID` остаются голым `string` — это
// внешние reference-id (newtype добавит шум без выгоды; их валидация — на уровне
// `corevalidate.ResourceID` в service-слое перед запросом к репо).
type Network struct {
	ID                     string
	ProjectID              string
	Name                   RcNameVPC
	Description            RcDescription
	Labels                 RcLabels
	DefaultSecurityGroupID string
	// IPv4CidrBlocks / IPv6CidrBlocks — объявленный супернет сети (declared
	// supernet). У Network НЕТ primary-блока: это чистый набор супернет-блоков,
	// из которых нарезаются подсети. Каждый Subnet.ipv4CidrPrimary обязан быть
	// подмножеством одного из них (валидация на Subnet.Create). Мутируются только
	// через :add-cidr-blocks / :remove-cidr-blocks, не через Update (immutable).
	IPv4CidrBlocks []string
	IPv6CidrBlocks []string
	// DefaultRouteTableID — id system-provisioned default RouteTable сети (°),
	// создаётся при Network.Create, авто-ассоциируется к новым подсетям. Output-only.
	DefaultRouteTableID string
	// VRFID — SRv6 VRF tenancy id, аллоцируется control-plane'ом (sequence).
	// Output-only/инфра-чувствительное: отдается ТОЛЬКО через
	// InternalNetworkService.GetNetwork, не валидируется (не пользовательский ввод).
	VRFID uint32
}

// Validate проверяет все семантически-нагруженные поля Network по domain-
// контракту (разрешительная политика валидации + ограничения cardinality/ключа/
// значения label'ов). Возвращает доменную `*ValidationError` (stdlib, без gRPC) с
// FieldViolation'ами, либо nil; gRPC InvalidArgument строит serviceerr.FromValidation.
//
// Вызывается use-case-слоем ПЕРЕД repo.Insert / repo.Update — domain
// становится единственным источником правды о валидности.
func (n Network) Validate() error {
	return combineValidation(
		n.Name.Validate(),
		n.Description.Validate(),
		ValidateLabels(n.Labels),
	)
}

// Equal — deep equality по domain-полям. `CreatedAt` сюда не входит (он в
// repo-leaf Record). Используется для noop-detection в Update-flow и для
// equality-проверок в use-case тестах.
func (n Network) Equal(other Network) bool {
	return n.ID == other.ID &&
		n.ProjectID == other.ProjectID &&
		n.Name == other.Name &&
		n.Description == other.Description &&
		LabelsEqual(n.Labels, other.Labels) &&
		n.DefaultSecurityGroupID == other.DefaultSecurityGroupID &&
		n.DefaultRouteTableID == other.DefaultRouteTableID &&
		stringSlicesEqual(n.IPv4CidrBlocks, other.IPv4CidrBlocks) &&
		stringSlicesEqual(n.IPv6CidrBlocks, other.IPv6CidrBlocks)
}
