// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package provider

import (
	"google.golang.org/protobuf/proto"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"
	registryv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/registry/v1"
	storagev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/storage/v1"
	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"
)

// Описания ресурсов, чья механика полностью совпадает с общим каркасом.
//
// Здесь НЕТ ресурсов с вложенными объектами (адрес, шлюз, таблица маршрутизации, группа
// безопасности, роль, привязка прав, машина) — у них своя форма, и втискивать её в каркас
// значило бы сделать его условным. Их отсутствие названо предметом в гейте паритета, а не
// умолчанием.

// Общие поля, повторяющиеся у каждого ресурса платформы. Собраны функцией, а не
// скопированы: скопированный набор разъезжается по одному полю за раз.
func commonFields(scopeAttr, scopeDoc string) []fieldSpec {
	return []fieldSpec{
		{name: scopeAttr, kind: fString, required: true, immutable: true, doc: scopeDoc},
		{name: "name", kind: fString, required: true,
			doc: "Имя в пределах области. Обязательно: по нему провайдер находит уже " +
				"созданный ресурс, если ответ на создание потерялся."},
		{name: "description", kind: fString, doc: "Произвольное описание."},
		{name: "labels", kind: fStringMap, doc: "Метки."},
		{name: "created_at", kind: fString, computed: true},
	}
}

func withFields(base []fieldSpec, extra ...fieldSpec) []fieldSpec {
	return append(append([]fieldSpec{}, base...), extra...)
}

// ---- storage --------------------------------------------------------------------------

var storageVolumeSpec = flatSpec{
	tfName: "kacho_storage_volume", human: "Том", pathCol: "/storage/v1/volumes",
	idField: "volumeId", prefix: ids.PrefixVolume, scopeParam: "projectId", scopeAttr: "project_id",
	descr: "Блочный том. Заводится либо пустым по размеру, либо из снимка или образа.",
	deleteHint: "Удалению мешает подключение к машине: сначала отсоедините том. " +
		"Снимки, снятые с тома, удалению не мешают.",
	newCreate:     func() proto.Message { return &storagev1.CreateVolumeRequest{} },
	newUpdate:     func() proto.Message { return &storagev1.UpdateVolumeRequest{} },
	updateIDField: "volume_id",
	fields: withFields(commonFields("project_id", "Проект-владелец."),
		fieldSpec{name: "zone_id", kind: fString, required: true, immutable: true,
			doc: "Зона тома. Машина, к которой он подключается, обязана быть в той же зоне."},
		fieldSpec{name: "disk_type_id", kind: fString, immutable: true,
			doc: "Тип диска из справочника storage."},
		fieldSpec{name: "size_bytes", kind: fInt64,
			doc: "Размер в байтах. Увеличивается изменением; уменьшить нельзя."},
		fieldSpec{name: "block_size", kind: fInt64, immutable: true, doc: "Размер блока."},
		fieldSpec{name: "source_snapshot_id", kind: fString, immutable: true,
			doc: "Снимок-источник. Взаимоисключающ с образом-источником."},
		fieldSpec{name: "source_image_id", kind: fString, immutable: true,
			doc: "Образ-источник. Его регион обязан совпадать с регионом зоны тома."},
		fieldSpec{name: "status", kind: fString, computed: true},
	),
}

var storageSnapshotSpec = flatSpec{
	tfName: "kacho_storage_snapshot", human: "Снимок", pathCol: "/storage/v1/snapshots",
	idField: "snapshotId", prefix: ids.PrefixStorageSnapshot, scopeParam: "projectId", scopeAttr: "project_id",
	descr:         "Снимок тома — точка, из которой создаются новые тома и образы.",
	deleteHint:    "Удалению мешают тома и образы, созданные из этого снимка.",
	newCreate:     func() proto.Message { return &storagev1.CreateSnapshotRequest{} },
	newUpdate:     func() proto.Message { return &storagev1.UpdateSnapshotRequest{} },
	updateIDField: "snapshot_id",
	fields: withFields(commonFields("project_id", "Проект-владелец."),
		fieldSpec{name: "source_volume_id", kind: fString, required: true, immutable: true,
			doc: "Том, с которого снят снимок."},
		fieldSpec{name: "status", kind: fString, computed: true},
	),
}

