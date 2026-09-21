// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// logout_exits_census_test.go — ВЫХОД ГАСИТ ОБА ИМЕНИ НА КАЖДОМ ВЫХОДЕ, КОТОРЫЙ
// ПРЕДЛАГАЕТ ПРОДУКТ, а не на одном обработчике.
//
// # Чем эта проба отличается от предыдущей, и почему предыдущей не хватало
//
// Соседняя проба (`logout_carrier_states_test.go`) измеряет ОДИН обработчик —
// `/oauth/logout` — и показывает «2 имени из 2». Утверждение верное и узкое:
// оно о том обработчике, а не о выходе. Единица счёта там — обработчик, а
// спрашивать надо по ДВУМ осям: КТО ГАСИТ и КТО ЭТОТ ВЫХОД ВЫЗЫВАЕТ. Выход,
// который никто не вызывает, гасит что угодно и ничего не решает; выход,
// который вызывают, гасит ровно то, что гасит.
//
// # Границы измерения названы, а не подразумеваются
//
// Отсюда видны выходы, достижимые ЧЕРЕЗ КРАЙ. Консоли живут вне этого дерева,
// и их собственный выход — переход на самообслуживание чужой стороны — отсюда
// не измеряется. Это сказано вслух: «ноль находок» здесь означает «ноль на
// осмотренном», и осмотренное названо переписью.
package handler_test

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/gateway/internal/handler"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// endedNames — имена, погашенные ответом.
func endedNames(res *http.Response) map[string]bool {
	out := map[string]bool{}
	for _, c := range res.Cookies() {
		if c.MaxAge < 0 {
			out[c.Name] = true
		}
	}
	return out
}

// carrierEndingShape — форма гашения без имени: путь, срок и флаги защиты.
//
// Смотреть только на ЗНАК СРОКА недостаточно, и это не придирка: браузер
// сопоставляет печенье по имени, пути и домену. Гашение с другим путём он не
// находит — печенье остаётся, «выйти» оставляет человека вошедшим, а знак
// срока при этом верен, и проверка по нему молчит.
func carrierEndingShape(c *http.Cookie) string {
	return fmt.Sprintf("path=%q maxage=%d httponly=%v secure=%v samesite=%d",
		c.Path, c.MaxAge, c.HttpOnly, c.Secure, c.SameSite)
}

// endingShapesOf — формы гашения из ответа, по имени.
func endingShapesOf(res *http.Response) map[string]string {
	out := map[string]string{}
	for _, c := range res.Cookies() {
		if c.MaxAge < 0 {
			out[c.Name] = carrierEndingShape(c)
		}
	}
	return out
}

