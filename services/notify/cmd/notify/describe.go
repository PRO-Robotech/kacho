// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"fmt"
	"log/slog"

	"github.com/PRO-Robotech/corelib/grpcsrv"
	"github.com/PRO-Robotech/corelib/servicecontract"

	"github.com/PRO-Robotech/kacho/services/notify/internal/config"
)

// serviceName — имя процесса в дескрипторе и отказах.
const serviceName servicecontract.ServiceName = "kacho-notify"

// describe собирает дескриптор посадки notify и отдаёт его общему
// конструктору: режим и `sslmode` своей базы судит он, а не этот корень.
//
// Форма хоста — `no-grpc` (З15): gRPC-слушателей нет, поэтому проводки
// носителя нет вовсе, а круг отправителей и домен доверия объявлены
// неприменимыми — их читают звенья извлечения личности, которых у процесса без
// слушателей нет.
func describe(cfg config.Config, logger *slog.Logger) (servicecontract.Descriptor, error) {
	mode, err := servicecontract.ParseMode(cfg.AuthMode)
	if err != nil {
		return servicecontract.Descriptor{}, fmt.Errorf("KACHO_NOTIFY_AUTH_MODE: %w", err)
	}
	return servicecontract.New(servicecontract.Spec{
		Service: serviceName,
		Mode:    mode,
		Logger:  logger,

		HostForm: servicecontract.HostNoGRPC,

		Forwarders: servicecontract.NotApplicable[grpcsrv.TrustedForwarders](
			"gRPC-слушателей у notify нет (форма no-grpc): переданную личность конечного " +
				"пользователя принимать некому, ручки круга отправителей у процесса нет"),
		TrustDomain: servicecontract.NotApplicable[grpcsrv.TrustDomain](
			"gRPC-слушателей у notify нет (форма no-grpc): личность входящего предъявителя " +
				"не разбирается — входящих вызовов нет. Сервер ленты источника notify проверяет " +
				"по точному SAN записи перечня источников, а не по домену"),

		DBSSLMode: servicecontract.Value(cfg.DBSSLMode),
	})
}
