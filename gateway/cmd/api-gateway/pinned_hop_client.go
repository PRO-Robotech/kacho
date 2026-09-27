// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// pinned_hop_client.go — HTTP clients for the hops the edge makes on its own
// behalf: to the key set its verifier trusts, and to our revocation authority.
//
// WHAT GOES OVER THESE HOPS. The key-set hop carries the material the edge
// verifies EVERY bearer's signature with; whoever substitutes it in transit
// substitutes the access decision. The revocation hop carries the caller's LIVE
// bearer on every cache miss — introspection asks about a token by sending it —
// and a bearer is a bearer: whoever reads it off the wire can use it.
//
// The hop to the previous identity provider's ADMIN API is gone (#2734), and so
// is its client: the edge no longer asks that provider anything.
//
// WHY THE TRUST ANCHOR IS A KNOB AND NOT A DERIVATION. Moving a hop to TLS only
// helps if the certificate is VERIFIED, and a certificate on an in-cluster
// address comes from the internal CA — which the process does not trust by
// default, because its default pool is the system roots. The bundle path is
// therefore configuration, exactly like the addresses themselves.
//
// WHY AN UNUSABLE ANCHOR REFUSES TO START. The tempting fallback — "cannot read
// the bundle, carry on with the system roots" — produces the one state nobody
// can see: the operator has configured verification against the internal CA,
// the process is not doing it, and everything works until a certificate
// rotates. Worse, the failure mode when it finally bites is fleet-wide: the
// introspection layer classifies an unknown-authority handshake as a PERMANENT
// misconfiguration (see permanentTransportFailure in the middleware) and then
// refuses every request. Refusing at boot puts that in the operator's hands, at
// the one moment they are looking.
package main

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
)

// jwksHopCAEnv — ручка якоря доверия ХОПА ЗА КЛЮЧАМИ ВЕРИФИКАЦИИ. Держится
// константой, чтобы текст отказа старта и поле настроек не разошлись: оператор,
// читающий отказ, обязан узнать, что править, не открывая этот файл.
//
// По этому хопу едет материал, которым край проверяет ПОДПИСЬ каждого
// предъявителя, и подменивший его в пути подменяет решение о доступе.
// Сертификат на внутрикластерном адресе выписан внутренним центром, в корнях
// процесса его нет — значит связка задаётся настройкой, как и адрес.
//
// БЕЗ ЭТОЙ РУЧКИ ЗАЩИЩЁННЫЙ ТРАНСПОРТ БЫЛ НЕДОСТИЖИМ, и обходили это двумя
// способами, каждый из которых хуже проблемы: увести край НАПРЯМУЮ к провайдеру мимо
// фасада (тот самый обход, который у края уже однажды находили и чинили) либо снять
// проверку сертификата — то есть объявить защиту и не выполнять её.
//
// Имя ручки объявлено в конфигурации (`config.JWKSCAFileKnob`) и здесь
// не повторяется: текст отказа обязан называть ровно то имя, которое читает поле.
const jwksHopCAEnv = config.JWKSCAFileKnob

// newJWKSHopClient строит клиент хопа за ключами верификации, ограниченный
// timeout.
//
// caFile пуст ⇒ якоря нет и транспорт по умолчанию не трогается: незащищённый
// внутрикластерный адрес якоря не требует, и выдуманный якорь отказал бы стенду,
// настроенному так намеренно. caFile задан ⇒ клиент проверяет собеседника ТОЛЬКО
// этой связкой — не «вдобавок к системным корням»: хопу к внутреннему центру
// незачем принимать публично выписанный сертификат на то же имя.
//
// Отдельное имя, ОДНА реализация (`newPinnedHopClient`): два экземпляра одного
// кода разъезжаются, и разъезжается ровно тот, где дефект ещё не нашли.
func newJWKSHopClient(caFile string, timeout time.Duration) (*http.Client, error) {
	return newPinnedHopClient(jwksHopCAEnv, caFile, timeout)
}

// platformRevocationCAEnv — ручка якоря доверия хопа к НАШЕМУ авторитету отзыва.
const platformRevocationCAEnv = "KACHO_API_GATEWAY_PLATFORM_TOKEN_REVOCATION_CA_FILE"

