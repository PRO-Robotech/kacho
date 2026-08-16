// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

import (
	"strings"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
)

// DataplaneConfig — секция `dataplane`: то, что посадка ЗАЯВЛЯЕТ о датаплейне,
// которому управляющий контур vpc отдаёт принятое от арендатора, — возможности
// его исполнителя и адресное пространство, которое платформа держит за собой.
//
// # Зачем это вообще настройка, а не константа продукта
//
// Контур принимает адресные диапазоны, правила и ограничения, а исполняет их не
// он. Возможности исполнителя контур не знает и вывести не может: они зависят от
// стенда. Пока их никто не объявил, «принято» и «реализуемо» неотличимы — запрос
// проходит проверку контура и не может быть исполнен, а узнать об этом можно
// только по последствиям в чужой отладке. Поэтому профиль ОБЪЯВЛЯЕТСЯ, а страж
// старта (Config.ValidateExecutorProfile, S5) не поднимает боевую посадку, чьё
// объявление противоречит тому, что контур уже принимает.
//
// Тот же довод — у перечня служебных диапазонов (ReservedPrefixes, страж S6):
// какая часть адресного пространства обслуживает саму платформу, зависит от
// посадки, а литерал в коде описывал бы один стенд и лгал бы про остальные.
//
// # Полярность умолчаний — «не объявлено», а не «умеет»
//
// Нулевое значение означает НЕобъявленную возможность. Обратная полярность была бы
// удобнее чарту и неверна по существу: посадка, забывшая объявить профиль,
// получала бы «умеет всё» молча — ровно то состояние, ради исключения которого
// страж написан.
type DataplaneConfig struct {
	// Executor — профиль возможностей исполнителя.
	Executor ExecutorProfileConfig `mapstructure:"executor"`

	// ReservedPrefixes — адресные диапазоны, которые платформа держит ЗА СОБОЙ:
	// служебные адреса узлов, адреса служб внутри подсети, точка получения
	// метаданных экземпляра. Оба семейства адресов, каждая запись — CIDR в
	// канонической форме.
	//
	// Подсеть арендатора, объявленная поверх такого диапазона, принимается
	// контуром и не работает: продукт «иногда не работает», а отладка уходит в
	// сеть, тогда как причина — в перекрытии. Поэтому перечень проверяется на
	// пути запроса (Subnet.Create и `:addCidrBlocks` — единственные два места,
	// где диапазон подсети объявляется).
	//
	// Читать это поле НАПРЯМУЮ нельзя — только через Config.ReservedPrefixes:
	// сырая настройка бывает непустой при нуле объявленных диапазонов (одинокая
	// запятая разбирается в две пустые записи), и предикат по её длине разошёлся
	// бы с предикатом читателя ровно там, где расхождение опасно. Негодную запись
	// (опечатка, непустые host-биты, форма IPv4-в-IPv6, всё семейство целиком)
	// отвергает страж старта, а не путь запроса.
	ReservedPrefixes []string `mapstructure:"reserved-prefixes"`
}

