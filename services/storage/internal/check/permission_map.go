// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package check

import (
	storagev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/storage/v1"
	"github.com/PRO-Robotech/kacho/pkg/authz"
)

// FGA-скоупинг для kacho-storage — зеркалит permission-catalog api-gateway
// (defense-in-depth: ОБА листенера резолвят один и тот же scope-object).
//
// Семантика per-RPC (issue #62 fix — раньше ВСЁ гейтилось на cluster-синглтоне,
// из-за чего project-scoped `editor` (есть `project:<p>#viewer/editor`, но нет
// cluster-грантов) получал 403 на List/Get/Create/Update/Delete СВОЕГО проекта;
// bootstrap-суиты это маскировали через iam cluster-admin short-circuit):
//   - List/Create               → parent scope `project:<project_id>`, tier viewer/editor;
//   - Get/Update/Delete/ListOps  → object-self `storage_<res>:<res_id>`, tier viewer/editor;
//   - DiskType Get/List          → viewer на cluster singleton (глобальный read-only каталог);
//   - InternalVolume Attach/Detach → object-self `storage_volume:<volume_id>` ПЛЮС
//     вопрос про ВТОРОЙ объект (машину) на уровне данных, в use-case: у этих RPC
//     два адресата с разными владельцами, и per-RPC Check покрывает только один;
//   - InternalVolume GetInternal → object-self `storage_volume:<volume_id>`;
//   - InternalVolume ListAttachments → ScopeFiltered: единичного объекта нет (инстансы
//     называет вызывающий), видимость решается per-object в use-case;
//   - InternalImage GetInternal  → object-self `storage_image:<image_id>`, tier viewer;
//   - InternalDiskType CRUD      → system_admin на cluster singleton (admin каталог).
//
// Карта обязана покрывать КАЖДЫЙ served RPC обоих листенеров: corelib authz fail-closed
// (`DecisionUnmapped` → «permission denied (rpc not mapped)»), поэтому пропуск = RPC,
// нерабочий при любых грантах. Гейт класса — TestPermissionMap_CoversEveryServedStorageRPC
// (обход protobuf-дескрипторов `kacho.cloud.storage.v1`).
//
// Смысл гейта здесь — AuthN+AuthZ на ОБОИХ листенерах (internal :9091 НЕ освобождён,
// security.md): defense-in-depth поверх mTLS. Авторитетный object-scoped scope_extractor
// дублируется в permission-catalog api-gateway; тексты relation/object совпадают,
// поэтому оба Check выносят одно и то же решение. cluster_kacho_root — ClusterSingletonID
// из kacho-iam, один на деплой. owner-tuple materialization (fgaproxy RegisterResource) —
// см. internal/fgaregister; project-editor per-object доступ на storage-объекты
// материализуется iam-реконсайлером (см. iam authzmap/feed_registry storage-типы).
const (
	objectTypeProject  = "project"
	objectTypeVolume   = "storage_volume"
	objectTypeSnapshot = "storage_snapshot"
	objectTypeImage    = "storage_image"

	objectTypeCluster      = "cluster"
	clusterSingletonObject = "cluster_kacho_root"

	relationViewer      = "viewer"
	relationEditor      = "editor"
	relationSystemAdmin = "system_admin"
)

// staticClusterCatalog — extractor, всегда возвращающий (cluster, cluster_kacho_root).
// Используется ТОЛЬКО там, где предмет вопроса действительно кластерный: глобальный
// read-only каталог (DiskType) и admin-CRUD над ним (InternalDiskType). Данные
// конкретных тенантов на этот объект вешать нельзя — `cluster:<root>#viewer@user:*`
// пишет bootstrap, поэтому cluster-`viewer` пропускает любого аутентифицированного
// субъекта (см. запись ListAttachments ниже).
func staticClusterCatalog() authz.ObjectExtractor {
	return func(any) (string, string, error) {
		return objectTypeCluster, clusterSingletonObject, nil
	}
}

// viewerCluster / adminCluster — cluster-singleton tier entries (каталог / admin).
// Object/project-scoped entries строятся через scoped() ниже.
func viewerCluster(perm string) authz.RPCEntry {
	return authz.RPCEntry{Relation: relationViewer, Extract: staticClusterCatalog(), Permission: perm}
}

