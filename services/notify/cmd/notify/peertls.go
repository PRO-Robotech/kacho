// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	"github.com/PRO-Robotech/kacho/services/notify/internal/config"
)

// checkPeerTLS — удостоверение notify для mTLS к источникам и kaname обязано
// быть ГОДНЫМ к моменту старта: сертификат службы с ключом образуют пару, в
// файле УЦ есть хотя бы один сертификат.
//
// Путь, заданный ручкой, но не дающий годного материала, — тот же «mTLS
// выключен» (ось NTF1-G15), что и незаданный путь: отказ старта с именем
// ручки, до подъёма чего бы то ни было, а не отказ первого соединения.
// Транспорт клиента источника (точный SAN сервера ленты, З21) строится из тех
// же ручек конфигурации.
func checkPeerTLS(cfg config.Config) error {
	certPEM, err := os.ReadFile(cfg.PeerTLSCertFile)
	if err != nil {
		return fmt.Errorf("%s: ось mTLS: сертификат службы не читается: %w",
			config.KnobOfField("PeerTLSCertFile"), err)
	}
	keyPEM, err := os.ReadFile(cfg.PeerTLSKeyFile)
	if err != nil {
		return fmt.Errorf("%s: ось mTLS: ключ службы не читается: %w",
			config.KnobOfField("PeerTLSKeyFile"), err)
	}
	if _, err := tls.X509KeyPair(certPEM, keyPEM); err != nil {
		return fmt.Errorf("%s, %s: ось mTLS: сертификат и ключ службы не образуют пару: %w",
			config.KnobOfField("PeerTLSCertFile"), config.KnobOfField("PeerTLSKeyFile"), err)
	}
	caPEM, err := os.ReadFile(cfg.PeerTLSCAFile)
	if err != nil {
		return fmt.Errorf("%s: ось mTLS: УЦ серверов не читается: %w",
			config.KnobOfField("PeerTLSCAFile"), err)
	}
	if !x509.NewCertPool().AppendCertsFromPEM(caPEM) {
		return fmt.Errorf("%s: ось mTLS: в файле УЦ нет ни одного сертификата PEM",
			config.KnobOfField("PeerTLSCAFile"))
	}
	return nil
}
