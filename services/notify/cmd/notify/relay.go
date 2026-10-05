// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"crypto/x509"

	"github.com/PRO-Robotech/kacho/services/notify/internal/config"
	"github.com/PRO-Robotech/kacho/services/notify/internal/smtp"
)

// newRelaySender — отправитель из загруженной и проверенной конфигурации
// (З20 «Узел почты», З26): узел, порт, режим TLS и имя `AUTH` — из разобранного
// адреса `notify.smtp.connectionURI` ([config.Config.Relay]); удостоверение —
// ручка `notify.smtp.credential`; срок — `notify.smtp.sessionTimeout`;
// доверенный набор — roots. Имя проверки сертификата — узел адреса
// (smtp.NewSender ставит ServerName = Relay.Host). Читатель разобранного
// адреса один — этот набор соединения (CX1-78).
func newRelaySender(cfg config.Config, roots *x509.CertPool) (*smtp.Sender, error) {
	r := cfg.Relay()
	return smtp.NewSender(smtp.Relay{
		Host:           r.Host,
		Port:           r.Port,
		ImplicitTLS:    r.ImplicitTLS,
		Roots:          roots,
		Username:       r.Username,
		Credential:     cfg.Credential().Value(),
		SessionTimeout: cfg.SMTPSessionTimeout,
	})
}
