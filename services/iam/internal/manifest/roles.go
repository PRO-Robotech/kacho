// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package manifest

import (
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/PRO-Robotech/kacho/services/iam/internal/domain"
)

// roles.go — раздел `roles` (приёмка §2.6, §2.6а; сценарии MOD-MR-10 …
// MOD-MR-15).
//
// # Единственный источник ролей сегодня — МИГРАЦИИ, и манифест их не замещает
//
// Решение эпика говорит, что раздел остаётся авторским, потому что аннотации о
// нём не говорят ничего. Это верно и недостаточно: аннотации молчат, а миграции
// ГОВОРЯТ — системных ролей вида `<модуль>.<ресурс>.<ярус>` объявлено
// применёнными миграциями 51, а применённую миграцию не правят (ban #5).
//
// Значит манифест, объявляющий те же роли, был бы ВТОРЫМ объявлением. Поэтому
// раздел объявляет роли уровня аккаунта и проекта, которые уезжают через
// существующий RoleService.Create, а системная роль отвергается ЯВНО — с именем
// поля и причиной. Это исход 2 запрета «принято-и-проигнорировано»: приняв, мы
// вернули бы вызывающему успех и уверенность, что его роль заведена, тогда как
// заводит её миграция, которой в изменении нет.
//
// # Системность — СЛЕДСТВИЕ яруса, а не отдельный признак
//
// Контракт говорит это дословно (`role.proto`, DefinitionTier): `is_system`
// ВЫВОДИТСЯ из `tier_type == iam.cluster`, а не хранится отдельным флагом.
// Поэтому отказ по ярусу и есть отказ системной роли, а не его приближение, и
// второго ключа «системная ли она» здесь нет by construction.
//
// # Право роли записывается ключом `classes`, и это ЕДИНСТВЕННАЯ его форма
//
// Здесь стояло обратное — «ОТСУТСТВИЕ ключа `classes`, потому что раскрывать
// `classes → verbs` некому». Оговорка была верна в день записи и пережила свой
// предмет по ОБЕИМ своим половинам (приёмка `classes-form-of-role-right.md`
// §0.3): раскрыватель появился (`roleexport`), а раскрывать нечего вовсе —
// перевод `classes → Verbs` есть КОПИЯ, а не раскрытие, потому что значение под
// обоими написаниями одно и то же: перечень ОБОЗНАЧЕНИЙ КЛАССА.
//
// Отсюда устройство:
//
//   - `classes:` — форма права роли. Значение — обозначения класса ДВУХ
//     законных словарей: каноническое имя (`get list create update delete`) и
//     имя, объявленное снятым разделом `deprecatedVerbs` ЭТОГО же манифеста
//     (`read`). Закрытой проверки набора здесь НЕТ намеренно: словарь снятых
//     имён принадлежит манифесту, поэтому «вне обоих словарей» есть суждение о
//     ЗНАЧЕНИИ, и выносит его стадия 1 своей причиной. Загрузчик судит ФОРМУ:
//     ключ, тип, мощность;
//   - `verbs:` — снят и отвергается ЯВНО, с указанием преемника
//     (`refuseRuleVerbs`). Молчаливое сохранение прежнего смысла держало бы два
//     ключа об одном предмете, из которых описан один;
//   - поимённый перечень ДЕЙСТВИЙ формой права роли не является и здесь не
//     заводится: принять его, не умея проверить полноту по классу, значило бы
//     свести его к классу молча. Форма возвращается вместе с проверкой полноты
//     (#1844).
//
// # Расхождение имён с `domain.Rule` ОБЪЯВЛЕНО, а не запрещено
//
// Хранимая форма несёт для этого значения ОДНО поле — `Verbs`, — и второго не
// получит: поле публичного контракта необратимо, а читателя у него не было бы
// (единственный путь материализации читает `Verbs`). Поэтому имена расходятся, и
// расхождение объявлено СЛОВАРЁМ (`ruleKeyToDomainField`), который самоистекает:
// запись, чьей стороны в дереве больше нет, роняет пробу перевода.
//
// # Форму самой выдачи судит ДОМЕН, а не копия его правил здесь
//
// Подстановка, взаимоисключающие селекторы, мощности, грамматика токенов,
// снятые типы и доступность признаковой выборки — всё это уже выражено в
// `domain.Rule.Validate`, и её тексты суть часть контракта. Копия этих правил
// здесь разошлась бы с оригиналом при первой же правке домена, и разошлась бы
// молча: обе стороны отвечают одинаково на законном входе.

