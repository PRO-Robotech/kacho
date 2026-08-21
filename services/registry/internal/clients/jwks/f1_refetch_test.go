// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// f1_refetch_test.go — F1-23: у перезапроса набора два повода, и вынужденный
// (подписант назвал ключ, которого в снимке нет) намеренно ИГНОРИРУЕТ срок
// годности. Снимок бывает свежим и уже неполным — именно этот повод поглощает
// ротацию, случившуюся в середине отсрочки.
package jwks

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/pkg/tokenpolicy"
	"github.com/stretchr/testify/require"
)

// TestF1_23_UnknownKeyIDForcesExactlyOneRefetch — F1-23, несущая половина.
//
// Ключ, ПОЯВИВШИЙСЯ в перезапрошенном наборе, спасает токен. Без этой половины
// перезапрос, не делающий ничего, проходит пробу: «ровно один перезапрос» и
// «токен отвергнут» верны и для реализации, которая ходит впустую.
func TestF1_23_UnknownKeyIDForcesExactlyOneRefetch(t *testing.T) {
	ks := newKeySet(t)
	ks.addRSA(t, "k-1")
	v := newVerifier(t, ourPair(ks))
	at := time.Now()
	atClock(v, &at)

	// Снимок наполнен: одно обращение.
	_, err := v.Verify(context.Background(), ks.mintRS(t, "k-1", typAccessJWT, platformClaims("sva-1", at, time.Minute)))
	require.NoError(t, err)
	require.Equal(t, int32(1), ks.fetches.Load())

	// Ротация В СЕРЕДИНЕ срока годности снимка: снимок свеж и уже неполон.
	ks.addRSA(t, "k-2")
	at = at.Add(defaultMinRefresh + time.Second)

	sub, err := v.Verify(context.Background(), ks.mintRS(t, "k-2", typAccessJWT, platformClaims("sva-2", at, time.Minute)))
	require.NoError(t, err,
		"ключ, появившийся в перезапрошенном наборе, обязан спасти токен — иначе вынужденный перезапрос не делает ничего")
	require.Equal(t, "sva-2", sub)
	require.Equal(t, int32(2), ks.fetches.Load(), "ровно ОДИН вынужденный перезапрос")
}

// TestF1_23_UnknownKeyIDThatNeverAppearsIsRejected — вторая половина: ключ не
// появился ⇒ токен отвергнут, и второго обращения за него не платится.
func TestF1_23_UnknownKeyIDThatNeverAppearsIsRejected(t *testing.T) {
	ks := newKeySet(t)
	ks.addRSA(t, "k-1")
	v := newVerifier(t, ourPair(ks))
	at := time.Now()
	atClock(v, &at)

	_, err := v.Verify(context.Background(), ks.mintRS(t, "k-1", typAccessJWT, platformClaims("sva-1", at, time.Minute)))
	require.NoError(t, err)

	at = at.Add(defaultMinRefresh + time.Second)
	stranger := unsigned(tokenpolicy.AlgRS256, "k-absent", typAccessJWT, platformClaims("sva-9", at, time.Minute))
	_, err = v.Verify(context.Background(), stranger)
	require.ErrorIs(t, err, ErrInvalidToken)
	require.Equal(t, int32(2), ks.fetches.Load(), "ровно один вынужденный перезапрос, и он не помог")
}

// TestF1_23_RepeatedUnknownKeyIDsAreBoundedByTheDeclaredInterval — F1-23,
// половина про цену.
//
// Интервал берётся из объявленной политики: идентификатор ключа читается ДО
// проверки подписи, поэтому поток выдуманных идентификаторов иначе превращается
// в поток обращений к публикатору.
func TestF1_23_RepeatedUnknownKeyIDsAreBoundedByTheDeclaredInterval(t *testing.T) {
	require.Equal(t, tokenpolicy.UnknownKeyIDRefetchInterval, defaultMinRefresh,
		"интервал вынужденного перезапроса обязан браться из объявленной политики, а не заводиться на месте")

	ks := newKeySet(t)
	ks.addRSA(t, "k-1")
	v := newVerifier(t, ourPair(ks))
	at := time.Now()
	atClock(v, &at)

	_, err := v.Verify(context.Background(), ks.mintRS(t, "k-1", typAccessJWT, platformClaims("sva-1", at, time.Minute)))
	require.NoError(t, err)
	require.Equal(t, int32(1), ks.fetches.Load())

	// Наплыв неизвестных идентификаторов ВНУТРИ одного окна интервала.
	at = at.Add(tokenpolicy.UnknownKeyIDRefetchInterval / 2)
	for i := 0; i < 20; i++ {
		tok := unsigned(tokenpolicy.AlgRS256, fmt.Sprintf("made-up-%d", i), typAccessJWT,
			platformClaims("sva-x", at, time.Minute))
		_, verr := v.Verify(context.Background(), tok)
		require.Error(t, verr)
	}
	require.Equal(t, int32(1), ks.fetches.Load(),
		"внутри окна интервала наплыв неизвестных идентификаторов не стоит ни одного обращения к источнику")

	// Положительный контроль: за окном интервала перезапрос всё-таки происходит —
	// иначе «ни одного обращения» верно и для реализации, переставшей ходить
	// вовсе, и законная ротация не подхватывалась бы никогда.
	at = at.Add(tokenpolicy.UnknownKeyIDRefetchInterval + time.Second)
	ks.addRSA(t, "k-2")
	sub, err := v.Verify(context.Background(), ks.mintRS(t, "k-2", typAccessJWT, platformClaims("sva-2", at, time.Minute)))
	require.NoError(t, err, "за окном интервала ротация обязана подхватываться")
	require.Equal(t, "sva-2", sub)
	require.Equal(t, int32(2), ks.fetches.Load())
}
