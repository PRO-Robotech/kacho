// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package limits

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/PRO-Robotech/corelib/notify/feed"
)

// Границы ручек на источник (§8): `notify.sourceLimits.<модуль>.rate` и
// `.burst`.
const (
	SourceRateMin  = 1
	SourceRateMax  = 1000
	SourceBurstMin = 1
	SourceBurstMax = 10000
)

// SourceLimits — ручки одного источника: темп ведра (строк в секунду), его
// ёмкость и пауза.
type SourceLimits struct {
	Rate   int
	Burst  int
	Paused bool
}

// RateBound и BurstBound — границы полей [SourceLimits].
func RateBound() Bound  { return Bound{Knob: "rate", Min: SourceRateMin, Max: SourceRateMax} }
func BurstBound() Bound { return Bound{Knob: "burst", Min: SourceBurstMin, Max: SourceBurstMax} }

// Validate судит поля по границам; находки — все разом, с именем поля.
func (s SourceLimits) Validate() error {
	var bad []string
	if b := RateBound(); !b.Contains(s.Rate) {
		bad = append(bad, fmt.Sprintf("rate = %d вне границы %s", s.Rate, b))
	}
	if b := BurstBound(); !b.Contains(s.Burst) {
		bad = append(bad, fmt.Sprintf("burst = %d вне границы %s", s.Burst, b))
	}
	if len(bad) > 0 {
		return errors.New(strings.Join(bad, "; "))
	}
	return nil
}

// SourceGate — ведро и пауза одного источника в памяти реплики (NTF1-H04,
// NTF1-H06). Ведро ограничивает размер `Claim` и строк не теряет: невыданное
// остаётся в ленте источника до следующего такта.
type SourceGate struct {
	module string
	limits SourceLimits
	now    func() time.Time

	throttled prometheus.Counter

	mu     sync.Mutex
	tokens float64
	last   time.Time
}

// NewSourceGate собирает ворота источника module. Ручки вне границы §8 —
// отказ. Метрики `notify_source_throttled_total{source}` и
// `notify_source_paused{source}` регистрируются в reg; вторые ворота на том же
// реестре берут уже зарегистрированное семейство.
func NewSourceGate(module string, sl SourceLimits, now func() time.Time, reg prometheus.Registerer) (*SourceGate, error) {
	if strings.TrimSpace(module) == "" {
		return nil, errors.New("ворота источника: имя модуля пусто")
	}
	if now == nil {
		return nil, errors.New("ворота источника: часы не заданы")
	}
	if reg == nil {
		return nil, errors.New("ворота источника: реестр метрик не задан")
	}
	if err := sl.Validate(); err != nil {
		return nil, fmt.Errorf("ворота источника %s: %w", module, err)
	}
	throttled, err := registerVec(reg, prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "notify_source_throttled_total",
		Help: "Claims of a source cut by its bucket (rate, burst): rows stay in the source feed until a later tick.",
	}, []string{"source"}))
	if err != nil {
		return nil, err
	}
	paused, err := registerVec(reg, prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "notify_source_paused",
		Help: "1 when the source is paused by notify.sourceLimits.<module>.paused (only security is claimed), else 0.",
	}, []string{"source"}))
	if err != nil {
		return nil, err
	}
	g := &SourceGate{
		module:    module,
		limits:    sl,
		now:       now,
		throttled: throttled.WithLabelValues(module),
		tokens:    float64(sl.Burst),
		last:      now(),
	}
	pausedValue := 0.0
	if sl.Paused {
		pausedValue = 1
	}
	paused.WithLabelValues(module).Set(pausedValue)
	return g, nil
}

// Classes — классы, которые забирает `Claim` источника: на паузе источника
// или при достигнутом суточном потолке потока — только security, иначе
// классы сети ([NetworkClasses]) (NTF1-H05, NTF1-H06). Пауза одного источника
// других не трогает.
func (g *SourceGate) Classes(ceilingReached bool) []feed.Class {
	if g.limits.Paused || ceilingReached {
		return []feed.Class{feed.ClassSecurity}
	}
	return NetworkClasses()
}

// Take — сколько из n строк источник выдаёт сейчас по ведру: не больше n и
// не больше накопленного. Урезанный запрос — +1
// `notify_source_throttled_total{source}`.
func (g *SourceGate) Take(n int) int {
	if n <= 0 {
		return 0
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.now()
	if elapsed := now.Sub(g.last); elapsed > 0 {
		g.tokens = math.Min(float64(g.limits.Burst), g.tokens+elapsed.Seconds()*float64(g.limits.Rate))
		g.last = now
	}
	got := min(n, int(math.Floor(g.tokens)))
	g.tokens -= float64(got)
	if got < n {
		g.throttled.Inc()
	}
	return got
}

// registerVec регистрирует семейство; уже зарегистрированное тем же описанием
// возвращается как есть — несколько ворот и реплик делят один реестр.
func registerVec[C prometheus.Collector](reg prometheus.Registerer, c C) (C, error) {
	if err := reg.Register(c); err != nil {
		var are prometheus.AlreadyRegisteredError
		if errors.As(err, &are) {
			if existing, ok := are.ExistingCollector.(C); ok {
				return existing, nil
			}
		}
		var zero C
		return zero, fmt.Errorf("регистрация метрики: %w", err)
	}
	return c, nil
}

// NetworkClasses — классы, которые источник отдаёт notify по сети: закрытый
// перечень ленты без классов только процесса (feed.LocalOnlyClasses —
// obligation, NTF-5 Р12). Строку такого класса берёт только точка входа ленты
// в процессе её владельца; Claim сервера ленты её не выдаёт, и в контракте
// corelib.notify класса нет. Перечень выводится из двух перечней ленты, а не
// выписывается: класс, который лента добавит, попадает сюда либо в классы
// только процесса без правки notify.
func NetworkClasses() []feed.Class {
	local := feed.LocalOnlyClasses()
	out := make([]feed.Class, 0, len(feed.Classes()))
	for _, c := range feed.Classes() {
		if !slices.Contains(local, c) {
			out = append(out, c)
		}
	}
	return out
}
