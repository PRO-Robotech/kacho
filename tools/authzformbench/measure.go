// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package authzformbench

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/PRO-Robotech/kacho/internal/gitenv"
)

// Outcome — three categories, never two.
//
// "not-run" is what a benchmark most wants to lose: a shape whose stack did not
// come up, whose operation timed out, whose store refused the request, has NOT
// won. It is carried to the report with its reason and is never averaged, never
// omitted and never counted in anybody's favour.
type Outcome string

const (
	Measured Outcome = "measured"
	Refused  Outcome = "refused" // the engine answered "no" — a fact about the engine
	NotRun   Outcome = "not-run" // nothing was measured; the reason says why
	// NotApplicable — четвёртый исход, и он НЕ вычитается ни в чью пользу и не
	// сворачивается в «не выполнилось».
	//
	// Иначе ячейка «выдача в той же транзакции» у движка попадёт в «не
	// выполнилось», отчёт скажет «не-измеренных 2», и читатель решит, что замер
	// не доехал, — тогда как это самый содержательный результат таблицы.
	NotApplicable Outcome = "not-applicable"
)

// Op names one measured operation.
type Op string

const (
	OpGrant    Op = "W1-grant"         // materialize the binding over N objects
	OpRevoke   Op = "W2-revoke-1"      // withdraw ONE subject
	OpRelabel1 Op = "W3-relabel-1"     // one object enters the set
	OpRelabelK Op = "W4-relabel-K"     // K objects enter the set
	OpCheck    Op = "R1-check"         // one object, one verb, one subject
	OpPage50   Op = "R2-one-partition" // ONE BatchCheck request (size = cfg.Partition)
	OpPageFull Op = "R3-page-full"     // a whole page: cfg.PageSize, partitioned+parallel
	OpVolume   Op = "V-volume"         // tuples and bytes

	// Операции, которых у движка отношений нет by construction, и каскад.
	//
	// Они заведены в СЛОВАРЕ, а не описаны одной прозой: ячейка, которой нет в
	// словаре, не входит в сумму категорий и не печатается — отчёт был бы полон
	// по своему собственному счёту и молчал бы о самых дорогих вопросах.
	OpInlineGrant  Op = "T-inline-grant"  // выдача в той же транзакции, что и предмет выдачи
	OpInlineRevoke Op = "T-inline-revoke" // отзыв в той же транзакции
	OpCascade      Op = "C-cascade"       // вопрос каскадного принципала (три верхних уровня)
)

// opsWrite / opsRead — порядок операций в отчёте и единственное место, где он
// объявлен. Перечень, выписанный в каждой функции по отдельности, разошёлся бы с
// собой на первой же новой операции.
var (
	opsWrite = []Op{OpGrant, OpRevoke, OpRelabel1, OpRelabelK, OpInlineGrant, OpInlineRevoke, OpVolume}
	opsRead  = []Op{OpCheck, OpPage50, OpPageFull, OpCascade}
	opsAll   = []Op{OpGrant, OpRevoke, OpRelabel1, OpRelabelK, OpInlineGrant, OpInlineRevoke,
		OpCheck, OpPage50, OpPageFull, OpCascade, OpVolume}
)

// Cell is one (form, N, op) result.
type Cell struct {
	Form    Form
	N       int
	Op      Op
	Outcome Outcome
	Reason  string
	Place   string // откуда снята ячейка — отчёт из двух мест без этого признака выдаёт за один прогон два

	Repeats            int
	P50, P95, Min, Max float64 // milliseconds; zero when Outcome != Measured

	// Прежняя колонка `Requests` расщеплена на две САМОСТОЯТЕЛЬНЫЕ: у движка
	// «обращение» — это HTTP-вызов, за которым стоит неизвестное число запросов к
	// его Postgres, у формы E — SQL-стейтмент. Величины разные, ни одна не
	// называется «обращениями» без уточнения, и ни в одной ячейке они не
	// складываются.
	ReqEngine int    // круговые обращения к движку
	StmtSQL   int    // SQL-стейтменты
	StmtNote  string // непусто ⇒ StmtSQL НЕ измерен, здесь причина; печатается вместо величины

	Parts  int // на сколько частей разложена страница (у формы E — одна)
	Tuples int // строк намерения, которых операция коснулась

	GrantTotal      int64 // volume only: grant rows in the store
	GrantBytes      int64 // volume only: logical row bytes of ALL its rows (см. Volume)
	StructuralRows  int64 // volume only: то, что есть у каждой формы одинаково
	StructuralBytes int64
	TableBytes      int64 // volume only: whole relations, incl. indexes — NOT per-shape
}

