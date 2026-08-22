// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package jwks

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// mintWithHeader — токен с ПРОИЗВОЛЬНЫМИ полями заголовка.
//
// Харнесс собирает заголовок из трёх известных полей и добавить в него ничего не
// даёт; проверить обработку неизвестного нечем by construction, поэтому сборка
// здесь своя, а не «как у всех».
func (ks *keySet) mintWithHeader(t *testing.T, kid string, hdr map[string]any,
	claims map[string]any) string {
	t.Helper()
	key, ok := ks.rsaKeys[kid]
	require.Truef(t, ok, "в наборе нет ключа RSA %q", kid)

	full := map[string]any{"alg": "RS256", "kid": kid, "typ": typAccessJWT}
	for k, v := range hdr {
		full[k] = v
	}
	hb, err := json.Marshal(full)
	require.NoError(t, err)
	cb, err := json.Marshal(claims)
	require.NoError(t, err)

	si := b64u(hb) + "." + b64u(cb)
	sum := sha256.Sum256([]byte(si))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	require.NoError(t, err)
	return si + "." + b64u(sig)
}

// TestCriticalHeadersPairOfOppositePolarity — два требования об одном предмете,
// смотрящие в РАЗНЫЕ стороны, проверяются РЯДОМ (#902).
//
// RFC 7515 §4.1.11 требует ОТКАЗА от неизвестного, помеченного обязательным;
// RFC 7519 (EID 8060) требует ПРИНЯТИЯ неизвестного, не помеченного. Ужесточение
// первого без второго ломает совместимость: всякий издатель вправе добавить своё
// поле, и токены перестали бы приниматься. Послабление второго без первого
// открывает приём токена, чьё условие мы не исполнили.
//
// Поэтому обе стороны стоят в одной пробе: разнесённые, они разойдутся на первой
// правке, и разойдётся молча именно та, которую забудут.
func TestCriticalHeadersPairOfOppositePolarity(t *testing.T) {
	ks := newKeySet(t)
	ks.addRSA(t, "k-1")
	v := newVerifier(t, ourPair(ks))
	now := time.Now()
	claims := platformClaims("sva-1", now, time.Minute)

	t.Run("помеченный обязательным неизвестный — ОТКАЗ", func(t *testing.T) {
		_, err := v.Verify(context.Background(), ks.mintWithHeader(t, "k-1",
			map[string]any{"crit": []any{"kacho-unknown-ext"}, "kacho-unknown-ext": 1},
			claims))
		require.ErrorIs(t, err, ErrInvalidToken,
			"отправитель заявил, что без понимания этого параметра токен принимать "+
				"нельзя; принять его значит согласиться с условием, которого не проверил")
	})

	t.Run("НЕ помеченный неизвестный — ПРИНИМАЕТСЯ", func(t *testing.T) {
		sub, err := v.Verify(context.Background(), ks.mintWithHeader(t, "k-1",
			map[string]any{"kacho-unknown-ext": 1}, claims))
		require.NoError(t, err,
			"игнорирование не помеченного неизвестного и есть то, на чём держится "+
				"совместимость: без этой половины отказ выше означал бы запрет "+
				"любого расширения заголовка")
		require.Equal(t, "sva-1", sub)
	})

	t.Run("положительный контроль: заголовок без расширений", func(t *testing.T) {
		sub, err := v.Verify(context.Background(),
			ks.mintWithHeader(t, "k-1", nil, claims))
		require.NoError(t, err,
			"без него обе ветки выше зеленели бы на проверяющем, отвергающем всё")
		require.Equal(t, "sva-1", sub)
	})

	t.Run("пустой crit — законный вход", func(t *testing.T) {
		sub, err := v.Verify(context.Background(), ks.mintWithHeader(t, "k-1",
			map[string]any{"crit": []any{}}, claims))
		require.NoError(t, err,
			"требование касается только того, что отправитель ЯВНО пометил "+
				"обязательным; пустой перечень не помечает ничего")
		require.Equal(t, "sva-1", sub)
	})
}
