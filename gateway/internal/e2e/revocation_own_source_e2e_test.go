// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// revocation_own_source_e2e_test.go — ПОЛОСА ПРЕДЪЯВИТЕЛЯ СПРАШИВАЕТ ОБ ОТЗЫВЕ
// НАШ ИСТОЧНИК, И ЕГО МОЛЧАНИЕ МЯГКО НЕ ПРОХОДИТ.
//
// # Предмет (#2728)
//
// Источников было два, и файл назывался по ним: сперва НАША запись отзыва (её
// пишет выход человека), потом чужой поставщик по административному пути его
// интроспекции. Молчали они по-разному, и в этом был предмет: молчание чужого —
// объявленный мягкий проход, молчание нашего — тот же класс, что закрыт на
// браузерной полосе, «чеканим, отзываем и свой же отзыв не исполняем».
//
// Чужая половина снята вместе с поставщиком: его токенов край не принимает, и
// вопрос ему был бы утверждением о предмете, которого у него нет. Вместе с ней
// ушёл и объявленный мягкий проход — исключать стало нечего.
//
// # Что здесь утверждается СЕЙЧАС
//
// Отказ при молчании НАШЕГО источника — и законный близнец рядом, отличающийся
// РОВНО ОДНИМ фактом: ответил источник или нет. Без пары «отказ при молчании»
// неотличим от «отказ всегда», а правка, делающая отказом любой исход, меняет
// дефект безопасности на отказ в обслуживании.
//
// Соседний файл (`revocation_check_e2e_test.go`) поднимает ту же полосу с
// читателем на HTTP-интроспекции: сегодня это путь НАШЕГО авторитета
// (`WithPlatformRevocationCheck` в композиционном корне), и два молчания там
// классифицируются по ответу узла, а не по тому, чей он.
package e2e_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// ourRevocationSource — НАШ источник отзыва, чьим поведением управляет проба:
// отвечает «отозван», «не отозван» либо не отвечает вовсе.
//
// Счётчик обращений здесь не для полноты: им проверяется, что источник вообще
// СПРОШЕН. Полоса, не задавшая вопроса, отвечала бы «годно» на любое
// удостоверение, и отличить её от исправной по коду ответа нечем.
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

// ownSourceHarness — полоса предъявителя, поднятая ТОЙ ЖЕ композицией, что
// собирает композиционный корень: наш источник отзыва, спрашиваемый внутренним
// путём службы доступа.
type ownSourceHarness struct {
	revocationHarness
	ours *ourRevocationSource
}

