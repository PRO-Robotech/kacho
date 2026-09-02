// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package manifest

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"

	"github.com/PRO-Robotech/kacho/services/iam/internal/domain"
)

// roles.go — раздел `roles` (приёмка §2.6, §2.6а; сценарии MOD-MR-10 …
// MOD-MR-15).
//
// # Кластерный ярус ПРИНИМАЕТСЯ: отказ ФОРМЫ заменён отказом ВЛАДЕНИЯ
//
// Здесь стоял отказ `ErrSystemRoleNotAuthorable`: кластерный ярус отвергался
// потому, что системные роли сеет миграция. Довод был верен и предмет пережил:
// пока писателем остаётся миграция, встроенная в образ iam (`//go:embed`), новый
// модуль требует пересборки образа iam — то есть его релиза. Приёмка
// `roles-come-as-data-not-migrations.md` (§2.2, §3.1, §3.2) заводит ТРЕТЬЕГО
// писателя — применителя манифеста, — и отказ по ярусу становится ложью о
// продукте.
//
// Цена прежнего отказа измерена, а не предположена: ВСЕ живые системные роли —
// кластерные (`is_system` вычисляется ровно из непустого `cluster_id`), поэтому
// исполнимого входа, которым модуль объявил бы свою роль, не существовало ни
// одного. Раздел был объявлен, разобран, покрыт типами — и отвергался на
// единственном ярусе, на котором живут роли продукта. Это дословно класс
// «Неисполнимая возможность: два правила об одном поле».
//
// Что НЕ снимается вместе с ним: роль ЧУЖОГО модуля по-прежнему отвергается
// (`ErrRoleForeignModule`), и ярусы аккаунта и проекта принимаются с теми же
// текстами. Снятие, расширившее право объявления или сузившее соседнюю полосу,
// было бы регрессией, а не переносом.
//
// # Системность — СЛЕДСТВИЕ яруса, а не отдельный признак
//
// Контракт говорит это дословно (`role.proto`, DefinitionTier): `is_system`
// ВЫВОДИТСЯ из `tier_type == iam.cluster`, а не хранится отдельным флагом.
// Поэтому второго ключа «системная ли она» здесь нет by construction, и якорь
// кластерного яруса ЧИТАЕТСЯ: кластер один, и значение, отличное от синглтона,
// отвергается с именем поля. Молча подставить синглтон — не исход: автор
// манифеста получил бы успех на объявлении, которого платформа не исполняла.
//
// # Форма идентификатора роли объявлена ОДИН раз — `RoleIDForm`
//
// Правил о ней в дереве было ЧЕТЫРЕ, и они расходились: опубликованная схема
// требовала ровно двух сегментов и допускала заглавные; ограничение таблицы
// `roles_system_name_check` допускает до трёх сегментов и только нижний регистр;
// разбор проверял лишь наличие точки. Прогон образца схемы по живым именам:
// выразимо ТРИ из сорока восьми. Здесь форма объявляется однажды и равна
// ПЕРЕСЕЧЕНИЮ двух действующих правил — ограничения таблицы (верхняя граница,
// ban #5: её не правят) и требования `<модуль>.<имя>` самого манифеста, — а не
// третьему правилу.
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
	// ErrRoleIDOutOfForm — идентификатор не подчиняется единственной объявленной
	// форме имени системной роли. Отдельный отказ, а не общий «не той формы»:
	// чинится он иначе — переписыванием имени под ограничение таблицы, — и автор
	// обязан узнать ПРАВИЛО, а не только факт отказа.
	ErrRoleIDOutOfForm = errors.New("manifest: role id is outside the declared system-role name form")
	// ErrRoleForeignModule — роль чужого модуля: объявление за чужой домен.
	ErrRoleForeignModule = errors.New("manifest: role belongs to another module")
	// ErrRoleIDDuplicated — две роли под одним идентификатором.
	ErrRoleIDDuplicated = errors.New("manifest: two roles share one id")
	// ErrRoleTierRequired — роль не сказала, на каком ярусе она определена.
	ErrRoleTierRequired = errors.New("manifest: role tier is required")
	// ErrRoleTierUnknown — ярус вне закрытого набора.
	ErrRoleTierUnknown = errors.New("manifest: role tier type is outside the closed set")
	// ErrRoleTierAnchorUnknown — якорь яруса назван и не тот. Сегодня предмет у
	// него один — кластерный ярус: кластер в продукте СИНГЛТОН, поэтому значение
	// проверяемо, а «принять и не читать» на нём запрещено.
	ErrRoleTierAnchorUnknown = errors.New("manifest: role tier anchor is not the one this tier has")
	// ErrRoleRuleInvalid — выдача не проходит проверку домена. Несёт ДОСЛОВНЫЙ
	// текст домена: тексты отказов — часть контракта.
	ErrRoleRuleInvalid = errors.New("manifest: role rule is rejected by the domain")
	// ErrRoleRuleVerbsRetired — правило роли записано снятым ключом `verbs`.
	// Отдельный отказ, а не общий «неизвестное поле»: последний верен и
	// бесполезен — автор не узнаёт, чем чинится.
	ErrRoleRuleVerbsRetired = errors.New("manifest: `verbs` is not a form of a role right anymore")
	// ErrRoleNameRequired — роль не несёт человекочитаемого имени.
	ErrRoleNameRequired = errors.New("manifest: role name is required")
	// ErrRoleDescriptionTooShort — описание роли короче предела прозы.
	ErrRoleDescriptionTooShort = errors.New("manifest: role description is shorter than the declared bound")
	// ErrRoleRulesRequired — роль не несёт ни одного правила: выданная, она не
	// даёт НИ ОДНОГО права.
	ErrRoleRulesRequired = errors.New("manifest: role grants nothing")
)