// Stats fills the percentile fields from raw millisecond samples.
func (c *Cell) Stats(ms []float64) {
	if len(ms) == 0 {
		c.Outcome = NotRun
		if c.Reason == "" {
			c.Reason = "zero samples"
		}
		return
	}
	s := append([]float64(nil), ms...)
	sort.Float64s(s)
	c.Repeats = len(s)
	c.Min, c.Max = s[0], s[len(s)-1]
	c.P50 = pct(s, 0.50)
	c.P95 = pct(s, 0.95)
}

func pct(sorted []float64, p float64) float64 {
	if len(sorted) == 1 {
		return sorted[0]
	}
	idx := p * float64(len(sorted)-1)
	lo := int(math.Floor(idx))
	hi := int(math.Ceil(idx))
	if lo == hi {
		return sorted[lo]
	}
	frac := idx - float64(lo)
	return sorted[lo]*(1-frac) + sorted[hi]*frac
}

// Config governs one run.
type Config struct {
	Ns           []int // the curve — at least four orders, or the slope is unknown
	Subjects     int   // S
	Verbs        []string
	Role         string
	RelabelK     int // K for W4
	PageSize     int // R3 page size (the contract page is 1000)
	Partition    int // objects per BatchCheck request
	Parallelism  int // partitions in flight
	WriteRepeats int // measured repeats for write ops (warm-up is extra)
	ReadRepeats  int // measured samples for read ops (warm-up is extra)
	Forms        []Form
}

// DefaultConfig mirrors the production numbers wherever one exists: the partition
// and parallelism are authzfilter's (50 / 8) and the page is the contract's max
// (pkg/validate.MaxPageSize = 1000). Where no production number exists — repeats —
// the value is the minimum the methodology demands, not a comfortable one.
func DefaultConfig() Config {
	return Config{
		Ns:           []int{10, 100, 1000, 10000},
		Subjects:     10,
		Verbs:        DefaultVerbs(),
		Role:         "editor",
		RelabelK:     100,
		PageSize:     1000,
		Partition:    50,
		Parallelism:  8,
		WriteRepeats: 5,
		ReadRepeats:  20,
		Forms:        AllForms,
	}
}

// Provenance is what makes a number evidence rather than an anecdote.
type Provenance struct {
	When        string
	TreeRev     string
	Machine     string
	OpenFGA     string
	Postgres    string
	CLI         string
	BatchCap    int // MEASURED off the engine, not assumed
	ModelPath   string
	ModelDigest string

	// RelPostgres — Postgres формы E: своя посадка, не датастор движка.
	RelPostgres string
	// StmtProducers — состояние производителя `StmtSQL` по КАЖДОМУ месту снятия.
	// Величина, у входа которой нет производителя, зеленеет молча — поэтому это
	// печатается всегда, и при успехе, и при провале.
	StmtProducers []ProducerStatus
	// CascadeChain — объявленная цепь обхода каскада и её глубина.
	CascadeChain string
	CascadeDepth int
}

func CollectProvenance(st *Stack, modelPath string, modelDigest string) Provenance {
	rev := "unknown"
	if out, err := gitenv.Command("", "rev-parse", "HEAD").Output(); err == nil {
		rev = strings.TrimSpace(string(out))
	}
	host, _ := os.Hostname()
	var mem string
	if b, err := os.ReadFile("/proc/meminfo"); err == nil {
		for _, ln := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(ln, "MemTotal:") {
				mem = strings.TrimSpace(strings.TrimPrefix(ln, "MemTotal:"))
				break
			}
		}
	}
	return Provenance{
		When:          time.Now().Format(time.RFC3339),
		TreeRev:       rev,
		Machine:       fmt.Sprintf("%s %s/%s %d cpu, MemTotal %s", host, runtime.GOOS, runtime.GOARCH, runtime.NumCPU(), mem),
		OpenFGA:       st.OpenFGA,
		Postgres:      st.Postgres,
		CLI:           envOr("AUTHZFORMBENCH_CLI_IMAGE", defaultCLIImage),
		BatchCap:      st.BatchCap,
		ModelPath:     modelPath,
		ModelDigest:   modelDigest,
		RelPostgres:   st.RelPostgres,
		StmtProducers: []ProducerStatus{st.StmtProducer, relProducerStatus},
		CascadeChain:  CascadeChain,
		CascadeDepth:  CascadeDepth,
	}
}

