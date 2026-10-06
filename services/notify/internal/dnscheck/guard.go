// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package dnscheck — собственный страж старта notify по DNS установки и
// периодическая перепроверка (Р19 приёмки NTF-1; замысел issue-2915 §12а
// «Проверки и род ответа», «Резолвер и срок запроса», «Смена ключа DKIM»).
//
// Проверок три — [CheckDKIM], [CheckSPF], [CheckDMARC]; исход каждой — закрытый
// тип `{ok, violated(причина), noanswer}` ([Result]). Разбор записей — закрытые
// таблицы ([parseSPF], [parseDMARC], [parseDKIM]), род ответа — [classifyErr],
// имя запроса — [queryName], всегда абсолютное.
//
// Попытка — три проверки ПАРАЛЛЕЛЬНО, каждая под своим сроком запроса
// [queryTimeout]; нарушение любой отменяет остальные. На старте ([Guard.Boot])
// нарушение — отказ с именем проверки, «ответа нет» — повтор после
// [retryPause] до срока старта; на перепроверке ([Guard.Run]) — одна попытка на
// такт, «ответа нет» признак не меняет. Отмена родителя исходом проверки не
// является: признак не меняется, отказа с именем проверки нет.
//
// Срок старта, пауза и интервал перепроверки идут по внедрённым часам
// ([Clock]); срок одного запроса — по реальному времени (контекст), его
// отсчитывает сетевой стек.
package dnscheck

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/PRO-Robotech/kacho/services/notify/internal/dkimkey"
)

// queryTimeout — срок одного запроса DNS (§8, постоянная, не ручка).
const queryTimeout = 5 * time.Second

// retryPause — пауза между попытками стража на старте (§8, N44-1): мгновенный
// SERVFAIL не крутит цикл.
const retryPause = time.Second

// Clock — внедряемые часы: срок старта, пауза и интервал перепроверки.
type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

type systemClock struct{}

func (systemClock) Now() time.Time                         { return time.Now() }
func (systemClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// SystemClock — часы процесса для корня.
var SystemClock Clock = systemClock{}

// Options — входы стража. Все поля обязательны.
type Options struct {
	// FromDomain — домен `From` (домен адреса отправителя установки, Д101) в
	// нормализованной форме загрузчика.
	FromDomain string
	// KeyFile, SelectorFile — пути пары DKIM в томе объекта
	// (KACHO_NOTIFY_DKIM_KEY_FILE, KACHO_NOTIFY_DKIM_SELECTOR_FILE).
	KeyFile      string
	SelectorFile string
	// FS — порт чтения тома; корень передаёт dkimkey.OS.
	FS dkimkey.PairFS
	// Clock — внедряемые часы; корень передаёт [SystemClock].
	Clock Clock
	// Registerer — реестр признака `notify_mail_dns_posture{check}`.
	Registerer prometheus.Registerer
	Logger     *slog.Logger
}

// Guard — страж DNS установки и перепроверка. Пара, которой идёт подпись,
// меняется атомарно ([Guard.Pair]).
type Guard struct {
	r       *net.Resolver
	o       Options
	pair    atomic.Pointer[dkimkey.Pair]
	posture *prometheus.GaugeVec
}

// New собирает страж. nil-резолвер — отказ: подстановки net.DefaultResolver
// внутри пакета нет, резолвер пода передаёт корень явно (CX1-131 (б)).
func New(r *net.Resolver, o Options) (*Guard, error) {
	var bad []string
	if r == nil {
		bad = append(bad, "резолвер не передан")
	}
	if strings.TrimSuffix(o.FromDomain, ".") == "" {
		bad = append(bad, "домен From пуст")
	}
	if o.KeyFile == "" || o.SelectorFile == "" {
		bad = append(bad, "пути пары DKIM не заданы")
	}
	if o.FS == nil {
		bad = append(bad, "порт чтения тома не передан")
	}
	if o.Clock == nil {
		bad = append(bad, "часы не переданы")
	}
	if o.Registerer == nil {
		bad = append(bad, "реестр признака не передан")
	}
	if o.Logger == nil {
		bad = append(bad, "журнал не передан")
	}
	if len(bad) > 0 {
		return nil, fmt.Errorf("dnscheck: страж DNS установки: %s", strings.Join(bad, "; "))
	}
	posture := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "notify_mail_dns_posture",
		Help: "Исход последней проверки DNS установки по проверке: 1 — исправна, 0 — нарушена. " +
			"«Ответа нет» значение не меняет.",
	}, []string{"check"})
	if err := o.Registerer.Register(posture); err != nil {
		return nil, fmt.Errorf("dnscheck: признак notify_mail_dns_posture: %w", err)
	}
	return &Guard{r: r, o: o, posture: posture}, nil
}