// platformRevocationCertEnv / platformRevocationKeyEnv — ручки КЛИЕНТСКОЙ пары
// этого хопа.
//
// ПОЧЕМУ ЗДЕСЬ ОНА НУЖНА, А НА СОСЕДНЕМ ХОПЕ НЕТ. Авторитет отзыва — НАШ, он
// живёт на внутреннем слушателе и спрашивающего опознаёт: слушатель запрашивает
// сертификат, а сам авторитет отвечает опознавательным словом тому, кто
// проверенной цепочки не предъявил. Хоп за набором ключей раздаёт открытый
// материал и спрашивающего не опознаёт.
//
// ПОЧЕМУ РУЧКА, А НЕ ВЫВОД ИЗ УЖЕ НАСТРОЕННОЙ ЛИЧНОСТИ. Выведенная пара всегда
// непуста — значит контроль выглядел бы настроенным в любом профиле, включая
// тот, где пары нет, и «предъявляем сертификат» стало бы неотличимо от
// «предъявлять нечего». Адрес и якорь доверия на этом хопе заданы явно по той
// же причине; личность — третья величина того же рода.
const (
	platformRevocationCertEnv = "KACHO_API_GATEWAY_PLATFORM_TOKEN_REVOCATION_CERT_FILE"
	platformRevocationKeyEnv  = "KACHO_API_GATEWAY_PLATFORM_TOKEN_REVOCATION_KEY_FILE"
)

// newPlatformRevocationHopClient — тот же клиент для хопа к нашему авторитету
// отзыва, но ПРЕДЪЯВЛЯЮЩИЙ клиентскую пару, когда она задана.
//
// По этому хопу едет ПРЕДЪЯВЛЕННЫЙ токен: авторитет спрашивают, посылая ему
// само удостоверение, — а прочитанное с провода удостоверение пригодно тому, кто
// его прочитал.
//
// Пара пуста ⇒ хоп идёт без сертификата: профиль, где авторитет его не
// спрашивает, законен и отказывать ему нечем. Пара задана НАПОЛОВИНУ ⇒ отказ в
// старте: половина означает намерение оператора, а молча не исполненное
// намерение здесь стоит вечного fail-closed на каждом предъявителе нашей
// чеканки — того самого состояния, которое нельзя увидеть, не спросив
// авторитета.
func newPlatformRevocationHopClient(
	caFile, certFile, keyFile string, timeout time.Duration,
) (*http.Client, error) {
	certFile, keyFile = strings.TrimSpace(certFile), strings.TrimSpace(keyFile)
	switch {
	case certFile == "" && keyFile == "":
		return newPinnedHopClientWithIdentity(platformRevocationCAEnv, caFile, nil, timeout)
	case keyFile == "":
		return nil, fmt.Errorf(
			"%s=%q задан без %s — отказ в старте: авторитет отзыва спрашивает "+
				"проверенную цепочку, половина пары её не даёт, и хоп ушёл бы в "+
				"постоянный отказ каждому предъявителю нашей чеканки",
			platformRevocationCertEnv, certFile, platformRevocationKeyEnv)
	case certFile == "":
		return nil, fmt.Errorf(
			"%s=%q задан без %s — отказ в старте: ключ без сертификата предъявить нечего",
			platformRevocationKeyEnv, keyFile, platformRevocationCertEnv)
	}

	pair, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf(
			"%s=%q / %s=%q не читаются как пара (%v) — отказ в старте: продолжить "+
				"без сертификата значило бы объявить личность на хопе и не предъявлять её",
			platformRevocationCertEnv, certFile, platformRevocationKeyEnv, keyFile, err)
	}
	return newPinnedHopClientWithIdentity(platformRevocationCAEnv, caFile, &pair, timeout)
}

// newPinnedHopClient строит клиент, доверяющий ТОЛЬКО указанной связке.
//
// `envName` попадает в текст отказа: оператор, читающий отказ, обязан узнать, какую
// ручку ему править, не открывая исходник.
func newPinnedHopClient(envName, caFile string, timeout time.Duration) (*http.Client, error) {
	return newPinnedHopClientWithIdentity(envName, caFile, nil, timeout)
}

// newPinnedHopClientWithIdentity — та же связка плюс НЕОБЯЗАТЕЛЬНАЯ клиентская
// пара. Одна реализация на все хопы: два экземпляра одного кода разъезжаются, и
// разъезжается ровно тот, где дефект ещё не нашли.
func newPinnedHopClientWithIdentity(
	envName, caFile string, identity *tls.Certificate, timeout time.Duration,
) (*http.Client, error) {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	if strings.TrimSpace(caFile) == "" {
		if identity == nil {
			return &http.Client{Timeout: timeout}, nil
		}
		// Якоря нет, а личность есть: связку сузить нечем, но предъявить пару
		// мы обязаны — иначе заданная оператором личность молча не доедет.
		tr := http.DefaultTransport.(*http.Transport).Clone()
		tr.TLSClientConfig = &tls.Config{
			Certificates: []tls.Certificate{*identity},
			MinVersion:   tls.VersionTLS12,
		}
		return &http.Client{Timeout: timeout, Transport: tr}, nil
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
	if identity != nil {
		tr.TLSClientConfig.Certificates = []tls.Certificate{*identity}
	}
	return &http.Client{Timeout: timeout, Transport: tr}, nil
}
