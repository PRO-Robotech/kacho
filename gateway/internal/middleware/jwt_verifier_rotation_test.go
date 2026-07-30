// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// jwt_verifier_rotation_test.go — предмет: перезапрос JWKS по неизвестному kid.
//
// У перезапроса два РАЗНЫХ повода: «снимок устарел по времени» и «подписант
// назвал ключ, которого в снимке нет». Второй поводом времени не является:
// снимок может быть свежим и при этом уже неполным — ровно это и происходит при
// ротации подписного ключа. Если предпосылка «свежий снимок» стоит внутри самой
// процедуры перезапроса, она перекрывает единственного вызывающего, которому
// нужна обратная ветка, и ротация становится отказом всех НОВЫХ токенов до
// истечения TTL.
//
// Вторая половина предмета — цена: вынужденный перезапрос обязан иметь
// собственный минимальный интервал, иначе неизвестный kid превращается в
// усилитель нагрузки на источник ключей.
package middleware

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// rotatingJWKS — источник ключей, у которого набор можно подменить, со счётчиком
// обращений.
type rotatingJWKS struct {
	mu      sync.Mutex
	keys    string
	srv     *httptest.Server
	fetches atomic.Int64
}

func newRotatingJWKS(t *testing.T, kid string) *rotatingJWKS {
	t.Helper()
	r := &rotatingJWKS{keys: jwksBodyForKid(kid)}
	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		r.fetches.Add(1)
		r.mu.Lock()
		body := r.keys
		r.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(r.srv.Close)
	return r
}

func (r *rotatingJWKS) rotateTo(kid string) {
	r.mu.Lock()
	r.keys = jwksBodyForKid(kid)
	r.mu.Unlock()
}

func jwksBodyForKid(kid string) string {
	return fmt.Sprintf(`{"keys":[{"kty":"RSA","kid":%q,"n":"abc","e":"AQAB"}]}`, kid)
}

// TestJWKSCache_UnknownKidRefetchesWhileCacheIsFresh — несущая проба: снимок
// СВЕЖИЙ (TTL заведомо не истёк), подписант назвал новый kid, и кэш обязан
// сходить за набором ещё раз и разрешить ключ.
//
// Утверждение наблюдаемое с двух сторон: (а) ключ разрешён; (б) обращение к
// источнику действительно произошло — счётчик вырос ровно на одно. Проверка
// «вернулась ошибка» ветку бы не различила: она возвращается и когда перезапроса
// не было вовсе.
func TestJWKSCache_UnknownKidRefetchesWhileCacheIsFresh(t *testing.T) {
	src := newRotatingJWKS(t, "kid-old")
	// TTL заведомо больше длительности пробы: снимок всё время «свежий».
	c := NewJWKSCache(src.srv.URL, time.Hour, &http.Client{Timeout: 5 * time.Second})

	if _, err := c.Resolve(context.Background(), "kid-old"); err != nil {
		t.Fatalf("прогрев кэша: %v", err)
	}
	warm := src.fetches.Load()
	if warm != 1 {
		t.Fatalf("прогрев должен стоить одно обращение, стоил %d", warm)
	}

	src.rotateTo("kid-new")

	k, err := c.Resolve(context.Background(), "kid-new")
	if err != nil {
		t.Fatalf("новый kid не разрешён при свежем снимке (%v): после ротации подписного "+
			"ключа каждый НОВОВЫДАННЫЙ токен отвергается до истечения TTL", err)
	}
	if k.Kid != "kid-new" {
		t.Fatalf("разрешён не тот ключ: %q", k.Kid)
	}
	if got := src.fetches.Load(); got != warm+1 {
		t.Fatalf("обращений к источнику %d, ожидалось %d: вынужденный перезапрос не состоялся",
			got, warm+1)
	}
}

// TestJWKSCache_KnownKidIsServedFromCache — законная половина той же формы:
// известный kid при свежем снимке обслуживается из кэша и НЕ вызывает обращений.
// Без неё «фикс» мог бы состоять в снятии кэширования вовсе.
func TestJWKSCache_KnownKidIsServedFromCache(t *testing.T) {
	src := newRotatingJWKS(t, "kid-old")
	c := NewJWKSCache(src.srv.URL, time.Hour, &http.Client{Timeout: 5 * time.Second})

	if _, err := c.Resolve(context.Background(), "kid-old"); err != nil {
		t.Fatalf("прогрев кэша: %v", err)
	}
	warm := src.fetches.Load()

	for i := 0; i < 50; i++ {
		if _, err := c.Resolve(context.Background(), "kid-old"); err != nil {
			t.Fatalf("известный kid должен разрешаться из кэша: %v", err)
		}
	}
	if got := src.fetches.Load(); got != warm {
		t.Fatalf("известный kid стоил %d дополнительных обращений — кэш перестал кэшировать",
			got-warm)
	}
}

// TestJWKSCache_UnknownKidFloodIsRateLimited — цена вынужденного перезапроса:
// поток неизвестных kid не должен становиться усилителем нагрузки на источник
// ключей. В пределах собственного минимального интервала перезапрос ровно один,
// сколько бы неизвестных kid ни пришло.
func TestJWKSCache_UnknownKidFloodIsRateLimited(t *testing.T) {
	src := newRotatingJWKS(t, "kid-old")
	c := NewJWKSCache(src.srv.URL, time.Hour, &http.Client{Timeout: 5 * time.Second})

	if _, err := c.Resolve(context.Background(), "kid-old"); err != nil {
		t.Fatalf("прогрев кэша: %v", err)
	}
	warm := src.fetches.Load()

	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_, _ = c.Resolve(context.Background(), fmt.Sprintf("kid-absent-%d", n))
		}(i)
	}
	wg.Wait()

	extra := src.fetches.Load() - warm
	if extra != 1 {
		t.Fatalf("64 неизвестных kid стоили %d обращений к источнику ключей вместо одного: "+
			"неизвестный kid стал усилителем нагрузки", extra)
	}
}

// TestJWKSCache_ForcedRefreshIntervalPremiseHolds — предпосылка запрета: интервал
// вынужденного перезапроса заметно КОРОЧЕ TTL кэша по умолчанию. Если это
// перестанет быть верным, ограничение перестанет отличаться от «ждать TTL», то
// есть вернёт исходное поведение, оставшись при этом на виду.
func TestJWKSCache_ForcedRefreshIntervalPremiseHolds(t *testing.T) {
	const defaultTTL = 300 * time.Second // KACHO_JWKS_CACHE_TTL_SECONDS по умолчанию
	if forcedRefreshMinInterval <= 0 {
		t.Fatalf("интервал вынужденного перезапроса не задан (%v) — ограничения нет вовсе",
			forcedRefreshMinInterval)
	}
	if forcedRefreshMinInterval >= defaultTTL/10 {
		t.Fatalf("интервал вынужденного перезапроса %v сопоставим с TTL кэша %v: окно отказа "+
			"новых токенов после ротации почти не сократилось", forcedRefreshMinInterval, defaultTTL)
	}
}
