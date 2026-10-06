// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deliver

// dkim_fixture_test.go — условия проб подписи письма на пути клетки 10
// (полоса N15; приёмка NTF-1 Р19 «Подпись», «Смена ключа во время работы»,
// «Адрес возврата в NTF-1»; NTF1-P02, P03, P17, P18; Д101). Испытуемого
// (`deliver` с подписчиком, пакет `dkim`) файл НЕ называет: его и самопроверку
// (dkim_fixture_selfcheck_test.go) можно исполнить без них.
//
// Что здесь есть:
//
//   - страж DNS установки — НАСТОЯЩИЙ `dnscheck.Guard` (полоса N14) на зоне
//     испытания `dnstest` (UDP на петле), с томом формы kubelet и управляемыми
//     часами: пара, которой идёт подпись, — `Guard.Pair()` после старта и после
//     такта перепроверки;
//   - письмо — настоящий сборщик notify (`render`) над шаблоном корпуса corelib
//     `hello/ru` (кириллица);
//   - проверка подписи принятого узлом письма — НЕЗАВИСИМАЯ реализация
//     `github.com/emersion/go-msgauth/dkim` по записям той же зоны.

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	msgdkim "github.com/emersion/go-msgauth/dkim"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/PRO-Robotech/corelib/notify/address"
	"github.com/PRO-Robotech/corelib/notify/form"
	"github.com/PRO-Robotech/corelib/notify/spec"

	"github.com/PRO-Robotech/kacho/services/notify/internal/dkimkey"
	"github.com/PRO-Robotech/kacho/services/notify/internal/dnscheck"
	"github.com/PRO-Robotech/kacho/services/notify/internal/dnscheck/dnstest"
	"github.com/PRO-Robotech/kacho/services/notify/internal/render"
	"github.com/PRO-Robotech/kacho/services/notify/internal/render/emlprobe"
)

const (
	// signDomain — домен `From` установки пробы (домен fixtureFrom): `d=`.
	signDomain = "example.invalid"
	// signSelA, signSelB — селекторы пар A и B.
	signSelA = "n15a"
	signSelB = "n15b"

	dkimKeyFile = "dkim.key"
	dkimSelFile = "dkim.selector"
	dkimGenA    = "..2026_10_06_00_00_00.000000001"
	dkimGenB    = "..2026_10_06_00_01_00.000000002"

	guardBootDeadline    = 2 * time.Minute
	guardRecheckInterval = time.Hour
)

// r19SignedHeaders — перечень `h=` Р19 без List-Unsubscribe; `from` — дважды.
var r19SignedHeaders = []string{"from", "from", "to", "subject", "date", "message-id", "mime-version", "content-type"}

// ── ключи и постоянная пара ──────────────────────────────────────────────────

var (
	signKeysOnce       sync.Once
	signKeyA, signKeyB *rsa.PrivateKey
	signKeysErr        error
)

func signKeys(t testing.TB) (*rsa.PrivateKey, *rsa.PrivateKey) {
	t.Helper()
	signKeysOnce.Do(func() {
		if signKeyA, signKeysErr = rsa.GenerateKey(rand.Reader, 2048); signKeysErr != nil {
			return
		}
		signKeyB, signKeysErr = rsa.GenerateKey(rand.Reader, 2048)
	})
	if signKeysErr != nil {
		t.Fatalf("ФИКСТУРА: ключи DKIM пробы не выпущены: %v", signKeysErr)
	}
	return signKeyA, signKeyB
}

// fixturePair — постоянная пара подписчика проб, чей предмет не подпись.
func fixturePair(t testing.TB) dkimkey.Pair {
	t.Helper()
	a, _ := signKeys(t)
	return dkimkey.Pair{Selector: signSelA, Key: a, Generation: dkimGenA}
}

// fixedPairs — источник постоянной пары.
type fixedPairs struct{ p dkimkey.Pair }

func (f fixedPairs) Pair() dkimkey.Pair { return f.p }

// ── том формы kubelet ────────────────────────────────────────────────────────

type dkimVolume struct {
	t   testing.TB
	dir string
}