// relProducerStatus — состояние производителя формы E, каким оно объявлено до
// прогона. Фактическое состояние проверяется при открытии КАЖДОГО её хранилища и
// роняет открытие, если контроль не прошёл: величина, снятая производителем без
// контроля, печаталась бы неотличимо от измеренной.
var relProducerStatus = ProducerStatus{
	Place:    "форма E (Postgres формы E)",
	Producer: "счётчик стейтментов на pgx.Tracer собственного пула",
	OK:       true,
	Note:     "контроль в обе стороны прогоняется при открытии каждого хранилища и роняет открытие",
}

// classify turns an error into an outcome. A refusal by the engine and a failure to
// reach it are different facts and are kept apart; folding them would let a shape
// that the engine REJECTS look like a shape the harness merely could not reach.
func classify(err error) (Outcome, string) {
	if err == nil {
		return Measured, ""
	}
	// Неприменимость по построению — не отказ и не недоезд: у неё свой исход и
	// своя строка отчёта с причиной.
	if errors.Is(err, ErrNotApplicable) {
		return NotApplicable, err.Error()
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return Refused, apiErr.Error()
	}
	return NotRun, err.Error()
}

// Runner executes the matrix.
type Runner struct {
	Stack *Stack
	Cfg   Config
	Canon string

	models map[Form][]byte
	Notes  map[Form]string
}

// NewRunner готовит модели форм.
//
// «Модель не требуется» — законный ответ, а не отказ: конструктор пяти прежних
// форм требовал DSL для КАЖДОЙ, и до этой правки шестая форма не заводилась бы
// вовсе — раньше первого замера и раньше любой проверки того, отвечает ли она
// правильно. Ошибка при этом никуда не делась: неизвестное имя формы по-прежнему
// возвращает ошибку, иначе «модель не требуется» покрывало бы и опечатку.
func NewRunner(st *Stack, cfg Config, canon string) (*Runner, error) {
	r := &Runner{Stack: st, Cfg: cfg, Canon: canon,
		models: map[Form][]byte{}, Notes: map[Form]string{}}
	for _, f := range cfg.Forms {
		dsl, note, err := ModelFor(f, canon)
		switch {
		case errors.Is(err, ErrModelNotRequired):
			r.Notes[f] = note
			continue
		case err != nil:
			return nil, fmt.Errorf("model for %s: %w", f, err)
		}
		js, err := TransformDSL(dsl)
		if err != nil {
			return nil, fmt.Errorf("transform model for %s: %w", f, err)
		}
		r.models[f] = js
		r.Notes[f] = note
	}
	return r, nil
}

// NewSeededStore builds a store for `f`, seeds the structural tuples and (when
// `grant`) the shape's grant tuples. Returned ready to be asked questions.
//
// Возвращается ГРАНИЦА, а не конкретный тип: до неё шестая форма к матрице не
// подключалась ни одной операцией.
func (r *Runner) NewSeededStore(ctx context.Context, f Form, sc Scenario, grant bool, name string) (RightsStore, error) {
	st, err := r.openStore(ctx, f, sc, name)
	if err != nil {
		return nil, err
	}
	var g []Tuple
	if grant {
		g = Grant(f, sc)
	}
	if _, err := st.Seed(ctx, sc.Structural(), g); err != nil {
		_ = st.Teardown(ctx)
		return nil, err
	}
	return st, nil
}

// openStore создаёт пустое хранилище формы за границей.
func (r *Runner) openStore(ctx context.Context, f Form, sc Scenario, name string) (RightsStore, error) {
	if f == FormE {
		if r.Stack.RelDSN == "" {
			return nil, fmt.Errorf("у формы E нет своей БД — стек поднят без неё")
		}
		return newRelStore(ctx, r.Stack.RelDSN, relSchemaName(name), sc)
	}
	model, ok := r.models[f]
	if !ok {
		return nil, fmt.Errorf("модель формы %s не подготовлена", f)
	}
	st, err := r.Stack.NewStore(ctx, name, model)
	if err != nil {
		return nil, err
	}
	return &engineStore{st: st, stmts: r.Stack.stmts, prod: r.Stack.StmtProducer}, nil
}

