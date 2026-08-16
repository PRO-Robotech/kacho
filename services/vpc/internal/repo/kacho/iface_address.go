// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package kacho

import (
	"context"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
)

// AddressFilter — фильтр для списка адресов. Лежит в пакете kacho вместе с
// Pagination / NetworkFilter / SecurityGroupFilter (см. doc-комментарий на
// Pagination).
type AddressFilter struct {
	ProjectID string
	Name      string
	Filter    string
	// SubnetID — фильтр по подсети: матчит internal_ipv4.subnet_id ИЛИ
	// internal_ipv6.subnet_id (для ListAddresses?subnet_id=). "" = без фильтра.
	SubnetID string
	// IPAddress — сужение по ЗНАЧЕНИЮ выданного адреса. Замена снятому
	// `GetByValue`: тот отвечал на «чей это адрес?», но его внешняя ветвь была
	// неавторизуема по построению — область бралась из подсети, а у внешнего адреса
	// подсети нет.
	//
	// Матчит ВСЕ четыре формы владения: внутренний v4, внутренний v6, внешний v4,
	// внешний v6. «Чей это адрес» — один вопрос, а не четыре, и сужение, покрывающее
	// только внутренние, отвечало бы «не найдено» на законный внешний адрес.
	// "" = без сужения.
	IPAddress string
}

// AddressReaderIface — read-операции над Address в read-only TX-области
// (единый CQRS-контракт, parity с остальными VPC-ресурсами).
type AddressReaderIface interface {
	Get(ctx context.Context, id string) (*AddressRecord, error)
	List(ctx context.Context, f AddressFilter, p Pagination) ([]*AddressRecord, string, error)
	// GetByValue — lookup по ВНУТРЕННЕМУ IP; subnetID — необязательное сужение,
	// и сужает оно по внутренней спецификации адреса (`internal_ipv4.subnet_id`).
	// ErrNotFound если адреса не существует.
	//
	// Внешнего значения этот lookup не принимает намеренно: у адреса ровно одна
	// спецификация (oneof), у внешней подсети нет, поэтому «внешнее значение +
	// подсеть» не совпадает ни с одной строкой ни при каких данных. Раньше
	// параметр здесь был, и вызывающий получал за него «не найдено» про
	// существующий адрес; теперь такой запрос отвергается по имени поля в
	// use-case'е (address.GetByValueUseCase), а хранилище не делает вид, что
	// умеет отвечать.
	GetByValue(ctx context.Context, internalIP, subnetID string) (*AddressRecord, error)
	// GetReference возвращает referrer-row адреса. ErrNotFound если address
	// не существует ИЛИ у него нет referrer'а.
	GetReference(ctx context.Context, addressID string) (*domain.AddressReference, error)
	// ReferencesForAddresses — batch lookup referrer'ов для набора address-id
	// (map id→ref; отсутствующие ключи = нет referrer'а). Пустой вход → пустой map.
	ReferencesForAddresses(ctx context.Context, addressIDs []string) (map[string]*domain.AddressReference, error)
}

