// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package allowlist

import "strings"

// AllowedMethods — публичные RPC-пути, маршрутизируемые через api-gateway.
// Методы *InternalService.* НИКОГДА не включаются (запрет #6): их REST-проекция
// доступна только на cluster-internal listener (см. restmux/mux.go).
//
// Состав этой карты — НЕ дело вкуса и не «что вспомнили»: он обязан совпадать с
// публичной поверхностью дескрипторов ровно, и это проверяется вычислением, а не
// перечислением (parity_test.go). Единственный критерий исключения — Internal*;
// «забыли дописать» исключением не является и раньше выглядело снаружи так же,
// как намеренное сокрытие, потому что резолвер отвечает на оба случая одним и тем
// же NotFound.
// Активны: iam, vpc, compute, storage, geo, loadbalancer, registry, operation.
// loadbalancer (kacho-nlb) — NetworkLoadBalancer / Listener / TargetGroup
// публичные методы добавлены ниже. registry (kacho-registry) — RegistryService
// control-plane. InternalResourceLifecycleService (streaming, gRPC-direct only)
// и InternalRegistryService (GC/stats admin, :9091) — НЕ в allowlist;
// блокируются HasInternalSuffix.
var AllowedMethods = map[string]struct{}{
	// vpc.v1 — NetworkService
	"/kacho.cloud.vpc.v1.NetworkService/Get":              {},
	"/kacho.cloud.vpc.v1.NetworkService/List":             {},
	"/kacho.cloud.vpc.v1.NetworkService/Create":           {},
	"/kacho.cloud.vpc.v1.NetworkService/Update":           {},
	"/kacho.cloud.vpc.v1.NetworkService/Delete":           {},
	"/kacho.cloud.vpc.v1.NetworkService/AddCidrBlocks":    {}, // :verb supernet growth (redesign-2026)
	"/kacho.cloud.vpc.v1.NetworkService/RemoveCidrBlocks": {}, // :verb supernet shrink (redesign-2026)
	"/kacho.cloud.vpc.v1.NetworkService/ListOperations":   {},
	// vpc.v1 — CidrGroupService (именованный набор префиксов; цель правила
	// группы безопасности вместо копии перечня в каждом правиле)
	"/kacho.cloud.vpc.v1.CidrGroupService/Get":    {},
	"/kacho.cloud.vpc.v1.CidrGroupService/List":   {},
	"/kacho.cloud.vpc.v1.CidrGroupService/Create": {},
	"/kacho.cloud.vpc.v1.CidrGroupService/Update": {},
	"/kacho.cloud.vpc.v1.CidrGroupService/Delete": {},

	// AddressPool — административная поверхность на публичном слушателе (ADM-1
	// S1). В списке маршрутизируемого стоит ПУБЛИЧНЫЙ сервис; одноимённый
	// `InternalAddressPoolService` сюда не попадает и попасть не может —
	// `HasInternalSuffix` его отсекает, и это по-прежнему так.
	//
	// Присутствие здесь НЕ даёт доступа: каждый из одиннадцати гейтится
	// `system_admin` @ `cluster` записью каталога прав. Список отвечает на другой
	// вопрос — «существует ли такой маршрут», а не «кому он открыт».
	"/kacho.cloud.vpc.v1.AddressPoolService/Get":                  {},
	"/kacho.cloud.vpc.v1.AddressPoolService/List":                 {},
	"/kacho.cloud.vpc.v1.AddressPoolService/ListAddresses":        {},
	"/kacho.cloud.vpc.v1.AddressPoolService/GetUtilization":       {},
	"/kacho.cloud.vpc.v1.AddressPoolService/Create":               {},
	"/kacho.cloud.vpc.v1.AddressPoolService/Update":               {},
	"/kacho.cloud.vpc.v1.AddressPoolService/Delete":               {},
	"/kacho.cloud.vpc.v1.AddressPoolService/AddCidrBlocks":        {},
	"/kacho.cloud.vpc.v1.AddressPoolService/RemoveCidrBlocks":     {},
	"/kacho.cloud.vpc.v1.AddressPoolService/BindAsNetworkDefault": {},
	"/kacho.cloud.vpc.v1.AddressPoolService/UnbindNetworkDefault": {},
	"/kacho.cloud.vpc.v1.CidrGroupService/AddCidrBlocks":          {},
	"/kacho.cloud.vpc.v1.CidrGroupService/RemoveCidrBlocks":       {},
	"/kacho.cloud.vpc.v1.CidrGroupService/ListOperations":         {},
	// vpc.v1 — QuotaService (только чтение: величины администрируются на
	// внутреннем слушателе через iam.v1.InternalLimitService)
	"/kacho.cloud.vpc.v1.QuotaService/List": {},
	// vpc.v1 — SubnetService
	"/kacho.cloud.vpc.v1.SubnetService/Get":               {},
	"/kacho.cloud.vpc.v1.SubnetService/List":              {},
	"/kacho.cloud.vpc.v1.SubnetService/Create":            {},
	"/kacho.cloud.vpc.v1.SubnetService/Update":            {},
	"/kacho.cloud.vpc.v1.SubnetService/Delete":            {},
	"/kacho.cloud.vpc.v1.SubnetService/AddCidrBlocks":     {},
	"/kacho.cloud.vpc.v1.SubnetService/RemoveCidrBlocks":  {},
	"/kacho.cloud.vpc.v1.SubnetService/ListOperations":    {},
	"/kacho.cloud.vpc.v1.SubnetService/ListUsedAddresses": {},
	// vpc.v1 — AddressService
	"/kacho.cloud.vpc.v1.AddressService/Get":            {},
	"/kacho.cloud.vpc.v1.AddressService/List":           {},
	"/kacho.cloud.vpc.v1.AddressService/Create":         {},
	"/kacho.cloud.vpc.v1.AddressService/Update":         {},
	"/kacho.cloud.vpc.v1.AddressService/Delete":         {},
	"/kacho.cloud.vpc.v1.AddressService/ListOperations": {},
	// vpc.v1 — RouteTableService
	"/kacho.cloud.vpc.v1.RouteTableService/Get":            {},
	"/kacho.cloud.vpc.v1.RouteTableService/List":           {},
	"/kacho.cloud.vpc.v1.RouteTableService/Create":         {},
	"/kacho.cloud.vpc.v1.RouteTableService/Update":         {},
	"/kacho.cloud.vpc.v1.RouteTableService/Delete":         {},
	"/kacho.cloud.vpc.v1.RouteTableService/ListOperations": {},
	// :verb-мутации маршрутов (REST POST /vpc/v1/routeTables/{id}:add-routes и т.д.).
	// Все три отвечают UNIMPLEMENTED с названной причиной: у маршрута нет
	// идентификатора ни в контракте, ни в хранилище, а два из трёх адресуют его
	// именно по нему (vpc, 07-known-divergences.md, запись 26). В списке они
	// остаются НАМЕРЕННО: снятие отсюда дало бы вызывающему отказ края вместо
	// отказа владельца, то есть скрыло бы причину за общим «метод не разрешён».
	// vpc.v1 — NetworkInterfaceService (first-class ресурс домена, REST /vpc/v1/networkInterfaces).
	// Проекция `/vpc/v1/networkInterfaces/{id}/internal` принадлежит
	// InternalNetworkInterfaceService и здесь отсутствует — её несёт internal-листенер.
	"/kacho.cloud.vpc.v1.NetworkInterfaceService/Get":            {},
	"/kacho.cloud.vpc.v1.NetworkInterfaceService/List":           {},
	"/kacho.cloud.vpc.v1.NetworkInterfaceService/Create":         {},
	"/kacho.cloud.vpc.v1.NetworkInterfaceService/Update":         {},
	"/kacho.cloud.vpc.v1.NetworkInterfaceService/Delete":         {},
	"/kacho.cloud.vpc.v1.NetworkInterfaceService/ListOperations": {},
	// vpc.v1 — SecurityGroupService
	"/kacho.cloud.vpc.v1.SecurityGroupService/Get":            {},
	"/kacho.cloud.vpc.v1.SecurityGroupService/List":           {},
	"/kacho.cloud.vpc.v1.SecurityGroupService/Create":         {},
	"/kacho.cloud.vpc.v1.SecurityGroupService/Update":         {},
	"/kacho.cloud.vpc.v1.SecurityGroupService/UpdateRules":    {},
	"/kacho.cloud.vpc.v1.SecurityGroupService/UpdateRule":     {},
	"/kacho.cloud.vpc.v1.SecurityGroupService/Delete":         {},
	"/kacho.cloud.vpc.v1.SecurityGroupService/ListOperations": {},
	// vpc.v1 — GatewayService (NAT egress)
	"/kacho.cloud.vpc.v1.GatewayService/Get":            {},
	"/kacho.cloud.vpc.v1.GatewayService/List":           {},
	"/kacho.cloud.vpc.v1.GatewayService/Create":         {},
	"/kacho.cloud.vpc.v1.GatewayService/Update":         {},
	"/kacho.cloud.vpc.v1.GatewayService/Delete":         {},
	"/kacho.cloud.vpc.v1.GatewayService/ListOperations": {},
	// compute.v1 — QuotaService (только чтение: величины администрируются на
	// внутреннем слушателе через iam.v1.InternalLimitService)
	"/kacho.cloud.compute.v1.QuotaService/List": {},

	// compute.v1 — InstanceService
	"/kacho.cloud.compute.v1.InstanceService/Get":                      {},
	"/kacho.cloud.compute.v1.InstanceService/List":                     {},
	"/kacho.cloud.compute.v1.InstanceService/Create":                   {},
	"/kacho.cloud.compute.v1.InstanceService/Update":                   {},
	"/kacho.cloud.compute.v1.InstanceService/Delete":                   {},
	"/kacho.cloud.compute.v1.InstanceService/GetSerialPortOutput":      {},
	"/kacho.cloud.compute.v1.InstanceService/Stop":                     {},
	"/kacho.cloud.compute.v1.InstanceService/Start":                    {},
	"/kacho.cloud.compute.v1.InstanceService/Restart":                  {},
	"/kacho.cloud.compute.v1.InstanceService/AttachDisk":               {},
	"/kacho.cloud.compute.v1.InstanceService/DetachDisk":               {},
	"/kacho.cloud.compute.v1.InstanceService/AttachNetworkInterface":   {},
	"/kacho.cloud.compute.v1.InstanceService/DetachNetworkInterface":   {},
	"/kacho.cloud.compute.v1.InstanceService/ListOperations":           {},
	"/kacho.cloud.compute.v1.InstanceService/SimulateMaintenanceEvent": {},
	// compute.v1 — MachineTypeService (read-only sizing catalog; cluster-viewer,
	// parity с geo Region/Zone). Admin CRUD — InternalMachineTypeService на :9091
	// (НЕ в allowlist; HasInternalSuffix блокирует автоматически, ban #6).
	"/kacho.cloud.compute.v1.MachineTypeService/Get":  {},
	"/kacho.cloud.compute.v1.MachineTypeService/List": {},
	// compute.v1 — GuestAccessKeyService (публичные ключи входа арендатора в свои
	// машины). Чтения sync, мутации — Operation. Пообъектный тип прав
	// (`compute_guest_access_key`) нужен потому, что запрос несёт только
	// идентификатор ключа: проект резолвит владелец прав, а не вызывающий.
	"/kacho.cloud.compute.v1.GuestAccessKeyService/Get":            {},
	"/kacho.cloud.compute.v1.GuestAccessKeyService/List":           {},
	"/kacho.cloud.compute.v1.GuestAccessKeyService/Create":         {},
	"/kacho.cloud.compute.v1.GuestAccessKeyService/Update":         {},
	"/kacho.cloud.compute.v1.GuestAccessKeyService/Delete":         {},
	"/kacho.cloud.compute.v1.GuestAccessKeyService/ListOperations": {},
	// compute.v1 — PlacementGroupService (правила взаимного размещения машин).
	// Пообъектный тип прав нужен по той же причине, что у ключа: запрос несёт
	// только идентификатор группы.
	"/kacho.cloud.compute.v1.PlacementGroupService/Get":            {},
	"/kacho.cloud.compute.v1.PlacementGroupService/List":           {},
	"/kacho.cloud.compute.v1.PlacementGroupService/Create":         {},
	"/kacho.cloud.compute.v1.PlacementGroupService/Update":         {},
	"/kacho.cloud.compute.v1.PlacementGroupService/Delete":         {},
	"/kacho.cloud.compute.v1.PlacementGroupService/ListOperations": {},
	// compute.v1 — Geography (Region/Zone) НЕ публичная поверхность compute:
	// выделена в leaf-сервис kacho-geo (см. geo.v1 ниже).

	// storage.v1 — QuotaService (только чтение: величины администрируются на
	// внутреннем слушателе через iam.v1.InternalLimitService)
	"/kacho.cloud.storage.v1.QuotaService/List": {},

	// storage.v1 — VolumeService (kacho-storage; Volume — block-storage ресурс,
	// выделен из compute Disk). Read — sync; мутации — async Operation (sop-prefix).
	"/kacho.cloud.storage.v1.VolumeService/Get":            {},
	"/kacho.cloud.storage.v1.VolumeService/List":           {},
	"/kacho.cloud.storage.v1.VolumeService/Create":         {},
	"/kacho.cloud.storage.v1.VolumeService/Update":         {},
	"/kacho.cloud.storage.v1.VolumeService/Delete":         {},
	"/kacho.cloud.storage.v1.VolumeService/ListOperations": {},
	// ChangeDiskType — смена класса на живом томе. Публичный глагол: арендатор
	// вправе переехать между классами, не пересоздавая том. Плоскость данных
	// переезда наружу не видна — она остаётся предметом сверщика.
	"/kacho.cloud.storage.v1.VolumeService/ChangeDiskType": {},
	// storage.v1 — SnapshotService (StorageSnapshot `snp`, отдельно от compute Snapshot)
	"/kacho.cloud.storage.v1.SnapshotService/Get":            {},
	"/kacho.cloud.storage.v1.SnapshotService/List":           {},
	"/kacho.cloud.storage.v1.SnapshotService/Create":         {},
	"/kacho.cloud.storage.v1.SnapshotService/Update":         {},
	"/kacho.cloud.storage.v1.SnapshotService/Delete":         {},
	"/kacho.cloud.storage.v1.SnapshotService/ListOperations": {},
	// Copy — перенос снимка в другую зону. Публичный: без него снимок заперт в
	// зоне своего тома, и восстановление в соседней зоне невозможно арендатору.
	"/kacho.cloud.storage.v1.SnapshotService/Copy": {},
	// storage.v1 — DiskTypeService (read-only справочник; admin-CRUD — через
	// InternalDiskTypeService на :9091, НЕ в allowlist).
	"/kacho.cloud.storage.v1.DiskTypeService/Get":  {},
	"/kacho.cloud.storage.v1.DiskTypeService/List": {},
	// storage.v1 — ImageService (StorageImage `img`; boot-image ресурс, выделен из
	// compute Image). Read — sync; мутации — async Operation. InternalImageService
	// (GetInternal, инфра-проекция) — НЕ в allowlist (HasInternalSuffix, ban #6).
	"/kacho.cloud.storage.v1.ImageService/Get":            {},
	"/kacho.cloud.storage.v1.ImageService/List":           {},
	"/kacho.cloud.storage.v1.ImageService/Create":         {},
	"/kacho.cloud.storage.v1.ImageService/Update":         {},
	"/kacho.cloud.storage.v1.ImageService/Delete":         {},
	"/kacho.cloud.storage.v1.ImageService/ListOperations": {},
	// Copy — перенос образа в другую зону. Публичный по той же причине, что и у
	// снимка: образ, запертый в одной зоне, не даёт запустить машину в соседней.
	// Register (принятие уже существующего объекта плоскости данных) публичным НЕ
	// является и живёт на InternalImageService — это административный глагол,
	// который называет координату в хранилище (ban #6).
	"/kacho.cloud.storage.v1.ImageService/Copy": {},
	// storage.v1 — InternalVolumeService (Attach/Detach/ListAttachments/GetInternal,
	// инфра-чувствительные placement-поля) и InternalDiskTypeService (admin CRUD) —
	// НЕ в allowlist (HasInternalSuffix блокирует автоматически; ban #6). :9091 only.

	// geo.v1 — RegionService (read-only справочник).
	// Geography живет в leaf-сервисе kacho-geo; теперь единственный owner.
	"/kacho.cloud.geo.v1.RegionService/Get":  {},
	"/kacho.cloud.geo.v1.RegionService/List": {},
	// geo.v1 — ZoneService (read-only справочник)
	"/kacho.cloud.geo.v1.ZoneService/Get":  {},
	"/kacho.cloud.geo.v1.ZoneService/List": {},
	// geo.v1 — InternalRegionService / InternalZoneService.* — НЕ в allowlist
	// (admin-CRUD на :9091; HasInternalSuffix блокирует автоматически, запрет #6).

	// iam.v1 — AccountService
	"/kaname.cloud.iam.v1.AccountService/Get":  {},
	"/kaname.cloud.iam.v1.AccountService/List": {},
	// quota.v1 — IdentityQuotaService (только чтение, и только о СЕБЕ: поля,
	// которым можно было бы назвать чужую личность, у запроса нет).
	//
	// Служба объявлена в пакете общей формы ответа, а не в `iam.v1`: та форма уже
	// зависит от `iam.v1`, и обратная ссылка замкнула бы пакеты друг на друга.
	"/kacho.cloud.quota.v1.IdentityQuotaService/List": {},

	"/kaname.cloud.iam.v1.AccountService/Create":            {},
	"/kaname.cloud.iam.v1.AccountService/Update":            {},
	"/kaname.cloud.iam.v1.AccountService/Delete":            {},
	"/kaname.cloud.iam.v1.AccountService/ListOperations":    {},
	"/kaname.cloud.iam.v1.AccountService/ListAllOperations": {}, // account-scoped module list (REST GET /iam/v1/accounts/{id}/operations:all)
	// iam.v1 — ProjectService
	"/kaname.cloud.iam.v1.ProjectService/Get":            {},
	"/kaname.cloud.iam.v1.ProjectService/List":           {},
	"/kaname.cloud.iam.v1.ProjectService/Create":         {},
	"/kaname.cloud.iam.v1.ProjectService/Update":         {},
	"/kaname.cloud.iam.v1.ProjectService/Delete":         {},
	"/kaname.cloud.iam.v1.ProjectService/ListOperations": {},
	// iam.v1 — UserService (НЕТ публичного Create — Users создаются через
	// InternalUserService.UpsertFromIdentity).
	// Update — публичная async-мутация: mutable только labels (identity-поля
	// immutable), возвращает Operation; parity с RoleService/ServiceAccountService.
	"/kaname.cloud.iam.v1.UserService/Get":            {},
	"/kaname.cloud.iam.v1.UserService/List":           {},
	"/kaname.cloud.iam.v1.UserService/Update":         {}, // public labels-only mutation (REST PATCH /iam/v1/users/{user_id}); record_writer on iam_user, acr 1
	"/kaname.cloud.iam.v1.UserService/Delete":         {},
	"/kaname.cloud.iam.v1.UserService/ListOperations": {}, // per-resource ops (REST GET /iam/v1/users/{user_id}/operations)
	// Административный запрет участию и его снятие (REST POST
	// /iam/v1/users/{user_id}:block|:unblock). Пара обязана быть здесь ОБЕ: без
	// записи в этом списке директор отвергает метод раньше, чем что-либо о нём
	// узнают каталог и таблица маршрутов, и односторонний недосмотр оставил бы
	// заблокированного без пути снятия.
	"/kaname.cloud.iam.v1.UserService/Block":   {}, // identity_suspender on iam_user, acr 2
	"/kaname.cloud.iam.v1.UserService/Unblock": {}, // identity_suspender on iam_user, acr 2
	// Приглашение по адресу почты (REST POST /iam/v1/users:invite) — единственный
	// публичный путь появления пользователя; Create по-прежнему только internal.
	"/kaname.cloud.iam.v1.UserService/Invite": {},
	// Исключение человека из аккаунта (REST POST
	// /iam/v1/users/{user_id}:removeFromAccount) — ПАРА к приглашению выше, и
	// стоять они обязаны обе: аккаунт, который умеет только вводить людей и не
	// умеет выводить, копит участников, которых не может убрать (#1127).
	// Гейтится `member_remover` на АККАУНТЕ (тот же круг, что у Invite), acr 2;
	// строку личности не трогает — её снятие спрашивает `identity_remover`
	// (#1131).
	"/kaname.cloud.iam.v1.UserService/RemoveFromAccount": {},
	// iam.v1 — UserTokenService (REST .../users/{user_id}/tokens) —
	// выдача, перечисление и отзыв неинтерактивных токенов пользователя.
	"/kaname.cloud.iam.v1.UserTokenService/Issue":  {},
	"/kaname.cloud.iam.v1.UserTokenService/List":   {},
	"/kaname.cloud.iam.v1.UserTokenService/Revoke": {},
	// iam.v1 — ServiceAccountService
	"/kaname.cloud.iam.v1.ServiceAccountService/Get":    {},
	"/kaname.cloud.iam.v1.ServiceAccountService/List":   {},
	"/kaname.cloud.iam.v1.ServiceAccountService/Create": {},
	"/kaname.cloud.iam.v1.ServiceAccountService/Update": {},
	"/kaname.cloud.iam.v1.ServiceAccountService/Delete": {},
	// Disable / Enable — explicit actions over the state that decides whether a
	// service account may authenticate. Public on purpose: it is the owner of a
	// machine identity who takes it out of service, not only a cluster operator.
	"/kaname.cloud.iam.v1.ServiceAccountService/Disable":        {},
	"/kaname.cloud.iam.v1.ServiceAccountService/Enable":         {},
	"/kaname.cloud.iam.v1.ServiceAccountService/ListOperations": {},
	// iam.v1 — SAKeyService (REST .../serviceAccounts/{service_account_id}/keys) —
	// выдача, перечисление и отзыв ключей служебной учётки.
	"/kaname.cloud.iam.v1.SAKeyService/Issue":  {},
	"/kaname.cloud.iam.v1.SAKeyService/List":   {},
	"/kaname.cloud.iam.v1.SAKeyService/Revoke": {},
	// iam.v1 — AuthorizeService (REST /iam/v1/authorize:* и /iam/v1/me) —
	// tenant-facing запросы к модели прав: предпросмотр разрешений в консоли и
	// самоописание вызывающего. Публичный сервис, не Internal*.
	"/kaname.cloud.iam.v1.AuthorizeService/Check":           {},
	"/kaname.cloud.iam.v1.AuthorizeService/BatchCheck":      {},
	"/kaname.cloud.iam.v1.AuthorizeService/ListSubjects":    {},
	"/kaname.cloud.iam.v1.AuthorizeService/ExpandRelations": {},
	"/kaname.cloud.iam.v1.AuthorizeService/WhoAmI":          {},
	// iam.v1 — GroupService
	"/kaname.cloud.iam.v1.GroupService/Get":            {},
	"/kaname.cloud.iam.v1.GroupService/List":           {},
	"/kaname.cloud.iam.v1.GroupService/Create":         {},
	"/kaname.cloud.iam.v1.GroupService/Update":         {},
	"/kaname.cloud.iam.v1.GroupService/Delete":         {},
	"/kaname.cloud.iam.v1.GroupService/AddMember":      {},
	"/kaname.cloud.iam.v1.GroupService/RemoveMember":   {},
	"/kaname.cloud.iam.v1.GroupService/ListMembers":    {},
	"/kaname.cloud.iam.v1.GroupService/ListOperations": {},

	// iam.v1 — MembershipService: чтение членства на аккаунт-скоупных путях.
	// Только два ЧТЕНИЯ: глаголов изменения у ресурса на этой поверхности нет.
	"/kaname.cloud.iam.v1.MembershipService/Get":  {},
	"/kaname.cloud.iam.v1.MembershipService/List": {},
	// iam.v1 — RoleService
	// Role.rules[].module — скалярная строка; REST Create/Update маршалят это
	// поле; отдельной allowlist-записи не требуется (новых RPC нет).
	"/kaname.cloud.iam.v1.RoleService/Get":            {},
	"/kaname.cloud.iam.v1.RoleService/List":           {},
	"/kaname.cloud.iam.v1.RoleService/Create":         {},
	"/kaname.cloud.iam.v1.RoleService/Update":         {},
	"/kaname.cloud.iam.v1.RoleService/Delete":         {},
	"/kaname.cloud.iam.v1.RoleService/ListOperations": {},
	// iam.v1 — AccessBindingService
	"/kaname.cloud.iam.v1.AccessBindingService/Get":    {},
	"/kaname.cloud.iam.v1.AccessBindingService/List":   {}, // unified paginated read (REST GET /iam/v1/accessBindings), F11
	"/kaname.cloud.iam.v1.AccessBindingService/Create": {},
	"/kaname.cloud.iam.v1.AccessBindingService/Update": {}, // public mutation (REST PATCH /iam/v1/accessBindings/{access_binding_id}); clears deletion_protection, editor relation (parity with Delete)
	"/kaname.cloud.iam.v1.AccessBindingService/Delete": {},

	// LimitService — административная поверхность пределов на ПУБЛИЧНОМ бэкенде
	// (ADM-1 S1, #878). Наружу выставлен публичный глагол, а не `Internal*`:
	// запрет 6 не смягчён, `HasInternalSuffix` не тронут. Доступ закрывает
	// отношение `system_admin` @ `cluster`, которое подстановочный кортеж
	// `user:*` не выполняет, — поэтому публикация адреса круга не расширяет, а
	// делает отказ честным: 403 вместо 404.
	"/kaname.cloud.iam.v1.LimitService/Get":            {},
	"/kaname.cloud.iam.v1.LimitService/List":           {},
	"/kaname.cloud.iam.v1.LimitService/Create":         {},
	"/kaname.cloud.iam.v1.LimitService/Update":         {},
	"/kaname.cloud.iam.v1.LimitService/Delete":         {},
	"/kaname.cloud.iam.v1.AccessBindingService/Revoke": {}, // soft-revoke :verb (REST POST /iam/v1/accessBindings/{access_binding_id}:revoke), F10

	"/kaname.cloud.iam.v1.AccessBindingService/ListByScope":           {}, // public sync read (REST GET /iam/v1/accessBindings:listByScope)
	"/kaname.cloud.iam.v1.AccessBindingService/ListBySubject":         {},
	"/kaname.cloud.iam.v1.AccessBindingService/ListByAccount":         {},
	"/kaname.cloud.iam.v1.AccessBindingService/ListOperations":        {}, // per-resource ops (REST GET /iam/v1/accessBindings/{access_binding_id}/operations)
	"/kaname.cloud.iam.v1.AccessBindingService/ListSubjectPrivileges": {}, // public sync read
	"/kaname.cloud.iam.v1.AccessBindingService/ListAssignableRoles":   {}, // public sync read (REST GET /iam/v1/accessBindings:listAssignableRoles)
	"/kaname.cloud.iam.v1.AccessBindingService/ListByRole":            {}, // public sync read (REST GET /iam/v1/accessBindings:listByRole)
	"/kaname.cloud.iam.v1.AccessBindingService/ExpandAccess":          {}, // public sync read (REST GET /iam/v1/accessBindings:expandAccess)
	// iam.v1 — PermissionCatalogService
	// Public, sync read (REST GET /iam/v1/permissionCatalog): authenticated-floor
	// read — отношение `viewer` на кластере, выданное системной выдачей субъекту
	// «любой аутентифицированный» (#893/#895), чтобы UI собирал role/permission
	// palette без Internal* RPC. Прежде здесь стояла полоса `<exempt>`: пол был тот
	// же, но доступ не показывался перечислением выдач и не отзывался. MUST be
	// reachable on the external listener (else 404/NotFound при gRPC-маршрутизации).
	"/kaname.cloud.iam.v1.PermissionCatalogService/ListPermissionCatalog": {},
	// iam.v1 — InternalIAMService / InternalUserService.* — НЕ в allowlist
	// (HasInternalSuffix блокирует автоматически; запрет #6). Речь ровно про
	// ВНЕШНИЙ listener: на внутреннем у части этих RPC есть и REST-маршрут
	// (restmux заводит их на internalMux), и это запрету #6 не противоречит.

	// loadbalancer.v1 — QuotaService (только чтение: величины администрируются на
	// внутреннем слушателе через iam.v1.InternalLimitService)
	"/kacho.cloud.loadbalancer.v1.QuotaService/List": {},

	// loadbalancer.v1 — NetworkLoadBalancerService (kacho-nlb)
	"/kacho.cloud.loadbalancer.v1.NetworkLoadBalancerService/Get":             {},
	"/kacho.cloud.loadbalancer.v1.NetworkLoadBalancerService/List":            {},
	"/kacho.cloud.loadbalancer.v1.NetworkLoadBalancerService/Create":          {},
	"/kacho.cloud.loadbalancer.v1.NetworkLoadBalancerService/Update":          {},
	"/kacho.cloud.loadbalancer.v1.NetworkLoadBalancerService/Delete":          {},
	"/kacho.cloud.loadbalancer.v1.NetworkLoadBalancerService/Move":            {},
	"/kacho.cloud.loadbalancer.v1.NetworkLoadBalancerService/GetTargetStates": {},
	"/kacho.cloud.loadbalancer.v1.NetworkLoadBalancerService/ListOperations":  {},
	// loadbalancer.v1 — ListenerService (first-class FGA object)
	"/kacho.cloud.loadbalancer.v1.ListenerService/Get":            {},
	"/kacho.cloud.loadbalancer.v1.ListenerService/List":           {},
	"/kacho.cloud.loadbalancer.v1.ListenerService/Create":         {},
	"/kacho.cloud.loadbalancer.v1.ListenerService/Update":         {},
	"/kacho.cloud.loadbalancer.v1.ListenerService/Delete":         {},
	"/kacho.cloud.loadbalancer.v1.ListenerService/ListOperations": {},
	// loadbalancer.v1 — TargetGroupService
	"/kacho.cloud.loadbalancer.v1.TargetGroupService/Get":            {},
	"/kacho.cloud.loadbalancer.v1.TargetGroupService/List":           {},
	"/kacho.cloud.loadbalancer.v1.TargetGroupService/Create":         {},
	"/kacho.cloud.loadbalancer.v1.TargetGroupService/Update":         {},
	"/kacho.cloud.loadbalancer.v1.TargetGroupService/Delete":         {},
	"/kacho.cloud.loadbalancer.v1.TargetGroupService/Move":           {},
	"/kacho.cloud.loadbalancer.v1.TargetGroupService/AddTargets":     {},
	"/kacho.cloud.loadbalancer.v1.TargetGroupService/RemoveTargets":  {},
	"/kacho.cloud.loadbalancer.v1.TargetGroupService/ListOperations": {},
	// loadbalancer.v1 — InternalResourceLifecycleService.* — НЕ в allowlist
	// (HasInternalSuffix блокирует автоматически; запрет #6). gRPC-direct only;
	// streaming Subscribe не имеет HTTP-аннотаций, REST не регистрируется.

	// registry.v1 — QuotaService (только чтение: величины администрируются на
	// внутреннем слушателе через iam.v1.InternalLimitService)
	"/kacho.cloud.registry.v1.QuotaService/List": {},

	// registry.v1 — RegistryService (kacho-registry, control-plane реестра)
	// Read — sync; мутации (Create/Update/Delete/DeleteTag) — async Operation.
	// List/ListRepositories/ListTags авторизуются listauthz внутри сервиса
	// (proto <exempt>), но в allowlist они присутствуют как публичные gRPC-пути.
	// InternalRegistryService.* (GC/stats admin, :9091) — НЕ в allowlist
	// (HasInternalSuffix блокирует автоматически; ban #6). Data-plane OCI v2 —
	// отдельная поверхность, не через api-gateway.
	"/kacho.cloud.registry.v1.RegistryService/Get":              {},
	"/kacho.cloud.registry.v1.RegistryService/List":             {},
	"/kacho.cloud.registry.v1.RegistryService/Create":           {},
	"/kacho.cloud.registry.v1.RegistryService/Update":           {},
	"/kacho.cloud.registry.v1.RegistryService/Delete":           {},
	"/kacho.cloud.registry.v1.RegistryService/ListRepositories": {},
	"/kacho.cloud.registry.v1.RegistryService/ListTags":         {},
	"/kacho.cloud.registry.v1.RegistryService/DeleteTag":        {},
	"/kacho.cloud.registry.v1.RegistryService/ListOperations":   {},
	// registry.v1 — Repository config-overlay (RG-1). Публичные RPC на том же
	// RegistryService: sync-чтение GetRepository/ListReferrers + async-мутации
	// CreateRepository/UpdateRepository/DeleteRepository/RenameRepository. Все
	// шесть — gateway `<exempt>` (per-repo Check + existence-hiding в handler'е:
	// COMPOSITE-объект registry_repository:<reg>/<repo> не выразим gateway
	// scope_extractor'ом; deny → uniform NOT_FOUND, иначе existence-oracle).
	"/kacho.cloud.registry.v1.RegistryService/GetRepository":    {},
	"/kacho.cloud.registry.v1.RegistryService/ListReferrers":    {},
	"/kacho.cloud.registry.v1.RegistryService/CreateRepository": {},
	"/kacho.cloud.registry.v1.RegistryService/UpdateRepository": {},
	"/kacho.cloud.registry.v1.RegistryService/DeleteRepository": {},
	"/kacho.cloud.registry.v1.RegistryService/RenameRepository": {},

	// operation (без v1!) — OperationService (in-process OpsProxy, фан-аут по domain-prefix)
	"/kacho.cloud.operation.OperationService/Get":    {},
	"/kacho.cloud.operation.OperationService/Cancel": {},
}

