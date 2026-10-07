// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package dnscheck_test

// guard_integration_test.go — собственный страж старта notify по DNS установки
// и периодическая перепроверка (полоса N14; приёмка NTF-1 S6: NTF1-P01
// (integration), P04…P11, P15…P19; замысел §12а «Резолвер и срок запроса»,
// «Форма попытки», «Смена ключа DKIM», «Ручки и признак»).
//
// Уровень — integration: зона DNS испытания (zone_test.go) — настоящий UDP-сервер,
// резолвер — стандартный Go. Часы — управляемые (порт часов стража): срок
// старта, пауза между попытками и интервал перепроверки идут по ним; срок
// одного запроса DNS — по реальному времени (контекст), как в бою.
//
// Контракт испытуемого (имена — §12а; форма опций — этой пробы):
//
//	type Clock interface {
//	    Now() time.Time
//	    After(d time.Duration) <-chan time.Time
//	}
//	type Options struct {
//	    FromDomain   string               // домен From = домен адреса возврата в NTF-1 (Д101)
//	    KeyFile      string               // KACHO_NOTIFY_DKIM_KEY_FILE
//	    SelectorFile string               // KACHO_NOTIFY_DKIM_SELECTOR_FILE
//	    FS           dkimkey.PairFS       // корень — dkimkey.OS
//	    Clock        Clock
//	    Registerer   prometheus.Registerer // признак notify_mail_dns_posture{check}
//	    Logger       *slog.Logger
//	}
//	func New(r *net.Resolver, o Options) (*Guard, error)       // nil-резолвер — отказ
//	func (g *Guard) Boot(ctx context.Context, deadline time.Duration) error
//	func (g *Guard) Run(ctx context.Context, interval time.Duration) // перепроверка до отмены ctx
//	func (g *Guard) Pair() dkimkey.Pair                           // пара, которой идёт подпись

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/PRO-Robotech/kacho/services/notify/internal/dkimkey"
	"github.com/PRO-Robotech/kacho/services/notify/internal/dnscheck"
)

const (
	fromDomain = "example.test"
	selA       = "sela"
	selB       = "selb"
	keyFile    = "dkim.key"
	selFile    = "dkim.selector"
	genA       = "..2026_10_05_00_00_00.000000001"
	genB       = "..2026_10_05_00_01_00.000000002"

	bootDeadline    = 2 * time.Minute
	recheckInterval = time.Hour
)

// ── управляемые часы ───────────────────────────────────────────────────────

type waiter struct {
	at time.Time
	ch chan time.Time
}

type fakeClock struct {
	mu      sync.Mutex
	now     time.Time
	waiters []waiter
	calls   int // сколько раз взведено ожидание (After)
}

func newClock() *fakeClock { return &fakeClock{now: time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	ch := make(chan time.Time, 1)
	if d <= 0 {
		ch <- c.now
		return ch
	}
	c.waiters = append(c.waiters, waiter{at: c.now.Add(d), ch: ch})
	return ch
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	keep := c.waiters[:0]
	var fire []waiter
	for _, w := range c.waiters {
		if !w.at.After(c.now) {
			fire = append(fire, w)
		} else {
			keep = append(keep, w)
		}
	}
	c.waiters = keep
	now := c.now
	c.mu.Unlock()
	for _, w := range fire {
		w.ch <- now
	}
}

func (c *fakeClock) Waiters() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.waiters)
}

func (c *fakeClock) Calls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

// waitUntil ждёт условия по реальному времени с конечным бюджетом.
func waitUntil(t *testing.T, what string, budget time.Duration, cond func() bool) {
	t.Helper()
	end := time.Now().Add(budget)
	for time.Now().Before(end) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("за %v не наступило: %s", budget, what)
}

// ── пара DKIM в томе формы kubelet ─────────────────────────────────────────

var (
	keysOnce   sync.Once
	keyA, keyB *rsa.PrivateKey
	keysErr    error
)