func newDKIMVolume(t testing.TB, gen string, key *rsa.PrivateKey, sel string) *dkimVolume {
	t.Helper()
	v := &dkimVolume{t: t, dir: t.TempDir()}
	v.generation(gen, key, sel)
	for _, n := range []string{dkimKeyFile, dkimSelFile} {
		if err := os.Symlink(filepath.Join("..data", n), filepath.Join(v.dir, n)); err != nil {
			t.Fatalf("ФИКСТУРА: ссылка %s: %v", n, err)
		}
	}
	v.swap(gen)
	return v
}

func (v *dkimVolume) generation(gen string, key *rsa.PrivateKey, sel string) {
	v.t.Helper()
	if err := os.Mkdir(filepath.Join(v.dir, gen), 0o755); err != nil {
		v.t.Fatalf("ФИКСТУРА: поколение %s: %v", gen, err)
	}
	pemKey := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	for n, b := range map[string][]byte{dkimKeyFile: pemKey, dkimSelFile: []byte(sel)} {
		if err := os.WriteFile(filepath.Join(v.dir, gen, n), b, 0o600); err != nil {
			v.t.Fatalf("ФИКСТУРА: файл %s: %v", n, err)
		}
	}
}

// swap — переключение `..data` на поколение gen (шаг kubelet: ссылка рядом и
// атомарное переименование).
func (v *dkimVolume) swap(gen string) {
	v.t.Helper()
	tmp := filepath.Join(v.dir, "..data_tmp")
	if err := os.Symlink(gen, tmp); err != nil {
		v.t.Fatalf("ФИКСТУРА: ..data_tmp: %v", err)
	}
	if err := os.Rename(tmp, filepath.Join(v.dir, "..data")); err != nil {
		v.t.Fatalf("ФИКСТУРА: переключение ..data: %v", err)
	}
}

// ── управляемые часы стража ──────────────────────────────────────────────────

type guardWaiter struct {
	at time.Time
	ch chan time.Time
}

type guardClock struct {
	mu      sync.Mutex
	now     time.Time
	waiters []guardWaiter
	calls   int
}

func newGuardClock() *guardClock {
	return &guardClock{now: time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)}
}

func (c *guardClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *guardClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	ch := make(chan time.Time, 1)
	if d <= 0 {
		ch <- c.now
		return ch
	}
	c.waiters = append(c.waiters, guardWaiter{at: c.now.Add(d), ch: ch})
	return ch
}

func (c *guardClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	kept := c.waiters[:0]
	for _, w := range c.waiters {
		if !w.at.After(c.now) {
			w.ch <- c.now
			continue
		}
		kept = append(kept, w)
	}
	c.waiters = kept
}

func (c *guardClock) Waiters() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.waiters)
}

func (c *guardClock) Calls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

