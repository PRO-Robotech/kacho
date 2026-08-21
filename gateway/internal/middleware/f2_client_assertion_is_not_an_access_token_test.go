// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware_test

// f2_client_assertion_is_not_an_access_token_test.go — разделение двух видов
// подписанного, направление ВТОРОЕ (приёмка F2, §11 F, сценарий F2-35), на двух
// поверхностях предъявления из трёх: нативной gRPC края и REST края.
//
// Третья — плоскость данных реестра — проверяется своим проверяющим и своей
// пробой (services/registry/internal/clients/jwks). Разнесение не косметическое:
// у края и у реестра РАЗНЫЕ реализации приёма, и проба, поставленная одной,
// ничего не утверждает о другой.
//
// # Почему направление второе нужно отдельно от первого
//
// Первое направление (токен доступа, поданный как утверждение) закрывается
// проверяющим утверждение. Оно ничего не говорит про обратное: проба одного
// направления зелена при сломанном другом. Здесь спрашивается обратное —
// утверждение клиента, подписанное ключом клиента, поданное как токен доступа.
//
// # Почему предъявляются обе поверхности края, хотя проверяющий один
//
// Экземпляр проверяющего край строит один и переиспользует его на обеих своих
// поверхностях — но ЦЕПОЧКИ вокруг него разные: gRPC читает удостоверение из
// метаданных вызова и отвечает кодом, REST читает заголовок запроса и отвечает
// состоянием ответа. Поверхность, на которой отказ теряется по дороге наружу,
// неотличима снаружи от поверхности, которая ничего не проверяет.

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
	"github.com/PRO-Robotech/kacho/pkg/tokenpolicy"
)

// f2ClientID — идентификатор клиента, которым утверждение называет само себя:
// у него издатель и субъект совпадают и оба равны ему.
const f2ClientID = "uoc_0123456789abcdefg"

// f2ClientKey — ключ КЛИЕНТА: он зарегистрирован в реестре клиентов iam и в
// наборе проверочных ключей платформы его нет. Ровно это и есть третий признак
// разделения — чей ключ подписал.
type f2ClientKey struct {
	priv *ecdsa.PrivateKey
	kid  string
}

func newF2ClientKey(t *testing.T) f2ClientKey {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	return f2ClientKey{priv: k, kid: "client-key-1"}
}

// sign собирает утверждение клиента: тип утверждения в заголовке, издатель и
// субъект — сам клиент, адресат — идентификатор нашего издателя.
//
// kidOverride позволяет пробе назвать ЧУЖОЙ идентификатор ключа — тот, что
// платформа публикует своим. Это сознательная уступка нападающему: подписи он
// подделать не может, но заставить нас взять для проверки наш собственный ключ
// — вполне, и проверить надо именно это.
func (k f2ClientKey) sign(t *testing.T, issuer, kidOverride string) string {
	t.Helper()
	now := time.Now()
	tok := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
		"iss": issuer,
		"sub": f2ClientID,
		"aud": []any{testIssuer},
		"iat": now.Unix(),
		"exp": now.Add(2 * time.Minute).Unix(),
		"jti": "jti-f2-35",
	})
	tok.Header["typ"] = tokenpolicy.TokenTypeClientAssertion
	kid := k.kid
	if kidOverride != "" {
		kid = kidOverride
	}
	tok.Header["kid"] = kid
	raw, err := tok.SignedString(k.priv)
	require.NoError(t, err)
	return raw
}

// f2Assertions — три входа одного направления, от самого честного к самому
// щедрому к нападающему. Все три обязаны быть отвергнуты.
//
// Лестница нужна потому, что одиночный вход не отличает «отвергнуто несколькими
// признаками» от «отвергнуто одним, а остальные не работают вовсе»: убери
// завтра тот единственный — и проба останется зелёной ровно до первого
// настоящего предъявления.
func f2Assertions(t *testing.T, fix *jwksFixture) map[string]string {
	t.Helper()
	key := newF2ClientKey(t)
	return map[string]string{
		// Утверждение как есть: издателем названо само себя.
		"утверждение клиента как есть": key.sign(t, f2ClientID, ""),
		// Издатель исправлен на нашего: снят признак, к трём не относящийся, —
		// чтобы отказ ниже нельзя было объяснить одним лишь несовпадением
		// издателя.
		"исправлен издатель": key.sign(t, testIssuer, ""),
		// И ключ назван НАШИМ: проверяющий возьмёт из набора платформы тот
		// самый ключ, чей идентификатор назвал предъявитель. Остаётся подпись —
		// её закрытой половины у клиента нет.
		"исправлен издатель, назван ключ платформы": key.sign(t, testIssuer, fix.kid),
	}
}

