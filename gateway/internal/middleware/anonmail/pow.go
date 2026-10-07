// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package anonmail

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/bits"
	"strings"
	"time"
)

// ChallengeTTL — срок вызова proof-of-work (З9, §8 «Константы кода»): верхняя
// граница решения на 24 битах в браузере с запасом. Ручкой не является и в
// ключи Р8 не входит; бюджет решателя консоли меньше его (общий файл векторов
// testdata/pow_vectors.json, проба TestPoW_SharedVectors).
const ChallengeTTL = 5 * time.Minute

// Форма вызова: v1 ‖ id[16] ‖ expiresAt[8, unix-секунды, big-endian] ‖ bits[1]
// ‖ HMAC-SHA256(k, всё предыдущее)[32], base64url без дополнения.
const (
	challengeVersion  = 1
	challengeIDLen    = 16
	challengeSignedAt = 1 + challengeIDLen + 8 + 1
	challengeLen      = challengeSignedAt + sha256.Size
	// nonceMaxLen — предел длины nonce: решатель ищет десятичное число, и
	// строка длиннее — не решение, а нагрузка на хеш.
	nonceMaxLen = 64
	// powKeyMinBytes — нижняя граница ключа подписи (З9); та же граница у
	// стража старта (`config.ReadAnonMailPoWKey`).
	powKeyMinBytes = 32
)

// Отказы проверки доказательства. Любой из них — новый вызов (NTF2-62); до
// службы запрос не доходит.
var (
	errProofMalformed        = errors.New("anonmail: proof header is malformed")
	errProofSignature        = errors.New("anonmail: challenge signature does not verify")
	errProofExpired          = errors.New("anonmail: challenge has expired")
	errProofInsufficientWork = errors.New("anonmail: proof does not carry the required work")
)

// IssuedChallenge — выданный вызов.
type IssuedChallenge struct {
	Token     string
	Bits      int
	ExpiresAt time.Time
}

// Proof — доказательство, прошедшее чистую проверку. Одноразовость (`ID`)
// проверяет хранилище в транзакции решения.
type Proof struct {
	ID        [challengeIDLen]byte
	Bits      int
	ExpiresAt time.Time
}

// PoW — выпуск и чистая проверка вызовов края (З9). Проверка локальна, без
// обращения к службе (CX2-11 (в)).
type PoW struct {
	key  []byte
	now  func() time.Time
	rand io.Reader
}

// NewPoW строит выпуск вызовов на ключе подписи. Ключ короче 32 байт — отказ:
// подпись на коротком ключе подбирается.
func NewPoW(key []byte, now func() time.Time) (*PoW, error) {
	if len(key) < powKeyMinBytes {
		return nil, fmt.Errorf("anonmail: proof-of-work key is %d bytes, the floor is %d", len(key), powKeyMinBytes)
	}
	if now == nil {
		return nil, errors.New("anonmail: proof-of-work clock is nil")
	}
	return &PoW{key: append([]byte(nil), key...), now: now, rand: rand.Reader}, nil
}

// Mint выпускает вызов сложности bits со сроком ChallengeTTL от часов звена.
func (p *PoW) Mint(difficulty int) (IssuedChallenge, error) {
	if difficulty < 1 || difficulty > 255 {
		return IssuedChallenge{}, fmt.Errorf("anonmail: difficulty %d is outside 1..255", difficulty)
	}
	exp := p.now().Add(ChallengeTTL).Truncate(time.Second)
	buf := make([]byte, challengeLen)
	buf[0] = challengeVersion
	if _, err := io.ReadFull(p.rand, buf[1:1+challengeIDLen]); err != nil {
		return IssuedChallenge{}, fmt.Errorf("anonmail: challenge id: %w", err)
	}
	binary.BigEndian.PutUint64(buf[1+challengeIDLen:], uint64(exp.Unix())) // #nosec G115 -- срок после эпохи
	buf[challengeSignedAt-1] = byte(difficulty)
	copy(buf[challengeSignedAt:], p.sign(buf[:challengeSignedAt]))
	return IssuedChallenge{Token: base64.RawURLEncoding.EncodeToString(buf), Bits: difficulty, ExpiresAt: exp}, nil
}

func (p *PoW) sign(b []byte) []byte {
	m := hmac.New(sha256.New, p.key)
	_, _ = m.Write(b)
	return m.Sum(nil)
}

// Verify — чистая проверка заголовка `X-Kacho-Proof: <challenge>:<nonce>`:
// разбор, подпись (постоянное время), срок по часам звена, число ведущих
// нулевых битов SHA-256(challenge ":" nonce) ≥ bits вызова. Одноразовость здесь
// НЕ проверяется: её проверяет хранилище внутри транзакции решения (CX2-43).
func (p *PoW) Verify(header string) (Proof, error) {
	i := strings.LastIndexByte(header, ':')
	if i <= 0 || i == len(header)-1 {
		return Proof{}, errProofMalformed
	}
	token, nonce := header[:i], header[i+1:]
	if !validNonce(nonce) {
		return Proof{}, errProofMalformed
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil || len(raw) != challengeLen || raw[0] != challengeVersion {
		return Proof{}, errProofMalformed
	}
	if !hmac.Equal(raw[challengeSignedAt:], p.sign(raw[:challengeSignedAt])) {
		return Proof{}, errProofSignature
	}
	exp := time.Unix(int64(binary.BigEndian.Uint64(raw[1+challengeIDLen:])), 0) // #nosec G115 -- подписанное значение края
	if !p.now().Before(exp) {
		return Proof{}, errProofExpired
	}
	difficulty := int(raw[challengeSignedAt-1])
	if leadingZeroBits(sha256.Sum256([]byte(token+":"+nonce))) < difficulty {
		return Proof{}, errProofInsufficientWork
	}
	var out Proof
	copy(out.ID[:], raw[1:1+challengeIDLen])
	out.Bits = difficulty
	out.ExpiresAt = exp
	return out, nil
}

// validNonce — nonce: непустой, не длиннее nonceMaxLen, только [0-9A-Za-z_-].
func validNonce(s string) bool {
	if s == "" || len(s) > nonceMaxLen {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && c != '-' && c != '_' {
			return false
		}
	}
	return true
}

// leadingZeroBits — число ведущих нулевых битов хеша.
func leadingZeroBits(h [sha256.Size]byte) int {
	n := 0
	for _, b := range h {
		if b == 0 {
			n += 8
			continue
		}
		return n + bits.LeadingZeros8(b)
	}
	return n
}
