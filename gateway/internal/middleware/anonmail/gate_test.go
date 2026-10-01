// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package anonmail

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

const (
	pathRecovery = middleware.LoginLanePathRecovery
	pathRegister = middleware.LoginLanePathRegister
)

// TestGate_NTF2_60a_SourceLadderAndAddressIndependence — сценарий 60 (а): ряд с
// одного источника, чередуя адреса A и Z; получивший вызов повторяется с
// доказательством; ряд идёт до H пропущенных и ещё один запрос.
func TestGate_NTF2_60a_SourceLadderAndAddressIndependence(t *testing.T) {
	l := testLimits()
	r := memoryRig(t, l)
	F, P, H := l.Source.Free, l.Source.PoW, l.Source.Hard
	t.Logf("ручки: F=%d P=%d H=%d · Db=%d Dh=%d", F, P, H, l.PoWBits.Base, l.PoWBits.High)
	bitsByN := map[int]int{}
	for n := 1; n <= H; n++ {
		email := "a@example.test"
		if n%2 == 0 {
			email = "z@example.test"
		}
		first := r.send(pathRecovery, "198.51.100.7", "", `{"email":"`+email+`"}`)
		if n <= F {
			if first.Code != http.StatusOK {
				t.Fatalf("запрос %d (≤ F): %d %s", n, first.Code, first.Body.String())
			}
			continue
		}
		tok, bits := challengeOf(t, first)
		bitsByN[n] = bits
		again := r.send(pathRecovery, "198.51.100.7", tok+":"+solve(t, tok, bits), `{"email":"`+email+`"}`)
		if again.Code != http.StatusOK {
			t.Fatalf("запрос %d с доказательством: %d %s", n, again.Code, again.Body.String())
		}
	}
	over := r.send(pathRecovery, "198.51.100.7", "", `{"email":"z@example.test"}`)
	if !isRateLimited(t, over) {
		t.Fatalf("запрос сверх H: %d %s, ожидался RATE_LIMITED", over.Code, over.Body.String())
	}
	ra, err := strconv.Atoi(over.Header().Get("Retry-After"))
	if err != nil || ra <= 0 {
		t.Errorf("Retry-After %q не целое > 0", over.Header().Get("Retry-After"))
	}
	for n := F + 1; n <= H; n++ {
		want := l.PoWBits.Base
		if n > P {
			want = l.PoWBits.High
		}
		if bitsByN[n] != want {
			t.Errorf("запрос %d: difficultyBits %d, ожидалось %d", n, bitsByN[n], want)
		}
	}
	t.Logf("difficultyBits по номеру: %v · Retry-After %s", bitsByN, over.Header().Get("Retry-After"))
	if got := r.lane.calls.Load(); got != int64(H) {
		t.Errorf("до полосы дошло %d запросов, пропущено звеном %d", got, H)
	}
	// Близнец: F запросов с другого источника — без вызова.
	for i := 0; i < F; i++ {
		if rec := r.send(pathRecovery, "198.51.100.8", "", `{}`); rec.Code != http.StatusOK {
			t.Fatalf("близнец S2, запрос %d: %d", i+1, rec.Code)
		}
	}
}

// TestGate_NTF2_60_ResponsesAreEqualForAnyAddress — ответ на каждой ступени не
// зависит от адресата: тело запроса звено не читает. Сравниваются ответы двух
// независимых рядов, различающихся только адресом в теле, без значений
// challenge и expiresAt.
func TestGate_NTF2_60_ResponsesAreEqualForAnyAddress(t *testing.T) {
	l := testLimits()
	run := func(email string) []string {
		r := memoryRig(t, l)
		var out []string
		for n := 1; n <= l.Source.Hard+1; n++ {
			rec := r.send(pathRecovery, "198.51.100.7", "", `{"email":"`+email+`"}`)
			shape := strconv.Itoa(rec.Code)
			if rec.Code == http.StatusTooManyRequests {
				b := parseStatus(t, rec)
				d := b.Details[0]
				shape += fmt.Sprintf(" %d %s %s bits=%s ra=%q", b.Code, b.Message, d.Reason, d.Metadata["difficultyBits"], rec.Header().Get("Retry-After"))
				if d.Reason == "PROOF_OF_WORK_REQUIRED" {
					tok := d.Metadata["challenge"]
					bits, _ := strconv.Atoi(d.Metadata["difficultyBits"])
					_ = r.send(pathRecovery, "198.51.100.7", tok+":"+solve(t, tok, bits), `{"email":"`+email+`"}`)
				}
			}
			out = append(out, shape)
		}
		return out
	}
	a, z := run("a@example.test"), run("z@example.test")
	for i := range a {
		if a[i] != z[i] {
			t.Errorf("ступень %d: для A %q, для Z %q", i+1, a[i], z[i])
		}
	}
}

