// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"go/parser"
	"go/token"
	"path"
	"strconv"
	"strings"
	"testing"
)

// accumulatorreader_injection_test.go — доказательство, что обход накопителей
// СПОСОБЕН упасть и СПОСОБЕН смолчать.
//
// # Почему синтетический корпус, а не настоящее дерево
//
// Показать оба исхода на настоящем дереве нельзя, не сломав его: находка
// требует внести дефект, молчание — внести законный близнец, и оба пришлось бы
// коммитить. Поэтому суждение отделено от обхода
// ([accumulatorVerdict] — чистая функция), и сюда подаётся корпус, собранный
// прямо здесь.
//
// # Что доказывается, и по какой оси каждое
//
//	НАХОДКА          накопитель без читателя назван КООРДИНАТОЙ, а не числом
//	МОЛЧАНИЕ         тот же накопитель с читателем в импортирующем пакете
//	СВОЙ ПАКЕТ       чтение у себя же читателем НЕ считается
//	БЕЗ ИМПОРТА      совпадение имени без импорта объявителя — не читатель
//	ЗНАЧЕНИЕ МЕТОДА  `x.Counts` без скобок — законная форма читателя
//	ЗАЩЁЛКА          `atomic.Bool` → голый `bool` накопителем НЕ является
//	СТРУКТУРА        снимок вокруг одной защёлки накопителем ЯВЛЯЕТСЯ
//	НЕ СЛЕПОК        метод с параметром слепком не считается
//	ОБОБЩЁННЫЙ       `*Cache[K, V]` — тот же носитель, что `*Cache`
//	САМОИСТЕЧЕНИЕ    запись, которой нечего прощать, — находка (обе формы)
//
// Оси разведены намеренно: одна проба «на всё» зеленела бы на трёх сломанных
// осях из десяти, и понять, какая именно держится, было бы нечем.

// synthFacts собирает корпус из пар «путь → исходник» тем же разбором, каким
// гейт читает дерево. Второй реализации разбора здесь нет by construction:
// разошедшись с первой, она доказывала бы свойство не того кода.
func synthFacts(t *testing.T, files map[string]string) []goFileFacts {
	t.Helper()
	fset := token.NewFileSet()
	var out []goFileFacts
	for rel, src := range files {
		file, err := parser.ParseFile(fset, rel, src, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("разбор синтетики %s: %v", rel, err)
		}
		imports := map[string]bool{}
		for _, im := range file.Imports {
			p, uerr := strconv.Unquote(im.Path.Value)
			rel, own := treeRelOfImport(p)
			if uerr != nil || !own {
				continue
			}
			imports[rel] = true
		}
		out = append(out, goFileFacts{
			rel: rel, dir: path.Dir(rel), pkgName: file.Name.Name,
			file: file, fset: fset, imports: imports,
		})
	}
	return out
}

// judge — обход синтетики тем же путём, каким судится дерево.
func judge(t *testing.T, files map[string]string, ledger []openAccumulatorFinding) (accs []accumulator,
	findings, stale []string) {
	t.Helper()
	facts := synthFacts(t, files)
	accs = collectAccumulators(t, facts)
	findings, stale, _, _ = accumulatorVerdict(accs, collectAccumulatorReaders(facts),
		importersOfDirs(facts), ledger)
	return accs, findings, stale
}

// carrierSrc — носитель ровно той формы, ради которой гейт заведён.
const carrierSrc = `package acc

import "sync/atomic"

type Refusals struct {
	refused atomic.Uint64
	passed  atomic.Uint64
}

func (r *Refusals) Record() { r.refused.Add(1) }

func (r *Refusals) Counts() (refused, passed uint64) {
	return r.refused.Load(), r.passed.Load()
}
`

// TestAccumulatorGateFindsACarrierWithoutAReader — НАХОДКА, названная координатой.
func TestAccumulatorGateFindsACarrierWithoutAReader(t *testing.T) {
	accs, findings, stale := judge(t, map[string]string{"gateway/internal/acc/acc.go": carrierSrc}, nil)

	if len(accs) != 1 {
		t.Fatalf("накопителей распознано %d, ожидался 1 — предикат обхода не видит своего предмета", len(accs))
	}
	if len(findings) != 1 {
		t.Fatalf("находок %d, ожидалась 1: %v", len(findings), findings)
	}
	if !strings.Contains(findings[0], "gateway/internal/acc/acc.go:12") {
		t.Errorf("находка не называет КООРДИНАТУ объявления: %q", findings[0])
	}
	if !strings.Contains(findings[0], "Refusals.Counts()") {
		t.Errorf("находка не называет метод-слепок: %q", findings[0])
	}
	if len(stale) != 0 {
		t.Errorf("на пустой таблице записей просроченных быть не может: %v", stale)
	}
}