var storageImageSpec = flatSpec{
	tfName: "kacho_storage_image", human: "Образ", pathCol: "/storage/v1/images",
	idField: "imageId", prefix: ids.PrefixStorageImage, scopeParam: "projectId", scopeAttr: "project_id",
	descr: "Образ — региональный источник для создания томов.",
	deleteHint: "Удалению мешают тома, созданные из этого образа, и машины, ссылающиеся " +
		"на него как на источник загрузки.",
	newCreate:     func() proto.Message { return &storagev1.CreateImageRequest{} },
	newUpdate:     func() proto.Message { return &storagev1.UpdateImageRequest{} },
	updateIDField: "image_id",
	fields: withFields(commonFields("project_id", "Проект-владелец."),
		fieldSpec{name: "region_id", kind: fString, required: true, immutable: true,
			doc: "Регион образа. Том создаётся из него только в зоне ЭТОГО региона."},
		fieldSpec{name: "source_snapshot_id", kind: fString, immutable: true,
			doc: "Снимок-источник. Взаимоисключающ с томом-источником."},
		fieldSpec{name: "source_volume_id", kind: fString, immutable: true,
			doc: "Том-источник."},
		fieldSpec{name: "status", kind: fString, computed: true},
	),
}

// ---- iam ------------------------------------------------------------------------------

var iamAccountSpec = flatSpec{
	tfName: "kacho_iam_account", human: "Аккаунт", pathCol: "/iam/v1/accounts",
	// Префикс литералом, а не константой: экспортированных констант для префиксов iam в
	// каталоге платформы НЕТ — там они лежат внутри списка членства. Опечатка тем не менее
	// не проходит молча: импорт сверяет строку и с этим префиксом, и с членством в
	// каталоге, поэтому чужой или несуществующий префикс отвергается.
	idField: "accountId", prefix: "acc", scopeParam: "", scopeAttr: "owner_user_id",
	descr: "Аккаунт — собственная область пользователя, внутри которой живут проекты.\n\n" +
		":::warning Аккаунт заводят редко и осознанно\n" +
		"Это корень тенантности: под ним лежат все проекты и вся выданная в них поверхность. " +
		"Обычная работа идёт в существующем аккаунте, а не заводит новый на каждую задачу.\n:::",
	deleteHint:    "Удалению мешают проекты аккаунта: сначала снимите их.",
	newCreate:     func() proto.Message { return &iamv1.CreateAccountRequest{} },
	newUpdate:     func() proto.Message { return &iamv1.UpdateAccountRequest{} },
	updateIDField: "account_id",
	fields: []fieldSpec{
		{name: "owner_user_id", kind: fString, required: true, immutable: true,
			doc: "Владелец аккаунта. Он — СТРУКТУРНЫЙ источник прав на свой аккаунт: " +
				"создаёт и сносит его сам, не дожидаясь выдачи."},
		{name: "name", kind: fString, required: true, doc: "Имя аккаунта."},
		{name: "description", kind: fString, doc: "Произвольное описание."},
		{name: "labels", kind: fStringMap, doc: "Метки."},
		{name: "created_at", kind: fString, computed: true},
	},
}

// ---- registry ---------------------------------------------------------------------------

