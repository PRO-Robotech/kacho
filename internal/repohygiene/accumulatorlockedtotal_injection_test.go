// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"
)

// accumulatorlockedtotal_injection_test.go — вторая разновидность накопителя:
// итог ПОД ЗАМКОМ (kacho#2740, критерии 3–4).
//
// # Чего перепись не видела
//
// Первая разновидность — атомарное поле плюс экспортируемый слепок. Докладчик
// окна журнала края (`introspectionFailureReporter`) держит итог иначе: целое
// поле под `sync.Mutex`, отданное наружу неэкспортируемым методом прямо в
// строку журнала. Ни одного признака первой разновидности у него нет, и
// «ноль находок» переписи означал здесь «целый вид не прочитан».
//
// # Что доказывается, и по какой оси каждое
//
//	НАХОДКА          итог под замком без клетки назван координатой и методом
//	МОЛЧАНИЕ         тот же итог с экспортируемым слепком, читаемым снаружи
//	ТОЛЬКО СВОЙ      итог, отдаваемый лишь неэкспортируемым методом, — находка,
//	                 даже если этот метод зовут в своём пакете
//	СБРОС            величина, которую обнуляют, — состояние, а не итог
//	БЕЗ ЗАМКА        счёт без замка — не накопитель горячего пути
//	НЕ ОТДАН         итог, не выходящий ни одним результатом, — номер, а не величина
//	ВНЕ ПРОЦЕССА     дублёр для проб, не достижимый от main, не судится;
//	                 тот же носитель, достижимый от main, — судится
//	ФОРМЫ            встроенный замок, RWMutex, `+=`, отдача преобразованием и
//	                 полем структуры — те же законные формы одного предмета
//
// Каждая ось — своя проба: одна «на всё» зеленела бы на сломанных осях.

// windowReporterSrc — докладчик окна ровно той формы, что у края: итог под
// замком, отданный только неэкспортируемым методом в строку журнала.
const windowReporterSrc = `package rep

import "sync"

type windowReporter struct {
	mu         sync.Mutex
	total      int64
	suppressed int64
}

func (r *windowReporter) observe() (report bool, total, represents int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.total++
	r.suppressed++
	represents = r.suppressed
	r.suppressed = 0
	return true, r.total, represents
}
`

// inProcess дописывает в корпус `package main`, импортирующий названные
// каталоги: судится только код, который поднимается процессом.
func inProcess(files map[string]string, dirs ...string) map[string]string {
	src := "package main\n\nimport (\n"
	for _, d := range dirs {
		src += "\t_ \"" + modulePathPrefix + d + "\"\n"
	}
	src += ")\n\nfunc main() {}\n"
	files["gateway/cmd/proc/main.go"] = src
	return files
}

// TestLockedTotalGateFindsAWindowReporterWithoutACell — НАХОДКА: докладчик окна
// без клетки краснит перепись и назван координатой.
func TestLockedTotalGateFindsAWindowReporterWithoutACell(t *testing.T) {
	t.Parallel()
	accs, findings, _ := judge(t, inProcess(map[string]string{"gateway/internal/rep/rep.go": windowReporterSrc},
		"gateway/internal/rep"), nil)
	if len(accs) != 1 {
		t.Fatalf("накопителей распознано %d, ожидался 1 — итог под замком не виден переписи: %+v",
			len(accs), accs)
	}
	if len(findings) != 1 {
		t.Fatalf("находок %d, ожидалась 1: %v", len(findings), findings)
	}
	if !strings.Contains(findings[0], "gateway/internal/rep/rep.go:5") {
		t.Errorf("находка не называет КООРДИНАТУ объявления типа: %q", findings[0])
	}
	if !strings.Contains(findings[0], "windowReporter") || !strings.Contains(findings[0], "observe") {
		t.Errorf("находка не называет носитель и метод, отдающий итог: %q", findings[0])
	}
	// Сброшенное поле (`suppressed`) — состояние окна, а не итог: находка
	// говорит об итоге.
	if strings.Contains(findings[0], "suppressed") {
		t.Errorf("сбрасываемое поле названо итогом: %q", findings[0])
	}
}

