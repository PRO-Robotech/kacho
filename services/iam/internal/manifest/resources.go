// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package manifest

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/PRO-Robotech/kacho/services/iam/internal/authzmap"
)

// resources.go — раздел `resources` (приёмка
// services/iam/docs/engineering/acceptance/module-manifest-resources-roles-deprecated.md,
// §1.3, §2.2 … §2.5; сценарии MOD-MR-01 … MOD-MR-09, MOD-MR-27).
//
// # Раздел НЕОДНОРОДЕН, и это главное о нём
//
// Решение эпика #1087 (исход B) говорит две вещи сразу, и вторая обычно теряется
// при пересказе: раздел ПОРОЖДАЕТСЯ из аннотаций контрактов и объявлением права
// не является — но руками в нём объявляется ровно то, чего аннотации не несут.
// То есть у одних ключей записи есть производитель в дереве, у других автор-
// человек, и перегенерация обязана сохранить вторые.
//
//	порождается    name · objectType · parents[] · verbs[]
//	объявляется    doc · relations[] · subjects[] · tiers[]
//
// Вид ключей записи называет сама запись — ключом `producer` из закрытого набора
// `derived | authored`. Он не порождается и ресурс не описывает: он говорит, чем
// являются ОСТАЛЬНЫЕ ключи, и без него вопрос «пережил ли авторский ключ
// перегенерацию» даже не формулируется.
//
// # Почему ДВА ресурса объявляются авторски — и обе причины постоянны
//
// Типов закрытой таблицы, не встречающихся ни в одном `scope_extractor.
// object_type` каталога, ровно два: `vpc_address_pool` (ресурс admin-only, его
// глаголы живут на внутреннем слушателе — ban #6) и `registry_repository`
// (объект составной, резолвится в обработчике, якорю нечего извлекать из поля
// запроса — анти-BOLA). Ни то, ни другое не пропуск, и порождение их не
// восстановит: без `producer: authored` цепочка «аннотации → resources →
// таблицы» потеряла бы две грантуемые записи МОЛЧА.
//
// # Правило вывода `objectType ← <module>_<resource>` СНЯТО целиком
//
// Оно покрывает 17 записей закрытой таблицы из 27 (без нормализации регистра —
// 8 из 27), то есть автор всё равно обязан знать, попадает ли его ресурс в
// исключение, — а это и есть та работа, которую вывод обещал снять. Раздел к
// тому же порождается, и у генератора нет причины опускать ключ, значение
// которого он уже держит в руках: опущение экономит байты файла и платит
// правилом, живущим в двух местах — в генераторе (когда опускать) и в
// загрузчике (как восстановить). Поэтому `objectType` обязателен у каждого
// ресурса, а его значение резолвится закрытой таблицей `authzmap`.
//
// # Форм у глагола ДВЕ, а правило класса ОДНО
//
// Короткая форма — строка (`get`), длинная — отображение (`{name:
// addCidrBlocks, class: update}`). Класс короткой выводит единственная
// экспортируемая функция ClassOfCanonicalVerb; второе объявление того же правила
// стережёт гейт дерева (internal/repohygiene TestVerbClassRuleIsDeclaredOnce),
// потому что единственность — свойство ДЕРЕВА, а не пакета.

