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

// storageFields — поля ресурса storage: общие для платформы, свои и отчётные.
//
// Отчётные (состояние, его причина, отметка правки) собраны функцией, а не переписаны
// трижды: у тома, снимка и образа это ОДИН предмет, и разъехаться они могут только по
// недосмотру. Причина состояния — закрытый словарь края; пусто означает, что причины нет,
// то есть штатное состояние.
//
// Чего в наборе НЕТ и почему это названо здесь, а не умолчано: обобщённой проекции
// «кто мной пользуется» (`usedBy` у всех трёх, `attachments` у тома). Это повторяющиеся
// ВЛОЖЕННЫЕ объекты, а каркас плоских ресурсов их не выражает — граница объявлена им
// самим. Источник истины у этой проекции всё равно чужой (тома, машины, образы), поэтому
// её отсутствие ничего не отнимает у декларации: подключение тома описывается машиной, а
// не томом.
func storageFields(extra ...fieldSpec) []fieldSpec {
	out := commonFields("project_id", "Проект-владелец.")
	out = append(out, extra...)
	return append(out,
		fieldSpec{name: "status", kind: fString, computed: true,
			doc: "Состояние ресурса у края."},
		fieldSpec{name: "status_reason", kind: fString, computed: true,
			doc: "Почему ресурс в этом состоянии. Закрытый словарь края; пусто — причины " +
				"нет. Отличает «бэкенд не ответил, сверщик вернётся» от «отказано по " +
				"существу»: по этой разнице решают, ждать или звать оператора."},
		fieldSpec{name: "updated_at", kind: fString, computed: true,
			doc: "Отметка последнего изменения."},
	)
}

var storageVolumeSpec = flatSpec{
	tfName: "kacho_storage_volume", human: "Том", pathCol: "/storage/v1/volumes",
	idField: "volumeId", prefix: ids.PrefixVolume, scopeParam: "projectId", scopeAttr: "project_id",
	descr: "Блочный том. Заводится либо пустым по размеру, либо из снимка или образа.\n\n" +
		"Том РОЖДАЕТСЯ в намерении: завершённая операция создания означает, что запись " +
		"закоммичена, а объект у плоскости данных материализует сверщик. Поэтому сразу " +
		"после apply `status` законно читается как `CREATING`.",
	deleteHint: "Удалению мешает подключение к машине: сначала отсоедините том. " +
		"Снимки, снятые с тома, удалению не мешают.",
	newCreate:     func() proto.Message { return &storagev1.CreateVolumeRequest{} },
	newUpdate:     func() proto.Message { return &storagev1.UpdateVolumeRequest{} },
	updateIDField: "volume_id",
	// Класс диска меняется ПЕРЕНОСОМ ДАННЫХ, а не правкой поля: у края для этого свой
	// глагол, том всё это время наблюдаем в MIGRATING, а при отказе данные остаются на
	// исходном классе. Без этой декларации у провайдера остался бы единственный способ
	// выразить смену класса — пересоздать том, то есть потерять данные на операции,
	// которую край делает без потерь.
	verbFields: []verbField{{
		attr: "disk_type_id", verb: "changeDiskType",
		newRequest: func() proto.Message { return &storagev1.ChangeDiskTypeRequest{} },
		idField:    "volume_id", valueField: "disk_type_id",
		hint: "Целевой класс обязан принимать новые тома и предлагаться В ТОЙ ЖЕ зоне, где " +
			"стоит том: зона этим глаголом не меняется. Размер тома обязан укладываться в " +
			"границы целевого класса, а сам том — быть свободен либо подключён, но не занят " +
			"другой операцией.\n\n" +
			"Если целевой класс требует БОЛЬШЕГО минимального размера, увеличьте том " +
			"отдельным применением и смените класс следующим: рост тома необратим, поэтому " +
			"провайдер не применяет его раньше смены класса — иначе отказ смены оставил бы " +
			"вас с выросшим томом, которого вы отдельно не просили.",
	}},
	fields: storageFields(
		fieldSpec{name: "zone_id", kind: fString, required: true, immutable: true,
			doc: "Зона тома. Машина, к которой он подключается, обязана быть в той же зоне."},
		fieldSpec{name: "disk_type_id", kind: fString, required: true,
			doc: "Класс диска из справочника storage. Обязателен: умолчания край не " +
				"подставляет.\n\nИзменение НЕ пересоздаёт том — провайдер зовёт операцию " +
				"края `changeDiskType`, и данные переезжают на новый класс. Зону она не " +
				"меняет: перенос между зонами идёт копией снимка."},
		fieldSpec{name: "size_bytes", kind: fInt64,
			doc: "Размер в байтах. Увеличивается изменением; уменьшить нельзя."},
		fieldSpec{name: "source_snapshot_id", kind: fString, immutable: true,
			doc: "Снимок-источник. Взаимоисключающ с образом-источником."},
		fieldSpec{name: "source_image_id", kind: fString, immutable: true,
			doc: "Образ-источник. Его регион обязан совпадать с регионом зоны тома."},
		fieldSpec{name: "used_bytes", kind: fInt64, computed: true, absentIsNull: true,
			doc: "Фактически занятые байты, наблюдённые у плоскости данных.\n\n" +
				"Значения НЕТ, пока бэкенд потребление не сообщил, — и это не ноль: ноль " +
				"означал бы «том пуст», а по такому утверждению строят счёт и решают о " +
				"расширении."},
	),
}