// TestGate_NTF2_60b_PoWEqualsHardGivesOnlyBaseChallenges — близнец (б).
func TestGate_NTF2_60b_PoWEqualsHardGivesOnlyBaseChallenges(t *testing.T) {
	l := testLimits()
	l.Source.PoW = l.Source.Hard
	r := memoryRig(t, l)
	for n := 1; n <= l.Source.Hard; n++ {
		bits, rec := r.pass(pathRecovery, "198.51.100.7")
		if rec.Code != http.StatusOK {
			t.Fatalf("запрос %d: %d", n, rec.Code)
		}
		if n > l.Source.Free && bits != l.PoWBits.Base {
			t.Errorf("запрос %d: difficultyBits %d при POW = HARD, ожидалось Db = %d", n, bits, l.PoWBits.Base)
		}
	}
	if !isRateLimited(t, r.send(pathRecovery, "198.51.100.7", "", "")) {
		t.Error("сверх H — не RATE_LIMITED")
	}
}

// TestGate_NTF2_60c_WindowsAreHalfOpenOnTheRight — (в1)–(в3): окно (t − W, t];
// пропущенный в t1 момент считается по t1 + W − 1 с и выходит в t1 + W.
func TestGate_NTF2_60c_WindowsAreHalfOpenOnTheRight(t *testing.T) {
	l := testLimits()
	l.Global.Burst = 100 // (в1): F ≤ BURST — все F запросов построения в один момент
	l.Global.RatePerSecond = 50
	src := "198.51.100.9"
	t.Run("в1 FREE", func(t *testing.T) {
		r := memoryRig(t, l)
		t1 := r.clock.Now()
		for i := 0; i < l.Source.Free; i++ {
			if _, rec := r.pass(pathRecovery, src); rec.Code != http.StatusOK {
				t.Fatal(rec.Code)
			}
		}
		r.clock.Set(t1.Add(l.Source.FreeWindow - time.Second))
		_, bits := challengeOf(t, r.send(pathRecovery, src, "", ""))
		if bits != l.PoWBits.Base {
			t.Errorf("t1 + W_F − 1 с: биты %d", bits)
		}
		r.clock.Set(t1.Add(l.Source.FreeWindow))
		if rec := r.send(pathRecovery, src, "", ""); rec.Code != http.StatusOK {
			t.Errorf("t1 + W_F: %d, ожидался пропуск без вызова", rec.Code)
		}
	})
	t.Run("в2 POW", func(t *testing.T) {
		r := memoryRig(t, l)
		t1 := r.clock.Now()
		for i := 0; i < l.Source.PoW; i++ {
			if _, rec := r.pass(pathRecovery, src); rec.Code != http.StatusOK {
				t.Fatal(rec.Code)
			}
		}
		r.clock.Set(t1.Add(l.Source.PoWWindow - time.Second))
		if _, bits := challengeOf(t, r.send(pathRecovery, src, "", "")); bits != l.PoWBits.High {
			t.Errorf("t1 + W_P − 1 с: биты %d, ожидалось Dh", bits)
		}
		r.clock.Set(t1.Add(l.Source.PoWWindow))
		rec := r.send(pathRecovery, src, "", "")
		if rec.Code == http.StatusTooManyRequests {
			if _, bits := challengeOf(t, rec); bits == l.PoWBits.High {
				t.Errorf("t1 + W_P: вызов Dh, окно POW не опустело")
			}
		}
		t.Logf("t1 + W_P: %d", rec.Code)
	})
	t.Run("в3 HARD", func(t *testing.T) {
		r := memoryRig(t, l)
		t1 := r.clock.Now()
		for i := 0; i < l.Source.Hard; i++ {
			if _, rec := r.pass(pathRecovery, src); rec.Code != http.StatusOK {
				t.Fatal(rec.Code)
			}
		}
		r.clock.Set(t1.Add(l.Source.HardWindow - time.Second))
		rec := r.send(pathRecovery, src, "", "")
		if !isRateLimited(t, rec) {
			t.Errorf("t1 + W_H − 1 с: %d, ожидался RATE_LIMITED", rec.Code)
		}
		if ra := rec.Header().Get("Retry-After"); ra != "1" {
			t.Errorf("t1 + W_H − 1 с: Retry-After %q, ожидалась 1", ra)
		}
		r.clock.Set(t1.Add(l.Source.HardWindow))
		if isRateLimited(t, r.send(pathRecovery, src, "", "")) {
			t.Error("t1 + W_H: RATE_LIMITED, окно HARD не опустело")
		}
	})
}