var registryRegistrySpec = flatSpec{
	tfName: "kacho_registry_registry", human: "Реестр", pathCol: "/registry/v1/registries",
	idField: "registryId", prefix: ids.PrefixRegistry, scopeParam: "projectId", scopeAttr: "project_id",
	descr: "Реестр образов OCI.\n\n" +
		"Путь загрузки строится по НЕИЗМЕНЯЕМОМУ идентификатору реестра — " +
		"`$домен/$registryId/$репозиторий:$тег`, — а не по имени: имя меняется, и пути " +
		"ломались бы вместе с ним.",
	deleteHint:    "Удалению мешают репозитории реестра: сначала снимите их.",
	newCreate:     func() proto.Message { return &registryv1.CreateRegistryRequest{} },
	newUpdate:     func() proto.Message { return &registryv1.UpdateRegistryRequest{} },
	updateIDField: "registry_id",
	fields: withFields(commonFields("project_id", "Проект-владелец."),
		fieldSpec{name: "region_id", kind: fString, required: true, immutable: true,
			doc: "Регион реестра."},
		fieldSpec{name: "default_repository_visibility", kind: fString, updateOnly: true,
			doc: "Видимость новых репозиториев по умолчанию: `PRIVATE` или `PUBLIC`.\n\n" +
				"Контракт СОЗДАНИЯ этого поля не несёт — оно есть только у изменения. " +
				"Поэтому реестр с заданной видимостью заводится в два шага: создать, потом " +
				"донастроить. Между шагами существует краткое окно с умолчанием края."},
	),
}

// ---- vpc --------------------------------------------------------------------------------

var vpcNetworkInterfaceSpec = flatSpec{
	tfName: "kacho_vpc_network_interface", human: "Сетевой интерфейс",
	pathCol: "/vpc/v1/networkInterfaces", idField: "networkInterfaceId", prefix: ids.PrefixNetworkInterface,
	scopeParam: "projectId", scopeAttr: "project_id",
	descr: "Сетевой интерфейс — самостоятельный ресурс, а не часть машины: он заводится " +
		"отдельно и подключается к машине по ссылке.\n\n" +
		"Подключения здесь нет и быть не может: край отвергает его на создании синхронно и " +
		"с именем поля. Привязку выполняет владелец машины — только он способен проверить " +
		"инварианты подключения (та же зона, принадлежность машины, атомарная смена " +
		"владельца, номер гнезда). Кто держит интерфейс сейчас, видно у самой машины.",
	deleteHint:    "Удалению мешает подключение к машине: сначала отсоедините интерфейс.",
	newCreate:     func() proto.Message { return &vpcv1.CreateNetworkInterfaceRequest{} },
	newUpdate:     func() proto.Message { return &vpcv1.UpdateNetworkInterfaceRequest{} },
	updateIDField: "network_interface_id",
	fields: withFields(commonFields("project_id", "Проект-владелец."),
		fieldSpec{name: "subnet_id", kind: fString, required: true, immutable: true,
			doc: "Подсеть интерфейса. Её зона задаёт зону интерфейса, а значит и машины."},
		fieldSpec{name: "security_group_ids", kind: fStringList,
			doc: "Группы безопасности, применяемые к трафику интерфейса."},
		// Здесь стояли `instance_id` и `index`. Контракт создания их несёт, но край
		// отвергает ОБА безусловно — синхронно, первыми проверками и с именем поля.
		// Предлагать их в конфигурации значило обещать возможность, которой нет:
		// пользователь пишет строку, план её показывает, а отказ приходит применением. И
		// обратного пути у них тоже не было — проекция чтения интерфейса этих полей не
		// несёт вовсе (номера зарезервированы), поэтому даже вычисляемым зеркалом они
		// быть не могли: в состоянии навсегда осталась бы пустая строка.
		fieldSpec{name: "v4_address_ids", kind: fStringList, doc: "Привязанные адреса IPv4."},
		fieldSpec{name: "v6_address_ids", kind: fStringList, doc: "Привязанные адреса IPv6."},
		fieldSpec{name: "bandwidth_limit_mbps", kind: fInt64,
			doc: "Верхняя граница полосы интерфейса, Мбит/с. `0` — ограничения нет.\n\n" +
				"Поле принимается НЕ НА КАЖДОМ СТЕНДЕ: полосу выдерживает тот, кто несёт " +
				"трафик, и умеет он это не везде. Там, где умение не объявлено, непустая " +
				"величина отвергается краем синхронно и с именем поля — то есть отказ " +
				"придёт применением, а не планом. Величина принимается строго выше " +
				"гарантированной полосы интерфейса и не выше того, что гарантирует стенд."},
		fieldSpec{name: "status", kind: fString, computed: true},
	),
}
