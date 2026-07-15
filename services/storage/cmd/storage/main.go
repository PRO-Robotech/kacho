// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Command storage — gRPC control-plane сервис Storage (Volume / VolumeAttachment /
// Snapshot / DiskType).
//
// Владелец домена Storage. Публичный :9090 — Volume/Snapshot/DiskType Service
// (Get/List sync + Create/Update/Delete async Operation); cluster-internal :9091 —
// InternalVolumeService (Attach/Detach/ListAttachments/GetInternal, ребро
// compute→storage) + InternalDiskTypeService (admin CRUD), никогда не на внешнем
// TLS endpoint (ban #6). Миграции — отдельный бинарь cmd/migrator.
package main

import (
	"log"
	"os"

	"github.com/PRO-Robotech/kacho/services/storage/internal/config"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: storage {serve}")
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
