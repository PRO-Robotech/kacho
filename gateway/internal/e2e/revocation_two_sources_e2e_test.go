// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// revocation_two_sources_e2e_test.go — ПОЛОСА ПРЕДЪЯВИТЕЛЯ СПРАШИВАЕТ ДВА
// ИСТОЧНИКА ОТЗЫВА, И МОЛЧАТ ОНИ ПО-РАЗНОМУ.
//
// # Предмет (#2728)
//
// Соседний файл (`revocation_check_e2e_test.go`) поднимает полосу так, как она
// выглядела до появления НАШЕЙ записи отзыва: один источник, провайдер. Посадка
// давно другая — композиционный корень собирает `LocalThenProviderRevocation`,
// то есть «свой источник, потом чужой» (`cmd/api-gateway/main.go`). И у этой
// композиции молчаний ДВА, а не одно:
//
//   - молчит ЧУЖОЙ провайдер — объявленный мягкий проход. Его доступностью мы
//     не управляем, размен назван вслух и предъявлен пробой;
//   - молчит НАШ источник — и это ТОТ ЖЕ класс, что уже закрыт на браузерной
//     полосе и на полосе нашего издателя: чеканим, отзываем и свой же отзыв не
//     исполняем.
//
// Для кода обе ошибки были неразличимы: композиция возвращала молчание своего
// источника нетипизированной обёрткой, вердикт `revocationUnanswered`
// выносился — и не читался никем, потому что у обеих развилок в `auth.go` не
// было ни ветки на него, ни `default`.
//
// # Что здесь утверждается
//
// РАЗЛИЧЕНИЕ, а не ужесточение. Каждое отрицание стоит рядом со своим законным
// близнецом, и близнец отличается РОВНО ОДНИМ фактом — тем, ЧЕЙ источник
// замолчал. Без этой пары «отказ при молчании» неотличим от «отказ всегда», а
// правка, делающая отказом оба молчания, меняет дефект безопасности на отказ в
// обслуживании.
package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// ourRevocationSource — НАШ источник отзыва, чьим поведением управляет проба:
// отвечает «отозван», «не отозван» либо не отвечает вовсе.
//
// Счётчик обращений здесь не для полноты: им проверяется, что при молчании
// своего источника чужой НЕ спрашивается — вердикт по половине картины был бы
// вердиктом о другом предмете.
type ourRevocationSource struct {
	asked   atomic.Int64
	revoked atomic.Bool
	silence atomic.Pointer[error]
}

func (s *ourRevocationSource) IsSessionRevoked(_ context.Context, _ string) (bool, error) {
	s.asked.Add(1)
	if e := s.silence.Load(); e != nil {
		return false, *e
	}
	return s.revoked.Load(), nil
}

func (s *ourRevocationSource) fallSilent(err error) { s.silence.Store(&err) }

// twoSourceHarness — полоса предъявителя, поднятая ТОЙ ЖЕ композицией, что
// собирает композиционный корень: наш источник первым, провайдер вторым.
type twoSourceHarness struct {
	revocationHarness
	ours         *ourRevocationSource
	providerAsks *atomic.Int64
}

// newTwoSourceHarness поднимает край с обоими источниками. providerDown делает
// чужого провайдера недоступным — это и есть тот один факт, которым законный
// близнец отличается от предмета.
func newTwoSourceHarness(t *testing.T, hydra *hydraFixture, providerDown bool) twoSourceHarness {
	t.Helper()

	asks := &atomic.Int64{}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		asks.Add(1)
		if providerDown {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"active": true})
	}))
	t.Cleanup(provider.Close)

	verifier, err := middleware.NewJWTVerifier(middleware.JWTVerifierConfig{
		Issuers: []middleware.IssuerKeySet{{
			Issuer: testIssuer, KeySetURL: hydra.jwksURL,
			TokenTypes:              []string{middleware.LegacyTokenType, middleware.PlatformTokenType},
			TolerateAbsentTokenType: true,
		}},
		ExpectedAudience: testAudience,
	})
	require.NoError(t, err)

	providerCache, err := middleware.NewIntrospectionCache(middleware.IntrospectionCacheConfig{
		HydraIntrospectionURL: provider.URL,
		// TTL минимальный: кеш здесь не предмет, а помеха — на втором вопросе о
		// том же удостоверении проба мерила бы кеш, а не полосу.
		TTL:     time.Millisecond,
		Timeout: 500 * time.Millisecond,
	})
	require.NoError(t, err)

	ours := &ourRevocationSource{}
	logs := &bytes.Buffer{}
	auth := middleware.NewAuthInterceptor(
		middleware.AuthModeProduction, "", nil,
		slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
	).
		WithVerifier(verifier).
		WithRevocationCheck(middleware.NewLocalThenProviderRevocation(ours, providerCache), 0)

	reached := &atomic.Bool{}
	handler := auth.HTTP(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached.Store(true)
		w.WriteHeader(http.StatusOK)
	}))
	return twoSourceHarness{
		revocationHarness: revocationHarness{handler: handler, auth: auth, logs: logs, reached: reached},
		ours:              ours,
		providerAsks:      asks,
	}
}

