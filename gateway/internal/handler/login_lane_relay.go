// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// login_lane_relay.go — ретрансляция глаголов полосы формы на слушатель службы
// (приёмка Ф3 Р2, Р7, Р16; задача kacho#1269): четыре глагола Ф3, регистрация
// Ф4 (kacho#2699), два глагола восстановления Ф5 (kacho#2701) и шесть глаголов
// второго фактора Ф12 (Р4, kacho#1281). Перечень — одно объявление,
// `middleware.LoginLaneRoutes`.
//
// # Почему ретрансляция, а не перевод в RPC
//
// Три глагола формы ставят и гасят печенье, и владелец печенья — служба.
// Перевод формы в RPC края заставил бы край собирать `Set-Cookie` из ответа
// службы по своим правилам — второе место об атрибутах печенья, расходящееся с
// настройкой службы молча. Ретрансляция передаёт запрос и ответ КАК ЕСТЬ, и
// состав «как есть» назван, потому что без оговорки он уносил бы на слушатель
// переданную личность.
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
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync/atomic"
	"time"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
	"github.com/PRO-Robotech/kacho/gateway/internal/principalmeta"
)

// LoginLaneUnreachableMessage — текст отказа края, когда слушатель формы
// недостижим. Один на все глаголы перечня; причины не несёт.
const LoginLaneUnreachableMessage = "service unavailable; try again later"

// LoginLaneRelayConfig — DI-мешок ретранслятора.
type LoginLaneRelayConfig struct {
	Logger *slog.Logger
	// Target — адрес слушателя формы службы (схема и хост[:порт]); путь запроса
	// передаётся как есть. Ручка `KACHO_API_GATEWAY_IAM_LOGIN_LANE_URL`.
	Target string
	// Transport — транспорт к слушателю (mTLS с клиентским сертификатом края);
	// nil → транспорт по умолчанию (пробы).
	Transport http.RoundTripper
	// ClientIP — оператор вывода клиентского адреса из запроса; обязателен.
	ClientIP func(*http.Request) string
	// Timeout — предел на одну ретрансляцию; ноль берёт умолчание.
	Timeout time.Duration
}

// LoginLaneRelaySnapshot — ПРОЧИТАННЫЕ клетки ретрансляции (Ф3-48).
type LoginLaneRelaySnapshot struct {
	// Relayed — ретранслировано по глаголу; ключи — закрытый словарь
	// `middleware.LoginLaneRoutes`, каждая клетка существует с нулём.
	Relayed map[string]uint64
	// Unreachable — слушатель формы недостижим (ответ 503 текстом края).
	Unreachable uint64
}

// LoginLaneRelay — обработчик путей формы из объявления перечня.
type LoginLaneRelay struct {
	logger  *slog.Logger
	proxy   *httputil.ReverseProxy
	timeout time.Duration

	relayed     map[string]*atomic.Uint64
	unreachable atomic.Uint64
}

// NewLoginLaneRelay собирает ретранслятор. Адрес и оператор адреса обязательны:
// ретранслятор без цели отвечал бы 503 на каждом запросе всю свою жизнь, и это
// было бы неотличимо от «служба лежит».
func NewLoginLaneRelay(cfg LoginLaneRelayConfig) (*LoginLaneRelay, error) {
	if cfg.Logger == nil {
		return nil, errors.New("login lane relay: logger is required")
	}
	if cfg.ClientIP == nil {
		return nil, errors.New("login lane relay: client address operator is required")
	}
	target, err := url.Parse(cfg.Target)
	if err != nil || target.Scheme == "" || target.Host == "" {
		return nil, errors.New("login lane relay: target must be an absolute URL with scheme and host")
	}
	// Цель — только схема и хост: путь запроса ретранслируется как есть, а
	// базовый путь у слушателя формы не объявлен (Р2: точное совпадение путей).
	target = &url.URL{Scheme: target.Scheme, Host: target.Host}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	r := &LoginLaneRelay{logger: cfg.Logger, timeout: timeout, relayed: map[string]*atomic.Uint64{}}
	for _, rt := range middleware.LoginLaneRoutes() {
		r.relayed[rt.Verb] = &atomic.Uint64{}
	}
	clientIP := cfg.ClientIP
	r.proxy = &httputil.ReverseProxy{
		Transport: cfg.Transport,
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			// Библиотека уже сняла клиентские заголовки пересылки
			// (`Forwarded`, `X-Forwarded-*`) до Rewrite; SetXForwarded НЕ зовётся
			// — он приписал бы цепочку, а служба читает ОДИН адрес.
			principalmeta.StripCredentialAndIdentityHeaders(pr.Out.Header)
			if ip := clientIP(pr.In); ip != "" {
				pr.Out.Header.Set("X-Forwarded-For", ip)
			}
		},
		ModifyResponse: endEveryCarrierOnLogout,
		ErrorHandler:   r.unreachableHandler,
		ErrorLog:     slog.NewLogLogger(cfg.Logger.Handler(), slog.LevelError),
	}
	return r, nil
}

