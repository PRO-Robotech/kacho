# Намеренные поведенческие решения (и где они отступают от конвенций Kachō)

Это **не баги** и **не задачи** — осознанные решения, которые могут удивить
ревьюера: либо мы **отступаем** от опубликованного контракта Kachō и его
конвенций (с обоснованием), либо **deliberately не делаем** того, что
напрашивается. Цель файла — чтобы это не «фиксили» по второму разу.

**Отступление измеряется от нашего собственного контракта** — proto-формы,
конвенций API (`api-conventions.md`), правил целостности данных и безопасности.
Сравнение с чужим API предметом записи быть не может: у такой записи нет
критерия закрытия, потому что закрывать нечего.

**Сюда НЕ пишем** то, что просто корректно реализует контракт — это спека
(см. `00-overview.md`, `01-resources.md`, `04-api-surface.md`). Например:
Compute-ресурсы project-scoped; `metadata` омитится из `Instance` в `List`;
Disk size max в Update меньше, чем в Create — всё это **и есть** контракт,
отступления тут нет.

Баги / подтверждённые отступления, которые решили выровнять — GitHub Issues
(`PRO-Robotech/kacho-compute` / `kacho-api-gateway`), см. `06-conventions.md`
→ «Где фиксировать находки» и workspace `CLAUDE.md` §14.4.

---

## 1. Malformed / wrong-prefix resource id → `NotFound` вместо `InvalidArgument`

Конвенция Kachō (`api-conventions.md`) требует: malformed own-owned id →
**первым стейтментом RPC** sync `InvalidArgument "invalid <res> id '<X>'"`
(`corevalidate.ResourceID`); well-formed-но-отсутствующий → `NotFound`.

Здесь первая половина не выполнена. Proto-поля `*_id` помечены только
`(length) = "<=50"` — это max-длина, не format-regex, а на входе RPC синтаксис id
**не валидируется** (нет prefix-check, нет base32-check) — идём в `repo.Get` → если
строки нет, получаем sentinel `ErrNotFound` → `NOT_FOUND "<Resource> <id> not found"`.
Следствие для вызывающего: явный мусор получает тон **отсутствия ресурса** вместо
терминального отказа по формату.

Выравнивание затрагивает ~все RPC, берущие resource-id, + newman-кейсы,
ассертящие «garbage id → InvalidArgument». **Что нужно для закрытия:** добавить
`corevalidate.ResourceID` первым стейтментом каждого handler'а (или общий
decorator) → завести GitHub Issue `PRO-Robotech/kacho-compute`, мигрировать
newman-кейсы. Низкоприоритетно (реальные клиенты в это редко упираются).

> §2 (name-validation) снят: единственным его содержанием была несверенность с
> чужим API, то есть предмета у записи не было. Собственный контракт имени
> зафиксирован proto-pattern'ом и `corevalidate.NameCompute` — см.
> `06-conventions.md`. Номер не переиспользуется: на §-номера ссылаются другие
> документы.

## 3. Instance precondition error texts ещё не закреплены как контракт

State-машина (см. `03-instance-lifecycle.md`) определена корректно по семантике,
но **тексты** `FailedPrecondition`-ошибок при нарушении precondition пока
placeholder'ы. По конвенции Kachō тон сообщений — часть контракта (стабильный,
меняется только осознанно), поэтому незакреплённая формулировка — отступление.
Текущие формулировки (могут ещё измениться):
- `Start` при не-`STOPPED` → ожидаем `"Instance is not stopped"` / `"Cannot
  start instance in state <X>"`;
- `Stop` при не-`RUNNING` → `"Instance is not running"` / `"Cannot stop instance
  in state <X>"`;
- `Restart` при не-`RUNNING` → `"Instance is not running"`;
- `Update` resources_spec/platform_id при не-`STOPPED` → `"Instance must be
  stopped"`;
- `AttachDisk`/`DetachDisk`/`AddNat`/`RemoveNat` при не-`{RUNNING,STOPPED}` →
  precondition-текст probe;
- `AttachNetworkInterface`/`DetachNetworkInterface` при не-`STOPPED` →
  proto-комментарий говорит «must have STOPPED status» — текст ошибки probe;
- `DetachDisk` boot disk → `"Boot disk cannot be detached"` / similar;
- `AttachDisk` disk не READY / wrong zone / уже attached → `"The disk is being
  used"` / `"Disk and instance must be in the same zone"` — probe.

**Что нужно для закрытия:** пройти каждый precondition (намеренно нарушить,
записать текст+код), зафиксировать формулировки в контракте — регламент
`tests/newman/docs/PRODUCT-REQUIREMENTS.md` + newman-ассерты на точный текст.
До закрепления — фиксируется здесь.

## 4. Disk size «only increase» / `Image.min_disk_size` constraint texts ещё не закреплены

- `Disk.Update` с уменьшением `size` → ожидаем `InvalidArgument "Disk size can
  only be increased"` (точная формулировка ещё не закреплена).
- `Disk.Create` с `image_id`, где `size < image.min_disk_size` → ожидаем
  `InvalidArgument "Disk size <X> is less than minimum disk size <Y> for image
  <id>"` (текст probe).
- `Disk.Create` с `snapshot_id`, где `size < snapshot.disk_size` → аналогично
  (текст probe).
- `block_size` whitelist — точный допустимый set (4096, 8192, ...) probe;
  невалидный → `InvalidArgument` (текст probe).

**Что нужно для закрытия:** закрепить тексты в регламенте
`tests/newman/docs/PRODUCT-REQUIREMENTS.md` и в newman-ассертах на точный текст;
несоответствие кода закреплённому тексту — Issue.

## 5. Control-plane simulation — Instance/Disk lifecycle мгновенный, данных нет

Самое крупное by-design расхождение. Kachō — control plane only:
- **Instance status transitions мгновенны** — нет реального гипервизора → переходы
  происходят синхронно внутри TX worker'а соответствующей операции (без таймеров,
  без задержки provisioning) — статус наблюдаем сразу после `Operation.done`.
- **`ERROR` / `CRASHED` статусы не достигаются штатно** — нет реального VM, нечему
  крашиться (объявлены в enum как часть контракта, но control plane их не выставляет).
- **`GetSerialPortOutput` — синтетический текст** (стабильный per-instance
  плейсхолдер вида `[ OK ] Reached target Multi-User System.`), не реальный
  console-вывод.
- **`Image.Create` через `uri`-source — мгновенный «download»** (control-plane
  заглушка), статус сразу `READY`, `storage_size` синтетический: реального
  скачивания из объектного хранилища нет, промежуточного `CREATING` не бывает.
- **disk data не существует** — Disk/Snapshot/Image — только метаданные. Snapshot
  «делается» мгновенно из Disk `READY`.
- **`SimulateMaintenanceEvent` — no-op** (operation сразу `done`, Instance не
  переселяется по `maintenance_policy`).
- **`reserved_instance_pool_id` / `host_group_id` / `host_id` / `gpu_cluster_id`
  / `placement_policy.placement_group_id`** — хранятся как переданные значения,
  но реальных ReservedInstancePool / HostGroup / Host / GpuCluster / PlacementGroup
  нет (proto vendored, реализация отложена) → existence-check этих ссылок **не
  делается** (в отличие от subnet/SG/address, которые валидируются через vpcClient).
- При краше pod'а compute операция остаётся `done=false` навсегда (общее
  ограничение `operations.Run` без heartbeat/cleanup; `operations.Wait(30s)` на
  graceful shutdown спасает только от in-flight worker'ов при штатном завершении).

**Это не «фиксится»** — это архитектурное решение Kachō (control plane only,
весь проект). Если когда-нибудь появится data-plane проект — он отдельный (как
`kacho-vpc-implement` для VPC).

## 6. DiskType / Region / Zone admin CRUD через `Internal*` сервисы — kacho-only расширение

Публичная поверхность справочника compute — только чтение: `MachineTypeService.{Get,List}`
(статический discovery, без Create/Update/Delete). Сеять справочник всё равно кому-то
надо, поэтому admin-CRUD живёт в `InternalMachineTypeService.{Create,Update,Delete}` на
cluster-internal порту `:9091`, проброшенном через api-gateway internal mux на
`/compute/v1/internal/machineTypes` — для admin-tooling / UI.

> [!note] Здесь перечислялись ещё два справочника — их владелец не compute
> Прежняя редакция называла в этом же ряду справочник типов дисков и ось размещения
> (регион/зона) вместе с их внутренними админ-сервисами и одним REST-адресом. Ни одного
> из этих контрактов в `proto/kacho/cloud/compute/v1/` нет (предикат:
> `grep -hE '^message (DiskType|Region|Zone)\b'` даёт ноль), владельцы — `services/storage/`
> и `services/geo/`. Снятый адрес не воспроизводится: в обратных кавычках он читается как
> живой маршрут, которого край не обслуживает.

Это **сознательное решение** (admin-функция не расширяет публичный сервис
ресурса — иначе она засветилась бы на external endpoint). На external TLS
endpoint эти POST/PATCH/DELETE paths
**не должны** быть доступны (workspace `CLAUDE.md` §запрет 6) — публичными
остаются только Get/List у `DiskTypeService` / `RegionService` / `ZoneService`.

### 6.1. Geography (Region/Zone) — owner kacho-compute (эпик KAC-15)