// TestF2_35_ClientAssertionIsNotAnAccessTokenOnTheGRPCEdge — F2-35, поверхность
// первая: нативная gRPC края (main.go: authInterceptor.Unary()).
//
// # Что показала инъекция — измерено, а не предположено
//
// Подменённый на всеприемлющий проверяющий роняет ВСЕ отрицательные ступени
// обеих поверхностей края и оставляет зелёными оба положительных контроля.
// Значит проба меряет решение проверяющего, а не форму вызова вокруг него.
//
// Снятие же ОДНОЙ проверки внутри проверяющего пробу не роняет: у каждой
// ступени отвергающих несколько. Это и есть проверяемая форма утверждения «ни
// один признак не является единственным» — поведение, воспроизводимое
// инъекцией, а не заявление в комментарии.
//
// # Что проба утверждать НЕ вправе, и почему это сказано вслух
//
// Признак типа на этой поверхности сегодня не участвует: объявленный тип край
// читает ровно в одном месте и ровно ради одного отказа — доказательства
// владения, поданного вместо токена. Своего типа он не требует и отсутствие
// типа принимает. Проба поэтому утверждает СОСТАВ отказа, а не тип; ждать от
// неё покраснения на снятой проверке типа нельзя — проверять там нечего.
// Предикат, которым это перемеряется за секунду:
//
//	grep -n 'hdr.Typ' gateway/internal/middleware/jwt_verifier.go
//
// Одно вхождение — состояние, описанное здесь. Появится второе (край начнёт
// требовать свой тип) — эту оговорку надо снять вместе с ним: предмет и
// предикат снятия заведены задачей #940, чтобы оговорка не пережила своё
// основание.
func TestF2_35_ClientAssertionIsNotAnAccessTokenOnTheGRPCEdge(t *testing.T) {
	fix := newJWKSFixture(t, "ES256")
	auth := middleware.NewAuthInterceptor(middleware.AuthModeDev, "",
		&countingLookup{}, authTestLogger()).
		WithVerifier(rs256Verifier(t, fix))

	call := func(t *testing.T, bearer string) (bool, error) {
		t.Helper()
		ctx := metadata.NewIncomingContext(context.Background(),
			metadata.Pairs("authorization", "Bearer "+bearer))
		reached := false
		handler := func(context.Context, any) (any, error) { reached = true; return nil, nil }
		_, err := auth.Unary()(ctx, nil,
			&grpc.UnaryServerInfo{FullMethod: "/iam/WhoAmI"}, handler)
		return reached, err
	}

	for name, raw := range f2Assertions(t, fix) {
		t.Run(name, func(t *testing.T) {
			reached, err := call(t, raw)
			require.Error(t, err, "утверждение клиента не является токеном доступа")
			require.Equal(t, codes.Unauthenticated, status.Code(err),
				"неудавшаяся аутентификация — отказ, а не понижение до анонима")
			require.False(t, reached, "обработчик не имеет права выполниться")
		})
	}

	// Положительный контроль ЭТОЙ поверхности. Без него всё выше зелено на
	// крае, отвергающем любой вход, — включая законный.
	t.Run("законный токен доступа принимается", func(t *testing.T) {
		reached, err := call(t, fix.sign(t, hydraClaims("user", "usr_alice_acc_a1b2")))
		require.NoError(t, err)
		require.True(t, reached, "положительный контроль: законный токен доводит вызов до обработчика")
	})
}

// TestF2_35_ClientAssertionIsNotAnAccessTokenOnTheRESTEdge — F2-35, поверхность
// вторая: REST края (main.go: authInterceptor.HTTP(inner)).
func TestF2_35_ClientAssertionIsNotAnAccessTokenOnTheRESTEdge(t *testing.T) {
	fix := newJWKSFixture(t, "ES256")
	auth := middleware.NewAuthInterceptor(middleware.AuthModeDev, "",
		&countingLookup{}, authTestLogger()).
		WithVerifier(rs256Verifier(t, fix))

	call := func(t *testing.T, bearer string) (bool, int) {
		t.Helper()
		reached := false
		srv := auth.HTTP(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			reached = true
			w.WriteHeader(http.StatusOK)
		}))
		req := httptest.NewRequest(http.MethodGet, "/iam/v1/whoami", nil)
		req.Header.Set("Authorization", "Bearer "+bearer)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		return reached, rec.Code
	}

	for name, raw := range f2Assertions(t, fix) {
		t.Run(name, func(t *testing.T) {
			reached, code := call(t, raw)
			require.Equal(t, http.StatusUnauthorized, code,
				"утверждение клиента не является токеном доступа")
			require.False(t, reached, "обработчик не имеет права выполниться")
		})
	}

	t.Run("законный токен доступа принимается", func(t *testing.T) {
		reached, code := call(t, fix.sign(t, hydraClaims("user", "usr_alice_acc_a1b2")))
		require.Equal(t, http.StatusOK, code)
		require.True(t, reached, "положительный контроль: законный токен доводит запрос до обработчика")
	})
}
