// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// login_lane_relay.go — ретрансляция записей объявления `middleware.LoginLaneRoutes`
// на слушатели службы доступа: глаголов полосы формы (приёмка Ф3 Р2, Р7, Р16;
// задача kacho#1269; Ф4 kacho#2699, Ф5 kacho#2701, Ф12 Р4 kacho#1281) — на
// слушатель формы, и трёх координат церемонии авторизации (замысел LINE-A-1
// §5.1б, полоса L13, kacho#2817; обнаружение — kacho#2721) — на слушатель выдачи.
//
// # Один экземпляр — одна цель
//
// Ретранслятор сводит свой адрес к схеме и хосту, значит экземпляр обслуживает
// ровно одну цель, и у каждой цели СВОЙ экземпляр со своей парой «адрес плюс
// удостоверение» под стражем старта (замысел LINE-A-1 §5.1б п. 2а, §7 инв. 36).
// Цель экземпляр объявляет сам (`Serves`), и обслуживает он ТОЛЬКО записи этой
// цели: путь чужой цели на нём — ошибка провязки, а не запрос. Монтаж записей на
// ретрансляторы своих целей — одна функция, `MountLoginLaneRoutes`: её зовут и
// композиционный корень, и пробы.
//
// # Почему ретрансляция, а не перевод в RPC
//
// Три глагола формы ставят и гасят печенье, и владелец печенья — служба.
// Перевод формы в RPC края заставил бы край собирать `Set-Cookie` из ответа
// службы по своим правилам — второе место об атрибутах печенья, расходящееся с
// настройкой службы молча. Ретрансляция передаёт запрос и ответ КАК ЕСТЬ, и
// состав «как есть» назван, потому что без оговорки он уносил бы на слушатель
// переданную личность. Координатам церемонии транскодировать не во что вовсе:
// контракта у них нет (замысел LINE-A-1 З1), и ответ церемонии — код
// перенаправления с `Location`, который пишет служба, — обязан уйти как есть.
//
// # Что ретранслированный запрос НЕСЁТ и чего НЕ несёт
//
// Несёт: метод, путь с параметрами, тело, `Content-Type`, печенья (`Cookie` —
// оба наших) и РОВНО ОДИН заголовок, добавленный краем, — `X-Forwarded-For` с
// одним клиентским адресом, выведенным существующим оператором чтения цепочки
// справа по числу доверенных прыжков (`middleware.ContextExtractor.ClientIP`).
// Не несёт: `Authorization` и ни одного заголовка пространства `x-kacho-` в
// обеих формах написания — их снимает тот же оператор, что снимает
// удостоверение перед пересылкой, расширенный на пространство
// (`principalmeta.StripCredentialAndIdentityHeaders`), включая шесть заголовков
// принципала, которые полоса личности пишет в запрос до продолжения.
//
// Исключение одно и названо записью объявления: на обмене кода
// (`LoginLaneRoute.CarriesClientBasic`) переживает снятие удостоверение КЛИЕНТА
// базовой схемой под голым именем, одним значением
// (`principalmeta.StripCredentialAndIdentityHeadersKeepingClientBasic`) — служба
// принимает клиента на обмене и обновлении только так (RFC 6749 §2.3.1), и
// без него каждый обмен через край был бы `invalid_client` (kacho#2721).
// Предъявитель на этой записи снимается так же, как на прочих.
//
// # Полоса сессии стоит ПЕРЕД ретранслятором, и это несущее (Ф3-51)
//
// Обработчик крепится в `httpMux` за `authInterceptor.HTTP` — как «кто я».
// Носитель отсечённой сессии до службы не доходит: его отвергает полоса. Сюда
// доходят «сессии нет» (исход судит служба по записи), живая сессия (личность
// выставлена и здесь снята) и недоступность вопросов края — только на
// глаголах, носителя не читающих, и на выходе (служба ответит своим 503);
// какие это глаголы, решает запись глагола в объявлении, и на остальных полоса
// отвечает F4d-23 сама. Читатель отсечки остаётся один и на крае; ретранслятор
// его не несёт по построению.
//
// # Служба недостижима САМОЙ ретрансляцией
//
// Соединения нет — иная посылка, чем «хранилище службы недоступно»: край
// отвечает `UNAVAILABLE` / 503 своим фиксированным текстом, одним на все
// глаголы, без `Set-Cookie`, носитель цел. Текст причины не несёт (§8 инв. 3).
package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync/atomic"
	"time"

	"github.com/PRO-Robotech/kacho/gateway/internal/listenerorigin"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
	"github.com/PRO-Robotech/kacho/gateway/internal/principalmeta"
)