Раньше зоны проксировались из kacho-vpc `InternalZoneService` («зону бери из VPC
модуля»). С эпика **`KAC-15`** Geography (Region/Zone) **полностью переехала в
kacho-compute** — это **намеренное решение** (не баг): компьют — owner, у него
свои таблицы `regions`/`zones` (миграция `0003_geography_owner.sql`, seed
`ru-central1` + `ru-central1-{a,b,d}` здесь), `RegionService`/`ZoneService` читают
из них; **нет** ни proxy в kacho-vpc, ни `skipPeer`-fallback-таблицы. `disk_types.
zone_ids`, `Disk.zone_id`, `Instance.zone_id` валидируются локально. Другие
сервисы (kacho-vpc — `Subnet.zone_id`, `AddressPool.zone_id`, `Address.zone_id`)
валидируют `zone_id` вызовом нашего `ZoneService.Get` (`kacho-vpc → kacho-compute`
runtime-edge; раньше было наоборот). `Region.Delete` блокируется FK `zones.region_id`
RESTRICT (same-DB), если есть зоны; `Zone.Delete` проверяет своих dependents
(instances/disks/disk_types), кросс-сервисных (vpc-подсети) — нет (admin-ответственность,
workspace `CLAUDE.md` §«Кросс-доменные ссылки на ресурсы»). Старый
`KACHO_COMPUTE_VPC_INTERNAL_GRPC_ADDR` и зеркало-таблица `zones` упразднены.

### 6.2. `Instance` NIC бэкуется ресурсом kacho-vpc `NetworkInterface` (эпик KAC-9)

> **Снято (no auto-NIC).** `Instance` создаётся **без** network interface:
> NIC-привязка вынесена из lifecycle Instance целиком. `Instance.Create`
> игнорирует `network_interface_specs`, NIC-строки в `instance_network_interfaces`
> не пишутся и не читаются, а RPC `AttachNetworkInterface` /
> `DetachNetworkInterface` / `UpdateNetworkInterface` / `AddOneToOneNat` /
> `RemoveOneToOneNat` — `Unimplemented`. Соответственно снято и ребро
> `kacho-compute → kacho-vpc` (NIC-spec/IPAM): единый клиент vpc и его порт удалены как
> мёртвый код — имя файла здесь не воспроизводится, процитированное оно читается как живая
> координата. Сегодня в `internal/clients/` лежат два узких клиента vpc (NIC и подсеть),
> заведённые под другую задачу. Описание ниже сохранено как история прежнего дизайна.

**Намеренное решение** (clean-API дизайн; verbatim-parity отложена): compute-NIC
бэкуется first-class ресурсом kacho-vpc `NetworkInterface` (вариант А, эпик
`KAC-2`/`KAC-9`). `compute.v1.Instance.NetworkInterface += nic_id` (proto field 7;
а `NetworkInterfaceSpec += nic_id`); он source of truth интерфейса (адрес, SG,
data-plane wiring), `subnet_id` / `primary_v4_address` / `security_group_ids` —
read-only denorm-зеркало. `NetworkInterfaceSpec` принимает **exactly one of
{`subnet_id`, `nic_id`}** — `subnet_id` больше **не** безусловно `(required)`. На
`Instance.Create`: `nic_id` → attach уже существующего kacho-vpc NIC к инстансу;
`subnet_id` → inline-создание Address + NetworkInterface + attach. `SKIP_PEER_VALIDATION`
→ синтетический NIC без kacho-vpc-ресурса (`nic_id=''`). На `Instance.Delete` —
detach + delete kacho-vpc NIC (release его Address-ресурсов; best-effort). Device-index
интерфейса — `compute.v1.NetworkInterface.index` (как было). Миграция
`0005_instance_nic_id.sql` (`instance_network_interfaces.nic_id TEXT NOT NULL
DEFAULT ''`; `''` = legacy / synthetic NIC). Новый runtime cross-domain edge
(зафиксирован в workspace `CLAUDE.md`): `kacho-compute → kacho-vpc` (NIC
create/attach/detach + эфемерный Address IPAM).

## 7. Blocked-on-missing-service — отложено до появления зависимого сервиса

Не расхождение «по решению», а пробел из-за нереализованного peer-сервиса
(workspace `CLAUDE.md` §запрет 4 / принцип 4 — откладываем только то, чему нужен
ещё не существующий сервис). Помечается `blocked:*`-меткой.

| Что | Зависит от | Текущее поведение | Что нужно для закрытия |
|---|---|---|---|
| `Disk.Create` / `AttachedDiskSpec.DiskSpec` поле `kms_key_id`; `Disk/Image/Snapshot.kms_key` | `kacho-kms` | поле принимается синтаксически, но шифрование не реализовано; `kms_key` в ответе пуст. Попытка использовать → `blocked:kacho-kms` (либо игнор, либо `Unimplemented`) | реализовать `kacho-kms` → валидировать `kms_key_id` через kms-client, проставлять `kms_key` |
| `Image.Create` поле `os_product_ids` (marketplace product IDs) | `kacho-marketplace` | `product_ids` хранятся как переданы (license IDs), но marketplace-семантика не реализована | реализовать `kacho-marketplace` → валидировать product IDs |
| `Instance.filesystems[]` / `filesystem_specs[]` | `kacho-filesystem` (ресурса Filesystem нет) | `filesystem_specs[]` в `Instance.Create` отвергается синхронным `INVALID_ARGUMENT` (см. §7.1); `filesystems[]` всегда пуст. RPC `AttachFilesystem`/`DetachFilesystem` и весь контракт `FilesystemService` **удалены** как мертворождённые (не были реализованы, зарегистрированы и не имели типа в модели прав) | реализовать `kacho-filesystem` + ресурс Filesystem → вернуть attach/detach контракт вместе с реализацией |
| `Disk.Create` поле `snapshot_schedule_ids` | `kacho-snapshot-schedule` | `snapshot_schedule_ids` игнорируется. RPC `Disk.ListSnapshotSchedules` и весь контракт `SnapshotScheduleService` **удалены** как мертворождённые (не были реализованы, зарегистрированы и не имели типа в модели прав) | реализовать `kacho-snapshot-schedule` + ресурс SnapshotSchedule → вернуть list-контракт вместе с реализацией |
| `Disk.Relocate` (cross-zone disk move) | — (частично; нужен реальный cross-zone disk relocation pipeline) | меняет `zone_id` с проверкой «disk не attached»; cross-zone semantics simplified (нет реального переноса данных — control-plane) | по сути закрыто на control-plane уровне; «полное» закрытие требует data-plane (не делается) |
| `Instance.Relocate` (cross-zone instance move) | `Disk.Relocate` + restart-семантика | `Unimplemented` / частично | реализовать cross-zone disk move для всех attached disks + restart-логику |
| `Instance.SimulateMaintenanceEvent` | — (control-plane: нечего симулировать) | no-op (operation сразу done, Empty) | по сути закрыто на control-plane уровне; «реальное» поведение требует data-plane |
| Ресурсы `DiskPlacementGroup`, `PlacementGroup`, `HostGroup`, `HostType`, `GpuCluster`, `Filesystem`, `SnapshotSchedule`, `ReservedInstancePool`, `Maintenance` | каждый — отдельный store/домен | proto vendored, сервисы не реализованы (`enhancement` / `blocked:*`); связанные поля в Instance/Disk хранятся, но не интерпретируются | реализовать соответствующие домены (отдельные acceptance-документы) |

> Каждый `blocked:*` пункт также имеет (или должен иметь) GitHub Issue в
> `PRO-Robotech/kacho-compute` с меткой `blocked:<service>` и описанием «при
> каких условиях браться». Этот файл — карта by-design состояния; Issues —
> трекинг работы.

### 7.1. Поля, которые принимались и выбрасывались, — отвергаются явно

`CreateInstanceRequest` несёт семь полей, за которыми в compute нет **ни одной**
подсистемы. До 2026-07-28 они принимались и молча выбрасывались: клиент получал
`200` + `Operation`, инстанс создавался, а параметр не применялся нигде. Это
запрещённый исход (workspace `.claude/rules/api-conventions.md`
§«Принято-и-проигнорировано»): вызывающий уверен, что настройка сработала, и
узнаёт правду только по последствиям. Теперь каждое из шести отвергается
**синхронно, первым стейтментом `Create`** — до конвертации в use-case и до любой
другой валидации (`handler.RejectUnsupportedCreateFields`).

| Поле (proto № ) | Что было бы нужно, чтобы его реализовать | Почему отказ, а не реализация |
|---|---|---|
| `network_settings` (15) | ускорение NIC (`SOFTWARE_ACCELERATED`/`HARDWARE_ACCELERATED`) — свойство физического хоста и его сетевой карты | NIC — first-class ресурс `kacho-vpc`, а host-wiring по two-projection живёт только в `Internal*`. Публичного места под этот выбор в compute нет |
| `filesystem_specs` (17) | домен `kacho-filesystem` + ресурс `Filesystem` (см. таблицу §7) | контракт `FilesystemService` уже удалён как мертворождённый с формулировкой «вернуть вместе с реализацией»; поле-спека шло тем же путём |
| `local_disk_specs` (18) | провижининг host-local диска на выбранном хосте | host-local ёмкость выбирается **типом машины** (`MachineType`), а не массивом на каждом инстансе — это отдельный канал, а не поле в чужом сообщении |
| `maintenance_policy` (21) | планировщик обслуживания хостов + живая миграция | data-plane; у control-plane нечего переселять (ср. §7: `SimulateMaintenanceEvent` — no-op ровно по этой причине) |
| `maintenance_grace_period` (22) | то же + уведомление гостя через metadata-сервис | та же подсистема, что и (21) |
| `serial_port_settings` (23) | домен авторизации в консоли (`INSTANCE_METADATA`/`OS_LOGIN`) | домена OS-Login нет, а чтение консоли — отдельная read-поверхность, не настройка на `Create` |
| `ssh_public_keys` (33) | доставка ключей в гостя: metadata-сервис либо guest-agent, плюс место хранения | control-plane в гостя ничего не кладёт. Ключи не персистились ни в одной колонке и не читались ни одним потребителем — но при этом **удовлетворяли страж достижимости** (`F5`): «машина будет запущена и недостижима» снималось списком, который никуда не доедет, то есть страж отпускал ровно тот случай, ради которого заведён. Терм снят вместе с приёмом поля; страж теперь снимается только внешним адресом или `acknowledgeUnreachable` |