// RoleIDForm — ЕДИНСТВЕННОЕ объявление формы идентификатора роли манифеста
// (приёмка §3.2.1). Экспортировано намеренно: опубликованная схема обязана
// нести ЭТО значение, а не свою копию, и сверяет их гейт Г10
// (`schemarolesform_internal_test.go`), а не надежда.
//
// Форма есть ПЕРЕСЕЧЕНИЕ двух уже действующих правил, а не третье правило:
//
//   - верхняя граница — ограничение таблицы `roles_system_name_check`
//     (`0056_role_definition_tier.sql`, применённая миграция, ban #5: её не
//     правят): нижний регистр, дефис в первом сегменте, подчёркивание в
//     сегментах после первого, НЕ БОЛЕЕ ТРЁХ сегментов;
//   - нижняя граница — сам манифест: идентификатор есть `<модуль>.<имя>`,
//     поэтому односегментное имя манифестом невыразимо by construction, и
//     `{1,2}` вместо `{0,2}` — конъюнкция с уже действующим требованием
//     `validateRoleIdentity`, а не сужение по вкусу.
//
// Проверка на живом наборе, обе стороны: образец принимает сорок четыре живых
// имени из сорока восьми и отвергает четыре — ровно `admin`, `edit`, `view`,
// `owner`, то есть в точности класс ролей БЕЗ модуля-владельца, недостижимый
// для любого манифеста (приёмка §3.4). Совпадение двух независимо полученных
// множеств и есть подтверждение, что граница проведена там.
const RoleIDForm = `^[a-z][a-z0-9-]*(\.[a-z][a-z0-9_]*){1,2}$`

// roleSystemNameConstraint — имя ограничения таблицы, которое отвергло бы
// негодное имя у писателя. Стоит в тексте отказа намеренно: без него автор
// узнаёт ФАКТ отказа и не узнаёт ПРАВИЛА, а SQLSTATE 23514 от писателя не
// назовёт ни поля, ни координаты.
const roleSystemNameConstraint = "roles_system_name_check"

