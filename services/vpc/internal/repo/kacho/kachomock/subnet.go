// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package kachomock

import (
	"context"
	"net/netip"
	"sort"
	"time"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
)

// cidrContainsMock — outer ⊇ inner (outer не длиннее inner И покрывает его сетевой
// адрес). Невалидный префикс → false. In-memory аналог pg-оператора cidr `>>=`.
func cidrContainsMock(outer, inner string) bool {
	o, err := netip.ParsePrefix(outer)
	if err != nil {
		return false
	}
	i, err := netip.ParsePrefix(inner)
	if err != nil {
		return false
	}
	return o.Bits() <= i.Bits() && o.Contains(i.Addr())
}

// firstCoveringBlock — первый candidate-блок, покрывающий хотя бы один subnetCidr,
// который НЕ покрыт ни одним retained-блоком (in-memory аналог pg-запроса
// SupernetBlockCoveringSubnet). Пустая строка → нет осиротевших.
func firstCoveringBlock(subnetCidrs, candidate, retained []string) string {
	for _, sc := range subnetCidrs {
		for _, cb := range candidate {
			if !cidrContainsMock(cb, sc) {
				continue
			}
			covered := false
			for _, rb := range retained {
				if cidrContainsMock(rb, sc) {
					covered = true
					break
				}
			}
			if !covered {
				return cb
			}
		}
	}
	return ""
}

// In-memory Subnet reader/writer для kachomock.
//
// Subnet-specific operations: AddressesBySubnet (внутри read-snapshot'а — для
// SubnetService.Delete precheck и ListUsedAddresses), SetCidrBlocks
// (AddCidrBlocks/RemoveCidrBlocks; EXCLUDE constraint на pg-side не
// моделируется).

// ---- Subnet reader ----

// subnetReader — read-only snapshot Subnet (+ addresses для AddressesBySubnet).
type subnetReader struct {
	snap  map[string]*kacho.SubnetRecord
	addrs map[string]*kacho.AddressRecord
}

func (r *subnetReader) Get(_ context.Context, id string) (*kacho.SubnetRecord, error) {
	s, ok := r.snap[id]
	if !ok {
		return nil, repo.ErrNotFound
	}
	cp := *s
	return &cp, nil
}

func (r *subnetReader) List(_ context.Context, f kacho.SubnetFilter, _ kacho.Pagination) ([]*kacho.SubnetRecord, string, error) {
	var result []*kacho.SubnetRecord
	for _, s := range r.snap {
		if (f.ProjectID == "" || s.ProjectID == f.ProjectID) &&
			(f.NetworkID == "" || s.NetworkID == f.NetworkID) &&
			(f.Name == "" || string(s.Name) == f.Name) {
			cp := *s
			result = append(result, &cp)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.Before(result[j].CreatedAt) })
	return result, "", nil
}

// ListByIDs — фильтрация поверх ids set теми же in-memory предикатами, что List.
// Пустой allowedIDs → (nil, "", nil).
func (r *subnetReader) ListByIDs(_ context.Context, f kacho.SubnetFilter, allowedIDs []string, _ kacho.Pagination) ([]*kacho.SubnetRecord, string, error) {
	if len(allowedIDs) == 0 {
		return nil, "", nil
	}
	allowed := make(map[string]struct{}, len(allowedIDs))
	for _, id := range allowedIDs {
		allowed[id] = struct{}{}
	}
	var result []*kacho.SubnetRecord
	for _, s := range r.snap {
		if _, ok := allowed[s.ID]; !ok {
			continue
		}
		if (f.ProjectID == "" || s.ProjectID == f.ProjectID) &&
			(f.NetworkID == "" || s.NetworkID == f.NetworkID) &&
			(f.Name == "" || string(s.Name) == f.Name) {
			cp := *s
			result = append(result, &cp)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.Before(result[j].CreatedAt) })
	return result, "", nil
}