func keys(t *testing.T) (*rsa.PrivateKey, *rsa.PrivateKey) {
	t.Helper()
	keysOnce.Do(func() {
		if keyA, keysErr = rsa.GenerateKey(rand.Reader, 2048); keysErr != nil {
			return
		}
		keyB, keysErr = rsa.GenerateKey(rand.Reader, 2048)
	})
	if keysErr != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: ключи пробы: %v", keysErr)
	}
	return keyA, keyB
}

type volume struct {
	t   *testing.T
	dir string
}

func newVolume(t *testing.T, gen string, key *rsa.PrivateKey, sel string) *volume {
	t.Helper()
	v := &volume{t: t, dir: t.TempDir()}
	v.generation(gen, key, sel)
	for _, n := range []string{keyFile, selFile} {
		if err := os.Symlink(filepath.Join("..data", n), filepath.Join(v.dir, n)); err != nil {
			t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: ссылка %s: %v", n, err)
		}
	}
	v.swap(gen)
	return v
}

func (v *volume) generation(gen string, key *rsa.PrivateKey, sel string) {
	v.t.Helper()
	if err := os.Mkdir(filepath.Join(v.dir, gen), 0o755); err != nil {
		v.t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: поколение: %v", err)
	}
	pemKey := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	for n, b := range map[string][]byte{keyFile: pemKey, selFile: []byte(sel)} {
		if err := os.WriteFile(filepath.Join(v.dir, gen, n), b, 0o600); err != nil {
			v.t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: файл %s: %v", n, err)
		}
	}
}

func (v *volume) swap(gen string) {
	v.t.Helper()
	tmp := filepath.Join(v.dir, "..data_tmp")
	if err := os.Symlink(gen, tmp); err != nil {
		v.t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: ..data_tmp: %v", err)
	}
	if err := os.Rename(tmp, filepath.Join(v.dir, "..data")); err != nil {
		v.t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: переключение ..data: %v", err)
	}
}

// ── записи зоны ────────────────────────────────────────────────────────────

func dkimValue(t *testing.T, pub *rsa.PublicKey) string {
	t.Helper()
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: SPKI: %v", err)
	}
	return base64.StdEncoding.EncodeToString(der)
}

// soundZone — зона с исправными записями NTF1-P01 для селектора selA и ключа A.
// Запись DKIM отдаётся несколькими строками TXT.
func soundZone(t *testing.T) *zone {
	t.Helper()
	a, _ := keys(t)
	z := startZone(t)
	z.set(selA+"._domainkey."+fromDomain+".", split255("v=DKIM1; k=rsa; p="+dkimValue(t, &a.PublicKey), 200))
	z.set("_dmarc."+fromDomain+".", []string{"v=DMARC1; p=reject"})
	z.set(fromDomain+".", []string{"v=spf1 ip4:192.0.2.0/24 -all"})
	return z
}

// ── стенд пробы ────────────────────────────────────────────────────────────

type rig struct {
	t     *testing.T
	zone  *zone
	clock *fakeClock
	reg   *prometheus.Registry
	log   *syncBuf
	vol   *volume
	guard *dnscheck.Guard
}

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func newRig(t *testing.T, z *zone) *rig {
	t.Helper()
	a, _ := keys(t)
	r := &rig{t: t, zone: z, clock: newClock(), reg: prometheus.NewRegistry(), log: &syncBuf{}}
	r.vol = newVolume(t, genA, a, selA)
	g, err := dnscheck.New(z.resolver(), dnscheck.Options{
		FromDomain:   fromDomain,
		KeyFile:      filepath.Join(r.vol.dir, keyFile),
		SelectorFile: filepath.Join(r.vol.dir, selFile),
		FS:           dkimkey.OS,
		Clock:        r.clock,
		Registerer:   r.reg,
		Logger:       slog.New(slog.NewJSONHandler(r.log, &slog.HandlerOptions{Level: slog.LevelDebug})),
	})
	if err != nil {
		t.Fatalf("конструктор стража отверг исправные опции: %v", err)
	}
	r.guard = g
	return r
}

