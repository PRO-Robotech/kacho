// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package domain — newtypes общего назначения для VPC-ресурсов и их Validate().
//
// Семантически-нагруженные поля (Name/Description/Labels) — не голый `string` /
// `map[string]string`, а self-validating newtypes: вся валидация живет в домене,
// он становится источником истины. Все newtypes реализуют единый контракт
// `Validate() error`, возвращая доменную `*ValidationError` (stdlib, без gRPC);
// трансляция в gRPC InvalidArgument — serviceerr.FromValidation.
package domain

import (
	"regexp"
	"unicode/utf8"

	"github.com/PRO-Robotech/kacho/pkg/validate/nameform"
)

// ---- Newtypes для базовых строковых полей -----------------------------------

// RcNameVPC — имя VPC-ресурса (Network, Subnet, Address, RouteTable,
// SecurityGroup, NetworkInterface, CidrGroup, AddressPool, Gateway). Форму
// задаёт ЕДИНСТВЕННОЕ объявление дерева — `corevalidate.NameForm`; своей у
// сервиса больше нет (#715).
type RcNameVPC string

// RcDescription — описание ресурса; UTF-8 длина ≤ 256.
type RcDescription string

// ---- Labels (typed map key/value) -------------------------------------------

// LabelKey — ключ label (`^[a-z][-_./\\@a-z0-9]{0,62}$`, 1..63 bytes).
type LabelKey string

// LabelVal — значение label (0..63 bytes).
type LabelVal string

// RcLabels — набор labels с типизированными key/value. Тонкая обертка над map,
// чтобы domain не зависел от сторонних контейнеров; контракт — Len/Get/Put/Iterate.
type RcLabels map[LabelKey]LabelVal

// Len возвращает число пар.
func (d RcLabels) Len() int { return len(d) }

// Get возвращает значение по ключу и признак наличия.
func (d RcLabels) Get(k LabelKey) (LabelVal, bool) {
	v, ok := d[k]
	return v, ok
}

// Put кладет пару, лениво инициализируя набор (zero value пригоден к использованию).
func (d *RcLabels) Put(k LabelKey, v LabelVal) {
	if *d == nil {
		*d = make(RcLabels)
	}
	(*d)[k] = v
}

// Iterate обходит пары; возврат false из fn останавливает обход. Порядок не определен.
func (d RcLabels) Iterate(fn func(LabelKey, LabelVal) bool) {
	for k, v := range d {
		if !fn(k, v) {
			return
		}
	}
}

// ---- Regex'ы ---------------------------------------------------------------
//
// Формы ИМЕНИ здесь нет и быть не должно: её единственное объявление —
// `corevalidate.NameForm` в общем фундаменте. Прежняя редакция этого места
// объявляла себя «источником истины» и держала СВОЮ форму, шире общей
// (заглавные, подчёркивание). Двух источников истины об одном предмете не
// бывает — бывает один верный и один, о расхождении которого никто не узнает;
// так и вышло (#715).

var labelKeyRe = regexp.MustCompile(`^[a-z][-_./\\@a-z0-9]{0,62}$`)