// ─── Предмет: молчит НАШ источник ⇒ отказ ───────────────────────────────────

// TestE2E_Revocation_BearerLaneRefusesWhenOurOwnRevocationSourceIsSilent —
// предикат снятия #2728 дословно.
//
// Имя взято из предиката и снабжено префиксом файла (`TestE2E_Revocation_`),
// которым здесь названы все пробы этой полосы.
func TestE2E_Revocation_BearerLaneRefusesWhenOurOwnRevocationSourceIsSilent(t *testing.T) {
	hydra := newHydra(t)
	defer hydra.close()

	// (1) ЗАКОННЫЙ БЛИЗНЕЦ, он же несущая половина: оба источника отвечают ⇒
	// запрос обслуживается. Без него проба зеленела бы на крае, отвергающем
	// каждое удостоверение.
	live := newTwoSourceHarness(t, hydra, false)
	rec := live.call(t, hydra, "jti-both-answer")
	require.Equal(t, http.StatusOK, rec.Code,
		"оба источника отвечают, а удостоверение отвергнуто — положительный контроль не выполняется")
	require.True(t, live.reached.Load(), "запрос не дошёл до обработчика при исправных источниках")
	require.Positive(t, live.ours.asked.Load(), "НАШ источник отзыва не спрашивали вовсе")
	require.Positive(t, live.providerAsks.Load(), "провайдер не спрошен — свой источник его не заменяет")

	// (2) ПРЕДМЕТ: тот же край, тот же токен, отличие РОВНО ОДНО — замолчал НАШ
	// источник. «Не дозвонился» не есть «разрешено».
	h := newTwoSourceHarness(t, hydra, false)
	h.ours.fallSilent(errors.New("сосед не ответил"))
	before := h.providerAsks.Load()
	rec = h.call(t, hydra, "jti-our-source-silent")

	assert.False(t, h.reached.Load(),
		"НАШ источник отзыва молчит, а запрос дошёл до обработчика — отозванное "+
			"удостоверение действует всё время, пока наша служба не отвечает")
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code,
		"неисправность НАША, а не удостоверения: клиенту полагается повтор, а не повторная аутентификация")
	assert.Equal(t, before, h.providerAsks.Load(),
		"при молчащем своём источнике спрошен чужой — вердикт вынесен по половине картины")

	// (3) Та же посадка на НАТИВНОЙ gRPC-поверхности: пробел на одной
	// поверхности обессмысливает другую.
	g := newTwoSourceHarness(t, hydra, false)
	g.ours.fallSilent(errors.New("сосед не ответил"))
	_, err := g.unary(t, hydra, "jti-our-source-silent-grpc")
	require.Error(t, err, "gRPC-поверхность приняла удостоверение при молчащем своём источнике отзыва")
	assert.Equal(t, codes.Unavailable, status.Code(err))
	assert.False(t, g.reached.Load(), "запрос дошёл до обработчика на gRPC-поверхности")
}

// ─── Законный близнец: молчит ЧУЖОЙ провайдер ⇒ мягкий проход СОХРАНЯЕТСЯ ───

// TestE2E_Revocation_ForeignProviderSilenceKeepsItsDocumentedSoftPass — вторая
// половина различения, и без неё правка выше читается как ужесточение.
//
// Отличие от предмета — РОВНО ОДИН факт: замолчал не наш источник, а чужой.
func TestE2E_Revocation_ForeignProviderSilenceKeepsItsDocumentedSoftPass(t *testing.T) {
	hydra := newHydra(t)
	defer hydra.close()

	h := newTwoSourceHarness(t, hydra, true)
	rec := h.call(t, hydra, "jti-foreign-provider-down")

	require.Equal(t, http.StatusOK, rec.Code,
		"чужой провайдер недоступен, и удостоверение отвергнуто — дефект безопасности "+
			"заменён отказом в обслуживании: доступностью третьей стороны мы не управляем")
	assert.True(t, h.reached.Load(), "объявленный мягкий проход чужого провайдера снят")
	assert.Positive(t, h.ours.asked.Load(), "наш источник не спрошен — порядок вопросов нарушен")
	assert.Contains(t, h.logs.String(), "revocation check unavailable",
		"мягкий проход обязан быть громким: контроль, переставший исполняться молча, — потерянный контроль")
}

