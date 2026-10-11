// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// email_change_lane_test.go — два глагола смены адреса почты (приёмка F8-S6
// Р1, Р2; F8S6-10, F8S6-11; kacho#3062) на полосе сессии и в звене прав края.
//
// Решения записей: до подтверждения адреса — закрыты (EC Р2 службы),
// недоступность службы — ответ края (KA1): оба читают носитель живой сессии.
// Исход смены — предъявление кода с ответом 2xx — гасит кеш решений реплики,
// которая его ретранслировала (Р2). Здесь судится ИСХОД этих решений, каждое
// отрицание — в паре с близнецом, отличающимся одним фактом.
package middleware

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// F8S6-10 (И): неподтверждённая сессия на обоих путях смены адреса — отказ
// адреса края побайтно, цель формы не вызвана; близнец — та же сессия с
// отметкой подтверждения: цель формы вызвана ровно раз.
func TestEmailChangeLane_F8S6_10_UnverifiedAddressIsRefusedVerifiedTwinReachesTheFormTarget(t *testing.T) {
	for verb, path := range emailChangeWant {
		rig := newAddressRig(t, unverifiedOwnSession())
		requireAddressRefusal(t, "F8S6-10 неподтверждённый адрес · "+verb, rig.present(http.MethodPost, path, true))
		if rig.reached[path] != 0 || rig.relayed[RelayTargetForm] != 0 || rig.next.served != 0 {
			t.Errorf("F8S6-10 %s: неподтверждённая сессия прошла край — цель формы %d, слушатель формы %d, следующее звено %d",
				verb, rig.reached[path], rig.relayed[RelayTargetForm], rig.next.served)
		}

		twin := newAddressRig(t, liveOwnSession())
		rec := twin.present(http.MethodPost, path, true)
		if rec.Code != http.StatusOK || !notAddressRefusal(rec) {
			t.Errorf("F8S6-10 %s близнец с подтверждённым адресом: %d %s", verb, rec.Code, rec.Body.String())
			continue
		}
		if twin.reached[path] != 1 || twin.relayed[RelayTargetForm] != 1 || twin.next.served != 0 {
			t.Errorf("F8S6-10 %s близнец: цель формы вызвана %d раз (ожидался 1), слушатель формы %d, следующее звено %d (ожидался 0) — путь обязан быть записью объявления",
				verb, twin.reached[path], twin.relayed[RelayTargetForm], twin.next.served)
		}
	}
}

// F8S6-08 сторона края (Р1 `relayWhenUnanswered` = false): служба не ответила
// о сессии — ответ края (KA1) побайтно, носитель цел, цель формы не вызвана.
// Близнецы: тот же запрос при ответившей службе — ретранслирован; тот же режим
// на записи `login` (`relayWhenUnanswered`) — ретранслирован.
func TestEmailChangeLane_F8S6_10_UnansweredServiceIsAnsweredByTheEdgeLoginTwinIsRelayed(t *testing.T) {
	for verb, path := range emailChangeWant {
		book, cut := f12Books()
		chain, relay := formChain(t, book, cut)
		if got := runFormVerb(chain, relay, formVerbRequest(path, carrierA, false)); got.relayed != 1 || got.code != http.StatusOK {
			t.Errorf("F8S6-10 %s близнец «служба отвечает»: получено %d %q, ретранслировано %d — путь обязан быть записью объявления",
				verb, got.code, got.body, got.relayed)
			continue
		}
		for _, mode := range unansweredModes {
			book, cut := f12Books()
			mode.set(book, cut)
			chain, relay := formChain(t, book, cut)
			expectRefusedKept(t, "F8S6-10 "+mode.name+" · "+verb, runFormVerb(chain, relay, formVerbRequest(path, carrierA, false)))

			if got := runFormVerb(chain, relay, formVerbRequest(LoginLanePathLogin, carrierA, false)); got.relayed != 1 {
				t.Errorf("F8S6-10 близнец %s · login: запись с relayWhenUnanswered обязана ретранслироваться; получено %d %q, ретранслировано %d",
					mode.name, got.code, got.body, got.relayed)
			}
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// F8S6-11 — самосброс кеша решений на исходе смены адреса (Р2).

// emailChangeFlushKey — закешированный вердикт «разрешено» для субъекта смены.
const emailChangeFlushKey = "user:usr-email-change|iam.projects.get|project|prj1|"

// enforcingAuthzWithCachedAllow — звено прав в посадке «проверка включена» с
// одним закешированным вердиктом «разрешено». Путь полосы формы освобождён от
// каталога (`isPublicHTTPPath`), и вопроса к модели на нём нет: звену нужны
// только кеш и накопитель, остальные зависимости путь не читает.
func enforcingAuthzWithCachedAllow(t *testing.T) *AuthzMiddleware {
	t.Helper()
	m := &AuthzMiddleware{
		cfg:     AuthzMiddlewareConfig{Enabled: true, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))},
		cache:   newDecisionCache(100, 5*time.Second, time.Now),
		metrics: NewAuthzMetrics(),
		now:     time.Now,
	}
	m.cache.put(emailChangeFlushKey)
	if !m.cache.allowed(emailChangeFlushKey) {
		t.Fatalf("предпосылка: вердикт «разрешено» не закеширован")
	}
	return m
}

// relayAnswering — следующее звено в роли ретранслятора полосы формы: отвечает
// статусом службы и считает обращения.
type relayAnswering struct {
	status int
	served int
}

func (r *relayAnswering) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	r.served++
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(r.status)
	_, _ = w.Write([]byte(`{}`))
}