const (
	// MaxNameLen — максимум для Name полей ресурсов.
	MaxNameLen = 63
	// MaxDescriptionLen — лимит описания (UTF-8 rune count).
	MaxDescriptionLen = 256
	// MaxLabels — максимальное число label-пар на ресурс.
	MaxLabels = 64
	// MaxNetworkCidrBlocks — потолок declared-супернета сети НА СЕМЕЙСТВО
	// (ipv4_cidr_blocks / ipv6_cidr_blocks по отдельности). Набор — tenant-
	// управляемый и аддитивный (:add-cidr-blocks идемпотентен и накапливается
	// между вызовами), при этом он парсится заново на КАЖДОМ Subnet.Create /
	// Subnet.AddCidrBlocks и целиком сериализуется в каждом Network.Get/List —
	// без потолка это unbounded рост на горячем пути. Дублируется DB-CHECK
	// networks_cidr_blocks_cardinality (миграция 0016) как атомарный backstop.
	MaxNetworkCidrBlocks = 64
	// MaxSubnetCidrBlocks — потолок набора диапазонов подсети НА СЕМЕЙСТВО.
	// Набор тоже tenant-управляемый и аддитивный, а на каждое изменение он
	// перекладывается в нормализованную child-таблицу по строке на диапазон
	// (под row-lock подсети и share-lock сети) и попарно проверяется на
	// пересечение; кроме того он целиком уезжает в каждый ответ Get/List.
	// Дублируется DB-CHECK subnets_cidr_blocks_cardinality (миграция 0024).
	MaxSubnetCidrBlocks = 64
	// MaxCidrGroupBlocks — потолок состава именованного набора префиксов НА
	// СЕМЕЙСТВО. Набор аддитивен и управляется арендатором (:add-cidr-blocks
	// идемпотентен и накапливается между вызовами), целиком уезжает в каждый
	// ответ Get/List и раскладывается в строки дочерней таблицы по строке на
	// член.
	//
	// Решающая причина именно этой величины — не стоимость чтения, а то, чем
	// набор СТАНОВИТСЯ у исполнителя: ссылка на набор экономит место в плоскости
	// управления, а на узле множественная клауза разворачивается по правилу на
	// члена. То есть потолок набора и есть потолок размера правила, поэтому он
	// взят равным адресным наборам сети и подсети (`MaxNetworkCidrBlocks` /
	// `MaxSubnetCidrBlocks`), а не потолку числа правил.
	//
	// Дублируется DB-CHECK cidr_groups_cidr_cardinality (миграция 0035) как
	// атомарный backstop: синхронная проверка ограничивает ОДИН запрос, а CHECK
	// отвечает КАЖДОМУ писателю.
	MaxCidrGroupBlocks = 64
	// MaxNICSecurityGroups — потолок числа групп безопасности на интерфейсе.
	// Массив приходит от вызывающего и резолвится при каждом создании и
	// обновлении интерфейса; кроме того каждая группа участвует в предикате
	// «на меня ссылаются» при её удалении. Дублируется DB-CHECK
	// network_interfaces_sg_cardinality (миграция 0024).
	MaxNICSecurityGroups = 16
	// MaxSecurityGroupRules — потолок числа правил в группе безопасности.
	// Набор задаётся вызывающим одним сообщением, а каждое правило со ссылкой
	// на другую группу требует резолва этой ссылки — без потолка стоимость
	// одного запроса линейна по величине, которую выбирает вызывающий.
	// Дублируется DB-CHECK security_groups_rules_cardinality (миграция 0024).
	MaxSecurityGroupRules = 256
	// MaxStaticRoutes — потолок числа статических маршрутов в таблице
	// маршрутизации. Набор задаёт вызывающий телом запроса (RouteTable.Create и
	// Update с маской static_routes несут ИТОГОВЫЙ набор целиком; аддитивных
	// глаголов у маршрутов нет — они сняты с контракта вместе со своими
	// сообщениями, потому что StaticRoute не несёт идентичности и адресовать
	// поэлементную правку нечем). Длина оплачивается
	// трижды: синхронным разбором каждой записи (префикс без host-bits +
	// next-hop), сериализацией набора в JSONB и полной выдачей набора в КАЖДОМ
	// Get/List этой таблицы и в payload каждого её события outbox. Без потолка
	// единственный ограничитель — предел размера одного gRPC-сообщения.
	//
	// 256, как у правил группы, а НЕ 64: маршрут — единица политики, по одной
	// записи на пункт назначения, и их число растёт вместе с числом достижимых
	// сетей — тот же профиль, что у правил. 64 стоит там, где набор описывает
	// адресный план (супернет сети, диапазоны подсети) и где каждая запись
	// дороже: попарная проверка пересечений квадратична, а сама запись
	// раскладывается в строку дочерней таблицы под блокировками. У маршрута нет
	// ни того, ни другого — его запись самая дешёвая из четырёх наборов, поэтому
	// потолок берётся по верхней границе ряда, а не по нижней.
	//
	// Дублируется DB-CHECK route_tables_static_routes_cardinality (миграция
	// 0028) как атомарный backstop: синхронная проверка ограничивает один запрос,
	// прошедший через use-case, а CHECK — саму строку, независимо от writer'а.
	MaxStaticRoutes = 256
	// MaxLabelKeyLen — длина ключа label в байтах.
	MaxLabelKeyLen = 63
	// MaxLabelValueLen — длина значения label в байтах.
	MaxLabelValueLen = 63
)

