// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package authzfilter

// FGA object types storage-домена (передаются в AuthorizeService.BatchCheck как
// `resource.type`). Обязаны совпадать с closed-table object-типами аннотаций
// методов в `proto/` (из них выводится карта прав) и permission-catalog api-gateway —
// иначе фильтр спрашивал бы про объект, которого в модели нет, и отказывал бы ВСЕМ.
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
// kaname умеет резолвить (канонические get/list), иначе запрос падает на
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
	// ActionVolumeAttach / ActionVolumeDetach — аудит-строки вопроса про ИНСТАНС на
	// путях привязки/отвязки. Совпадают с permission-строками этих RPC в каталоге,
	// поэтому в аудите iam второй вопрос того же вызова читается как его часть, а не
	// как посторонний запрос.
	ActionVolumeAttach = "storage.volumes.attach"
	ActionVolumeDetach = "storage.volumes.detach"
)

// Отношения на `compute_instance`, которыми гейтятся привязка и отвязка.
//
// Значения — РОВНО те, которыми compute гейтит собственные
// `InstanceService/AttachDisk` и `DetachDisk` в permission-каталоге (`v_update` на
// `compute_instance`). Совпадение обязательно: композитный путь (tenant → compute →
// storage) и прямой путь на внутренний листенер должны требовать одного и того же,
// иначе прямой путь оказывается слабее составного — а именно так и было, когда
// вопрос про инстанс не задавался вовсе.
//
// Оба verb'а несут в модели `or super_admin`, поэтому каскад трёх верхних уровней
// супер-доступа (облако / бутстрап / администратор аккаунта) резолвится без
// отдельной ветки.
const (
	// RelationInstanceUpdate — «менять эту машину». Привязка добавляет строку в её
	// набор привязок, то есть меняет машину.
	RelationInstanceUpdate = "v_update"
	// RelationInstanceDelete — «снести эту машину». Отвязка принимает его ДОПОЛНИТЕЛЬНО
	// к v_update: снос идёт под личностью инициатора, который держит право удаления, и
	// шаг освобождения томов обязан ему пройти. Требуй здесь только v_update — и машина
	// с томом станет неудаляемой для роли, у которой есть удаление и нет изменения
	// (строка машины удаляется ПОСЛЕДНЕЙ, после отвязки).
	RelationInstanceDelete = "v_delete"
)