// boot исполняет страж старта; пока страж ждёт часов (пауза между попытками,
// срок старта), часы двигаются на 1 с каждые 50 мс реального времени: попытка
// на ответившей зоне укладывается в миллисекунды и между шагами часов.
// Бюджет реального времени — 60 с.
func (r *rig) boot(deadline time.Duration) error {
	r.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- r.guard.Boot(ctx, deadline) }()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	limit := time.After(60 * time.Second)
	for {
		select {
		case err := <-done:
			return err
		case <-tick.C:
			if r.clock.Waiters() >= 1 {
				r.clock.Advance(time.Second)
			}
		case <-limit:
			r.t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: страж старта не вернулся за 60 с реального времени")
			return nil
		}
	}
}

// posture — значение признака notify_mail_dns_posture{check}; -1 — ряда нет.
func (r *rig) posture(check string) float64 {
	r.t.Helper()
	mfs, err := r.reg.Gather()
	if err != nil {
		r.t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: сбор метрик: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() != "notify_mail_dns_posture" {
			continue
		}
		for _, m := range mf.GetMetric() {
			for _, lp := range m.GetLabel() {
				if lp.GetName() == "check" && lp.GetValue() == check {
					return m.GetGauge().GetValue()
				}
			}
		}
	}
	return -1
}

func (r *rig) requirePosture(want map[string]float64) {
	r.t.Helper()
	for c, w := range want {
		if got := r.posture(c); got != w {
			r.t.Fatalf("notify_mail_dns_posture{check=%q} = %v, ожидалось %v", c, got, w)
		}
	}
}

// warnLines — строки журнала уровня WARN.
func (r *rig) warnLines() []string {
	var out []string
	for _, l := range strings.Split(r.log.String(), "\n") {
		if strings.Contains(l, `"level":"WARN"`) {
			out = append(out, l)
		}
	}
	return out
}

// runRecheck запускает перепроверку и ждёт, пока она взведёт такт.
func (r *rig) runRecheck() (stop func(), done <-chan struct{}) {
	r.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan struct{})
	n := r.clock.Calls()
	go func() { r.guard.Run(ctx, recheckInterval); close(ch) }()
	waitUntil(r.t, "перепроверка взвела такт на внедрённых часах", 5*time.Second, func() bool { return r.clock.Calls() > n })
	r.t.Cleanup(func() { cancel(); <-ch })
	return cancel, ch
}

// tick — часы проходят интервал перепроверки + 1 с; такт исполнен, когда
// перепроверка взвела следующий.
func (r *rig) tick() {
	r.t.Helper()
	n := r.clock.Calls()
	r.clock.Advance(recheckInterval + time.Second)
	waitUntil(r.t, "такт перепроверки исполнен и следующий взведён", 15*time.Second, func() bool { return r.clock.Calls() > n })
}

var all1 = map[string]float64{"dkim": 1, "spf": 1, "dmarc": 1}

// ── пробы ──────────────────────────────────────────────────────────────────

// §12а, CX1-131 (б): конструктор отказывает на nil-резолвере — подстановки
// net.DefaultResolver внутри пакета нет.
func TestNewRefusesANilResolver(t *testing.T) {
	a, _ := keys(t)
	v := newVolume(t, genA, a, selA)
	_, err := dnscheck.New(nil, dnscheck.Options{
		FromDomain: fromDomain, KeyFile: filepath.Join(v.dir, keyFile), SelectorFile: filepath.Join(v.dir, selFile),
		FS: dkimkey.OS, Clock: newClock(), Registerer: prometheus.NewRegistry(), Logger: slog.New(slog.DiscardHandler),
	})
	if err == nil {
		t.Fatal("конструктор принял nil-резолвер")
	}
}