// Виды находок различаются не ради красоты: «тип объекта не назван», «якорь вне
// набора», «класс не выводится» чинятся разными правками, и вызывающий (цель
// сборки, читающая дерево) вправе их различать.
var (
	// ErrResourceNameRequired — ресурс не назвал себя.
	ErrResourceNameRequired = errors.New("manifest: resource name is required")
	// ErrResourceNameDuplicated — два ресурса под одним именем. Отказ называет
	// ОБА индекса: названный первый заставил бы чинить по одному.
	ErrResourceNameDuplicated = errors.New("manifest: two resources share one name")
	// ErrObjectTypeRequired — тип объекта модели прав не назван. Правило вывода
	// из имени СНЯТО (см. шапку), поэтому восстановить его нечем.
	ErrObjectTypeRequired = errors.New("manifest: resource objectType is required")
	// ErrObjectTypeUnknown — тип объекта вне закрытой таблицы authzmap.
	ErrObjectTypeUnknown = errors.New("manifest: resource objectType is outside the closed table")
	// ErrParentRequired — указатель на объект-владелец не назван.
	ErrParentRequired = errors.New("manifest: resource parents are required")
	// ErrParentUnknown — тип объекта указателя вне закрытого набора.
	ErrParentUnknown = errors.New("manifest: resource parent type is outside the closed set")
	// ErrParentNameRequired — указатель не назвал себя.
	ErrParentNameRequired = errors.New("manifest: resource parent name is required")
	// ErrParentNameDuplicated — два указателя под одним именем. Второй объявил бы
	// то же отношение модели во второй раз, и верно из двух было бы одно.
	ErrParentNameDuplicated = errors.New("manifest: two resource parents share one name")
	// ErrParentFormRedundant — длинная форма указателя при совпадающих имени и
	// типе: одно значение, записываемое двумя способами.
	ErrParentFormRedundant = errors.New("manifest: parent written in the long form while its name equals its type")
	// ErrProducerRequired — запись не сказала, чем являются её ключи.
	ErrProducerRequired = errors.New("manifest: resource producer is required")
	// ErrProducerUnknown — вид ключей вне закрытого набора.
	ErrProducerUnknown = errors.New("manifest: resource producer is outside the closed set")
	// ErrVerbNameRequired — глагол не назвал себя.
	ErrVerbNameRequired = errors.New("manifest: verb name is required")
	// ErrVerbClassNotDerivable — класс не выводится из имени, и он не назван.
	ErrVerbClassNotDerivable = errors.New("manifest: verb class is not derivable from the name")
	// ErrVerbClassUnknown — класс вне закрытого набора.
	ErrVerbClassUnknown = errors.New("manifest: verb class is outside the closed set")
	// ErrRelationShadowsVerb — объявленное отношение занимает имя, порождаемое
	// глаголом. Два объявления одного предмета, из которых верно одно.
	ErrRelationShadowsVerb = errors.New("manifest: authored relation shadows a generated verb relation")
	// ErrRelationNameRequired — отношение не назвало себя.
	ErrRelationNameRequired = errors.New("manifest: relation name is required")
	// ErrCascadeTermIncomplete — терм каскада назван наполовину: без отношения
	// либо без указателя, от которого он выводится.
	ErrCascadeTermIncomplete = errors.New("manifest: cascade term is incomplete")
	// ErrCascadeFromUnknown — терм каскада выводится от указателя, которого у
	// ресурса нет: отношение адресовало бы объект, которого блок не объявляет.
	ErrCascadeFromUnknown = errors.New("manifest: cascade term derives from an undeclared parent")
	// ErrTierNameRequired — ярус не назвал себя.
	ErrTierNameRequired = errors.New("manifest: tier name is required")
	// ErrTierSourceUnknown — ярус выводится от отношения, которого блок не несёт.
	ErrTierSourceUnknown = errors.New("manifest: tier derives from a relation the block does not declare")
	// ErrTierFormRedundant — длинная форма яруса без собственных источников:
	// одно значение, записанное двумя способами.
	ErrTierFormRedundant = errors.New("manifest: tier written in the long form while deriving from the chain")
	// ErrSourceListEmpty — перечень источников объявлен пустым. Пустой перечень
	// неотличим от опущенного ключа, а формы «без источников вовсе» канон не несёт.
	ErrSourceListEmpty = errors.New("manifest: an empty source list is not a form of the model")
)

// canonicalVerbClasses — ЕДИНСТВЕННОЕ в дереве объявление правила «класс из
// имени»: имя глагола, ТОЧНО совпавшее с одним из этих пяти, и есть свой класс.
//
// Набор одновременно служит закрытым перечнем принимаемых значений ключа
// `class` — и это не совпадение, а построение: класс, которого нельзя вывести
// ни из одного канонического имени, никем не производился бы.
//
// Правило живёт ЗДЕСЬ, а не в authzmap рядом с verbClass, и это отступление от
// §6 приёмки названо вместе с замером: `authzmap.verbClass` — классификатор
// ЯРУСА (чтение · запись · администрирование) по 30 токенам, а не правило
// класса действия по пяти. Экспортировав его, задача экспортировала бы другое
// правило; предикат — `sed -n '/^func verbClass/,/^}/p'
// services/iam/internal/authzmap/permissions_to_relations.go`, в теле три
// возвращаемых яруса и ни одного класса.
var canonicalVerbClasses = []string{"get", "list", "create", "update", "delete"}

// ClassOfCanonicalVerb возвращает класс действия, выводимый из ИМЕНИ глагола, и
// ok=false, когда имя не совпадает ни с одним каноническим точно.
//
// Экспортирована, потому что импортёров у неё двое: загрузчик (восстанавливает
// класс короткой формы) и генератор раздела (#1092 — эмитит короткую форму ровно
// тогда, когда эта функция вернула ok). Тогда правило нельзя рассогласовать by
// construction, а не по договорённости.
func ClassOfCanonicalVerb(name string) (string, bool) {
	if contains(canonicalVerbClasses, name) {
		return name, true
	}
	return "", false
}

// CanonicalVerbs возвращает КОПИЮ закрытого набора канонических классов действия
// в объявленном порядке.
//
// Импортёров три, и каждый назван, потому что без перечислителя каждый держал бы
// ВТОРУЮ копию набора: рендер блоков модели (#1089) · отказ о пустом классе
// (`manifest/roleexport`, #1090), который обязан назвать автору пригодные классы
// ресурса · сам разбор. Правило членства (`ClassOfCanonicalVerb`) отвечает про
// одно имя и перечислить набор не даёт.
//
// Набор здесь один, а ПОРЯДОК блоков модели принадлежит канону и объявлен у
// рендера отдельно — их согласие держит проба равенства множеств
// (modelrender: TestCanonicalVerbOrderAgreesWithTheClassRule), а не совпадение,
// на которое никто не смотрит.
//
// Отдаётся копией, чтобы вызывающий не переписал набор на месте.
func CanonicalVerbs() []string {
	out := make([]string, len(canonicalVerbClasses))
	copy(out, canonicalVerbClasses)
	return out
}

