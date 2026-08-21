// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// admin_hop_client.go — the HTTP client for the identity provider's ADMIN API.
//
// WHAT GOES OVER THIS HOP. Two calls share it: the logout handler's
// provider-side session kill, and — since the revocation check moved onto the
// authN layer — token introspection, which runs on EVERY authenticated request
// that misses the short-TTL cache. That second one changed the nature of the
// hop: it no longer carries only administrative calls, it carries the caller's
// LIVE bearer. A bearer is a bearer: whoever reads it off the wire can use it.
//
// WHY THE TRUST ANCHOR IS A KNOB AND NOT A DERIVATION. Moving the hop to TLS
// only helps if the certificate is VERIFIED, and the provider's certificate on
// an in-cluster address comes from the internal CA — which the process does not
// trust by default, because its default pool is the system roots. The bundle
// path is therefore configuration, exactly like the addresses themselves.
//
// WHY AN UNUSABLE ANCHOR REFUSES TO START. The tempting fallback — "cannot read
// the bundle, carry on with the system roots" — produces the one state nobody
// can see: the operator has configured verification against the internal CA,
// the process is not doing it, and everything works until a certificate
// rotates. Worse, the failure mode when it finally bites is fleet-wide: the
// introspection layer classifies an unknown-authority handshake as a PERMANENT
// misconfiguration (see permanentTransportFailure in the middleware) and then
// refuses every request rather than waving them through. Refusing at boot puts
// that in the operator's hands, at the one moment they are looking.
package main

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// adminHopCAEnv — the environment variable naming the trust anchor. Held as a
// constant so the refuse-to-start message and the config field cannot drift
// apart: an operator reading the refusal must be able to act on it without
// reading this file.
const adminHopCAEnv = "KACHO_HYDRA_ADMIN_CA_FILE"

// jwksHopCAEnv — то же для ХОПА ЗА КЛЮЧАМИ ВЕРИФИКАЦИИ.
//
// Хоп другой, а требование ровно то же: по нему едет материал, которым край
// проверяет ПОДПИСЬ каждого предъявителя, и подменивший его в пути подменяет решение
// о доступе. Сертификат на внутрикластерном адресе выписан внутренним центром, в
// корнях процесса его нет — значит связка задаётся настройкой, как и адрес.
//
// БЕЗ ЭТОЙ РУЧКИ ЗАЩИЩЁННЫЙ ТРАНСПОРТ БЫЛ НЕДОСТИЖИМ, и обходили это двумя
// способами, каждый из которых хуже проблемы: увести край НАПРЯМУЮ к провайдеру мимо
// фасада (тот самый обход, который у края уже однажды находили и чинили) либо снять
// проверку сертификата — то есть объявить защиту и не выполнять её.
const jwksHopCAEnv = "KACHO_HYDRA_JWKS_CA_FILE"

// newAdminHopClient builds the client used for every call to the provider's
// admin API, bounded by timeout.
//
// caFile empty ⇒ no trust anchor is configured and the default transport is
// used unchanged. That is not an oversight: an in-cluster admin API served over
// plaintext http needs no anchor, and inventing one would refuse a stand that
// is deliberately configured that way.
//
// caFile set ⇒ the returned client verifies the peer against THAT bundle and
// nothing else. Not "in addition to the system roots": an internal-CA hop has
// no business accepting a publicly-issued certificate for the same name, and
// narrowing the anchor is the whole point of pinning it.
func newAdminHopClient(caFile string, timeout time.Duration) (*http.Client, error) {
	return newPinnedHopClient(adminHopCAEnv, caFile, timeout)
}

// newJWKSHopClient — тот же клиент для хопа за ключами верификации.
//
// Отдельное имя, ОДНА реализация: два экземпляра одного кода разъезжаются, и
// разъезжается ровно тот, где дефект ещё не нашли. Разница между хопами — только имя
// ручки в тексте отказа, и она параметр.
func newJWKSHopClient(caFile string, timeout time.Duration) (*http.Client, error) {
	return newPinnedHopClient(jwksHopCAEnv, caFile, timeout)
}

// platformRevocationCAEnv — ручка якоря доверия хопа к НАШЕМУ авторитету отзыва.
const platformRevocationCAEnv = "KACHO_API_GATEWAY_PLATFORM_TOKEN_REVOCATION_CA_FILE"

// newPlatformRevocationHopClient — тот же клиент для хопа к нашему авторитету
// отзыва.
//
// По этому хопу едет ПРЕДЪЯВЛЕННЫЙ токен, а не только административный вызов:
// авторитет спрашивают, посылая ему само удостоверение. Значит требование к
// транспорту здесь то же, что у административного хопа, и по той же причине —
// прочитанное с провода удостоверение пригодно тому, кто его прочитал.
func newPlatformRevocationHopClient(caFile string, timeout time.Duration) (*http.Client, error) {
	return newPinnedHopClient(platformRevocationCAEnv, caFile, timeout)
}

// newPinnedHopClient строит клиент, доверяющий ТОЛЬКО указанной связке.
//
// `envName` попадает в текст отказа: оператор, читающий отказ, обязан узнать, какую
// ручку ему править, не открывая исходник.
func newPinnedHopClient(envName, caFile string, timeout time.Duration) (*http.Client, error) {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	if strings.TrimSpace(caFile) == "" {
		return &http.Client{Timeout: timeout}, nil
	}

	// #nosec G304 -- путь к корневому сертификату задаёт оператор в настройках процесса;
	// на вход запроса он не приходит. Пустой путь отсечён выше, нечитаемый — отказ старта.
	pem, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf(
			"%s=%q cannot be read (%v) — refusing to start: continuing on the system "+
				"root store would leave this hop unverified against the internal CA "+
				"while reading as configured", envName, caFile, err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf(
			"%s=%q holds no PEM certificate — refusing to start: the resulting trust "+
				"store would be EMPTY, so every handshake on this hop would fail "+
				"permanently", envName, caFile)
	}

	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	return &http.Client{Timeout: timeout, Transport: tr}, nil
}
