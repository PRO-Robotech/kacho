// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package limits — сетка на адресата, суточный потолок потока, ограда ключа
// сетки, ведро и пауза источника шлюза notify (kacho#2915, замысел NTF-1 З24,
// §6, §8; приёмка NTF1-H01…H10).
//
// # Что где живёт
//
//   - Сетка и потолок — CAS-строки базы kacho_notify ([Limiter]): резерв —
//     одна транзакция READ COMMITTED (ограда, окна сетки, потолок) до
//     `MAIL FROM`; освобождение — на любом исходе строки, кроме `SENT`, по
//     ключам резерва ([Reservation]).
//   - Ограда ключа сетки — строка-одиночка `recipient_key_fence`: её пишет
//     старт реплики ([Limiter.WriteFence]) под табличным замком, читает
//     `FOR SHARE` каждый резерв. Резерв под заменённым ключом —
//     [ErrRecipientKeySuperseded], а не исход сетки.
//   - Ведро и пауза источника — память реплики ([SourceGate]).
//
// Адреса открытым текстом пакет не хранит и не печатает: ключ строки сетки —
// HMAC-SHA256(ключ сетки, нормализованный адрес), метки метрик — класс и
// модуль источника.
package limits

import (
	"fmt"
	"strconv"
	"strings"
)

// Границы ручек сетки и потолка (§8 замысла). Единственное место чисел: страж
// конфигурации называет их в тексте отказа, [Grid.Validate] судит по ним.
const (
	SecurityPerDayMin = 1
	SecurityPerDayMax = 1000
	NoticePerHourMin  = 1
	NoticePerHourMax  = 10000
	NoticePerDayMin   = 1
	NoticePerDayMax   = 10000
	GlobalPerDayMin   = 1
	GlobalPerDayMax   = 10000000
)

// Имена ручек сетки и потолка — имена values и текста отказа (§8).
const (
	KnobSecurityPerDay = "notify.limits.recipient.security.perDay"
	KnobNoticePerHour  = "notify.limits.recipient.notice.perHour"
	KnobNoticePerDay   = "notify.limits.recipient.notice.perDay"
	KnobGlobalPerDay   = "notify.limits.global.perDay"
)

// Grid — сетка на адресата и суточный потолок потока установки.
type Grid struct {
	// SecurityPerDay — писем класса security на адресата за сутки UTC.
	SecurityPerDay int
	// NoticePerHour — писем класса notice на адресата за час.
	NoticePerHour int
	// NoticePerDay — писем класса notice на адресата за сутки UTC.
	NoticePerDay int
	// GlobalPerDay — суточный потолок потока установки. Достигнутый, он
	// останавливает notice; security идёт выше него (NTF1-H05).
	GlobalPerDay int
}

// Bound — граница ручки и её имя.
type Bound struct {
	Knob     string
	Min, Max int
}

// String — граница в форме текста отказа: `[1..1000]`.
func (b Bound) String() string {
	return "[" + strconv.Itoa(b.Min) + ".." + strconv.Itoa(b.Max) + "]"
}

// Contains — значение в границе (обе границы включены).
func (b Bound) Contains(v int) bool { return v >= b.Min && v <= b.Max }

// GridBounds — границы ручек сетки в порядке полей [Grid].
func GridBounds() []Bound {
	return []Bound{
		{KnobSecurityPerDay, SecurityPerDayMin, SecurityPerDayMax},
		{KnobNoticePerHour, NoticePerHourMin, NoticePerHourMax},
		{KnobNoticePerDay, NoticePerDayMin, NoticePerDayMax},
		{KnobGlobalPerDay, GlobalPerDayMin, GlobalPerDayMax},
	}
}

func (g Grid) values() []int {
	return []int{g.SecurityPerDay, g.NoticePerHour, g.NoticePerDay, g.GlobalPerDay}
}

// Validate судит каждую ручку сетки по её границе; находки — все разом.
func (g Grid) Validate() error {
	var bad []string
	for i, b := range GridBounds() {
		if v := g.values()[i]; !b.Contains(v) {
			bad = append(bad, fmt.Sprintf("%s = %d вне границы %s", b.Knob, v, b))
		}
	}
	if len(bad) > 0 {
		return fmt.Errorf("сетка лимитов: %s", strings.Join(bad, "; "))
	}
	return nil
}

// SecurityNetCovers — инвариант NTF1-H07: сетка security на адресата не меньше
// 1,25 × суммы суточных limits шаблонов security сборки. Иначе законный поток
// security одного адресата упирался бы в сетку раньше, чем в лимиты шаблонов
// источника, и письма безопасности терялись бы на сетке. Сравнение — в целых:
// 4 × perDay ≥ 5 × sum.
func SecurityNetCovers(perDay, bundleSecuritySum int) error {
	if bundleSecuritySum < 0 {
		return fmt.Errorf("%s: сумма суточных limits шаблонов security %d отрицательна", KnobSecurityPerDay, bundleSecuritySum)
	}
	if 4*perDay >= 5*bundleSecuritySum {
		return nil
	}
	need := strings.Replace(strconv.FormatFloat(1.25*float64(bundleSecuritySum), 'f', -1, 64), ".", ",", 1)
	return fmt.Errorf("%s: сетка security %d меньше 1,25 × %d (= %s) — сумма суточных limits шаблонов "+
		"security сборки не помещается в сетку на адресата (NTF1-H07)", KnobSecurityPerDay, perDay, bundleSecuritySum, need)
}