// scopeAnchors — закрытый набор ЯКОРЕЙ ОБЛАСТИ платформы. Тот же словарь, что у
// областей выдачи (`iam.project | iam.account | iam.cluster`) без точечного
// префикса, и он ОСТАЁТСЯ закрытым: расширяется тип указателя, а не право.
var scopeAnchors = []string{"project", "account", "cluster"}

// parentTypeIsKnown — тип объекта, на который указатель вправе указывать.
//
// Принимается ДВА словаря, и они разные: якорь области (закрытый набор выше,
// `cluster` живёт только в нём — записи в таблице типов у него нет вовсе) и тип
// закрытой таблицы `authzmap`. Слить их одним ключом значило бы объявить, что
// ресурс-владелец и область выдачи суть одно, — а замер по канону говорит
// обратное: `registry_repository` указывает на `registry_registry`, который
// областью выдачи не является ни при каком написании.
func parentTypeIsKnown(typ string) bool {
	if contains(scopeAnchors, typ) {
		return true
	}
	_, ok := authzmap.DottedType(typ)
	return ok
}

// superAdminRelation — имя отношения супер-доступа в блоке модели. Объявлено
// ОДИН раз: имя порождается рендером, а на него ссылаются источники ярусов и
// якорь примечания, и второе объявление разошлось бы с первым молча.
const superAdminRelation = "super_admin"

// SuperAdminRelation возвращает имя отношения супер-доступа.
//
// Экспортировано ради рендера блоков модели: он порождает это отношение, а
// загрузчик проверяет ссылки на него, и вторая копия имени разошлась бы с первой.
func SuperAdminRelation() string { return superAdminRelation }

// defaultTierChain — ярусы по умолчанию, в порядке убывания прав. Порядок
// НЕСУЩИЙ: каждый следующий ярус выводится от предыдущего, а первый — от
// супер-доступа.
var defaultTierChain = []string{"admin", "editor", "viewer"}

// defaultSubjectSet — состав субъектов, который канон несёт у ярусов и у
// отношений действий по умолчанию.
var defaultSubjectSet = []string{"user", "service_account", "group#member"}

// DefaultTiers возвращает КОПИЮ умолчательной цепочки ярусов.
//
// Импортёров двое — рендер (порождает строки) и загрузчик (проверяет ссылки на
// ярусы у ресурса, который свой набор не объявил). Вторая копия цепочки
// разошлась бы с первой молча: обе стороны отвечают одинаково ровно там, где
// совпадают.
func DefaultTiers() []string {
	out := make([]string, len(defaultTierChain))
	copy(out, defaultTierChain)
	return out
}

// DefaultSubjects возвращает КОПИЮ умолчательного состава субъектов.
func DefaultSubjects() []string {
	out := make([]string, len(defaultSubjectSet))
	copy(out, defaultSubjectSet)
	return out
}

// resourceProducers — закрытый набор видов ключей записи.
var resourceProducers = []string{"derived", "authored"}

// Resource — один ресурс модуля.
//
// Порождённые и авторские ключи лежат В ОДНОЙ структуре намеренно: они
// описывают один предмет, и разнесение их по двум сообщениям заставило бы
// вызывающего склеивать записи по имени — ровно та работа, которую ключ
// `producer` снимает одним словом.
type Resource struct {
	// Name — имя ресурса в написании закрытой таблицы типов (единственное
	// число, camelCase): `securityGroup`, а не `security_groups`.
	Name string `yaml:"name"`
	// ObjectType — тип объекта модели прав. ОБЯЗАТЕЛЕН: правило вывода из имени
	// снято целиком (см. шапку файла).
	ObjectType string `yaml:"objectType"`
	// Parents — указатели на объекты, под которыми живёт ресурс, в порядке
	// блока модели. Первый — якорь области; остальные структурные.
	Parents []Parent `yaml:"parents"`
	// Producer — чем являются ОСТАЛЬНЫЕ ключи записи: `derived` (порождены из
	// аннотаций) либо `authored` (написаны человеком, аннотаций у ресурса нет).
	Producer string `yaml:"producer"`
	// Doc — АВТОРСКИЙ комментарий блока модели. Перегенерация обязана его
	// сохранить: производителя у него нет.
	Doc string `yaml:"doc"`
	// Subjects — АВТОРСКИЙ состав субъектов, когда он уже общего. Умолчание
	// здесь расширило бы доступ молча.
	Subjects []string `yaml:"subjects"`
	// Tiers — АВТОРСКИЙ набор ярусов, когда он отличается от общего: составом
	// либо источниками вывода.
	Tiers []ResourceTier `yaml:"tiers"`
	// Cascade — правая часть отношения супер-доступа, когда она отличается от
	// умолчания `super_admin from <первый указатель>`.
	Cascade []CascadeTerm `yaml:"cascade"`
	// Relations — АВТОРСКИЕ отношения модели, не выводимые ни из одного
	// действия: RPC под них нет.
	Relations []Relation `yaml:"relations"`
	// Verbs — действия ресурса. Обе формы записи принимаются (см. Verb).
	Verbs []Verb `yaml:"verbs"`
}

