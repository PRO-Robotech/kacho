// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package domain

// CidrGroup — именованный набор префиксов: предмет, на который ссылается правило
// группы безопасности вместо того, чтобы нести свою копию перечня.
//
// Семантически-нагруженные поля (Name/Description/Labels) — newtypes со
// встроенным Validate(). `CreatedAt` сюда НЕ входит: он DB-managed и живёт в
// repo-сущности `CidrGroupRecord`.
//
// `ID` / `ProjectID` остаются голым `string` — это внешние reference-id, их
// формат проверяет `corevalidate.ResourceID` в use-case-слое.
//
// Два поля состава, а не одно с выведенным семейством, — решение, а не
// оформление: смешанный набор невыразим НА ВХОДЕ, до записи. У исполнителя один
// член чужого семейства снимает правило целиком (allow исчезает — трафик
// запрещён; drop исчезает — трафик разрешён), и отказ там наступает поздно, при
// разборе выражения, а не при записи.
type CidrGroup struct {
	ID          string
	ProjectID   string
	Name        RcNameVPC
	Description RcDescription
	Labels      RcLabels
	// V4CidrBlocks / V6CidrBlocks — текущий состав набора по семействам,
	// канонической сетевой формы (host-биты нулевые). Мутируются ТОЛЬКО
	// глаголами :add-cidr-blocks / :remove-cidr-blocks; Update их не касается.
	V4CidrBlocks []string
	V6CidrBlocks []string
}

// Validate проверяет семантически-нагруженные поля по domain-контракту.
// Возвращает доменную `*ValidationError` (stdlib, без gRPC) либо nil.
//
// Состав здесь НЕ проверяется: формат префикса и потолок — cross-field
// инварианты use-case-слоя (`validateCidrGroupBlocks` /
// `validateCidrGroupCardinality`), а последний вдобавок держится конструкцией
// базы, потому что синхронная проверка ограничивает один запрос, а накопленное
// между вызовами остаётся непроверенным.
func (g CidrGroup) Validate() error {
	return combineValidation(
		g.Name.Validate(),
		g.Description.Validate(),
		ValidateLabels(g.Labels),
	)
}

// Equal — deep equality по domain-полям. `CreatedAt` сюда не входит (он в
// repo-leaf Record). Оба семейства состава участвуют: набор, выпавший из
// сравнения, читался бы как «не изменился» на каждом пути, который принимает
// решение по этой функции.
func (g CidrGroup) Equal(other CidrGroup) bool {
	return g.ID == other.ID &&
		g.ProjectID == other.ProjectID &&
		g.Name == other.Name &&
		g.Description == other.Description &&
		LabelsEqual(g.Labels, other.Labels) &&
		stringSlicesEqual(g.V4CidrBlocks, other.V4CidrBlocks) &&
		stringSlicesEqual(g.V6CidrBlocks, other.V6CidrBlocks)
}

// CidrGroupBlockCount — общее число членов набора по обоим семействам. Ровно то
// число, которое ресурс отдаёт полем `cidr_block_count`: считается в одном
// месте, чтобы «сумма обоих семейств» не разошлась с тем, что видит вызывающий.
func (g CidrGroup) CidrGroupBlockCount() int {
	return len(g.V4CidrBlocks) + len(g.V6CidrBlocks)
}