// SupernetBlockCoveringSubnet — in-memory ∉-guard: перебирает ВСЕ подсети сети в
// snapshot'е (mock не пагинирует — этим он не воспроизводит реальный первополосный
// баг, но корректно моделирует контракт метода для unit-теста use-case'а).
func (r *subnetReader) SupernetBlockCoveringSubnet(_ context.Context, networkID string, candidateBlocks, retainedBlocks []string) (string, error) {
	for _, s := range r.snap {
		if s.NetworkID != networkID {
			continue
		}
		blocks := append(append([]string{}, s.V4CidrBlocks...), s.V6CidrBlocks...)
		if b := firstCoveringBlock(blocks, candidateBlocks, retainedBlocks); b != "" {
			return b, nil
		}
	}
	return "", nil
}

// AddressesBySubnet — filter by internal_ipv4.subnet_id / internal_ipv6.subnet_id.
// Simplified mock: фильтрует addrs по совпадению spec.SubnetID. Pagination в
// тестах не нужна — возвращаем все за один вызов.
func (r *subnetReader) AddressesBySubnet(_ context.Context, subnetID string, _ kacho.Pagination) ([]*kacho.AddressRecord, string, error) {
	var result []*kacho.AddressRecord
	for _, a := range r.addrs {
		if a.InternalIpv4 != nil && a.InternalIpv4.SubnetID == subnetID {
			cp := *a
			result = append(result, &cp)
			continue
		}
		if a.InternalIpv6 != nil && a.InternalIpv6.SubnetID == subnetID {
			cp := *a
			result = append(result, &cp)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.Before(result[j].CreatedAt) })
	return result, "", nil
}

// ---- Subnet writer ----

// subnetWriter — write-«TX» Subnet. Writer видит свои writes —
// Get/List/AddressesBySubnet поверх localSubs.
type subnetWriter struct {
	w *writerImpl
}

func (sw *subnetWriter) Get(_ context.Context, id string) (*kacho.SubnetRecord, error) {
	if _, deleted := sw.w.deletedSubIDs[id]; deleted {
		return nil, repo.ErrNotFound
	}
	s, ok := sw.w.localSubs[id]
	if !ok {
		return nil, repo.ErrNotFound
	}
	cp := *s
	return &cp, nil
}

// GetForUpdate — in-memory mock не моделирует row-lock; семантика = Get.
func (sw *subnetWriter) GetForUpdate(ctx context.Context, id string) (*kacho.SubnetRecord, error) {
	return sw.Get(ctx, id)
}

func (sw *subnetWriter) List(_ context.Context, f kacho.SubnetFilter, _ kacho.Pagination) ([]*kacho.SubnetRecord, string, error) {
	var result []*kacho.SubnetRecord
	for id, s := range sw.w.localSubs {
		if _, deleted := sw.w.deletedSubIDs[id]; deleted {
			continue
		}
		if (f.ProjectID == "" || s.ProjectID == f.ProjectID) &&
			(f.NetworkID == "" || s.NetworkID == f.NetworkID) &&
			(f.Name == "" || string(s.Name) == f.Name) {
			cp := *s
			result = append(result, &cp)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.Before(result[j].CreatedAt) })
	return result, "", nil
}

// ListByIDs — writer-side: фильтрация поверх ids set теми же in-memory
// предикатами, что List. Пустой allowedIDs → (nil, "", nil).
func (sw *subnetWriter) ListByIDs(_ context.Context, f kacho.SubnetFilter, allowedIDs []string, _ kacho.Pagination) ([]*kacho.SubnetRecord, string, error) {
	if len(allowedIDs) == 0 {
		return nil, "", nil
	}
	allowed := make(map[string]struct{}, len(allowedIDs))
	for _, id := range allowedIDs {
		allowed[id] = struct{}{}
	}
	var result []*kacho.SubnetRecord
	for id, s := range sw.w.localSubs {
		if _, deleted := sw.w.deletedSubIDs[id]; deleted {
			continue
		}
		if _, ok := allowed[id]; !ok {
			continue
		}
		if (f.ProjectID == "" || s.ProjectID == f.ProjectID) &&
			(f.NetworkID == "" || s.NetworkID == f.NetworkID) &&
			(f.Name == "" || string(s.Name) == f.Name) {
			cp := *s
			result = append(result, &cp)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.Before(result[j].CreatedAt) })
	return result, "", nil
}