// LoginLaneUnreachableMessage — текст отказа края, когда слушатель цели
// недостижим. Один на все записи перечня и на обе цели; причины не несёт.
const LoginLaneUnreachableMessage = "service unavailable; try again later"

// LoginLaneRelayTimeout — предел одной ретрансляции, НАЗВАННАЯ величина.
//
// Одна на обе цели, и это решение (замысел LINE-A-1 §7 инв. 30, строка 4): для
// координат церемонии своей величины не заводится — ретранслятор один по
// механизму, его предел один, и две ручки, управляющие одним
// `context.WithTimeout`, разошлись бы молча, причём в сторону «больше».
// Неотвечающий слушатель даёт отказ края в пределах этой величины, а не держит
// горутину и соединение края.
const LoginLaneRelayTimeout = 10 * time.Second

// LoginLaneRelayConfig — DI-мешок ретранслятора.
type LoginLaneRelayConfig struct {
	Logger *slog.Logger
	// Serves — цель, записи которой обслуживает экземпляр; обязательна и
	// принадлежит закрытому перечню `middleware.RelayTargets()`.
	Serves middleware.RelayTarget
	// Target — адрес слушателя цели (схема и хост[:порт]); путь запроса
	// передаётся как есть. Ручка — своя у каждой цели:
	// `KACHO_API_GATEWAY_IAM_LOGIN_LANE_URL` у формы,
	// `KACHO_API_GATEWAY_IAM_ISSUANCE_URL` у выдачи.
	Target string
	// Transport — транспорт к слушателю: набор удостоверения выводится из
	// режима предъявления цели (взаимный TLS у формы, односторонний у выдачи);
	// nil → транспорт по умолчанию (пробы).
	Transport http.RoundTripper
	// ClientIP — оператор вывода клиентского адреса из запроса; обязателен.
	ClientIP func(*http.Request) string
	// AnonMailGate — звено-ограничитель анонимной почты края (приёмка NTF-2,
	// Р5; замысел issue-2917, З8): ставится ПЕРЕД ретрансляцией записей цели с
	// признаком `anonMail` (`middleware.LoginLaneRoute.AnonMail`) и только их.
	// Обязательно у цели, у которой такие записи есть: ретранслятор без звена
	// пропускал бы почтовые глаголы без лимита — ветки «звена нет → пропустить»
	// нет. У цели без таких записей задавать его нечему, и заданное — ошибка
	// сборки (звено, которое ничего не судит).
	AnonMailGate func(http.Handler) http.Handler
	// Timeout — предел на одну ретрансляцию; ноль берёт `LoginLaneRelayTimeout`.
	// Композиционный корень его НЕ задаёт (гейт корня): значение для проб.
	Timeout time.Duration
}

// LoginLaneRelaySnapshot — ПРОЧИТАННЫЕ клетки одного ретранслятора (Ф3-48).
type LoginLaneRelaySnapshot struct {
	// Target — цель ретранслятора.
	Target middleware.RelayTarget
	// Relayed — ретранслировано по записи; ключи — записи объявления ЭТОЙ цели,
	// каждая клетка существует с нулём.
	Relayed map[string]uint64
	// Unreachable — слушатель цели недостижим (ответ 503 текстом края).
	Unreachable uint64
}

