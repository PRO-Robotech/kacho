// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package limits_test

// limits_integration_test.go — сетка на адресата и потолок потока на живой
// базе `kacho_notify` (полоса N7, kacho#2915; приёмка NTF1-H01…H03, H05, H09,
// H10; замысел З24, §6). Предмет каждой пробы — исход резерва испытуемого
// (`limits.Limiter`) и строки базы, которые он оставил; исход строки ленты
// (`DROPPED`/`DEFER`), SMTP и `Claim` — дело конвейера N3 по этому исходу
// (клетка 9 Р11), и здесь не переутверждаются.

import (
	"context"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PRO-Robotech/corelib/notify/feed"
	"github.com/PRO-Robotech/corelib/pgtest"

	"github.com/PRO-Robotech/kacho/services/notify/internal/limits"
)

// noon — сутки и час проб, далёкие от границы окна.
var noon = time.Date(2026, 10, 4, 12, 10, 0, 0, time.UTC)

// NTF1-H01 — сетка `security` на адресата: S резервов проходят, S+1-й —
// исчерпание сетки (клетка 9: `DROPPED(recipient_net)` у конвейера) и
// `notify_recipient_net_hits_total{class="security"}` +1. Близнец — при S−1
// резервах следующий проходит. Ограда, потолок и класс у обоих одни.
func TestLimits_NTF1H01_SecurityNetOverflowIsRefused(t *testing.T) {
	const s = 3
	pool := openPool(t, pgtest.NewDB(t))
	clk := newClock(noon)
	grid := gridWide
	grid.SecurityPerDay = s
	r := started(t, pool, keyOne, grid, clk)
	to := addr(t, "h01.recipient@example.invalid")
	ctx := context.Background()

	for i := range s - 1 {
		if _, err := r.lim.Reserve(ctx, row("kaname", feed.ClassSecurity, to)); err != nil {
			t.Fatalf("резерв %d из S−1 отвергнут: %v", i+1, err)
		}
	}
	// Близнец: при S−1 доставленных строка проходит.
	if _, err := r.lim.Reserve(ctx, row("kaname", feed.ClassSecurity, to)); err != nil {
		t.Fatalf("близнец: при S−1 резервах S-й отвергнут: %v", err)
	}
	before := counter(t, r.reg, "notify_recipient_net_hits_total", map[string]string{"class": "security"})
	_, err := r.lim.Reserve(ctx, row("kaname", feed.ClassSecurity, to))
	if !errors.Is(err, limits.ErrRecipientNetExhausted) {
		t.Fatalf("резерв сверх сетки security S=%d: ожидалось ErrRecipientNetExhausted, получено %v", s, err)
	}
	if errors.Is(err, limits.ErrRecipientKeySuperseded) || errors.Is(err, limits.ErrGlobalCeilingReached) {
		t.Fatalf("исчерпание сетки неотличимо от сторожа ограды или потолка: %v", err)
	}
	if got := counter(t, r.reg, "notify_recipient_net_hits_total", map[string]string{"class": "security"}); got != before+1 {
		t.Fatalf("notify_recipient_net_hits_total{class=security}: %v → %v, ожидалось +1", before, got)
	}
	if got := sumCount(t, pool, feed.ClassSecurity); got != s {
		t.Fatalf("счётчик сетки security в базе %d, ожидалось %d — отвергнутый резерв оставил вклад", got, s)
	}
}

