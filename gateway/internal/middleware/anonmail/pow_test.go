// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package anonmail

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"testing"
	"time"
)

var testPoWKey = bytes.Repeat([]byte{0x5a}, 32)

// solve — перебор nonce до нужного числа ведущих нулевых битов; так же ищет
// решатель консоли (З10).
func solve(t testing.TB, challenge string, bits int) string {
	t.Helper()
	for i := 0; i < 1<<26; i++ {
		n := strconv.Itoa(i)
		if leadingZeroBits(sha256.Sum256([]byte(challenge+":"+n))) >= bits {
			return n
		}
	}
	t.Fatalf("решение %d бит не найдено", bits)
	return ""
}

// unsolved — nonce, НЕ дающий нужного числа битов: отрицательный близнец.
func unsolved(t testing.TB, challenge string, bits int) string {
	t.Helper()
	for i := 0; ; i++ {
		n := strconv.Itoa(i)
		if leadingZeroBits(sha256.Sum256([]byte(challenge+":"+n))) < bits {
			return n
		}
	}
}

func fixedClock(t time.Time) func() time.Time { return func() time.Time { return t } }

// TestPoW_NTF2_62_VerifyRefusesEachBrokenProofAndAcceptsTheTwin — проверка
// доказательства (З9, NTF2-62): разбор, подпись постоянным временем, срок по
// часам звена, число ведущих нулевых битов. Каждый отрицательный вариант
// меняет ровно один факт против верного близнеца.
func TestPoW_NTF2_62_VerifyRefusesEachBrokenProofAndAcceptsTheTwin(t *testing.T) {
	t0 := time.Unix(1_800_000_000, 0)
	m, err := NewPoW(testPoWKey, fixedClock(t0))
	if err != nil {
		t.Fatal(err)
	}
	ch, err := m.Mint(8)
	if err != nil {
		t.Fatal(err)
	}
	if ch.Bits != 8 || !ch.ExpiresAt.Equal(t0.Add(ChallengeTTL)) {
		t.Fatalf("вызов %+v: ожидались 8 бит и срок t0 + %s", ch, ChallengeTTL)
	}
	nonce := solve(t, ch.Token, 8)

	// Близнец — верное доказательство.
	p, err := m.Verify(ch.Token + ":" + nonce)
	if err != nil {
		t.Fatalf("верное доказательство отвергнуто: %v", err)
	}
	if p.Bits != 8 || !p.ExpiresAt.Equal(ch.ExpiresAt) || p.ID == ([16]byte{}) {
		t.Errorf("доказательство %+v: биты, срок или id не те, что у вызова", p)
	}

	// (а) nonce без нужного числа битов.
	if _, err := m.Verify(ch.Token + ":" + unsolved(t, ch.Token, 8)); !errors.Is(err, errProofInsufficientWork) {
		t.Errorf("(а) nonce без 8 нулевых битов: %v", err)
	}
	// (б) истёкший вызов: в момент expiresAt и позже — отказ, на секунду
	// раньше — пропуск.
	late, _ := NewPoW(testPoWKey, fixedClock(ch.ExpiresAt))
	if _, err := late.Verify(ch.Token + ":" + nonce); !errors.Is(err, errProofExpired) {
		t.Errorf("(б) в момент expiresAt: %v", err)
	}
	early, _ := NewPoW(testPoWKey, fixedClock(ch.ExpiresAt.Add(-time.Second)))
	if _, err := early.Verify(ch.Token + ":" + nonce); err != nil {
		t.Errorf("(б) близнец за секунду до срока: %v", err)
	}
	// (г) вызов, изменённый в одном знаке.
	b := []byte(ch.Token)
	if b[10] == 'A' {
		b[10] = 'B'
	} else {
		b[10] = 'A'
	}
	if _, err := m.Verify(string(b) + ":" + solve(t, string(b), 8)); !errors.Is(err, errProofSignature) {
		t.Errorf("(г) вызов, изменённый в одном знаке: %v", err)
	}
	// Чужой ключ — та же подпись не сходится.
	other, _ := NewPoW(bytes.Repeat([]byte{0x11}, 32), fixedClock(t0))
	if _, err := other.Verify(ch.Token + ":" + nonce); !errors.Is(err, errProofSignature) {
		t.Errorf("чужой ключ: %v", err)
	}
	// Форма заголовка.
	for _, h := range []string{"", ":", ch.Token, ch.Token + ":", ":" + nonce, "!!!:1",
		ch.Token + ":" + nonce + " ", ch.Token + ":" + string(bytes.Repeat([]byte("1"), 65))} {
		if _, err := m.Verify(h); !errors.Is(err, errProofMalformed) {
			t.Errorf("заголовок %q: %v, ожидался отказ формы", h, err)
		}
	}
}