// TestGate_NTF2_61_SubnetAxis — сценарий 61: источники одной подсети, каждый не
// больше своего FREE; сверх Ps — вызов базовой сложности; сверх Hs —
// RATE_LIMITED; близнец — те же числа из разных подсетей без вызова.
func TestGate_NTF2_61_SubnetAxis(t *testing.T) {
	l := testLimits()
	l.Global.Burst, l.Global.RatePerSecond = 1000, 1000
	l.Source.Free = 2
	sets := []struct {
		name     string
		lim      config.AnonMailSubnetLimits
		inSubnet func(i int) string
		apart    func(i int) string
	}{
		{"а IPv4 /24", l.SubnetV4Len24, func(i int) string { return fmt.Sprintf("203.0.113.%d", i+1) },
			func(i int) string { return fmt.Sprintf("10.%d.%d.1", i/200, i%200) }},
		{"б IPv6 /56", l.SubnetV6Len56, func(i int) string { return fmt.Sprintf("2001:db8:1:%x::1", i) },
			func(i int) string { return fmt.Sprintf("2001:db8:%x:100::1", 2+i) }},
		{"в IPv6 /48", l.SubnetV6Len48, func(i int) string { return fmt.Sprintf("2001:db8:2:%x00::1", i+1) },
			func(i int) string { return fmt.Sprintf("2001:%x:100::1", 0x1000+i) }},
	}
	for _, s := range sets {
		t.Run(s.name, func(t *testing.T) {
			r := memoryRig(t, l)
			passed, src := 0, 0
			// Каждый источник — один запрос: ниже своего FREE.
			for passed < s.lim.PoW {
				if _, rec := r.pass(pathRegister, s.inSubnet(src)); rec.Code != http.StatusOK {
					t.Fatalf("до Ps, запрос %d: %d", passed+1, rec.Code)
				}
				src++
				passed++
			}
			rec := r.send(pathRegister, s.inSubnet(src), "", "")
			if _, bits := challengeOf(t, rec); bits != l.PoWBits.Base {
				t.Errorf("сверх Ps: биты %d, ожидалось POW_BITS_BASE", bits)
			}
			for passed < s.lim.Hard {
				if _, rec := r.pass(pathRegister, s.inSubnet(src)); rec.Code != http.StatusOK {
					t.Fatalf("до Hs, запрос %d: %d", passed+1, rec.Code)
				}
				src++
				passed++
			}
			if !isRateLimited(t, r.send(pathRegister, s.inSubnet(src), "", "")) {
				t.Error("сверх Hs — не RATE_LIMITED")
			}
			// Близнец.
			tw := memoryRig(t, l)
			for i := 0; i < s.lim.Hard+1; i++ {
				if rec := tw.send(pathRegister, s.apart(i), "", ""); rec.Code != http.StatusOK {
					t.Fatalf("близнец, запрос %d из своей подсети: %d %s", i+1, rec.Code, rec.Body.String())
				}
			}
		})
	}
}