// NTF1-H02 — сетка `notice` в час: сверх — исчерпание с моментом освобождения
// окна (по нему конвейер выбирает DEFER или EXPIRED по сроку строки); после
// освобождения окна (управляемые часы) — резерв проходит. Близнец — при N−1
// резервах N-й проходит сразу.
func TestLimits_NTF1H02_NoticeHourNetFreesWithTheWindow(t *testing.T) {
	const n = 3
	pool := openPool(t, pgtest.NewDB(t))
	clk := newClock(noon)
	grid := gridWide
	grid.NoticePerHour = n
	r := started(t, pool, keyOne, grid, clk)
	to := addr(t, "h02.recipient@example.invalid")
	ctx := context.Background()

	for i := range n {
		if _, err := r.lim.Reserve(ctx, row("probe", feed.ClassNotice, to)); err != nil {
			t.Fatalf("резерв %d из N=%d отвергнут (близнец: при N−1 следующий проходит): %v", i+1, n, err)
		}
	}
	_, err := r.lim.Reserve(ctx, row("probe", feed.ClassNotice, to))
	var ex *limits.ExhaustedError
	if !errors.As(err, &ex) || !errors.Is(err, limits.ErrRecipientNetExhausted) {
		t.Fatalf("резерв сверх сетки notice N=%d в час: ожидалось *ExhaustedError, получено %v", n, err)
	}
	if want := noon.Truncate(time.Hour).Add(time.Hour); !ex.FreeAt.Equal(want) || ex.Class != feed.ClassNotice {
		t.Fatalf("исчерпание называет класс %q и освобождение %s, ожидалось notice и %s (конец часового окна)",
			ex.Class, ex.FreeAt, want)
	}
	clk.Set(noon.Truncate(time.Hour).Add(time.Hour))
	if _, err := r.lim.Reserve(ctx, row("probe", feed.ClassNotice, to)); err != nil {
		t.Fatalf("окно часа освобождено (часы %s), а резерв отвергнут: %v", clk.Now(), err)
	}
}

// NTF1-H02, суточное окно `notice` и атомарность двух окон (CX1-15): сутки
// исчерпаны при свободном часе — исчерпание с освобождением в полночь UTC, и
// строка часа вклада не получила (оба окна — одна транзакция).
func TestLimits_NTF1H02_NoticeDayNetRollsBackTheHourWindow(t *testing.T) {
	pool := openPool(t, pgtest.NewDB(t))
	clk := newClock(noon)
	grid := gridWide
	grid.NoticePerDay = 2
	r := started(t, pool, keyOne, grid, clk)
	to := addr(t, "h02.day@example.invalid")
	ctx := context.Background()
	for range 2 {
		if _, err := r.lim.Reserve(ctx, row("probe", feed.ClassNotice, to)); err != nil {
			t.Fatalf("резерв в пределах суток отвергнут: %v", err)
		}
	}
	_, err := r.lim.Reserve(ctx, row("probe", feed.ClassNotice, to))
	var ex *limits.ExhaustedError
	if !errors.As(err, &ex) {
		t.Fatalf("сутки notice исчерпаны: ожидалось *ExhaustedError, получено %v", err)
	}
	if want := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC); !ex.FreeAt.Equal(want) {
		t.Fatalf("освобождение суточного окна %s, ожидалось %s", ex.FreeAt, want)
	}
	// Два окна по два резерва: 4. Третий резерв часа, не откатившийся с отказом суток, дал бы 5.
	if got := sumCount(t, pool, feed.ClassNotice); got != 4 {
		t.Fatalf("сумма счётчиков notice %d, ожидалось 4 — отказ одного окна не откатил другое", got)
	}
}

// NTF1-H03 — сетка точна поперёк реплик: две реплики (один ключ, своя
// ограда у каждой, общая база) параллельно резервируют 2S строк `security` на
// один адрес → прошло ровно S, исчерпание — ровно S, счётчик в базе = S.
// Близнец — та же нагрузка последовательно даёт то же. В базе нет адреса ни
// открытым текстом, ни иной обратимой формой; ключ строки — HMAC (Р10).
func TestLimits_NTF1H03_NetIsExactAcrossReplicas(t *testing.T) {
	const s = 6
	for _, parallel := range []bool{true, false} {
		name := map[bool]string{true: "параллельно", false: "последовательно (близнец)"}[parallel]
		t.Run(name, func(t *testing.T) {
			pool := openPool(t, pgtest.NewDB(t))
			clk := newClock(noon)
			grid := gridWide
			grid.SecurityPerDay = s
			reps := []*replica{started(t, pool, keyOne, grid, clk), started(t, pool, keyOne, grid, clk)}
			const raw = "h03.recipient@example.invalid"
			to := addr(t, raw)

			var (
				mu            sync.Mutex
				ok, exhausted int
				other         []error
				wg            sync.WaitGroup
				gate          = make(chan struct{})
			)
			one := func(r *replica) {
				_, err := r.lim.Reserve(context.Background(), row("kaname", feed.ClassSecurity, to))
				mu.Lock()
				defer mu.Unlock()
				switch {
				case err == nil:
					ok++
				case errors.Is(err, limits.ErrRecipientNetExhausted):
					exhausted++
				default:
					other = append(other, err)
				}
			}
			for i := range 2 * s {
				r := reps[i%2]
				if parallel {
					wg.Add(1)
					go func() { defer wg.Done(); <-gate; one(r) }()
				} else {
					one(r)
				}
			}
			close(gate)
			wg.Wait()
			if len(other) > 0 {
				t.Fatalf("резервы вернули ошибки вне исходов сетки: %v", other)
			}
			if ok != s || exhausted != s {
				t.Fatalf("прошло %d, исчерпание %d из %d; ожидалось %d и %d", ok, exhausted, 2*s, s, s)
			}
			if got := sumCount(t, pool, feed.ClassSecurity); got != s {
				t.Fatalf("счётчик сетки в kacho_notify %d, ожидалось %d", got, s)
			}
			var keyOK bool
			if err := pool.QueryRow(context.Background(),
				`SELECT bool_and(key = $1) FROM recipient_net`, netKey(t, keyOne, to)).Scan(&keyOK); err != nil || !keyOK {
				t.Fatalf("ключ строки сетки не HMAC-SHA256(ключ сетки, адрес) (совпадение %v, %v)", keyOK, err)
			}
			requireNoAddressInTables(t, pool, raw)
		})
	}
}