// relSchemaName делает из имени store'а имя схемы: Postgres не любит в
// идентификаторе ни дефиса, ни длины сверх 63 знаков.
func relSchemaName(name string) string {
	s := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
			return r
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		default:
			return '_'
		}
	}, name)
	if len(s) > 50 {
		s = s[:50]
	}
	return "e_" + s
}

// RunWrites measures W1..W4 and the volume for one (form, N).
//
// Each repeat gets a FRESH store: a write measured against a store that already
// carries the tuples is measuring an idempotent no-op, which is a different
// operation wearing the same name.
func (r *Runner) RunWrites(ctx context.Context, f Form, sc Scenario) []Cell {
	cells := map[Op]*Cell{}
	samples := map[Op][]float64{}
	for _, op := range opsWrite {
		cells[op] = &Cell{Form: f, N: sc.N, Op: op, Outcome: Measured}
	}

	spares := make([]string, 0, sc.Spare)
	for i := sc.N; i < sc.N+sc.Spare; i++ {
		spares = append(spares, sc.Object(i))
	}
	relabelOne := spares[0]
	relabelK := spares[1:min(1+r.Cfg.RelabelK, len(spares))]
	// Объект «встраиваемых» операций лежит ЗА пределами засеянного набора — он
	// заводится самой транзакцией, вместе с выдачей на него. Это и вернее по
	// смыслу (предмет выдачи создаётся тут же), и не трогает фикстуру пяти
	// прежних форм: возьми он лишний запасной объект — структурная часть выросла
	// бы на строку у КАЖДОЙ формы, и база сравнения сдвинулась бы вся целиком.
	// Сдвиг был бы одинаков и потому безобиден для сравнения форм между собой —
	// но числа перестали бы совпадать с прежними, а «те же, что до правки» это то
	// единственное, чем граница доказывается.
	inlineObj := sc.Object(sc.N + sc.Spare)

	fail := func(op Op, err error) {
		o, reason := classify(err)
		c := cells[op]
		c.Outcome, c.Reason = o, reason
	}

	total := r.Cfg.WriteRepeats + 1 // repeat 0 is warm-up and is discarded
	for rep := 0; rep < total; rep++ {
		st, err := r.openStore(ctx, f, sc, fmt.Sprintf("w-%s-n%d-r%d", f, sc.N, rep))
		if err == nil {
			_, err = st.Seed(ctx, sc.Structural(), nil)
			if err != nil {
				_ = st.Teardown(ctx)
			}
		}
		if err != nil {
			for _, op := range opsWrite {
				if cells[op].Outcome == Measured {
					fail(op, err)
				}
			}
			return finish(cells, samples, rep > 0)
		}
		for _, op := range opsWrite {
			cells[op].Place = st.Place()
			cells[op].StmtNote = stmtNote(st)
		}

		grant := Grant(f, sc)
		d, cnt, err := timedC(func() (Counters, error) { return st.Write(ctx, grant) })
		if err != nil {
			fail(OpGrant, err)
		} else if rep > 0 {
			samples[OpGrant] = append(samples[OpGrant], d)
			cells[OpGrant].ReqEngine, cells[OpGrant].StmtSQL = cnt.ReqEngine, cnt.StmtSQL
			cells[OpGrant].Tuples = len(grant)
		}

		if rep == total-1 && cells[OpGrant].Outcome == Measured && cells[OpVolume].Outcome == Measured {
			// Volume is read HERE — after the grant and BEFORE the relabel and revoke
			// operations — because those mutate the store, and a count taken after them
			// is the count of a different thing.
			//
			// It was taken at the end of the repeat in the first version of this file,
			// and the guard in TestHarnessDiscriminates* caught it: at N=20, M=4, S=5 the
			// shape predicts 400 tuples and the store held 440 (+20 relabel-1, +100
			// relabel-K, −80 revoke). Recorded here rather than quietly corrected: a
			// volume figure is the one number in this report that looks unimpeachable,
			// so the fact that it was wrong once — and that only the cross-check against
			// the shape's own arithmetic found it — is the reason that cross-check stays.
			vol, verr := st.Volume(ctx)
			if verr != nil {
				fail(OpVolume, verr)
			} else {
				cells[OpVolume].GrantTotal = vol.GrantRows
				cells[OpVolume].GrantBytes = vol.GrantBytes
				cells[OpVolume].StructuralRows = vol.StructuralRows
				cells[OpVolume].StructuralBytes = vol.StructuralBytes
				cells[OpVolume].TableBytes = vol.TableBytes
				samples[OpVolume] = []float64{0} // volume has no duration; one sample keeps Stats honest
			}
		}

		if cells[OpGrant].Outcome == Measured {
			t1 := RelabelOne(f, sc, relabelOne)
			d, cnt, err := timedC(func() (Counters, error) { return st.Write(ctx, t1) })
			if err != nil {
				fail(OpRelabel1, err)
			} else if rep > 0 {
				samples[OpRelabel1] = append(samples[OpRelabel1], d)
				cells[OpRelabel1].ReqEngine, cells[OpRelabel1].StmtSQL = cnt.ReqEngine, cnt.StmtSQL
				cells[OpRelabel1].Tuples = len(t1)
			}

			tk := RelabelMany(f, sc, relabelK)
			d, cnt, err = timedC(func() (Counters, error) { return st.Write(ctx, tk) })
			if err != nil {
				fail(OpRelabelK, err)
			} else if rep > 0 {
				samples[OpRelabelK] = append(samples[OpRelabelK], d)
				cells[OpRelabelK].ReqEngine, cells[OpRelabelK].StmtSQL = cnt.ReqEngine, cnt.StmtSQL
				cells[OpRelabelK].Tuples = len(tk)
			}

			rv := RevokeSubject(f, sc, sc.Subjects[0])
			d, cnt, err = timedC(func() (Counters, error) { return st.Remove(ctx, rv) })
			if err != nil {
				fail(OpRevoke, err)
			} else if rep > 0 {
				samples[OpRevoke] = append(samples[OpRevoke], d)
				cells[OpRevoke].ReqEngine, cells[OpRevoke].StmtSQL = cnt.ReqEngine, cnt.StmtSQL
				cells[OpRevoke].Tuples = len(rv)
			}

			// Встраиваемая пара. У движка отношений она неприменима by
			// construction — и это отдельный исход, а не неудача замера.
			r.runInline(ctx, st, sc, inlineObj, rep, cells, samples, fail)
		}

		_ = st.Teardown(ctx)
	}
	return finish(cells, samples, true)
}

