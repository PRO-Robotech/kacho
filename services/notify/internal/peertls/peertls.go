// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package peertls — транспорт notify к внутренним серверам платформы (лентам
// источников и kaname): удостоверение notify для mTLS и точный URI SAN листа
// сервера. Одно место на оба ребра: проверка, расходящаяся между ними, —
// второе написание одного утверждения «этот сервер — тот, кого назвала
// конфигурация».
package peertls

import (
	"crypto/tls"
	"errors"
	"fmt"

	"google.golang.org/grpc/credentials"
)

// ErrSANMismatch — сервер предъявил удостоверение, отличное от ожидаемого:
// рукопожатие отвергнуто, вызов до сервера не доходит.
var ErrSANMismatch = errors.New("URI SAN сервера не совпадает с ожидаемым")

// ExactSAN — транспорт к серверу: цепочка и имя узла проверяются штатно по
// base, и сверх того лист сервера обязан нести ровно один URI-SAN, равный
// want (SPIFFE ID — один на лист). Иначе — отказ рукопожатия; refused
// получает предъявленные SAN (журнал и тревога — у вызывающего). base не
// меняется.
func ExactSAN(base *tls.Config, want string, refused func(got []string)) credentials.TransportCredentials {
	cfg := base.Clone()
	if cfg.MinVersion < tls.VersionTLS12 {
		cfg.MinVersion = tls.VersionTLS12
	}
	cfg.VerifyConnection = func(cs tls.ConnectionState) error {
		if len(cs.PeerCertificates) == 0 {
			refused(nil)
			return fmt.Errorf("%w: сервер не предъявил сертификат", ErrSANMismatch)
		}
		uris := cs.PeerCertificates[0].URIs
		if len(uris) == 1 && uris[0].String() == want {
			return nil
		}
		got := make([]string, 0, len(uris))
		for _, u := range uris {
			got = append(got, u.String())
		}
		refused(got)
		return fmt.Errorf("%w: ожидался %s, предъявлено %v", ErrSANMismatch, want, got)
	}
	return credentials.NewTLS(cfg)
}
