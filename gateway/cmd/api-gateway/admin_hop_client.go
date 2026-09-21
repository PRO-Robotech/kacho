// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// admin_hop_client.go — HTTP-клиенты хопов, по которым край добывает то, чем
// решает о доступе.
//
// ЧТО ПО НИМ ЕДЕТ. Хопов два: за ключами верификации — материалом, которым край
// проверяет ПОДПИСЬ каждого предъявителя, — и к НАШЕМУ авторитету отзыва,
// которого спрашивают, посылая ему само предъявленное удостоверение. Второй
// поэтому несёт живое удостоверение, а не административный вызов: прочитавший
// его с провода им и воспользуется.
//
// ЗДЕСЬ БЫЛ ТРЕТИЙ — К АДМИНИСТРАТИВНОМУ API ЧУЖОГО ПОСТАВЩИКА. Он нёс оба его
// вызова: снятие сессии входа на выходе человека и интроспекцию его токенов.
// Снят вместе с обоими: сессии такой не заводится, токенов таких край не
// принимает.
//
// ПОЧЕМУ ЯКОРЬ ДОВЕРИЯ — РУЧКА, А НЕ ВЫВОД. Перевод хопа на TLS помогает лишь
// тогда, когда сертификат ПРОВЕРЯЕТСЯ, а сертификат внутрикластерного адреса
// выписан внутренним центром, которого в корнях процесса по умолчанию нет.
// Значит путь связки — настройка, ровно как и сам адрес.
//
// ПОЧЕМУ НЕГОДНЫЙ ЯКОРЬ ОТКАЗЫВАЕТ В СТАРТЕ. Соблазнительный откат — «связку
// прочитать не смог, пойду по системным корням» — даёт единственное состояние,
// которого никто не видит: оператор настроил проверку по внутреннему центру,
// процесс её не делает, и всё работает до первой ротации сертификата. Отказ при
// старте отдаёт это оператору в тот момент, когда он смотрит.
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

// keySetHopCAEnv — ручка якоря доверия ХОПА ЗА КЛЮЧАМИ ВЕРИФИКАЦИИ.
//
// Держится константой, чтобы текст отказа старта и поле настроек не разъехались:
// оператор, читающий отказ, обязан суметь по нему действовать, не открывая этот
// файл.
//
// ПЕРЕИМЕНОВАНА вместе со своим полем (прежде `KACHO_HYDRA_JWKS_CA_FILE`): имя
// несло чужой продукт, а предмет у ручки наш — хоп едет к нашему зеркалу набора
// на внутреннем слушателе службы доступа.
const keySetHopCAEnv = "KACHO_API_GATEWAY_TOKEN_KEYSET_CA_FILE"

// newJWKSHopClient — клиент хопа за ключами верификации.
//
// caFile пусто ⇒ якоря нет, транспорт по умолчанию неизменён. Это не упущение:
// внутрикластерный адрес, отданный по открытому HTTP, связки не требует, и
// выдумывать её значило бы отвергнуть стенд, намеренно так настроенный.
//
// caFile задано ⇒ клиент проверяет узел по ЭТОЙ связке и ни по чему больше. Не
// «вдобавок к системным корням»: хопу внутреннего центра нечего принимать
// публично выписанный сертификат на то же имя, и сужение якоря есть весь смысл
// его закрепления.
//
// Отдельное имя, ОДНА реализация с соседним хопом: два экземпляра одного кода
// разъезжаются, и разъезжается ровно тот, где дефект ещё не нашли. Разница
// между хопами — только имя ручки в тексте отказа, и она параметр.
func newJWKSHopClient(caFile string, timeout time.Duration) (*http.Client, error) {
	return newPinnedHopClient(keySetHopCAEnv, caFile, timeout)
}

// platformRevocationCAEnv — ручка якоря доверия хопа к НАШЕМУ авторитету отзыва.
const platformRevocationCAEnv = "KACHO_API_GATEWAY_PLATFORM_TOKEN_REVOCATION_CA_FILE"

// platformRevocationCertEnv / platformRevocationKeyEnv — ручки КЛИЕНТСКОЙ пары
// этого хопа.
//
// ПОЧЕМУ ЗДЕСЬ ОНА НУЖНА, А НА ДВУХ СОСЕДНИХ ХОПАХ НЕТ. Авторитет отзыва —
// НАШ, он живёт на внутреннем слушателе и спрашивающего опознаёт: слушатель
// запрашивает сертификат, а сам авторитет отвечает опознавательным словом
// тому, кто проверенной цепочки не предъявил. Соседние хопы идут к внешнему
// поставщику, который нас так не спрашивает.
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
// По этому хопу едет ПРЕДЪЯВЛЕННЫЙ токен, а не только административный вызов:
// авторитет спрашивают, посылая ему само удостоверение. Значит требование к
// транспорту здесь то же, что у административного хопа, и по той же причине —
// прочитанное с провода удостоверение пригодно тому, кто его прочитал.
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
