// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package protoconv — ЕДИНЫЙ источник конверсии domain→proto для kacho-geo
// (domain.Region/Zone → geov1.Region/Zone). Раньше это отображение было
// продублировано в трёх местах: тонкий handler (public Get/List), use-case
// marshaller (Operation.response async-worker'а) и LRO-recovery reader-adapter
// (reconciler разрешает осиротевшую операцию в тот же response). Три копии обязаны
// быть байт-идентичны (иначе клиент, поллящий одну логическую операцию, увидит
// разный payload в зависимости от того, отработал ли обычный worker или recovery).
// Централизация убирает риск дрейфа: новое поле geov1.Region/Zone добавляется в
// ОДНОМ месте.
//
// Пакет несёт только детерминированное field-mapping (+ единый timestamp-формат
// Kachō: created_at усекается до секунд). Он не является transport-слоем: и
// handler, и recovery-adapter, и use-case-marshaller одинаково зависят от него как
// от общего доменно-проекционного хелпера; anypb-обёртка (LRO-специфика) остаётся
// на стороне use-case.
package protoconv

import (
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	geov1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/geo/v1"

	"github.com/PRO-Robotech/kacho/services/geo/internal/domain"
)

// Region конвертирует domain.Region → geov1.Region (created_at усекается до секунд).
func Region(r *domain.Region) *geov1.Region {
	return &geov1.Region{
		Id:        r.ID,
		Name:      r.Name,
		CreatedAt: ts(r.CreatedAt),
	}
}

// Zone конвертирует domain.Zone → geov1.Zone (created_at усекается до секунд).
func Zone(z *domain.Zone) *geov1.Zone {
	return &geov1.Zone{
		Id:        z.ID,
		RegionId:  z.RegionID,
		Status:    geov1.Zone_Status(z.Status),
		Name:      z.Name,
		CreatedAt: ts(z.CreatedAt),
	}
}

// ts — единый timestamp-формат Kachō: усечение до секунд перед проекцией в proto.
func ts(t time.Time) *timestamppb.Timestamp {
	return timestamppb.New(t.Truncate(time.Second))
}