// runInline меряет выдачу и отзыв, написанные в ОДНОЙ транзакции с предметом
// выдачи.
func (r *Runner) runInline(ctx context.Context, st RightsStore, sc Scenario, obj string, rep int,
	cells map[Op]*Cell, samples map[Op][]float64, fail func(Op, error)) {
	data, grant := InlineIntent(sc, obj)
	d, cnt, err := timedC(func() (Counters, error) { return st.InlineGrant(ctx, data, grant) })
	if err != nil {
		fail(OpInlineGrant, err)
	} else if rep > 0 {
		samples[OpInlineGrant] = append(samples[OpInlineGrant], d)
		cells[OpInlineGrant].ReqEngine, cells[OpInlineGrant].StmtSQL = cnt.ReqEngine, cnt.StmtSQL
		cells[OpInlineGrant].Tuples = len(data) + len(grant)
	}

	rdata, revoke := InlineRevokeIntent(sc, obj)
	d, cnt, err = timedC(func() (Counters, error) { return st.InlineRevoke(ctx, rdata, revoke) })
	if err != nil {
		fail(OpInlineRevoke, err)
	} else if rep > 0 {
		samples[OpInlineRevoke] = append(samples[OpInlineRevoke], d)
		cells[OpInlineRevoke].ReqEngine, cells[OpInlineRevoke].StmtSQL = cnt.ReqEngine, cnt.StmtSQL
		cells[OpInlineRevoke].Tuples = len(rdata) + len(revoke)
	}
}

// stmtNote — причина, по которой колонка `StmtSQL` этого места не печатается.
// Пусто ⇒ производитель прошёл контроль в обе стороны и величина измерена.
func stmtNote(st RightsStore) string {
	p := st.StmtProducer()
	if p.OK {
		return ""
	}
	return "производитель не прошёл контроль: " + p.Note
}