// NTF1-P01 (integration) — старт при исправных записях: готов, признаки = 1.
// Запись DKIM приходит несколькими строками TXT, и проверка её принимает.
func TestNTF1P01BootPassesOnSoundRecords(t *testing.T) {
	r := newRig(t, soundZone(t))
	if err := r.boot(bootDeadline); err != nil {
		t.Fatalf("страж отверг исправные записи: %v", err)
	}
	r.requirePosture(all1)
}

// NTF1-P04…P07, P15, P16, P19 — по одному изменённому факту; близнец — P01.
// Отказ старта называет проверку и не несёт содержимого ключа.
func TestNTF1BootRefusesOnEachViolatedCheck(t *testing.T) {
	_, b := keys(t)
	cases := []struct {
		id, name string
		edit     func(t *testing.T, z *zone)
		check    string
	}{
		{"NTF1-P04", "ключ записи DKIM не совпадает с выведенным из закрытого", func(t *testing.T, z *zone) {
			z.set(selA+"._domainkey."+fromDomain+".", split255("v=DKIM1; k=rsa; p="+dkimValue(t, &b.PublicKey), 200))
		}, "dkim"},
		{"NTF1-P05", "DMARC p=none", func(_ *testing.T, z *zone) {
			z.set("_dmarc."+fromDomain+".", []string{"v=DMARC1; p=none"})
		}, "dmarc"},
		{"NTF1-P06", "SPF завершается +all", func(_ *testing.T, z *zone) {
			z.set(fromDomain+".", []string{"v=spf1 ip4:192.0.2.0/24 +all"})
		}, "spf"},
		{"NTF1-P07", "две записи v=spf1", func(_ *testing.T, z *zone) {
			z.set(fromDomain+".", []string{"v=spf1 -all"}, []string{"v=spf1 ~all"})
		}, "spf"},
		{"NTF1-P15", "записи <селектор>._domainkey нет", func(_ *testing.T, z *zone) {
			z.set(selA + "._domainkey." + fromDomain + ".")
		}, "dkim"},
		{"NTF1-P16", "записи _dmarc нет", func(_ *testing.T, z *zone) {
			z.set("_dmarc." + fromDomain + ".")
		}, "dmarc"},
		{"NTF1-P19", "у домена адреса возврата нет v=spf1", func(_ *testing.T, z *zone) {
			z.set(fromDomain+".", []string{"google-site-verification=probe"})
		}, "spf"},
	}
	a, _ := keys(t)
	keyText := dkimValue(t, &a.PublicKey)
	for _, c := range cases {
		t.Run(c.id+" "+c.name, func(t *testing.T) {
			z := soundZone(t)
			c.edit(t, z)
			r := newRig(t, z)
			err := r.boot(bootDeadline)
			if err == nil {
				t.Fatalf("%s: страж принял записи с нарушением %q", c.id, c.check)
			}
			if !strings.Contains(err.Error(), c.check) {
				t.Fatalf("%s: отказ не называет проверку %q: %v", c.id, c.check, err)
			}
			for _, other := range []string{"dkim", "spf", "dmarc"} {
				if other != c.check && strings.Contains(err.Error(), "проверка "+other) {
					t.Fatalf("%s: отказ называет чужую проверку %q: %v", c.id, other, err)
				}
			}
			if strings.Contains(err.Error(), keyText[:32]) || strings.Contains(err.Error(), "PRIVATE KEY") {
				t.Fatalf("%s: отказ несёт содержимое ключа: %v", c.id, err)
			}
		})
	}
}

// NTF1-P08 — SERVFAIL дольше срока: отказ с исчерпанием срока.
func TestNTF1P08ServfailLongerThanDeadlineRefusesStart(t *testing.T) {
	z := soundZone(t)
	z.setMode(zoneServfail)
	r := newRig(t, z)
	err := r.boot(10 * time.Second)
	if err == nil {
		t.Fatal("страж принял старт при SERVFAIL дольше срока")
	}
	if !strings.Contains(err.Error(), "срок") {
		t.Fatalf("отказ не называет исчерпание срока: %v", err)
	}
	if !containsAny(err.Error(), "dkim", "spf", "dmarc") {
		t.Fatalf("отказ не называет проверку: %v", err)
	}
}

