// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package address

import (
	"context"
	"log/slog"

	"github.com/PRO-Robotech/kacho/pkg/ids"
	corevalidate "github.com/PRO-Robotech/kacho/pkg/validate"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	kachorepo "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
)

// GetAddressUseCase — простой read: id-валидация + перевод repo-sentinel в gRPC
// status + обогащение UsedBy (referrer-tracking). Use-case можно было бы и
// опустить, но handler-у удобнее единый шов.
//
// Открывает Reader-TX явно через `repo.Reader(ctx)` — routing на slave-реплику
// станет automatic, когда та появится; пока на той же мастер-pool.
//
// # AuthZ
//
// Видимость единичного чтения энфорсит per-RPC authz-interceptor ПРЯМЫМ
// per-object Check'ом (`vpc_address:<id>` / relation `v_get`, permission_map),
// ровно как для Update/Delete. Deny на СУЩЕСТВУЮЩЕМ объекте interceptor
// превращает в NotFound (existence-hiding, ErrHideExistence), deny на
// отсутствующем — в passthrough, и handler отдаёт дословный NotFound из БД. Поэтому
// use-case никакой собственной authz-проверки не делает и не знает про фильтр.
//
// Здесь ранее стоял второй гейт `enforceGetVisible`, который спрашивал
// «перечисли ВСЕ адреса, которые subject'у можно» и искал id в ответе. Он был
// (а) избыточен — тот же вопрос уже задан interceptor'ом точнее и дешевле, и
// (б) НЕВЕРЕН: перечисление упиралось в жёсткий предел прежнего движка прав
// (1000 по умолчанию, без продолжения), поэтому на долгоживущем сторе
// собственный адрес тенанта выпадал за префикс и Get отдавал 404 при
// существующей строке и существующем гранте. Подробности — package-doc
// `internal/authzfilter`.
type GetAddressUseCase struct {
	repo Repo
}

// NewGetAddressUseCase создает GetAddressUseCase.
func NewGetAddressUseCase(r Repo) *GetAddressUseCase {
	return &GetAddressUseCase{repo: r}
}

// Execute возвращает repo-entity Address. NotFound → mapRepoErr → gRPC NotFound.
// UsedBy обогащается best-effort (failure → лог + адрес без UsedBy).
func (u *GetAddressUseCase) Execute(ctx context.Context, id string) (*kachorepo.AddressRecord, error) {
	if err := corevalidate.ResourceID("address", ids.PrefixAddress, id); err != nil {
		return nil, err
	}
	r, err := u.repo.Reader(ctx)
	if err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	defer func() { _ = r.Close() }()
	a, err := r.Addresses().Get(ctx, id)
	if err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	loadUsedBy(ctx, r.Addresses(), []*kachorepo.AddressRecord{a})
	return a, nil
}

// GetByValueUseCase возвращает Address по его ВНУТРЕННЕМУ IP-значению в
// названной подсети.
//
// # Почему только внутренний
//
// Область у запроса ровно одна — подсеть (`oneof scope`), и авторизация метода
// читает именно её (см. AuthZ ниже). Внутренний адрес в подсети размещается,
// поэтому вопрос «какой адрес имеет значение X внутри подсети S» разрешим.
// Внешний адрес подсети не имеет вовсе (в схеме `external_ipv4` и
// `internal_ipv4` — разные nullable jsonb, и `Address` — oneof: адрес бывает
// либо тем, либо другим), поэтому тот же вопрос про внешнее значение не имеет
// ответа НИ ПРИ КАКИХ данных.
//
// Поэтому запрос, назвавший внешний адрес, отвергается синхронно и по имени
// поля — первым стейтментом, до любого обращения к хранилищу. Прежде он доезжал
// до выборки, сужение по подсети не совпадало ни с одной строкой, и вызывающий
// получал «не найдено» про адрес, который существует: ложное утверждение об
// отсутствии в ответ на запрос, который контракт рекламирует.
//
// Поиск по внешнему адресу здесь НЕ реализуется: ему нужна область, которой у
// этого RPC нет (проект/аккаунт), а взять её, сняв сужение по подсети, нельзя —
// это открыло бы межтенантный оракул по внешним адресам. Полноценный поиск —
// отдельная работа со своей приёмкой.
//
// # AuthZ
//
// Как и GetAddressUseCase, собственного authz-гейта не несёт: RPC целиком гейтит
// per-RPC authz-interceptor прямым Check'ом `v_get @ vpc_subnet:<req.subnet_id>`
// (fail-closed, если subnet_id не задан) — handler своей проверки не делает и НЕ
// дублирует. Прежний `enforceGetVisible` здесь страдал ровно тем же дефектом
// ListObjects-предела, что и на GetAddressUseCase (собственный адрес за
// 1000-префиксом → ложный 404).
type GetByValueUseCase struct {
	repo Repo
}

// NewGetByValueUseCase создает GetByValueUseCase.
func NewGetByValueUseCase(r Repo) *GetByValueUseCase {
	return &GetByValueUseCase{repo: r}
}

// Execute — sync-валидация + lookup по внутреннему IP + загрузка UsedBy.
func (u *GetByValueUseCase) Execute(ctx context.Context, externalIP, internalIP, subnetID string) (*kachorepo.AddressRecord, error) {
	// Первым стейтментом: у этого RPC нет области, в которой внешнее значение
	// разрешимо, поэтому вопрос отвергается явно и по имени поля — а не тихо
	// превращается в «не найдено» про существующий адрес.
	if externalIP != "" {
		return nil, serviceerr.InvalidArg("external_ipv4_address",
			"external_ipv4_address: lookup by external address is not supported here — the only scope this request has is subnet_id, and an external address does not belong to a subnet; look the address up by id, or list addresses in the project")
	}
	if internalIP == "" {
		return nil, serviceerr.InvalidArg("internal_ipv4_address", "internal_ipv4_address: required")
	}
	r, err := u.repo.Reader(ctx)
	if err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	defer func() { _ = r.Close() }()
	a, err := r.Addresses().GetByValue(ctx, internalIP, subnetID)
	if err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	loadUsedBy(ctx, r.Addresses(), []*kachorepo.AddressRecord{a})
	return a, nil
}

// loadUsedBy обогащает каждый адрес из набора полем UsedBy (referrer-tracking,
// output-only) — кто использует адрес. Best-effort: ошибка чтения
// address_references → лог + адреса без UsedBy (graceful degradation, не валит
// чтение). Пустой/nil вход — no-op.
//
// Принимает `AddressReaderIface` (writer-iface тоже его embed'ит) — caller
// передает reader/writer из своей открытой TX.
func loadUsedBy(ctx context.Context, addrReader AddressReaderIface, addrs []*kachorepo.AddressRecord) {
	if len(addrs) == 0 {
		return
	}
	idsList := make([]string, 0, len(addrs))
	for _, a := range addrs {
		if a != nil {
			idsList = append(idsList, a.ID)
		}
	}
	if len(idsList) == 0 {
		return
	}
	refs, err := addrReader.ReferencesForAddresses(ctx, idsList)
	if err != nil {
		slog.WarnContext(ctx, "failed to load address referrers (used_by); returning addresses without it", "err", err)
		return
	}
	for _, a := range addrs {
		if a == nil {
			continue
		}
		if ref, ok := refs[a.ID]; ok && ref != nil {
			a.UsedBy = []*domain.AddressReference{ref}
		}
	}
}