func waitFor(t testing.TB, what string, budget time.Duration, cond func() bool) {
	t.Helper()
	end := time.Now().Add(budget)
	for !cond() {
		if time.Now().After(end) {
			t.Fatalf("ФИКСТУРА: не дождались за %s: %s", budget, what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// ── страж на зоне испытания ──────────────────────────────────────────────────

// guardRig — страж DNS установки на паре A, зона с исправными записями Р19
// для селектора signSelA.
type guardRig struct {
	t     testing.TB
	zone  *dnstest.Zone
	vol   *dkimVolume
	clock *guardClock
	reg   *prometheus.Registry
	log   *logBuffer
	guard *dnscheck.Guard
}

func newGuardRig(t testing.TB) *guardRig {
	t.Helper()
	a, _ := signKeys(t)
	z := dnstest.Start(t)
	z.PublishSound(signDomain, signSelA, &a.PublicKey)
	logger, lb := newLog()
	g := &guardRig{t: t, zone: z, vol: newDKIMVolume(t, dkimGenA, a, signSelA), clock: newGuardClock(),
		reg: prometheus.NewRegistry(), log: lb}
	guard, err := dnscheck.New(z.Resolver(), dnscheck.Options{
		FromDomain:   signDomain,
		KeyFile:      filepath.Join(g.vol.dir, dkimKeyFile),
		SelectorFile: filepath.Join(g.vol.dir, dkimSelFile),
		FS:           dkimkey.OS,
		Clock:        g.clock,
		Registerer:   g.reg,
		Logger:       logger,
	})
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: страж DNS установки не собран: %v", err)
	}
	g.guard = guard
	return g
}

// publish — запись DKIM селектора sel с открытым ключом k.
func (g *guardRig) publish(sel string, k *rsa.PrivateKey) {
	g.t.Helper()
	der, err := x509.MarshalPKIXPublicKey(&k.PublicKey)
	if err != nil {
		g.t.Fatalf("ФИКСТУРА: SPKI селектора %s: %v", sel, err)
	}
	g.zone.Set(sel+"._domainkey."+signDomain+".",
		dnstest.Split("v=DKIM1; k=rsa; p="+base64.StdEncoding.EncodeToString(der), 200))
}

// boot — страж старта; часы идут на 1 с каждые 20 мс, пока страж ждёт.
// Не прошёл — условие пробы не создано.
func (g *guardRig) boot() {
	g.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- g.guard.Boot(ctx, guardBootDeadline) }()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	limit := time.After(60 * time.Second)
	for {
		select {
		case err := <-done:
			if err != nil {
				g.t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: страж старта на исправной зоне (NTF1-P01) не прошёл: %v", err)
			}
			if p := g.guard.Pair(); p.Selector != signSelA {
				g.t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: после старта пара на селекторе %q, ожидался %q", p.Selector, signSelA)
			}
			return
		case <-tick.C:
			if g.clock.Waiters() >= 1 {
				g.clock.Advance(time.Second)
			}
		case <-limit:
			g.t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: страж старта не вернулся за 60 с реального времени")
		}
	}
}

// runRecheck — перепроверка до конца пробы; ждёт, пока взведён первый такт.
func (g *guardRig) runRecheck() {
	g.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	n := g.clock.Calls()
	go func() { g.guard.Run(ctx, guardRecheckInterval); close(done) }()
	waitFor(g.t, "перепроверка взвела такт", 5*time.Second, func() bool { return g.clock.Calls() > n })
	g.t.Cleanup(func() { cancel(); <-done })
}

// tick — часы проходят интервал перепроверки + 1 с; такт исполнен, когда
// взведён следующий.
func (g *guardRig) tick() {
	g.t.Helper()
	n := g.clock.Calls()
	g.clock.Advance(guardRecheckInterval + time.Second)
	waitFor(g.t, "такт перепроверки исполнен", 15*time.Second, func() bool { return g.clock.Calls() > n })
}

// rotateTo — в объекте ключа DKIM пара заменена на (sel, k) новым поколением.
func (g *guardRig) rotateTo(gen, sel string, k *rsa.PrivateKey) {
	g.t.Helper()
	g.vol.generation(gen, k, sel)
	g.vol.swap(gen)
}

// posture — значение notify_mail_dns_posture{check}; -1 — ряда нет.
func (g *guardRig) posture(check string) float64 {
	g.t.Helper()
	mfs, err := g.reg.Gather()
	if err != nil {
		g.t.Fatalf("ФИКСТУРА: сбор метрик стража: %v", err)
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

// ── письмо настоящего сборщика ───────────────────────────────────────────────

// renderedLetter — письмо `hello/ru` (кириллица) от fixtureFrom к fixtureTo,
// собранное настоящим сборщиком notify.
func renderedLetter(t testing.TB) []byte {
	t.Helper()
	dir, err := emlprobe.CorelibCorpusDir("notice-plain")
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: корпус corelib: %v", err)
	}
	cat, _, err := spec.LoadFS(os.DirFS(dir), ".")
	if err != nil || len(cat.Templates) != 1 {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: шаблон hello корпуса: шаблонов %d, %v", len(cat.Templates), err)
	}
	name, err := form.ParseHeaderText("Облако Kacho")
	if err != nil {
		t.Fatalf("ФИКСТУРА: имя отправителя: %v", err)
	}
	from, err := address.Normalize(fixtureFrom)
	if err != nil {
		t.Fatalf("ФИКСТУРА: адрес отправителя: %v", err)
	}
	to, err := address.Normalize(fixtureTo)
	if err != nil {
		t.Fatalf("ФИКСТУРА: адресат: %v", err)
	}
	r, err := render.New(render.Config{Origin: "https://console.example.invalid", From: render.Sender{Name: name, Address: from}})
	if err != nil {
		t.Fatalf("ФИКСТУРА: render.New: %v", err)
	}
	msg, err := r.Render(render.Letter{Namespace: nsProbe, RowID: "0192a6c0-7a00-7000-8000-0000000000f5",
		Template: cat.Templates[0], Locale: "ru", To: to, Date: time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatalf("ФИКСТУРА: письмо hello/ru не собрано: %v", err)
	}
	return msg
}

// ── принятое узлом письмо и независимая проверка ─────────────────────────────

// wireForm — письмо в форме провода: узел пробы отдаёт тело DotReader-а
// (концы строк `\n`), сборщик выпускает CRLF; обратное преобразование
// восстанавливает отправленные байты.
func wireForm(b []byte) []byte {
	return bytes.ReplaceAll(bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n")), []byte("\n"), []byte("\r\n"))
}

// verifyOn — проверка подписей письма независимой реализацией по записям зоны z.
func verifyOn(t testing.TB, z *dnstest.Zone, msg []byte) []*msgdkim.Verification {
	t.Helper()
	vs, err := msgdkim.VerifyWithOptions(bytes.NewReader(msg), &msgdkim.VerifyOptions{
		LookupTXT: func(domain string) ([]string, error) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			return z.Resolver().LookupTXT(ctx, strings.TrimSuffix(domain, ".")+".")
		},
	})
	if err != nil {
		t.Fatalf("независимая проверка не разобрала письмо: %v", err)
	}
	return vs
}

// bodyHashFailed — исход именно «тело не сходится», а не разбор и не DNS.
func bodyHashFailed(v *msgdkim.Verification) bool {
	return v.Err != nil && !msgdkim.IsPermFail(v.Err) && !msgdkim.IsTempFail(v.Err) &&
		v.Err.Error() == "dkim: body hash did not verify"
}

// dkimTags — теги первого DKIM-Signature письма; пробелы в значениях сняты.
// Заголовка нет — nil.
func dkimTags(t testing.TB, msg []byte) map[string]string {
	t.Helper()
	h, err := readHeader(msg)
	if err != nil {
		t.Fatalf("заголовки письма не разбираются: %v", err)
	}
	raw := h.Values("Dkim-Signature")
	if len(raw) == 0 {
		return nil
	}
	out := map[string]string{}
	for _, part := range strings.Split(raw[0], ";") {
		if k, v, ok := strings.Cut(part, "="); ok {
			out[strings.TrimSpace(k)] = strings.Join(strings.Fields(v), "")
		}
	}
	return out
}

func readHeader(msg []byte) (textproto.MIMEHeader, error) {
	return textproto.NewReader(bufio.NewReader(bytes.NewReader(msg))).ReadMIMEHeader()
}

// signedHeaderKeys — `h=` по именам в нижнем регистре.
func signedHeaderKeys(tags map[string]string) []string {
	var out []string
	for _, k := range strings.Split(tags["h"], ":") {
		if k = strings.ToLower(strings.TrimSpace(k)); k != "" {
			out = append(out, k)
		}
	}
	return out
}

func countOf(xs []string, x string) int {
	n := 0
	for _, v := range xs {
		if v == x {
			n++
		}
	}
	return n
}

// flipOneBodyByte — копия письма с ровно одним изменённым октетом тела.
func flipOneBodyByte(t testing.TB, msg []byte) []byte {
	t.Helper()
	i := bytes.Index(msg, []byte("\r\n\r\n"))
	if i < 0 {
		t.Fatalf("ФИКСТУРА: в письме нет тела")
	}
	out := append([]byte(nil), msg...)
	for j := i + 4; j < len(out); j++ {
		if c := out[j]; (c >= 'a' && c < 'z') || (c >= 'A' && c < 'Z') {
			out[j] = c + 1
			return out
		}
	}
	t.Fatalf("ФИКСТУРА: в теле нет латинской буквы для изменения")
	return nil
}

// independentlySigned — письмо, подписанное НЕЗАВИСИМОЙ реализацией с
// параметрами Р19: самопроверка доказывает им, что проверка различает pass и fail.
func independentlySigned(t testing.TB, msg []byte, sel string, k *rsa.PrivateKey) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := msgdkim.Sign(&out, bytes.NewReader(msg), &msgdkim.SignOptions{
		Domain: signDomain, Selector: sel, Signer: k,
		HeaderCanonicalization: msgdkim.CanonicalizationRelaxed,
		BodyCanonicalization:   msgdkim.CanonicalizationRelaxed,
		HeaderKeys:             r19SignedHeaders,
	}); err != nil {
		t.Fatalf("ФИКСТУРА: независимая подпись: %v", err)
	}
	return out.Bytes()
}