// ---- Validate()-методы ------------------------------------------------------

// Validate проверяет имя против единственной формы дерева.
//
// Форма берётся из `nameform` — пакета БЕЗ транспорта, поэтому домен остаётся
// stdlib-чистым и не тянет gRPC (правило слоёв, `architecture.md`). Своей формы
// у сервиса нет и заводить её нельзя: две формы одного правила расходятся молча
// (#715).
//
// ПУСТАЯ СТРОКА ЗДЕСЬ ПРОХОДИТ, и это не послабление, а необходимость.
// `nameform.OK("")` — ЛОЖЬ, но newtype валидируется на ОБОИХ путях, а на пути
// создания совокупная `Validate()` ресурса исполняется РАНЬШЕ, чем существует
// идентификатор (сеть: проверка до `ids.NewID`, подстановка — после). Отвергай
// newtype пустую строку — всякое создание без имени падало бы ДО того, как
// умолчание вообще можно подставить.
//
// Поэтому контракт newtype ровно такой: «форма годная ЛИБО пусто-до-подстановки».
// Законно ли пустое ИМЕННО СЕЙЧАС, решает use-case: на создании оно заменяется
// `validate.NameOrDefault`, на правке — `validate.NameOnUpdate`. Не «чини» это
// на строгую проверку — сломаешь создание без имени во всех девяти ресурсах разом.
func (n RcNameVPC) Validate() error {
	s := string(n)
	if s == "" {
		return nil
	}
	if !nameform.OK(s) {
		return newValidationError("name", nameFormViolationMsg)
	}
	return nil
}

// nameFormViolationMsg — текст отказа по форме имени.
//
// Форма подставляется из канона, а поясняющий хвост — ЛИТЕРАЛ, и это признанный
// форк: `validate.Name` на пути правки строит ТОТ ЖЕ текст своим литералом, а
// сюда его не позвать — он возвращает транспортную ошибку, из-за которой домен и
// тянул бы gRPC. Тон сообщения — часть контракта (`api-conventions.md`), поэтому
// расхождение здесь было бы наблюдаемо арендатором: один и тот же негодный ввод
// отвечал бы по-разному на создании и на правке.
//
// Форк не оставлен на честное слово — его держит проба
// `name_message_parity_test.go`: она сверяет ЭТОТ текст с тем, что производит
// `validate.Name`, побайтово, и краснеет в день, когда любая из сторон поправится
// одна. Настоящее снятие форка — за владельцем `nameform`: текст обязан жить там
// же, где форма.
const nameFormViolationMsg = "name must match " + nameform.Form +
	" (lowercase letters, digits, hyphens; starts and ends with a letter or digit; 1..63 chars)"

// Validate проверяет длину description (UTF-8 rune count ≤ MaxDescriptionLen).
func (d RcDescription) Validate() error {
	if utf8.RuneCountInString(string(d)) > MaxDescriptionLen {
		return newValidationError("description", "description length exceeds 256 chars")
	}
	return nil
}