// TestGate_NTF2_61g_SubnetWindows — (г): окна подсети W_Ps и W_Hs.
func TestGate_NTF2_61g_SubnetWindows(t *testing.T) {
	l := testLimits()
	l.Global.Burst, l.Global.RatePerSecond = 1000, 1000
	l.Source.Free = 2
	for _, step := range []struct {
		name   string
		n      int
		window time.Duration
		hard   bool
	}{{"Ps", l.SubnetV4Len24.PoW, l.SubnetPoWWindow, false}, {"Hs", l.SubnetV4Len24.Hard, l.SubnetHardWindow, true}} {
		t.Run(step.name, func(t *testing.T) {
			r := memoryRig(t, l)
			t1 := r.clock.Now()
			for i := 0; i < step.n; i++ {
				if _, rec := r.pass(pathRegister, fmt.Sprintf("203.0.113.%d", i+1)); rec.Code != http.StatusOK {
					t.Fatal(rec.Code)
				}
			}
			r.clock.Set(t1.Add(step.window - time.Second))
			rec := r.send(pathRegister, "203.0.113.250", "", "")
			if step.hard && !isRateLimited(t, rec) {
				t.Errorf("t1 + W_Hs − 1 с: %d, ожидался RATE_LIMITED", rec.Code)
			}
			if !step.hard {
				challengeOf(t, rec)
			}
			r.clock.Set(t1.Add(step.window))
			rec = r.send(pathRegister, "203.0.113.251", "", "")
			if step.hard && isRateLimited(t, rec) {
				t.Error("t1 + W_Hs: RATE_LIMITED")
			}
			if !step.hard && rec.Code != http.StatusOK {
				t.Errorf("t1 + W_Ps: %d, ожидался пропуск без вызова", rec.Code)
			}
		})
	}
}

// TestGate_NTF2_62_EachBrokenProofGetsANewChallenge — сценарий 62: (а) nonce
// без нужных битов, (б) истёкший вызов, (в) верное доказательство, уже
// использованное пропуском 200 (фикстура строится пропуском — З9, CX2-43),
// (г) вызов, изменённый в одном знаке; каждый — 429 с НОВЫМ вызовом, полоса не
// вызвана. Близнец — верное доказательство впервые: 200.
func TestGate_NTF2_62_EachBrokenProofGetsANewChallenge(t *testing.T) {
	l := testLimits()
	r := memoryRig(t, l)
	src := "198.51.100.20"
	for i := 0; i < l.Source.Free; i++ {
		r.send(pathRecovery, src, "", "")
	}
	tokC, bitsC := challengeOf(t, r.send(pathRecovery, src, "", ""))
	good := tokC + ":" + solve(t, tokC, bitsC)

	before := r.lane.calls.Load()
	expectNew := func(name, proof string) {
		t.Helper()
		rec := r.send(pathRecovery, src, proof, "")
		tok, _ := challengeOf(t, rec)
		if tok == tokC {
			t.Errorf("%s: в ответе прежний вызов, ожидался новый", name)
		}
	}
	expectNew("а", tokC+":"+unsolved(t, tokC, bitsC))
	tamper := []byte(tokC)
	tamper[12] ^= 1
	if tamper[12] == '-' || tamper[12] == '_' {
		tamper[12] = 'Q'
	}
	expectNew("г", string(tamper)+":"+solve(t, string(tamper), bitsC))
	if got := r.lane.calls.Load(); got != before {
		t.Fatalf("отвергнутое доказательство дошло до полосы: %d", got-before)
	}
	// Близнец: верное доказательство к C впервые — пропуск; оно же строит
	// фикстуру (в).
	if rec := r.send(pathRecovery, src, good, ""); rec.Code != http.StatusOK {
		t.Fatalf("близнец: верное доказательство впервые — %d %s", rec.Code, rec.Body.String())
	}
	expectNew("в", good)
	// (б) — после срока вызова.
	tokB, bitsB := challengeOf(t, r.send(pathRecovery, src, "", ""))
	r.clock.Add(ChallengeTTL)
	expectNew("б", tokB+":"+solve(t, tokB, bitsB))
	if got := r.lane.calls.Load(); got != before+1 {
		t.Errorf("до полосы дошло %d запросов сверх построения, ожидался 1 (близнец)", got-before)
	}
}