// roleIDRe — скомпилированная `RoleIDForm`. Второго образца рядом не заводится:
// он разошёлся бы с первым молча — оба отвечают «валидно» на валидном входе.
var roleIDRe = regexp.MustCompile(RoleIDForm)

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
		faults = append(faults, validateRoleProse(role, doc, i)...)
		faults = append(faults, validateRoleTier(role, doc, i)...)
		if len(role.Rules) == 0 {
			faults = append(faults, linkFault{
				kind:  ErrRoleRulesRequired,
				coord: locate(doc, "roles", i),
				detail: fmt.Sprintf("roles[%d].rules: роль не несёт ни одного правила — выданная, "+
					"она не даёт НИ ОДНОГО права, и отличить её от неисполненной выдачи "+
					"вызывающему нечем: привязка есть, доступа нет", i),
			})
		}
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
	if !roleIDRe.MatchString(role.ID) {
		return []error{linkFault{
			kind:  ErrRoleIDOutOfForm,
			coord: locate(doc, "roles", i, "id"),
			detail: fmt.Sprintf("roles[%d].id: получено %q; форма имени роли — %s "+
				"(ограничение таблицы %s: нижний регистр, дефис в первом сегменте, "+
				"подчёркивание в последующих, не более трёх сегментов). Приняв такое имя, "+
				"манифест обещал бы роль, которую писатель отвергнет ограничением таблицы: "+
				"отказ пришёл бы SQLSTATE 23514 без поля и без координаты",
				i, role.ID, RoleIDForm, roleSystemNameConstraint),
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

// RoleTierTypes — ярусы, которые манифест вправе объявить. Перечень
// экспортирован и ОДИН: опубликованная схема несёт ЭТИ значения, а не свою
// копию, и сверяет их гейт Г10. Порядок канонический — от кластера к проекту.
//
// `iam.cluster` стоит здесь с этой работы. До неё он отвергался, и следствие
// было тихим: все живые системные роли — кластерные, значит исполнимого входа у
// раздела не существовало ни одного.
var RoleTierTypes = []string{
	domain.ScopeTypeClusterDotted,
	domain.ScopeTypeAccountDotted,
	domain.ScopeTypeProjectDotted,
}

// roleTierTypesList — перечень для текста отказа. Собирается из RoleTierTypes,
// а не выписывается: выписанный не сдвинулся бы от нового яруса.
func roleTierTypesList() string { return strings.Join(RoleTierTypes, " · ") }

// validateRoleProse — то, что роль говорит ЧЕЛОВЕКУ: имя и описание.
//
// Судится отдельно от идентификатора намеренно: идентификатор адресует роль
// машинно и уже проверен, а имя с описанием читает тот, кто решает, выдавать ли
// её. Роль без них выдаётся вслепую, и отказаться от такой выдачи не на чем.
func validateRoleProse(role *Role, doc *yaml.Node, i int) []error {
	var faults []error
	if strings.TrimSpace(role.Name) == "" {
		faults = append(faults, linkFault{
			kind:  ErrRoleNameRequired,
			coord: locate(doc, "roles", i),
			detail: fmt.Sprintf("roles[%d].name: роль не несёт человекочитаемого имени — "+
				"идентификатор адресует её машинно, а выбирает роль человек, и выбирать ему нечем", i),
		})
	}
	if proseShorterThan(role.Description, minProseRunes) {
		faults = append(faults, linkFault{
			kind:  ErrRoleDescriptionTooShort,
			coord: locate(doc, "roles", i),
			detail: fmt.Sprintf("roles[%d].description: %d знаков, требуется не менее %d — "+
				"описание отвечает на вопрос, кому эту роль выдают; строка короче предела "+
				"на него не отвечает и стоит ради прохождения проверки",
				i, utf8.RuneCountInString(strings.TrimSpace(role.Description)), minProseRunes),
		})
	}
	return faults
}

// validateRoleTier — ярус определения роли.
//
// Порядок проверок — часть контракта: сперва ярус назван, потом он из набора,
// потом якорь назван, и только потом якорь ПРОЧИТАН. Иначе автор получил бы
// отказ о якоре яруса, которого платформа не знает вовсе.
func validateRoleTier(role *Role, doc *yaml.Node, i int) []error {
	if role.Tier == nil {
		return []error{linkFault{
			kind:  ErrRoleTierRequired,
			coord: locate(doc, "roles", i),
			detail: fmt.Sprintf("roles[%d].tier: не сказано, на каком ярусе роль определена; "+
				"принимаются: %s", i, roleTierTypesList()),
		}}
	}
	if !slices.Contains(RoleTierTypes, role.Tier.TierType) {
		return []error{linkFault{
			kind:  ErrRoleTierUnknown,
			coord: locate(doc, "roles", i, "tier", "tierType"),
			detail: fmt.Sprintf("roles[%d].tier.tierType: получено %q; принимаются: %s",
				i, role.Tier.TierType, roleTierTypesList()),
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
	// Якорь кластерного яруса ЧИТАЕТСЯ, а не принимается молча: кластер в
	// продукте синглтон (`roles_definition_tier_xor` плюс единственная строка
	// `cluster`), поэтому у поля ровно два законных исхода — прочитать и
	// отвергнуть чужое значение. Молча подставить синглтон было бы третьим, и
	// он запрещён: автор манифеста получил бы успех на объявлении, которого
	// платформа не исполняла.
	if role.Tier.TierType == domain.ScopeTypeClusterDotted &&
		role.Tier.TierID != domain.ClusterSingletonID {
		return []error{linkFault{
			kind:  ErrRoleTierAnchorUnknown,
			coord: locate(doc, "roles", i, "tier", "tierId"),
			detail: fmt.Sprintf("roles[%d].tier.tierId: получено %q; кластер в продукте один, "+
				"и его якорь — %q. Подставить единственное значение молча нельзя: манифест "+
				"объявил бы ярус, которого платформа не исполняет",
				i, role.Tier.TierID, domain.ClusterSingletonID),
		}}
	}
	return nil
}
