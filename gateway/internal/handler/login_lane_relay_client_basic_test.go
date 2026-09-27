// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// login_lane_relay_client_basic_test.go — удостоверение КЛИЕНТА на записи обмена
// кода (kacho#2721, возврат ревью круга 1).
//
// Служба на обмене кода и на обновлении аутентифицирует клиента ТОЛЬКО базовой
// схемой (RFC 6749 §2.3.1): обработчик выдачи читает `r.BasicAuth()`, а секрет в
// теле и подписанное утверждение на этих полосах отвергает `invalid_client`.
// Ретранслятор, снимающий `Authorization` безусловно, делал КАЖДЫЙ обмен через
// край отказом `invalid_client` — при зелёных пробах края, потому что ни одна из
// них не предъявляла клиента.
//
// Цепочка — производственной формы, в порядке корня: посадка production-strict,
// провязаны обе полосы края, читающие `Authorization` (базовый секрет с нашей
// маркой и подписанный предъявитель), полоса сессии и полоса прав с пустым
// каталогом. Базовую схему ни одна полоса края не читает — обе ждут `Bearer `, —
// и проба это утверждает счётом обращений к их авторитетам, а не прочтением.
//
// Положительный случай один (запись обмена, базовая схема, голое имя, одно
// значение) в трёх законных написаниях схемы. Каждый близнец меняет ровно один
// факт: схему, запись, форму имени, число значений.
package handler_test

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	iamv1 "github.com/PRO-Robotech/kaname/pkg/api/kaname/cloud/iam/v1"

	"github.com/PRO-Robotech/kacho/gateway/internal/handler"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
	"github.com/PRO-Robotech/kacho/gateway/internal/principalmeta"
	"github.com/PRO-Robotech/kacho/internal/privateloopback"
)

// askedVerifier — проверяющий подписанного предъявителя, считающий обращения.
type askedVerifier struct{ asked atomic.Int64 }

func (v *askedVerifier) Verify(context.Context, string) (*middleware.VerifiedToken, error) {
	v.asked.Add(1)
	return nil, errors.New("probe verifier refuses every token")
}

// askedAuthority — авторитет базового секрета с нашей маркой, считающий
// обращения.
type askedAuthority struct{ asked atomic.Int64 }

func (a *askedAuthority) Resolve(context.Context, string) (*iamv1.ResolveBasicCredentialResponse, error) {
	a.asked.Add(1)
	return nil, errors.New("probe authority refuses every credential")
}

// prodEdge — край под `own` производственной формы.
type prodEdge struct {
	chain     http.Handler
	listener  *formListenerStub
	relays    map[middleware.RelayTarget]*handler.LoginLaneRelay
	verifier  *askedVerifier
	authority *askedAuthority
}

func newProdEdge(t *testing.T, own *fakeOwn) *prodEdge {
	t.Helper()
	e := &prodEdge{
		listener:  &formListenerStub{status: http.StatusOK, body: `{}`},
		relays:    map[middleware.RelayTarget]*handler.LoginLaneRelay{},
		verifier:  &askedVerifier{},
		authority: &askedAuthority{},
	}
	srv := privateloopback.NewServer(t, e.listener)
	t.Cleanup(srv.Close)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	var set []*handler.LoginLaneRelay
	for _, tg := range middleware.RelayTargets() {
		r, err := handler.NewLoginLaneRelay(handler.LoginLaneRelayConfig{
			Logger: logger, Serves: tg, Target: srv.URL,
			ClientIP: middleware.NewContextExtractor(time.Now, true, middleware.WithTrustedProxyHops(1)).ClientIP,
			Timeout:  2 * time.Second,
		})
		if err != nil {
			t.Fatalf("ретранслятор цели %q не собрался: %v", tg, err)
		}
		e.relays[tg] = r
		set = append(set, r)
	}
	mux := http.NewServeMux()
	if _, err := handler.MountLoginLaneRoutes(mux, http.NotFoundHandler(), set...); err != nil {
		t.Fatalf("монтаж объявления: %v", err)
	}
	authz, err := middleware.NewAuthzMiddleware(middleware.AuthzMiddlewareConfig{
		Enabled:         true,
		Catalog:         middleware.NewPermissionCatalog(),
		Subjects:        middleware.NewSubjectExtractor(true),
		Context:         middleware.NewContextExtractor(time.Now, true),
		Resources:       middleware.NewResourceExtractor(nil),
		Checker:         refusingChecker{},
		Logger:          logger,
		CacheTTL:        5 * time.Second,
		CacheMaxEntries: 100,
		PublicAllowlist: middleware.DefaultPublicAllowlist(),
		RestRouter:      middleware.NewRestRouter(),
	})
	if err != nil {
		t.Fatalf("полоса прав: %v", err)
	}
	a := middleware.NewAuthInterceptor(middleware.AuthModeProductionStrict, "", nil, logger).
		WithBasicCredentialLane(middleware.NewBasicCredentialLane(e.authority).WithLogger(logger)).
		WithVerifier(e.verifier).
		WithHumanSession(own).
		WithSessionCutoffCheck(&fakeCut{}, time.Hour)
	e.chain = a.HTTP(authz.HTTP(mux))
	return e
}