// NTF1-P09 — SERVFAIL короче срока (близнец P08): готов, признаки = 1.
func TestNTF1P09ServfailShorterThanDeadlinePasses(t *testing.T) {
	z := soundZone(t)
	r := newRig(t, z)
	t0 := r.clock.Now()
	z.setFailing(func() bool { return r.clock.Now().Before(t0.Add(3 * time.Second)) })
	if err := r.boot(10 * time.Second); err != nil {
		t.Fatalf("SERVFAIL короче срока отверг старт: %v", err)
	}
	r.requirePosture(all1)
}

// NTF1-P10 — запись изменилась во время работы: dmarc = 0, одна строка WARN,
// перепроверка не остановлена (отправка не останавливается: у стража нет пути,
// которым он прекратил бы работу процесса).
func TestNTF1P10RecordChangedWhileRunning(t *testing.T) {
	z := soundZone(t)
	r := newRig(t, z)
	if err := r.boot(bootDeadline); err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: близнец P01 не прошёл: %v", err)
	}
	_, done := r.runRecheck()
	z.set("_dmarc."+fromDomain+".", []string{"v=DMARC1; p=none"})
	r.tick()
	r.requirePosture(map[string]float64{"dmarc": 0, "dkim": 1, "spf": 1})
	if w := r.warnLines(); len(w) != 1 || !strings.Contains(w[0], "dmarc") {
		t.Fatalf("ожидалась ровно одна строка WARN о dmarc, получено %d:\n%s", len(w), strings.Join(w, "\n"))
	}
	select {
	case <-done:
		t.Fatal("перепроверка остановилась на нарушении — отправка была бы остановлена")
	default:
	}
}

// NTF1-P11 — записи не менялись (близнец P10): признаки = 1, WARN нет.
func TestNTF1P11RecordsUnchanged(t *testing.T) {
	r := newRig(t, soundZone(t))
	if err := r.boot(bootDeadline); err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: близнец P01 не прошёл: %v", err)
	}
	r.runRecheck()
	r.tick()
	r.requirePosture(all1)
	if w := r.warnLines(); len(w) != 0 {
		t.Fatalf("записи не менялись, а строк WARN %d:\n%s", len(w), strings.Join(w, "\n"))
	}
}

// NTF1-P17 (сторона перечитывания) — пара в объекте сменена на «селектор B,
// ключ B», запись B опубликована: подпись переходит на B, dkim = 1, одна
// строка о переходе без содержимого ключа.
func TestNTF1P17KeyRotationWhileRunning(t *testing.T) {
	_, b := keys(t)
	z := soundZone(t)
	z.set(selB+"._domainkey."+fromDomain+".", split255("v=DKIM1; k=rsa; p="+dkimValue(t, &b.PublicKey), 200))
	r := newRig(t, z)
	if err := r.boot(bootDeadline); err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: близнец P01 не прошёл: %v", err)
	}
	if got := r.guard.Pair().Selector; got != selA {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: после старта пара на селекторе %q, ожидался %q", got, selA)
	}
	r.runRecheck()
	r.vol.generation(genB, b, selB)
	r.vol.swap(genB)
	r.tick()
	p := r.guard.Pair()
	if p.Selector != selB || p.Key == nil || p.Key.N.Cmp(b.N) != 0 {
		t.Fatalf("подпись не перешла на пару B: селектор %q", p.Selector)
	}
	r.requirePosture(all1)
	var moved []string
	for _, l := range strings.Split(r.log.String(), "\n") {
		if strings.Contains(l, "подпись перешла на селектор") {
			moved = append(moved, l)
		}
	}
	if len(moved) != 1 || !strings.Contains(moved[0], selB) {
		t.Fatalf("ожидалась одна строка журнала о переходе на %q, получено %d:\n%s", selB, len(moved), r.log.String())
	}
	if strings.Contains(r.log.String(), "PRIVATE KEY") || strings.Contains(r.log.String(), dkimValue(t, &b.PublicKey)[:32]) {
		t.Fatalf("журнал несёт содержимое ключа:\n%s", r.log.String())
	}
}