// ServeHTTP ретранслирует запрос; ответ службы уходит клиенту как есть.
func (r *LoginLaneRelay) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	verb := middleware.LoginLaneVerb(req.URL.Path)
	if verb == "" {
		// Обработчик крепится ТОЛЬКО на пути перечня; иной путь здесь —
		// ошибка провязки, а не запрос, который стоит ретранслировать.
		http.NotFound(w, req)
		return
	}
	ctx, cancel := context.WithTimeout(req.Context(), r.timeout)
	defer cancel()
	r.relayed[verb].Add(1)
	r.proxy.ServeHTTP(w, req.WithContext(ctx))
}

// unreachableHandler — служба недостижима самой ретрансляцией: 503 текстом края,
// `google.rpc.Status` JSON с кодом UNAVAILABLE (14), без `Set-Cookie`.
func (r *LoginLaneRelay) unreachableHandler(w http.ResponseWriter, req *http.Request, err error) {
	r.unreachable.Add(1)
	r.logger.Error("login lane: service unreachable; answering 503 with the edge's fixed text",
		"path", req.URL.Path, "err", err)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = w.Write([]byte(`{"code":14,"message":"` + LoginLaneUnreachableMessage + `"}`))
}

// Stats — слепок клеток для диагностической поверхности.
func (r *LoginLaneRelay) Stats() LoginLaneRelaySnapshot {
	out := LoginLaneRelaySnapshot{Relayed: make(map[string]uint64, len(r.relayed)), Unreachable: r.unreachable.Load()}
	for verb, c := range r.relayed {
		out.Relayed[verb] = c.Load()
	}
	return out
}

// endEveryCarrierOnLogout дополняет ответ глагола ВЫХОДА гашением тех имён
// носителя, которых служба не погасила.
//
// # Почему это делает край, а не служба
//
// Имён носителя два, и служба знает ровно одно — своё. Чужое принадлежит
// стороне, которой она не управляет и о которой не обязана знать; попроси мы её
// гасить чужое имя, мы завели бы в службе знание о соседе, снимаемом с
// платформы.
//
// # Почему это несущее, а не аккуратность
//
// Выход, гасящий одно имя из двух, оставляет человека ВОШЕДШИМ по второму. В
// переходном состоянии это буквально так: нажав «выйти», человек продолжает
// ходить по чужой сессии. Обработчик `/oauth/logout` гасит оба имени, но выход,
// который предлагает форма, идёт не через него — а гасит то, что гасит, не
// обработчик с лучшими намерениями.
//
// # Что функция НЕ делает
//
// Она не трогает имена, которые служба погасила сама: у ответа службы своя
// власть над своим именем, и переписывать его значило бы решать за неё. Она
// молчит на всех глаголах, кроме выхода: гашение на любом ответе полосы формы
// выбрасывало бы человека при каждом обращении к ней.
func endEveryCarrierOnLogout(resp *http.Response) error {
	if middleware.LoginLaneVerb(resp.Request.URL.Path) != middleware.LoginLaneVerbLogout {
		return nil
	}
	already := map[string]bool{}
	for _, c := range resp.Cookies() {
		already[c.Name] = true
	}
	for _, name := range middleware.SessionCarrierNames() {
		if already[name] {
			continue
		}
		// Атрибуты те же, что у выдачи и у `EndSessionCarriers`: браузер
		// сопоставляет печенье по имени, пути и домену, и гашение с другим путём
		// его не нашло бы.
		resp.Header.Add("Set-Cookie", (&http.Cookie{
			Name:     name,
			Value:    "",
			Path:     "/",
			MaxAge:   -1,
			HttpOnly: true,
			Secure:   true,
			SameSite: http.SameSiteLaxMode,
		}).String())
	}
	return nil
}
