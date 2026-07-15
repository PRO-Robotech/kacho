// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Command kacho-geo — gRPC control-plane Geography (Region / Zone).
//
// Leaf-сервис платформенной топологии: по build не зависит ни от чего в Kachō,
// в runtime — consumer authz Check у kacho-iam. Публичный :9090 — read-only
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
	switch os.Args[1] {
	case "serve":
		if err := runServe(cfg); err != nil {
			log.Fatal(err)
		}
	default:
		log.Fatalf("unknown command: %s (migrations: use the kacho-migrator binary)", os.Args[1])
	}
}