// LoginLaneRelay — обработчик записей объявления одной цели.
type LoginLaneRelay struct {
	logger  *slog.Logger
	proxy   *httputil.ReverseProxy
	timeout time.Duration
	serves  middleware.RelayTarget

	relayed     map[string]*atomic.Uint64
	unreachable atomic.Uint64
	// gated — ретрансляция за звеном-ограничителем: записи с признаком
	// `anonMail`; nil у цели без таких записей.
	gated http.Handler
}

// NewLoginLaneRelay собирает ретранслятор. Адрес и оператор адреса обязательны:
// ретранслятор без цели отвечал бы 503 на каждом запросе всю свою жизнь, и это
// было бы неотличимо от «служба лежит».
func NewLoginLaneRelay(cfg LoginLaneRelayConfig) (*LoginLaneRelay, error) {
	if cfg.Logger == nil {
		return nil, errors.New("login lane relay: logger is required")
	}
	if !cfg.Serves.Valid() {
		return nil, fmt.Errorf("login lane relay: target %q is not in the closed set of relay targets — "+
			"a relay without a declared target has no records to serve", cfg.Serves)
	}
	if cfg.ClientIP == nil {
		return nil, errors.New("login lane relay: client address operator is required")
	}
	target, err := url.Parse(cfg.Target)
	if err != nil || target.Scheme == "" || target.Host == "" {
		return nil, errors.New("login lane relay: target must be an absolute URL with scheme and host")
	}
	// Цель — только схема и хост: путь запроса ретранслируется как есть, а
	// базовый путь у слушателя цели не объявлен (Р2: точное совпадение путей).
	target = &url.URL{Scheme: target.Scheme, Host: target.Host}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = LoginLaneRelayTimeout
	}
	r := &LoginLaneRelay{logger: cfg.Logger, timeout: timeout, serves: cfg.Serves, relayed: map[string]*atomic.Uint64{}}
	var anonMail []string
	for _, rt := range middleware.LoginLaneRoutes() {
		if rt.Target == cfg.Serves {
			r.relayed[rt.Verb] = &atomic.Uint64{}
			if rt.AnonMail() {
				anonMail = append(anonMail, rt.Path)
			}
		}
	}
	switch {
	case len(anonMail) > 0 && cfg.AnonMailGate == nil:
		return nil, fmt.Errorf("login lane relay: records %v carry anonMail and need the anonymous mail limiter — "+
			"relaying them without it would place mail without any limit", anonMail)
	case len(anonMail) == 0 && cfg.AnonMailGate != nil:
		return nil, fmt.Errorf("login lane relay: target %q has no anonMail records — the limiter would judge nothing", cfg.Serves)
	case len(anonMail) > 0:
		r.gated = cfg.AnonMailGate(http.HandlerFunc(r.relay))
		if r.gated == nil {
			return nil, errors.New("login lane relay: anonymous mail limiter returned no handler")
		}
	}
	clientIP := cfg.ClientIP
	r.proxy = &httputil.ReverseProxy{
		Transport: cfg.Transport,
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			// Библиотека уже сняла клиентские заголовки пересылки
			// (`Forwarded`, `X-Forwarded-*`) до Rewrite; SetXForwarded НЕ зовётся
			// — он приписал бы цепочку, а служба читает ОДИН адрес.
			//
			// Запись берётся из объявления: ServeHTTP уже пропустил сюда
			// только путь своей цели. Не найденная запись снимает всё.
			if rt, ok := middleware.LoginLaneRouteFor(pr.In.URL.Path); ok && rt.CarriesClientBasic() {
				principalmeta.StripCredentialAndIdentityHeadersKeepingClientBasic(pr.Out.Header)
			} else {
				principalmeta.StripCredentialAndIdentityHeaders(pr.Out.Header)
			}
			if ip := clientIP(pr.In); ip != "" {
				pr.Out.Header.Set("X-Forwarded-For", ip)
			}
		},
		ErrorHandler: r.unreachableHandler,
		ErrorLog:     slog.NewLogLogger(cfg.Logger.Handler(), slog.LevelError),
	}
	return r, nil
}