// Parent — указатель на объект, под которым живёт ресурс: ИМЯ отношения в блоке
// и ТИП объекта, на который оно указывает.
//
// # Имя и тип — разные строки, и это замер, а не осторожность
//
// У 26 модульных блоков канона из 27 они совпадают (`define project: [project]`),
// у одного — нет: `registry_repository` несёт `define parent: [registry_registry]`.
// Пока имя выводилось из типа, эта пара не порождалась НИ ПРИ КАКОМ значении
// ключа, то есть блок был недостижим by construction.
//
// # Форм записи ДВЕ, и каждое значение выразимо ровно ОДНОЙ
//
//	project                                    короткая: имя равно типу
//	{name: parent, type: registry_registry}    длинная: они различаются
//
// Длинная форма при совпадающих имени и типе ОТВЕРГАЕТСЯ: одно значение, два
// способа записи — ровно тот класс, который манифест и ловит. Тот же приём, что
// у действия (`Verb`): короткая форма ровно там, где выводить есть из чего.
type Parent struct {
	// Name — имя отношения-указателя в блоке модели.
	Name string `yaml:"name"`
	// Type — тип объекта, на который указатель указывает.
	Type string `yaml:"type"`

	// long — запись пришла длинной формой. Неэкспортируемое и без yaml-тега: это
	// НЕ ключ документа, а форма его записи, и обход полей структур (MOD-MF-21)
	// его не видит.
	long bool
}

// UnmarshalYAML принимает обе формы и НЕ теряет строгость к неизвестному ключу.
//
// Библиотека не проносит `Decoder.KnownFields(true)` внутрь собственного
// UnmarshalYAML (то же измерено у `Verb`), поэтому ключи сверяются здесь, до
// разбора, и отказ называет ключ и номер строки ровно как это делает библиотека.
func (p *Parent) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		if node.Tag != "!!str" {
			return fmt.Errorf("line %d: a parent written as a scalar must be a string, got %s",
				node.Line, node.Tag)
		}
		p.Name, p.Type, p.long = node.Value, node.Value, false
		return nil
	case yaml.MappingNode:
		for i := 0; i+1 < len(node.Content); i += 2 {
			key := node.Content[i]
			if key.Value != "name" && key.Value != "type" {
				return fmt.Errorf("line %d: field %s not found in type parent", key.Line, key.Value)
			}
		}
		var raw struct {
			Name string `yaml:"name"`
			Type string `yaml:"type"`
		}
		if err := node.Decode(&raw); err != nil {
			return err
		}
		p.Name, p.Type, p.long = raw.Name, raw.Type, true
		return nil
	default:
		return fmt.Errorf("line %d: a parent is a string or a mapping, got %s",
			node.Line, nodeKindName(node.Kind))
	}
}

// CascadeTerm — один терм правой части отношения супер-доступа: `<relation> from
// <from>`. Термы соединяются `or` в порядке объявления.
//
// # Написаний каскада в каноне ЧЕТЫРЕ, и это замер, а не осторожность
//
//	super_admin from <указатель>                                        19 блоков — умолчание
//	admin from account                                                   4 блока
//	any_admin from cluster                                               2 блока
//	admin from account or any_admin from cluster                         1 блок
//	super_admin from project or admin from account or any_admin ...      1 блок
//
// Восемь блоков несут написание, которого одно умолчание не даёт ни при каком
// входе. Форма структурная, а не текстовая: разбор строки `admin from account`
// завёл бы ВТОРОЙ разборщик грамматики модели прав, и он разошёлся бы с первым
// молча — на той самой форме, которой не знает.
type CascadeTerm struct {
	// Relation — отношение НА ОБЪЕКТЕ-ВЛАДЕЛЬЦЕ, от которого выводится каскад.
	// Загрузчик его существования не проверяет и проверить не может: отношение
	// принадлежит блоку владельца, а не этому. Проверяет сверка с каноном.
	Relation string `yaml:"relation"`
	// From — ИМЯ указателя этого ресурса, по которому идёт вывод. Указатель
	// обязан быть объявлен: иначе каскад адресует объект, которого блок не несёт.
	From string `yaml:"from"`
}

// ResourceTier — ярус прав ресурса.
//
// Имя типа несёт слово «ресурс», потому что в этом же пакете живёт ЯРУС РОЛИ
// (`Tier` в roles.go) — уровень, на котором роль определена. Предметы разные:
// здесь ступень прав внутри блока модели, там якорь определения роли, — и одно
// имя на двоих читалось бы как один предмет.
//
// # Форм записи ДВЕ, и каждое значение выразимо ровно ОДНОЙ
//
//	admin                                короткая: ярус выводится от ПРЕДЫДУЩЕГО
//	{name: admin, from: [owner, super_admin]}   длинная: источники названы
//
// Замер: `account` несёт `define admin: [...] or owner or super_admin`,
// `iam_user` — `define viewer: [...] or subject or editor`. Постоянная цепочка
// `admin → editor → viewer` этих двух написаний не даёт ни при каком входе.
//
// Длинная форма без собственных источников ОТВЕРГАЕТСЯ: она означала бы ровно то
// же, что короткая.
type ResourceTier struct {
	// Name — имя яруса в блоке модели.
	Name string `yaml:"name"`
	// From — отношения, от которых ярус выводится, в порядке правой части. Пусто
	// означает умолчание: предыдущий ярус, а для первого — супер-доступ.
	From []string `yaml:"from"`

	// long — запись пришла длинной формой (см. Parent.long).
	long bool
}