// observe — единственная функция, выставляющая признак: метка — значение
// закрытого перечня [Check].
func (g *Guard) observe(c Check, ok bool) {
	v := 0.0
	if ok {
		v = 1
	}
	g.posture.WithLabelValues(string(c)).Set(v)
}

// Pair — пара, которой идёт подпись. До успешного [Guard.Boot] — нулевая.
func (g *Guard) Pair() dkimkey.Pair {
	if p := g.pair.Load(); p != nil {
		return *p
	}
	return dkimkey.Pair{}
}

// LoadPair — чтение пары ([dkimkey.ReadPairFS]) и проверки загрузки, которых у
// пакета чтения нет: селектор в форме имени DNS ([ValidSelector]). Одна функция
// для загрузчика конфигурации и перепроверки.
func LoadPair(fsys dkimkey.PairFS, keyFile, selectorFile string) (dkimkey.Pair, error) {
	p, err := dkimkey.ReadPairFS(fsys, keyFile, selectorFile)
	if err != nil {
		return dkimkey.Pair{}, err
	}
	if !ValidSelector(p.Selector) {
		return dkimkey.Pair{}, &dkimkey.Error{Part: dkimkey.PartSelector, Name: filepath.Base(selectorFile),
			Reason: dkimkey.ReasonSelectorForm}
	}
	return p, nil
}

// Boot — страж старта со сроком deadline по внедрённым часам. Нарушение любой
// проверки — немедленный отказ с её именем и причиной; «ответа нет» дольше
// срока — отказ «исчерпан срок» с именами проверок без ответа. nil — все три
// проверки исправны, признаки = 1.
func (g *Guard) Boot(ctx context.Context, deadline time.Duration) error {
	pair, err := LoadPair(g.o.FS, g.o.KeyFile, g.o.SelectorFile)
	if err != nil {
		return fmt.Errorf("страж DNS установки: %w", err)
	}
	g.pair.Store(&pair)

	end := g.o.Clock.Now().Add(deadline)
	var silent []Check
	for {
		remaining := end.Sub(g.o.Clock.Now())
		if remaining <= 0 {
			return deadlineErr(deadline, silent)
		}
		res := g.attempt(ctx, pair, checks[:], min(queryTimeout, remaining))
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("страж DNS установки прерван: %w", err)
		}
		if c, r, ok := firstViolation(res); ok {
			g.observe(c, false)
			return fmt.Errorf("страж DNS установки: проверка %s нарушена: %s", c, r.Reason)
		}
		silent = silent[:0]
		for _, c := range checks {
			if res[c].Outcome == OutcomeNoAnswer {
				silent = append(silent, c)
			}
		}
		if len(silent) == 0 {
			for _, c := range checks {
				g.observe(c, true)
			}
			g.o.Logger.Info("страж DNS установки пройден", "domain", g.o.FromDomain, "selector", pair.Selector)
			return nil
		}
		g.o.Logger.Info("страж DNS установки: ответа нет, попытка повторится", "checks", joinChecks(silent))
		if !end.After(g.o.Clock.Now()) {
			return deadlineErr(deadline, silent)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("страж DNS установки прерван: %w", ctx.Err())
		case <-g.o.Clock.After(retryPause):
		}
	}
}

func deadlineErr(deadline time.Duration, silent []Check) error {
	if len(silent) == 0 {
		silent = checks[:]
	}
	return fmt.Errorf("страж DNS установки: исчерпан срок старта %s, ответа нет по проверке %s", deadline,
		joinChecks(silent))
}

func joinChecks(cs []Check) string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = string(c)
	}
	return strings.Join(out, ", ")
}

// firstViolation — нарушенная проверка в порядке перечня.
func firstViolation(res map[Check]Result) (Check, Result, bool) {
	for _, c := range checks {
		if r, ok := res[c]; ok && r.Outcome == OutcomeViolated {
			return c, r, true
		}
	}
	return "", Result{}, false
}

// Run — перепроверка с интервалом по внедрённым часам до отмены ctx. Зовётся
// после успешного [Guard.Boot]. Остановить отправку у неё пути нет: нарушение
// — признак 0 и строка WARN.
func (g *Guard) Run(ctx context.Context, interval time.Duration) {
	if g.pair.Load() == nil {
		g.o.Logger.Error("перепроверка DNS установки не начата: страж старта не пройден")
		return
	}
	for {
		if ctx.Err() != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-g.o.Clock.After(interval):
		}
		g.recheck(ctx)
	}
}

