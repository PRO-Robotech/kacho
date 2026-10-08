// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package rules — правила извещений оператора, нужные обоим развёртываниям
// notify (замысел issue-2924 З1, З7, З22; приёмка NTF-5 Р4, Р5): таблица видов,
// таблица переходов с их письмами и закрываемыми этапами, правило напоминаний,
// вывод категории и закрытый перечень 18 типов ресурса арендатора.
//
// Пакет без сетевых зависимостей и без хранилища: его читают приём и переходы
// notify-api и фиксация заявки notify-sender. Второй копии этих таблиц ни в
// одном пакете нет — расходятся такие копии ровно там, где расхождение не видно.
package rules

import (
	"slices"
	"time"
)

// Kind — вид извещения (Р4). Написание — то же, что в базе (`notices.kind`).
type Kind string

const (
	KindMaintenance      Kind = "MAINTENANCE"
	KindOutage           Kind = "OUTAGE"
	KindDecommission     Kind = "DECOMMISSION"
	KindSuspension       Kind = "SUSPENSION"
	KindSecurityIncident Kind = "SECURITY_INCIDENT"
	KindTermsChange      Kind = "TERMS_CHANGE"
)

// State — состояние извещения (Р5). Написание — то же, что в базе.
type State string

const (
	StateScheduled  State = "SCHEDULED"
	StateInProgress State = "IN_PROGRESS"
	StateCompleted  State = "COMPLETED"
	StateCancelled  State = "CANCELLED"
)

// Category — категория контакта, по которой идёт адресация (Р4, Р21).
type Category string

const (
	CategorySecurity     Category = "SECURITY"
	CategoryAccountLegal Category = "ACCOUNT_LEGAL"
	CategoryOperations   Category = "OPERATIONS"
)

// Stage — этап письма (Р4, Р5). Написание — то же, что в базе
// (`notice_stage_events.stage`).
type Stage string

const (
	StageScheduled   Stage = "scheduled"
	StageReminder    Stage = "reminder"
	StageRescheduled Stage = "rescheduled"
	StageCancelled   Stage = "cancelled"
	StageCompleted   Stage = "completed"
	StageStarted     Stage = "started"
	StageResolved    Stage = "resolved"
	StageLifted      Stage = "lifted"
	StageOpened      Stage = "opened"
	StageClosed      Stage = "closed"
)

// Presence — что вид говорит о поле момента: обязательно либо запрещено.
// Третьего состояния («по желанию») у полей вида нет (Р4).
type Presence uint8

const (
	// Required — поле обязано быть задано.
	Required Presence = iota + 1
	// Forbidden — поле задавать нельзя: момент ставит сервер либо его нет вовсе.
	Forbidden
)

// KindRule — строка таблицы видов Р4.
type KindRule struct {
	// StartsAt, EndsAt — поля момента на входе Create.
	StartsAt, EndsAt Presence
	// Initial — начальное состояние извещения.
	Initial State
	// Category — категория контакта (выходное поле `category`).
	Category Category
	// Affected — допустимы ли затронутые ресурсы.
	Affected bool
	// AllAccounts — допустима ли аудитория «все аккаунты».
	AllAccounts bool
	// CreateStage — этап письма при создании.
	CreateStage Stage
}

// kinds — таблица видов Р4. Порядок ключей не значим.
var kinds = map[Kind]KindRule{
	KindMaintenance: {StartsAt: Required, EndsAt: Required, Initial: StateScheduled,
		Category: CategoryOperations, Affected: true, AllAccounts: true, CreateStage: StageScheduled},
	KindOutage: {StartsAt: Forbidden, EndsAt: Forbidden, Initial: StateInProgress,
		Category: CategoryOperations, Affected: true, AllAccounts: true, CreateStage: StageStarted},
	KindDecommission: {StartsAt: Required, EndsAt: Forbidden, Initial: StateScheduled,
		Category: CategoryOperations, Affected: true, AllAccounts: true, CreateStage: StageScheduled},
	KindSuspension: {StartsAt: Forbidden, EndsAt: Forbidden, Initial: StateInProgress,
		Category: CategoryAccountLegal, Affected: false, AllAccounts: false, CreateStage: StageStarted},
	KindSecurityIncident: {StartsAt: Forbidden, EndsAt: Forbidden, Initial: StateInProgress,
		Category: CategorySecurity, Affected: false, AllAccounts: true, CreateStage: StageOpened},
	KindTermsChange: {StartsAt: Required, EndsAt: Forbidden, Initial: StateScheduled,
		Category: CategoryAccountLegal, Affected: false, AllAccounts: true, CreateStage: StageScheduled},
}

// RuleOf — строка таблицы видов; false — вид вне перечня.
func RuleOf(k Kind) (KindRule, bool) {
	r, ok := kinds[k]
	return r, ok
}

// Kinds — виды перечня в порядке объявления контракта.
func Kinds() []Kind {
	return []Kind{KindMaintenance, KindOutage, KindDecommission, KindSuspension,
		KindSecurityIncident, KindTermsChange}
}

// Verb — глагол перехода (Р5). Написание — то, которым его называет текст
// отказа `does not support <Verb>`.
type Verb string

const (
	VerbStart    Verb = "Start"
	VerbComplete Verb = "Complete"
	VerbCancel   Verb = "Cancel"
	VerbUpdate   Verb = "Update"
)

// scheduledKinds — виды, рождённые в SCHEDULED: им доступны Start, Cancel,
// Update (Р5).
var scheduledKinds = []Kind{KindMaintenance, KindDecommission, KindTermsChange}

