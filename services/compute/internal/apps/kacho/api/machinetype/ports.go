// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package machinetype

import "github.com/PRO-Robotech/kacho/services/compute/internal/ports"

// Port-интерфейсы и связанные value-объекты вынесены в leaf-пакет
// `internal/ports` — это позволяет переиспользовать общий test-helper
// `internal/ports/portmock` без import-cycle. Здесь — type-alias'ы для
// удобства: use-case и adapter'ы (`internal/repo`) ссылаются на
// `machinetype.*` имена.

type (
	// Pagination — постраничная навигация.
	Pagination = ports.Pagination

	// MachineTypeRepo — port-интерфейс каталога machine-type (COMP-1 F7).
	MachineTypeRepo = ports.MachineTypeRepo
	// MachineTypeFilter — фильтр списка machine-type (name=/family=/minGpus=).
	MachineTypeFilter = ports.MachineTypeFilter
)