// TestGate_NTF2_74_GlobalFlowChallengesEvenSourcesBelowTheirThreshold — общий
// поток: B + G + 1 источников из своих /24 по одному запросу в одну секунду
// часов пробы; пропущенных без вызова — от B до B + G, остальным — вызов
// базовой сложности, не RATE_LIMITED; доказательство пропускает. Близнец — те
// же запросы равномерно за ⌈(B+G+1)/G⌉ + 1 секунд — все без вызова.
func TestGate_NTF2_74_GlobalFlowChallengesEvenSourcesBelowTheirThreshold(t *testing.T) {
	l := testLimits()
	B, G := l.Global.Burst, int(l.Global.RatePerSecond)
	n := B + G + 1
	r := memoryRig(t, l)
	free, challenged := 0, 0
	t0 := r.clock.Now()
	for i := 0; i < n; i++ {
		r.clock.Set(t0.Add(time.Duration(i) * time.Second / time.Duration(n)))
		ip := fmt.Sprintf("10.%d.%d.1", 1+i/250, i%250)
		rec := r.send(pathRecovery, ip, "", "")
		switch {
		case rec.Code == http.StatusOK:
			free++
		case isRateLimited(t, rec):
			t.Fatalf("источник %d: RATE_LIMITED — общий поток даёт вызов, а не отказ", i)
		default:
			tok, bits := challengeOf(t, rec)
			if bits != l.PoWBits.Base {
				t.Errorf("источник %d: биты %d, ожидалось POW_BITS_BASE", i, bits)
			}
			challenged++
			if again := r.send(pathRecovery, ip, tok+":"+solve(t, tok, bits), ""); again.Code != http.StatusOK {
				t.Errorf("источник %d с доказательством: %d", i, again.Code)
			}
		}
	}
	t.Logf("B=%d G=%d · без вызова %d · с вызовом %d", B, G, free, challenged)
	if free < B || free > B+G || challenged < 1 {
		t.Errorf("без вызова %d (ожидалось от %d до %d), с вызовом %d (≥ 1)", free, B, B+G, challenged)
	}
	// Близнец.
	tw := memoryRig(t, l)
	span := time.Duration((n+G-1)/G+1) * time.Second
	t1 := tw.clock.Now()
	for i := 0; i < n; i++ {
		tw.clock.Set(t1.Add(span * time.Duration(i) / time.Duration(n)))
		if rec := tw.send(pathRecovery, fmt.Sprintf("10.%d.%d.1", 1+i/250, i%250), "", ""); rec.Code != http.StatusOK {
			t.Errorf("близнец, источник %d: %d", i, rec.Code)
		}
	}
}

// TestGate_NTF2_79_SourceKeyIsTheAddressAtTheTrustedDepth — сценарий 79: при
// h доверенных прыжках ключ источника — адрес на доверенной глубине; часть
// цепочки дальше неё на ключ не влияет. Близнец — разные адреса на глубине.
func TestGate_NTF2_79_SourceKeyIsTheAddressAtTheTrustedDepth(t *testing.T) {
	l := testLimits()
	l.Global.Burst, l.Global.RatePerSecond = 1000, 1000
	const h = 1
	mk := func(t *testing.T) *rig {
		return newRig(t, l, func(c *testClock) Store { return NewMemoryStore(l, c.Now) }, h)
	}
	t.Logf("h = %d · F = %d", h, l.Source.Free)
	r := mk(t)
	for i := 0; i < l.Source.Free; i++ {
		rec := r.send(pathRecovery, "192.0.2.254", "", "", fmt.Sprintf("10.%d.0.1", i), "198.51.100.77")
		if rec.Code != http.StatusOK {
			t.Fatalf("(а) запрос %d: %d", i+1, rec.Code)
		}
	}
	challengeOf(t, r.send(pathRecovery, "192.0.2.254", "", "", "10.200.0.1", "198.51.100.77"))
	tw := mk(t)
	for i := 0; i < l.Source.Free+1; i++ {
		rec := tw.send(pathRecovery, "192.0.2.254", "", "", "10.9.9.9", fmt.Sprintf("198.51.%d.77", i))
		if rec.Code != http.StatusOK {
			t.Fatalf("(б) запрос %d: %d", i+1, rec.Code)
		}
	}
}

