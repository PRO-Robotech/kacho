// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// revocation_transport.go — учётные данные ребра к авторитету отзыва.
//
// # Почему у этого ребра СВОЙ клиент, а не общий с загрузкой набора ключей
//
// Набор проверочных ключей несёт только публичный материал, и его маршрут
// открыт задокументированным исключением из требования аутентификации. Это
// обоснование НЕ распространяется на маршрут, которому ПРИСЫЛАЮТ предъявленный
// токен: там на проводе оказывается удостоверение, а не публичный ключ.
// Переиспользовать исключение молча значило бы принять запрещённое допущение
// «внутренний периметр доверенный».
//
// Поэтому: загрузка набора остаётся origin-agnostic и клиентского сертификата не
// предъявляет, а обращение к авторитету отзыва идёт ОТДЕЛЬНЫМ клиентом со своими
// учётными данными — по той же дисциплине «на ребро», что и остальные исходящие
// рёбра реестра.
//
// # Якорь, объявленный и непригодный, роняет СТАРТ
//
// Откат на системные корни здесь запрещён: он всегда «работает», поэтому ошибка
// в якоре становится ненаблюдаемой, а доверие — незаявленным. Всякая
// непригодность (файла нет, в файле нет сертификатов, пара ключей не читается,
// имя сервера не задано) — отказ при построении.
package jwks

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// RevocationTransport — объявленные учётные данные ребра «реестр → авторитет
// отзыва». Поля повторяют дисциплину клиентских учётных данных остальных рёбер:
// якорь, пара клиентского сертификата, проверяемое имя сервера.
type RevocationTransport struct {
	// Enable — ребро идёт по TLS с предъявлением клиентского сертификата.
	// Выключено допустимо только на открытом HTTP (локальные фикстуры).
	Enable bool
	// CAFiles — якоря доверия, которыми проверяется сертификат авторитета.
	// Пусто при Enable — отказ: молчаливый откат на системные корни означал бы
	// доверие, которого никто не объявлял.
	CAFiles []string
	// CertFile/KeyFile — пара клиентского сертификата. Авторитет отвергает
	// вызывающего, чью цепочку транспорт не проверил, поэтому пара обязательна:
	// без неё контроль присутствует и не отказывает ни разу, потому что каждый
	// его вызов отвергается на рукопожатии.
	CertFile string
	KeyFile  string
	// ServerName — имя, проверяемое против сертификата авторитета. Пусто
	// отключило бы эту проверку — отказ.
	ServerName string
}

// httpClientFor строит клиента обращений к авторитету по объявленному адресу.
//
// Разбор адреса нужен именно здесь: «объявлено TLS, а адрес открытый» и
// «адрес закрытый, а учётных данных нет» — два разных вида несогласованности, и
// оба обязаны быть видны при построении, а не на первом запросе.
func (t RevocationTransport) httpClientFor(authorityURL string) (*http.Client, error) {
	u, err := url.Parse(strings.TrimSpace(authorityURL))
	if err != nil {
		return nil, fmt.Errorf("revocation authority URL %q is not a URL: %w", authorityURL, err)
	}
	secure := strings.EqualFold(u.Scheme, "https")

	if !t.Enable {
		if secure {
			return nil, errors.New("revocation authority is reached over https but its client credentials are " +
				"not declared: the authority refuses a caller whose chain the transport did not verify, so the " +
				"control would exist and never refuse — every call of it would fail the handshake")
		}
		if len(t.CAFiles) > 0 || t.CertFile != "" || t.KeyFile != "" || t.ServerName != "" {
			return nil, errors.New("revocation transport credentials are declared but disabled — " +
				"a declaration nobody reads outlives its subject silently")
		}
		return &http.Client{Timeout: introspectionTimeout}, nil
	}

	if !secure {
		return nil, fmt.Errorf("revocation transport credentials are declared, but the authority URL scheme "+
			"is %q: client credentials mean nothing without TLS", u.Scheme)
	}
	if len(t.CAFiles) == 0 {
		return nil, errors.New("revocation transport is enabled but no trust anchor is declared; " +
			"falling back to the system roots would make the trust unstated and the misconfiguration invisible")
	}
	if t.ServerName == "" {
		return nil, errors.New("revocation transport is enabled but no server name is declared; " +
			"an empty name disables the check of the authority's certificate")
	}
	if t.CertFile == "" || t.KeyFile == "" {
		return nil, errors.New("revocation transport is enabled but the client certificate pair is incomplete; " +
			"the authority refuses a caller whose chain the transport did not verify")
	}

	pool := x509.NewCertPool()
	for _, f := range t.CAFiles {
		pem, rerr := os.ReadFile(f)
		if rerr != nil {
			return nil, fmt.Errorf("revocation trust anchor %q could not be read: %w", f, rerr)
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("revocation trust anchor %q carries no certificate: "+
				"a declared but unusable anchor refuses the start rather than silently doing nothing", f)
		}
	}
	pair, lerr := tls.LoadX509KeyPair(t.CertFile, t.KeyFile)
	if lerr != nil {
		return nil, fmt.Errorf("revocation client certificate pair could not be loaded: %w", lerr)
	}

	return &http.Client{
		Timeout: introspectionTimeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs:      pool,
				Certificates: []tls.Certificate{pair},
				ServerName:   t.ServerName,
				MinVersion:   tls.VersionTLS12,
			},
			// Собственные сроки на установление соединения: ребро стоит на пути
			// запроса, и неотвечающий пир обязан давать отказ, а не удерживать
			// горутину до общего срока.
			TLSHandshakeTimeout:   3 * time.Second,
			ResponseHeaderTimeout: introspectionTimeout,
		},
	}, nil
}
