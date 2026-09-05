// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package ports содержит port-интерфейсы (Clean Architecture boundaries) и
// связанные value-объекты (Pagination, *Filter) для kacho-compute.
//
// Это leaf-пакет: импортирует только `internal/domain`. Импортируется
// use-case-пакетами `internal/apps/kacho/api/<resource>` (ре-экспортируют через
// type-alias'ы), `internal/repo` / `internal/clients` (adapters реализуют эти
// интерфейсы) и `internal/ports/portmock` (общие fake'и для unit-тестов). Так
// избегается дублирование mock-реализаций и не создаётся import-cycle.
package ports

import (
	"context"
	"time"

	"github.com/PRO-Robotech/kacho/pkg/ownerregister"
	"github.com/PRO-Robotech/kacho/services/compute/internal/domain"
)

// Pagination — постраничная навигация.
type Pagination struct {
	PageToken string
	PageSize  int64
}

// InstanceFilter — фильтр для списка ВМ.
//
// Здесь НЕТ authz-измерения (прежнего `AllowedIDs`): видимость решается
// per-object ПОСЛЕ чтения страницы (`handler.filterVisible` →
// iam.AuthorizeService.BatchCheck), а не сужением SQL до заранее перечисленного
// allow-list'а. Перечисление упиралось в жёсткий предел прежнего движка прав
// (1000 объектов, без продолжения) и делало собственные ресурсы тенанта
// невидимыми. Движка нет, а форма вопроса остаётся: спрашиваем про страницу, а
// не про вселенную — см. package-doc `internal/authzfilter`.
type InstanceFilter struct {
	ProjectID string
	// Filter — raw filter expression (синтаксис Kachō: `name="<value>"`).
	Filter string
}

// OwnerRegistrar — синхронная post-commit регистрация owner-tuple в kaname
// (`InternalIAMService.RegisterResource`), зеркалящая тот же register-intent, что
// эмитится транзакционно в `compute_fga_register_outbox` — и НЕСЁТ ТУ ЖЕ ВЕРСИЮ,
// которой БД проштамповала эту строку внутри writer-транзакции (её возвращает
// repo.Insert/Update). Форма порта общая для всех сервисов
// ([ownerregister.Registration]): своей у compute не было — была копия,
// разошедшаяся с соседями. Делает owner-tuple
// эффективным раньше (до того как async register-drainer опросит outbox), сужая
// eventual-consistency-окно — чистая window-оптимизация. Best-effort: ошибка
// логируется, Create НЕ проваливается — durable outbox-intent + register-drainer
// остаются at-least-once backstop'ом. nil = не сконфигурирован (dev / нет
// iam-ребра) → полагаемся на drainer.
type OwnerRegistrar interface {
	Register(ctx context.Context, regs []ownerregister.Registration) error
}