// ExecutorProfileConfig — признаки и гарантии исполнителя датаплейна.
//
// Каждое поле читается стражем старта: поле, которое не участвует ни в одном
// решении, было бы принятым-и-проигнорированным — оператор задаёт значение, чарт
// его доставляет, поведение не меняется.
type ExecutorProfileConfig struct {
	// OverlappingTenantAddresses — исполнитель изолирует ОДИНАКОВЫЕ адреса разных
	// арендаторов.
	//
	// Несущий признак профиля. Уникальность диапазонов у vpc ограничена сетью по
	// построению (`EXCLUDE USING gist (network_id WITH =, …)` в миграциях подсетей
	// и пулов), поэтому два арендатора вправе держать один и тот же диапазон и
	// держат его штатно. Исполнитель, который такие адреса не различает, сводит их
	// трафик вместе. Боевая посадка без этого признака НЕ ПОДНИМАЕТСЯ: пока
	// изоляция не заявлена, принимать пересекающиеся диапазоны нельзя.
	OverlappingTenantAddresses bool `mapstructure:"overlapping-tenant-addresses"`

	// StateTrackingFamilies — семейства адресов, для которых исполнитель
	// отслеживает состояние соединения (`v4`, `v6`).
	//
	// Читать это поле НАПРЯМУЮ нельзя — только через Config.StateTrackingFamilies:
	// сырая настройка бывает непустой при нуле объявленных семейств (одинокая
	// запятая разбирается в две пустые записи), и предикат по её длине разошёлся бы
	// с предикатом читателя ровно там, где расхождение опасно.
	StateTrackingFamilies []string `mapstructure:"state-tracking-families"`

	// NamedSetReferenceInRule — правило может ссылаться на именованный набор
	// адресов вместо литерального диапазона.
	//
	// Предмет не гипотетический: цель правила группы безопасности задаётся ЛИБО
	// диапазонами, ЛИБО идентификатором группы (взаимоисключающие ветви уже
	// принятого контракта). Исполнитель, который вторую ветвь не умеет, оставляет
	// часть принятых правил неисполнимой.
	NamedSetReferenceInRule bool `mapstructure:"named-set-reference-in-rule"`

	// GuaranteedPayloadBytes — полезный размер кадра, который исполнитель
	// гарантированно проносит без фрагментации. Ноль — не «гарантия нуля», а
	// отсутствие гарантии.
	GuaranteedPayloadBytes int `mapstructure:"guaranteed-payload-bytes"`

	// GuaranteedBandwidthPerInterfaceMbps — полоса, гарантированная одному
	// интерфейсу, Мбит/с.
	GuaranteedBandwidthPerInterfaceMbps int `mapstructure:"guaranteed-bandwidth-per-interface-mbps"`

	// ConnectionLimitPerInterface — предел одновременно отслеживаемых соединений на
	// интерфейс.
	ConnectionLimitPerInterface int `mapstructure:"connection-limit-per-interface"`

	// ConnectionRateLimitPerInterfacePerSecond — темп установления новых соединений
	// на интерфейс, в секунду, который держит исполнитель этого стенда.
	//
	// Отдельная величина от предыдущей, а не её следствие, — по той же причине, по
	// какой они порознь опубликованы арендатору: предел одновременных защищает
	// ПАМЯТЬ под записями о соединениях, а темп — стоимость их появления. Поток
	// полуоткрытых соединений исчерпывает второе, не приближаясь к первому.
	ConnectionRateLimitPerInterfacePerSecond int `mapstructure:"connection-rate-limit-per-interface-per-second"`

	// ConnectionRateBurstPerInterface — кратковременный всплеск темпа установления
	// соединений на интерфейс, который держит исполнитель этого стенда.
	//
	// Объявляется ВМЕСТЕ с постоянным темпом и порознь с ним не имеет смысла: темп
	// без права на всплеск отвергал бы законную неравномерность нагрузки, а всплеск
	// без темпа не говорит, сколько его можно держать.
	ConnectionRateBurstPerInterface int `mapstructure:"connection-rate-burst-per-interface"`

	// TenantSettableBandwidthLimit — арендатор вправе сам задать ограничение полосы
	// (а не только получить гарантированную). Объявляется вместе с
	// GuaranteedBandwidthPerInterfaceMbps: ограничивать не от чего, если полоса не
	// объявлена, и такая пара отвергается как самопротиворечие.
	TenantSettableBandwidthLimit bool `mapstructure:"tenant-settable-bandwidth-limit"`
}

// knownAddressFamilies — словарь семейств. Закрытый: за пределами v4/v6 у контура
// нет ни поля запроса, ни проверки, поэтому третье значение здесь означало бы
// объявление, которому нечего соответствовать.
var knownAddressFamilies = []string{"v4", "v6"}

// AddressFamilies — нормализованное множество семейств, объявленных посадкой.
//
// Тип заведён по той же причине, что круг доверенных отправителей в общей
// библиотеке: у величины обязан быть ОДИН предикат непустоты, и спрашивать его
// обязаны и страж старта, и читатель. Сырая настройка для этого не годится —
// `","` разбирается в две пустые записи, то есть «непусто» по длине и «пусто» по
// существу.
//
// Значение неизменяемо после сборки: Declared/Unknown отдают копии.
type AddressFamilies struct {
	// declared — семейства из словаря, в порядке словаря (не в порядке ввода):
	// множество, а не список, и сравнение по нему не должно зависеть от того, как
	// оператор их перечислил.
	declared []string
	// unknown — записи, которых словарь не знает, в том виде, как их написал
	// оператор. Хранятся, чтобы отказ старта мог их НАЗВАТЬ: молча отброшенная
	// запись означала бы профиль, который оператор считает объявленным, а страж —
	// нет.
	unknown []string
}