**Форма отказа** (часть контракта): `INVALID_ARGUMENT`, сообщение **обобщённое**
(`"invalid argument"`), имена полей — в `google.rpc.BadRequest.field_violations[].field`
(snake_case). Сообщаются **все** непринимаемые поля запроса разом, чтобы легаси-клиент
узнал о них за один заход. Проверка идёт первой, поэтому запрос, невалидный и по другой
причине, всё равно отвечает про непринимаемое поле.

**Граница:** proto3 не отличает «поле не прислано» от «прислано нулевое значение» на
non-optional enum и на пустом `repeated`, поэтому зацепка — **заданное** значение
(непустой список / ненулевой enum / непустое вложенное сообщение). `maintenancePolicy:
"MAINTENANCE_POLICY_UNSPECIFIED"` и `filesystemSpecs: []` проходят: вызывающий ничего
не утверждал.

**Почему поля не сняты с контракта.** Третий законный исход — удалить поле из proto с
`reserved` номера и имени. Здесь он **не** выбран, и не «на будущее»: REST-край
разбирает тело с `DiscardUnknown: true` (`gateway/internal/restmux/mux.go`, см. также
`tests/newman/docs/RESULTS.md` §1 — наблюдалось на уже снятых полях), поэтому снятое
поле снова даёт `200` с молча отброшенным ключом. Обещание исчезло бы из схемы, но
обман вызывающего остался бы — и стал бы ненаблюдаемым для чёрного ящика. Отказ
убирает и то, и другое. Снятие станет верным ходом, когда край перестанет
отбрасывать неизвестные ключи молча; до тех пор это регресс, а не уборка.

**`UpdateInstanceRequest` — закрыто тем же способом.** Остаток (`ssh_public_keys` 23,
`metadata` 8, `network_settings` 10, `maintenance_policy` 14,
`maintenance_grace_period` 15, `serial_port_settings` 16) был прикрыт `update_mask`
**частично и потому обманчиво**: known-set (`instanceUpdateKnown`) их не содержит,
поэтому явное упоминание в маске давало `400` — но при **пустой** маске (full-object
PATCH) тело снова принималось и выбрасывалось. Один и тот же параметр отвечал
по-разному в зависимости от маски, а в «тихой» ветке — успехом. `ssh_public_keys`
вдобавок штамповал `statusReason` «takes effect on next boot»: продукт не просто
игнорировал параметр, он **подтверждал** его приём. Теперь все шесть отвергаются
`handler.RejectUnsupportedUpdateFields` первым стейтментом — той же формой отказа, и
одинаково при пустой и при явной маске. `metadata` при этом не «потеряна»: канал её
правки существует отдельным RPC (`UpdateMetadata`), и отказ на него указывает.

**`GetInstanceSerialPortOutputRequest.port` (2)** — тот же класс на read-поверхности:
ответ синтетический (control-plane консолей гостя не держит) и от порта не зависит,
поэтому принятый номер порта был обещанием выбора, которого нет. Отвергается
(`handler.RejectUnsupportedSerialPortFields`); незаданный порт проходит.

**Замки:** `internal/handler/instance_create_unsupported_fields_test.go` (по полю:
код + имя в деталях + обобщённый текст + «Operation не создана»; первенство проверки;
все за один заход; нулевые значения проходят; разбор ровно тех REST-тел, что шлёт
newman); `internal/handler/instance_dropped_fields_test.go` (седьмое поле Create,
шесть полей Update при пустой И при явной маске, `port`, плюс контрольные случаи
«обычная правка проходит» и «незаданный порт проходит»);
`tests/newman/cases/instance-redesign.py` (`INST-RD-CR-VAL-UNSUPPORTED-*` — семь
негативов + «все семь разом», `INST-RD-UPD-VAL-UNSUPPORTED-*`,
`INST-RD-GET-VAL-UNSUPPORTED-SERIAL-PORT`).

**Гейт класса, а не перечень замков:**
`internal/handler/request_field_classification_test.go` обходит дескрипторы входных
сообщений всей выставленной поверхности и требует, чтобы КАЖДОЕ поле лежало ровно в
одной из трёх полос — «читается» / «отвергается» (проверяется поведением) /
«недостижимо, потому что RPC отдаёт `Unimplemented`» (проверяется вызовом). Новое
поле в proto, не попавшее никуда, роняет сборку в тот же момент, когда появилось.
Гейт честно объявляет границу полосы «читается»: ссылка доказывает **упоминание**, а
не влияние на поведение — ровно на этом и держался `ssh_public_keys`, который хендлер
читал, передавал в DTO и терял. Поэтому полосу «отвергается» гейт подтверждает
поведением, а не декларацией; при поиске ссылок тела функций `RejectUnsupported*`
вырезаются, иначе сама проверка отказа выглядела бы чтением (проверено инъекцией: без
вырезания гейт оставался зелёным на реальном дефекте).

## 8. Instance NIC IPv4 — реальные адреса через эфемерные VPC `Address`-ресурсы

> **Снято (no auto-NIC).** Instance больше не выделяет IPv4 через kacho-vpc IPAM:
> раз NIC не создаётся (см. §6.2), эфемерные `Address`-ресурсы, referrer-tracking
> и teardown не выполняются. Прежний единый клиент vpc (IPAM + referrer) удалён как мёртвый
> код — имя не воспроизводится по той же причине, что выше. Раздел ниже сохранён как история
> прежнего дизайна.

`Instance.Create` (и `AddOneToOneNat`) выделяют **реальные** IPv4 для NIC-ей
через kacho-vpc IPAM, создавая в kacho-vpc эфемерные `Address`-ресурсы:

- **internal IP** — `AddressService.Create` с `internal_ipv4_address_spec.subnet_id`
  → kacho-vpc inline выделяет IP из CIDR подсети; compute читает его обратно и
  хранит `address.id` в колонке `instance_network_interfaces.primary_v4_address_id`;
- **external (one-to-one NAT) IP** — `AddressService.Create` с
  `external_ipv4_address_spec.zone_id` → kacho-vpc inline выделяет публичный IP
  из `AddressPool` (cascade resolve); `address.id` + флаг `ephemeral` хранятся
  в JSONB `primary_v4_nat`. Если клиент передал `one_to_one_nat.address_id` — это
  его reserved Address, compute его **не** создаёт и **не** удаляет (`ephemeral=false`).

На `Instance.Delete` (и `RemoveOneToOneNat`) compute удаляет эти эфемерные
`Address`-ресурсы (best-effort: VPC недоступен / уже удалён → warning в лог, не
валит операцию). Если клиент передал `primary_v4_address_spec.address` вручную —
адрес валидируется на принадлежность CIDR подсети и используется как есть,
`Address`-ресурс не создаётся. В режиме `KACHO_COMPUTE_SKIP_PEER_VALIDATION=true`
NIC-ам по-прежнему выдаются синтетические IP (`10.0.0.x` / `203.0.113.x`), VPC не
дёргается.

**Referrer-tracking (с этой фичи) и эфемерный-in-use:** аллокация
адресов в `AddressService.Create` рождает их в состоянии `reserved=true, used=false`
(как обычные user-reserved-адреса). Compute после успешного `repo.Insert` инстанса
помечает их фактическим состоянием:

- **эфемерные адреса, которые compute создал сам** (internal `<vmid>-nicN` и
  ephemeral external `<vmid>-natN`, если NAT не указывал `address_id`) → вызов
  `InternalAddressService.MarkAddressEphemeralInUse(addressId, "compute_instance",
  instanceId, instanceName)` — атомарно (в одной tx на стороне kacho-vpc) ставит
  `reserved=false, used=true` и upsert-ит referrer. В REST-ответе адрес выглядит
  как `{"reserved": false, "used": true, "usedBy": [{"referrer":
  {"type":"compute_instance","id":"<instanceId>"}}, "type":"USED_BY"}]}` —
  «эфемерный, в работе у инстанса».
- **reserved user-адреса** (`one_to_one_nat.address_id` указан клиентом)
  → `SetAddressReference` (только referrer, `reserved=true` не трогаем) — адрес
  остаётся reserved-by-user, просто получает `used_by` ссылку на инстанс.

Обе операции best-effort: ошибка → warning, IP уже выделен, `Instance.Create`
не валится. На `Instance.Delete`/`RemoveOneToOneNat`: эфемерные адреса
удаляются (`DeleteAddress` — referrer-row уходит через FK CASCADE в kacho-vpc),
у reserved-адреса referrer снимается явно (`ClearAddressReference`) — адрес
снова `used=false`, остаётся reserved. В `KACHO_COMPUTE_SKIP_PEER_VALIDATION=true`
все Mark/Set/ClearAddressReference — no-op.

**Что НЕ расхождение:** field-семантика эфемерных адресов теперь корректна
(`reserved=false, used=true, used_by=[…]`) — никакого «фиктивно reserved» не
осталось.

**Осознанный trade-off, который сохраняется:** внутренний IPAM инстанса
**не прозрачен** — каждый авто-аллоцированный NIC-IP это полноценная строка в
`addresses`, видимая в `AddressService.List` (`GET /vpc/v1/addresses?projectId=...`,
`name` вида `<instanceId>-nic0` / `<instanceId>-nat0`, поля
`reserved=false, used=true, used_by=[…]`). Цена — служебные адреса видны тенанту
в списке; выигрыш — переиспользование существующего VPC IPAM без новых
cross-service RPC / миграций в kacho-vpc. Альтернатива (тонкий internal-RPC
`AllocateInternalIPInSubnet` / `AllocateExternalIPInZone` + лёгкая таблица
allocations в kacho-vpc) отложена. Newman-кейсы kacho-vpc, проверяющие
`AddressService.List`, изолированы по `runId` и не пересекаются с
compute-инстансами, так что суиты не ломаются.

---

## 9. FGA-register intent несёт labels+parent и эмитится на `Instance.Update(labels)` (epic RSAB β)

