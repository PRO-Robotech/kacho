// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// f1_token_type_lane_test.go — отсутствие типа и НЕСОВПАДЕНИЕ типа суть разные
// вещи, и различает их только полоса.
//
// На НАШЕЙ полосе производитель типа — мы сами, поэтому отсутствие типа
// означало бы, что мы не выпускаем того, что требуем: отказ. На полосе
// ПРЕЖНЕГО издателя форму заголовка диктует он, и требовать от неё того, чего
// мы у него не проверяли, значило бы поставить работу живого контура на
// непроверенное допущение о третьей стороне — а цена ошибки здесь
// несимметрична обычной: не видимый отказ одного запроса, а отказ КАЖДОГО.
//
// Несовпадающий тип отвергается на ОБЕИХ полосах: послабление выдано
// отсутствию, а не произвольному значению. Уходит оно вместе с записью
// прежнего издателя.
package jwks

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestTokenTypeAbsenceIsToleratedOnlyOnTheLegacyLane(t *testing.T) {
	ours := newKeySet(t)
	ours.addRSA(t, "our-1")
	legacy := newKeySet(t)
	legacy.addRSA(t, "legacy-1")
	v := newVerifier(t, ourPair(ours), legacyPair(legacy))
	now := time.Now()

	t.Run("наша полоса: типа нет — отказ", func(t *testing.T) {
		_, err := v.Verify(context.Background(),
			ours.mintRS(t, "our-1", "", platformClaims("sva-1", now, time.Minute)))
		require.ErrorIs(t, err, ErrInvalidToken,
			"производитель типа на нашей полосе — мы сами; отсутствие типа означало бы, что мы не выпускаем того, что требуем")
	})

	t.Run("наша полоса: тип на месте — принимается", func(t *testing.T) {
		sub, err := v.Verify(context.Background(),
			ours.mintRS(t, "our-1", typAccessJWT, platformClaims("sva-1", now, time.Minute)))
		require.NoError(t, err, "положительный контроль: без него отказ выше зелен на проверяющем, отвергающем всё")
		require.Equal(t, "sva-1", sub)
	})

	t.Run("полоса прежнего издателя: типа нет — принимается", func(t *testing.T) {
		sub, err := v.Verify(context.Background(),
			legacy.mintRS(t, "legacy-1", "", legacyClaims("cid-1", now, time.Minute)))
		require.NoError(t, err,
			"строгость здесь не добавляет защиты — подпись, издатель, адресат и привязка ключа уже отвергли бы чужой токен, — а отказывает КАЖДОМУ обращению живого контура")
		require.Equal(t, "cid-1", sub)
	})

	t.Run("полоса прежнего издателя: НЕСОВПАДАЮЩИЙ тип — отказ", func(t *testing.T) {
		_, err := v.Verify(context.Background(),
			legacy.mintRS(t, "legacy-1", typAccessJWT, legacyClaims("cid-1", now, time.Minute)))
		require.ErrorIs(t, err, ErrInvalidToken,
			"послабление выдано ОТСУТСТВИЮ типа, а не произвольному значению: иначе тип нашего контура сошёл бы за тип чужого")
	})

	t.Run("полоса прежнего издателя: свой тип — принимается", func(t *testing.T) {
		sub, err := v.Verify(context.Background(),
			legacy.mintRS(t, "legacy-1", typJWT, legacyClaims("cid-1", now, time.Minute)))
		require.NoError(t, err, "положительный контроль полосы")
		require.Equal(t, "cid-1", sub)
	})
}