// ServeHTTP ретранслирует запрос; ответ службы уходит клиенту как есть —
// включая код перенаправления и его `Location`: `ModifyResponse` не задан, и
// ответ церемонии край не переписывает.
func (r *LoginLaneRelay) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	rt, ok := middleware.LoginLaneRouteFor(req.URL.Path)
	if !ok || rt.Target != r.serves {
		// Обработчик крепится ТОЛЬКО на пути своей цели; иной путь здесь —
		// ошибка провязки, а не запрос, который стоит ретранслировать.
		http.NotFound(w, req)
		return
	}
	if rt.AnonMail() {
		// Звено решает до ретрансляции: пропущенный запрос оно передаёт
		// дальше само, остальным отвечает своим вызовом или отказом.
		r.gated.ServeHTTP(w, req)
		return
	}
	r.relay(w, req)
}

// relay — сама ретрансляция записи (после звена, где оно есть).
func (r *LoginLaneRelay) relay(w http.ResponseWriter, req *http.Request) {
	rt, _ := middleware.LoginLaneRouteFor(req.URL.Path)
	ctx, cancel := context.WithTimeout(req.Context(), r.timeout)
	defer cancel()
	r.relayed[rt.Verb].Add(1)
	r.proxy.ServeHTTP(w, req.WithContext(ctx))
}

// Serves — цель ретранслятора.
func (r *LoginLaneRelay) Serves() middleware.RelayTarget { return r.serves }

// Limit — предел одной ретрансляции, которым экземпляр исполняет запрос.
// Читатель — самоотчёт старта: величина предела объявляется при провязке.
func (r *LoginLaneRelay) Limit() time.Duration { return r.timeout }

// unreachableHandler — служба недостижима самой ретрансляцией: 503 текстом края,
// `google.rpc.Status` JSON с кодом UNAVAILABLE (14), без `Set-Cookie`.
func (r *LoginLaneRelay) unreachableHandler(w http.ResponseWriter, req *http.Request, err error) {
	r.unreachable.Add(1)
	// В журнал идёт ПУТЬ и только он: строка запроса навигации церемонии несёт
	// `state` и вызов PKCE (замысел LINE-A-1 §7 инв. 35 — свойство сохранено).
	r.logger.Error("login lane: service unreachable; answering 503 with the edge's fixed text",
		"target", string(r.serves), "path", req.URL.Path, "err", err)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = w.Write([]byte(`{"code":14,"message":"` + LoginLaneUnreachableMessage + `"}`))
}

// Stats — слепок клеток для диагностической поверхности.
func (r *LoginLaneRelay) Stats() LoginLaneRelaySnapshot {
	out := LoginLaneRelaySnapshot{Target: r.serves, Relayed: make(map[string]uint64, len(r.relayed)), Unreachable: r.unreachable.Load()}
	for verb, c := range r.relayed {
		out.Relayed[verb] = c.Load()
	}
	return out
}

// LoginLaneRelaySnapshots — слепки заведённых ретрансляторов; незаведённый
// (посадка external) пропускается — его клетки стоят нулями на поверхности, и
// это отличимо от «ретрансляций не было» по посадке в самоотчёте.
func LoginLaneRelaySnapshots(relays ...*LoginLaneRelay) []LoginLaneRelaySnapshot {
	var out []LoginLaneRelaySnapshot
	for _, r := range relays {
		if r != nil {
			out = append(out, r.Stats())
		}
	}
	return out
}