var (
	// ErrRoleIDRequired — роль не назвала себя.
	ErrRoleIDRequired = errors.New("manifest: role id is required")
	// ErrRoleIDMalformed — идентификатор роли не той формы `<module>.<name>`.
	ErrRoleIDMalformed = errors.New("manifest: role id is not `<module>.<name>`")
	// ErrRoleForeignModule — роль чужого модуля: объявление за чужой домен.
	ErrRoleForeignModule = errors.New("manifest: role belongs to another module")
	// ErrRoleIDDuplicated — две роли под одним идентификатором.
	ErrRoleIDDuplicated = errors.New("manifest: two roles share one id")
	// ErrRoleTierRequired — роль не сказала, на каком ярусе она определена.
	ErrRoleTierRequired = errors.New("manifest: role tier is required")
	// ErrRoleTierUnknown — ярус вне закрытого набора.
	ErrRoleTierUnknown = errors.New("manifest: role tier type is outside the closed set")
	// ErrSystemRoleNotAuthorable — системная роль в манифесте. Отдельный отказ,
	// а не общий «ярус неверен»: чинится он другим — не правкой манифеста, а
	// миграцией, и автор обязан это узнать.
	ErrSystemRoleNotAuthorable = errors.New("manifest: a system role is not authorable by a manifest")
	// ErrRoleRuleInvalid — выдача не проходит проверку домена. Несёт ДОСЛОВНЫЙ
	// текст домена: тексты отказов — часть контракта.
	ErrRoleRuleInvalid = errors.New("manifest: role rule is rejected by the domain")
	// ErrRoleRuleVerbsRetired — правило роли записано снятым ключом `verbs`.
	// Отдельный отказ, а не общий «неизвестное поле»: последний верен и
	// бесполезен — автор не узнаёт, чем чинится.
	ErrRoleRuleVerbsRetired = errors.New("manifest: `verbs` is not a form of a role right anymore")
)

// Role — роль, объявленная манифестом модуля.
type Role struct {
	// ID — `<модуль>.<имя>`; модуль обязан совпадать с модулем манифеста.
	ID string `yaml:"id"`
	// Name — человекочитаемое имя роли.
	Name string `yaml:"name"`
	// Description — для чего роль. Роль без назначения некому отозвать:
	// следующий не знает, действует ли ещё основание.
	Description string `yaml:"description"`
	// Tier — ярус, на котором роль ОПРЕДЕЛЕНА. Указатель: «ярус не назван» и
	// «назван пустым» суть разные утверждения, и второе — тоже ошибка автора.
	Tier *Tier `yaml:"tier"`
	// Rules — выдачи роли, изоморфные `domain.Rule`.
	Rules []Rule `yaml:"rules"`
}

// Tier — ярус определения роли, изоморфный `iam.v1.DefinitionTier`.
type Tier struct {
	TierType string `yaml:"tierType"`
	TierID   string `yaml:"tierId"`
}

// Rule — одна выдача роли.
//
// Имена ключей совпадают с полями `domain.Rule` всюду, кроме ОДНОГО названного
// расхождения: право роли пишется ключом `classes`, а хранится полем `Verbs`.
// Расхождение объявлено словарём ниже, и объявленность утверждается пробой
// перевода (MOD-RC-06 … MOD-RC-08), а не выписанным списком.
type Rule struct {
	Module    string   `yaml:"module"`
	Resources []string `yaml:"resources"`
	// Classes — обозначения КЛАССА, которыми правило распоряжается. Два
	// законных словаря: каноническое имя и имя, объявленное снятым разделом
	// `deprecatedVerbs` этого манифеста. Значение судит стадия 1, форму —
	// загрузчик.
	Classes       []string          `yaml:"classes"`
	ResourceNames []string          `yaml:"resourceNames"`
	MatchLabels   map[string]string `yaml:"matchLabels"`
}

// ruleKeyToDomainField — СЛОВАРЬ объявленных расхождений «ключ манифеста → поле
// `domain.Rule`».
//
// Он и есть то, ради чего писался прежний изоморфизм по именам, только
// допускающее НАЗВАННОЕ расхождение вместо запрета всякого. Словарь
// самоистекает: запись, чьей стороны в дереве больше нет, роняет пробу
// перевода, — поэтому «расхождение объявлено» не переживает свой предмет.
var ruleKeyToDomainField = map[string]string{"classes": "Verbs"}

// DomainRule — выдача манифеста в форме домена. Перевод ОДИН и здесь: вторая
// точка перевода разошлась бы с первой на первом же новом поле.
//
// Значение `Classes` кладётся в `Verbs` ДОСЛОВНО — элемент в элемент, без
// приведения и без сортировки. Приведение снятого имени к его классу сделало бы
// невоспроизводимой строку живой роли: применённая миграция
// `0031_reseed_system_roles_rules.sql` несёт у ярусов чтения ровно
// `["read","list","get"]`, а приведение дало бы `["get","list","get"]` — другую
// строку. Во что снятое имя разрешается, решает стадия 1 (`roleexport.classOf`),
// и решает ОДИН раз.
func (r Rule) DomainRule() domain.Rule {
	return domain.Rule{
		Module:        r.Module,
		Resources:     r.Resources,
		Verbs:         r.Classes,
		ResourceNames: r.ResourceNames,
		MatchLabels:   r.MatchLabels,
	}
}

