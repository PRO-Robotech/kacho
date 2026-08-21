// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// f1_constructor_test.go — единственное место, где пробы F1 строят проверяющего.
package jwks

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// newVerifier строит проверяющего по объявленным записям источников.
func newVerifier(t *testing.T, pairs ...issuerPair) *Verifier {
	t.Helper()
	return newVerifierWith(t, nil, pairs...)
}

// newVerifierWith строит проверяющего с читателем авторитета отзыва. nil —
// отзыв не читается ни по одной записи.
//
// Отзыв читается на предъявлении токена НАШЕЙ чеканки; полоса прежнего издателя
// своего поведения не меняет — она вне области F1.
func newVerifierWith(t *testing.T, revoke RevocationReader, pairs ...issuerPair) *Verifier {
	t.Helper()
	require.NotEmpty(t, pairs, "предпосылка пробы: хотя бы одна запись источника")

	sources := make([]KeySetSource, 0, len(pairs))
	for _, p := range pairs {
		sources = append(sources, KeySetSource{
			Issuer:    p.issuer,
			URL:       p.url,
			TokenType: p.typ,
			// Отражает ПРОИЗВОДСТВЕННОЕ соответствие (BuildTokenIssuerBindings):
			// послабление на отсутствующий тип выдано только полосе прежнего
			// издателя. Харнесс, снисходительнее продукта, сделал бы невидимым
			// ровно тот дефект, ради которого его подставляют.
			TolerateAbsentTokenType: p.issuer != testPlatformIss,
			ReadRevocation:          revoke != nil && p.issuer == testPlatformIss,
		})
	}

	var opts []Option
	if revoke != nil {
		opts = append(opts, WithRevocationReader(revoke))
	}
	v, err := New(sources, testAud, opts...)
	require.NoError(t, err, "предпосылка пробы: записи источников законны")
	return v
}

// newTestVerifier — проверяющий полосы ПРЕЖНЕГО издателя в его сегодняшней
// форме: один издатель, один адрес набора, тип токена `JWT`.
//
// Существует ради проб, написанных до того, как издатель стал множеством: их
// предмет — поведение этой полосы, и оно меняться не должно. Пробы F1 строят
// проверяющего через newVerifier.
func newTestVerifier(t *testing.T, keySetURL, aud, issuer string) *Verifier {
	t.Helper()
	v, err := New([]KeySetSource{{Issuer: issuer, URL: keySetURL, TokenType: typJWT}}, aud)
	require.NoError(t, err)
	return v
}

// onlyRecord возвращает единственную запись источника — для проб, которые
// утверждают о состоянии снимка (срок годности, содержимое).
func (v *Verifier) onlyRecord(t *testing.T) *issuerRecord {
	t.Helper()
	require.Len(t, v.records, 1, "предпосылка пробы: у проверяющего ровно одна запись источника")
	for _, rec := range v.records {
		return rec
	}
	return nil
}
