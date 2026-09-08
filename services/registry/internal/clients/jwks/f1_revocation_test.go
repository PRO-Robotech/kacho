// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// f1_revocation_test.go — F1-25: отзыв читается НА ПРЕДЪЯВЛЕНИИ.
//
// Контроль, действующий только в местах выдачи удостоверения, отзывом не
// является: он лишь не выдаёт нового. Отличие от задержки распространения
// существенно — задержка сходится сама ограниченным окном, а отзыв без читателя
// не сходится вовсе, потому что сходиться нечему.
package jwks

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/pkg/authz"
	"github.com/stretchr/testify/require"
)

// authority — авторитет отзыва по форме RFC 7662 со всеми ручками, которых
// требует сценарий: что отвечать, каким кодом, с каким типом содержимого, и
// сколько раз его спросили.
type authority struct {
	srv   *httptest.Server
	asked atomic.Int32

	mu          sync.Mutex
	revoked     map[string]bool // сырое удостоверение → отозвано
	status      int
	contentType string
	rawBody     []byte
}

func newAuthority(t *testing.T) *authority {
	t.Helper()
	a := &authority{revoked: map[string]bool{}}
	a.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.asked.Add(1)
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if err := r.ParseForm(); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		token := r.PostForm.Get("token")

		a.mu.Lock()
		status, ct, raw := a.status, a.contentType, a.rawBody
		active := !a.revoked[token]
		a.mu.Unlock()

		if status != 0 && status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		if ct == "" {
			ct = "application/json"
		}
		w.Header().Set("Content-Type", ct)
		if raw != nil {
			_, _ = w.Write(raw)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"active": active})
	}))
	t.Cleanup(a.srv.Close)
	return a
}

func (a *authority) revoke(token string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.revoked[token] = true
}

func (a *authority) setStatus(code int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.status = code
}

func (a *authority) setBody(ct string, body []byte) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.contentType, a.rawBody = ct, body
}

func newReader(t *testing.T, a *authority, at *time.Time) *IntrospectionReader {
	t.Helper()
	r, err := NewIntrospectionReader(a.srv.URL, RevocationTransport{},
		WithIntrospectionClock(func() time.Time { return *at }))
	require.NoError(t, err)
	return r
}

// TestF1_25_RevocationIsReadOnPresentation — F1-25, вопрос СКВОЗЬ обе стороны:
// записали отзыв — предъявили токен — получили отказ. Двумя пробами по половине
// это не проверяется: каждая утверждала бы о своей стороне, а вместе они могут
// не работать.
//
// Несущая половина — ПРИНЯТИЕ: без неё читатель, который ВСЕГДА отвечает
// отказом (не дозвонился, читает не то, не читает вовсе), проходит пробу
// целиком, то есть «контроль, не отказавший ни разу», только зеркально.
func TestF1_25_RevocationIsReadOnPresentation(t *testing.T) {
	ks := newKeySet(t)
	ks.addRSA(t, "our-1")
	a := newAuthority(t)
	at := time.Now()
	v := newVerifierWith(t, newReader(t, a, &at), ourPair(ks))

	tok := ks.mintRS(t, "our-1", typAccessJWT, platformClaims("sva-1", at, 30*time.Minute))

	sub, err := v.Verify(context.Background(), tok)
	require.NoError(t, err, "несущая половина: НЕ отозванный субъект при ДОСТУПНОМ авторитете принимается")
	require.Equal(t, "sva-1", sub)
	require.Positive(t, a.asked.Load(), "авторитет обязан быть спрошен на предъявлении, а не только на выдаче")

	// Отзыв записан. Ждём объявленное окно и предъявляем тот же токен.
	a.revoke(tok)
	at = at.Add(authz.RevocationPolicy.Ceiling + time.Second)

	_, err = v.Verify(context.Background(), tok)
	require.ErrorIs(t, err, ErrInvalidToken,
		"после записи отзыва и по истечении объявленного окна тот же токен обязан отвергаться")
}

// TestF1_25_UnreachableAuthorityFailsClosed — F1-25: недоступность авторитета
// даёт ОТКАЗ, а не проход. Положительный контроль стоит рядом: пока авторитет
// доступен, тот же токен принимается — иначе отрицание зелено на проверяющем,
// отвергающем всё.
func TestF1_25_UnreachableAuthorityFailsClosed(t *testing.T) {
	ks := newKeySet(t)
	ks.addRSA(t, "our-1")
	a := newAuthority(t)
	at := time.Now()
	v := newVerifierWith(t, newReader(t, a, &at), ourPair(ks))

	tok := ks.mintRS(t, "our-1", typAccessJWT, platformClaims("sva-1", at, 30*time.Minute))
	_, err := v.Verify(context.Background(), tok)
	require.NoError(t, err, "положительный контроль: при доступном авторитете токен принимается")

	a.srv.Close()
	at = at.Add(authz.RevocationPolicy.Ceiling + time.Second) // утвердительный ответ уже не свеж

	_, err = v.Verify(context.Background(), tok)
	require.ErrorIs(t, err, ErrInvalidToken,
		"«не дозвонился» не означает «разрешено»: ответ не получен — доступ закрыт")
}

