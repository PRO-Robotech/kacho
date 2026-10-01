// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Command notify-probe — проба-источник извещений стенда (NTF-1, З29).
//
// Служба kacho, подключённая к notify процедурой «подключить службу»: лента
// извещений в своей базе (kacho_notifyprobe), сервер ленты
// (`corelib.notify.InternalNotificationFeedService`), журнал подписки с одним
// ключом ленты и сервер подписки, звено идентичности служб на перечне
// `{Subscribe, Claim, Ack}` с таблицей `{SAN notify → notify}`, шаблон
// `probe-hello` класса notice. Проба — стендовый объект: в цепочке `prod` её
// нет, и письма она ставит только по вызову, расписания у неё нет.
//
// Обе её службы — Internal*: на внешний край не выходят (ban #6) и служатся
// только внутренним слушателем.
package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/PRO-Robotech/corelib/observability"

	"github.com/PRO-Robotech/kacho/services/notify/cmd/notify-probe/internal/config"
)

const usage = "usage: notify-probe {serve}"

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	err := run(ctx, os.Args[1:], os.Stdout)
	cancel()
	if err != nil {
		log.Fatal(err)
	}
}

// run — процесс целиком: разбор команды, конфигурация, подъём. Отказ
// конфигурации возвращается ДО первого обращения к базе и до подъёма
// слушателей: незаданный флаг доставки — отказ старта с именем переменной
// (NTF1-N08), а не «модуль выключен».
func run(ctx context.Context, args []string, out io.Writer) error {
	if len(args) < 1 {
		return fmt.Errorf("%s", usage)
	}
	switch args[0] {
	case "serve":
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("config: %w", err)
		}
		logger := observability.NewSlogger(out)
		slog.SetDefault(logger)
		return runServe(ctx, cfg, logger)
	default:
		return fmt.Errorf("unknown command %q (%s)", args[0], usage)
	}
}
