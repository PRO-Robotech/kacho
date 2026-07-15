// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package domain

import "fmt"

// DiskType — тип диска (admin-справочник; публичный read-only Get/List, admin CRUD
// через Internal* на :9091). id — admin-assigned slug ("network-ssd"). zone_ids —
// зоны, где тип доступен (cross-service ссылки на geo.Zone по id, без FK).
type DiskType struct {
	ID              string
	Name            string
	Description     string
	ZoneIDs         []string
	PerformanceTier string
}

// Validate проверяет domain-инварианты DiskType перед сохранением.
func (d DiskType) Validate() error {
	if d.ID == "" {
		return fmt.Errorf("disk_type id is required")
	}
	return validateName("disk_type name", d.Name)
}