// TestAccumulatorGateIsSilentOnACarrierWithAReader — МОЛЧАНИЕ на законном близнеце.
func TestAccumulatorGateIsSilentOnACarrierWithAReader(t *testing.T) {
	_, findings, _ := judge(t, map[string]string{
		"gateway/internal/acc/acc.go": carrierSrc,
		"gateway/internal/rd/rd.go": `package rd

import "` + modulePathPrefix + `gateway/internal/acc"

func Report(r *acc.Refusals) uint64 { a, b := r.Counts(); return a + b }
`,
	}, nil)
	if len(findings) != 0 {
		t.Fatalf("гейт красен на ЗАКОННОЙ провязке — такой снимут следующим как ложный: %v", findings)
	}
}

// TestAccumulatorGateDoesNotCountAReadInTheDeclaringPackage — чтение у себя же
// величину наружу не выводит.
//
// Ось нужна дословно: полоса базового секрета читает свой же слепок, чтобы
// напечатать предупреждение, и без этой оси гейт засчитал бы это как «величина
// дошла до читателя».
//
// Держит ось УСЛОВИЕ ИМПОРТА, а не отдельная проверка «не свой каталог»: пакет
// не импортирует сам себя. Отдельная проверка в гейте СТОЯЛА и была снята —
// именно эта проба показала, что она недостижима: при её снятии проба
// оставалась зелёной, то есть доказывала не то, что заявляла.
func TestAccumulatorGateDoesNotCountAReadInTheDeclaringPackage(t *testing.T) {
	_, findings, _ := judge(t, map[string]string{
		"gateway/internal/acc/acc.go": carrierSrc,
		"gateway/internal/acc/self.go": `package acc

func (r *Refusals) Announce() uint64 { a, b := r.Counts(); return a + b }
`,
	}, nil)
	if len(findings) != 1 {
		t.Fatalf("внутренний вызов засчитан читателем: находок %d, ожидалась 1", len(findings))
	}
}

// TestAccumulatorGateDoesNotCountANameCollision — совпадение имени без импорта
// объявителя читателем не является.
func TestAccumulatorGateDoesNotCountANameCollision(t *testing.T) {
	_, findings, _ := judge(t, map[string]string{
		"gateway/internal/acc/acc.go": carrierSrc,
		"gateway/internal/other/other.go": `package other

type Unrelated struct{}

func (Unrelated) Counts() (uint64, uint64) { return 0, 0 }

func Use(u Unrelated) uint64 { a, b := u.Counts(); return a + b }
`,
	}, nil)
	if len(findings) != 1 {
		t.Fatalf("чужой одноимённый метод засчитан читателем: находок %d, ожидалась 1", len(findings))
	}
}

// TestAccumulatorGateCountsAMethodValueAsAReader — `x.Counts` без скобок.
//
// Форма не экзотическая, а основная: композиционный корень отдаёт читателя
// именно значением метода. Гейт, требующий скобок, требовал бы ритуала.
func TestAccumulatorGateCountsAMethodValueAsAReader(t *testing.T) {
	_, findings, _ := judge(t, map[string]string{
		"gateway/internal/acc/acc.go": carrierSrc,
		"gateway/internal/rd/rd.go": `package rd

import "` + modulePathPrefix + `gateway/internal/acc"

type reg struct{ read func() (uint64, uint64) }

func Wire(r *acc.Refusals) reg { return reg{read: r.Counts} }
`,
	}, nil)
	if len(findings) != 0 {
		t.Fatalf("значение метода не засчитано читателем: %v", findings)
	}
}

// TestAccumulatorGateIgnoresALatch — `atomic.Bool` → голый `bool` величиной не
// является.
//
// Ось найдена ИНЪЕКЦИЕЙ, а не чтением: первая редакция предиката краснела на
// защёлках `closed`/`stopped`/`ready`, которых в дереве десятки, — и такой гейт
// сняли бы следующим как ложный.
func TestAccumulatorGateIgnoresALatch(t *testing.T) {
	accs, findings, _ := judge(t, map[string]string{
		"gateway/internal/acc/latch.go": `package acc

import "sync/atomic"

type Latch struct{ closed atomic.Bool }

func (l *Latch) Close()       { l.closed.Store(true) }
func (l *Latch) Closed() bool { return l.closed.Load() }
`,
	}, nil)
	if len(accs) != 0 {
		t.Fatalf("защёлка распознана накопителем: %+v", accs)
	}
	if len(findings) != 0 {
		t.Fatalf("гейт красен на защёлке: %v", findings)
	}
}

// TestAccumulatorGateKeepsASnapshotBuiltAroundALatch — вычет узок: снимок,
// отдающий СТРУКТУРУ, остаётся накопителем, даже если внутри одна защёлка.
//
// Обратная сторона предыдущей оси. Без неё вычет съел бы кэш вердиктов базовой
// полосы — тот самый предмет, ради которого гейт трогали (#1221).
func TestAccumulatorGateKeepsASnapshotBuiltAroundALatch(t *testing.T) {
	accs, findings, _ := judge(t, map[string]string{
		"gateway/internal/acc/lane.go": `package acc

import "sync/atomic"

type Stats struct {
	Entries    int
	AtCapacity bool
}

type Lane struct{ atCapacity atomic.Bool }

func (l *Lane) CacheStats() Stats { return Stats{AtCapacity: l.atCapacity.Load()} }
`,
	}, nil)
	if len(accs) != 1 {
		t.Fatalf("снимок вокруг защёлки потерян обходом: накопителей %d, ожидался 1", len(accs))
	}
	if len(findings) != 1 {
		t.Fatalf("находок %d, ожидалась 1 — у снимка читателя нет", len(findings))
	}
}

