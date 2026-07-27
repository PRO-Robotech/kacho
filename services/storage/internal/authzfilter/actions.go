// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package authzfilter

// FGA object types storage-домена (передаются в AuthorizeService.BatchCheck как
// `resource.type`). Обязаны совпадать с closed-table object-типами
// internal/check/permission_map.go и permission-catalog api-gateway — иначе фильтр
// спрашивал бы про объект, которого в модели нет, и отказывал бы ВСЕМ.
const (
	ResourceTypeVolume   = "storage_volume"
	ResourceTypeSnapshot = "storage_snapshot"
	ResourceTypeImage    = "storage_image"
)

// Action-строки storage-домена, формат `<domain>.<resource>.<verb>`.
//
// Action едет в каждом AuthorizeCheckRequest для аудита/трассировки; РЕШЕНИЕ
// принимается по явному `required_relation`, который фильтр пинит на батч
// (`viewer` — см. visibilityRelations), а не по server-side
// деривации verb→relation. Но verb всё равно ОБЯЗАН быть из числа тех, что
// kacho-iam умеет резолвить (канонические get/list), иначе запрос падает на
// action-валидации ещё до проверки отношения.
//
// Значения — ровно те `Permission`, что несут List-RPC в permission_map.go
// (storage.volumes.list / storage.snapshots.list / storage.images.list), поэтому
// аудит-строка фильтра совпадает со строкой per-RPC Check того же вызова.
const (
	ActionVolumeList   = "storage.volumes.list"
	ActionSnapshotList = "storage.snapshots.list"
	ActionImageList    = "storage.images.list"
)