// InstanceRepo — port-интерфейс репозитория ВМ.
type InstanceRepo interface {
	Get(ctx context.Context, id string) (*domain.Instance, error)
	List(ctx context.Context, f InstanceFilter, p Pagination) ([]*domain.Instance, string, error)
	// Insert вставляет строку ВМ (+ outbox CREATED + FGA register-intent) в одной
	// writer-tx. compute больше НЕ держит local attach-state (storage-split
	// cutover): том↔Instance-привязка живёт в kacho-storage, boot_volume/
	// secondary_volumes — read-only зеркало, пересчитываемое на чтении. Возвращает
	// созданную ВМ (без attached-строк — их нет).
	Insert(ctx context.Context, in *domain.Instance) (*domain.Instance, []ownerregister.Registration, error)
	// Update обновляет mutable descriptive/resource поля (status НЕ трогает —
	// им владеет SetStatusCAS). emitLabelsRegister: true когда "labels" присутствует
	// в update-mask (или full-object PATCH применяет labels) → repo эмитит свежий FGA
	// register-intent с обновлёнными labels в той же writer-tx (refresh IAM
	// resource_mirror); false → register-intent НЕ эмитится.
	//
	// changed — фактически изменённые mask-поля; repo пишет ТОЛЬКО их колонки
	// (column-scoped UPDATE). Без scoping конкурентный Update по другому полю
	// затирается значением из устаревшего Get-снимка (lost update) — read-modify-write
	// вне одной TX. Пустой changed → no-op reload (behaviour-preserving).
	Update(ctx context.Context, in *domain.Instance, emitLabelsRegister bool, changed []string) (*domain.Instance, []ownerregister.Registration, error)
	// SetStatusCAS атомарно переводит instance из expected-status в next-status
	// (CAS на DB-уровне: conditional UPDATE WHERE id=$1 AND status=$expected).
	// Если row не существует → ErrNotFound; если status не совпадает с
	// expected → ErrFailedPrecondition (state transition not allowed). Возвращает
	// обновлённую ВМ (+ outbox UPDATED в той же TX). Within-service-инвариант на
	// DB-уровне (CAS), не software check-then-act — защита от second-writer-wins.
	SetStatusCAS(ctx context.Context, id string, expected, next domain.InstanceStatus) (*domain.Instance, error)
	// GateForAttach — ОДНОСТЕЙТМЕНТНАЯ ПРЕДПРОВЕРКА disk/NIC-attach саги, НЕ
	// compare-and-swap: она ничего не пишет и не держит строку. Возвращает
	// self-describing payload (zone_id, project_id, name) для форварда в storage/vpc,
	// если инстанс в {RUNNING, STOPPED}; не в допустимом состоянии →
	// ErrFailedPrecondition ("Instance must be RUNNING or STOPPED"); отсутствует →
	// ErrNotFound. Существование и состояние решаются одним стейтментом (один снимок),
	// поэтому полоса ошибки не может назвать не то, что произошло.
	//
	// Гонку attach-vs-delete она СУЖАЕТ, но НЕ ЗАКРЫВАЕТ: после возврата конкурентный
	// Delete успевает поставить DELETING и отпустить привязки, пока форвард ещё в пути.
	// Закрытие требует сериализации, которой у предпроверки нет (счётчик in-flight на
	// строке либо advisory-lock, удерживаемый обеими сагами) — это отдельная работа с
	// db-/system-design-ревью; до неё остаток закрывается компенсацией инициатора и
	// sweeper'ом владельца привязки. Реализация: instance_repo.go GateForAttach.
	GateForAttach(ctx context.Context, id string) (zoneID, projectID, name string, err error)
	// MarkDeleting атомарно переводит инстанс в DELETING (идемпотентно: повтор на
	// уже-DELETING инстансе — no-op OK). Возвращает инстанс (для self-describing
	// release-payload delete-саги). Отсутствует → ErrNotFound. Ставится ПЕРЕД
	// release'ом привязок, чтобы конкурентный AttachDisk-гейт видел DELETING и падал.
	MarkDeleting(ctx context.Context, id string) (*domain.Instance, error)
	// Delete удаляет строку ВМ (+ outbox DELETED + FGA unregister-intent) в одной
	// writer-tx. Это ФИНАЛЬНЫЙ шаг delete-саги: том/NIC-привязки уже сняты через
	// storage.Detach/vpc.Detach в use-case ДО этого вызова (compute local
	// attach-state больше нет; ПОРЯДОК — строка инстанса удаляется ПОСЛЕДНЕЙ, чтобы
	// crash не осиротил привязки). Отсутствует → ErrNotFound.
	Delete(ctx context.Context, id string) error
	// ListStuckDeleting возвращает id машин, вошедших в удаление раньше чем
	// olderThan назад и там оставшихся.
	//
	// Порядок delete-саги делает крах безопасным, но повторить её было некому:
	// разрешитель осиротевших операций по контракту рабочую функцию не
	// перезапускает. Без этой выборки машина остаётся в DELETING навсегда, а её
	// интерфейсы и тома — занятыми у владельцев, которые снятия не запрашивают.
	//
	// olderThan — отсрочка: свежее удаление прямо сейчас доделывает законный
	// исполнитель, и подхватывать его нельзя, иначе оба снимают одни и те же
	// привязки наперегонки.
	ListStuckDeleting(ctx context.Context, olderThan time.Duration) ([]string, error)
	// TryClaimStuckDeleteSweep берёт проход добивателя на себя — ровно одна
	// реплика из всех, подошедших к проходу одновременно.
	//
	// Возвращает (release, true, nil), если проход наш; (nil, false, nil), если
	// его уже исполняет другая реплика. Проигрыш — ШТАТНЫЙ исход, а не отказ:
	// проигравший пропускает тик и приходит на следующем.
	//
	// Тип замыкания — голая функция, а не тип из адаптера: порт живёт в слое
	// use-case, и втянуть сюда драйвер базы значило бы протащить адаптер через
	// весь граф импортов.
	TryClaimStuckDeleteSweep(ctx context.Context) (release func(context.Context), ok bool, err error)
}