// clientBasic — удостоверение клиента базовой схемой: идентификатор и секрет
// кодируются формой до base64 (RFC 6749 §2.3.1), как их раскодирует служба.
func clientBasic(scheme, id, secret string) string {
	return scheme + " " + base64.StdEncoding.EncodeToString([]byte(id+":"+secret))
}

// codeExchange — обмен кода: POST формой на запись обмена.
func codeExchange() *http.Request {
	req := httptest.NewRequest(http.MethodPost, middleware.CeremonyPathToken,
		strings.NewReader("grant_type=authorization_code&code=ac-1&code_verifier=v-1&redirect_uri=https%3A%2F%2Fconsole.kacho.local%2Fcallback"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = relayedClientIP + ":4242"
	return req
}

// arrivedClient — что служба прочла бы своим читателем удостоверения клиента.
func arrivedClient(h http.Header) (id, secret string, ok bool) {
	return (&http.Request{Header: h}).BasicAuth()
}

func carriers() []struct {
	name string
	own  *fakeOwn
} {
	return []struct {
		name string
		own  *fakeOwn
	}{
		{"no_session", &fakeOwn{found: false}},
		{"live_session", &fakeOwn{found: true, sess: liveSession()}},
	}
}

// Положительный случай: на записи обмена удостоверение клиента базовой схемой
// доезжает до слушателя выдачи ОДНИМ значением, как прислано, и служба читает из
// него ту пару, что предъявил клиент. Мостовой формы и пространства `x-kacho-`
// на слушателе нет.
func TestLoginLaneRelay_2721_TokenRecordCarriesClientBasicToIssuance(t *testing.T) {
	for _, c := range carriers() {
		for _, scheme := range []string{"Basic", "basic", "BASIC"} {
			t.Run(c.name+"/"+scheme, func(t *testing.T) {
				e := newProdEdge(t, c.own)
				sent := clientBasic(scheme, "console", "s3cret")
				req := codeExchange()
				req.Header.Set("Authorization", sent)
				req.AddCookie(&http.Cookie{Name: middleware.OurSessionCarrierName, Value: "s2-live"})

				rec := httptest.NewRecorder()
				e.chain.ServeHTTP(rec, req)
				if rec.Code != http.StatusOK || e.listener.count() != 1 {
					t.Fatalf("обмен кода обязан дойти до слушателя выдачи: код %d, дошло %d, тело %s",
						rec.Code, e.listener.count(), rec.Body.String())
				}
				got := e.listener.last()
				if got.path != middleware.CeremonyPathToken {
					t.Fatalf("дошёл не обмен: %s", got.path)
				}
				if v := got.header.Values("Authorization"); len(v) != 1 || v[0] != sent {
					t.Fatalf("удостоверение клиента на слушателе выдачи: %q, прислано %q — "+
						"служба ответит invalid_client на каждом обмене через край", v, sent)
				}
				if id, secret, ok := arrivedClient(got.header); !ok || id != "console" || secret != "s3cret" {
					t.Fatalf("служба прочла бы клиента (%q, %q, %v), предъявлен (console, s3cret)", id, secret, ok)
				}
				if v := got.header.Get(principalmeta.BridgePrefix + "Authorization"); v != "" {
					t.Fatalf("мостовая форма имени уехала на слушатель выдачи: %q", v)
				}
				if left := kachoHeaders(got.header); len(left) != 0 {
					t.Fatalf("на слушатель выдачи уехали заголовки пространства x-kacho-: %v", left)
				}
				if n := e.verifier.asked.Load() + e.authority.asked.Load(); n != 0 {
					t.Fatalf("полосы края, читающие Authorization, спрошены о базовой схеме %d раз(а)", n)
				}
				if s := e.relays[middleware.RelayTargetIssuance].Stats(); s.Relayed["token"] != 1 {
					t.Fatalf("клетка ретрансляции обмена: %v", s.Relayed)
				}
			})
		}
	}
}

// Близнец по СХЕМЕ: тот же обмен, предъявитель вместо удостоверения клиента —
// до слушателя выдачи не доезжает ничего. Предъявитель непрозрачный: полосам
// края он не принадлежит, и до ретранслятора запрос доходит.
func TestLoginLaneRelay_2721_TokenRecordStillStripsBearer(t *testing.T) {
	for _, c := range carriers() {
		t.Run(c.name, func(t *testing.T) {
			e := newProdEdge(t, c.own)
			req := codeExchange()
			req.Header.Set("Authorization", "Bearer must-not-cross")
			req.AddCookie(&http.Cookie{Name: middleware.OurSessionCarrierName, Value: "s2-live"})

			rec := httptest.NewRecorder()
			e.chain.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK || e.listener.count() != 1 {
				t.Fatalf("обмен кода обязан дойти до слушателя выдачи: код %d, дошло %d", rec.Code, e.listener.count())
			}
			if v := e.listener.last().header.Values("Authorization"); len(v) != 0 {
				t.Fatalf("предъявитель уехал на слушатель выдачи: %q", v)
			}
		})
	}
}

// Близнец по ЗАПИСИ: базовая схема на КАЖДОЙ иной записи объявления снимается —
// служба аутентифицирует клиента базовой схемой ровно на обмене. Перечень
// записей берётся из объявления.
func TestLoginLaneRelay_2721_ClientBasicIsStrippedOnEveryOtherRecord(t *testing.T) {
	var other int
	for _, rt := range middleware.LoginLaneRoutes() {
		if rt.Path == middleware.CeremonyPathToken {
			continue
		}
		other++
		t.Run(rt.Verb, func(t *testing.T) {
			e := newProdEdge(t, &fakeOwn{found: false})
			req, _, _ := forgedFormRequest(rt)
			req.Header.Set("Authorization", clientBasic("Basic", "console", "s3cret"))
			req.Header.Del(principalmeta.BridgePrefix + "Authorization")

			rec := httptest.NewRecorder()
			e.chain.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK || e.listener.count() != 1 {
				t.Fatalf("запись %q обязана ретранслироваться: код %d, дошло %d", rt.Verb, rec.Code, e.listener.count())
			}
			if v := e.listener.last().header.Values("Authorization"); len(v) != 0 {
				t.Fatalf("базовая схема уехала на слушатель цели %q с записи %q: %q", rt.Target, rt.Verb, v)
			}
		})
	}
	if other == 0 {
		t.Fatal("иных записей в объявлении нет — близнецу не на чем стоять, это не зелёный")
	}
	t.Logf("перепись: иных записей объявления %d", other)
}

// Близнец по ФОРМЕ ИМЕНИ: удостоверение клиента только под мостовым именем — не
// доезжает ни под каким. Служба читает голое имя; мостовое на ретрансляции
// снимается всегда.
func TestLoginLaneRelay_2721_BridgedClientBasicIsStripped(t *testing.T) {
	e := newProdEdge(t, &fakeOwn{found: false})
	req := codeExchange()
	req.Header.Set(principalmeta.BridgePrefix+"Authorization", clientBasic("Basic", "console", "s3cret"))

	rec := httptest.NewRecorder()
	e.chain.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || e.listener.count() != 1 {
		t.Fatalf("обмен кода обязан дойти до слушателя выдачи: код %d, дошло %d", rec.Code, e.listener.count())
	}
	got := e.listener.last().header
	if v := got.Values("Authorization"); len(v) != 0 {
		t.Fatalf("мостовая форма превратилась в голую: %q", v)
	}
	if v := got.Values(principalmeta.BridgePrefix + "Authorization"); len(v) != 0 {
		t.Fatalf("мостовая форма уехала на слушатель выдачи: %q", v)
	}
}

// Близнец по ЧИСЛУ ЗНАЧЕНИЙ: два значения `Authorization` — базовая схема и
// предъявитель — снимаются оба. Какое из двух служба обязана проверять, запрос
// не говорит (RFC 6749 §2.3: один способ аутентификации на запрос), и выбор края
// был бы решением, которого никто не принимал.
func TestLoginLaneRelay_2721_TwoAuthorizationValuesAreBothStripped(t *testing.T) {
	e := newProdEdge(t, &fakeOwn{found: false})
	req := codeExchange()
	req.Header.Add("Authorization", clientBasic("Basic", "console", "s3cret"))
	req.Header.Add("Authorization", "Bearer must-not-cross")

	rec := httptest.NewRecorder()
	e.chain.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || e.listener.count() != 1 {
		t.Fatalf("обмен кода обязан дойти до слушателя выдачи: код %d, дошло %d", rec.Code, e.listener.count())
	}
	if v := e.listener.last().header.Values("Authorization"); len(v) != 0 {
		t.Fatalf("из двух значений Authorization край выбрал сам: %q", v)
	}
}