// Transition — строка таблицы переходов Р5.
type Transition struct {
	Verb Verb
	// From — состояние, из которого переход допустим; To — в которое ведёт.
	From, To State
	// Kinds — виды, которым глагол доступен.
	Kinds []Kind
}

// Supports — доступен ли глагол виду.
func (t Transition) Supports(k Kind) bool { return slices.Contains(t.Kinds, k) }

// KindNames — написания видов глагола: параметр условия записи `kind = ANY($n)`.
func (t Transition) KindNames() []string {
	out := make([]string, len(t.Kinds))
	for i, k := range t.Kinds {
		out[i] = string(k)
	}
	return out
}

var transitions = map[Verb]Transition{
	VerbStart:    {Verb: VerbStart, From: StateScheduled, To: StateInProgress, Kinds: scheduledKinds},
	VerbComplete: {Verb: VerbComplete, From: StateInProgress, To: StateCompleted, Kinds: Kinds()},
	VerbCancel:   {Verb: VerbCancel, From: StateScheduled, To: StateCancelled, Kinds: scheduledKinds},
	VerbUpdate:   {Verb: VerbUpdate, From: StateScheduled, To: StateScheduled, Kinds: scheduledKinds},
}

// TransitionOf — строка таблицы переходов глагола. Глагол вне перечня —
// ошибка программы, а не входа: глаголы называет код, а не запрос.
func TransitionOf(v Verb) Transition {
	t, ok := transitions[v]
	if !ok {
		panic("rules: глагол вне таблицы переходов: " + string(v))
	}
	return t
}

// Letter — этап письма перехода для вида; false — у перехода письма нет (Р5).
func Letter(v Verb, k Kind) (Stage, bool) {
	switch v {
	case VerbStart:
		return "", false
	case VerbCancel:
		return StageCancelled, true
	case VerbUpdate:
		return StageRescheduled, true
	case VerbComplete:
		switch k {
		case KindMaintenance, KindDecommission:
			return StageCompleted, true
		case KindOutage:
			return StageResolved, true
		case KindSuspension:
			return StageLifted, true
		case KindSecurityIncident:
			return StageClosed, true
		case KindTermsChange:
			return "", false
		}
	}
	return "", false
}

// Superseded — этапы неотправленных строк, которые переход делает
// неактуальными (таблица Р5 «какие неотправленные строки переход делает
// неактуальными»). У Update закрываются только строки ревизий не новее
// прежней — это условие ставит запись, а не таблица.
func Superseded(v Verb, k Kind) []Stage {
	switch v {
	case VerbStart:
		return []Stage{StageReminder}
	case VerbCancel, VerbUpdate:
		return []Stage{StageScheduled, StageRescheduled, StageReminder}
	case VerbComplete:
		if _, letter := Letter(v, k); !letter {
			return nil
		}
		return []Stage{StageScheduled, StageRescheduled, StageReminder, StageStarted, StageOpened}
	}
	return nil
}

// StageNames — написания этапов: параметр условия записи `stage = ANY($n)`.
func StageNames(stages []Stage) []string {
	out := make([]string, len(stages))
	for i, s := range stages {
		out[i] = string(s)
	}
	return out
}

// Reminders — моменты напоминаний извещения (Р5, З7 п.1): MAINTENANCE — одно в
// startsAt − lead; DECOMMISSION — startsAt − 30 сут, − 7 сут, − 1 сут; прочие
// виды напоминаний не имеют. Момент, не лежащий строго после now, не
// назначается. Порядок — по возрастанию момента.
//
// Единственная реализация правила: её зовут фиксация Create (notify-sender) и
// Update (notify-api).
func Reminders(k Kind, startsAt, now time.Time, lead time.Duration) []time.Time {
	var offsets []time.Duration
	switch k {
	case KindMaintenance:
		offsets = []time.Duration{lead}
	case KindDecommission:
		offsets = []time.Duration{30 * 24 * time.Hour, 7 * 24 * time.Hour, 24 * time.Hour}
	case KindOutage, KindSuspension, KindSecurityIncident, KindTermsChange:
		return nil
	}
	var out []time.Time
	for _, off := range offsets {
		if at := startsAt.Add(-off); at.After(now) {
			out = append(out, at)
		}
	}
	slices.SortFunc(out, func(a, b time.Time) int { return a.Compare(b) })
	return out
}

// tenantResourceTypes — закрытый перечень 18 типов ресурса арендатора (Р4, З22).
// Константа контракта: новый тип дописывается правкой контракта, префиксом
// идентификатора он не выводится.
var tenantResourceTypes = []string{
	"vpc_network", "vpc_subnet", "vpc_security_group", "vpc_route_table",
	"vpc_address", "vpc_gateway", "vpc_network_interface", "vpc_cidr_group",
	"compute_instance", "compute_placement_group",
	"nlb_network_load_balancer", "nlb_target_group", "nlb_listener",
	"registry_registry", "registry_repository",
	"storage_volume", "storage_snapshot", "storage_image",
}

// TenantResourceTypes — перечень типов ресурса арендатора; срез-копия.
func TenantResourceTypes() []string { return slices.Clone(tenantResourceTypes) }

// IsTenantResourceType — входит ли тип в перечень.
func IsTenantResourceType(t string) bool { return slices.Contains(tenantResourceTypes, t) }

// Limits входа Create (Р4, Р10).
const (
	// MaxAudienceScopes — наибольшее число областей одной формы аудитории.
	MaxAudienceScopes = 100
	// MaxAffectedResources — наибольшее число затронутых ресурсов.
	MaxAffectedResources = 50
)