// MachineTypeFilter — фильтр для списка machine-type (COMP-1 F7/F19). Ambient
// cluster-scoped каталог: per-object list-authz к нему не применяется вовсе
// (read ambient, project-scope EXEMPT) — handler передаёт nil-фильтр, в отличие от
// Disk/Image/Snapshot/Instance. Whitelist: name=/family=/minGpus=.
type MachineTypeFilter struct {
	// Name — точное имя (напр. "std-v3-2"). Пусто → без фильтра.
	Name string
	// Family — класс; MachineTypeFamilyUnspecified (0) → без фильтра.
	Family domain.MachineTypeFamily
	// MinGPUs — минимум GPU (напр. 4); 0 → без фильтра.
	MinGPUs int32
}

// MachineTypeUpdate — резолвнутый набор изменений одной правки каталога.
// nil-поле означает «колонку не трогать»; LabelsSet отличает «метки названы
// маской» от «не названы» (пустая карта — законное значение).
//
// Тип существует, чтобы в БД уезжали ТОЛЬКО названные маской колонки. Раньше
// правка читала строку, сливала маску в памяти и писала ВЕСЬ снимок обратно:
// две правки с непересекающимися масками затирали друг друга, обе рапортовали
// успех, и потеря была молчаливой.
type MachineTypeUpdate struct {
	Description        *string
	Family             *domain.MachineTypeFamily
	EffectiveResources *domain.EffectiveResources
	AvailableZones     *[]string
	Status             *domain.MachineTypeStatus
	Labels             map[string]string
	LabelsSet          bool
}

// Touched сообщает, называет ли правка хоть одну колонку. Правка, не называющая
// ничего, — не ошибка вызывающего, но и записи не требует.
func (u MachineTypeUpdate) Touched() bool {
	return u.Description != nil || u.Family != nil || u.EffectiveResources != nil ||
		u.AvailableZones != nil || u.Status != nil || u.LabelsSet
}

// MachineTypeRepo — port-интерфейс каталога machine-type (read + admin CRUD).
// Ambient cluster-каталог: List не фильтруется per-object. Insert/Update/Delete —
// admin-only (InternalMachineTypeService).
//
// Update принимает id + НАБОР ИЗМЕНЕНИЙ, а не собранный domain-объект: инвариант
// «правка не трогает то, чего не называла» выражается одним UPDATE по названным
// колонкам, а не последовательностью «прочитал → слил → записал всё».
type MachineTypeRepo interface {
	Get(ctx context.Context, id string) (*domain.MachineType, error)
	List(ctx context.Context, f MachineTypeFilter, p Pagination) ([]*domain.MachineType, string, error)
	Insert(ctx context.Context, mt *domain.MachineType) (*domain.MachineType, error)
	// Update применяет названные маской колонки одним стейтментом и возвращает
	// финальную строку. 0 строк → ErrNotFound.
	Update(ctx context.Context, id string, u MachineTypeUpdate) (*domain.MachineType, error)
	Delete(ctx context.Context, id string) error
}

// PlacementGroupUpdate — резолвнутый набор изменений одной правки группы.
//
// Стратегии и якоря размещения здесь НЕТ, и это не пропуск: смена любого из них
// поменяла бы смысл размещения уже стоящих машин, а перекладывать их задним
// числом мы не будем. Нужна другая стратегия — заводится другая группа.
type PlacementGroupUpdate struct {
	Name        *string
	Description *string
	Labels      map[string]string
	LabelsSet   bool
}

// Touched сообщает, называет ли правка хоть одну колонку.
func (u PlacementGroupUpdate) Touched() bool {
	return u.Name != nil || u.Description != nil || u.LabelsSet
}

// GuestAccessKeyUpdate — резолвнутый набор изменений одной правки ключа.
// nil-поле означает «колонку не трогать»; LabelsSet отличает «метки названы
// маской» от «не названы» (пустая карта — законное значение).
//
// Материала ключа и отпечатка здесь НЕТ, и это не пропуск: подменить материал
// значило бы сменить того, кто может войти, не сменив ни идентификатора, ни
// ссылок на него с машин. Смена материала выражается парой «завести новый,
// снять старый», и каждая половина этой пары видна в журнале.
type GuestAccessKeyUpdate struct {
	Name      *string
	Labels    map[string]string
	LabelsSet bool
}

// Touched сообщает, называет ли правка хоть одну колонку.
func (u GuestAccessKeyUpdate) Touched() bool { return u.Name != nil || u.LabelsSet }

