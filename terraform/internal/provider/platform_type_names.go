// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package provider

// Имена типов ресурсов платформы — по одному объявлению на тип.
//
// # Зачем словарь, если сверка и так стерегла совпадение
//
// Имя типа записывалось ДВАЖДЫ независимыми написаниями: в описании типа (`Metadata`) и в
// пространстве ключа повторной подачи, уезжающем на край. Оба написания давали одно
// значение, и совпадение стерегла сверка — то есть живого расхождения не было.
//
// Предмет был не в расхождении, а в том, ЧЕМ держалось его отсутствие. Свойство держалось
// ПРОВЕРКОЙ: проверку можно снять, обойти или не позвать, и до тех пор, пока в дереве стоит
// двойная форма, всякий следующий ресурс пишется по образцу соседа — число двойных мест
// растёт само. Здесь имя объявлено ОДИН раз и читается обоими написаниями, поэтому
// «переехало одно, не переехало второе» перестало быть представимым: второго места, которое
// могло бы отстать, попросту нет.
//
// Сверка при этом ОСТАЛАСЬ (`idempotency_namespace_test.go`) и страхует форму, которую
// построение сделало невыразимой: литерал, вписанный на месте, остаётся законной формой
// языка. Её молчание перестало быть единственным основанием доверять — но основанием быть
// не перестало.
//
// # Почему приставка выводится, а не вписывается
//
// Имя провайдера объявлено один раз — `providerTypeName` (iam_type_names.go). Имя типа в
// словаре ниже получается из него склейкой, поэтому смена имени провайдера доезжает до этих
// имён by construction: их не два списка, а один и производные от него.
//
// Словарь охватывает ресурсы, чьё описание живёт в СВОЁМ файле. Ресурс, описанный таблицей
// (`flat_specs.go`, `iam_resources.go`), объявляет своё имя ОДИН раз — полем `tfName` своего
// описания, — и оба написания читают это поле; сводить такое имя сюда было бы вторым местом
// об одном предмете. Приставку часть таких описаний при этом несёт литералом; предмет
// соседний и здесь не решается.
//
// # Почему имена типов доступа живут отдельным словарём
//
// Они не выводятся из имени провайдера вовсе — служба доступа поставляется своим продуктом
// и несёт своё имя (`kaname_*`), а вместе с именами там объявлен и переезд состояния с
// прежних имён. Здесь переезжать нечему: имена типов платформы не менялись, и объявлять
// переезд «на всякий случай» значило бы завести обещание без предмета.
const (
	typeNameComputeInstance       = providerTypeName + "_compute_instance"
	typeNameComputePlacementGroup = providerTypeName + "_compute_placement_group"
	typeNameComputeGuestAccessKey = providerTypeName + "_compute_guest_access_key"

	typeNameVPCNetwork       = providerTypeName + "_vpc_network"
	typeNameVPCSubnet        = providerTypeName + "_vpc_subnet"
	typeNameVPCAddress       = providerTypeName + "_vpc_address"
	typeNameVPCSecurityGroup = providerTypeName + "_vpc_security_group"
	typeNameVPCRouteTable    = providerTypeName + "_vpc_route_table"
	typeNameVPCCIDRGroup     = providerTypeName + "_vpc_cidr_group"

	typeNameNLBLoadBalancer = providerTypeName + "_nlb_load_balancer"
	typeNameNLBListener     = providerTypeName + "_nlb_listener"
	typeNameNLBTargetGroup  = providerTypeName + "_nlb_target_group"

	typeNameRegistryRepository = providerTypeName + "_registry_repository"
)