// flushOutcome — исход одного обращения через звено прав: сдвинулся ли счётчик
// поколений кеша и жив ли закешированный вердикт.
type flushOutcome struct {
	code      int
	served    int
	genMoved  bool
	allowLeft bool
}

func throughAuthz(t *testing.T, path string, serviceStatus int) flushOutcome {
	t.Helper()
	// Предпосылка: путь — запись объявления, освобождённая от каталога. Без
	// записи звено спросило бы каталог, которого у этой сборки нет, и исход
	// говорил бы о сборке, а не о самосбросе (это предмет F8S6-10).
	if !isPublicHTTPPath(path) {
		t.Fatalf("предпосылка: %s не освобождён от каталога — записи объявления нет (F8S6-10), о самосбросе судить не по чему", path)
	}
	m := enforcingAuthzWithCachedAllow(t)
	relay := &relayAnswering{status: serviceStatus}
	before := m.cache.generation()
	rec := httptest.NewRecorder()
	m.HTTP(relay).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"code":"123456","csrfToken":"x"}`)))
	return flushOutcome{
		code:      rec.Code,
		served:    relay.served,
		genMoved:  m.cache.generation() != before,
		allowLeft: m.cache.allowed(emailChangeFlushKey),
	}
}

// TestEmailChangeLane_F8S6_11_ConfirmSuccessFlushesTheDecisionCache — (а)
// ретрансляция предъявления с ответом службы 200 гасит кеш (поколение
// сдвинуто, вердикт снят); (б) тот же путь с ответом 401 кеш не трогает. (а) и
// (б) различаются только статусом ответа службы.
//
// Отрицательные контроли рядом: запрос смены (`email-change`) исхода не несёт —
// служба пишет строку очереди смены субъекта только на предъявлении (EC Р8
// п. 8); соседнее предъявление кода (`verify-email-confirm`) права и адрес не
// меняет. Без них утверждение (а) выполнялось бы звеном, гасящим кеш на любом
// 2xx полосы формы.
func TestEmailChangeLane_F8S6_11_ConfirmSuccessFlushesTheDecisionCache(t *testing.T) {
	confirm := emailChangeWant["email-change-confirm"]

	// (а) исход смены — 200 службы.
	got := throughAuthz(t, confirm, http.StatusOK)
	if got.code != http.StatusOK || got.served != 1 {
		t.Fatalf("F8S6-11 (а): ответ %d, обращений к ретранслятору %d — ответ службы обязан уйти как есть", got.code, got.served)
	}
	if !got.genMoved || got.allowLeft {
		t.Errorf("F8S6-11 (а): предъявление с ответом 200 — поколение кеша сдвинуто = %v, вердикт «разрешено» жив = %v; "+
			"реплика, обслужившая смену, обязана погасить кеш сразу (Р2)", got.genMoved, got.allowLeft)
	}

	// (б) близнец — тот же путь, служба отвечает 401.
	got = throughAuthz(t, confirm, http.StatusUnauthorized)
	if got.code != http.StatusUnauthorized || got.served != 1 {
		t.Fatalf("F8S6-11 (б): ответ %d, обращений к ретранслятору %d", got.code, got.served)
	}
	if got.genMoved || !got.allowLeft {
		t.Errorf("F8S6-11 (б): предъявление с ответом 401 — поколение кеша сдвинуто = %v, вердикт жив = %v; "+
			"неподошедший код ничего не меняет, кеш обязан остаться", got.genMoved, got.allowLeft)
	}

	// Иной 2xx того же пути — тоже исход смены: граница — класс 2xx, а не 200.
	if got = throughAuthz(t, confirm, http.StatusNoContent); !got.genMoved {
		t.Errorf("F8S6-11: предъявление с ответом 204 кеш не погасило — граница Р2 «2xx», а не «200»")
	}

	// Отрицательные контроли: 200 на соседних записях кеш не трогает.
	for _, path := range []string{emailChangeWant["email-change"], LoginLanePathVerifyEmailConfirm, LoginLanePathLogin} {
		if got := throughAuthz(t, path, http.StatusOK); got.genMoved || !got.allowLeft {
			t.Errorf("F8S6-11 контроль %s: ответ 200 сдвинул поколение кеша = %v (вердикт жив = %v) — исход смены несёт только предъявление",
				path, got.genMoved, got.allowLeft)
		}
	}
}