// TestLockedTotalGateIsSilentWithACell — МОЛЧАНИЕ: тот же докладчик с
// экспортируемым слепком, который читает пакет диагностической поверхности.
func TestLockedTotalGateIsSilentWithACell(t *testing.T) {
	t.Parallel()
	accs, findings, _ := judge(t, inProcess(map[string]string{
		"gateway/internal/rep/rep.go": windowReporterSrc + `
func (r *windowReporter) Total() uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return uint64(r.total)
}

type Window interface{ Total() uint64 }

func Windows() []Window { return []Window{&windowReporter{}} }
`,
		"gateway/internal/surface/surface.go": `package surface

import "` + modulePathPrefix + `gateway/internal/rep"

func Collect() (sum uint64) {
	for _, w := range rep.Windows() {
		sum += w.Total()
	}
	return sum
}
`,
	}, "gateway/internal/rep", "gateway/internal/surface"), nil)
	if len(accs) != 1 {
		t.Fatalf("накопителей распознано %d, ожидался 1: %+v", len(accs), accs)
	}
	if len(findings) != 0 {
		t.Fatalf("перепись красна на докладчике С КЛЕТКОЙ — такой гейт снимут как ложный: %v", findings)
	}
}

// TestLockedTotalGateDoesNotCountTheOwnLogLine — чтение итога в СВОЁМ пакете
// (строка журнала) клеткой не является.
func TestLockedTotalGateDoesNotCountTheOwnLogLine(t *testing.T) {
	t.Parallel()
	_, findings, _ := judge(t, inProcess(map[string]string{
		"gateway/internal/rep/rep.go": windowReporterSrc,
		"gateway/internal/rep/use.go": `package rep

import "log"

func fail(r *windowReporter) {
	if report, total, n := r.observe(); report {
		log.Printf("check unanswered: total=%d represents=%d", total, n)
	}
}
`,
	}, "gateway/internal/rep"), nil)
	if len(findings) != 1 {
		t.Fatalf("строка журнала своего пакета засчитана клеткой: находок %d, ожидалась 1", len(findings))
	}
}

// TestLockedTotalGateIgnoresAResetCounter — СБРОС: счёт, который обнуляют,
// говорит «сколько подряд сейчас», а не «сколько с запуска».
func TestLockedTotalGateIgnoresAResetCounter(t *testing.T) {
	t.Parallel()
	accs, _, _ := judge(t, inProcess(map[string]string{
		"gateway/internal/brk/brk.go": `package brk

import "sync"

type breaker struct {
	mu          sync.Mutex
	consecutive int
}

func (b *breaker) unanswered() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.consecutive++
	return b.consecutive
}

func (b *breaker) answered() {
	b.mu.Lock()
	b.consecutive = 0
	b.mu.Unlock()
}
`,
	}, "gateway/internal/brk"), nil)
	if len(accs) != 0 {
		t.Fatalf("сбрасываемый счёт принят за итог: %+v", accs)
	}
}

// TestLockedTotalGateIgnoresAnUnguardedCounter — БЕЗ ЗАМКА: позиция разборщика,
// номер строки — счёт одного владельца, а не накопитель горячего пути.
func TestLockedTotalGateIgnoresAnUnguardedCounter(t *testing.T) {
	t.Parallel()
	accs, _, _ := judge(t, inProcess(map[string]string{
		"gateway/internal/scan/scan.go": `package scan

type scanner struct{ line int }

func (s *scanner) next() int { s.line++; return s.line }
`,
	}, "gateway/internal/scan"), nil)
	if len(accs) != 0 {
		t.Fatalf("счёт без замка принят за накопитель: %+v", accs)
	}
}

