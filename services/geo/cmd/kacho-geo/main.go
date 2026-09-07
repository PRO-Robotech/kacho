// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Command kacho-geo — gRPC control-plane Geography (Region / Zone).
//
// Leaf-сервис платформенной топологии: по build не зависит ни от чего в Kachō,
// в runtime — consumer authz Check у kaname. Публичный :9090 — read-only
// (RegionService/ZoneService Get/List); cluster-internal :9091 — admin CRUD
// (InternalRegion/ZoneService), никогда не на внешнем TLS endpoint (только
// cluster-internal).
package main

import (
	"log"
	"os"

	"github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/config"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: kacho-geo {serve}")
	}
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	// СТРАЖ ПОСАДКИ ЗДЕСЬ НЕ СТОИТ — И ЭТО НЕ ПРОПУСК.
	//
	// Посадку судит центральный дескриптор: `describe` в serve.go подаёт режим,
	// `sslmode`, круг отправителей и транспорт обоих слушателей в
	// `servicecontract.New`, ошибка возвращается наверх и роняет старт ДО подъёма
	// слушателей. Локальная копия тех же двух осей стояла здесь один круг и была
	// снята: она несла СВОЙ перечень безопасных значений `sslmode` в файле, чей
	// пакет и так импортирует `pkg/servicecontract`, и звала тот же
	// `grpcsrv.ForwarderGate` с теми же именами ручек. Два места об одном
	// предмете, из которых верно одно, — и расходятся они молча.
	//
	// Довод «второй процесс из того же конфига получит его без проверки» не
	// держится замером: мигратор (`cmd/migrator`) грузит этот конфиг ради одного
	// DSN и стража НЕ ЗВАЛ никогда — ни до снятия, ни после. Обоснование
	// описывало намерение, а не код.
	switch os.Args[1] {
	case "serve":
		if err := runServe(cfg); err != nil {
			log.Fatal(err)
		}
	default:
		log.Fatalf("unknown command: %s (migrations: use the kacho-migrator binary)", os.Args[1])
	}
}