var storageSnapshotSpec = flatSpec{
	tfName: "kacho_storage_snapshot", human: "Снимок", pathCol: "/storage/v1/snapshots",
	idField: "snapshotId", prefix: ids.PrefixStorageSnapshot, scopeParam: "projectId", scopeAttr: "project_id",
	descr: "Снимок тома — точка, из которой создаются новые тома и образы.\n\n" +
		"Источников у снимка ДВА, и ровно один задаётся: `source_volume_id` — снять с " +
		"тома, `source_snapshot_id` вместе с `zone_id` — скопировать существующий снимок " +
		"в другую зону. Копия — самостоятельный снимок со своим идентификатором, именем " +
		"и жизненным циклом; исходный она не меняет.\n\n" +
		"Отдельного типа ресурса у копии нет намеренно: у владельца она лежит в той же " +
		"таблице, и его собственная проверка объявляет источники взаимоисключающими. " +
		"Второй тип означал бы две личности одной сущности края в состоянии Terraform.",
	deleteHint:    "Удалению мешают тома и образы, созданные из этого снимка.",
	newCreate:     func() proto.Message { return &storagev1.CreateSnapshotRequest{} },
	newUpdate:     func() proto.Message { return &storagev1.UpdateSnapshotRequest{} },
	updateIDField: "snapshot_id",
	copyFrom: &copyRoute{
		verb: "copy", sourceAttr: "source_snapshot_id", sourceField: "snapshot_id",
		newRequest: func() proto.Message { return &storagev1.CopySnapshotRequest{} },
		idField:    "snapshotId",
		carry: []carriedField{
			{attr: "project_id", field: "project_id"},
			{attr: "zone_id", field: "target_zone_id"},
			{attr: "name", field: "name"},
			{attr: "description", field: "description"},
			{attr: "labels", field: "labels"},
		},
		requiredWithCopy:     []string{"zone_id"},
		forbiddenWithCopy:    []string{"source_volume_id"},
		forbiddenWithoutCopy: []string{"zone_id"},
	},
	fields: storageFields(
		fieldSpec{name: "source_volume_id", kind: fString, immutable: true,
			doc: "Том, с которого снят снимок. Взаимоисключающ с `source_snapshot_id`."},
		fieldSpec{name: "source_snapshot_id", kind: fString, immutable: true,
			notInCreate: true, notInResponse: true,
			doc: "Снимок, копией которого создаётся этот. Задаётся вместе с `zone_id` — " +
				"копия делается В ДРУГУЮ зону, и это единственный законный путь провести " +
				"данные через границу размещения: зона тома неизменяема.\n\n" +
				"Значение хранится состоянием Terraform: контракт ресурса происхождение " +
				"копии не выводит, поэтому обратное чтение его не приносит."},
		fieldSpec{name: "zone_id", kind: fString, immutable: true, notInCreate: true,
			doc: "Зона снимка.\n\nУ снимка, снятого с тома, задавать её нельзя — она " +
				"наследуется от тома-источника. У КОПИИ она обязательна и называет " +
				"целевую зону.\n\nСобственный якорь у снимка есть потому, что ссылка на " +
				"источник обнуляется при удалении тома: зона, добираемая через источник, " +
				"однажды стала бы пустой, и проверка когерентности проходила бы всегда."},
		fieldSpec{name: "size_bytes", kind: fInt64, computed: true,
			doc: "Размер снимка в байтах. Выводится краем из источника."},
	),
}

