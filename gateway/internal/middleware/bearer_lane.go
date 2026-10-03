// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware

// bearer_lane.go — клетки полосы предъявителя на приборе (kacho#2740).
//
// Полоса предъявителя спрашивает об отзыве двух наших источников — авторитет
// нашей чеканки и запись отзыва — и на каждом исходе поступает по-своему
// (auth_revocation.go). Прежде исход был виден только строкой журнала раз в
// окно, и строка называла ПЕРВОЕ событие окна, а не продолжительность
// состояния: «мигнуло однажды» и «держится час» читались одинаково. У соседней
// полосы (наша сессия, SessionLaneCounts) клетки на приборе были; у этой — ни
// одной.
//
// Клетка — на КАЖДОЕ состояние, и перечень состояний выводится из кода: те же
// исходы, что ветки revocationCheck и platformRevocationCheck. Каждая клетка
// существует с нулём до первого события, чтобы ноль отличался от «полосы нет».

import "sync/atomic"

// BearerLaneState — одно состояние полосы: источник вопроса и исход.
type BearerLaneState struct {
	// Source — `authority` (авторитет нашей чеканки) либо `record` (запись
	// отзыва для токена другой записи приёма).
	Source string
	// Outcome — исход вопроса.
	Outcome string
}

const (
	bearerSourceAuthority = "authority"
	bearerSourceRecord    = "record"

	// Исходы — по веткам revocationCheck / platformRevocationCheck.
	bearerOutcomeLive          = "live"          // источник ответил: жив
	bearerOutcomeRevoked       = "revoked"       // источник ответил: отозван — единый отказ 401
	bearerOutcomeUnanswered    = "unanswered"    // источник не ответил — ответ Р1 (503)
	bearerOutcomeMisconfigured = "misconfigured" // ответил не источник отзыва — ответ Р1 (503)
	bearerOutcomeNoIdentifier  = "no_identifier" // спросить нечем: у токена нет jti — ответ Р1 (503)
	bearerOutcomeNotWired      = "not_wired"     // читатель не провязан: авторитет — Р1; запись — не спрашивали
)

// bearerLaneStates — закрытый перечень состояний в порядке объявления.
var bearerLaneStates = []BearerLaneState{
	{bearerSourceAuthority, bearerOutcomeLive},
	{bearerSourceAuthority, bearerOutcomeRevoked},
	{bearerSourceAuthority, bearerOutcomeUnanswered},
	{bearerSourceAuthority, bearerOutcomeMisconfigured},
	{bearerSourceAuthority, bearerOutcomeNoIdentifier},
	{bearerSourceAuthority, bearerOutcomeNotWired},
	{bearerSourceRecord, bearerOutcomeLive},
	{bearerSourceRecord, bearerOutcomeRevoked},
	{bearerSourceRecord, bearerOutcomeUnanswered},
	{bearerSourceRecord, bearerOutcomeMisconfigured},
	{bearerSourceRecord, bearerOutcomeNoIdentifier},
	{bearerSourceRecord, bearerOutcomeNotWired},
}

// BearerLaneStates — перечень состояний полосы: словарь меток прибора.
func BearerLaneStates() []BearerLaneState {
	return append([]BearerLaneState(nil), bearerLaneStates...)
}

// BearerLaneCounts — накопитель клеток полосы предъявителя.
type BearerLaneCounts struct {
	cells [12]atomic.Uint64
}

func bearerStateIndex(source, outcome string) int {
	for i, s := range bearerLaneStates {
		if s.Source == source && s.Outcome == outcome {
			return i
		}
	}
	return -1
}

// record — одно событие состояния. Неизвестное состояние — ошибка сборки
// полосы, и она не теряется: паники на пути запроса нет, но и клетки нет —
// это держит TestBearerLaneRecordsOnlyDeclaredStates.
func (c *BearerLaneCounts) record(source, outcome string) {
	if c == nil {
		return
	}
	if i := bearerStateIndex(source, outcome); i >= 0 {
		c.cells[i].Add(1)
	}
}

// BearerLaneSnapshot — прочитанные клетки полосы предъявителя.
type BearerLaneSnapshot struct {
	cells [12]uint64
}

// Snapshot — слепок клеток для коллектора диагностической поверхности.
func (c *BearerLaneCounts) Snapshot() BearerLaneSnapshot {
	var s BearerLaneSnapshot
	if c == nil {
		return s
	}
	for i := range c.cells {
		s.cells[i] = c.cells[i].Load()
	}
	return s
}

// Value — величина клетки состояния; неизвестное состояние — ноль.
func (s BearerLaneSnapshot) Value(st BearerLaneState) uint64 {
	if i := bearerStateIndex(st.Source, st.Outcome); i >= 0 {
		return s.cells[i]
	}
	return 0
}

// Set задаёт величину клетки — для проб прибора, собирающих слепок вручную.
func (s *BearerLaneSnapshot) Set(st BearerLaneState, v uint64) {
	if i := bearerStateIndex(st.Source, st.Outcome); i >= 0 {
		s.cells[i] = v
	}
}

// BearerLane — накопитель клеток полосы предъявителя; композиционный корень
// отдаёт его слепок коллектору диагностической поверхности.
func (a *AuthInterceptor) BearerLane() *BearerLaneCounts {
	return a.bearerLane
}