// UnmarshalYAML принимает обе формы яруса и НЕ теряет строгость к неизвестному
// ключу (см. Parent.UnmarshalYAML — то же измерено у действия).
func (tr *ResourceTier) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		if node.Tag != "!!str" {
			return fmt.Errorf("line %d: a tier written as a scalar must be a string, got %s",
				node.Line, node.Tag)
		}
		tr.Name, tr.From, tr.long = node.Value, nil, false
		return nil
	case yaml.MappingNode:
		for i := 0; i+1 < len(node.Content); i += 2 {
			key := node.Content[i]
			if key.Value != "name" && key.Value != "from" {
				return fmt.Errorf("line %d: field %s not found in type tier", key.Line, key.Value)
			}
		}
		var raw struct {
			Name string   `yaml:"name"`
			From []string `yaml:"from"`
		}
		if err := node.Decode(&raw); err != nil {
			return err
		}
		tr.Name, tr.From, tr.long = raw.Name, raw.From, true
		return nil
	default:
		return fmt.Errorf("line %d: a tier is a string or a mapping, got %s",
			node.Line, nodeKindName(node.Kind))
	}
}

// Relation — отношение модели прав, объявленное человеком.
//
// Текст определения здесь НЕ разбирается: его грамматика принадлежит модели
// прав, и второй её разборщик разошёлся бы с первым молча. Рендер блоков модели
// — предмет #1104 → #1089.
type Relation struct {
	Name       string `yaml:"name"`
	Definition string `yaml:"definition"`
}

// Verb — действие ресурса. Записывается ДВУМЯ формами:
//
//   - get                                   короткая: класс выводится из имени
//   - {name: addCidrBlocks, class: update}   длинная: класс назван
//
// Обычной структурой Go это не разбирается («cannot unmarshal !!str `get` into
// verb»), поэтому у типа свой UnmarshalYAML.
type Verb struct {
	Name  string `yaml:"name"`
	Class string `yaml:"class"`
}

// UnmarshalYAML принимает обе формы и НЕ теряет свойство, которое держит
// `Decoder.KnownFields(true)`.
//
// Библиотека не проносит строгость внутрь собственного UnmarshalYAML: узел
// разбирается умолчательно, и ключ `clazz` уехал бы молча — то есть контракт
// обещал бы возможность, которой нет. Поэтому ключи сверяются здесь, до разбора,
// и отказ называет ключ и номер строки ровно как это делает библиотека.
func (v *Verb) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		if node.Tag != "!!str" {
			return fmt.Errorf("line %d: a verb written as a scalar must be a string, got %s",
				node.Line, node.Tag)
		}
		v.Name = node.Value
		return nil
	case yaml.MappingNode:
		for i := 0; i+1 < len(node.Content); i += 2 {
			key := node.Content[i]
			if key.Value != "name" && key.Value != "class" {
				return fmt.Errorf("line %d: field %s not found in type verb", key.Line, key.Value)
			}
		}
		var raw struct {
			Name  string `yaml:"name"`
			Class string `yaml:"class"`
		}
		if err := node.Decode(&raw); err != nil {
			return err
		}
		v.Name, v.Class = raw.Name, raw.Class
		return nil
	default:
		return fmt.Errorf("line %d: a verb is a string or a mapping, got %s",
			node.Line, nodeKindName(node.Kind))
	}
}

// validateResources — форма и связность раздела `resources`.
//
// Класс короткой формы ВОССТАНАВЛИВАЕТСЯ здесь, а не в UnmarshalYAML: правило
// «класс из имени» обязано иметь одно место вызова, иначе оно поедет вслед за
// разбором и в генератор, и в загрузчик по отдельности.
//
// Находки собираются ВСЕ: названная первая заставила бы автора манифеста чинить
// их по одной, по прогону на каждую, и скрыла бы, сколько их всего.
func validateResources(m *Manifest, doc *yaml.Node) []error {
	var faults []error
	seen := map[string][]int{}

	for i := range m.Resources {
		r := &m.Resources[i]

		switch r.Name {
		case "":
			faults = append(faults, linkFault{
				kind:   ErrResourceNameRequired,
				coord:  locate(doc, "resources", i),
				detail: "ресурс не назвал себя: имя связывает его со строками каталога и с ролями",
			})
		default:
			seen[r.Name] = append(seen[r.Name], i)
		}

		faults = append(faults, validateResourceAnchors(r, doc, i)...)
		faults = append(faults, validateResourceCascade(r, doc, i)...)
		faults = append(faults, validateResourceTiers(r, doc, i)...)
		faults = append(faults, validateResourceVerbs(r, doc, i)...)
		faults = append(faults, validateResourceRelations(r, doc, i)...)
	}

	// Дубли называются ОБА, и в порядке документа: отказ, зависящий от обхода
	// карты, читался бы по-разному от прогона к прогону.
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		idx := seen[name]
		if len(idx) < 2 {
			continue
		}
		where := make([]string, 0, len(idx))
		for _, i := range idx {
			where = append(where, fmt.Sprintf("resources[%d]", i))
		}
		faults = append(faults, linkFault{
			kind:  ErrResourceNameDuplicated,
			coord: locate(doc, "resources", idx[0]),
			detail: fmt.Sprintf("имя %q объявлено %d раза: %s — имя ресурса адресует его в ролях, "+
				"и два адресата у одного имени делают выдачу невыразимой",
				name, len(idx), strings.Join(where, ", ")),
		})
	}
	return faults
}

