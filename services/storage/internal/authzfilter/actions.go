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
	// ResourceTypeComputeInstance — тип объекта ЧУЖОГО домена, и это осознанно.
	// InternalVolumeService/ListAttachments перечисляет привязки инстансов, которые
	// назвал вызывающий; право видеть эти привязки вытекает из права на ИНСТАНС, а
	// не на каждый том в отдельности. Модель прав общая для всех доменов, поэтому
	// вопрос задаётся напрямую — вызова в compute не происходит, цикла нет.
	ResourceTypeComputeInstance = "compute_instance"
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
	// ActionAttachmentsList — аудит-строка перечисления привязок; совпадает с
	// permission-строкой этого RPC в каталоге, чтобы аудит iam отличал перечисление
	// привязок от перечисления томов.
	ActionAttachmentsList = "storage.volumes.listAttachments"
)