// TestLogoutExits_EveryExitReachableThroughTheEdgeEndsEveryName — перепись по
// двум осям.
func TestLogoutExits_EveryExitReachableThroughTheEdgeEndsEveryName(t *testing.T) {
	names := middleware.SessionCarrierNames()
	require.NotEmpty(t, names)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Ось «кто гасит» × ось «кто вызывает»: каждая запись — выход, который
	// продукт ПРЕДЛАГАЕТ человеку, и то, чем он достижим.
	type exit struct {
		name   string
		caller string
		serve  func(t *testing.T) *http.Response
	}
	exits := []exit{
		{
			name:   "обработчик выхода края",
			caller: "POST /oauth/logout",
			serve: func(t *testing.T) *http.Response {
				h, err := handler.NewLogoutHandler(handler.LogoutHandlerConfig{Logger: logger})
				require.NoError(t, err)
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/oauth/logout", nil))
				return rec.Result()
			},
		},
		{
			name:   "глагол выхода полосы формы",
			caller: "POST " + middleware.LoginLanePathLogout,
			serve: func(t *testing.T) *http.Response {
				// Служба гасит ТОЛЬКО своё имя: чужого она не знает и знать не
				// может — оно принадлежит стороне, которой она не управляет.
				//
				// Форма её гашения — КАНОНИЧЕСКАЯ. Дублёр не вправе быть
				// снисходительнее настоящего: служба выдаёт наше печенье с
				// полными атрибутами защиты и гасит его так же, а небрежная
				// форма в фикстуре проверяла бы вход, которого продукт не
				// производит. Её заголовок проходит через край НЕИЗМЕННЫМ и
				// судится её стороной — сравнение форм ниже о том, что КРАЙ не
				// заводит второй формы.
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					for _, c := range middleware.SessionCarrierEndings() {
						if c.Name == middleware.OurSessionCarrierName {
							http.SetCookie(w, c)
						}
					}
					w.WriteHeader(http.StatusOK)
				}))
				t.Cleanup(upstream.Close)

				relay, err := handler.NewLoginLaneRelay(handler.LoginLaneRelayConfig{
					Logger:   logger,
					Target:   upstream.URL,
					ClientIP: middleware.NewContextExtractor(time.Now, true, middleware.WithTrustedProxyHops(1)).ClientIP,
					Timeout:  2 * time.Second,
				})
				require.NoError(t, err)
				rec := httptest.NewRecorder()
				relay.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, middleware.LoginLanePathLogout, nil))
				return rec.Result()
			},
		},
	}

	// ЗНАМЕНАТЕЛЬ ПРОВЕРЯЕТСЯ, А НЕ ПОДРАЗУМЕВАЕТСЯ. Записей здесь две, и
	// заголовок обещает «на КАЖДОМ выходе, который предлагает продукт».
	// Выходов на полосе формы ровно столько, сколько глаголов выхода в
	// объявлении путей: появится второй — перечень выше обязано пересмотреть то
	// же изменение, а не заметить потом.
	laneExits := 0
	for _, rt := range middleware.LoginLaneRoutes() {
		if rt.Verb == middleware.LoginLaneVerbLogout {
			laneExits++
		}
	}
	if laneExits != 1 {
		t.Fatalf("глаголов выхода в объявлении путей %d, перечень выходов рассчитан на 1 — "+
			"знаменатель переписи разошёлся с деревом", laneExits)
	}

	full := 0
	for _, e := range exits {
		t.Run(e.name, func(t *testing.T) {
			ended := endedNames(e.serve(t))
			missing := []string{}
			for _, n := range names {
				if !ended[n] {
					missing = append(missing, n)
				}
			}
			if len(missing) > 0 {
				t.Errorf("выход %q (вызывается: %s) гасит %d имени из %d; не погашены: %v. "+
					"Человек нажал «выйти» и по одному из имён остался вошедшим",
					e.name, e.caller, len(ended), len(names), missing)
				return
			}
			full++
		})
	}
	// ФОРМА ГАШЕНИЯ ОДНА НА ВСЕ ВЫХОДЫ, и это про то, что КРАЙ не заводит
	// второй формы. Разные атрибуты у двух выходов — расхождение, которого
	// никто не решал, и молчащее: знак срока у обоих верен, а браузер одно из
	// них не сопоставит и печенье оставит.
	shapes := map[string]map[string]string{}
	for _, e := range exits {
		shapes[e.name] = endingShapesOf(e.serve(t))
	}
	reference := map[string]string{}
	for _, n := range names {
		for exitName, got := range shapes {
			if got[n] == "" {
				continue
			}
			if reference[n] == "" {
				reference[n] = got[n]
				continue
			}
			if got[n] != reference[n] {
				t.Errorf("имя %q гасится РАЗНОЙ формой: %s против %s (выход %q). Браузер "+
					"сопоставляет печенье по имени, пути и домену — одно из гашений он не найдёт, "+
					"и «выйти» оставит человека вошедшим при верном знаке срока",
					n, reference[n], got[n], exitName)
			}
		}
	}

	t.Logf("перепись: выходов, достижимых через край, осмотрено %d · гасят все имена %d · "+
		"имён в перечне %d · различных форм гашения %d. ВНЕ ОСМОТРА: собственный выход консолей — четыре консоли живут "+
		"вне этого дерева, и переход на самообслуживание чужой стороны отсюда не наблюдается",
		len(exits), full, len(names), len(distinctShapes(shapes)))
}