// validateResourceAnchors — тип объекта, якорь области и вид ключей записи.
func validateResourceAnchors(r *Resource, doc *yaml.Node, i int) []error {
	var faults []error

	switch r.ObjectType {
	case "":
		faults = append(faults, linkFault{
			kind:  ErrObjectTypeRequired,
			coord: locate(doc, "resources", i, "objectType"),
			detail: "тип объекта модели прав обязан быть назван дословно: правило «тип = " +
				"<модуль>_<ресурс>» снято, оно не действует у 10 записей закрытой таблицы из 27",
		})
	default:
		if _, ok := authzmap.DottedType(r.ObjectType); !ok {
			faults = append(faults, linkFault{
				kind:  ErrObjectTypeUnknown,
				coord: locate(doc, "resources", i, "objectType"),
				detail: fmt.Sprintf("типа %q нет в закрытой таблице типов iam: селектор роли "+
					"адресовал бы тип, которого не существует, и не дал бы НИ ОДНОГО "+
					"пообъектного права — молча, при действующей на вид привязке", r.ObjectType),
			})
		}
	}

	faults = append(faults, validateResourceParents(r, doc, i)...)

	switch {
	case r.Producer == "":
		faults = append(faults, linkFault{
			kind:  ErrProducerRequired,
			coord: locate(doc, "resources", i, "producer"),
			detail: fmt.Sprintf("запись не сказала, чем являются её ключи; принимаются: %s. "+
				"Без этого перегенерация не знает, что сохранять, и вопрос «пережил ли "+
				"авторский ключ» не формулируется вовсе", strings.Join(resourceProducers, ", ")),
		})
	case !contains(resourceProducers, r.Producer):
		faults = append(faults, linkFault{
			kind:  ErrProducerUnknown,
			coord: locate(doc, "resources", i, "producer"),
			detail: fmt.Sprintf("вид ключей %q вне закрытого набора; принимаются: %s",
				r.Producer, strings.Join(resourceProducers, ", ")),
		})
	}
	return faults
}

// validateResourceParents — указатели ресурса: имя, тип и единственность формы.
//
// Находки собираются ВСЕ и по каждому указателю: названная первая заставила бы
// автора чинить их по одной, по прогону на каждую.
func validateResourceParents(r *Resource, doc *yaml.Node, i int) []error {
	var faults []error
	if len(r.Parents) == 0 {
		return []error{linkFault{
			kind:  ErrParentRequired,
			coord: locate(doc, "resources", i, "parents"),
			detail: fmt.Sprintf("указатель на объект-владелец не назван ни один; первый из них "+
				"есть якорь области, и принимаются якоря %s либо тип закрытой таблицы iam. "+
				"Без указателя блок модели не с чем связать, и каскад супер-доступа "+
				"выводить не от чего", strings.Join(scopeAnchors, ", ")),
		}}
	}

	seen := map[string]int{}
	for k := range r.Parents {
		p := &r.Parents[k]
		switch {
		case p.Name == "":
			faults = append(faults, linkFault{
				kind:  ErrParentNameRequired,
				coord: locate(doc, "resources", i, "parents", k),
				detail: fmt.Sprintf("resources[%d].parents[%d].name: указатель не назвал себя — "+
					"безымянное отношение нечем адресовать в модели", i, k),
			})
		default:
			if first, dup := seen[p.Name]; dup {
				faults = append(faults, linkFault{
					kind:  ErrParentNameDuplicated,
					coord: locate(doc, "resources", i, "parents", k),
					detail: fmt.Sprintf("resources[%d].parents[%d].name: имя %q уже объявлено "+
						"указателем resources[%d].parents[%d] — одно отношение модели, "+
						"объявленное дважды, и верно из двух одно", i, k, p.Name, i, first),
				})
			} else {
				seen[p.Name] = k
			}
		}

		switch {
		case p.Type == "":
			faults = append(faults, linkFault{
				kind:  ErrParentUnknown,
				coord: locate(doc, "resources", i, "parents", k),
				detail: fmt.Sprintf("resources[%d].parents[%d].type: тип объекта не назван; "+
					"принимаются якорь области (%s) либо тип закрытой таблицы iam",
					i, k, strings.Join(scopeAnchors, ", ")),
			})
		case !parentTypeIsKnown(p.Type):
			faults = append(faults, linkFault{
				kind:  ErrParentUnknown,
				coord: locate(doc, "resources", i, "parents", k),
				detail: fmt.Sprintf("resources[%d].parents[%d].type: тип %q вне закрытого набора; "+
					"принимаются якорь области (%s) либо тип закрытой таблицы iam. Указатель на "+
					"тип, которого не существует, объявил бы отношение, по которому никто не "+
					"постучится", i, k, p.Type, strings.Join(scopeAnchors, ", ")),
			})
		}

		if p.long && p.Name != "" && p.Name == p.Type {
			faults = append(faults, linkFault{
				kind:  ErrParentFormRedundant,
				coord: locate(doc, "resources", i, "parents", k),
				detail: fmt.Sprintf("resources[%d].parents[%d]: имя равно типу (%q), а запись "+
					"длинная — одно значение, записанное двумя способами. Напишите строкой: "+
					"`parents: [%s]`", i, k, p.Name, p.Name),
			})
		}
	}
	return faults
}

