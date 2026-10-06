// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package dkim_test

// harness_test.go — провязка фикстуры к испытуемому. Единственный файл
// фикстуры, который называет пакет `dkim`: фикстура (fixture_test.go) и её
// самопроверка собираются и исполняются без него.
//
// Что пробы требуют от пакета `dkim` (Р19 «Подпись»; замысел §12а «Подпись»):
//
//	// PairSource — источник пары, которой идёт подпись: *dnscheck.Guard
//	// (пара меняется атомарно на такте перепроверки).
//	type PairSource interface{ Pair() dkimkey.Pair }
//	// NewSigner — пустой домен либо nil-источник — ошибка.
//	func NewSigner(domain string, pairs PairSource) (*Signer, error)
//	// Sign — подпись окончательного письма текущей парой источника:
//	// DKIM-Signature в начало, байты письма не меняются. Нулевая пара —
//	// ошибка: письма без подписи нет.
//	func (s *Signer) Sign(msg []byte) ([]byte, error)
//
// Подпись — crypto/rsa (SignPKCS1v15, SHA-256), канонизация relaxed/relaxed,
// теги v, a=rsa-sha256, c, d, s, h, bh, b; h= — from дважды, to, subject,
// date, message-id, mime-version, content-type и list-unsubscribe, если
// заголовок есть.

import (
	"testing"

	"github.com/PRO-Robotech/kacho/services/notify/internal/dkim"
)

var _ dkim.PairSource = (*switchablePairs)(nil)

// newSigner — подписчик домена fromDomain над источником pairs.
func newSigner(t testing.TB, pairs dkim.PairSource) *dkim.Signer {
	t.Helper()
	s, err := dkim.NewSigner(fromDomain, pairs)
	if err != nil {
		t.Fatalf("dkim.NewSigner(%q, источник пары): %v", fromDomain, err)
	}
	return s
}

// sign — подпись письма; ошибка подписи — красный пробы.
func sign(t testing.TB, s *dkim.Signer, msg []byte) []byte {
	t.Helper()
	out, err := s.Sign(msg)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	return out
}

// newSignerErr — конструктор как есть: для проб отказа конструктора.
func newSignerErr(domain string, pairs dkim.PairSource) (*dkim.Signer, error) {
	return dkim.NewSigner(domain, pairs)
}
