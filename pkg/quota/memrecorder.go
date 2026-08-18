// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package quota

import "sync"

// MemRecorder — сток наблюдаемости в памяти.
//
// Живёт в прод-пакете, а не в файле проб, НАМЕРЕННО: его берут пробы владельцев
// (`services/*/...`), а из чужого пакета файл `_test.go` не виден. Ровно так же
// устроен дублёр стока у доставки исходящих.
//
// Дублёр НЕ снисходительнее настоящего: он ничего не глотает и ничего не
// нормализует — иначе он маскировал бы дефект, ради которого его подставляют.
type MemRecorder struct {
	mu sync.Mutex

	Pulls       map[string]int
	Failures    map[string]int
	AppliedRows map[string]float64
	LastSuccess map[string]float64
}

// NewMemRecorder собирает пустой сток.
func NewMemRecorder() *MemRecorder {
	return &MemRecorder{
		Pulls:       map[string]int{},
		Failures:    map[string]int{},
		AppliedRows: map[string]float64{},
		LastSuccess: map[string]float64{},
	}
}

func (m *MemRecorder) IncPulls(schema string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Pulls[schema]++
}

func (m *MemRecorder) IncPullFailures(schema string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Failures[schema]++
}

func (m *MemRecorder) AddAppliedRows(schema string, rows float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.AppliedRows[schema] += rows
}

func (m *MemRecorder) SetLastSuccessUnix(schema string, unix float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.LastSuccess[schema] = unix
}

// Snapshot отдаёт слепок под замком — пробы читают его, не гоняясь с тянущим.
func (m *MemRecorder) Snapshot(schema string) (pulls, failures int, rows, lastSuccess float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.Pulls[schema], m.Failures[schema], m.AppliedRows[schema], m.LastSuccess[schema]
}