Эпик **Resource-scoped AccessBinding**, под-фаза **β** (label+parent sync
`compute→iam`). compute проталкивает копию labels + parent-scope каждого ресурса
в kacho-iam по **существующему** ребру `compute→iam` (FGA-proxy `RegisterResource`,
SEC-D) — IAM наполняет output-only зеркало `resource_mirror` (источник истины =
compute), которое γ будет читать для selector-матчинга и containment-гейта.
Граф остаётся ацикличным: данные **push-ит** consumer (compute), IAM ничего не
запрашивает (нет ребра `iam→compute`).

Намеренные решения compute-стороны:

- **Payload расширен внутри существующего `payload JSONB`, БЕЗ новой миграции.**
  `fgaintent.Payload` теперь несёт `labels` + `parent_project_id`
  (`= project_id` ресурса) рядом с owner-tuple set — ровно так же, как `Tuples`
  уже лежат в той же JSONB-колонке `compute_fga_register_outbox.payload`.
  `compute_fga_register_outbox` — транзиентный relay (строки дренятся и могут
  прунится); выделенные колонки денормализовали бы opaque-relay payload без
  потребителя. Поэтому schema-миграция **не требуется** (additive-поле внутри
  JSONB). Это by-design, не пропуск (ban #5/#11 не нарушены — applied-миграции
  не тронуты, тех-долга нет).
- **`parent_account_id` пуст.** compute не резолвит `project→account` на
  hot-path ресурса (owner-tuple использует только `project:<id>`). IAM
  принимает пустой parent gracefully (β-02/β-09). Если в будущем понадобится
  account-scope, его резолв добавляется на стороне compute или IAM отдельно.