// validateRoles — форма раздела `roles`. Находки собираются ВСЕ.
func validateRoles(m *Manifest, doc *yaml.Node) []error {
	var faults []error
	seen := map[string]int{}

	for i := range m.Roles {
		role := &m.Roles[i]
		faults = append(faults, validateRoleIdentity(role, m.Module, doc, i, seen)...)
		faults = append(faults, validateRoleTier(role, doc, i)...)
		for j, rule := range role.Rules {
			if err := rule.DomainRule().Validate(false); err != nil {
				faults = append(faults, linkFault{
					kind:   ErrRoleRuleInvalid,
					coord:  locate(doc, "roles", i, "rules", j),
					detail: err.Error(),
				})
			}
		}
	}
	return faults
}

// validateRoleIdentity — идентификатор роли: назван · той формы · своего модуля
// · не повторён.
func validateRoleIdentity(role *Role, module string, doc *yaml.Node, i int, seen map[string]int) []error {
	if role.ID == "" {
		return []error{linkFault{
			kind:   ErrRoleIDRequired,
			coord:  locate(doc, "roles", i),
			detail: "роль не назвала себя: выдача ссылается на роль идентификатором, и безымянную назвать нечем",
		}}
	}
	owner, _, ok := strings.Cut(role.ID, ".")
	switch {
	case !ok || owner == "":
		return []error{linkFault{
			kind:  ErrRoleIDMalformed,
			coord: locate(doc, "roles", i, "id"),
			detail: fmt.Sprintf("roles[%d].id: идентификатор %q не той формы: `<модуль>.<имя>`",
				i, role.ID),
		}}
	case owner != module:
		return []error{linkFault{
			kind:  ErrRoleForeignModule,
			coord: locate(doc, "roles", i, "id"),
			detail: fmt.Sprintf("roles[%d].id: роль %q принадлежит модулю %q, а манифест — модуля %q; "+
				"объявлять роль за чужой домен манифест не вправе", i, role.ID, owner, module),
		}}
	}
	if prev, dup := seen[role.ID]; dup {
		return []error{linkFault{
			kind:  ErrRoleIDDuplicated,
			coord: locate(doc, "roles", i, "id"),
			detail: fmt.Sprintf("идентификатор %q объявлен дважды: roles[%d] и roles[%d] — "+
				"выдача, сославшаяся на него, адресует две роли сразу", role.ID, prev, i),
		}}
	}
	seen[role.ID] = i
	return nil
}

// validateRoleTier — ярус определения роли.
//
// Порядок проверок — часть контракта: системный ярус отвергается СВОИМ отказом
// ДО общего «ярус вне набора», иначе автор получил бы «неизвестный ярус» о
// ярусе, который платформе прекрасно известен и просто не его.
func validateRoleTier(role *Role, doc *yaml.Node, i int) []error {
	if role.Tier == nil {
		return []error{linkFault{
			kind:  ErrRoleTierRequired,
			coord: locate(doc, "roles", i),
			detail: fmt.Sprintf("roles[%d].tier: не сказано, на каком ярусе роль определена; "+
				"принимаются: %s · %s", i, domain.ScopeTypeAccountDotted, domain.ScopeTypeProjectDotted),
		}}
	}
	if role.Tier.TierType == domain.ScopeTypeClusterDotted {
		return []error{linkFault{
			kind:  ErrSystemRoleNotAuthorable,
			coord: locate(doc, "roles", i, "tier", "tierType"),
			detail: fmt.Sprintf("roles[%d].tier.tierType: получено %q; принимаются: %s · %s. "+
				"Системность роли ВЫВОДИТСЯ из яруса, и системные роли сеются миграцией — "+
				"манифест их не замещает, а применённую миграцию не правят",
				i, role.Tier.TierType, domain.ScopeTypeAccountDotted, domain.ScopeTypeProjectDotted),
		}}
	}
	if _, _, ok := domain.CustomDefinitionTierToScope(role.Tier.TierType, role.Tier.TierID); !ok {
		return []error{linkFault{
			kind:  ErrRoleTierUnknown,
			coord: locate(doc, "roles", i, "tier", "tierType"),
			detail: fmt.Sprintf("roles[%d].tier.tierType: получено %q; принимаются: %s · %s",
				i, role.Tier.TierType, domain.ScopeTypeAccountDotted, domain.ScopeTypeProjectDotted),
		}}
	}
	if role.Tier.TierID == "" {
		return []error{linkFault{
			kind:  ErrRoleTierRequired,
			coord: locate(doc, "roles", i, "tier", "tierId"),
			detail: fmt.Sprintf("roles[%d].tier.tierId: якорь яруса не назван — ярус без якоря "+
				"не адресует ни одного объекта", i),
		}}
	}
	return nil
}