// ─── Решение об `Unimplemented` НАШЕГО источника ────────────────────────────

// TestE2E_Revocation_OurSourceAnsweringUnimplementedIsSilenceNotARolloutPass —
// РЕШЕНИЕ, названное вслух и закреплённое.
//
// На браузерной полосе тот же код транспорта отделён типизированным признаком
// (`ErrSessionCutoffUnsupported`) и проходит ГРОМКО: вопрос об отсечке появился
// позже края, и «метода нет» там означает окно раската — состояние, которое
// сходится само.
//
// ЗДЕСЬ ЭТО НЕПРИМЕНИМО, и довод — замер, а не вкус. `IsRevoked` не новее ни
// одного из внутренних вопросов, которые край уже задаёт тому же соседу: по
// 16 закреплённым версиям `kaname` в кеше модулей он присутствует в КАЖДОЙ, где
// есть хоть один из них (`SessionCutoffOf`, `CheckBasicCredentialLive` —
// с v0.1.1; `InternalHumanSessionService.Resolve` — позже). Реплики, которая
// отвечает «метода нет» на `IsRevoked` и при этом отвечает на остальные, не
// существует: у неё нет ни одного, и браузерная полоса такую реплику уже
// отвергает (`ErrHumanSessionUnsupported` — отказ, не проход).
//
// Поэтому послабление здесь было бы послаблением, которому НЕЧЕГО исключать:
// предиката снятия у него нет, и снять его было бы нечем. Решение: «метода
// нет» от НАШЕГО источника есть молчание, и потому отказ.
func TestE2E_Revocation_OurSourceAnsweringUnimplementedIsSilenceNotARolloutPass(t *testing.T) {
	hydra := newHydra(t)
	defer hydra.close()

	h := newTwoSourceHarness(t, hydra, false)
	h.ours.fallSilent(status.Error(codes.Unimplemented, "unknown method IsRevoked"))
	rec := h.call(t, hydra, "jti-our-source-unimplemented")

	assert.False(t, h.reached.Load(),
		"«метода нет» от НАШЕГО источника прошло как мягкий проход — послабление, "+
			"которому нечего исключать, живёт дольше любого окна раската")
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

// ─── Композиция, собранная наполовину ───────────────────────────────────────

// TestE2E_Revocation_HalfAssembledCompositionRefuses — третье молчание того же
// класса: конструктор обоих участников ТРЕБУЕТ, но не проверяет, и композиция
// с одним участником отвечала бы, спрашивая половину.
func TestE2E_Revocation_HalfAssembledCompositionRefuses(t *testing.T) {
	hydra := newHydra(t)
	defer hydra.close()

	verifier, err := middleware.NewJWTVerifier(middleware.JWTVerifierConfig{
		Issuers: []middleware.IssuerKeySet{{
			Issuer: testIssuer, KeySetURL: hydra.jwksURL,
			TokenTypes:              []string{middleware.LegacyTokenType, middleware.PlatformTokenType},
			TolerateAbsentTokenType: true,
		}},
		ExpectedAudience: testAudience,
	})
	require.NoError(t, err)

	logs := &bytes.Buffer{}
	auth := middleware.NewAuthInterceptor(
		middleware.AuthModeProduction, "", nil,
		slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
	).
		WithVerifier(verifier).
		WithRevocationCheck(middleware.NewLocalThenProviderRevocation(nil, nil), 0)

	reached := &atomic.Bool{}
	handler := auth.HTTP(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached.Store(true)
		w.WriteHeader(http.StatusOK)
	}))
	h := revocationHarness{handler: handler, auth: auth, logs: logs, reached: reached}

	rec := h.call(t, hydra, "jti-half-assembled")
	assert.False(t, reached.Load(),
		"композиция собрана наполовину, а запрос обслужен — отличить это от исправной "+
			"работы нечем, и контроль снят целиком")
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}