// ProjectClient — port для проверки существования Project в kaname
// (ProjectService.Get). Аргумент projectID — id владельца-проекта; в схеме
// kacho-compute он лежит в колонке `project_id`.
type ProjectClient interface {
	Exists(ctx context.Context, projectID string) (bool, error)
}

// ProjectAccountClient — тот же клиент проекта, умеющий вдобавок назвать аккаунт.
//
// Отдельный, более широкий порт, а не расширение `ProjectClient`: существование
// проекта спрашивают все пути мутации, а аккаунт — только материализация строк
// учёта квоты. Расширив узкий порт, пришлось бы учить `AccountOf` каждую
// подставную реализацию в пробах, где предмет проверки другой, — и `AccountOf`
// появился бы там заглушкой, которая ничего не значит.
type ProjectAccountClient interface {
	ProjectClient
	AccountOf(ctx context.Context, projectID string) (string, error)
}

// NicAttachSpec — self-describing NIC-attach payload for compute→kacho-vpc
// InternalNetworkInterfaceService.Attach. compute forwards the instance's
// zone/name/project so kacho-vpc can validate zone-coherence (anycast/REGIONAL
// subnet excepted) against its OWN network_interfaces + subnets rows — kacho-vpc
// never calls compute back (acyclic edge; the NIC binding lives on the vpc-side
// row, compute holds no local attach-state).
type NicAttachSpec struct {
	NICID          string
	InstanceID     string
	InstanceName   string
	InstanceZoneID string
	ProjectID      string
	// Index — requested slot (eth0=0, eth1=1, …). 0 lets kacho-vpc assign the first
	// free slot atomically.
	Index int32
}

// NicAttachment — a single NIC↔Instance binding enriched with the instance-local
// slot index + a denormalised mirror of the NIC's addressing (source of truth =
// kacho-vpc NetworkInterface). Output-only on the compute side; used to build the
// read-only Instance.network_interfaces[] mirror on Get/List.
type NicAttachment struct {
	NICID            string
	InstanceID       string
	Index            int32
	SubnetID         string
	PrimaryV4Address string
	PrimaryV6Address string
	SecurityGroupIDs []string
	MACAddress       string
}

// NicClient — port for compute→kacho-vpc InternalNetworkInterfaceService (NIC↔
// Instance attach coordination, internal :9091 mTLS). kacho-vpc owns the binding
// and enforces the atomic used_by_id CAS + zone-coherence; compute only forwards a
// self-describing payload and mirrors the result. Peer unavailable → fail-closed
// (Unavailable) on the attach/detach mutations. ListByInstance is a best-effort
// batched read for the Get/List mirror (graceful-degrade — the mirror is omitted
// when kacho-vpc is unreachable, the Instance read itself never fails).
type NicClient interface {
	Attach(ctx context.Context, spec NicAttachSpec) (*NicAttachment, error)
	Detach(ctx context.Context, nicID, instanceID string) error
	ListByInstance(ctx context.Context, instanceIDs []string) ([]NicAttachment, error)
}

// VolumeAttachMode — access mode of a volume attachment. Neutral value type
// (ports imports only domain, never a grpc-stub) mirroring the wire enum
// storage.v1.VolumeAttachment.Mode by ordinal, so the clients-adapter maps it
// trivially. UNSPECIFIED(0) → storage defaults to READ_WRITE.
type VolumeAttachMode int32

const (
	// VolumeAttachModeUnspecified — mode not set (storage treats as READ_WRITE).
	VolumeAttachModeUnspecified VolumeAttachMode = 0
	// VolumeAttachModeReadWrite — read/write attachment.
	VolumeAttachModeReadWrite VolumeAttachMode = 1
	// VolumeAttachModeReadOnly — read-only attachment.
	VolumeAttachModeReadOnly VolumeAttachMode = 2
)

// VolumeAttachSpec — self-describing volume-attach payload for compute→kacho-storage
// InternalVolumeService.Attach. compute forwards the instance's zone/name/project +
// the requested attach parameters so kacho-storage can validate zone/project
// coherence and perform the atomic attach-CAS against its OWN `volumes` /
// `volume_attachments` rows — kacho-storage never calls compute back (acyclic edge;
// the attach-state lives on the storage-side row, compute holds no local attach-state).
type VolumeAttachSpec struct {
	VolumeID       string
	InstanceID     string
	InstanceName   string
	InstanceZoneID string
	ProjectID      string
	// DeviceName — guest device name, unique within the instance.
	DeviceName string
	// IsBoot — whether the volume acts as the persistent root overlay.
	IsBoot bool
	// Mode — access mode of the attachment.
	Mode VolumeAttachMode
	// AutoDelete — whether the volume is deleted together with the instance.
	AutoDelete bool
}