// TestF1_25_UnrecognisedAnswerFailsClosed — исход, который нельзя опознать, —
// это отказ. Разбирать «почти ответ» нельзя: страница ошибки, разобранная
// снисходительно, читается как утверждение авторитета, которого он не делал.
func TestF1_25_UnrecognisedAnswerFailsClosed(t *testing.T) {
	cases := map[string]func(a *authority){
		"код не 200": func(a *authority) { a.setStatus(http.StatusInternalServerError) },
		"код 404 — не тот эндпоинт":   func(a *authority) { a.setStatus(http.StatusNotFound) },
		"тело не JSON":                func(a *authority) { a.setBody("text/html", []byte("<html>oops</html>")) },
		"JSON без признака":           func(a *authority) { a.setBody("application/json", []byte(`{"sub":"sva-1"}`)) },
		"JSON с признаком не тем":     func(a *authority) { a.setBody("application/json", []byte(`{"active":"yes"}`)) },
		"тип содержимого не объявлен": func(a *authority) { a.setBody(" ", []byte(`{"active":true}`)) },
	}

	for name, break_ := range cases {
		t.Run(name, func(t *testing.T) {
			ks := newKeySet(t)
			ks.addRSA(t, "our-1")
			a := newAuthority(t)
			at := time.Now()
			v := newVerifierWith(t, newReader(t, a, &at), ourPair(ks))

			tok := ks.mintRS(t, "our-1", typAccessJWT, platformClaims("sva-1", at, 30*time.Minute))
			break_(a)

			_, err := v.Verify(context.Background(), tok)
			require.ErrorIs(t, err, ErrInvalidToken, "неопознанный исход авторитета закрывает доступ")
		})
	}

	t.Run("положительный контроль: опознанный утвердительный ответ принимается", func(t *testing.T) {
		ks := newKeySet(t)
		ks.addRSA(t, "our-1")
		a := newAuthority(t)
		at := time.Now()
		v := newVerifierWith(t, newReader(t, a, &at), ourPair(ks))
		a.setBody("application/json", []byte(`{"active":true}`))

		sub, err := v.Verify(context.Background(),
			ks.mintRS(t, "our-1", typAccessJWT, platformClaims("sva-1", at, 30*time.Minute)))
		require.NoError(t, err)
		require.Equal(t, "sva-1", sub)
	})
}

// TestF1_25_WindowComesFromTheDeclaredPolicy — окно берётся из УЖЕ ОБЪЯВЛЕННОЙ
// политики, своего числа фаза не заводит.
//
// Утверждаются обе стороны: внутри окна утвердительный ответ удерживается (иначе
// авторитет спрашивался бы на каждый запрос — та самая нагрузка, ради
// амортизации которой окно и существует), за окном — спрашивается заново.
func TestF1_25_WindowComesFromTheDeclaredPolicy(t *testing.T) {
	ks := newKeySet(t)
	ks.addRSA(t, "our-1")
	a := newAuthority(t)
	at := time.Now()
	reader := newReader(t, a, &at)
	require.LessOrEqual(t, reader.Window(), authz.RevocationPolicy.Ceiling,
		"окно обязано укладываться в объявленный потолок политики")
	require.Equal(t, authz.RevocationPolicy.Default, reader.Window(),
		"окно берётся из объявленной политики, а не заводится здесь")

	v := newVerifierWith(t, reader, ourPair(ks))
	tok := ks.mintRS(t, "our-1", typAccessJWT, platformClaims("sva-1", at, 30*time.Minute))

	_, err := v.Verify(context.Background(), tok)
	require.NoError(t, err)
	require.Equal(t, int32(1), a.asked.Load())

	// Внутри окна авторитет не спрашивается повторно.
	at = at.Add(reader.Window() / 2)
	_, err = v.Verify(context.Background(), tok)
	require.NoError(t, err)
	require.Equal(t, int32(1), a.asked.Load(), "внутри объявленного окна утвердительный ответ удерживается")

	// За окном — спрашивается заново, и записанный отзыв доезжает.
	at = at.Add(reader.Window() + time.Second)
	a.revoke(tok)
	_, err = v.Verify(context.Background(), tok)
	require.ErrorIs(t, err, ErrInvalidToken, "за окном ответ перезапрашивается, и отзыв доезжает")
	require.Equal(t, int32(2), a.asked.Load())
}