// newOwnSourceHarness поднимает край с нашим источником отзыва.
func newOwnSourceHarness(t *testing.T, hydra *hydraFixture) ownSourceHarness {
	t.Helper()

	verifier, err := middleware.NewJWTVerifier(middleware.JWTVerifierConfig{
		Issuers: []middleware.IssuerKeySet{{
			Issuer: testIssuer, KeySetURL: hydra.jwksURL,
			TokenTypes:              []string{middleware.LegacyTokenType, middleware.PlatformTokenType},
			TolerateAbsentTokenType: true,
		}},
		ExpectedAudience: testAudience,
	})
	require.NoError(t, err)

	ours := &ourRevocationSource{}
	logs := &bytes.Buffer{}
	auth := middleware.NewAuthInterceptor(
		middleware.AuthModeProduction, "", nil,
		slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
	).
		WithVerifier(verifier).
		WithRevocationCheck(middleware.NewOwnRevocationSource(ours), 0)

	reached := &atomic.Bool{}
	handler := auth.HTTP(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached.Store(true)
		w.WriteHeader(http.StatusOK)
	}))
	return ownSourceHarness{
		revocationHarness: revocationHarness{handler: handler, auth: auth, logs: logs, reached: reached},
		ours:              ours,
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

	// (1) ЗАКОННЫЙ БЛИЗНЕЦ, он же несущая половина: источник отвечает ⇒ запрос
	// обслуживается. Без него проба зеленела бы на крае, отвергающем каждое
	// удостоверение.
	live := newOwnSourceHarness(t, hydra)
	rec := live.call(t, hydra, "jti-source-answers")
	require.Equal(t, http.StatusOK, rec.Code,
		"источник отвечает, а удостоверение отвергнуто — положительный контроль не выполняется")
	require.True(t, live.reached.Load(), "запрос не дошёл до обработчика при исправном источнике")
	require.Positive(t, live.ours.asked.Load(), "НАШ источник отзыва не спрашивали вовсе")

	// (2) ПРЕДМЕТ: тот же край, тот же токен, отличие РОВНО ОДНО — замолчал НАШ
	// источник. «Не дозвонился» не есть «разрешено».
	h := newOwnSourceHarness(t, hydra)
	h.ours.fallSilent(errors.New("сосед не ответил"))
	rec = h.call(t, hydra, "jti-our-source-silent")

	assert.False(t, h.reached.Load(),
		"НАШ источник отзыва молчит, а запрос дошёл до обработчика — отозванное "+
			"удостоверение действует всё время, пока наша служба не отвечает")
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code,
		"неисправность НАША, а не удостоверения: клиенту полагается повтор, а не повторная аутентификация")

	// (3) Та же посадка на НАТИВНОЙ gRPC-поверхности: пробел на одной
	// поверхности обессмысливает другую.
	g := newOwnSourceHarness(t, hydra)
	g.ours.fallSilent(errors.New("сосед не ответил"))
	_, err := g.unary(t, hydra, "jti-our-source-silent-grpc")
	require.Error(t, err, "gRPC-поверхность приняла удостоверение при молчащем своём источнике отзыва")
	assert.Equal(t, codes.Unavailable, status.Code(err))
	assert.False(t, g.reached.Load(), "запрос дошёл до обработчика на gRPC-поверхности")
}

// ЗДЕСЬ СТОЯЛА ВТОРАЯ ПОЛОВИНА РАЗЛИЧЕНИЯ — «молчит ЧУЖОЙ провайдер ⇒ мягкий
// проход СОХРАНЯЕТСЯ». Она снята вместе со своим предметом: провайдера нет,
// объявленному мягкому проходу нечего исключать, и запись, которой нечего
// исключать, — находка, а не послабление.
//
// Законный близнец у предмета при этом остался и стоит выше половиной (1):
// отличие от отказа по-прежнему РОВНО ОДИН факт — ответил источник или нет.

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

	h := newOwnSourceHarness(t, hydra)
	h.ours.fallSilent(status.Error(codes.Unimplemented, "unknown method IsRevoked"))
	rec := h.call(t, hydra, "jti-our-source-unimplemented")

	assert.False(t, h.reached.Load(),
		"«метода нет» от НАШЕГО источника прошло как мягкий проход — послабление, "+
			"которому нечего исключать, живёт дольше любого окна раската")
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

// ─── Читатель, собранный без источника ──────────────────────────────────────

// TestE2E_Revocation_ReaderWithoutASourceRefuses — то же молчание того же
// класса: конструктор участника ТРЕБУЕТ, но не проверяет, и читатель без
// источника отвечал бы на вопрос о безопасности, ничего не спросив.
//
// Прежде случай назывался «композиция, собранная наполовину». Композиции нет —
// участник остался один, — и половина стала нулём; предмет тот же, имя
// приведено к тому, что проверяется.
func TestE2E_Revocation_ReaderWithoutASourceRefuses(t *testing.T) {
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
		WithRevocationCheck(middleware.NewOwnRevocationSource(nil), 0)

	reached := &atomic.Bool{}
	handler := auth.HTTP(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached.Store(true)
		w.WriteHeader(http.StatusOK)
	}))
	h := revocationHarness{handler: handler, auth: auth, logs: logs, reached: reached}

	rec := h.call(t, hydra, "jti-no-source")
	assert.False(t, reached.Load(),
		"читатель собран без источника, а запрос обслужен — отличить это от исправной "+
			"работы нечем, и контроль снят целиком")
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}