// distinctShapes — сколько РАЗЛИЧНЫХ форм гашения встретилось на всех выходах.
// Единица означает «одна форма на все», и это то, что обязано быть.
func distinctShapes(shapes map[string]map[string]string) map[string]bool {
	out := map[string]bool{}
	for _, byName := range shapes {
		for _, shape := range byName {
			out[shape] = true
		}
	}
	return out
}

// Законный близнец: глагол, который выходом НЕ является, печений не гасит.
// Без него зелёное выше добывалось бы гашением на каждом ответе полосы формы —
// то есть выбрасыванием человека при любом обращении к ней.
func TestLoginLaneRelay_ANonLogoutVerbEndsNothing(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)

	relay, err := handler.NewLoginLaneRelay(handler.LoginLaneRelayConfig{
		Logger:   logger,
		Target:   upstream.URL,
		ClientIP: middleware.NewContextExtractor(time.Now, true, middleware.WithTrustedProxyHops(1)).ClientIP,
		Timeout:  2 * time.Second,
	})
	require.NoError(t, err)

	checked := 0
	for _, rt := range middleware.LoginLaneRoutes() {
		if rt.Path == middleware.LoginLanePathLogout {
			continue
		}
		checked++
		rec := httptest.NewRecorder()
		relay.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, rt.Path, nil))
		if n := len(endedNames(rec.Result())); n != 0 {
			t.Errorf("%s: глагол, выходом не являющийся, погасил %d имени", rt.Path, n)
		}
	}
	t.Logf("перепись: глаголов полосы формы осмотрено %d (из %d, выход исключён) · погасивших имена 0",
		checked, len(middleware.LoginLaneRoutes()))
}

// Вторая половина того же решения: край ДОПОЛНЯЕТ выполненный выход, а не
// решает о выходе сам. На отказе службы — «выход не выполнен» — не гасится
// ничто, иначе человек оказывался бы выброшен ровно тогда, когда служба
// сказала, что не выбрасывала.
func TestLoginLaneRelay_ARefusedLogoutEndsNothing(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	refusals := []int{
		http.StatusServiceUnavailable,
		http.StatusUnauthorized,
		http.StatusInternalServerError,
	}
	for _, code := range refusals {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(code)
			_, _ = w.Write([]byte(`{"message":"logout not performed; try again later"}`))
		}))
		relay, err := handler.NewLoginLaneRelay(handler.LoginLaneRelayConfig{
			Logger:   logger,
			Target:   upstream.URL,
			ClientIP: middleware.NewContextExtractor(time.Now, true, middleware.WithTrustedProxyHops(1)).ClientIP,
			Timeout:  2 * time.Second,
		})
		require.NoError(t, err)
		rec := httptest.NewRecorder()
		relay.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, middleware.LoginLanePathLogout, nil))
		if n := len(endedNames(rec.Result())); n != 0 {
			t.Errorf("отказ выхода кодом %d погасил %d имени — человек выброшен там, где служба "+
				"сказала, что не выбрасывала", code, n)
		}
		upstream.Close()
	}
	t.Logf("перепись: исходов отказа проверено %d · погасивших имена 0", len(refusals))
}