// stubStore — заглушка хранилища: исход задан пробой.
type stubStore struct{ v Verdict }

func (s stubStore) Decide(context.Context, Request) Verdict { return s.v }
func (s stubStore) Close() error                            { return nil }

// TestGate_CX2_28_StoreUnavailableIs503AndTheLaneIsNotCalled — модульная проба
// (CX2-28): хранилище недоступно → 503, code 14, текст `request limiter is
// unavailable`, пустой details, одинаково для обоих путей и любого адреса;
// фиктивная полоса формы вызовов не получила; счётчик +1 на каждый ответ.
// Близнец — хранилище исправно: пропуск.
func TestGate_CX2_28_StoreUnavailableIs503AndTheLaneIsNotCalled(t *testing.T) {
	l := testLimits()
	r := newRig(t, l, func(*testClock) Store { return stubStore{Verdict{Outcome: StoreUnavailable}} }, 0)
	a := r.send(pathRecovery, "198.51.100.30", "", `{"email":"a@example.test"}`)
	b := r.send(pathRecovery, "198.51.100.30", "", `{"email":"z@example.test"}`)
	c := r.send(pathRegister, "198.51.100.30", "", `{"email":"z@example.test","password":"x"}`)
	const want = `{"code":14,"message":"request limiter is unavailable","details":[]}` + "\n"
	for name, rec := range map[string]int{"а": a.Code, "б": b.Code, "в": c.Code} {
		if rec != http.StatusServiceUnavailable {
			t.Errorf("(%s): %d, ожидалось 503", name, rec)
		}
	}
	if a.Body.String() != want {
		t.Errorf("тело 503: %q, ожидалось %q", a.Body.String(), want)
	}
	if a.Body.String() != b.Body.String() || a.Body.String() != c.Body.String() {
		t.Errorf("ответы различаются по адресу или пути: %q · %q · %q", a.Body, b.Body, c.Body)
	}
	if got := r.lane.calls.Load(); got != 0 {
		t.Errorf("полоса получила %d запросов при недоступном хранилище", got)
	}
	if got := r.gate.Stats().StoreUnavailable; got != 3 {
		t.Errorf("счётчик недоступности %d, ожидалось 3", got)
	}
	tw := newRig(t, l, func(*testClock) Store { return stubStore{Verdict{Outcome: Pass}} }, 0)
	if rec := tw.send(pathRecovery, "198.51.100.30", "", ""); rec.Code != http.StatusOK || tw.lane.calls.Load() != 1 {
		t.Errorf("близнец: %d, полоса %d", rec.Code, tw.lane.calls.Load())
	}
	if tw.gate.Stats().StoreUnavailable != 0 {
		t.Error("близнец: счётчик недоступности сдвинулся")
	}
}

// TestGate_ConstructorRefusesAnIncompleteWiring — звено без хранилища, без
// вызова, без оператора адреса или без часов не строится: звено, отвечающее
// «пропустить» за отсутствием части, и есть ветка, которой нет.
func TestGate_ConstructorRefusesAnIncompleteWiring(t *testing.T) {
	l := testLimits()
	pow, _ := NewPoW(testPoWKey, time.Now)
	full := GateConfig{Store: NewMemoryStore(l, time.Now), PoW: pow, Limits: l,
		ClientIP: func(*http.Request) string { return "192.0.2.1" }, Now: time.Now,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	defer func() { _ = full.Store.Close() }()
	if _, err := NewGate(full); err != nil {
		t.Fatalf("полная провязка отвергнута: %v", err)
	}
	for name, mut := range map[string]func(*GateConfig){
		"store":    func(c *GateConfig) { c.Store = nil },
		"pow":      func(c *GateConfig) { c.PoW = nil },
		"clientIP": func(c *GateConfig) { c.ClientIP = nil },
		"now":      func(c *GateConfig) { c.Now = nil },
		"logger":   func(c *GateConfig) { c.Logger = nil },
		"limits":   func(c *GateConfig) { c.Limits = config.AnonMailLimits{} },
	} {
		c := full
		mut(&c)
		if _, err := NewGate(c); err == nil {
			t.Errorf("провязка без %s принята", name)
		}
	}
}