// requireNoAddressInTables — выгрузка таблиц kacho_notify не содержит
// посеянного адреса ни открытым текстом, ни локальной частью, ни hex адреса.
// Ключ сетки (HMAC) в своей колонке законен и здесь не ищется.
func requireNoAddressInTables(t *testing.T, pool *pgxpool.Pool, raw string) {
	t.Helper()
	local, _, _ := strings.Cut(raw, "@")
	forms := []string{raw, local, hex.EncodeToString([]byte(raw))}
	rowsSeen := 0
	for _, table := range []string{"recipient_net", "global_daily", "recipient_key_fence"} {
		rows, err := pool.Query(context.Background(), `SELECT row_to_json(t)::text FROM `+table+` t`)
		if err != nil {
			t.Fatalf("выгрузка %s не прочитана: %v", table, err)
		}
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				t.Fatal(err)
			}
			rowsSeen++
			for _, f := range forms {
				if strings.Contains(strings.ToLower(line), strings.ToLower(f)) {
					t.Errorf("выгрузка %s несёт посеянный адрес в форме %q: %s", table, f, line)
				}
			}
		}
		rows.Close()
	}
	if rowsSeen == 0 {
		t.Fatal("выгрузка таблиц пуста: «адреса нет» на пустой базе не утверждает ничего")
	}
}

// NTF1-H05 — суточный потолок потока P: после P резервов потолок достигнут —
// `notice` отвергается сторожем потолка (+1 `notify_global_ceiling_hits_total`),
// `security` проходит. Близнец — при P−1 потолок не достигнут и `notice`
// проходит. Какие классы забирает `Claim`, судит SourceGate.Classes по этому
// признаку (source_test.go).
func TestLimits_NTF1H05_GlobalCeilingLeavesOnlySecurity(t *testing.T) {
	const p = 3
	pool := openPool(t, pgtest.NewDB(t))
	clk := newClock(noon)
	grid := gridWide
	grid.GlobalPerDay = p
	r := started(t, pool, keyOne, grid, clk)
	ctx := context.Background()
	for i, a := range []string{"h05.a@example.invalid", "h05.b@example.invalid", "h05.c@example.invalid"} {
		if i == p-1 {
			reached, err := r.lim.CeilingReached(ctx)
			if err != nil || reached {
				t.Fatalf("близнец: при P−1=%d потолок назван достигнутым (%v, %v)", p-1, reached, err)
			}
		}
		if _, err := r.lim.Reserve(ctx, row("probe", feed.ClassNotice, addr(t, a))); err != nil {
			t.Fatalf("резерв %d ниже потолка P=%d отвергнут: %v", i+1, p, err)
		}
	}
	reached, err := r.lim.CeilingReached(ctx)
	if err != nil || !reached {
		t.Fatalf("за сутки отправлено P=%d, а потолок не достигнут (%v, %v)", p, reached, err)
	}
	if got := globalCount(t, pool, noon); got != p {
		t.Fatalf("global_daily за %s = %d, ожидалось %d", noon.Format("2006-01-02"), got, p)
	}
	before := counter(t, r.reg, "notify_global_ceiling_hits_total", nil)
	_, err = r.lim.Reserve(ctx, row("probe", feed.ClassNotice, addr(t, "h05.d@example.invalid")))
	if !errors.Is(err, limits.ErrGlobalCeilingReached) {
		t.Fatalf("notice при достигнутом потолке: ожидалось ErrGlobalCeilingReached, получено %v", err)
	}
	if got := counter(t, r.reg, "notify_global_ceiling_hits_total", nil); got != before+1 {
		t.Fatalf("notify_global_ceiling_hits_total: %v → %v, ожидалось +1", before, got)
	}
	if _, err := r.lim.Reserve(ctx, row("kaname", feed.ClassSecurity, addr(t, "h05.sec@example.invalid"))); err != nil {
		t.Fatalf("security выше потолка потока отвергнут: %v", err)
	}
}