// VolumeAttachmentInfo — a single volume↔Instance attachment (source of truth =
// kacho-storage Volume / volume_attachments row). Output-only on the compute side;
// used to build the read-only Instance.boot_disk / Instance.secondary_disks mirror
// on Get/List and as the confirmed result of Attach. Carries its owning VolumeID (the wire
// VolumeAttachment sub-record is nested under a Volume; ListAttachments/Attach flatten
// it with the id attached).
type VolumeAttachmentInfo struct {
	VolumeID     string
	InstanceID   string
	InstanceName string
	DeviceName   string
	IsBoot       bool
	Mode         VolumeAttachMode
	AutoDelete   bool
}

// StorageClient — port for compute→kacho-storage InternalVolumeService (volume↔
// Instance attach coordination, internal :9091 mTLS). kacho-storage owns the
// attachment and enforces the atomic attach-CAS + zone/project coherence; compute
// only forwards a self-describing payload and mirrors the result. Peer unavailable
// → fail-closed (Unavailable) on the attach/detach mutations. ListAttachments is a
// best-effort batched read for the Get/List mirror (graceful-degrade — the mirror is
// omitted when kacho-storage is unreachable, the Instance read itself never fails).
type StorageClient interface {
	Attach(ctx context.Context, spec VolumeAttachSpec) (*VolumeAttachmentInfo, error)
	Detach(ctx context.Context, volumeID, instanceID string) error
	ListAttachments(ctx context.Context, instanceIDs []string) ([]VolumeAttachmentInfo, error)
}

// ZoneRegistry — port для existence-check zone_id в Disk.Create / Instance.Create
// (и Disk.Relocate). Реализуется поверх kacho-geo (geo.v1.ZoneService.Get) через
// clients.GeoClient — Geography (Region/Zone) принадлежит kacho-geo. GetZone —
// чистый existence-check: nil → зона существует; ErrNotFound → зона неизвестна;
// иная ошибка (peer недоступен) пробрасывается для fail-closed на мутации.
type ZoneRegistry interface {
	GetZone(ctx context.Context, zoneID string) error
	// RegionOfZone возвращает region_id, которому принадлежит зона
	// (geo.v1.ZoneService.Get → `Zone.region_id`). Регион НИКОГДА не выводится из
	// имени зоны — имена региона и зоны произвольны, выводимой связи между ними
	// нет; единственный авторитет — владелец Geography. Нужен для
	// placement-coherence с REGIONAL (anycast) peer-ресурсом, у которого зоны нет.
	// Ошибка (зона неизвестна / geo недоступен) → fail-closed на мутации.
	RegionOfZone(ctx context.Context, zoneID string) (string, error)
}

// SubnetPlacement — placement-проекция vpc.Subnet, ограниченная тем, что нужно
// consumer'у для placement-coherence (data-integrity.md, placement-coherence).
// Дискриминатор — PlacementType: ZONAL несёт ZoneID (RegionID пуст), REGIONAL
// (anycast) несёт RegionID (ZoneID пуст, зоны нет by construction).
type SubnetPlacement struct {
	ID            string
	ProjectID     string
	PlacementType string // "ZONAL" | "REGIONAL"
	ZoneID        string
	RegionID      string
}

// Значения SubnetPlacement.PlacementType (parity с vpc.SubnetPlacementType).
const (
	SubnetPlacementZonal    = "ZONAL"
	SubnetPlacementRegional = "REGIONAL"
)

// SubnetRegistry — port peer-валидации подсети NIC-спеки через kacho-vpc
// (vpc.v1.SubnetService.Get) на request-path Instance.Create: машина создаётся в
// своей зоне, и её интерфейсы обязаны быть в той же зоне; REGIONAL (anycast)
// подсеть из зональной проверки исключена by construction (зоны нет — сравнивать
// не с чем), для неё остаётся региональная когерентность.
//
// Семантика ошибок: подсети нет / нет доступа → ErrNotFound (hide-existence,
// анти-BOLA); peer недоступен → gRPC Unavailable (fail-closed на мутации).
type SubnetRegistry interface {
	GetSubnet(ctx context.Context, subnetID string) (*SubnetPlacement, error)
}