// MountLoginLaneRoutes — монтаж объявления: каждая запись крепится ТОЧНЫМ путём
// на ретранслятор своей цели (замысел LINE-A-1 §7 инв. 33). Всё или ничего: при
// любом отказе на мультиплексор не попадает ни одна запись.
//
// Запись цели, отвечающей только на внешних слушателях
// (`RelayTarget.ExternalListenersOnly` — координаты церемонии), ретранслируется
// лишь с соединения, помеченного обёрткой внешнего слушателя; на внутреннем
// admin-REST слушателе края и на слушателе без обёртки запрос отдаётся `notHere`.
// Это обязан быть ТОТ ЖЕ обработчик, что смонтирован под `/` (так передаёт
// корень, и это судит его гейт): тогда ответ побайтно равен ответу слушателя
// на путь, которого у него нет, — ровно тому, что он отвечает на координату
// под посадкой external, где объявление не смонтировано вовсе. Свой
// производитель «не найдено» отличался бы телом и заголовками, и форма ответа
// выдавала бы, что путь здесь всё-таки есть (тот же класс снят у Internal* на
// внешнем слушателе — диспетчер `restmux`).
//
// Отказ — если мультиплексора нет, если ретрансляторов нет вовсе, если среди
// них nil или два на одну цель («последний победил» было бы решением, которого
// никто не принимал), если у записи объявления нет ретранслятора её цели —
// тогда отказ НАЗЫВАЕТ пути, которые остались бы мёртвыми, — и если `notHere`
// не передан, а записи, отвечающие только на внешних слушателях, есть: отказ
// называет и их. Возвращает число смонтированных записей.
func MountLoginLaneRoutes(mux *http.ServeMux, notHere http.Handler, relays ...*LoginLaneRelay) (int, error) {
	if mux == nil {
		return 0, errors.New("login lane mount: mux is nil")
	}
	if len(relays) == 0 {
		return 0, errors.New("login lane mount: no relays — every record of the declaration would answer 404")
	}
	by := make(map[middleware.RelayTarget]*LoginLaneRelay, len(relays))
	for i, r := range relays {
		if r == nil {
			return 0, fmt.Errorf("login lane mount: relay #%d is nil", i)
		}
		if prev, dup := by[r.serves]; dup && prev != r {
			return 0, fmt.Errorf("login lane mount: two relays serve target %q", r.serves)
		}
		by[r.serves] = r
	}
	routes := middleware.LoginLaneRoutes()
	var orphan []string
	for _, rt := range routes {
		if by[rt.Target] == nil {
			orphan = append(orphan, rt.Path+" ("+string(rt.Target)+")")
		}
	}
	if len(orphan) > 0 {
		return 0, fmt.Errorf("login lane mount: no relay for the target of %v — these records would answer 404", orphan)
	}
	if notHere == nil {
		var scoped []string
		for _, rt := range routes {
			if rt.Target.ExternalListenersOnly() {
				scoped = append(scoped, rt.Path+" ("+string(rt.Target)+")")
			}
		}
		if len(scoped) > 0 {
			return 0, fmt.Errorf("login lane mount: no answer for the internal listener on %v — "+
				"these records answer on external listeners only; pass the handler mounted at \"/\"", scoped)
		}
	}
	for _, rt := range routes {
		var h http.Handler = by[rt.Target]
		if rt.Target.ExternalListenersOnly() {
			h = externalListenersOnly{relay: h, notHere: notHere}
		}
		mux.Handle(rt.Path, h)
	}
	return len(routes), nil
}

// externalListenersOnly — запись ретранслируется только тогда, когда
// соединение принял слушатель, обёрнутый `listenerorigin.ExternalListener`
// (`listenerorigin.OnExternalListener`). Любое другое соединение — с
// внутреннего admin-REST слушателя, со слушателя без обёртки, с обеими
// обёртками — получает ответ `notHere`. Умолчание — отказ: метку «внешний»
// ставит только обёртка внешнего слушателя, и слушатель, её потерявший,
// церемонию не ретранслирует.
type externalListenersOnly struct {
	relay   http.Handler
	notHere http.Handler
}

func (h externalListenersOnly) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if !listenerorigin.OnExternalListener(req.Context()) {
		h.notHere.ServeHTTP(w, req)
		return
	}
	h.relay.ServeHTTP(w, req)
}
