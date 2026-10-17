// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

// session_cookie_domain.go — домен печенья нашей сессии у края (kacho#3174).
//
// # Предмет
//
// Печенье сессии выдаёт служба доступа с доменом из своей настройки
// `authn.login.cookie-domain` (`none` — только на хост). Гасит его, кроме
// службы, и край — отказом полосы личности (по отсечке и «сессии нет»). Браузер
// сопоставляет гашение с печеньем по имени, пути и ДОМЕНУ: гашение без домена
// печенье, выданное с доменом, не закрывает, и отказ остаётся стоящим. Поэтому
// край знает домен — своей ручкой с ТЕМ ЖЕ значением и тем же словарём, что у
// службы. Согласие двух величин на каждом стенде держит проба отрисовки
// `gateway/deploy/session_cookie_domain_render_test.go`.
//
// # Умолчания нет
//
// Как и у службы: необъявленная или пустая ручка — отказ в старте с названием
// ручки. Умолчание «только на хост» молча расходилось бы со службой ровно
// там, где оператор задал ей домен, — то есть было бы тем самым дефектом.

import (
	"fmt"
	"strings"
)

// KnobSessionCookieDomain — домен печенья нашей сессии; значение — то же, что
// у службы в `authn.login.cookie-domain`.
const KnobSessionCookieDomain = "KACHO_API_GATEWAY_SESSION_COOKIE_DOMAIN"

// SessionCookieDomainNone — слово «только на хост», то же, что у службы.
const SessionCookieDomainNone = "none"

// ParseSessionCookieDomain судит значение ручки: declared — объявлена ли
// переменная вообще. Возвращает домен для `Set-Cookie`: пусто — только на хост.
// Словарь и запреты формы — те же, что у службы (`none` либо имя без пробела,
// косой черты и двоеточия).
func ParseSessionCookieDomain(raw string, declared bool) (string, error) {
	if !declared {
		return "", fmt.Errorf("%s не объявлено: домен печенья сессии — то же значение, что "+
			"`authn.login.cookie-domain` службы доступа (слово «%s» — только на хост); умолчания нет (отказ в старте)",
			KnobSessionCookieDomain, SessionCookieDomainNone)
	}
	d := strings.TrimSpace(raw)
	switch {
	case d == "":
		return "", fmt.Errorf("%s объявлено пусто: домен печенья сессии — имя домена либо слово «%s» "+
			"(только на хост), как `authn.login.cookie-domain` службы доступа (отказ в старте)",
			KnobSessionCookieDomain, SessionCookieDomainNone)
	case d == SessionCookieDomainNone:
		return "", nil
	case strings.ContainsAny(d, " /:"):
		return "", fmt.Errorf("%s объявлено, но не годится: %q — нужно имя домена без схемы, пути и порта "+
			"либо слово «%s» (отказ в старте)", KnobSessionCookieDomain, raw, SessionCookieDomainNone)
	default:
		return d, nil
	}
}

// loadSessionCookieDomain — значение берётся из поля, заполненного загрузчиком;
// объявленность — у окружения.
func loadSessionCookieDomain(cfg *Config, lookup func(string) (string, bool)) error {
	_, declared := lookup(KnobSessionCookieDomain)
	d, err := ParseSessionCookieDomain(cfg.SessionCookieDomainRaw, declared)
	if err != nil {
		return err
	}
	cfg.SessionCookieDomain = d
	return nil
}