// recheck — один такт: пара перечитывается функцией загрузчика, затем одна
// попытка. Исходы чтения пары — таблица §12а «Ключ и селектор DKIM».
func (g *Guard) recheck(ctx context.Context) {
	cur := *g.pair.Load()
	next, err := LoadPair(g.o.FS, g.o.KeyFile, g.o.SelectorFile)
	var kerr *dkimkey.Error
	switch {
	case err != nil && errors.As(err, &kerr) && kerr.Reason == dkimkey.ReasonUnreadable:
		g.o.Logger.Warn("том DKIM: поколение не читается; подпись остаётся на прежней паре",
			"check", string(CheckDKIM), "selector", cur.Selector)
		g.recheckWith(ctx, cur, []Check{CheckSPF, CheckDMARC})
	case err != nil:
		g.observe(CheckDKIM, false)
		g.warnPairRefused(cur, err.Error())
		g.recheckWith(ctx, cur, []Check{CheckSPF, CheckDMARC})
	case next.Digest == cur.Digest:
		g.recheckWith(ctx, cur, checks[:])
	default:
		g.adopt(ctx, cur, next)
	}
}

// adopt — пара в объекте сменилась: новая проходит проверку dkim по своей
// записи. Прошла — подпись атомарно переходит на неё; нет — остаётся на прежней.
func (g *Guard) adopt(ctx context.Context, cur, next dkimkey.Pair) {
	res := g.attempt(ctx, next, checks[:], queryTimeout)
	if ctx.Err() != nil {
		return
	}
	switch r := res[CheckDKIM]; r.Outcome {
	case OutcomeOK:
		g.pair.Store(&next)
		g.observe(CheckDKIM, true)
		g.o.Logger.Info("подпись перешла на селектор "+next.Selector, "selector", next.Selector)
	case OutcomeViolated:
		g.observe(CheckDKIM, false)
		g.warnPairRefused(cur, fmt.Sprintf("проверка %s нарушена: %s", CheckDKIM, r.Reason))
	case OutcomeNoAnswer:
	}
	g.apply(res, []Check{CheckSPF, CheckDMARC})
}

func (g *Guard) warnPairRefused(cur dkimkey.Pair, why string) {
	g.o.Logger.Warn("пара DKIM в объекте сменилась и не прошла проверку dkim: "+why+
		"; подпись остаётся на селекторе "+cur.Selector+
		"; перезапуск в этом состоянии остановит отправку: старт откажет по проверке dkim",
		"check", string(CheckDKIM))
}

// recheckWith — одна попытка перепроверки на паре pair по проверкам which.
func (g *Guard) recheckWith(ctx context.Context, pair dkimkey.Pair, which []Check) {
	res := g.attempt(ctx, pair, which, queryTimeout)
	if ctx.Err() != nil {
		return
	}
	g.apply(res, which)
}

// apply — исходы перепроверки в признак: ok — 1, нарушение — 0 и строка WARN,
// «ответа нет» — без изменений.
func (g *Guard) apply(res map[Check]Result, which []Check) {
	for _, c := range which {
		r, ok := res[c]
		if !ok {
			continue
		}
		switch r.Outcome {
		case OutcomeOK:
			g.observe(c, true)
		case OutcomeViolated:
			g.observe(c, false)
			g.o.Logger.Warn("перепроверка DNS установки: проверка "+string(c)+" нарушена",
				"check", string(c), "reason", string(r.Reason))
		case OutcomeNoAnswer:
		}
	}
}

// attempt — проверки which параллельно, каждая под сроком perQuery; нарушение
// любой отменяет остальные (они получают «ответа нет»).
func (g *Guard) attempt(ctx context.Context, pair dkimkey.Pair, which []Check, perQuery time.Duration) map[Check]Result {
	actx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make([]Result, len(which))
	var wg sync.WaitGroup
	for i, c := range which {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = g.check(actx, c, pair, perQuery)
			if results[i].Outcome == OutcomeViolated {
				cancel()
			}
		}()
	}
	wg.Wait()
	out := make(map[Check]Result, len(which))
	for i, c := range which {
		out[c] = results[i]
	}
	return out
}

// check — одна проверка: запрос под своим сроком, род ответа, разбор записи.
func (g *Guard) check(ctx context.Context, c Check, pair dkimkey.Pair, timeout time.Duration) Result {
	qctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	txts, err := g.r.LookupTXT(qctx, queryName(c, pair.Selector, g.o.FromDomain))
	switch classifyErr(err) {
	case AnswerGot:
	case AnswerNoRecord:
		txts = nil
	case AnswerNone:
		return Result{Outcome: OutcomeNoAnswer}
	}
	switch c {
	case CheckDKIM:
		return parseDKIM(txts, &pair.Key.PublicKey)
	case CheckSPF:
		return parseSPF(txts)
	case CheckDMARC:
		return parseDMARC(txts)
	}
	return Result{Outcome: OutcomeNoAnswer}
}
