// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// login_lane_mount_test.go — пара «объявление ↔ монтаж» под гейтом (замысел
// LINE-A-1 §7 инв. 33, полоса L13).
//
// До этой полосы пара держалась ОДНИМ циклом в композиционном корне и ничем
// более: снять цикл — все пробы оставались зелёными (они собирали свою
// цепочку), а пути объявления начинали отвечать «не найдено». Монтаж теперь —
// одна функция, которую зовут и корень, и пробы; здесь судится её ИСХОД на
// мультиплексоре, а вызов из корня судит гейт композиционного корня.
package handler_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/gateway/internal/handler"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

func relayFor(t *testing.T, target middleware.RelayTarget) *handler.LoginLaneRelay {
	t.Helper()
	r, err := handler.NewLoginLaneRelay(handler.LoginLaneRelayConfig{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Serves: target,
		Target: "https://kaname.kacho.svc:9096", ClientIP: func(*http.Request) string { return "" },
	})
	require.NoError(t, err)
	return r
}

func everyTargetsRelay(t *testing.T) (map[middleware.RelayTarget]*handler.LoginLaneRelay, []*handler.LoginLaneRelay) {
	t.Helper()
	by := map[middleware.RelayTarget]*handler.LoginLaneRelay{}
	var set []*handler.LoginLaneRelay
	for _, tg := range middleware.RelayTargets() {
		r := relayFor(t, tg)
		by[tg] = r
		set = append(set, r)
	}
	return by, set
}

// Каждая запись объявления смонтирована ТОЧНЫМ паттерном на ретранслятор своей
// цели — перепись «записей N · смонтировано N». Запись цели, отвечающей только
// на внешних слушателях, смонтирована НЕ голым ретранслятором: голый
// ретранслировал бы и с внутреннего слушателя (исход судит
// ceremony_listener_test.go).
func TestMountLoginLaneRoutes_L13_EveryDeclaredRecordIsMountedOnItsTargetsRelay(t *testing.T) {
	by, set := everyTargetsRelay(t)
	mux := http.NewServeMux()
	n, err := handler.MountLoginLaneRoutes(mux, http.NotFoundHandler(), set...)
	require.NoError(t, err)
	routes := middleware.LoginLaneRoutes()
	require.NotEmpty(t, routes, "объявление пусто — судить нечего")
	require.Equal(t, len(routes), n, "смонтировано записей")
	mounted, scoped := 0, 0
	for _, rt := range routes {
		h, pattern := mux.Handler(httptest.NewRequest(http.MethodGet, rt.Path, nil))
		if pattern != rt.Path {
			t.Errorf("запись %q (%s): паттерн мультиплексора %q — монтаж обязан быть точным путём записи", rt.Verb, rt.Path, pattern)
			continue
		}
		if rt.Target.ExternalListenersOnly() {
			if h == http.Handler(by[rt.Target]) {
				t.Errorf("запись %q (%s) цели %q смонтирована голым ретранслятором — внутренний слушатель ретранслировал бы её", rt.Verb, rt.Path, rt.Target)
				continue
			}
			scoped++
		} else if h != http.Handler(by[rt.Target]) {
			t.Errorf("запись %q (%s) смонтирована не на ретранслятор своей цели %q", rt.Verb, rt.Path, rt.Target)
			continue
		}
		mounted++
		// Точный паттерн: путь с хвостовой косой чертой в запись не попадает.
		if _, p := mux.Handler(httptest.NewRequest(http.MethodGet, rt.Path+"/", nil)); p == rt.Path {
			t.Errorf("запись %q: путь %s/ резолвится в её паттерн — монтаж приставочный", rt.Verb, rt.Path)
		}
	}
	t.Logf("перепись: записей объявления %d · смонтировано на ретранслятор своей цели %d (из них только-внешних %d) · целей %d",
		len(routes), mounted, scoped, len(set))
}

