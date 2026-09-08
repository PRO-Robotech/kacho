// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package authzfilter

// Compute-domain FGA object types (consumed by
// iam.BatchCheck.checks[].resource.type). Must stay in sync with
// internal/check/permission_map.go.
const (
	ResourceTypeInstance       = "compute_instance"
	ResourceTypeGuestAccessKey = "compute_guest_access_key"
	ResourceTypePlacementGroup = "compute_placement_group"
)

// Compute-domain action strings. Format: `<domain>.<resource>.<verb>`.
//
// The action is carried on every AuthorizeCheckRequest for audit/trace; the
// DECISION is taken on the explicit `required_relation` the filter pins per batch
// (`v_get` — see authzfilter.visibilityRelations), not on a server-side
// verb→relation derivation. The verb still MUST be one kaname maps
// (`resolveActionToRelation` maps only the canonical RPC verbs get/list; "read" is
// UNMAPPED → "Illegal argument action"), because a request that fails
// action-validation never reaches the relation check.
//
// `v_get` is the SAME relation the per-RPC Check gate uses for Get
// (internal/check/permission_map.go `InstanceService/Get`) — that is what makes
// List visibility == Check-allow (read==enforce).
//
// Прежняя редакция этого абзаца называла таким отношением `viewer`. Это было
// неверно: Get гейтится `v_get`, а ярусные (`viewer`/`editor`/`admin`) и
// глагольные (`v_*`) отношения в модели РАЗВЯЗАНЫ — ни одно не выводится из
// другого. Комментарий утверждал инвариант, которого код не держал, и переживал
// обзоры именно поэтому.
const (
	ActionInstanceRead       = "compute.instances.list"
	ActionGuestAccessKeyRead = "compute.guest_access_keys.list"
	ActionPlacementGroupRead = "compute.placement_groups.list"
)

// PerObjectTypes — пообъектные типы прав, которые поднимает compute.
//
// Перечень назван ОДИН раз и читается всеми, кому он нужен (сужатель, дескриптор
// процесса, формы сокрытия существования). Выписанный второй раз, он разошёлся
// бы молча — и разошёлся бы именно там, где расхождение означает неполную карту
// скрытия, то есть отличимый ответ на «нет доступа».
var PerObjectTypes = []string{ResourceTypeInstance, ResourceTypeGuestAccessKey, ResourceTypePlacementGroup}