// TestAccumulatorGateIgnoresAMethodThatTakesArguments — метод с параметром
// слепком не является: слепок отвечает «каковы величины», а не «какова эта».
func TestAccumulatorGateIgnoresAMethodThatTakesArguments(t *testing.T) {
	accs, _, _ := judge(t, map[string]string{
		"gateway/internal/acc/acc.go": `package acc

import "sync/atomic"

type Buckets struct{ n atomic.Uint64 }

func (b *Buckets) At(i int) uint64 { _ = i; return b.n.Load() }
`,
	}, nil)
	if len(accs) != 0 {
		t.Fatalf("метод с параметром принят за слепок: %+v", accs)
	}
}

// TestAccumulatorGateSeesAGenericCarrier — обобщённый носитель виден обходу.
//
// Ось найдена переписью, а не чтением: первая редакция читала получателя только
// в формах `T` и `*T`, и `*Cache[K, V]` проваливался мимо. Слепой зоной оказался
// кэш общего примитива вытеснения — тот самый носитель, ради величины которого
// гейт в этот раз и трогали, — а перепись назвала на один накопитель меньше, чем
// есть, что от исправной работы неотличимо.
func TestAccumulatorGateSeesAGenericCarrier(t *testing.T) {
	accs, findings, _ := judge(t, map[string]string{
		"gateway/internal/acc/cache.go": `package acc

import "sync/atomic"

type Cache[K comparable, V any] struct{ evictions atomic.Uint64 }

func (c *Cache[K, V]) Evict() { c.evictions.Add(1) }

func (c *Cache[K, V]) Evictions() uint64 { return c.evictions.Load() }
`,
	}, nil)
	if len(accs) != 1 {
		t.Fatalf("обобщённый носитель не распознан: накопителей %d, ожидался 1", len(accs))
	}
	if len(findings) != 1 {
		t.Fatalf("находок %d, ожидалась 1 — читателя у носителя нет", len(findings))
	}
}

// TestOpenFindingWithAReaderIsStale — САМОИСТЕЧЕНИЕ, форма первая: у записи
// появился читатель.
func TestOpenFindingWithAReaderIsStale(t *testing.T) {
	_, findings, stale := judge(t, map[string]string{
		"gateway/internal/acc/acc.go": carrierSrc,
		"gateway/internal/rd/rd.go": `package rd

import "` + modulePathPrefix + `gateway/internal/acc"

func Report(r *acc.Refusals) uint64 { a, b := r.Counts(); return a + b }
`,
	}, []openAccumulatorFinding{{
		dir: "gateway/internal/acc", typ: "Refusals", accessor: "Counts", owner: "проба",
	}})
	if len(findings) != 0 {
		t.Fatalf("находок быть не должно: %v", findings)
	}
	if len(stale) != 1 || !strings.Contains(stale[0], "снимите запись") {
		t.Fatalf("запись, которой нечего прощать, не объявлена просроченной: %v", stale)
	}
}

// TestOpenFindingWithoutASubjectIsStale — САМОИСТЕЧЕНИЕ, форма вторая: носителя
// в дереве больше нет.
func TestOpenFindingWithoutASubjectIsStale(t *testing.T) {
	_, _, stale := judge(t, map[string]string{"gateway/internal/acc/acc.go": carrierSrc},
		[]openAccumulatorFinding{{
			dir: "gateway/internal/nowhere", typ: "Ghost", accessor: "Counts", owner: "проба",
		}})
	if len(stale) != 1 || !strings.Contains(stale[0], "носителя в дереве нет") {
		t.Fatalf("запись о несуществующем носителе не объявлена просроченной: %v", stale)
	}
}

// TestOpenFindingWithALiveSubjectIsSilent — законный близнец к двум пробам выше:
// запись, у которой предмет ЖИВ, молчит.
//
// Без неё самоистечение зеленело бы на таблице, объявляющей просроченным всё.
func TestOpenFindingWithALiveSubjectIsSilent(t *testing.T) {
	_, findings, stale := judge(t, map[string]string{"gateway/internal/acc/acc.go": carrierSrc},
		[]openAccumulatorFinding{{
			dir: "gateway/internal/acc", typ: "Refusals", accessor: "Counts", owner: "проба",
		}})
	if len(findings) != 0 {
		t.Errorf("запись с живым предметом не сняла находку: %v", findings)
	}
	if len(stale) != 0 {
		t.Errorf("запись с живым предметом объявлена просроченной: %v", stale)
	}
}