// TestF1_25_NegativeAnswerIsNotCached — отрицательный ответ не запоминается: он
// и так закрывает доступ, поэтому его запоминание не защищает ничего — оно лишь
// откладывало бы восстановление доступа после того, как его вернули.
func TestF1_25_NegativeAnswerIsNotCached(t *testing.T) {
	ks := newKeySet(t)
	ks.addRSA(t, "our-1")
	a := newAuthority(t)
	at := time.Now()
	reader := newReader(t, a, &at)
	v := newVerifierWith(t, reader, ourPair(ks))

	tok := ks.mintRS(t, "our-1", typAccessJWT, platformClaims("sva-1", at, 30*time.Minute))
	a.revoke(tok)

	_, err := v.Verify(context.Background(), tok)
	require.ErrorIs(t, err, ErrInvalidToken)

	// Доступ вернули В ТОТ ЖЕ момент времени: отрицательный ответ не должен
	// удерживаться ни секунды.
	a.mu.Lock()
	delete(a.revoked, tok)
	a.mu.Unlock()

	sub, err := v.Verify(context.Background(), tok)
	require.NoError(t, err, "возвращённый доступ виден сразу: отрицательный ответ не кэшируется")
	require.Equal(t, "sva-1", sub)
}

// TestF1_25_AuthorityIsAskedOnlyForOurIssuer — спрашиваем только про токены
// НАШЕЙ чеканки. Полоса прежнего издателя сохраняет сегодняшнее поведение — она
// вне области этой под-фазы, и менять её здесь значило бы менять то, о чём
// решения не принимали.
func TestF1_25_AuthorityIsAskedOnlyForOurIssuer(t *testing.T) {
	our := newKeySet(t)
	our.addRSA(t, "our-1")
	legacy := newKeySet(t)
	legacy.addRSA(t, "legacy-1")
	a := newAuthority(t)
	at := time.Now()
	v := newVerifierWith(t, newReader(t, a, &at), ourPair(our), legacyPair(legacy))

	// Токен прежнего издателя: авторитет о нём не спрашивается.
	sub, err := v.Verify(context.Background(),
		legacy.mintRS(t, "legacy-1", typJWT, legacyClaims("cid-1", at, 30*time.Minute)))
	require.NoError(t, err)
	require.Equal(t, "cid-1", sub)
	require.Zero(t, a.asked.Load(), "полоса прежнего издателя своего поведения не меняет")

	// Наш токен: спрашивается.
	sub, err = v.Verify(context.Background(),
		our.mintRS(t, "our-1", typAccessJWT, platformClaims("sva-1", at, 30*time.Minute)))
	require.NoError(t, err)
	require.Equal(t, "sva-1", sub)
	require.Equal(t, int32(1), a.asked.Load(), "о токене нашей чеканки авторитет спрашивается")
}

// TestF1_25_DeclaredControlWithoutAReaderIsRefused — объявленный контроль без
// читателя есть контроль, который не откажет ни разу за свою жизнь. Такой
// проверяющий не строится вовсе.
func TestF1_25_DeclaredControlWithoutAReaderIsRefused(t *testing.T) {
	ks := newKeySet(t)
	_, err := New([]KeySetSource{{
		Issuer: testPlatformIss, URL: ks.url(), TokenType: typAccessJWT, ReadRevocation: true,
	}}, testAud)
	require.Error(t, err, "чтение отзыва объявлено, читателя нет — контроль мёртв, и это видно при построении")

	// Положительный контроль: с читателем — строится.
	a := newAuthority(t)
	at := time.Now()
	v, err := New([]KeySetSource{{
		Issuer: testPlatformIss, URL: ks.url(), TokenType: typAccessJWT, ReadRevocation: true,
	}}, testAud, WithRevocationReader(newReader(t, a, &at)))
	require.NoError(t, err)
	require.NotNil(t, v)
}

// TestF1_25_AuthorityURLIsDeclaredNeverDerived — адрес авторитета задаётся явно.
// Выведенный адрес всегда непуст, поэтому контроль выглядел бы включённым, ведя
// в никуда, и ни один профиль развёртывания не обязан был бы ничего задавать,
// чтобы это заметить.
func TestF1_25_AuthorityURLIsDeclaredNeverDerived(t *testing.T) {
	for _, bad := range []string{"", "   ", "/introspect", "not a url", "kaname-internal:9097"} {
		_, err := NewIntrospectionReader(bad, RevocationTransport{})
		require.Errorf(t, err, "адрес авторитета %q не является объявленным абсолютным адресом", bad)
	}

	// Положительный контроль: объявленный абсолютный адрес с объявленными
	// учётными данными ребра принимается. Пара «адрес + учётные данные»
	// неразделима: маршруту отзыва присылают предъявленный токен, поэтому
	// исключение из аутентификации, которым живёт маршрут набора ключей, туда не
	// распространяется.
	pki := issueTestPKI(t)
	r, err := NewIntrospectionReader("https://kaname-internal.kacho.svc:9097/internal/tokens/introspect",
		RevocationTransport{
			Enable:     true,
			CAFiles:    []string{pki.caFile},
			CertFile:   pki.clientCert,
			KeyFile:    pki.clientKey,
			ServerName: "kaname-internal",
		})
	require.NoError(t, err)
	require.NotNil(t, r)
}