// NTF1-P18 — новая пара без записи не принимается (близнец P17): подпись на
// прежней паре, dkim = 0, одна строка WARN с причиной dkim и исходом перезапуска.
func TestNTF1P18NewPairWithoutRecordIsNotTaken(t *testing.T) {
	_, b := keys(t)
	z := soundZone(t)
	r := newRig(t, z)
	if err := r.boot(bootDeadline); err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: близнец P01 не прошёл: %v", err)
	}
	_, done := r.runRecheck()
	r.vol.generation(genB, b, selB)
	r.vol.swap(genB)
	r.tick()
	if got := r.guard.Pair().Selector; got != selA {
		t.Fatalf("пара без записи принята: подпись на селекторе %q, ожидался прежний %q", got, selA)
	}
	r.requirePosture(map[string]float64{"dkim": 0, "spf": 1, "dmarc": 1})
	w := r.warnLines()
	if len(w) != 1 || !strings.Contains(w[0], "dkim") || !strings.Contains(w[0], "перезапуск") {
		t.Fatalf("ожидалась одна строка WARN с причиной dkim и исходом перезапуска, получено %d:\n%s", len(w), strings.Join(w, "\n"))
	}
	select {
	case <-done:
		t.Fatal("перепроверка остановилась — отправка была бы остановлена")
	default:
	}
}

// N44-1 — форма попытки: три проверки параллельно — первые запросы трёх имён
// приходят в зону в пределах 1 с друг от друга (последовательно — через 5 с).
func TestBootAttemptRunsTheThreeChecksInParallel(t *testing.T) {
	z := soundZone(t)
	z.setMode(zoneSilent)
	r := newRig(t, z)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.guard.Boot(ctx, bootDeadline) }()
	first := map[string]time.Time{}
	waitUntil(t, "запросы трёх имён пришли в зону", 8*time.Second, func() bool {
		for _, a := range z.seen() {
			if _, ok := first[a.name]; !ok {
				first[a.name] = a.at
			}
		}
		return len(first) >= 3
	})
	cancel()
	<-done
	var lo, hi time.Time
	for _, at := range first {
		if lo.IsZero() || at.Before(lo) {
			lo = at
		}
		if at.After(hi) {
			hi = at
		}
	}
	if spread := hi.Sub(lo); spread > time.Second {
		t.Fatalf("первые запросы трёх проверок разошлись на %v (> 1 с) — проверки идут не параллельно", spread)
	}
}

// УК4-40, УК4-22 в части Р19 — родитель отменён во время молчания зоны на
// перепроверке: признак не изменён, WARN нет.
func TestRecheckCancelledParentLeavesPostureUnchanged(t *testing.T) {
	z := soundZone(t)
	r := newRig(t, z)
	if err := r.boot(bootDeadline); err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: близнец P01 не прошёл: %v", err)
	}
	stop, done := r.runRecheck()
	z.setMode(zoneSilent)
	before := len(z.seen())
	r.clock.Advance(recheckInterval + time.Second)
	waitUntil(t, "запрос перепроверки пришёл в молчащую зону", 5*time.Second, func() bool { return len(z.seen()) > before })
	stop()
	<-done
	r.requirePosture(all1)
	if w := r.warnLines(); len(w) != 0 {
		t.Fatalf("отмена родителя дала строки WARN:\n%s", strings.Join(w, "\n"))
	}
}

func containsAny(s string, subs ...string) bool {
	for _, x := range subs {
		if strings.Contains(s, x) {
			return true
		}
	}
	return false
}