func adminCluster(perm string) authz.RPCEntry {
	return authz.RPCEntry{Relation: relationSystemAdmin, Extract: staticClusterCatalog(), Permission: perm}
}

// scoped — object/project-scoped entry: relation + (objectType, id-extractor).
func scoped(relation, objectType, perm string, extractID func(req any) (string, error)) authz.RPCEntry {
	return authz.RPCEntry{
		Relation:   relation,
		Extract:    authz.StaticExtractor(objectType, extractID),
		Permission: perm,
	}
}

// PermissionMap сопоставляет каждый storage-RPC → требуемое relation + extractor.
func PermissionMap() authz.RPCMap {
	return authz.RPCMap{
		// ---- VolumeService (public :9090) ----
		"/kacho.cloud.storage.v1.VolumeService/List": scoped(relationViewer, objectTypeProject, "storage.volumes.list",
			func(req any) (string, error) { return req.(*storagev1.ListVolumesRequest).GetProjectId(), nil }),
		"/kacho.cloud.storage.v1.VolumeService/Create": scoped(relationEditor, objectTypeProject, "storage.volumes.create",
			func(req any) (string, error) { return req.(*storagev1.CreateVolumeRequest).GetProjectId(), nil }),
		"/kacho.cloud.storage.v1.VolumeService/Get": scoped(relationViewer, objectTypeVolume, "storage.volumes.get",
			func(req any) (string, error) { return req.(*storagev1.GetVolumeRequest).GetVolumeId(), nil }),
		"/kacho.cloud.storage.v1.VolumeService/ListOperations": scoped(relationViewer, objectTypeVolume, "storage.volumes.listOperations",
			func(req any) (string, error) { return req.(*storagev1.ListVolumeOperationsRequest).GetVolumeId(), nil }),
		"/kacho.cloud.storage.v1.VolumeService/Update": scoped(relationEditor, objectTypeVolume, "storage.volumes.update",
			func(req any) (string, error) { return req.(*storagev1.UpdateVolumeRequest).GetVolumeId(), nil }),
		"/kacho.cloud.storage.v1.VolumeService/Delete": scoped(relationEditor, objectTypeVolume, "storage.volumes.delete",
			func(req any) (string, error) { return req.(*storagev1.DeleteVolumeRequest).GetVolumeId(), nil }),

		// ---- SnapshotService (public :9090) ----
		"/kacho.cloud.storage.v1.SnapshotService/List": scoped(relationViewer, objectTypeProject, "storage.snapshots.list",
			func(req any) (string, error) { return req.(*storagev1.ListSnapshotsRequest).GetProjectId(), nil }),
		"/kacho.cloud.storage.v1.SnapshotService/Create": scoped(relationEditor, objectTypeProject, "storage.snapshots.create",
			func(req any) (string, error) { return req.(*storagev1.CreateSnapshotRequest).GetProjectId(), nil }),
		"/kacho.cloud.storage.v1.SnapshotService/Get": scoped(relationViewer, objectTypeSnapshot, "storage.snapshots.get",
			func(req any) (string, error) { return req.(*storagev1.GetSnapshotRequest).GetSnapshotId(), nil }),
		"/kacho.cloud.storage.v1.SnapshotService/Update": scoped(relationEditor, objectTypeSnapshot, "storage.snapshots.update",
			func(req any) (string, error) { return req.(*storagev1.UpdateSnapshotRequest).GetSnapshotId(), nil }),
		"/kacho.cloud.storage.v1.SnapshotService/Delete": scoped(relationEditor, objectTypeSnapshot, "storage.snapshots.delete",
			func(req any) (string, error) { return req.(*storagev1.DeleteSnapshotRequest).GetSnapshotId(), nil }),

		// ---- DiskTypeService (public :9090, read-only global catalog) ----
		"/kacho.cloud.storage.v1.DiskTypeService/Get":  viewerCluster("storage.diskTypes.get"),
		"/kacho.cloud.storage.v1.DiskTypeService/List": viewerCluster("storage.diskTypes.list"),

		// ---- ImageService (public :9090) ----
		"/kacho.cloud.storage.v1.ImageService/List": scoped(relationViewer, objectTypeProject, "storage.images.list",
			func(req any) (string, error) { return req.(*storagev1.ListImagesRequest).GetProjectId(), nil }),
		"/kacho.cloud.storage.v1.ImageService/Create": scoped(relationEditor, objectTypeProject, "storage.images.create",
			func(req any) (string, error) { return req.(*storagev1.CreateImageRequest).GetProjectId(), nil }),
		"/kacho.cloud.storage.v1.ImageService/Get": scoped(relationViewer, objectTypeImage, "storage.images.get",
			func(req any) (string, error) { return req.(*storagev1.GetImageRequest).GetImageId(), nil }),
		"/kacho.cloud.storage.v1.ImageService/ListOperations": scoped(relationViewer, objectTypeImage, "storage.images.listOperations",
			func(req any) (string, error) { return req.(*storagev1.ListImageOperationsRequest).GetImageId(), nil }),
		"/kacho.cloud.storage.v1.ImageService/Update": scoped(relationEditor, objectTypeImage, "storage.images.update",
			func(req any) (string, error) { return req.(*storagev1.UpdateImageRequest).GetImageId(), nil }),
		"/kacho.cloud.storage.v1.ImageService/Delete": scoped(relationEditor, objectTypeImage, "storage.images.delete",
			func(req any) (string, error) { return req.(*storagev1.DeleteImageRequest).GetImageId(), nil }),

		// ---- InternalVolumeService (:9091 attach-координация) ----
		//
		// Attach/Detach — ДВУХОБЪЕКТНЫЕ: запрос называет том И машину, у которых
		// РАЗНЫЕ владельцы. Запись ниже покрывает ТОЛЬКО том (`editor` на
		// `storage_volume`) — второго слота у authz.RPCEntry нет и быть не должно:
		// это ответ на один вопрос, а вопросов два.
		//
		// Про машину спрашивает use-case (volume.requireInstanceControl) — тем же
		// отношением, которым compute гейтит свои AttachDisk/DetachDisk (`v_update`
		// на `compute_instance`; отвязка принимает ещё `v_delete`, потому что снос
		// машины освобождает её тома под личностью инициатора). Пока этого вопроса
		// не было, держатель `editor` на СВОЁМ томе вписывал строку привязки в набор
		// ЧУЖОЙ машины и занимал её загрузочный слот. Замок класса —
		// volume.TestEveryStorageRPCNamingAnInstanceAsksTheModelAboutIt (обход
		// дескрипторов: RPC, чей запрос адресует машину, обязан иметь исполняемую
		// пробу отказа).
		"/kacho.cloud.storage.v1.InternalVolumeService/Attach": scoped(relationEditor, objectTypeVolume, "storage.volumes.attach",
			func(req any) (string, error) { return req.(*storagev1.AttachVolumeRequest).GetVolumeId(), nil }),
		"/kacho.cloud.storage.v1.InternalVolumeService/Detach": scoped(relationEditor, objectTypeVolume, "storage.volumes.detach",
			func(req any) (string, error) { return req.(*storagev1.DetachVolumeRequest).GetVolumeId(), nil }),
		"/kacho.cloud.storage.v1.InternalVolumeService/GetInternal": scoped(relationViewer, objectTypeVolume, "storage.volumes.getInternal",
			func(req any) (string, error) { return req.(*storagev1.GetInternalVolumeRequest).GetVolumeId(), nil }),
		// ListAttachments авторизуется НА УРОВНЕ ДАННЫХ, а не единичным per-RPC
		// Check'ом. Инстансы называет ВЫЗЫВАЮЩИЙ, а ответ касается томов, у каждого
		// из которых свой владелец — одного объекта, про который можно спросить
		// заранее, здесь нет.
		//
		// Раньше спрашивали `viewer` на синглтоне `cluster:cluster_kacho_root`. Это
		// relation ГЛОБАЛЬНОГО СПРАВОЧНИКА (регионы, зоны, типы дисков), и bootstrap
		// кластера намеренно пишет `cluster:<root>#viewer@user:*`, чтобы справочник
		// читал любой аутентифицированный субъект. То есть проверка пропускала всех и
		// отдавала привязки любых названных инстансов — из чужих проектов и чужих
		// аккаунтов. Привязки томов справочными данными не являются.
		//
		// ScopeFiltered=true: интерсептор пропускает Check (DecisionInternal), а
		// volume.UseCase сужает прочитанную страницу per-object (`viewer` на
		// `storage_volume:<id>`, батчами ≤100) — тем же предикатом, которым энфорсится
		// Volume.Get. Фильтр обязателен на развёрнутом стенде: production boot-guard
		// (config.Validate) отказывается стартовать при ListFilterEnabled=false.
		// Вызывающий без извлечённой identity отсекается в use-case БЕЗУСЛОВНО — в
		// отличие от публичных List, за которыми остаётся per-RPC Check, за этим RPC
		// его нет вовсе.
		"/kacho.cloud.storage.v1.InternalVolumeService/ListAttachments": {
			ScopeFiltered: true,
			Permission:    "storage.volumes.listAttachments",
		},

		// ---- InternalImageService (:9091 infra-проекция Image) ----
		"/kacho.cloud.storage.v1.InternalImageService/GetInternal": scoped(relationViewer, objectTypeImage, "storage.images.getInternal",
			func(req any) (string, error) { return req.(*storagev1.GetInternalImageRequest).GetImageId(), nil }),

		// ---- InternalDiskTypeService (:9091 admin CRUD, system_admin) ----
		"/kacho.cloud.storage.v1.InternalDiskTypeService/Create": adminCluster("storage.diskTypes.create"),
		"/kacho.cloud.storage.v1.InternalDiskTypeService/Update": adminCluster("storage.diskTypes.update"),
		"/kacho.cloud.storage.v1.InternalDiskTypeService/Delete": adminCluster("storage.diskTypes.delete"),

		// ---- LRO Operation-envelope (ReBAC-exempt, owner-scoped в handler) ----
		// Async-мутации возвращают Operation, клиент поллит OperationService.Get.
		// Зеркалит geo/vpc.
		//
		// Что Public:true РЕАЛЬНО делает: интерсептор (pkg/authz) на этой записи
		// возвращает allow СРАЗУ, ДО извлечения субъекта — то есть per-RPC
		// ReBAC-Check снят (object type storage_operation не существует), и
		// anti-anon-ветка субъект-экстрактора для этих двух RPC НЕ выполняется.
		// Не описывать это как «anti-anon сохраняется»: такой цепочки здесь нет,
		// и правка «под комментарий» вернула бы дыру.
		//
		// Что защищает на самом деле:
		//   1. ownership-gate в handler — owner-ключ выводится через
		//      operations.OwnerFromContext, поэтому запрос БЕЗ принципала ключа не
		//      получает вовсе и отваливается no-leak NotFound; предикат владельца
		//      живёт в SQL WHERE (GetOwned/CancelOwned) и дополнительно отвергает
		//      нулевой ключ;
		//   2. обязательный mTLS на обоих листенерах — на этот путь не попасть,
		//      не предъявив клиентский сертификат;
		//   3. список доверенных отправителей (cmd/storage/serve.go ←
		//      config.TrustedForwarders): передать личность конечного пользователя
		//      вправе только пир, чья личность сертификата в списке — api-gateway
		//      на публичном листенере и compute на внутреннем. Прочие соседи со
		//      своими законными сертификатами до периметра доходят, но их заголовки
		//      личности снимаются (withTrustedPrincipal вычищает носителя), поэтому
		//      владельцем операции они стать не могут и упираются в п.1.
		//
		// Как читать этот пункт, чтобы он снова не разъехался с кодом:
		// grpcsrv.principalIsTrusted сверяет сертификат со списком ТОЛЬКО когда
		// список непуст; на ПУСТОМ она возвращает true, то есть переданную личность
		// принял бы ЛЮБОЙ проверенный пир. Именно так здесь и было — список
		// задавался литералом `[]string{}`, ручки в конфиге не существовало, и
		// сужал круг один лишь обязательный mTLS. Теперь список приходит из
		// конфигурации, а боевой режим на пустом НЕ СТАРТУЕТ (config.Validate) —
		// поэтому «список сужает» стало описанием состояния, а не пожеланием.
		// Ослабить это обратно нельзя молча: страж старта и замки в
		// cmd/storage/trusted_forwarders*_test.go покраснеют.
		"/kacho.cloud.operation.OperationService/Get":    {Public: true},
		"/kacho.cloud.operation.OperationService/Cancel": {Public: true},
	}
}