// SupernetBlockCoveringSubnet — writer-side: перебирает подсети сети в localSubs
// (writer видит свои writes), исключая помеченные на удаление.
func (sw *subnetWriter) SupernetBlockCoveringSubnet(_ context.Context, networkID string, candidateBlocks, retainedBlocks []string) (string, error) {
	for id, s := range sw.w.localSubs {
		if _, deleted := sw.w.deletedSubIDs[id]; deleted {
			continue
		}
		if s.NetworkID != networkID {
			continue
		}
		blocks := append(append([]string{}, s.V4CidrBlocks...), s.V6CidrBlocks...)
		if b := firstCoveringBlock(blocks, candidateBlocks, retainedBlocks); b != "" {
			return b, nil
		}
	}
	return "", nil
}

func (sw *subnetWriter) AddressesBySubnet(_ context.Context, subnetID string, _ kacho.Pagination) ([]*kacho.AddressRecord, string, error) {
	var result []*kacho.AddressRecord
	for _, a := range sw.w.localAddrs {
		if a.InternalIpv4 != nil && a.InternalIpv4.SubnetID == subnetID {
			cp := *a
			result = append(result, &cp)
			continue
		}
		if a.InternalIpv6 != nil && a.InternalIpv6.SubnetID == subnetID {
			cp := *a
			result = append(result, &cp)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.Before(result[j].CreatedAt) })
	return result, "", nil
}

func (sw *subnetWriter) Insert(_ context.Context, s *domain.Subnet) (*kacho.SubnetRecord, error) {
	rec := &kacho.SubnetRecord{Subnet: *s, CreatedAt: time.Now().UTC()}
	sw.w.localSubs[s.ID] = rec
	cp := *rec
	return &cp, nil
}

func (sw *subnetWriter) Update(_ context.Context, s *domain.Subnet) (*kacho.SubnetRecord, error) {
	if _, deleted := sw.w.deletedSubIDs[s.ID]; deleted {
		return nil, repo.ErrNotFound
	}
	existing, ok := sw.w.localSubs[s.ID]
	if !ok {
		return nil, repo.ErrNotFound
	}
	existing.Subnet = *s
	cp := *existing
	return &cp, nil
}

func (sw *subnetWriter) Delete(_ context.Context, id string) error {
	if _, ok := sw.w.localSubs[id]; !ok {
		return repo.ErrNotFound
	}
	if sw.w.deletedSubIDs == nil {
		sw.w.deletedSubIDs = make(map[string]struct{})
	}
	sw.w.deletedSubIDs[id] = struct{}{}
	delete(sw.w.localSubs, id)
	return nil
}

func (sw *subnetWriter) SetCidrBlocks(_ context.Context, id string, v4, v6 []string) (*kacho.SubnetRecord, error) {
	if _, deleted := sw.w.deletedSubIDs[id]; deleted {
		return nil, repo.ErrNotFound
	}
	s, ok := sw.w.localSubs[id]
	if !ok {
		return nil, repo.ErrNotFound
	}
	s.V4CidrBlocks = v4
	s.V6CidrBlocks = v6
	cp := *s
	return &cp, nil
}

// Assertion: subnetReader/Writer implements iface.
var (
	_ kacho.SubnetReaderIface = (*subnetReader)(nil)
	_ kacho.SubnetWriterIface = (*subnetWriter)(nil)
)