// declaredRelationNames — имена ВСЕХ отношений, которые блок этого ресурса
// объявляет: указатели, супер-доступ, авторские отношения, ярусы и отношения
// действий.
//
// Один перечень на двоих: им проверяются источники яруса (ссылка на отношение,
// которого блок не несёт, — висячая) и якорь примечания. Второй такой перечень
// разошёлся бы с первым молча — на том самом виде отношения, о котором не знает.
func declaredRelationNames(r *Resource) map[string]struct{} {
	out := make(map[string]struct{}, len(r.Parents)+len(r.Tiers)+len(r.Relations)+len(r.Verbs)+1)
	for _, p := range r.Parents {
		if p.Name != "" {
			out[p.Name] = struct{}{}
		}
	}
	out[superAdminRelation] = struct{}{}
	for _, rel := range r.Relations {
		if rel.Name != "" {
			out[rel.Name] = struct{}{}
		}
	}
	tiers := r.Tiers
	if len(tiers) == 0 {
		for _, name := range DefaultTiers() {
			out[name] = struct{}{}
		}
	}
	for _, t := range tiers {
		if t.Name != "" {
			out[t.Name] = struct{}{}
		}
	}
	for _, v := range r.Verbs {
		if v.Name != "" {
			out[VerbRelationName(v.Name)] = struct{}{}
		}
	}
	return out
}

