// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// login_lane_validation.go — страж старта целей РЕТРАНСЛЯЦИИ края на службу
// доступа: слушателя полосы формы (приёмка Ф3 Р2, Р16, Ф3-45) и слушателя
// выдачи, на который уходят координаты церемонии авторизации (замысел LINE-A-1
// §5.1б п. 2, §7 инв. 36; kacho#2817).
//
// Под посадкой `own` край ретранслирует записи объявления на слушатели службы.
// Ретранслятор без цели — контроль, отказывающий на каждом запросе всю свою
// жизнь по одной и той же причине, и снаружи это неотличимо от «служба лежит»;
// поэтому пустой адрес под `own` — отказ старта с именем ручки, а не умолчание.
// Под `external` ретрансляции нет, и ручки не читаются.
//
// # Страж судит КАЖДУЮ пару «адрес плюс удостоверение», а не первую
//
// Целей две, и у каждой своя пара. Страж, судящий одну, пропускал бы вторую
// без стража — и именно её ось «абсолютный https» охраняет носитель сессии
// человека на прыжке.
//
// # Четыре оси у КАЖДОЙ цели; режим цели называет цену отсутствия пары
//
// Оси: адрес непуст; адрес — абсолютный `https` (по ретрансляции едет носитель
// сессии человека, и незашифрованный прыжок нёс бы его в открытом виде); корень
// пришпилен; пара «сертификат и ключ» края. Пара судится у обеих целей: оба
// слушателя службы узнают край ТОЛЬКО по его сертификату. Режим цели решает,
// чем кончается прыжок без пары, и это называет текст отказа:
//
//   - `mutual` (слушатель формы) — отказом на каждом рукопожатии;
//   - `optional-mutual` (слушатель выдачи) — молча: вызывающего без сертификата
//     слушатель допускает, но адрес источника берёт из `X-Forwarded-For` только
//     у пира с проверенным сертификатом края, у прочих — адрес соединения, то
//     есть самого края. Все люди за краем делили бы один предел навигаций
//     `authorize` и одно окно отказов обмена кода (приёмка темпа службы
//     `ceremony-pace-is-named-by-number.md`, Р7 п. 2–3, стадия S2 п. 1). Страж
//     старта службы этого не видит — поэтому его видит страж края.
package main

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/PRO-Robotech/corelib/identityposture"
	"github.com/PRO-Robotech/kacho/gateway/internal/config"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// Имена ручек клиентской пары — те же, что у gRPC-рёбер: одно удостоверение
// края на все хопы.
const (
	mtlsClientCertKnob = "KACHO_API_GATEWAY_MTLS_CLIENT_CERT_FILE"
	mtlsClientKeyKnob  = "KACHO_API_GATEWAY_MTLS_CLIENT_KEY_FILE"
	mtlsCAKnob         = "KACHO_API_GATEWAY_MTLS_CA_FILE"
)

// relayClientAuth — как слушатель-цель судит вызывающего на рукопожатии.
// Закрытый перечень: значение вне него — отказ старта, а не «как-нибудь».
type relayClientAuth string

const (
	// relayClientAuthMutual — слушатель требует клиентский сертификат и
	// допускает по нему ровно край.
	relayClientAuthMutual relayClientAuth = "mutual"
	// relayClientAuthOptionalMutual — слушатель запрашивает клиентский
	// сертификат и проверяет предъявленный; вызывающего без него допускает, но
	// край узнаёт только по сертификату.
	relayClientAuthOptionalMutual relayClientAuth = "optional-mutual"
)

// withoutPair — чем для слушателя этого режима кончается прыжок края без
// клиентской пары: продолжение текста отказа старта. Ошибка — режим вне
// закрытого перечня.
func (m relayClientAuth) withoutPair() (string, error) {
	switch m {
	case relayClientAuthMutual:
		return "asks the caller for a client certificate and admits exactly the edge by it; " +
			"a hop with nothing to present is refused on every handshake", nil
	case relayClientAuthOptionalMutual:
		return "asks the caller for a client certificate and recognises the edge by it alone; " +
			"a relayed request without the edge's pair is admitted but keyed by the peer address — " +
			"the edge's own — so every person behind the edge would share one pace limit", nil
	}
	return "", fmt.Errorf("client-auth mode %q of the relay target is not one of %q, %q",
		m, relayClientAuthMutual, relayClientAuthOptionalMutual)
}

// relayTargetDecl — цель ретрансляции края: ручка адреса и режим предъявления
// клиента, из которого выводится набор осей стража и удостоверение транспорта.
type relayTargetDecl struct {
	Serves  middleware.RelayTarget
	URLKnob string
	Mode    relayClientAuth
	// Subject — что по цели едет, для текстов отказа.
	Subject string
}