// IsAllowed проверяет, что метод находится в списке разрешенных публичных RPC.
func IsAllowed(methodPath string) bool {
	_, ok := AllowedMethods[methodPath]
	return ok
}

// HasInternalSuffix — эшелонированная защита: любой метод, чей gRPC-service
// помечен как internal, блокируется автоматически, даже если он случайно попал
// в AllowedMethods.
//
// Покрывает обе принятые в kacho-proto конвенции именования internal-сервисов:
//   - суффикс  "<Xxx>InternalService" (resource-manager: FolderInternalService);
//   - префикс  "Internal<Xxx>Service" (vpc: InternalAddressPoolService,
//     InternalNetworkService; compute: InternalMachineTypeService,
//     InternalRealizationService; geo: InternalRegionService, InternalZoneService).
//
// Путь имеет вид "/kacho.cloud.<domain>.v1.<Service>/<Method>"; проверяем сегмент
// между последней "." и "/".
func HasInternalSuffix(methodPath string) bool {
	if strings.Contains(methodPath, "InternalService") {
		return true
	}
	// methodPath = "/kacho.cloud.<domain>.v1.<Service>/<Method>"
	p := strings.TrimPrefix(methodPath, "/")
	slash := strings.IndexByte(p, '/')
	if slash < 1 {
		return false
	}
	pkgService := p[:slash] // "kacho.cloud.<domain>.v1.<Service>"
	dot := strings.LastIndexByte(pkgService, '.')
	if dot < 0 {
		return false
	}
	service := pkgService[dot+1:]
	return strings.HasPrefix(service, "Internal") && strings.HasSuffix(service, "Service")
}