// Инъекция: ретранслятора цели нет — монтаж отказывает и НАЗЫВАЕТ пути,
// оставшиеся без провязки; на мультиплексор не попадает ничего (всё или ничего).
func TestMountLoginLaneRoutes_L13_Injection_ATargetWithoutARelayNamesItsPaths(t *testing.T) {
	mux := http.NewServeMux()
	_, err := handler.MountLoginLaneRoutes(mux, http.NotFoundHandler(), relayFor(t, middleware.RelayTargetForm))
	require.Error(t, err)
	for _, p := range []string{middleware.CeremonyPathAuthorize, middleware.CeremonyPathToken, middleware.CeremonyPathDiscovery} {
		require.Contains(t, err.Error(), p, "отказ монтажа обязан назвать путь без ретранслятора")
	}
	_, pattern := mux.Handler(httptest.NewRequest(http.MethodPost, middleware.LoginLanePathLogin, nil))
	require.Empty(t, pattern, "монтаж отказал, но часть путей уже смонтирована")
}

// Инъекция: два ретранслятора одной цели, пустой набор и nil — отказ с
// причиной, а не молча «последний победил».
func TestMountLoginLaneRoutes_L13_Injection_DuplicateEmptyAndNilAreRefused(t *testing.T) {
	_, set := everyTargetsRelay(t)
	notHere := http.NotFoundHandler()
	_, err := handler.MountLoginLaneRoutes(http.NewServeMux(), notHere, append(set, relayFor(t, middleware.RelayTargetIssuance))...)
	require.Error(t, err)
	require.Contains(t, err.Error(), string(middleware.RelayTargetIssuance))

	_, err = handler.MountLoginLaneRoutes(http.NewServeMux(), notHere)
	require.Error(t, err)

	_, err = handler.MountLoginLaneRoutes(http.NewServeMux(), notHere, append(set, nil)...)
	require.Error(t, err)

	_, err = handler.MountLoginLaneRoutes(nil, notHere, set...)
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "mux"), "отказ на пустом мультиплексоре обязан назвать его: %v", err)
}

// Ретранслятор без объявленной цели не собирается: запись, дописанная без
// решения о цели, не получает ретранслятора «по умолчанию».
func TestNewLoginLaneRelay_L13_RefusesWithoutADeclaredTarget(t *testing.T) {
	for _, tg := range []middleware.RelayTarget{"", "foreign"} {
		_, err := handler.NewLoginLaneRelay(handler.LoginLaneRelayConfig{
			Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Serves: tg,
			Target: "https://kaname.kacho.svc:9096", ClientIP: func(*http.Request) string { return "" },
		})
		require.Error(t, err, "цель %q", tg)
	}
	r := relayFor(t, middleware.RelayTargetIssuance)
	require.Equal(t, middleware.RelayTargetIssuance, r.Serves())
}

// Инъекция: ответа внутреннего слушателя на запись, которой на нём нет, не
// передали — монтаж отказывает и НАЗЫВАЕТ записи, которые иначе либо
// ретранслировались бы с внутреннего слушателя, либо отвечали бы чем-то своим;
// на мультиплексор не попадает ничего. Близнец — тот же набор с ответом —
// монтируется.
func TestMountLoginLaneRoutes_L13_Injection_NoNotHereAnswerForExternalOnlyRecordsIsRefused(t *testing.T) {
	_, set := everyTargetsRelay(t)
	mux := http.NewServeMux()
	_, err := handler.MountLoginLaneRoutes(mux, nil, set...)
	require.Error(t, err)
	named := 0
	for _, rt := range middleware.LoginLaneRoutes() {
		if rt.Target.ExternalListenersOnly() {
			require.Contains(t, err.Error(), rt.Path, "отказ обязан назвать запись, отвечающую только на внешних слушателях")
			named++
		}
	}
	require.Positive(t, named, "в объявлении нет записей, отвечающих только на внешних слушателях — инъекции не во что попасть")
	_, pattern := mux.Handler(httptest.NewRequest(http.MethodPost, middleware.LoginLanePathLogin, nil))
	require.Empty(t, pattern, "монтаж отказал, но часть путей уже смонтирована")

	_, err = handler.MountLoginLaneRoutes(http.NewServeMux(), http.NotFoundHandler(), set...)
	require.NoError(t, err, "законный близнец с ответом внутреннего слушателя не смонтировался")
}