// NTF1-H10 — сетка `notice` поперёк источников: k строк из `kaname` и N−k из
// `probe` на один адрес исчерпывают сетку N, и строка счётчика у адреса одна
// на оба источника. Близнец — k−1 из `kaname` (всего N−1): строка проходит.
func TestLimits_NTF1H10_NoticeNetIsSharedAcrossSources(t *testing.T) {
	const n, k = 4, 2
	for _, twin := range []bool{false, true} {
		name := map[bool]string{false: "N поперёк источников", true: "близнец N−1"}[twin]
		t.Run(name, func(t *testing.T) {
			pool := openPool(t, pgtest.NewDB(t))
			clk := newClock(noon)
			grid := gridWide
			grid.NoticePerHour = n
			r := started(t, pool, keyOne, grid, clk)
			to := addr(t, "h10.recipient@example.invalid")
			ctx := context.Background()
			fromKaname := k
			if twin {
				fromKaname = k - 1
			}
			for range fromKaname {
				if _, err := r.lim.Reserve(ctx, row("kaname", feed.ClassNotice, to)); err != nil {
					t.Fatalf("резерв из kaname отвергнут: %v", err)
				}
			}
			for range n - k {
				if _, err := r.lim.Reserve(ctx, row("probe", feed.ClassNotice, to)); err != nil {
					t.Fatalf("резерв из probe отвергнут: %v", err)
				}
			}
			_, err := r.lim.Reserve(ctx, row("probe", feed.ClassNotice, to))
			if twin {
				if err != nil {
					t.Fatalf("близнец: всего N−1 поперёк источников, а строка отвергнута: %v", err)
				}
				return
			}
			if !errors.Is(err, limits.ErrRecipientNetExhausted) {
				t.Fatalf("N=%d писем поперёк kaname и probe, а резерв из probe не исчерпан: %v", n, err)
			}
			var rowsOfAddr int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM recipient_net WHERE key = $1 AND class = 'notice'`,
				netKey(t, keyOne, to)).Scan(&rowsOfAddr); err != nil {
				t.Fatal(err)
			}
			// Окон notice два (час, сутки): строк адреса две, а не по паре на источник.
			if rowsOfAddr != 2 {
				t.Fatalf("строк сетки notice у адреса %d, ожидалось 2 (час и сутки, одни на оба источника)", rowsOfAddr)
			}
		})
	}
}

// NTF1-H09 — метрики лимитов без PII: после срабатываний сетки, потолка, ведра
// и паузы с посеянными адресами реестр несёт все четыре семейства приёмки, и
// ни одно имя, метка или значение метки не содержит адреса, его локальной
// части, hex адреса, hex или base64 ключа сетки адреса.
func TestLimits_NTF1H09_LimitMetricsCarryNoPII(t *testing.T) {
	pool := openPool(t, pgtest.NewDB(t))
	clk := newClock(noon)
	grid := gridWide
	grid.SecurityPerDay, grid.NoticePerHour, grid.GlobalPerDay = 1, 1, 3
	r := started(t, pool, keyOne, grid, clk)
	ctx := context.Background()
	const secRaw, noticeRaw = "h09.security@example.invalid", "h09.notice@example.invalid"
	for range 2 {
		_, _ = r.lim.Reserve(ctx, row("kaname", feed.ClassSecurity, addr(t, secRaw)))
		_, _ = r.lim.Reserve(ctx, row("probe", feed.ClassNotice, addr(t, noticeRaw)))
	}
	_, _ = r.lim.Reserve(ctx, row("probe", feed.ClassNotice, addr(t, "h09.c1@example.invalid")))
	_, _ = r.lim.Reserve(ctx, row("probe", feed.ClassNotice, addr(t, "h09.c2@example.invalid")))

	gate, err := limits.NewSourceGate("probe", limits.SourceLimits{Rate: 1, Burst: 1, Paused: true}, clk.Now, r.reg)
	if err != nil {
		t.Fatalf("ворота источника не собраны на годных ручках: %v", err)
	}
	_ = gate.Take(5)
	_ = gate.Classes(false)

	fams := families(t, r.reg)
	for _, name := range []string{"notify_recipient_net_hits_total", "notify_source_throttled_total",
		"notify_global_ceiling_hits_total", "notify_source_paused"} {
		if !fams[name] {
			t.Errorf("семейства %s в реестре нет (есть %d семейств)", name, len(fams))
		}
	}
	mfs, err := r.reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	forms := append(piiForms(t, keyOne, secRaw), piiForms(t, keyOne, noticeRaw)...)
	labels := 0
	for _, mf := range mfs {
		for _, m := range mf.GetMetric() {
			for _, lp := range m.GetLabel() {
				labels++
				for _, f := range forms {
					if strings.Contains(lp.GetName(), f) || strings.Contains(lp.GetValue(), f) {
						t.Errorf("метрика %s несёт посеянный адрес в форме %q: %s=%s", mf.GetName(), f, lp.GetName(), lp.GetValue())
					}
				}
			}
		}
	}
	if labels == 0 {
		t.Fatal("у метрик ни одной метки: «PII нет» на пустом обходе не утверждает ничего")
	}
}

// УК30 — освобождение резерва однократно: повтор освобождения того же резерва
// (повтор `Ack` после записанного исхода) вклада второй раз не снимает.
// Близнец — второй резерв того же адреса освобождается своим вызовом.
func TestLimits_UK30_RepeatedReleaseReturnsOnce(t *testing.T) {
	pool := openPool(t, pgtest.NewDB(t))
	clk := newClock(noon)
	r := started(t, pool, keyOne, gridWide, clk)
	to := addr(t, "uk30.recipient@example.invalid")
	ctx := context.Background()
	first, err := r.lim.Reserve(ctx, row("probe", feed.ClassNotice, to))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.lim.Reserve(ctx, row("probe", feed.ClassNotice, to)); err != nil {
		t.Fatal(err)
	}
	for i := range 2 {
		if err := r.lim.Release(ctx, first); err != nil {
			t.Fatalf("освобождение %d того же резерва — ошибка: %v", i+1, err)
		}
	}
	// Два окна notice: после одного освобождения в каждом по 1 — сумма 2.
	if got := sumCount(t, pool, feed.ClassNotice); got != 2 {
		t.Fatalf("после двойного освобождения одного резерва сумма notice %d, ожидалось 2 — вклад снят дважды", got)
	}
	if got := globalCount(t, pool, noon); got != 1 {
		t.Fatalf("global_daily после двойного освобождения %d, ожидалось 1", got)
	}
}

// УК15, УК28 — резерв после DEFER возвращён: при S−1 доставленных и одном
// освобождённом резерве следующая строка проходит. Близнец — без освобождения
// та же строка исчерпывает сетку.
func TestLimits_UK15_ReleasedReserveFreesTheNet(t *testing.T) {
	const s = 2
	for _, release := range []bool{true, false} {
		name := map[bool]string{true: "освобождён", false: "близнец без освобождения"}[release]
		t.Run(name, func(t *testing.T) {
			pool := openPool(t, pgtest.NewDB(t))
			clk := newClock(noon)
			grid := gridWide
			grid.SecurityPerDay = s
			r := started(t, pool, keyOne, grid, clk)
			to := addr(t, "uk15.recipient@example.invalid")
			ctx := context.Background()
			if _, err := r.lim.Reserve(ctx, row("kaname", feed.ClassSecurity, to)); err != nil {
				t.Fatal(err)
			}
			deferred, err := r.lim.Reserve(ctx, row("kaname", feed.ClassSecurity, to))
			if err != nil {
				t.Fatal(err)
			}
			if release {
				if err := r.lim.Release(ctx, deferred); err != nil {
					t.Fatalf("освобождение резерва отложенной строки: %v", err)
				}
			}
			_, err = r.lim.Reserve(ctx, row("kaname", feed.ClassSecurity, to))
			if release && err != nil {
				t.Fatalf("резерв возвращён, при S−1 строка обязана пройти: %v", err)
			}
			if !release && !errors.Is(err, limits.ErrRecipientNetExhausted) {
				t.Fatalf("близнец: без освобождения сетка S=%d исчерпана, а резерв прошёл (%v)", s, err)
			}
		})
	}
}

// SDR-Н3 — резерв и освобождение одного адресата под конкуренцией не дают
// взаимной блокировки (40P01): замки строк в одном порядке. Счётчики после
// нагрузки = резервы − освобождения в каждом окне и в потолке.
func TestReleaseConcurrentWithReserveNoDeadlock(t *testing.T) {
	const workers, rounds = 6, 40
	pool := openPool(t, pgtest.NewDB(t))
	clk := newClock(noon)
	r := started(t, pool, keyOne, gridWide, clk)
	to := addr(t, "sdrn3.recipient@example.invalid")
	var (
		mu                 sync.Mutex
		reserved, released int
		errs               []error
		wg                 sync.WaitGroup
	)
	gate := make(chan struct{})
	for w := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-gate
			ctx := context.Background()
			for i := range rounds {
				res, err := r.lim.Reserve(ctx, row("probe", feed.ClassNotice, to))
				mu.Lock()
				if err != nil {
					errs = append(errs, err)
					mu.Unlock()
					continue
				}
				reserved++
				mu.Unlock()
				if (i+w)%2 == 0 {
					err := r.lim.Release(ctx, res)
					mu.Lock()
					if err != nil {
						errs = append(errs, err)
					} else {
						released++
					}
					mu.Unlock()
				}
			}
		}()
	}
	close(gate)
	wg.Wait()
	for _, err := range errs {
		var pg *pgconn.PgError
		if errors.As(err, &pg) && pg.Code == "40P01" {
			t.Fatalf("взаимная блокировка резерва и освобождения: %v", err)
		}
	}
	if len(errs) > 0 {
		t.Fatalf("резерв/освобождение под конкуренцией вернули %d ошибок, первая: %v", len(errs), errs[0])
	}
	want := reserved - released
	if got := sumCount(t, pool, feed.ClassNotice); got != 2*want {
		t.Fatalf("сумма notice %d при резервах %d и освобождениях %d (ожидалось 2×%d — два окна)", got, reserved, released, want)
	}
	if got := globalCount(t, pool, noon); got != want {
		t.Fatalf("global_daily %d, ожидалось %d", got, want)
	}
	t.Logf("резервов %d, освобождений %d, 40P01 — 0", reserved, released)
}

// SDR-Н3 — освобождение берёт ключи резерва: резерв в 23:59:59 UTC,
// освобождение после полуночи уменьшает строки дня и часа резерва; строк
// нового дня с вкладом нет, 23514 нет.
func TestReleaseAcrossMidnightUsesReserveKeys(t *testing.T) {
	pool := openPool(t, pgtest.NewDB(t))
	before := time.Date(2026, 10, 4, 23, 59, 59, 0, time.UTC)
	clk := newClock(before)
	r := started(t, pool, keyOne, gridWide, clk)
	ctx := context.Background()
	res, err := r.lim.Reserve(ctx, row("probe", feed.ClassNotice, addr(t, "midnight@example.invalid")))
	if err != nil {
		t.Fatal(err)
	}
	clk.Set(before.Add(2 * time.Second))
	if err := r.lim.Release(ctx, res); err != nil {
		t.Fatalf("освобождение после полуночи: %v", err)
	}
	var withContribution int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM recipient_net WHERE count > 0`).Scan(&withContribution); err != nil {
		t.Fatal(err)
	}
	if withContribution != 0 {
		t.Fatalf("после освобождения строк сетки с вкладом %d — уменьшены не строки резерва", withContribution)
	}
	if got := globalCount(t, pool, before); got != 0 {
		t.Fatalf("global_daily дня резерва %d, ожидалось 0", got)
	}
	if got := globalCount(t, pool, before.Add(2*time.Second)); got != 0 {
		t.Fatalf("global_daily нового дня %d, ожидалось 0", got)
	}
}

// SDR-Н1 — контекст освобождения собственный: крайний момент строки истёк к
// началу освобождения, а вклад всё равно возвращён.
func TestReleaseAfterRowDeadlineReturnsReserve(t *testing.T) {
	pool := openPool(t, pgtest.NewDB(t))
	clk := newClock(noon)
	r := started(t, pool, keyOne, gridWide, clk)
	res, err := r.lim.Reserve(context.Background(), row("probe", feed.ClassNotice, addr(t, "sdrn1@example.invalid")))
	if err != nil {
		t.Fatal(err)
	}
	rowCtx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	if err := r.lim.Release(rowCtx, res); err != nil {
		t.Fatalf("освобождение на истёкшем крайнем моменте строки: %v", err)
	}
	if got := sumCount(t, pool, feed.ClassNotice); got != 0 {
		t.Fatalf("вклад не возвращён: сумма notice %d", got)
	}
}

// CX1-68 (б) — ошибка базы в транзакции резерва не сторож: не исчерпание, не
// потолок, не замена ключа; резерва нет (транзакция откатилась), и
// `notify_reserve_db_errors_total` +1. Близнец — та же база после возврата
// таблицы: резерв проходит.
func TestLimits_CX168b_ReserveDBErrorIsNotTheGuard(t *testing.T) {
	pool := openPool(t, pgtest.NewDB(t))
	clk := newClock(noon)
	r := started(t, pool, keyOne, gridWide, clk)
	ctx := context.Background()
	to := addr(t, "dberr@example.invalid")
	if _, err := pool.Exec(ctx, `ALTER TABLE global_daily RENAME TO global_daily_moved`); err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: условие «ошибка базы» не создано: %v", err)
	}
	before := counter(t, r.reg, "notify_reserve_db_errors_total", nil)
	_, err := r.lim.Reserve(ctx, row("probe", feed.ClassNotice, to))
	if err == nil {
		t.Fatal("резерв без таблицы потолка прошёл")
	}
	for _, guard := range []error{limits.ErrRecipientKeySuperseded, limits.ErrRecipientNetExhausted, limits.ErrGlobalCeilingReached} {
		if errors.Is(err, guard) {
			t.Fatalf("ошибка базы выдана за исход сетки %v: %v", guard, err)
		}
	}
	if got := counter(t, r.reg, "notify_reserve_db_errors_total", nil); got != before+1 {
		t.Fatalf("notify_reserve_db_errors_total: %v → %v, ожидалось +1", before, got)
	}
	if got := sumCount(t, pool, feed.ClassNotice); got != 0 {
		t.Fatalf("резерв с ошибкой базы оставил вклад в сетке: %d", got)
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE global_daily_moved RENAME TO global_daily`); err != nil {
		t.Fatal(err)
	}
	if _, err := r.lim.Reserve(ctx, row("probe", feed.ClassNotice, to)); err != nil {
		t.Fatalf("близнец: таблица на месте, а резерв отвергнут: %v", err)
	}
}

// SDR-К1, предел оператора — посторонний держатель строки `global_daily`
// дольше TxStatementTimeout: резерв завершается ошибкой базы не позже предела
// (а не ждёт держателя), это не сторож, `notify_reserve_db_errors_total` +1.
// Близнец — держатель отпускает строку быстро: резерв проходит.
func TestLimits_SDRK1_ReserveWaitIsBoundedByStatementTimeout(t *testing.T) {
	for _, long := range []bool{true, false} {
		name := map[bool]string{true: "держатель дольше предела", false: "близнец: держатель короткий"}[long]
		t.Run(name, func(t *testing.T) {
			pool := openPool(t, pgtest.NewDB(t))
			clk := newClock(noon)
			r := started(t, pool, keyOne, gridWide, clk)
			ctx := context.Background()
			if _, err := r.lim.Reserve(ctx, row("probe", feed.ClassNotice, addr(t, "seed@example.invalid"))); err != nil {
				t.Fatalf("посев строки потолка: %v", err)
			}
			holder, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = holder.Rollback(ctx) }()
			if _, err := holder.Exec(ctx, `SELECT count FROM global_daily FOR UPDATE`); err != nil {
				t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: держатель строки потолка не взял замок: %v", err)
			}
			hold := limits.TxStatementTimeout + 5*time.Second
			if !long {
				hold = 300 * time.Millisecond
			}
			go func() { time.Sleep(hold); _ = holder.Rollback(ctx) }()

			before := counter(t, r.reg, "notify_reserve_db_errors_total", nil)
			start := time.Now()
			_, err = r.lim.Reserve(ctx, row("probe", feed.ClassNotice, addr(t, "sdrk1@example.invalid")))
			took := time.Since(start)
			if !long {
				if err != nil {
					t.Fatalf("близнец: держатель отпустил строку через %s, а резерв отвергнут: %v", hold, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("резерв дождался держателя (%s) вместо отказа по пределу %s", took, limits.TxStatementTimeout)
			}
			if took > limits.TxStatementTimeout+2*time.Second {
				t.Fatalf("резерв ждал %s при пределе оператора %s — предела у транзакции резерва нет", took, limits.TxStatementTimeout)
			}
			for _, guard := range []error{limits.ErrRecipientKeySuperseded, limits.ErrRecipientNetExhausted, limits.ErrGlobalCeilingReached} {
				if errors.Is(err, guard) {
					t.Fatalf("отказ по пределу выдан за исход сетки %v: %v", guard, err)
				}
			}
			if got := counter(t, r.reg, "notify_reserve_db_errors_total", nil); got != before+1 {
				t.Fatalf("notify_reserve_db_errors_total: %v → %v, ожидалось +1", before, got)
			}
		})
	}
}

// SDR-К1, предел простоя — держатель транзакции резерва после всех операторов
// молчит при открытом соединении (COMMIT не доходит до сервера): соседний
// резерв того же адреса коммитится не позже TxIdleTimeout + TxStatementTimeout,
// и новая реплика пишет ограду. Положительный близнец — держатель доводит
// транзакцию: соседний резерв без ожидания предела.
func TestReserveHolderVanishedDoesNotStallFleet(t *testing.T) {
	for _, vanish := range []bool{true, false} {
		name := map[bool]string{true: "держатель молчит", false: "близнец: держатель коммитит"}[vanish]
		t.Run(name, func(t *testing.T) {
			dsn := pgtest.NewDB(t)
			pool := openPool(t, dsn)
			clk := newClock(noon)
			proxy := newCommitSwallower(t, upstreamOf(t, dsn), vanish)
			holderPool := poolVia(t, dsn, proxy)
			// Ограду пишет соседняя реплика напрямую: прокси глотает и её COMMIT.
			neighbour := started(t, pool, keyOne, gridWide, clk)
			holder := newReplica(t, holderPool, keyOne, gridWide, clk)
			to := addr(t, "vanished@example.invalid")

			holderDone := make(chan error, 1)
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				_, err := holder.lim.Reserve(ctx, row("probe", feed.ClassNotice, to))
				holderDone <- err
			}()
			if vanish {
				// Условие: сеанс держателя «idle in transaction» с замками.
				deadline := time.Now().Add(10 * time.Second)
				for {
					var idle int
					if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM pg_stat_activity
						WHERE datname = current_database() AND state = 'idle in transaction'`).Scan(&idle); err != nil {
						t.Fatal(err)
					}
					if idle > 0 {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("НЕ ВЫПОЛНИЛОСЬ: держатель не встал в «idle in transaction» — условие пробы не создано")
					}
					pollPause()
				}
			} else if err := <-holderDone; err != nil {
				t.Fatalf("близнец: резерв держателя через прокси без проглатывания отвергнут: %v", err)
			}

			start := time.Now()
			ctx, cancel := context.WithTimeout(context.Background(), limits.TxIdleTimeout+limits.TxStatementTimeout+5*time.Second)
			defer cancel()
			_, err := neighbour.lim.Reserve(ctx, row("probe", feed.ClassNotice, to))
			took := time.Since(start)
			if err != nil {
				t.Fatalf("соседний резерв не закоммичен за %s: %v", took, err)
			}
			bound := limits.TxIdleTimeout + limits.TxStatementTimeout
			if !vanish {
				bound = time.Second
			}
			if took > bound {
				t.Fatalf("соседний резерв ждал %s, предел %s", took, bound)
			}
			fresh := newReplica(t, pool, keyOne, gridWide, clk)
			if err := fresh.lim.WriteFence(context.Background()); err != nil {
				t.Fatalf("после ухода держателя старт новой реплики не записал ограду: %v", err)
			}
			t.Logf("соседний резерв за %s (держатель молчит: %v)", took, vanish)
		})
	}
}