// sortedNames — имена перечня в детерминированном порядке: отказ, зависящий от
// обхода карты, читался бы по-разному от прогона к прогону.
func sortedNames(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for name := range set {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// validateResourceCascade — термы каскада супер-доступа.
func validateResourceCascade(r *Resource, doc *yaml.Node, i int) []error {
	var faults []error
	if r.Cascade == nil {
		return nil
	}
	if len(r.Cascade) == 0 {
		return []error{linkFault{
			kind:  ErrSourceListEmpty,
			coord: locate(doc, "resources", i, "cascade"),
			detail: fmt.Sprintf("resources[%d].cascade: перечень термов объявлен пустым. Пустой "+
				"перечень неотличим от опущенного ключа, а блока без каскада канон не несёт: "+
				"опустите ключ, и каскад возьмёт умолчание `super_admin from <первый указатель>`", i),
		}}
	}
	declaredParents := make(map[string]struct{}, len(r.Parents))
	for _, p := range r.Parents {
		if p.Name != "" {
			declaredParents[p.Name] = struct{}{}
		}
	}
	for k, term := range r.Cascade {
		if term.Relation == "" || term.From == "" {
			faults = append(faults, linkFault{
				kind:  ErrCascadeTermIncomplete,
				coord: locate(doc, "resources", i, "cascade", k),
				detail: fmt.Sprintf("resources[%d].cascade[%d]: терм каскада есть пара "+
					"`<relation> from <from>`, и названы обе половины либо ни одной; "+
					"получено relation=%q, from=%q", i, k, term.Relation, term.From),
			})
			continue
		}
		if _, ok := declaredParents[term.From]; !ok {
			faults = append(faults, linkFault{
				kind:  ErrCascadeFromUnknown,
				coord: locate(doc, "resources", i, "cascade", k),
				detail: fmt.Sprintf("resources[%d].cascade[%d].from: указателя %q у ресурса нет; "+
					"объявлены: %s. Каскад по необъявленному указателю адресовал бы объект, "+
					"которого блок не несёт, — и вердикт по нему был бы «нет» всегда",
					i, k, term.From, strings.Join(sortedNames(declaredParents), ", ")),
			})
		}
	}
	return faults
}

// validateResourceTiers — имена ярусов и отношения, от которых они выводятся.
func validateResourceTiers(r *Resource, doc *yaml.Node, i int) []error {
	var faults []error
	known := declaredRelationNames(r)
	for k := range r.Tiers {
		t := &r.Tiers[k]
		if t.Name == "" {
			faults = append(faults, linkFault{
				kind:  ErrTierNameRequired,
				coord: locate(doc, "resources", i, "tiers", k),
				detail: fmt.Sprintf("resources[%d].tiers[%d].name: ярус не назвал себя — "+
					"безымянное отношение нечем адресовать в модели", i, k),
			})
			continue
		}
		if t.long && t.From == nil {
			faults = append(faults, linkFault{
				kind:  ErrTierFormRedundant,
				coord: locate(doc, "resources", i, "tiers", k),
				detail: fmt.Sprintf("resources[%d].tiers[%d]: длинная форма без ключа from "+
					"означает ровно то же, что короткая, — одно значение, записанное двумя "+
					"способами. Напишите именем: `- %s`", i, k, t.Name),
			})
		}
		if t.From != nil && len(t.From) == 0 {
			faults = append(faults, linkFault{
				kind:  ErrSourceListEmpty,
				coord: locate(doc, "resources", i, "tiers", k),
				detail: fmt.Sprintf("resources[%d].tiers[%d].from: перечень источников объявлен "+
					"пустым. Яруса без источника канон не несёт: опустите ключ, и ярус "+
					"выведется от предыдущего", i, k),
			})
		}
		for j, src := range t.From {
			if _, ok := known[src]; ok {
				continue
			}
			faults = append(faults, linkFault{
				kind:  ErrTierSourceUnknown,
				coord: locate(doc, "resources", i, "tiers", k),
				detail: fmt.Sprintf("resources[%d].tiers[%d].from[%d]: отношения %q блок не "+
					"объявляет; объявлены: %s. Вывод от несуществующего отношения даёт "+
					"вердикт «нет» всегда, оставаясь на вид полноценным ярусом",
					i, k, j, src, strings.Join(sortedNames(known), ", ")),
			})
		}
	}
	return faults
}

// validateResourceVerbs — имя и класс каждого действия; класс короткой формы
// восстанавливается ТУТ ЖЕ, единственным вызовом правила.
func validateResourceVerbs(r *Resource, doc *yaml.Node, i int) []error {
	var faults []error
	for j := range r.Verbs {
		v := &r.Verbs[j]
		if v.Name == "" {
			faults = append(faults, linkFault{
				kind:   ErrVerbNameRequired,
				coord:  locate(doc, "resources", i, "verbs", j),
				detail: "действие не назвало себя: имя действия — сегмент права, по которому его выдают",
			})
			continue
		}
		if v.Class == "" {
			class, ok := ClassOfCanonicalVerb(v.Name)
			if !ok {
				faults = append(faults, linkFault{
					kind:  ErrVerbClassNotDerivable,
					coord: locate(doc, "resources", i, "verbs", j),
					detail: fmt.Sprintf("%s: класс действия %q не выводится — из имени класс берётся "+
						"ТОЛЬКО при точном совпадении с одним из %s; назовите класс явно",
						fmt.Sprintf("resources[%d].verbs[%d].class", i, j), v.Name,
						strings.Join(canonicalVerbClasses, " · ")),
				})
				continue
			}
			v.Class = class
		}
		if !contains(canonicalVerbClasses, v.Class) {
			faults = append(faults, linkFault{
				kind:  ErrVerbClassUnknown,
				coord: locate(doc, "resources", i, "verbs", j),
				detail: fmt.Sprintf("%s: класс %q вне закрытого набора; принимаются: %s",
					fmt.Sprintf("resources[%d].verbs[%d].class", i, j), v.Class,
					strings.Join(canonicalVerbClasses, ", ")),
			})
		}
	}
	return faults
}

// validateResourceRelations — объявленное отношение не вправе занять имя,
// которое порождает глагол того же ресурса.
func validateResourceRelations(r *Resource, doc *yaml.Node, i int) []error {
	var faults []error
	generated := map[string]string{}
	for _, v := range r.Verbs {
		if v.Name != "" {
			generated[VerbRelationName(v.Name)] = v.Name
		}
	}
	for k, rel := range r.Relations {
		if rel.Name == "" {
			faults = append(faults, linkFault{
				kind:   ErrRelationNameRequired,
				coord:  locate(doc, "resources", i, "relations", k),
				detail: "отношение не назвало себя: безымянное отношение нечем адресовать в модели",
			})
			continue
		}
		verb, shadows := generated[rel.Name]
		if !shadows {
			continue
		}
		faults = append(faults, linkFault{
			kind:  ErrRelationShadowsVerb,
			coord: locate(doc, "resources", i, "relations", k),
			detail: fmt.Sprintf("%s: имя %q уже порождается глаголом %q того же ресурса — "+
				"два объявления одного отношения, из которых верно одно",
				fmt.Sprintf("resources[%d].relations[%d].name", i, k), rel.Name, verb),
		})
	}
	return faults
}

// VerbRelationName — имя отношения модели, порождаемое действием.
//
// Экспортирована, потому что тот же вывод делает рендер блоков модели (#1089):
// вторая копия правила разошлась бы с первой молча, и разошлась бы там, где
// расхождение не видно — обе стороны отвечают одинаково на входе, где правило
// совпадает.
//
// # Приведение к нижнему регистру — НЕ косметика, и цена его отсутствия измерена
//
// Авторский глагол пишется верблюжьим (`addTargets`), отношение модели — строчным:
// канон несёт `define v_addtargets`. Тот же вывод уже сделан эмиттером —
// `authzmap.targetGroupVerbRelations` объявляет набор `nlb_target_group` именами
// `v_addtargets`/`v_removetargets` и говорит об этом дословно: «имя, написанное
// иначе, чем его собирает эмиттер, адресовало бы отношение, по которому никто не
// постучится».
//
// Без приведения эта функция расходилась с эмиттером на КАЖДОМ неканоническом
// глаголе, и расхождение было тихим: сравнение сторон не совпало бы ни разу и
// отняло бы живое право, выглядя рабочим. Тот же класс каталог держит
// ограничением таблицы — `CHECK (verb = lower(btrim(verb)))`.
//
// Держит правило проба против ДЕРЕВА (verb_relation_name_test.go), а не против
// литерала рядом: литерал согласился бы с любой редакцией правила.
func VerbRelationName(verb string) string { return "v_" + strings.ToLower(verb) }