// AddressWriterIface — write-операции + read (writer видит свои writes).
//
// DML-методы НЕ открывают свою TX и НЕ emit'ят outbox — это делает caller
// (use-case) через RepositoryWriter.Outbox().Emit(...) после успешного DML.
// Atomicity DML + outbox гарантируется тем, что обе операции идут через одну
// pgx.Tx (writer-instance).
//
// Address имеет специфические writer-методы для IPAM allocate-flow:
//   - SetInternalIPv4 — атомарное обновление internal_ipv4 JSONB-spec
//     (random-pick allocator: каждая попытка — отдельный вызов через writer).
//   - SetInternalIPv6 — то же для v6.
//   - AllocateIPFromFreelist / ReturnIPToFreelist — PG-native freelist allocator (v4).
//   - InitIPv6PoolCursor / AllocateExternalIPv6 / FreeExternalIPv6 — sparse v6 allocator.
//   - SetReference / MarkEphemeralInUse / ClearReference — referrer-tracking (CAS на upsert).
//
// Атомарность IPAM-flow: весь allocate (cascade resolve pool → allocate IP →
// emit Address.UPDATED outbox) идет в одной writer-TX. Use-case открывает
// writer, делает Insert + Allocate* + Outbox().Emit, потом Commit (либо Abort
// при error → Insert откатывается, компенсирующий delete не нужен).
type AddressWriterIface interface {
	AddressReaderIface
	Insert(ctx context.Context, a *domain.Address) (*AddressRecord, error)
	Update(ctx context.Context, a *domain.Address) (*AddressRecord, error)
	// GetForUpdate — Get с `SELECT ... FOR UPDATE` (row-lock) внутри writer-TX.
	// Сериализует read-modify-write в Update (doUpdate): конкурентный Update
	// блокируется на GetForUpdate до commit первого, затем читает уже обновлённый
	// row и применяет свою маску поверх — lost-update исключён (project-rule #10).
	GetForUpdate(ctx context.Context, id string) (*AddressRecord, error)
	Delete(ctx context.Context, id string) error
	// DeleteGuarded — атомарный CAS-delete: удаляет адрес ТОЛЬКО если он не
	// used и не deletion_protection, и возвращает удаленный record (свежий
	// snapshot — для return-to-freelist). Закрывает гонку между sync-проверкой
	// «in use / protected» и worker-DELETE: address_references → addresses ON
	// DELETE CASCADE, поэтому безусловный DELETE молча отцеплял бы
	// конкурентно приаттаченный NIC. 0 строк:
	//   used=true           → ErrFailedPrecondition "address %s is in use"
	//   deletion_protection → ErrFailedPrecondition "...deletion_protection..."
	//   нет строки          → ErrNotFound
	DeleteGuarded(ctx context.Context, id string) (*AddressRecord, error)
	// SetInternalIPv4 атомарно обновляет internal_ipv4 JSONB-spec. nil → no-op.
	// Внешний адрес этим путём не пишется: его занятие обязано проходить через
	// книгу учёта пула (Insert/аллокаторы), иначе реестр расходится с
	// реальностью на пути занятия.
	SetInternalIPv4(ctx context.Context, id string, internalIpv4 *domain.InternalIpv4Spec) (*AddressRecord, error)
	// SetInternalIPv6 атомарно обновляет internal_ipv6 JSONB-spec. nil → no-op.
	SetInternalIPv6(ctx context.Context, id string, spec *domain.InternalIpv6Spec) (*AddressRecord, error)

	// AllocateIPFromFreelist — PG-native v4 allocator: atomic pop из
	// address_pool_free_ips (FOR UPDATE SKIP LOCKED) + UPDATE
	// addresses.external_ipv4{address, address_pool_id}. ErrPoolExhausted если
	// freelist пуст.
	AllocateIPFromFreelist(ctx context.Context, poolID, addressID string) (string, error)
	// ReturnIPToFreelist кладет IP обратно в pool freelist. Идемпотентно
	// (ON CONFLICT DO NOTHING).
	ReturnIPToFreelist(ctx context.Context, poolID, ip string) error

	// InitIPv6PoolCursor инициализирует sparse counter-based allocator для
	// IPv6-пула. Идемпотентно (ON CONFLICT DO NOTHING).
	InitIPv6PoolCursor(ctx context.Context, poolID string) error
	// AllocateExternalIPv6 — sparse v6 allocator: pop released offset → fresh
	// counter → INSERT allocated → UPDATE addresses.external_ipv6 (все в этой
	// writer-TX). ErrPoolExhausted если cursor превысил host-bits CIDR'а.
	AllocateExternalIPv6(ctx context.Context, poolID, addressID, zoneID string) (string, error)
	// FreeExternalIPv6 — освобождает v6 у address (released_offsets ← offset;
	// addresses.external_ipv6 ← NULL). Идемпотентно.
	FreeExternalIPv6(ctx context.Context, addressID string) error

	// SetReference — атомарный CAS-upsert referrer-row + addresses.used=true.
	// Конфликт по адресу с ЧУЖИМ referrer'ом → ErrFailedPrecondition. Idempotent
	// re-attach к тому же referrer проходит.
	SetReference(ctx context.Context, ref *domain.AddressReference) (*domain.AddressReference, error)
	// MarkEphemeralInUse — атомарно reserved=false + used=true + upsert referrer
	// (= SetReference + reset reserved).
	MarkEphemeralInUse(ctx context.Context, ref *domain.AddressReference) (*domain.AddressReference, error)
	// ClearReference удаляет referrer-row + used=false. ErrNotFound если адрес
	// не существует.
	ClearReference(ctx context.Context, addressID string) error
	// ReleaseLease снимает аренду ПО ПРЕДЪЯВЛЕНИЮ ВЛАДЕНИЯ ею и НАЗЫВАЕТ исход.
	//
	// Отличие от ClearReference — в том, кто и по чему принимает решение.
	// ClearReference ключуется ТОЛЬКО на address_id: он снимет любую ссылку,
	// включая чужую, и ничего не сообщит о том, что снял. Здесь предъявленная
	// пара (referrer_type, referrer_id) и project_id — часть ОДНОГО стейтмента с
	// проверкой кардинальности (ban #10), а нулевая кардинальность разрешается в
	// той же транзакции: под row-lock проигравший гонку видит закоммиченное
	// состояние и отличает «строки адреса нет» от «ссылка чужая».
	//
	// Ветку RELEASED/DETACHED выбирает КОЛОНКА owned у владельца, а не признак,
	// который принёс вызывающий: своя копия признака у потребителя — это второе
	// место об одном предмете, и расходится оно молча.
	//
	// ОТСУТСТВИЕ АРЕНДЫ — НЕ ОШИБКА. Постусловие глагола — «этот потребитель не
	// держит аренды на этом адресе»; когда оно уже верно, работа сделана. Отказ
	// здесь заклинил бы снос потребителя навсегда: на полосе освобождения отказ
	// FailedPrecondition перманентен, строка изолируется и переизбирается вечно.
	//
	// ErrFailedPrecondition — только когда предъявленное владение НЕ ПОДТВЕРЖДЕНО:
	// ссылка принадлежит другому потребителю либо адрес принадлежит другому
	// проекту. ErrNotFound этот метод НЕ ПРОИЗВОДИТ НИ НА ОДНОМ ВХОДЕ.
	//
	// На ветке RELEASED адрес УДАЛЁН и удалённая строка возвращена — из неё
	// вызывающий берёт координаты, по которым возвращает аренду в пул (IP и пул
	// читаются из свежего snapshot, а не из чтения до транзакции).
	// `deletion_protection` на этой ветке НЕ ЧИТАЕТСЯ намеренно: флаг ограждает
	// арендатора от удаления СВОЕГО адреса, а эфемерная аренда, заведённая
	// модулем, его собственностью не является. Обратное превращало бы флаг в
	// вечный клин на сносе потребителя.
	ReleaseLease(ctx context.Context, req LeaseReleaseRequest) (*LeaseReleaseResult, error)
}

