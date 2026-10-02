// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Command kacho-notify — шлюз уведомлений платформы (NTF-1).
//
// Композиционный корень: загрузка конфигурации, страж старта, дескриптор
// посадки, диагностическая поверхность. gRPC-слушателей у notify нет вовсе
// (форма хоста `no-grpc`, З15, Д17): входящего глагола у шлюза нет, письма он
// забирает сам из лент источников.
package main

import (
	"log/slog"
	"os"

	"github.com/PRO-Robotech/corelib/observability"

	"github.com/PRO-Robotech/kacho/services/notify/internal/config"
)

func main() {
	logger := observability.NewSlogger(os.Stdout)
	slog.SetDefault(logger)

	if len(os.Args) != 2 || os.Args[1] != "serve" {
		logger.Error("использование: kacho-notify serve")
		os.Exit(2)
	}

	// Страж старта — до подъёма чего бы то ни было: незаданная, пустая или
	// вне границы ручка останавливает процесс с её именем (ban #16).
	cfg, err := config.Load()
	if err != nil {
		logger.Error("отказ старта", "err", err.Error())
		os.Exit(1)
	}
	if err := cfg.Validate(); err != nil {
		logger.Error("отказ старта", "err", err.Error())
		os.Exit(1)
	}

	if err := runServe(cfg, logger); err != nil {
		logger.Error("отказ старта", "err", err.Error())
		os.Exit(1)
	}
}