// ─────────────────────────────────────────────────────────────────────────────
// КОРЗИНЫ «ПРОЧЕЕ» У ИСХОДА ВЫХОДА НЕТ.
//
// Классификация была БИНАРНОЙ: дополняется полоса успеха (2xx), всё прочее —
// нет. «Прочее» при этом не пусто и не экзотично: перенаправление — обычная
// форма успеха браузерного выхода, и оно попадало в ветку «ничего не гасим»,
// то есть в сторону «оставить вошедшим».
//
// Исходов три, и третий назван, а не подразумевается:
//
//   - ВЫХОД ВЫПОЛНЕН (2xx либо перенаправление) → гасим;
//   - ВЫХОД ОТВЕРГНУТ службой (4xx/5xx с её ответом) → не гасим: человек
//     выброшен ровно тогда, когда служба сказала, что не выбрасывала;
//   - ИСХОД НЕ РАСПОЗНАН → гасим. Решение в сторону безопасности принято
//     явно: форму ответа чужого слушателя отсюда не измерить, и «не знаю»
//     обязано вести к состоянию, из которого человек может войти заново, а не
//     к состоянию, в котором он считается вошедшим.
//
// Знаменатель называется: проверяются все классы кода, КОТОРЫЕ МОГУТ БЫТЬ
// окончательным ответом на ретрансляции. Код 1xx таким не бывает — клиент Go
// его потребляет и ждёт окончательного, — и ставить его сюда значило бы
// проверять вход, которого ни один производитель не может выдать. Сам
// классификатор на нераспознанном коде судится отдельно и напрямую
// (`login_lane_relay_outcome_test.go`): его предмет — полнота разбора, а не
// путь запроса.

// logoutOutcomeClasses — по одному представителю на класс кода ответа.
func logoutOutcomeClasses() []struct {
	name    string
	code    int
	mustEnd bool
	why     string
} {
	return []struct {
		name    string
		code    int
		mustEnd bool
		why     string
	}{
		{"200 — выход выполнен", http.StatusOK, true, "обычный успех"},
		{"204 — выполнен, тела нет", http.StatusNoContent, true, "успех без тела"},
		{"302 — ПЕРЕНАПРАВЛЕНИЕ", http.StatusFound, true,
			"обычная форма успеха браузерного выхода; прежде попадала в «ничего не гасим»"},
		{"303 — перенаправление после POST", http.StatusSeeOther, true, "та же форма успеха"},
		{"401 — служба отвергла", http.StatusUnauthorized, false, "исход судит служба по записи"},
		{"409 — служба отвергла", http.StatusConflict, false, "исход судит служба по записи"},
		{"503 — выход не выполнен", http.StatusServiceUnavailable, false,
			"человек был бы выброшен ровно тогда, когда служба сказала, что не выбрасывала"},
	}
}

func TestLoginLaneRelay_LogoutOutcomeHasNoOtherBucket(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	names := middleware.SessionCarrierNames()
	classes := logoutOutcomeClasses()
	agreed := 0
	for _, tc := range classes {
		t.Run(tc.name, func(t *testing.T) {
			code := tc.code
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if code >= 300 && code < 400 {
					w.Header().Set("Location", "/signed-out")
				}
				w.WriteHeader(code)
			}))
			t.Cleanup(upstream.Close)

			relay, err := handler.NewLoginLaneRelay(handler.LoginLaneRelayConfig{
				Logger:   logger,
				Target:   upstream.URL,
				ClientIP: middleware.NewContextExtractor(time.Now, true, middleware.WithTrustedProxyHops(1)).ClientIP,
				Timeout:  2 * time.Second,
			})
			require.NoError(t, err)
			rec := httptest.NewRecorder()
			relay.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, middleware.LoginLanePathLogout, nil))

			ended := endedNames(rec.Result())
			all := true
			for _, n := range names {
				if !ended[n] {
					all = false
				}
			}
			if all != tc.mustEnd {
				t.Fatalf("код %d: погашено %d имени из %d, ожидалось гашение=%v — %s",
					tc.code, len(ended), len(names), tc.mustEnd, tc.why)
			}
			agreed++
		})
	}
	t.Logf("перепись: классов исхода проверено %d · сошлись %d · гасящих %d",
		len(classes), agreed, func() int {
			n := 0
			for _, c := range classes {
				if c.mustEnd {
					n++
				}
			}
			return n
		}())
}
