// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package domain

import "time"

// Zone — зона размещения (Geography — leaf-домен kacho-geo). Здесь — узкая
// read-проекция для валидации zone_id через порт ZoneRegistry: kacho-vpc хранит
// zone_id как TEXT без FK и проверяет существование вызовом geo.v1.ZoneService.Get.
// Поле-подпись у зоны стояло ЗДЕСЬ и снято вместе с полем у владельца (#716):
// идентичность зоны — её идентификатор, назначаемый администратором и
// человекочитаемый by construction. Зеркало было односторонним — сюда писали,
// отсюда не читал никто.
type Zone struct {
	ID        string
	RegionID  string
	CreatedAt time.Time
}

// Region — регион (Geography — leaf-домен kacho-geo). Узкая read-проекция для
// валидации region_id REGIONAL-подсети через порт RegionRegistry: kacho-vpc
// хранит region_id как TEXT без FK и проверяет существование вызовом
// geo.v1.RegionService.Get.
// Подпись региона снята вместе с полем у владельца (#716) — см. Zone выше.
type Region struct {
	ID        string
	CreatedAt time.Time
}