// TestLockedTotalGateIgnoresASequenceNumber — НЕ ОТДАН: номер, выдаваемый под
// замком и уходящий ключом, величиной не является.
func TestLockedTotalGateIgnoresASequenceNumber(t *testing.T) {
	t.Parallel()
	accs, _, _ := judge(t, inProcess(map[string]string{
		"gateway/internal/reg/reg.go": `package reg

import "sync"

type registry struct {
	mu   sync.Mutex
	next uint64
	all  map[uint64]string
}

func (r *registry) add(v string) func() {
	r.mu.Lock()
	r.next++
	id := r.next
	r.all[id] = v
	r.mu.Unlock()
	return func() { r.mu.Lock(); delete(r.all, id); r.mu.Unlock() }
}
`,
	}, "gateway/internal/reg"), nil)
	if len(accs) != 0 {
		t.Fatalf("номер последовательности принят за итог: %+v", accs)
	}
}

// TestLockedTotalGateKnowsEveryLawfulForm — ФОРМЫ: встроенный замок, RWMutex,
// `+=`, отдача преобразованием и полем структуры. Каждая — отдельный носитель
// без клетки, и каждый обязан стать находкой.
func TestLockedTotalGateKnowsEveryLawfulForm(t *testing.T) {
	t.Parallel()
	accs, findings, _ := judge(t, inProcess(map[string]string{
		"gateway/internal/forms/forms.go": `package forms

import "sync"

type Embedded struct {
	sync.Mutex
	n int64
}

func (e *Embedded) Add() { e.Lock(); e.n++; e.Unlock() }

func (e *Embedded) N() int64 { e.Lock(); defer e.Unlock(); return e.n }

type ReadWrite struct {
	mu    sync.RWMutex
	bytes uint64
}

func (w *ReadWrite) Write(n uint64) { w.mu.Lock(); w.bytes += n; w.mu.Unlock() }

func (w *ReadWrite) Bytes() float64 { w.mu.RLock(); defer w.mu.RUnlock(); return float64(w.bytes) }

type Stats struct{ Hits int }

type Cache struct {
	mu   sync.Mutex
	hits int
}

func (c *Cache) Hit() { c.mu.Lock(); c.hits++; c.mu.Unlock() }

func (c *Cache) Stats() Stats { c.mu.Lock(); defer c.mu.Unlock(); return Stats{Hits: c.hits} }
`,
	}, "gateway/internal/forms"), nil)
	got := map[string]bool{}
	for _, a := range accs {
		got[a.typ] = true
	}
	for _, want := range []string{"Embedded", "ReadWrite", "Cache"} {
		if !got[want] {
			t.Errorf("законная форма %s не распознана переписью: %+v", want, accs)
		}
	}
	if len(findings) != 3 {
		t.Errorf("находок %d, ожидались 3 (ни у одного носителя нет клетки): %v", len(findings), findings)
	}
}

// TestLockedTotalGateIgnoresATestDoubleOutsideTheProcess — ВНЕ ПРОЦЕССА: дублёр
// порта держит счёт вызовов под замком законно — его читает проба, а в процесс
// он не попадает. Близнец — тот же исходник, достижимый от main, — находка.
func TestLockedTotalGateIgnoresATestDoubleOutsideTheProcess(t *testing.T) {
	t.Parallel()
	double := `package portmock

import "sync"

type Registry struct {
	mu    sync.Mutex
	calls int
}

func (r *Registry) Register() { r.mu.Lock(); r.calls++; r.mu.Unlock() }

func (r *Registry) Calls() int { r.mu.Lock(); defer r.mu.Unlock(); return r.calls }
`
	accs, findings, _ := judge(t, inProcess(map[string]string{
		"services/x/internal/ports/portmock/portmock.go": double,
		"services/x/internal/app/app.go":                 "package app\n",
	}, "services/x/internal/app"), nil)
	if len(accs) != 0 || len(findings) != 0 {
		t.Fatalf("дублёр вне процесса судится как код процесса: накопители %+v, находки %v", accs, findings)
	}

	accs, findings, _ = judge(t, inProcess(map[string]string{
		"services/x/internal/ports/portmock/portmock.go": double,
	}, "services/x/internal/ports/portmock"), nil)
	if len(accs) != 1 || len(findings) != 1 {
		t.Fatalf("тот же носитель, достижимый от main, не судится: накопители %+v, находки %v", accs, findings)
	}
}
