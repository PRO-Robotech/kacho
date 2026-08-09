// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package serviceerr

import (
	kerrors "github.com/PRO-Robotech/kacho/pkg/errors"
)

// serviceDomain — источник отказа в ErrorInfo.domain. Назван один раз на сервис:
// повторённый по местам вызова литерал разъезжается молча, и разъедется он
// именно там, где деталь читают машиной, а не глазом.
const serviceDomain = "vpc"

// UnknownZone / UnknownRegion — полоса peer-validate к владельцу Geography
// (kacho-geo): чужой идентификатор корректен по форме, но у владельца не
// резолвится.
//
// Код — FAILED_PRECONDITION, а не INVALID_ARGUMENT: консумер здесь не «не нашёл
// СВОЁ», а «предусловие на ЧУЖОЙ ресурс не выполнено» (api-conventions.md
// §By-lane code-split). Код берётся у полосы, а не выписывается здесь, поэтому
// сменить канон можно ровно в одном месте — в объявлении полосы.
//
// Тексты не меняются: они часть контракта Kachō и утверждаются дословно.
// HTTP-статус края от смены кода тоже не меняется — по таблице
// api-conventions.md §gRPC-код → HTTP оба кода дают 400. Отличить полосу можно
// ровно по коду и машинному признаку, и это единственное, что здесь добавлено.
func UnknownZone(zoneID string) error {
	return kerrors.ReasonPeerResourceMissing.Errf(
		kerrors.PeerRef{Service: serviceDomain, ResourceType: "geo.zone", ResourceID: zoneID},
		"unknown zone id '%s'", zoneID)
}

func UnknownRegion(regionID string) error {
	return kerrors.ReasonPeerResourceMissing.Errf(
		kerrors.PeerRef{Service: serviceDomain, ResourceType: "geo.region", ResourceID: regionID},
		"unknown region id '%s'", regionID)
}