// Validate проверяет LabelKey-регекс (1..63 bytes, lowercase letters / digits /
// `-_./\\@`).
func (k LabelKey) Validate() error {
	s := string(k)
	if len(s) == 0 || len(s) > MaxLabelKeyLen || !labelKeyRe.MatchString(s) {
		return newValidationError("labels."+s, "invalid label key (1..63 chars, lowercase letters, digits, _-./\\@)")
	}
	return nil
}

// Validate проверяет LabelVal (0..63 bytes; пустая строка OK).
func (v LabelVal) Validate() error {
	if len(string(v)) > MaxLabelValueLen {
		return newValidationError("labels", "label value exceeds 63 chars")
	}
	return nil
}

// ValidateLabels пробегает по всем парам RcLabels и валидирует ключ + значение.
// Аналог corevalidate.Labels: возвращает первую ошибку (как и старый код), плюс
// дополнительно проверяет cardinality ≤ MaxLabels.
//
// Это свободная функция, а не метод: набор label'ов валидируется в контексте
// всего ресурса, поэтому вызов `ValidateLabels(n.Labels)` из `Network.Validate()`
// читается естественнее отдельного `Labels.Validate()`.
func ValidateLabels(labels RcLabels) error {
	if labels.Len() > MaxLabels {
		return newValidationError("labels", "too many labels (max 64)")
	}
	var firstErr error
	labels.Iterate(func(k LabelKey, v LabelVal) bool {
		if err := k.Validate(); err != nil {
			firstErr = err
			return false
		}
		if err := v.Validate(); err != nil {
			firstErr = err
			return false
		}
		return true
	})
	return firstErr
}

// ---- Helpers для конверсии RcLabels ↔ map[string]string ----------------------

// LabelsFromMap конвертирует обычный map[string]string в RcLabels.
// Используется в handler-слое: gRPC request приходит с map[string]string,
// внутри домена он становится RcLabels. nil-map → пустой RcLabels.
func LabelsFromMap(m map[string]string) RcLabels {
	var d RcLabels
	for k, v := range m {
		d.Put(LabelKey(k), LabelVal(v))
	}
	return d
}

// LabelsToMap — обратное преобразование, для DTO (dto/toproto).
// Возвращает nil если RcLabels пуст: пустой ресурс без labels отдает `Labels: nil`
// в proto (labels отсутствует в JSON).
func LabelsToMap(d RcLabels) map[string]string {
	if d.Len() == 0 {
		return nil
	}
	m := make(map[string]string, d.Len())
	d.Iterate(func(k LabelKey, v LabelVal) bool {
		m[string(k)] = string(v)
		return true
	})
	return m
}

// ---- Equal helpers ----------------------------------------------------------

// LabelsEqual — set-equality для RcLabels: равны, если одинаковое число пар
// и каждая пара (key→value) совпадает. Порядок (как у map) — не важен.
//
// Используется в `<Resource>.Equal()` для noop-detection в Update-flow и в
// equality-проверках use-case тестов. Порядок (как у map) не важен.
func LabelsEqual(a, b RcLabels) bool {
	if a.Len() != b.Len() {
		return false
	}
	equal := true
	a.Iterate(func(k LabelKey, v LabelVal) bool {
		bv, ok := b.Get(k)
		if !ok || bv != v {
			equal = false
			return false
		}
		return true
	})
	return equal
}

// stringSlicesEqual — order-sensitive equality для []string (reference-id
// массивов: SecurityGroupIDs, V4AddressIDs, V6AddressIDs у NIC). Для consistency
// выбран order-sensitive вариант: порядок reference-id фиксирован сервис-слоем
// (validate + insert) и не должен меняться без явного intent'а в Update.
func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// labelsMapEqual — equality для `map[string]string` (rule-level labels у
// SecurityGroupRule, см. domain/security_group.go: rule.Labels не RcLabels —
// JSONB round-trip ограничение). Order-insensitive (map-семантика).
func labelsMapEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if bv, ok := b[k]; !ok || bv != v {
			return false
		}
	}
	return true
}