// RunReads measures R1..R3 for one (form, N) against a single seeded store.
func (r *Runner) RunReads(ctx context.Context, f Form, sc Scenario) []Cell {
	cells := map[Op]*Cell{}
	samples := map[Op][]float64{}
	for _, op := range opsRead {
		cells[op] = &Cell{Form: f, N: sc.N, Op: op, Outcome: Measured}
	}
	st, err := r.NewSeededStore(ctx, f, sc, true, fmt.Sprintf("rd-%s-n%d", f, sc.N))
	if err == nil {
		// Каскадный принципал засевается ТОЛЬКО в хранилище чтения: в хранилище
		// записи он изменил бы колонку объёма, а она — база сравнения пяти форм.
		_, err = st.Write(ctx, CascadeSeed(f, sc))
		if err != nil {
			err = fmt.Errorf("засев каскадного принципала: %w", err)
			_ = st.Teardown(ctx)
		}
	}
	if err != nil {
		for _, op := range opsRead {
			o, reason := classify(err)
			cells[op].Outcome, cells[op].Reason = o, reason
		}
		return finish(cells, samples, false)
	}
	defer func() { _ = st.Teardown(ctx) }()
	for _, op := range opsRead {
		cells[op].Place = st.Place()
		cells[op].StmtNote = stmtNote(st)
	}

	subj := sc.Subjects[len(sc.Subjects)-1] // the LAST subject: never the one a
	// direct-index shape happens to have written first, so the sample is not
	// accidentally the cheapest row in the store.
	objs := sc.Objects()

	page := func(n int) []string {
		if n > len(objs) {
			n = len(objs)
		}
		return objs[:n]
	}

	// R1 walks the object set with a large prime stride rather than from the front.
	// Sampling objs[0..repeats) would keep asking about the rows a flat shape wrote
	// FIRST, which is the part of the index most likely to be warm — a cheapness
	// that belongs to the sampling, not to the shape.
	pick := func(rep int) string { return objs[(rep*7919)%len(objs)] }

	total := r.Cfg.ReadRepeats + 1
	for rep := 0; rep < total; rep++ {
		d, cnt, err := timedC(func() (Counters, error) {
			ok, c, e := st.Check(ctx, subj, "v_get", pick(rep))
			if e == nil && !ok {
				return c, fmt.Errorf("R1 asked a question the fixture says is ALLOWED and got deny — "+
					"the shape %s is not seeded as claimed", f)
			}
			return c, e
		})
		if err != nil {
			o, reason := classify(err)
			cells[OpCheck].Outcome, cells[OpCheck].Reason = o, reason
			break
		}
		if rep > 0 {
			samples[OpCheck] = append(samples[OpCheck], d)
			cells[OpCheck].ReqEngine, cells[OpCheck].StmtSQL = cnt.ReqEngine, cnt.StmtSQL
		}
	}

	for rep := 0; rep < total; rep++ {
		p := page(r.Cfg.Partition)
		d, cnt, err := timedC(func() (Counters, error) {
			res, c, e := st.BatchCheck(ctx, subj, "v_get", p)
			if e == nil {
				e = allMustAllow(f, "R2", res)
			}
			return c, e
		})
		if err != nil {
			o, reason := classify(err)
			cells[OpPage50].Outcome, cells[OpPage50].Reason = o, reason
			break
		}
		if rep > 0 {
			samples[OpPage50] = append(samples[OpPage50], d)
			cells[OpPage50].ReqEngine, cells[OpPage50].StmtSQL = cnt.ReqEngine, cnt.StmtSQL
			cells[OpPage50].Tuples = len(p)
		}
	}

	for rep := 0; rep < total; rep++ {
		p := page(r.Cfg.PageSize)
		var parts int
		d, cnt, err := timedC(func() (Counters, error) {
			res, np, c, e := st.CheckPage(ctx, subj, "v_get", p, r.Cfg.Partition, r.Cfg.Parallelism)
			parts = np
			if e == nil {
				e = allMustAllow(f, "R3", res)
			}
			return c, e
		})
		if err != nil {
			o, reason := classify(err)
			cells[OpPageFull].Outcome, cells[OpPageFull].Reason = o, reason
			break
		}
		if rep > 0 {
			samples[OpPageFull] = append(samples[OpPageFull], d)
			cells[OpPageFull].ReqEngine, cells[OpPageFull].StmtSQL = cnt.ReqEngine, cnt.StmtSQL
			cells[OpPageFull].Parts = parts
			cells[OpPageFull].Tuples = len(p)
		}
	}

	r.runCascade(ctx, st, f, sc, cells, samples)
	return finish(cells, samples, true)
}