// NewAddressFamilies собирает множество из того, что написал оператор.
//
// Пустые записи отбрасываются, пробелы по краям срезаются, регистр приводится,
// повторы схлопываются: ни одно решение по этому множеству не различает «две
// записи» и «одну, написанную дважды», а оператор, писавший через
// «запятая-пробел», обязан получить объявленный профиль, а не отказ.
func NewAddressFamilies(raw ...string) AddressFamilies {
	seen := make(map[string]struct{}, len(raw))
	var unknown []string
	seenUnknown := make(map[string]struct{}, len(raw))
	for _, s := range raw {
		norm := strings.ToLower(strings.TrimSpace(s))
		if norm == "" {
			continue
		}
		if !isKnownAddressFamily(norm) {
			if _, dup := seenUnknown[norm]; !dup {
				seenUnknown[norm] = struct{}{}
				unknown = append(unknown, strings.TrimSpace(s))
			}
			continue
		}
		seen[norm] = struct{}{}
	}
	var declared []string
	for _, f := range knownAddressFamilies {
		if _, ok := seen[f]; ok {
			declared = append(declared, f)
		}
	}
	if len(declared) == 0 && len(unknown) == 0 {
		// Канонический ноль: «не объявлено» обязано быть неотличимо от нулевого
		// значения, иначе у него появится два представления, и сравнение по одному
		// однажды разойдётся с другим.
		return AddressFamilies{}
	}
	return AddressFamilies{declared: declared, unknown: unknown}
}

func isKnownAddressFamily(norm string) bool {
	for _, f := range knownAddressFamilies {
		if f == norm {
			return true
		}
	}
	return false
}

// IsDeclared — ЕДИНСТВЕННЫЙ предикат «объявлено ли отслеживание состояния». Его
// спрашивают страж старта и самоотчёт о посадке, поэтому «страж пропустил» ⟺
// «семейства действительно объявлены» — по построению, а не по совпадению.
func (f AddressFamilies) IsDeclared() bool { return len(f.declared) > 0 }

// Declared отдаёт КОПИЮ объявленных семейств: одобренное стражем значение никто не
// должен уметь изменить после одобрения.
func (f AddressFamilies) Declared() []string {
	if len(f.declared) == 0 {
		return nil
	}
	out := make([]string, len(f.declared))
	copy(out, f.declared)
	return out
}

// Unknown отдаёт КОПИЮ записей, которых словарь не знает.
func (f AddressFamilies) Unknown() []string {
	if len(f.unknown) == 0 {
		return nil
	}
	out := make([]string, len(f.unknown))
	copy(out, f.unknown)
	return out
}

// Has — объявлено ли отслеживание состояния для семейства.
func (f AddressFamilies) Has(family string) bool {
	norm := strings.ToLower(strings.TrimSpace(family))
	for _, d := range f.declared {
		if d == norm {
			return true
		}
	}
	return false
}

// String — читаемое представление для журнала и отказа старта.
func (f AddressFamilies) String() string {
	if len(f.declared) == 0 {
		return "<not declared>"
	}
	return strings.Join(f.declared, ",")
}

// KnownAddressFamilies отдаёт КОПИЮ словаря — для текста отказа, который обязан
// сказать оператору, что вообще допустимо.
func KnownAddressFamilies() []string {
	out := make([]string, len(knownAddressFamilies))
	copy(out, knownAddressFamilies)
	return out
}

// StateTrackingFamilies — нормализованное множество из настроек. Единственный
// законный способ прочитать одноимённое поле: страж и читатель обязаны спрашивать
// одно и то же значение.
func (c Config) StateTrackingFamilies() AddressFamilies {
	return NewAddressFamilies(c.Dataplane.Executor.StateTrackingFamilies...)
}

// ReservedPrefixes — нормализованный перечень служебных диапазонов из настроек.
// Единственный законный способ прочитать одноимённое поле.
//
// Один источник значения на процесс: его читает и страж старта
// (ValidateReservedPrefixes), и проводка композиционного корня, отдающая перечень
// в use-case'ы подсети. Поэтому «страж пропустил» ⟺ «путь запроса сверяется с тем
// же перечнем» — по построению, а не по совпадению двух одинаково написанных тел.
//
// Нормализация и признание записи негодной живут в конструкторе доменного типа и
// здесь не пересказываются: два места об одном предмете разъезжаются молча. См.
// domain.NewReservedPrefixes.
func (c Config) ReservedPrefixes() domain.ReservedPrefixes {
	return domain.NewReservedPrefixes(c.Dataplane.ReservedPrefixes...)
}