- **Новый Update-триггер только на `labels`-mask (D-β6).** `emitFGARegisterIntent`
  эмитится на `Instance.Update` **тогда и только тогда**, когда `labels` в
  update-mask (или full-object PATCH применяет labels) — `InstanceRepo.Update`
  принимает `emitLabelsRegister bool`, use-case вычисляет его из mask. Прочие
  mutable-поля (name/description/…) **не** триггерят intent: они не влияют ни на
  label-membership, ни на parent (`project_id` инстанса immutable). Эмиссия — в
  той же writer-tx, что UPDATE (atomic, ban #10). Идемпотентность сохранена
  (at-least-once drainer → повтор payload, IAM UPSERT-идемпотентен).
- **Scope β = compute-first (D-β8).** Расширение реализовано для всех compute-
  ресурсов (Instance/Disk/Image/Snapshot — register-intent несёт их `Labels`
  единообразно: дёшево, один механизм `emitFGARegisterIntent`). Update-on-labels-
  триггер изначально только у **Instance** (β таргетировал `compute.instance`); для
  Disk/Image/Snapshot Update-триггер достроен в под-фазе **T3.1** (см. ниже).
  vpc-сторона — отдельная волна **β2**.

### 9.1 Update-on-labels emit достроен на Disk/Image/Snapshot (под-фаза T3.1, #113)

Закрытие разрыва D-β8 (и бага #113): ARM_LABELS-грант на cross-service
compute-ресурс **не ревокался** при снятии/смене метки, т.к. `Disk/Image/Snapshot.Update`
не эмитили `RegisterResource`/mirror.upsert на label-change → IAM `resource_mirror`
протухал и rsab держал стейл-членство. **Create-эмит этих трёх ресурсов уже нёс
`result.Labels` корректно** (bare-create-бага, как у vpc.SG/nlb.listener, у compute
**нет** — §0.1 acceptance T3.1). Фикс — только Update-путь:

- `DiskRepo.Update` / `ImageRepo.Update` / `SnapshotRepo.Update` теперь принимают
  `emitLabelsRegister bool` (parity с `InstanceRepo.Update`); use-case вычисляет его
  из update-mask (`labelsInMask`: `labels` ∈ mask, либо empty-mask = full-PATCH ⇒ true).
  Эмит `emitFGARegisterIntent(EventRegister, …, result.Labels)` — в **той же writer-tx**,
  что и UPDATE (atomic, ban #10 / SEC-D; rollback Update ⇒ intent не записан).
- **Gated (G-2):** non-label Update (name/description/size) **не** эмитит intent —
  меньше reconcile-шума; external-наблюдаемое поведение идентично always-emit
  (`source_version`-monotonic делает «лишний» upsert безвредным).
- **Полное снятие меток → mirror.upsert с `labels={}`, НЕ `UnregisterResource` (G-3).**
  Ресурс жив; mirror-строка должна остаться (с пустыми labels) — owner-tuple/containment
  на той же строке не сносятся, протухают **только** label-селекторы. `UnregisterResource`
  остаётся исключительно на `Delete` ресурса.
- IAM-сторона изменений **не требует** (G-6): rsab reconciler уже eager-revoke-ит
  fell-out members на `mirror.upsert` (`access_binding/reconcile/reconcile.go`).

Покрытие: `internal/repo/{disk,image,snapshot}_repo_integration_test.go`
(`Test{Disk,Image,Snapshot}Repo_T31Revoke03*_LabelRemoveEmitsMirrorUpsert` +
`*_T31Idm03*_NonLabelUpdateNoEmit`), testcontainers.

Регистрация intent остаётся **Internal-only :9091** (ban #6); расширение payload
authz не меняет (решение по subject-cert, не по содержимому payload — D-β9).

---

## Подтверждённые отступления, вынесенные в issues (здесь — указатель)

- **Malformed / wrong-prefix resource id → `NotFound` вместо `InvalidArgument`**
  — см. §1 выше. Тот же паттерн в kacho-vpc. → GitHub Issue
  `PRO-Robotech/kacho-compute` (создать при выравнивании).
- **`OperationService.Get`/`Cancel` с bad id** — api-gateway opsproxy парсит
  первые 3 символа id, на любой нероутящийся id возвращает `400 INVALID_ARGUMENT
  "operation_id has unknown prefix"`. По конвенции by-lane split
  (`api-conventions.md`) well-formed-но-нерезолвящийся own-owned id — это
  direct-read lane, то есть `404 NotFound "Operation <X> not found"`; malformed —
  `400 InvalidArgument`. Сейчас различия нет, оба схлопнуты в 400 — отступление по
  коду. Общий для всех kacho-* (issue в `kacho-api-gateway`, см. `../kacho-vpc/docs/
  architecture/07-known-divergences.md`).

---

## Security-hardening audit 2026-07-05 — осознанные архитектурные расхождения

Часть находок аудита закрыта фиксами (branch `sec-hardening-2026-07-05`); ниже —
находки, которые **осознанно НЕ меняются** как by-design, с обоснованием.

### OperationService.Get/Cancel — не гейтятся моделью прав; владелец энфорсится в сервисе

`OperationService.Get`/`Cancel` в `internal/check/permission_map.go` помечены
`Public: true` — на них **не** гоняется per-RPC FGA-Check.

**Почему не Check.** В модели прав нет типа объекта под операцию, и per-operation
tuple'ов никто не эмитит, поэтому вопрос «viewer на этой операции» **не имеет пути**
и отверг бы даже поллинг самого создателя сразу после успешной мутации. api-gateway
помечает эти RPC `<exempt>` по той же причине; `Public: true` в compute держит
интерсептор консистентным с ним (map-miss иначе fail-closed'ился бы `ErrUnmapped`).

> **Прежняя редакция описывала это как capability-модель и записывала «осознанно
> принятый риск» — она устарела (сверено с деревом 2026-08-02).** Здесь стояло, что
> авторизация сводится к знанию непрозрачного id, а усиление — «поведенческое
> изменение замороженного контракта, вне scope». Усиление **приземлилось**: доступ
> привязан к принципалу, создавшему операцию, предикатом владельца **в самом SQL**
> (`GetOwned` / `CancelOwned`, `internal/handler/operation_handler.go`); запрос без
> опознанного принципала владельцем не считается; не-владелец и несуществующая
> операция отвечают **одинаковым** `NotFound`, поэтому ответ не сообщает, существует
> ли операция вообще. Замки: `operation_handler_test.go`,
> `operation_ownership_forged_admin_test.go`.
>
> Оставлять запись в прежнем виде было опаснее, чем не иметь её вовсе: реестр
> расхождений читают как источник **решений**, поэтому следующий аудитор
> перепринимает то, что уже закрыто, — либо кто-то откатывает фикс на авторитете
> документа. Тот же исход разобран в `services/registry/docs/architecture/known-divergences.md` §9.

**Класс, ради которого запись остаётся.** Непрозрачный идентификатор — это
**capability**. Там, где он единственное основание доступа, любая его утечка
(журнал, заголовок перехода, общий трейс) становится выдачей прав, которую нельзя
ни ограничить областью, ни отозвать, ни увидеть в аудите. Поэтому «идентификатор
длинный и случайный» — не замена решению о доступе, а самое большее его дополнение:
энтропия защищает от перебора и ни от чего больше.

**Как поступать, когда модели нечего спросить.** Ровно так, как поступили здесь:
авторизовать **данными** — предикатом в запросе к своей БД, — а не отсутствием
вопроса. Отсутствие подходящего объекта в модели прав есть основание не звать
интерсептор, но **не** основание не принимать решения о доступе. Тот же ответ у
`vpc` §21 для RPC, у которого единого объекта нет by construction.

### service-слой возвращает gRPC-status (sentinel→code маппинг в use-case, не в handler)

Use-case'ы `internal/apps/kacho/api/<resource>/*` импортируют `google.golang.org/grpc/codes`+`status` и возвращают
`*status.Error` (через `mapRepoErr`/`mapZoneRefErr`). Формально §«Чистая архитектура»
(CLAUDE.md) относит transport-code маппинг к тонкому handler-слою.

**Почему остаётся в use-case (by-design для async-LRO):** все мутации — async
`Operation` (ban #9). Финальный gRPC-status ошибки мутации выставляется **воркером**
внутри `operations.Run(...)`-замыкания на `Operation.error`, а НЕ возвращается в
handler (handler уже отдал `Operation{done=false}` синхронно). Значит для
Create/Update/Delete/lifecycle маппинг sentinel→code **обязан** жить там, где
формируется operation-error (use-case/worker) — перенести его в handler физически
нельзя, handler этой ошибки не видит. Sync-reads (Get/List) возвращают ошибку в
handler, но держать для них отдельный маппинг-слой в отрыве от async-пути = дубль и
рассинхрон кодов между sync/async ветками одного ресурса.

Паттерн зеркалит `kacho-vpc` (self-labelled «копия VPC») — cross-service
консистентность форматирования operation-error намеренная. Полный вынос
sentinel→code в отдельный слой (если когда-либо) — **workspace-wide** согласованная
правка vpc+compute, не compute-only (иначе рассинхрон между сервисами).

---

## Security-hardening audit r3 2026-07-05 (branch `sec-hardening-r3-2026-07-05`)

**Закрыто фиксами** (contract-safe, internal-only):

- **Disk-resize монотонность — TOCTOU устранён (DB-level CAS).** Инвариант «размер
  диска может только увеличиваться» раньше держался только software-проверкой
  `if req.Size < d.Size` на устаревшем снимке + безусловный `UPDATE disks SET size`.
  Две конкурентные grow-операции (→20 GiB и →15 GiB от 10 GiB) обе проходили
  проверку, last-writer-wins давал итог 15 (усадка уже подтверждённого роста;
  нарушение ban #10). Фикс: `DiskRepo.Update` при size в mask применяет
  `UPDATE … WHERE id=$1 AND size <= $new RETURNING …`; 0 строк → EXISTS-проба
  различает NotFound vs `FailedPrecondition "Disk size can only be increased"`.
  Software-проверка оставлена как fast-path (чёткий InvalidArgument для
  single-threaded усадки), но НЕ авторитетна — монотонность гарантирует CAS.
  Тест жил рядом с этим кодом; вместе с блочным хранением он уехал в `services/storage/`,
  поэтому имя файла здесь не воспроизводится — в compute его нет, и цитата вела бы в пустоту.

- **Referenced-resource lookups больше не маскируют транзиентные сбои под NotFound.**
  Existence-check ссылочных ресурсов той же БД на request-path (в compute сегодня это
  `internal/apps/kacho/api/instance/instance.go`; полосы блочного хранения уехали в
  `services/storage/` вместе со своими ресурсами) слепо маппил
  ЛЮБУЮ non-nil ошибку в `codes.NotFound` → обрыв соединения/deadline во время
  lookup выдавался клиенту как перманентный «<Resource> not found» (CWE-388, не
  retryable). Фикс: новый `mapRefErr(err, resource, id)` — настоящий `ErrNotFound`
  → NotFound с тем же текстом; всё остальное → `mapRepoErr` (Internal без leak'а).
  Тест: `maperr_ref_test.go`.

- **Newman async-negative false-green устранён.** Негативные кейсы async-мутаций
  (state-machine, boot-disk-detach, wrong-zone attach, size-decrease, relocate)
  проверяли отказ условно (`if (j.error) …`) → регрессия, при которой нелегальная
  операция начинает УСПЕШНО проходить, оставалась зелёной (нарушение ban #12/#13).
  Фикс: безусловный `pm.expect(Boolean(j.error)).to.eql(true)` + проверка кода
  (кейсы инстанса и диска; имена файлов не воспроизводятся — набор с тех пор переразложен,
  а диск уехал в свой сервис); `DISK-CR-NEG-ZONE-UNKNOWN` переведён с «tolerate 200 без
  assert» на детерминированный `poll → assert_op_error_oneof([3,5])` (zone-check в
  `doCreate`-worker — async, как project). Хелпер `assert_op_error_oneof` добавлен в
  `tests/newman/scripts/gen.py`; коллекции перегенерированы.

**Осознанно НЕ меняется** (by-design / вне scope internal security-правки):

- **`Instance.host_id` / `host_group_id` — убраны из публичного контракта
  (resolved).** CLAUDE.md hard-rule относит физический хост к internal-only.
  Публичный proto-контракт `computev1.Instance` был bump-нут (kacho-proto,
  PR `PRO-Robotech/kacho-compute#76`) — оба placement-поля из него **удалены**, а
  `protoconv.Instance` больше их не мапит. Доменные поля `domain.Instance.HostID`/
  `HostGroupID` и DB-колонки сохранены (internal-only, round-trip в
  `instance_repo.go`), но на публичную gRPC/REST-поверхность не проецируются — то
  есть двух-проекционное требование удовлетворено на уровне контракта (публичная
  проекция физически не несёт placement). Актуально держать так при появлении
  scheduler'а, который начнёт писать реальный `host_id`: населять только
  Internal-проекцию, не public.

- **Пустой project-scope не гейтит доступ (pre-AuthN scaffolding).**
  `TenantCtx.ProjectIDs` в решении о доступе не участвует вовсе — он кормит только
  `IsAnonymous` (AuthN-гейт production-режима) и admin-гейт; метода-предиката на нём
  нет. Это задокументированный back-compat pre-AuthN режим (зеркалит kacho-vpc). Fail-closed
  недостижим для non-admin в production: `TenantUnaryInterceptor` при
  `productionMode` отбивает `IsAnonymous` первым, authz-заголовки trust-gated
  (только от verified api-gateway cert), а authoritative гейт — per-RPC FGA Check.
  Перевод пустого scope в deny сломал бы dev-mode и разошёлся бы с vpc без
  замещающего AuthN-слоя. Ужесточение — вместе с внедрением IAM-токенов, не раньше.

- **Use-case DTO несут raw proto-типы; `InstanceService` — крупный god-struct;
  `mapRepoErr`/`invalidArg` дублируют kacho-vpc.** Пре-существующие структурные
  паттерны, намеренно зеркалящие kacho-vpc (cross-service консистентность).
  Разбиение на UseCase-per-RPC / CQRS-порты, вынос proto→domain конверсии в handler
  и подъём error-mapping в `kacho-corelib` — **workspace-wide** согласованная
  правка (vpc+compute одновременно, иначе рассинхрон), не compute-only security-фикс.

## Security-hardening audit r6 2026-07-05 (branch `sec-hardening-r6-2026-07-05`)

**Закрыто фиксами** (contract-safe / deploy / test-only):

- **Helm-чарт больше не пиннит insecure dev-posture.** `deploy/templates/deployment.yaml`
  + `values.yaml` получили шаблонизированные `KACHO_COMPUTE_AUTH_MODE` (default
  `production`), `KACHO_COMPUTE_DB_SSLMODE` (default `require`), `KACHO_COMPUTE_REQUIRE_IAM`
  (default `true`) и `KACHO_COMPUTE_AUTHZ_TRUSTED_FORWARDER_SANS` (default = api-gateway
  SPIFFE-id) + `mtls.enable=true` по умолчанию — чтобы fail-closed гейты бинаря
  (`validateAuthMode`/`requireDBSSLMode`/`requireTrustedForwarders`/`RequireIAM`)
  реально взводились. `DB_SSLMODE` прокинут и в migrate-initContainer. Dev-стенд
  переключается явным `--set auth.mode=dev --set mtls.enable=false --set db.sslMode=disable`.
  `helm template`/`helm lint` зелёные. **Propagation:** umbrella (`kacho-deploy`)
  `values.dev.yaml` должен добавить `compute.auth.mode=dev` + `db.sslMode=disable`
  при re-vendor'е чарта (иначе dev-стенд унаследует новый production-default и упадёт
  на `requireDBSSLMode`); `values.prod.yaml` — заменить `env.KACHO_COMPUTE_DB_SSLMODE`
  passthrough структурными ключами.
- **`wrapPgErr` домаппил SQLSTATE 23514 (CHECK) → InvalidArgument и 23P01 (EXCLUDE)
  → FailedPrecondition** (data-integrity.md таблица). Раньше оба падали в default
  `ErrInternal` → `codes.Internal`, из-за чего user-reachable CHECK/EXCLUDE выглядел
  бы транзиентным серверным сбоем. Фиксированный текст (без leak'а pg-detail).
  Тест: `internal/repo/unique_test.go` (RED→GREEN).
- **`InternalWatchService.Watch` streaming покрыт integration-тестами.**
  `internal/handler/internal_watch_stream_integration_test.go` (testcontainers):
  catchup-cursor через границу батча (250 > catchupBatchSize), `kinds`-фильтр,
  `from_sequence_no` resume, bad-payload fallback, ResourceExhausted-cap. Прод-код
  не тронут.
- **LEAN:** удалён неиспользуемый `authzfilter.BypassFilter` (bypass выражается
  nil-фильтром в `resolveListFilter` / `Config.Enabled=false`) и dead-аксессор
  `Decision.IsEmpty()` (handler'ы ветвятся по `len(IDs())`). Исправлен stale-коммент
  в `ports.go` (колонка называется `project_id` — переименована миграцией 0009).

**Осознанно НЕ меняется** (платформенная консистентность; первый пункт с тех пор
закрыт — оставлен как след, чтобы не «фиксили» по второму разу в обратную сторону):

- ~~**сервис-слой оперирует доредизайновым словарём и текстом `"Folder with id %s not
  found"`**~~ — **ЗАКРЫТО** (audit 2026-07). Обоснование «паритет с контрактом
  стороннего облака» само по себе нарушало core-правило #2 (проектируем в терминах
  Kachō, без оглядки на чужие облака), поэтому пункт снят, а не продлён. Ресурс
  называется **`Project`** (`proto/kacho/cloud/iam/v1/project.proto`), клиент шлёт
  `projectId` — прежнее имя не называет ничего на публичной поверхности, искать его в
  доке бесполезно. `internal/apps/kacho/api/instance/project_check.go` отдаёт конвенционные
  `NotFound "Project %s not found"` и `Unavailable "project check: upstream project
  service unavailable"` (api-conventions.md, тон `"<Resource> %s not found"`).
  Regression-lock — `internal/apps/kacho/api/instance/project_check_test.go` (текст + отсутствие прежнего слова
  в сообщении). **Код НЕ менялся**: `NOT_FOUND` остаётся `NOT_FOUND`; перевод этой
  peer-validate-линии на `FAILED_PRECONDITION` (api-conventions.md by-lane
  code-split) — отдельное ломающее решение под свой тикет.
- **`fqdn()` шьёт `.ru-central1.internal` в tenant-facing `Instance.fqdn`.** Остаток
  того же класса (ban #2), но **не** точечно исправимый: суффикс — часть
  DNS-адресации инстанса и совпадает с id зон, которые сидит **kacho-geo**
  (`ru-central1-{a,b,d}`). Смена суффикса в одиночку рассинхронизирует compute с
  geo-seed и с уже выданными клиентам FQDN. Закрывается координированной сменой
  зональной топологии (geo-seed → compute `fqdn()` → newman), не compute-only
  правкой. Того же класса legacy-нейминг, НЕ являющийся error-контрактом:
  Легаси-нейминг того же класса снят целиком: колонки/индексы переименованы
  миграцией 0009, newman-фикстуры перегенерированы, и `tools/legacyfolder` роняет
  сборку, если имя вернётся.
- **`InternalWatchHandler` (transport-слой) держит `*pgxpool.Pool` + raw DSN и сам
  делает pgx-`Connect`/`LISTEN`/raw SELECT по `compute_outbox`** — обход repo-порта
  (dependency-rule). Структурно идентичен `kacho-vpc/internal/handler/internal_watch_handler.go`
  (комментарий в файле это фиксирует). Вынос в `OutboxReader`-порт — согласованная
  vpc+compute правка (та же причина, что god-struct/CQRS выше), не compute-only.
  Streaming-логика теперь покрыта тестами (см. закрытое выше), так что рефактор не
  теряет регрессионную сеть.

## Security-hardening audit r7b 2026-07-06 (branch `sec-hardening-r7b-2026-07-06`)

**Закрыто фиксами** (contract-safe / internal-only / test-only):

- **`Instance.Delete` auto-delete disk-set больше НЕ снимается out-of-tx (stale
  snapshot).** Раньше use-case считал множество auto_delete-дисков из `in.AttachedDisks`
  снятого `repo.Get`-снимком ВНЕ delete-транзакции и передавал список в `repo.Delete`.
  Конкурентный `AttachDisk(auto_delete=true)`, закоммиченный между этим Get и
  delete-TX, оставлял orphan-диск: его строку `attached_disks` унёс бы CASCADE, а сам
  диск не удалялся (стейл-список его не содержал) — cross-tx read-modify-write
  (нарушение ban #10). Фикс: `InstanceRepo.Delete(ctx, id)` (сигнатура сузилась —
  список больше не принимается) сам вычисляет auto-delete множество ВНУТРИ своей TX
  из `DELETE FROM attached_disks … RETURNING disk_id, auto_delete`, предварительно
  взяв `SELECT … FOR UPDATE` на row инстанса (сериализует против FK KEY-SHARE lock'а,
  который берёт конкурентный AttachDisk-INSERT → новая привязка не проскочит в окно
  между sweep'ом и DELETE instance). Тест на эту гонку жил рядом с привязкой дисков и уехал
  вместе с ней в `services/storage/` (владелец таблицы привязок) — имя файла не
  воспроизводится, в compute его нет.
- **Per-object FGA List-фильтр нельзя молча выключить в production.** `validateAuthMode`
  (production И production-strict) теперь требует `requireListFilter`: `KACHO_COMPUTE_LIST_FILTER_ENABLED=true`
  И непустой `KACHO_COMPUTE_AUTHZ_IAM_GRPC_ADDR` (иначе `authzConn=nil` → `buildListFilter`
  вернёт nil → handler'ы bypass'ят фильтр). Раньше бинарь стартовал healthy с
  выключенным фильтром, и principal с project-tier `viewer` видел ВСЕ ресурсы проекта,
  которые сервис тогда обслуживал (over-show / BOLA-lite, CWE-862). Fail-closed зеркалит
  `requireDBSSLMode`/`requireTrustedForwarders`. Тесты: `authmode_gate_test.go`
  (`*_RequiresListFilter`, prod + strict).
- **Админ-CRUD справочника типов дисков покрыт testcontainers-тестом.** Раньше write-путь
  гонялся только через portmock; реальный SQLSTATE→sentinel (PK 23505 → AlreadyExists;
  RETURNING/RowsAffected 0-rows → NotFound) не проверялся end-to-end. Прод-код не тронут.
  Запись историческая: и сервис справочника, и его тест уехали в `services/storage/`
  вместе с блочным хранением, поэтому ни имя сервиса, ни имя файла здесь не цитируются.
- **`Instance.SimulateMaintenanceEvent` / `Instance.ListOperations` получили
  функциональные тесты** (portmock): empty-id → InvalidArgument, missing-id → op-error
  NotFound / sync NotFound, happy. Прод-код не тронут (`instance_ops_test.go`).
- **LEAN:** async-LRO dispatch-обвязка (`operations.New → opsRepo.Create → operations.Run
  → return &op`), скопированная в ~19 мутирующих RPC, сведена в один helper
  `service.runOp(ctx, opsRepo, desc, meta, worker)` (обобщение существовавшего
  `lifecycle`). `Instance.Restart` теперь делегирует в `lifecycle` (не переинлайнит
  обвязку). Мандатный async-Operation-паттерн (ban #9), wire-контракт (LRO envelope,
  metadata-типы, error-mapping, outbox-emit) — сохранены дословно; централизована
  только hand-copied обвязка. Изменение контракта диспетчеризации правится в одном месте.

**Осознанно НЕ меняется** (workspace-wide структурные решения / замороженный
контракт / нужен координированный контрактный тикет):

- **Anemic domain — bare-primitive поля, без self-validating newtypes/конструкторов.**
  `domain.Instance/Disk/Image/Snapshot` — плоские структуры string/int; вся
  invariant/format-валидация в service-слое через `corevalidate`. Формально это
  отступление от `evgeniy`-регламента (self-validating domain newtypes). Dependency
  rule НЕ нарушен (domain импортирует только stdlib + kacho-proto — разрешено
  `architecture.md`); это modelling/robustness-gap, а не layering-leak. Паттерн
  зеркалит **все** kacho-* сервисы (cross-service консистентность). Введение
  `domain.ZoneID`/`NewInstance(...)` — **workspace-wide** согласованная правка
  (vpc+compute+…), не compute-only security-фикс. (findings7 #7)
- **Fat resource-service (все RPC на одном `*InstanceService`) + не-CQRS repo-порты.**
  Уже задокументировано в r3-секции («InstanceService — крупный god-struct»);
  разбиение на UseCase-per-RPC + Reader/Writer split — workspace-wide. (findings7 #8)
- **`mapRepoErr`/`stripSentinel`/tenant-interceptor/JSONB-helpers/sentinel-set —
  byte-for-byte копии kacho-vpc, не вынесены в `kacho-corelib`.** Файлы сами это
  фиксируют («копия VPC» / «Зеркалит kacho-vpc»). Часть копий несёт security-фиксы
  (CWE-388 transient-mask, CWE-209 upstream-leak) → drift между копиями не ловится
  компилятором. Подъём generic-логики в corelib (`serviceerr`/`tenant`/`db`) —
  **workspace-wide** правка (vpc+compute одновременно, иначе рассинхрон), уже помечена
  в r2/r3-секциях как не compute-only. (findings7 #1)
- **Config через envconfig struct-tags, не YAML/viper/koanf.** `internal/config` читает
  плоские `KACHO_COMPUTE_*` env через `corecfg.LoadPrefixed → envconfig.Process`.
  `evgeniy` предписывает YAML-config через viper/koanf; позитивная половина регламента
  соблюдена (есть отдельный `cmd/migrator`). Механизм config — **shared corelib-решение
  для всех kacho-сервисов** (см. `09-go-skills-applied.md` → `golang-spf13-viper`
  skipped: «corelib/config — envconfig — покрывает»); миграция на koanf/viper —
  workspace-wide corelib-решение, не compute-only. (findings7 #9)
- ~~**`Instance.Metadata` принимается без размерного лимита**~~ — **ЗАКРЫТО, и
  прежнее обоснование было ЛОЖНЫМ.** Здесь стояло: «ограничивает только default 4 MB
  gRPC message ceiling». Потолок сообщения ограничивает ОДИН вызов, а правка
  СЛИВАЕТСЯ в уже накопленное (`metadata = (metadata - $del) || $upsert`), поэтому
  хранимое он не ограничивал вовсе: карта росла от вызова к вызову до потолка поля
  БД. То есть документ описывал границу, которая не ограничивает, — форма без
  содержания на уровне самого документа.
  Прежняя запись не касалась НИ СЛОВОМ и второго следствия: каждая правка навсегда
  кладёт весь выросший блоб ещё и в две служебные таблицы (`response_data` операции
  и `compute_outbox`), которые не подчищаются никогда, — при том что база у сервиса
  одна на всех тенантов. Это вопрос доступности и удержания, а не формулировки
  контракта, и откладывать его было нечем.
  Сейчас бюджет есть на обоих уровнях: дельта — синхронно в проверке запроса
  (`domain.ValidateInstanceMetadata`, отказ называет поле и не стоит строки
  операции), ИТОГ СЛИЯНИЯ — конструкцией базы (`instances_metadata_budget_check`,
  миграция 0025), потому что «прочитать → сложить → проверить → записать» оставляло
  бы окно между проверкой и записью. Значения: 64 ключа, ключ ≤128 байт, суммарно
  ≤256 КиБ — с запасом на реальное назначение поля и на три порядка ниже потолка
  поля БД. Замки: `instance_metadata_budget_test.go` (синхронный отказ),
  `instance_metadata_budget_integration_test.go` (накопление: восемь правок по
  четверти бюджета — без базы проходили все восемь). (findings7 #5, закрыто r5)

## Security-hardening audit r8b 2026-07-06 (branch `sec-hardening-r8b-2026-07-06`)

**Закрыто фиксами** (contract-safe / internal-only / test-only):

- **Публичная domain→proto проекция (`protoconv.Instance`) получила регрессионный
  security-guard.** Пакет `internal/protoconv` — единственная tenant-facing
  serialization-граница; commit #76 снял проекцию infra-sensitive `host_id`/
  `host_group_id` с публичного `computev1.Instance` (security.md: placement/host-инвентарь
  — Internal-only, defense-in-depth против lateral-movement reconnaissance), но
  контракт-фикс приземлился БЕЗ теста (нарушение ban #12). Добавлен
  `internal/protoconv/protoconv_test.go`: (a) `TestInstance_OmitsInfraSensitivePlacement` —
  выставляет `domain.Instance.HostID`/`HostGroupID` в уникальные sentinel'ы и
  проверяет, что сериализованное публичное сообщение (`protojson.Marshal`) их НЕ
  содержит нигде (robust против любого leak-пути — отдельное поле ИЛИ случайно
  затолканное в другое проецируемое поле; verified RED — временная проекция HostID
  краснит тест); (b) `TestInstance_ProjectsExpectedFields` — фиксирует, что легитимные
  публичные поля (id/zone/status/resources/boot-vs-secondary-disk split/NIC) round-trip'ят
  (регрессия-drop ловится); (c) `TestInstanceMessage_HasNoHostPlacementField` —
  descriptor-level guard: если будущий proto-bump вернёт `host_id`/`host_group_id` на
  публичное сообщение, тест краснеет, форсируя осознанное Internal-vs-public решение
  до того, как protoconv сможет их спроецировать. Прод-код не тронут. (findings8 #1)
- **LEAN: `ports.ZoneRegistry.GetZone` сужен до чистого existence-check
  (`(ZoneInfo, error)` → `error`).** Все три прод-call-site (`Disk.Create`,
  `Disk.Relocate`, `Instance.Create`) отбрасывали возвращаемый `ZoneInfo` и держали
  только `error`; `GeoClient` плёл `RegionID` из `geo.v1.Zone`, который никто не читал
  (speculative generality / YAGNI, ban #11). Удалены `ports.ZoneInfo` + alias
  `service.ZoneInfo`; `GeoClient`/`NoopGeoClient`/`portmock.ZoneRegistry` перестали
  протаскивать discarded `RegionID` (portmock `data` стал set `map[string]struct{}`).
  Поведение (existence-check zone_id, fail-closed на NotFound/Unavailable через
  `mapZoneRefErr`) и wire-контракт (proto/REST/DB) — byte-for-byte идентичны; три
  call-site уже игнорировали значение. Тесты `geo_client_test.go` сужены под новую
  сигнатуру. (findings8 #9)
- **Экспортированный тест-хелпер `config.LoadInto` убран из прод-файла
  `internal/config/config.go`.** Он был `exported` (попадал в прод-API kacho-compute),
  мутировал process-global env (`os.Setenv`/`Unsetenv`) с discarded-ошибками и best-effort
  restore — приглашение к misuse из реального startup/reload-кода. Заменён на
  `t.Setenv`-обёртку `loadCfg(t, env)` в `_test.go` (auto-restore на `t.Cleanup`, паника
  при parallel-misuse, без discarded-ошибок): `internal/config/helpers_test.go` (14
  call-site config-пакета) и локальный хелпер в `cmd/compute/dialpeer_mtls_iam_test.go`
  (2 call-site). Прод-бинарь больше не несёт хелпер; env-имена и loader-путь
  (`corecfg.LoadPrefixed`) — без изменений. Прод-поведение не тронуто. (findings8 #8)

**Осознанно НЕ меняется** (workspace-wide структурные решения / clean-arch-легитимно):

- **`runServe` composition-root ~354 строки (`cmd/compute/main.go`).** Clean-arch
  (`architecture.md`) назначает `cmd/<svc>/main.go` **единственным** местом wiring
  (composition root), что легитимизирует длинную последовательность error-checked
  setup-блоков. Когезивные под-шаги уже вынесены в именованные хелперы
  (`startRegisterDrainer`/`buildListFilter`/`dialPeers`/`buildServices`); дальнейшее
  дробление на `setupListeners`/`setupBackgroundTasks` — не contract-safe minimal edit,
  а рефактор с реальным риском переупорядочить `defer pool.Close()`/`cancel()` (сам
  finding это отмечает как опасность). Держим как accepted clean-arch-паттерн; при
  добавлении нового peer/фонового таска — вносить wiring в существующие фазы. (findings8 #7)
- **Fat resource-service, non-CQRS repo-порты, anemic domain, envconfig-config,
  corelib-копии helper'ов** — все пять (findings8 #2–#6) уже задокументированы выше как
  workspace-wide решения: см. секцию **r7b** (findings7 #1/#7/#8/#9) и **r3**
  («InstanceService — god-struct», «копии VPC»). Это не compute-only фиксы: разбиение
  на UseCase-per-RPC + Reader/Writer split, self-validating newtypes, viper/koanf-config
  и подъём generic-helper'ов в `kacho-corelib` — координированные правки по всем kacho-*
  сервисам сразу (иначе cross-service рассинхрон). Здесь — только указатель, без
  дублирования. (findings8 #2–#6)

## Security-hardening audit r9b 2026-07-06 (branch `sec-hardening-r9b-2026-07-06`)

**Закрыто фиксами** (contract-safe / internal-only / test-only):

- **`breakglass=true` в production больше НЕ молчалив — громкий boot-WARN.**
  `KACHO_COMPUTE_AUTHZ_BREAKGLASS=true` целиком обходит per-RPC
  `InternalIAMService.Check` на обоих листенерах (object-self `v_get`/`v_update`/
  `v_delete` + cross-tenant Check не оцениваются — остаётся только AuthN). Раньше
  `validateAuthMode` в production/production-strict пропускала breakglass **без
  единого сигнала**; leftover-breakglass после инцидента проходил незамеченным
  (finding r9b#1: «gate silently disables all authz», CWE-862). Тогда решением стал
  громкий WARN.
  > **СУПЕРСЕДНУТО (production-readiness P0-5):** WARN заменён на **hard-reject** —
  > `validateAuthMode` в production/production-strict ОТКАЗЫВАЕТ старту. См. запись
  > «breakglass в production — hard-reject (супе­рседит warn-not-reject)» ниже.
- **`FuzzInstanceSpecValidate` больше не гоняет length-stub, а реальный
  validation-путь.** Прежний таргет вызывал `validateInstanceSpecStub` (только
  `len(s)>0 && ≤256KB`), пакет `internal/fuzz` не импортировал ни строки прод-кода —
  hollow-coverage (finding r9b#2, ban #13). Теперь fuzz-body прогоняет тот же путь,
  что RPC: `protojson → computev1.CreateInstanceRequest → handler.CreateReqFromProto →
  service.ValidateCreateInstanceReq`, ассертит no-panic и стабильный `InvalidArgument`
  на reject. Для этого выделены две чистые (behaviour-preserving) функции из inline-блоков:
  `service.ValidateCreateInstanceReq` (синхронная pre-flight валидация Create) и
  `handler.CreateReqFromProto` (proto→use-case конвертация); `Create`-методы теперь их
  вызывают. 12s fuzz-burst: 137 new-interesting, 0 crashers.
- **ban #2 (чужие облака) — имя провайдера вычищено из architecture-докладов.**
  `00-overview.md` (контракт-позиционирование, package-note, секция «Стабильность
  внешнего контракта»), `ARCHITECTURE.md`, `docs/architecture/README.md`,
  `07-known-divergences.md` (шапка) переписаны в own-product терминах (замороженный
  контракт продукта в `kacho.cloud.compute.v1`), без имени/хостнейма чужого облака
  (finding r9b#6). Rule-statement'ы в `06-conventions.md` (сам запрет) прежде
  сохраняли имя провайдера «осознанно, они формулируют ban» — это отменено:
  формулировка запрета не обязана его нарушать, а сам запрет теперь энфорсится
  гейтом `tools/foreignclouds`, который и держит список имён.
- **LEAN: comment-drift `«LRU eviction»` исправлен.** `authzfilter.Config.CacheMaxEntries`
  / `config.ListFilterCacheMaxEntries` обещали «LRU eviction», а `putCache` сбрасывает
  **произвольную** запись (TTL — первичный механизм). Комментарии приведены в
  соответствие с кодом (finding r9b#7, часть). Поведение не тронуто.

**Осознанно НЕ меняется** (workspace-wide / by-design):

- ~~**breakglass в production — warn-not-reject (не hard-reject, как предлагал finding).**~~
  **ОТМЕНЕНО.** Прежнее обоснование опиралось на то, что «канонический mirror `kacho-vpc`
  тоже warn-not-reject». Это оказалось не каноном, а **расхождением**: `kacho-geo` и
  `kacho-nlb` breakglass в production **отвергали**, а `kacho-registry` не гейтил его
  вовсе. Одна и та же настройка означала «сервис поднят вообще без авторизации» в одних
  сервисах и «сервис не поднимется» в других.
  WARN не защищает: leftover breakglass после инцидента переживает рестарт и оставляет
  ОБА листенера без per-RPC Check — прямое нарушение `security.md` «AuthN+AuthZ ВЕЗДЕ» и
  core-правила «production-mode boot-guard fail-closed → refuse-to-start».
  Выровнено по самому строгому: **hard-reject** в compute, vpc и registry (geo/nlb уже
  отвергали). Аварийный механизм не убран — он остаётся в `dev`, где и задуман
  (`TestValidateAuthMode_Dev_BreakglassAllowed`). Тесты: `authmode_gate_test.go`
  (`*_Production_BreakglassRefusesBoot` prod+strict — assert'ит СООБЩЕНИЕ, не только факт
  ошибки; `*_ProductionStrict_BreakglassRefusesBoot`; `*_Dev_BreakglassAllowed`).
- **`internal/authzfilter.FGAFilter` — per-service hand-rolled per-object фильтр,
  не `kacho-corelib/authz.ListObjectsService`.** Тот же класс, что «corelib-копии
  helper'ов» (findings7 #1 / r7b): подъём в corelib — координированная workspace-wide
  правка (vpc несёт идентичный двойник), не compute-only. **Corelib-примитив здесь
  вообще неприменим**: он строит видимость перечислением
  (`AuthorizeService.ListObjects`), а перечисление — сам источник дефекта. OpenFGA
  капит ListObjects server-side (`OPENFGA_LIST_OBJECTS_MAX_RESULTS`, default 1000) и
  **не отдаёт continuation-token**, поэтому pagination-loop corelib'а обрывается на
  первой странице, а `max_results=10000` — лишь client-side trim уже усечённого
  ответа: на сторе с >1000 объектов типа собственный ресурс тенанта выпадал из выдачи
  (List пуст, `Image.GetLatestByFamily` → NotFound) при живой строке и живом гранте.
  compute перешёл на ПРЯМОЙ per-object вопрос по прочитанной странице
  (`AuthorizeService.BatchCheck`, ≤100/батч, `viewer ∪ v_list` — тот же предикат).
  Консолидация per-object фильтра в corelib — cross-repo тикет. (finding r9b#7)
- **envconfig-config (#3), non-CQRS repo-порты (#4), anemic domain (#5)** — уже
  задокументированы как workspace-wide решения в секциях **r7b** (findings7 #9/#8/#7)
  и **r8b**. Здесь — только указатель. (findings r9b #3/#4/#5)

## 12. `List`-фильтр остаётся `name=`, хотя acceptance F14 обещал больше

**Решение:** whitelist `filter` в **единственном** List-репозитории compute, у которого
`filter` есть (`instance_repo.go`, `filter.Parse(f.Filter, []string{"name"})`) — **`name=`
и только он**. Прежняя редакция называла «все четыре репозитория
(`instance`/`disk`/`image`/`snapshot`)»: три из четырёх ушли к kacho-storage вместе с
дублем блочного хранения, и утверждение осталось про предмет, которого в дереве нет —
исключение обязано истекать вместе со своим предметом, иначе его унаследует следующая
слепая зона.

Прежняя редакция
COMP-1 F14 / COMP-1-36 заявляла ещё `placementGroupId=` и `instanceKind=`; расхождение
сведено **в пользу кода**, acceptance-док приведён к реализации (§Reconcile F14
filter-whitelist в `docs/specs/sub-phase-COMP-1-instance-machinetype-acceptance.md`).
Совпадает с нормативным `api-conventions.md` §pagination/filter («текущая фаза — `name=`»).

**Почему не расширили** (проверено на живой Postgres с применёнными миграциями, а не
рассуждением):

1. Заявленное **написание** нереализуемо. `pkg/filter.Parse` подставляет имя поля в SQL
   дословно (`FilterAST.ToSQL`), колонки — snake_case. `… AND instanceKind = $1` →
   `SQLSTATE 42703 column "instancekind" does not exist`; то же для `placementGroupId`.
   camelCase из дока дал бы `INTERNAL`, а не отфильтрованную страницу.
2. `instanceKind` не фильтруется строкой **в принципе**: `instances.instance_kind` —
   `INTEGER` (ordinal enum, миграция 0016), парсер производит только строковое значение.
   `… AND instance_kind = 'CONTAINER'` → `SQLSTATE 22P02 invalid input syntax for type
   integer`. Нужен enum-декодер в **общем** `pkg/filter` — кросс-сервисное изменение.
3. **Индекса нет, и завести его сейчас нечем оправдать.** Поле фильтра без индекса
   превращает `List` в полное сканирование под нагрузкой. `instance_kind` — ≤3 значения
   (нулевая селективность); `placement_group_id` — `DEFAULT ''` практически на всех
   строках. Существующие индексы `instances` — `(project_id)`, `(created_at)`, `(zone_id)`,
   `(machine_type_id)` + partial `UNIQUE(project_id,name) WHERE name<>''`; ни один не
   покрывает новые предикаты.
4. `placementGroupId` в COMP-1 — **opaque passthrough** (OQ4): без existence/coherence, а
   сам `PlacementGroup` появляется в **COMP-3**. Фильтр по нему осмыслен одновременно с
   ресурсом — там же он и заводится, вместе со **своим** partial-индексом
   `(project_id, placement_group_id, created_at, id) WHERE placement_group_id <> ''`.

Из трёх спорных полей технически реализуемо **одно** — `placement_group_id` (TEXT,
запрос отработал в замере). Оно отложено не по невозможности, а по п.3-4: индекс сейчас
был бы стоимостью записи без выигрыша чтения.

**Что вместо этого залочено (наблюдаемо, а не на честном слове):** любое не-`name` поле
фильтра отвергается **явно** — `INVALID_ARGUMENT "Bad expression at column 1. Unknown
field: \"<field>\""`, и **никогда** не игнорируется молча. Молчаливое игнорирование было
бы хуже отказа: caller получил бы нефильтрованную страницу под фильтром, который считает
применённым. Тесты: `internal/repo/list_filter_whitelist_test.go` (4 репозитория × 6
полей, assert кода **и** сообщения; проверен инъекцией — расширение whitelist'а красит
его) + newman `INST-RD-LST-FILTER-UNKNOWN-FIELD-REJECTED` (строгий 400, не `oneOf`).

## 13. Эшелон сетевой политики у compute отсутствует — и не заменяет авторизацию

**Предмет.** Комментарий композиционного корня описывал сетевую политику как
действующее ограничение internal-листенера. Такого шаблона у compute нет, и
утверждение было ложным. Комментарий приведён к правде (`cmd/compute/main.go`,
`registerInternalServices`).

**Почему это записано, а не «починено попутно».** Сетевая политика — эшелонирование,
а не авторизация, и заменить её собой не может: подтверждённый пир внутреннего CA —
по-прежнему пир, тенантных границ транспорт не различает. Ложная ссылка на неё была
опасна именно тем, что делала пробел в авторизации похожим на осознанный размен
(«доступ всё равно ограничен сетью»). Поэтому авторизация закрыта там, где она и
должна быть, — в самом сервисе, на уровне данных; см. §«поток журнала изменений» в
комментарии карты прав и `internal/handler/internal_watch_handler.go`.

**Следствие для контракта.** Отсечение тенантского REST от `Internal*`-поверхности
обеспечивает allow-list шлюза, а не сеть. Ни один из контролей сервиса на сетевую
политику не рассчитывает и её появление ничего не отменит.

**Что осталось.** Завести шаблон и включить его в боевом профиле — добавить эшелон.
Это отдельная работа по чарту, не условие корректности авторизации.

Образец брать у **kacho-nlb** (`services/nlb/deploy/templates/networkpolicy.yaml`) —
он живёт в чарте самого сервиса и включён по умолчанию, то есть ближе к желаемому
результату, чем umbrella-шаблон vpc, который в отладочном профиле выключен. Уточнение
переписи: шаблонов политики в дереве **четыре** (три в umbrella + свой у nlb), и
сервисов с политикой на internal-листенере **два** (vpc и nlb), а не один — первая
редакция этой записи и сообщение коммита `c4392ed1` считали их «на глаз» и ошиблись.
Ровно та же ошибка, из-за которой запись и появилась.

## 14. Событие удаления недоставляемо после снятия регистрации объекта — открытый продуктовый вопрос

**Механика.** `Instance.Delete` удаляет строку и в ТОЙ ЖЕ транзакции эмитит и запись
журнала `DELETED`, и намерение снять регистрацию объекта в модели прав
(`internal/repo/instance_repo.go`). После применения намерения дренажом ни один
субъект — включая того, кто удаление и выполнил, — не может иметь прав на объект,
которого больше нет. Поэтому per-row вопрос по записи `DELETED` отвечает «нет», и
запись не доставляется. Полезная нагрузка `DELETED` — `{"id": …}`, проекта не несёт,
то есть запасного предиката нет.

**Почему не «починено» вместе с авторизацией потока.** Сделать удаления видимыми —
значит решить, ЧЬИ удаления вправе видеть вызывающий. Единственный доступный ответ —
родительский проект, то есть выдать носителю проектного viewer'а знание об объектах,
которые он индивидуально видеть права не имел. Это оракул существования и расширение
доступа; такое решение — продуктовое, про видимость событий удаления, а не деталь
фикса авторизации. Догадка здесь была бы хуже названной проблемы.

**Чего делать НЕЛЬЗЯ:** доставлять `DELETED` в обход сужения. Это ровно та утечка,
ради закрытия которой сужение и введено, — и она вернула бы весь журнал целиком, а не
только удаления.

**Побочная неприятность.** До применения намерения ответ модели может быть «да»
(плюс положительный кэш фильтра), поэтому в окне до дренажа поведение зависит от
момента. Зафиксировано УСТОЯВШЕЕСЯ состояние — то, в которое приходит реальный стенд.

**Заблокировано наблюдаемым тестом**
(`TestIntegration_Watch_DeletionEventIsNotDeliverableOnceTheObjectIsGone`), причём
тест требует, чтобы запись была именно ОТВЕРГНУТА моделью, а не отброшена до вопроса:
иначе будущему решению о видимости удалений негде было бы вступить в силу.

**Практическое влияние сегодня — нулевое:** подписчиков у потока нет, UI и CLI его не
используют (`08-ui.md`, `02-data-flows.md`). Вопрос ставится до появления первого
потребителя, а не после.