// runCascade меряет вопрос КАСКАДНОГО принципала — того, кто чинит аварию.
//
// Перед первым замером здесь стоят ДВА отрицательных контроля, и они про разное.
// Первый: обычный посторонний на том же объекте обязан получить отказ — без него
// «разрешено каскадному» неотличимо от «разрешено всем», и самая быстрая форма —
// та, что разрешает каждому, — выиграла бы эту ячейку, не тронув ни одного
// соединения. Второй: тот же каскадный принципал на объекте ЧУЖОГО аккаунта
// обязан получить отказ — без него неотличимы «администратор ЭТОГО аккаунта» и
// «администратор любого», а это и есть предмет уровня 3.
//
// Глубина цепи объявлена (`leaf → project → account → cluster`) и печатается
// рядом с числом: глубина обязана быть названа, а не подразумеваться.
func (r *Runner) runCascade(ctx context.Context, st RightsStore, f Form, sc Scenario,
	cells map[Op]*Cell, samples map[Op][]float64) {
	c := cells[OpCascade]
	obj := sc.Object(sc.N / 2)

	denied, _, err := st.Check(ctx, "user:stranger-not-in-any-binding", "v_get", obj)
	if err != nil {
		c.Outcome, c.Reason = classify(err)
		return
	}
	if denied {
		c.Outcome = NotRun
		c.Reason = "отрицательный контроль каскада не сработал: посторонний получил доступ, " +
			"значит «разрешено каскадному» здесь неотличимо от «разрешено всем»"
		return
	}

	crossTenant, _, err := st.Check(ctx, cascadeAdmin, "v_get", sc.ForeignObject())
	if err != nil {
		c.Outcome, c.Reason = classify(err)
		return
	}
	if crossTenant {
		c.Outcome = NotRun
		c.Reason = "кросс-аккаунтный контроль каскада не сработал: администратор аккаунта " +
			"получил доступ к объекту ЧУЖОГО аккаунта, значит уровень 3 здесь неотличим " +
			"от уровня кластера"
		return
	}

	total := r.Cfg.ReadRepeats + 1
	for rep := 0; rep < total; rep++ {
		d, cnt, err := timedC(func() (Counters, error) {
			ok, cc, e := st.Check(ctx, cascadeAdmin, "v_get", obj)
			if e == nil && !ok {
				return cc, fmt.Errorf("каскадный принципал получил отказ у формы %s — "+
					"три верхних уровня доступа разрешаются каскадом, и форма, отвечающая здесь "+
					"отказом, отвечает не то же самое, что остальные", f)
			}
			return cc, e
		})
		if err != nil {
			c.Outcome, c.Reason = classify(err)
			return
		}
		if rep > 0 {
			samples[OpCascade] = append(samples[OpCascade], d)
			c.ReqEngine, c.StmtSQL = cnt.ReqEngine, cnt.StmtSQL
			c.Parts = CascadeDepth
		}
	}
}

func finish(cells map[Op]*Cell, samples map[Op][]float64, keep bool) []Cell {
	var out []Cell
	for _, op := range opsAll {
		c, ok := cells[op]
		if !ok {
			continue
		}
		if c.Outcome == Measured {
			if !keep {
				c.Outcome, c.Reason = NotRun, orDefault(c.Reason, "no repeat completed")
			} else {
				c.Stats(samples[op])
			}
		}
		out = append(out, *c)
	}
	return out
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// allMustAllow refuses a page of denials.
//
// Every object in these pages is inside the granted set, so every verdict must be
// allow. Without this the fastest possible result — a shape seeded wrong, answering
// "no" to everything without touching the index — would win the read columns
// outright, and nothing in a duration would give it away.
func allMustAllow(f Form, op string, res []bool) error {
	if len(res) == 0 {
		return fmt.Errorf("%s/%s: empty verdict vector", f, op)
	}
	for i, ok := range res {
		if !ok {
			return fmt.Errorf("%s/%s: object %d of %d was DENIED, but every object of this page is "+
				"inside the granted set — the store is not seeded as the shape claims, and its read "+
				"timings would be the timing of a denial", f, op, i, len(res))
		}
	}
	return nil
}

// timedC меряет длительность и возвращает счётчики РАЗДЕЛЬНО — обе колонки, ни
// одна из которых не называется «обращениями» без уточнения.
func timedC(fn func() (Counters, error)) (ms float64, c Counters, err error) {
	t0 := time.Now()
	c, err = fn()
	return float64(time.Since(t0).Microseconds()) / 1000.0, c, err
}