// LeaseReleaseRequest — предъявление владения арендой.
type LeaseReleaseRequest struct {
	AddressID    string
	ProjectID    string
	ReferrerType string
	ReferrerID   string
}

// LeaseOutcome — НАЗВАННЫЙ исход снятия аренды.
//
// Исход называется полем, а не выводится вызывающим из кода ошибки: код ошибки
// для такого вывода непригоден by construction — у владельца есть законные
// причины отвечать одним кодом на разные положения дел.
type LeaseOutcome string

const (
	// LeaseReleased — ЭТИМ вызовом: ссылка снята, адрес удалён.
	LeaseReleased LeaseOutcome = "RELEASED"
	// LeaseAlreadyReleased — строки адреса нет, аренда снята ранее.
	LeaseAlreadyReleased LeaseOutcome = "ALREADY_RELEASED"
	// LeaseDetached — ЭТИМ вызовом: адрес арендатора, ссылка снята, адрес оставлен.
	LeaseDetached LeaseOutcome = "DETACHED"
	// LeaseAlreadyDetached — адрес есть, ссылки этого потребителя нет.
	LeaseAlreadyDetached LeaseOutcome = "ALREADY_DETACHED"
)

// LeaseReleaseResult — исход плюс удалённая строка (только у LeaseReleased).
type LeaseReleaseResult struct {
	Outcome LeaseOutcome
	// Deleted — удалённая строка адреса; nil на всех исходах, кроме LeaseReleased.
	Deleted *AddressRecord
}
