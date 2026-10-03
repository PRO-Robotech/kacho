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
	presentedSourceAuthority = "authority"
	presentedSourceRecord    = "record"

	// Исходы — по веткам revocationCheck / platformRevocationCheck.
	presentedOutcomeLive          = "live"          // источник ответил: жив
	presentedOutcomeRevoked       = "revoked"       // источник ответил: отозван — единый отказ 401
	presentedOutcomeUnanswered    = "unanswered"    // источник не ответил — ответ Р1 (503)
	presentedOutcomeMisconfigured = "misconfigured" // ответил не источник отзыва — ответ Р1 (503)
	presentedOutcomeNoIdentifier  = "no_identifier" // спросить нечем: у токена нет jti — ответ Р1 (503)
	presentedOutcomeNotWired      = "not_wired"     // читатель не провязан: авторитет — Р1; запись — не спрашивали
)

// bearerLaneStates — закрытый перечень состояний в порядке объявления.
var bearerLaneStates = []BearerLaneState{
	{presentedSourceAuthority, presentedOutcomeLive},
	{presentedSourceAuthority, presentedOutcomeRevoked},
	{presentedSourceAuthority, presentedOutcomeUnanswered},
	{presentedSourceAuthority, presentedOutcomeMisconfigured},
	{presentedSourceAuthority, presentedOutcomeNoIdentifier},
	{presentedSourceAuthority, presentedOutcomeNotWired},
	{presentedSourceRecord, presentedOutcomeLive},
	{presentedSourceRecord, presentedOutcomeRevoked},
	{presentedSourceRecord, presentedOutcomeUnanswered},
	{presentedSourceRecord, presentedOutcomeMisconfigured},
	{presentedSourceRecord, presentedOutcomeNoIdentifier},
	{presentedSourceRecord, presentedOutcomeNotWired},
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

// record — одно событие состояния. Пары, которые пишет код полосы, сверяются с
// перечнем в обе стороны пробой TestBearerLaneRecordsOnlyDeclaredStates.
func (c *BearerLaneCounts) record(source, outcome string) {
	if c == nil {
		return
	}
	if i := bearerStateIndex(source, outcome); i >= 0 {
		c.cells[i].Add(1)
	}
}

// BearerSourceCells — клетки одного источника вопроса об отзыве.
type BearerSourceCells struct {
	Live, Revoked, Unanswered, Misconfigured, NoIdentifier, NotWired uint64
}

// BearerLaneSnapshot — прочитанные клетки полосы предъявителя по источникам.
type BearerLaneSnapshot struct {
	Authority BearerSourceCells
	Record    BearerSourceCells
}

// cell — клетка слепка для состояния; nil — состояние вне перечня.
func (s *BearerLaneSnapshot) cell(st BearerLaneState) *uint64 {
	var src *BearerSourceCells
	switch st.Source {
	case presentedSourceAuthority:
		src = &s.Authority
	case presentedSourceRecord:
		src = &s.Record
	default:
		return nil
	}
	switch st.Outcome {
	case presentedOutcomeLive:
		return &src.Live
	case presentedOutcomeRevoked:
		return &src.Revoked
	case presentedOutcomeUnanswered:
		return &src.Unanswered
	case presentedOutcomeMisconfigured:
		return &src.Misconfigured
	case presentedOutcomeNoIdentifier:
		return &src.NoIdentifier
	case presentedOutcomeNotWired:
		return &src.NotWired
	}
	return nil
}

// Snapshot — слепок клеток для коллектора диагностической поверхности.
func (c *BearerLaneCounts) Snapshot() BearerLaneSnapshot {
	var s BearerLaneSnapshot
	if c == nil {
		return s
	}
	for i, st := range bearerLaneStates {
		if p := s.cell(st); p != nil {
			*p = c.cells[i].Load()
		}
	}
	return s
}

// Value — величина клетки состояния; неизвестное состояние — ноль.
func (s BearerLaneSnapshot) Value(st BearerLaneState) uint64 {
	if p := s.cell(st); p != nil {
		return *p
	}
	return 0
}

// Set задаёт величину клетки — для проб прибора, собирающих слепок вручную.
func (s *BearerLaneSnapshot) Set(st BearerLaneState, v uint64) {
	if p := s.cell(st); p != nil {
		*p = v
	}
}

// BearerLane — накопитель клеток полосы предъявителя; композиционный корень
// отдаёт его слепок коллектору диагностической поверхности.
func (a *AuthInterceptor) BearerLane() *BearerLaneCounts {
	return a.bearerLane
}
