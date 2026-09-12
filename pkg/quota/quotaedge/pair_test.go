// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: Apache-2.0

package quotaedge

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/corelib/grpcclient"
	corequota "github.com/PRO-Robotech/corelib/quota"
)

// pair_test.go — проба предиката полноты пары.
//
// У каждого отрицательного случая здесь есть положительный близнец, меняющий
// РОВНО ОДИН факт: иначе неизвестно, что именно дало отказ, и предикат,
// отвергающий всё, неотличим от предиката, отвергающего нужное.

const peer = "kaname-internal.kacho.svc"

func absent(t grpcclient.TLSClient) Pair {
	return Pair{
		AuthorityKnob:  "KACHO_COMPUTE_QUOTA_AUTHORITY",
		Authority:      corequota.NotDeployed,
		TransportKnob:  "KACHO_COMPUTE_QUOTA_AUTHORITY_MTLS_ENABLE",
		ServerNameKnob: "KACHO_COMPUTE_QUOTA_AUTHORITY_MTLS_SERVERNAME",
		Transport:      t,
	}
}

// TestAbsentAuthorityWithServerNameIsRefused — предмет: имя для сверки при
// объявленном отсутствии собеседника.
func TestAbsentAuthorityWithServerNameIsRefused(t *testing.T) {
	err := ValidateAbsentAuthorityCarriesNoTransport(absent(grpcclient.TLSClient{ServerName: peer}))
	require.Error(t, err)
	require.Contains(t, err.Error(), "KACHO_COMPUTE_QUOTA_AUTHORITY_MTLS_SERVERNAME")
	require.Contains(t, err.Error(), peer, "отказ обязан процитировать имя")
	require.Contains(t, err.Error(), "KACHO_COMPUTE_QUOTA_AUTHORITY",
		"отказ обязан назвать и ручку адреса — иначе следующий шаг оператора не восстановлен")
}

// TestAbsentAuthorityWithEnableOnlyIsRefused — включённое удостоверение без
// имени тоже половина пары: заводится клиент к отсутствующему соседу.
func TestAbsentAuthorityWithEnableOnlyIsRefused(t *testing.T) {
	err := ValidateAbsentAuthorityCarriesNoTransport(absent(grpcclient.TLSClient{Enable: true}))
	require.Error(t, err)
	require.Contains(t, err.Error(), "KACHO_COMPUTE_QUOTA_AUTHORITY_MTLS_ENABLE = true")
}

// TestAbsentAuthorityWithTrustRootsOnlyIsRefused — корни доверия БЕЗ включения:
// самая тихая половина, потому что не видна ни в одном булевом признаке.
func TestAbsentAuthorityWithTrustRootsOnlyIsRefused(t *testing.T) {
	err := ValidateAbsentAuthorityCarriesNoTransport(
		absent(grpcclient.TLSClient{CAFiles: []string{"/etc/tls/ca.crt"}}))
	require.Error(t, err)
	require.Contains(t, err.Error(), "доверенные корни заданы")
}

// TestAbsentAuthorityWithEmptyTrustRootsIsSilent — близнец предыдущего: пустая
// строка в перечне корней — НЕ объявление.
//
// Без него предикат краснел бы на `cafiles: [""]`, которое рендерит чарт при
// пустом значении, и находка была бы ложной.
func TestAbsentAuthorityWithEmptyTrustRootsIsSilent(t *testing.T) {
	require.NoError(t, ValidateAbsentAuthorityCarriesNoTransport(
		absent(grpcclient.TLSClient{CAFiles: []string{"", " "}})))
}

// TestAbsentAuthorityWithoutTransportIsSilent — положительный близнец: законная
// посадка «домена величин в этой установке нет».
func TestAbsentAuthorityWithoutTransportIsSilent(t *testing.T) {
	require.NoError(t, ValidateAbsentAuthorityCarriesNoTransport(absent(grpcclient.TLSClient{})))
}

// TestAbsentAuthorityWithPaddedSpellingIsRefused — пробелы вокруг написания не
// выводят посадку из-под предиката.
//
// Разбор объявления их обрезает, поэтому предикат, судящий сырое значение, молчал
// бы там, где процесс считает домен отсутствующим.
func TestAbsentAuthorityWithPaddedSpellingIsRefused(t *testing.T) {
	p := absent(grpcclient.TLSClient{ServerName: peer})
	p.Authority = "  " + corequota.NotDeployed + "\n"
	require.Error(t, ValidateAbsentAuthorityCarriesNoTransport(p))
}

// TestDeclaredAuthorityWithTransportIsSilent — предикат сужает только посадку
// объявленного ОТСУТСТВИЯ.
//
// Объявленный адрес вместе с удостоверением — штатная боевая посадка, и красное
// на ней означало бы, что развернуть домен величин нельзя вовсе.
func TestDeclaredAuthorityWithTransportIsSilent(t *testing.T) {
	p := absent(grpcclient.TLSClient{Enable: true, ServerName: peer})
	p.Authority = "kaname-internal.kacho.svc:9091"
	require.NoError(t, ValidateAbsentAuthorityCarriesNoTransport(p))
}

// TestUnsetAuthorityIsNotThisPredicatesSubject — незаданное значение судит
// разбор объявления, а не этот предикат.
//
// Второй отказ об одном предмете разошёлся бы с первым молча; здесь проверяется
// именно молчание, а не вердикт.
func TestUnsetAuthorityIsNotThisPredicatesSubject(t *testing.T) {
	p := absent(grpcclient.TLSClient{Enable: true, ServerName: peer})
	p.Authority = ""
	require.NoError(t, ValidateAbsentAuthorityCarriesNoTransport(p))
}