// TestPoW_ChallengeWireForm — вызов: base64url(v1 ‖ id[16] ‖ expiresAt ‖ bits ‖
// HMAC-SHA256(k, v1 ‖ id ‖ expiresAt ‖ bits)) без дополнения; каждый вызов —
// свой id.
func TestPoW_ChallengeWireForm(t *testing.T) {
	m, _ := NewPoW(testPoWKey, fixedClock(time.Unix(1_800_000_000, 0)))
	a, _ := m.Mint(12)
	c, _ := m.Mint(12)
	raw, err := base64.RawURLEncoding.DecodeString(a.Token)
	if err != nil {
		t.Fatalf("вызов не base64url без дополнения: %v", err)
	}
	if len(raw) != 1+16+8+1+32 || raw[0] != 1 || raw[25] != 12 {
		t.Errorf("тело вызова %d байт, версия %d, биты %d", len(raw), raw[0], raw[25])
	}
	if a.Token == c.Token {
		t.Error("два вызова совпали — id не случаен")
	}
}

// TestPoW_KeyShorterThan32BytesIsRefused — ключ короче 32 байт не принимается
// (З9; страж старта судит ту же границу на файле).
func TestPoW_KeyShorterThan32BytesIsRefused(t *testing.T) {
	if _, err := NewPoW(bytes.Repeat([]byte{1}, 31), time.Now); err == nil {
		t.Error("ключ 31 байт принят")
	}
	if _, err := NewPoW(bytes.Repeat([]byte{1}, 32), time.Now); err != nil {
		t.Errorf("ключ 32 байт отвергнут: %v", err)
	}
	if _, err := NewPoW(bytes.Repeat([]byte{1}, 32), nil); err == nil {
		t.Error("часы nil приняты")
	}
}

// powVectors — общий файл векторов края и решателя консоли (З10, CX2-30 (в)).
type powVectors struct {
	ChallengeTTLSeconds int `json:"challengeTtlSeconds"`
	SolverBudgetSeconds int `json:"solverBudgetSeconds"`
	Vectors             []struct {
		Challenge       string `json:"challenge"`
		Nonce           string `json:"nonce"`
		LeadingZeroBits int    `json:"leadingZeroBits"`
		Bits            int    `json:"bits"`
		Accepted        bool   `json:"accepted"`
	} `json:"vectors"`
}

// TestPoW_SharedVectors — проверка края судится общим файлом векторов: число
// ведущих нулевых битов SHA-256(challenge ":" nonce) и исход «достаточно ли»
// совпадают с файлом; срок вызова кода равен сроку файла; бюджет решателя
// консоли меньше срока вызова. Решатель консоли читает тот же файл.
func TestPoW_SharedVectors(t *testing.T) {
	raw, err := os.ReadFile("testdata/pow_vectors.json")
	if err != nil {
		t.Fatalf("файл векторов: %v", err)
	}
	var v powVectors
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("разбор векторов: %v", err)
	}
	if time.Duration(v.ChallengeTTLSeconds)*time.Second != ChallengeTTL {
		t.Errorf("срок вызова в файле %d с, в коде %s", v.ChallengeTTLSeconds, ChallengeTTL)
	}
	if v.SolverBudgetSeconds <= 0 || v.SolverBudgetSeconds >= v.ChallengeTTLSeconds {
		t.Errorf("бюджет решателя %d с не меньше срока вызова %d с", v.SolverBudgetSeconds, v.ChallengeTTLSeconds)
	}
	accepted, refused := 0, 0
	for i, vec := range v.Vectors {
		got := leadingZeroBits(sha256.Sum256([]byte(vec.Challenge + ":" + vec.Nonce)))
		if got != vec.LeadingZeroBits {
			t.Errorf("вектор %d: ведущих нулевых битов %d, в файле %d", i, got, vec.LeadingZeroBits)
		}
		if ok := got >= vec.Bits; ok != vec.Accepted {
			t.Errorf("вектор %d: исход %v, в файле %v", i, ok, vec.Accepted)
		}
		if vec.Accepted {
			accepted++
		} else {
			refused++
		}
	}
	t.Logf("векторов %d: принимаемых %d, отвергаемых %d", len(v.Vectors), accepted, refused)
	if accepted == 0 || refused == 0 {
		t.Fatal("в файле векторов нет обеих сторон: проба не отличила бы решатель, принимающий всё")
	}
}

// TestLeadingZeroBits — счёт ведущих нулевых битов на границах байтов.
func TestLeadingZeroBits(t *testing.T) {
	var h [32]byte
	if got := leadingZeroBits(h); got != 256 {
		t.Errorf("нулевой хеш: %d", got)
	}
	h[0] = 0x80
	if got := leadingZeroBits(h); got != 0 {
		t.Errorf("0x80…: %d", got)
	}
	h[0], h[1] = 0, 0x01
	if got := leadingZeroBits(h); got != 15 {
		t.Errorf("0x00 0x01…: %d", got)
	}
}
