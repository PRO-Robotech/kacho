// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// f2_client_assertion_is_not_an_access_token_test.go — разделение двух видов
// подписанного, направление ВТОРОЕ (приёмка F2, §11 F, сценарий F2-35), на
// третьей поверхности предъявления: плоскости данных реестра
// (services/registry/internal/dataplane, вход `h.verifier.Verify`).
//
// Две другие поверхности — нативная gRPC края и REST края — проверяются своей
// пробой у края. Разнесение не косметическое: край и реестр принимают токен
// РАЗНЫМИ реализациями, и проба, поставленная одной, о другой не утверждает
// ничего. Реализация, переставшая отвергать, останется зелёной у соседа.
//
// # Что здесь спрашивается
//
// Утверждение клиента, подписанное ключом клиента, поданное как токен доступа.
// Признаки снимаются по одному, и на каждой ступени требуется отказ от
// оставшихся: одиночный вход не отличает «отвергнуто несколькими признаками»
// от «отвергнуто одним, а остальные не работают вовсе».
package jwks

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/pkg/tokenpolicy"
)

// f2ClientID — идентификатор клиента: утверждение называет им и издателя, и
// субъекта.
const f2ClientID = "uoc_0123456789abcdefg"

// f2SignAssertion собирает утверждение клиента ключом КЛИЕНТА — тем, которого в
// наборе проверочных ключей платформы нет и быть не может.
//
// Ключ порождается здесь, а не берётся из набора: возьми проба ключ набора,
// третий признак разделения (чей ключ подписал) исчез бы из неё вместе с
// предметом, и последняя ступень лестницы стала бы зелёной ни о чём.
func f2SignAssertion(t *testing.T, issuer, typ, kid string) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	hdr := map[string]any{"alg": "ES256", "kid": kid}
	if typ != "" {
		hdr["typ"] = typ
	}
	now := time.Now()
	payload := map[string]any{
		"iss": issuer,
		"sub": f2ClientID,
		// Адресат утверждения — идентификатор НАШЕГО издателя, а не адресат
		// ресурсной поверхности. Это второй признак, и он остаётся на месте.
		"aud": testPlatformIss,
		"iat": now.Unix(),
		"exp": now.Add(2 * time.Minute).Unix(),
		"jti": "jti-f2-35",
	}
	hb, err := json.Marshal(hdr)
	require.NoError(t, err)
	pb, err := json.Marshal(payload)
	require.NoError(t, err)

	si := b64u(hb) + "." + b64u(pb)
	sum := sha256.Sum256([]byte(si))
	r, s, err := ecdsa.Sign(rand.Reader, key, sum[:])
	require.NoError(t, err)
	return si + "." + b64u(append(padTo(r.Bytes(), 32), padTo(s.Bytes(), 32)...))
}

// TestF2_35_ClientAssertionIsNotAnAccessTokenOnTheRegistryDataPlane — F2-35,
// поверхность третья.
//
// # Что показала инъекция — измерено, а не предположено
//
// Снятие ЛЮБОГО ОДНОГО признака проверяющего не роняет эту пробу: снимешь
// проверку типа — отвергает подпись; снимешь подпись — отвергает адресат;
// снимешь адресат — отвергает подпись. Красной проба становится только когда
// сняты ВСЕ ТРИ разом. Это и есть проверяемая форма утверждения «ни один
// признак не является единственным»: не заявление в комментарии, а поведение,
// воспроизводимое инъекцией.
//
// Следствие для чтения: проба УТВЕРЖДАЕТ СОСТАВ, а не отдельный признак.
// Изоляция признака типа на этой поверхности живёт рядом, в
// f1_token_type_lane_test.go: там вход подписан ПРАВИЛЬНЫМ ключом и адресован
// правильной поверхности, поэтому решает ровно тип. Собрать такую изоляцию
// здесь нельзя by construction — у подлинного утверждения клиента подпись
// платформы не сходится никогда.
func TestF2_35_ClientAssertionIsNotAnAccessTokenOnTheRegistryDataPlane(t *testing.T) {
	ours := newKeySet(t)
	ours.addEC(t, "our-1")
	v := newVerifier(t, ourPair(ours))
	now := time.Now()

	// Ступень 0 — утверждение как есть: издателем названо само себя.
	t.Run("утверждение клиента как есть", func(t *testing.T) {
		_, err := v.Verify(context.Background(),
			f2SignAssertion(t, f2ClientID, tokenpolicy.TokenTypeClientAssertion, "client-key-1"))
		require.ErrorIs(t, err, ErrInvalidToken)
	})

	// Ступень 1 — снято всё, что к трём признакам не относится: издателем
	// назван наш, а ключом — ключ платформы, который проверяющий действительно
	// разрешит. Остаются признаки: тип и подпись.
	t.Run("исправлены издатель и ключ", func(t *testing.T) {
		_, err := v.Verify(context.Background(),
			f2SignAssertion(t, testPlatformIss, tokenpolicy.TokenTypeClientAssertion, "our-1"))
		require.ErrorIs(t, err, ErrInvalidToken)
	})

	// Ступень 2 — снят и признак типа: заголовок объявляет тип токена доступа.
	// Остаются адресат и подпись, и снять их нечем: адресат утверждения — наш
	// издатель, а не эта поверхность, а закрытой половины ключа платформы у
	// клиента нет.
	t.Run("исправлены издатель, ключ и тип", func(t *testing.T) {
		_, err := v.Verify(context.Background(),
			f2SignAssertion(t, testPlatformIss, tokenpolicy.TokenTypeAccess, "our-1"))
		require.ErrorIs(t, err, ErrInvalidToken)
	})

	// Положительный контроль ЭТОЙ поверхности. Без него всё выше зелено на
	// проверяющем, отвергающем любой вход, — включая законный токен доступа.
	t.Run("законный токен доступа принимается", func(t *testing.T) {
		sub, err := v.Verify(context.Background(),
			ours.mintES(t, "our-1", typAccessJWT, platformClaims("sva-1", now, time.Minute)))
		require.NoError(t, err)
		require.Equal(t, "sva-1", sub)
	})
}