var storageImageSpec = flatSpec{
	tfName: "kacho_storage_image", human: "Образ", pathCol: "/storage/v1/images",
	idField: "imageId", prefix: ids.PrefixStorageImage, scopeParam: "projectId", scopeAttr: "project_id",
	descr: "Образ — региональный источник для создания томов.\n\n" +
		"Источников три, и ровно один задаётся: `source_snapshot_id`, `source_volume_id` " +
		"либо `source_image_id` — копия существующего образа в регион `region_id`. Копия — " +
		"единственный путь переноса образа между регионами: `region_id` неизменяем, " +
		"«переехать» существующей строкой нечем.",
	deleteHint: "Удалению мешают тома, созданные из этого образа, и машины, ссылающиеся " +
		"на него как на источник загрузки.",
	newCreate:     func() proto.Message { return &storagev1.CreateImageRequest{} },
	newUpdate:     func() proto.Message { return &storagev1.UpdateImageRequest{} },
	updateIDField: "image_id",
	copyFrom: &copyRoute{
		verb: "copy", sourceAttr: "source_image_id", sourceField: "image_id",
		newRequest: func() proto.Message { return &storagev1.CopyImageRequest{} },
		idField:    "imageId",
		carry: []carriedField{
			{attr: "project_id", field: "project_id"},
			{attr: "region_id", field: "target_region_id"},
			{attr: "name", field: "name"},
			{attr: "description", field: "description"},
			{attr: "labels", field: "labels"},
		},
		forbiddenWithCopy: []string{"source_snapshot_id", "source_volume_id"},
	},
	fields: storageFields(
		fieldSpec{name: "region_id", kind: fString, required: true, immutable: true,
			doc: "Регион образа. Том создаётся из него только в зоне ЭТОГО региона. " +
				"У копии это ЦЕЛЕВОЙ регион."},
		fieldSpec{name: "source_snapshot_id", kind: fString, immutable: true,
			doc: "Снимок-источник. Взаимоисключающ с томом-источником и с образом-источником."},
		fieldSpec{name: "source_volume_id", kind: fString, immutable: true,
			doc: "Том-источник."},
		fieldSpec{name: "source_image_id", kind: fString, immutable: true,
			notInCreate: true, notInResponse: true,
			doc: "Образ, копией которого создаётся этот. Копия ложится в `region_id` и " +
				"наследует формат источника; метки и имя не наследуются.\n\n" +
				"Значение хранится состоянием Terraform: контракт ресурса происхождение " +
				"копии не выводит, поэтому обратное чтение его не приносит."},
		fieldSpec{name: "placement_type", kind: fString, computed: true,
			doc: "Семейство размещения. У образа всегда `REGIONAL` — он зоне-независим."},
		fieldSpec{name: "size_bytes", kind: fInt64, computed: true,
			doc: "Виртуальный размер образа в байтах."},
		fieldSpec{name: "min_disk_bytes", kind: fInt64, computed: true,
			doc: "Наименьший размер тома, который можно засеять этим образом. Граница " +
				"включающая: том меньшего размера край отвергает."},
		fieldSpec{name: "format", kind: fString, computed: true,
			doc: "Формат образа."},
	),
}

// ---- iam ------------------------------------------------------------------------------

var iamAccountSpec = flatSpec{
	tfName: typeNameIAMAccount, human: "Аккаунт", pathCol: "/iam/v1/accounts",
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
		// ВЫХОДНОЕ поле, а не вход: владельцем становится вызывающий, и край отвергает
		// присланное значение синхронно — даже если это собственный идентификатор
		// вызывающего. Требование его в описании делало ресурс неисполнимым ПРИ ЛЮБОМ
		// входе: непустое отвергал край, а пустого требование не допускало.
		{name: "owner_user_id", kind: fString, computed: true,
			doc: "Владелец аккаунта — вызывающий, применивший конфигурацию. Он СТРУКТУРНЫЙ " +
				"источник прав на свой аккаунт: создаёт и сносит его сам, не дожидаясь " +
				"выдачи. Поле не задаётся: край выводит владельца из вызывающего и " +
				"присланное значение отвергает."},
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