// relayTargetDecls — закрытый перечень целей стража, по записи на цель
// `middleware.RelayTargets()`; сходимость держит проба.
//
// Режимы — те, что объявляет поставка службы у своих слушателей: слушатель
// формы взаимный по решению приёмки Ф3 (Р16), слушатель выдачи — запрашивающий
// (`KANAME_REGISTRYTOKEN_SERVER_MTLS_CLIENTAUTHMODE=optional-mutual`, приёмка
// темпа службы Р7 п. 1, п. 5: взаимный отверг бы вызывающих без сертификата).
func relayTargetDecls() []relayTargetDecl {
	return []relayTargetDecl{
		{
			Serves: middleware.RelayTargetForm, URLKnob: config.LoginLaneURLKnob, Mode: relayClientAuthMutual,
			Subject: "the form-lane verbs (sign-in, sign-out, password change, the form token, registration, recovery, second factor)",
		},
		{
			Serves: middleware.RelayTargetIssuance, URLKnob: config.IssuanceURLKnob, Mode: relayClientAuthOptionalMutual,
			Subject: "the authorization ceremony (the authorize navigation and the code exchange)",
		},
	}
}

// relayTargetDeclFor — запись цели по имени; ok=false для цели вне перечня.
func relayTargetDeclFor(tg middleware.RelayTarget) (relayTargetDecl, bool) {
	for _, d := range relayTargetDecls() {
		if d.Serves == tg {
			return d, true
		}
	}
	return relayTargetDecl{}, false
}

// mustRelayTargetDecl — запись цели закрытого перечня; цель вне его — ошибка
// программиста, а не входа.
func mustRelayTargetDecl(tg middleware.RelayTarget) relayTargetDecl {
	d, ok := relayTargetDeclFor(tg)
	if !ok {
		panic(fmt.Sprintf("relay target %q has no guard declaration", tg))
	}
	return d
}

// relayGuardAxes — оси стража, одни у каждой цели; читатель — самоотчёт старта.
func relayGuardAxes() []string {
	return []string{"address set", "absolute https address", "root pinned", "client certificate and key as a pair"}
}

// LoginLaneConfig — срез настройки ОДНОЙ цели, который читает страж.
type LoginLaneConfig struct {
	Target         relayTargetDecl
	URL            string
	ClientCertFile string
	ClientKeyFile  string
	CAFile         string
}

// validateLoginLaneConfig отказывает в старте под `own` без годной цели.
//
// Метка окружения здесь НЕ читается намеренно: ретрансляция без цели негодна на
// любом стенде, а dev-посадка на поднятом стенде запрещена (ban #16).
func validateLoginLaneConfig(provider identityposture.Provider, cfg LoginLaneConfig) error {
	if provider != identityposture.Own {
		return nil
	}
	d := cfg.Target
	if _, known := relayTargetDeclFor(d.Serves); !known || strings.TrimSpace(d.URLKnob) == "" {
		return fmt.Errorf("relay target %q is not declared with its address knob — a relay whose target the guard "+
			"cannot name would carry its pair unguarded (refuse to start)", d.Serves)
	}
	cost, err := d.Mode.withoutPair()
	if err != nil {
		return fmt.Errorf("%s: %w (refuse to start)", d.URLKnob, err)
	}
	raw := strings.TrimSpace(cfg.URL)
	if raw == "" {
		return fmt.Errorf("%s is empty — on posture %s=own the edge relays %s to the identity service, "+
			"and a relay with no target would answer 503 to every request for its whole life, indistinguishable "+
			"from a service outage (refuse to start)",
			d.URLKnob, config.IdentityProviderKnob, d.Subject)
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("%s=%q is not an absolute URL with scheme and host (refuse to start)", d.URLKnob, raw)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("%s=%q is not https — the relayed request carries the person's session "+
			"cookie, and a plaintext hop would carry it in the clear (refuse to start)", d.URLKnob, raw)
	}
	cert, key, ca := strings.TrimSpace(cfg.ClientCertFile), strings.TrimSpace(cfg.ClientKeyFile), strings.TrimSpace(cfg.CAFile)
	switch {
	case cert == "" && key == "":
		return fmt.Errorf("%s and %s are both empty — the listener behind %s (mode %s) %s; "+
			"declare the pair together with %s (refuse to start)",
			mtlsClientCertKnob, mtlsClientKeyKnob, d.URLKnob, d.Mode, cost, d.URLKnob)
	case key == "":
		return fmt.Errorf("%s is set without %s — half a pair presents nothing (refuse to start)",
			mtlsClientCertKnob, mtlsClientKeyKnob)
	case cert == "":
		return fmt.Errorf("%s is set without %s — a key with no certificate has nothing to present (refuse to start)",
			mtlsClientKeyKnob, mtlsClientCertKnob)
	}
	if ca == "" {
		return fmt.Errorf("%s is empty — the certificate of the listener behind %s is issued by the internal CA "+
			"and this process trusts the system roots, so every handshake would fail with an unknown "+
			"authority; pin the bundle together with %s (refuse to start)", mtlsCAKnob, d.URLKnob, d.URLKnob)
	}
	return nil
}
