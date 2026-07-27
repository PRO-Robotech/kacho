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
// (б) НЕВЕРЕН: перечисление упирается в жёсткий предел OpenFGA ListObjects
// (default 1000, без continuation-token'а), поэтому на долгоживущем сторе
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

// GetByValueUseCase возвращает Address по его IP-значению (external или
// internal). oneof external_ipv4_address / internal_ipv4_address; optional
// subnet_id scope.
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

// Execute — sync-валидация + lookup по IP + загрузка UsedBy.
func (u *GetByValueUseCase) Execute(ctx context.Context, externalIP, internalIP, subnetID string) (*kachorepo.AddressRecord, error) {
	if externalIP == "" && internalIP == "" {
		return nil, serviceerr.InvalidArg("address", "address (external_ipv4_address or internal_ipv4_address) is required")
	}
	r, err := u.repo.Reader(ctx)
	if err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	defer func() { _ = r.Close() }()
	a, err := r.Addresses().GetByValue(ctx, externalIP, internalIP, subnetID)
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
