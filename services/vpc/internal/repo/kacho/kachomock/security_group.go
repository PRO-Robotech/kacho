// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package kachomock

import (
	"context"
	"sort"
	"time"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
)

// In-memory SecurityGroup reader/writer для kachomock.
//
// SG-specific operations:
//   - UpdateRules / UpdateRule — упрощенная семантика (без xmin-OCC; mock не
//     моделирует concurrent-conflict; pg-impl-side OCC покрывается
//     integration-тестом `security_group_occ_integration_test.go`).
//   - SG используется inline в Network.Create при `KACHO_VPC_DEFAULT_SG_INLINE=true`
//     (default) — default SG создается в той же writer-TX, что и Network.

// ---- SecurityGroup reader ----

// securityGroupReader — read-only snapshot SG.
//
// niSnap / netSnap — интерфейсы и сети того же снимка: по ним выводится
// «кем используется», ровно как pg-реализация выводит это боковым соединением
// по `security_group_ids` и `default_security_group_id`.
type securityGroupReader struct {
	snap    map[string]*kacho.SecurityGroupRecord
	niSnap  map[string]*kacho.NetworkInterfaceRecord
	netSnap map[string]*kacho.NetworkRecord
}

func (r *securityGroupReader) ReferrersFor(_ context.Context, sgIDs []string) (map[string][]kacho.SecurityGroupReferrer, error) {
	return sgReferrers(r.snap, r.niSnap, r.netSnap, sgIDs), nil
}

func (r *securityGroupReader) Get(_ context.Context, id string) (*kacho.SecurityGroupRecord, error) {
	sg, ok := r.snap[id]
	if !ok {
		return nil, repo.ErrNotFound
	}
	cp := *sg
	return &cp, nil
}

// GetMany — карта найденных; отсутствующие id просто отсутствуют (семантика
// хранилища).
func (r *securityGroupReader) GetMany(ctx context.Context, ids []string) (map[string]*kacho.SecurityGroupRecord, error) {
	out := make(map[string]*kacho.SecurityGroupRecord, len(ids))
	for _, id := range ids {
		sg, err := r.Get(ctx, id)
		if err != nil {
			continue
		}
		out[id] = sg
	}
	return out, nil
}

func (r *securityGroupReader) List(_ context.Context, f kacho.SecurityGroupFilter, p kacho.Pagination) ([]*kacho.SecurityGroupRecord, string, error) {
	if err := checkPagination(p); err != nil {
		return nil, "", err
	}
	var result []*kacho.SecurityGroupRecord
	for _, sg := range r.snap {
		if (f.ProjectID == "" || sg.ProjectID == f.ProjectID) &&
			(f.NetworkID == "" || sg.NetworkID == f.NetworkID) &&
			(f.Name == "" || string(sg.Name) == f.Name) {
			cp := *sg
			result = append(result, &cp)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.Before(result[j].CreatedAt) })
	return result, "", nil
}

// ---- SecurityGroup writer ----

// securityGroupWriter — write-«TX» SG. Writer видит свои writes — Get/List
// поверх localSGs.
type securityGroupWriter struct {
	w *writerImpl
}

func (sw *securityGroupWriter) Get(_ context.Context, id string) (*kacho.SecurityGroupRecord, error) {
	if _, deleted := sw.w.deletedSGIDs[id]; deleted {
		return nil, repo.ErrNotFound
	}
	sg, ok := sw.w.localSGs[id]
	if !ok {
		return nil, repo.ErrNotFound
	}
	cp := *sg
	return &cp, nil
}

// GetForUpdate — in-memory mock не моделирует row-lock; семантика = Get.
func (sw *securityGroupWriter) GetForUpdate(ctx context.Context, id string) (*kacho.SecurityGroupRecord, error) {
	return sw.Get(ctx, id)
}

// GetMany — карта найденных в состоянии writer-TX.
func (sw *securityGroupWriter) GetMany(ctx context.Context, ids []string) (map[string]*kacho.SecurityGroupRecord, error) {
	out := make(map[string]*kacho.SecurityGroupRecord, len(ids))
	for _, id := range ids {
		sg, err := sw.Get(ctx, id)
		if err != nil {
			continue
		}
		out[id] = sg
	}
	return out, nil
}

func (sw *securityGroupWriter) List(_ context.Context, f kacho.SecurityGroupFilter, p kacho.Pagination) ([]*kacho.SecurityGroupRecord, string, error) {
	if err := checkPagination(p); err != nil {
		return nil, "", err
	}
	var result []*kacho.SecurityGroupRecord
	for id, sg := range sw.w.localSGs {
		if _, deleted := sw.w.deletedSGIDs[id]; deleted {
			continue
		}
		if (f.ProjectID == "" || sg.ProjectID == f.ProjectID) &&
			(f.NetworkID == "" || sg.NetworkID == f.NetworkID) &&
			(f.Name == "" || string(sg.Name) == f.Name) {
			cp := *sg
			result = append(result, &cp)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.Before(result[j].CreatedAt) })
	return result, "", nil
}

func (sw *securityGroupWriter) ReferrersFor(_ context.Context, sgIDs []string) (map[string][]kacho.SecurityGroupReferrer, error) {
	return sgReferrers(sw.w.localSGs, sw.w.localNIs, sw.w.local, sgIDs), nil
}

// sgReferrers выводит потребителей групп из того же снимка — тот же предмет,
// который в базе выражают `network_interfaces.security_group_ids` и
// `networks.default_security_group_id`.
//
// Дублёр обязан быть НЕ СНИСХОДИТЕЛЬНЕЕ настоящего, иначе он прячет ровно тот
// дефект, ради которого его подставляют. Поэтому здесь воспроизведены оба
// свойства запроса, а не только выборка: граница проекта (потребитель чужого
// проекта не показывается) и потолок ответа (предел плюс одна строка —
// признак «есть ещё»). Порядок — тот же: сеть впереди интерфейсов, дальше по
// времени создания и идентификатору.
func sgReferrers(
	sgs map[string]*kacho.SecurityGroupRecord,
	nics map[string]*kacho.NetworkInterfaceRecord,
	nets map[string]*kacho.NetworkRecord,
	sgIDs []string,
) map[string][]kacho.SecurityGroupReferrer {
	out := make(map[string][]kacho.SecurityGroupReferrer, len(sgIDs))
	for _, id := range sgIDs {
		sg, ok := sgs[id]
		if !ok {
			continue
		}
		type row struct {
			ref  kacho.SecurityGroupReferrer
			kind int
			at   time.Time
		}
		var rows []row
		for _, n := range nets {
			if n.DefaultSecurityGroupID != id || n.ProjectID != sg.ProjectID {
				continue
			}
			rows = append(rows, row{
				ref: kacho.SecurityGroupReferrer{Type: kacho.SecurityGroupReferrerNetwork, ID: n.ID, Name: string(n.Name)},
				at:  n.CreatedAt,
			})
		}
		for _, ni := range nics {
			if ni.ProjectID != sg.ProjectID {
				continue
			}
			for _, held := range ni.SecurityGroupIDs {
				if held != id {
					continue
				}
				rows = append(rows, row{
					ref:  kacho.SecurityGroupReferrer{Type: kacho.SecurityGroupReferrerNIC, ID: ni.ID, Name: string(ni.Name)},
					kind: 1,
					at:   ni.CreatedAt,
				})
				break
			}
		}
		if len(rows) == 0 {
			continue
		}
		sort.Slice(rows, func(i, j int) bool {
			if rows[i].kind != rows[j].kind {
				return rows[i].kind < rows[j].kind
			}
			if !rows[i].at.Equal(rows[j].at) {
				return rows[i].at.Before(rows[j].at)
			}
			return rows[i].ref.ID < rows[j].ref.ID
		})
		if len(rows) > kacho.SecurityGroupUsedByFetch {
			rows = rows[:kacho.SecurityGroupUsedByFetch]
		}
		refs := make([]kacho.SecurityGroupReferrer, 0, len(rows))
		for _, r := range rows {
			refs = append(refs, r.ref)
		}
		out[id] = refs
	}
	return out
}

func (sw *securityGroupWriter) Insert(_ context.Context, sg *domain.SecurityGroup) (*kacho.SecurityGroupRecord, error) {
	rec := &kacho.SecurityGroupRecord{SecurityGroup: *sg, CreatedAt: time.Now().UTC()}
	sw.w.localSGs[sg.ID] = rec
	cp := *rec
	return &cp, nil
}

func (sw *securityGroupWriter) Update(_ context.Context, sg *domain.SecurityGroup) (*kacho.SecurityGroupRecord, error) {
	if _, deleted := sw.w.deletedSGIDs[sg.ID]; deleted {
		return nil, repo.ErrNotFound
	}
	existing, ok := sw.w.localSGs[sg.ID]
	if !ok {
		return nil, repo.ErrNotFound
	}
	existing.SecurityGroup = *sg
	cp := *existing
	return &cp, nil
}

func (sw *securityGroupWriter) Delete(_ context.Context, id string) error {
	if _, ok := sw.w.localSGs[id]; !ok {
		return repo.ErrNotFound
	}
	if sw.w.deletedSGIDs == nil {
		sw.w.deletedSGIDs = make(map[string]struct{})
	}
	sw.w.deletedSGIDs[id] = struct{}{}
	delete(sw.w.localSGs, id)
	return nil
}

// UpdateRules / UpdateRule — упрощенная семантика (без xmin-OCC; mock не
// моделирует concurrent-conflict). Достаточно для unit-тестов use-case'ов.
func (sw *securityGroupWriter) UpdateRules(_ context.Context, sgID string, deleteIDs []string, add []domain.SecurityGroupRule) (*kacho.SecurityGroupRecord, error) {
	if _, deleted := sw.w.deletedSGIDs[sgID]; deleted {
		return nil, repo.ErrNotFound
	}
	sg, ok := sw.w.localSGs[sgID]
	if !ok {
		return nil, repo.ErrNotFound
	}
	if len(deleteIDs) > 0 {
		toDel := make(map[string]struct{}, len(deleteIDs))
		for _, id := range deleteIDs {
			toDel[id] = struct{}{}
		}
		filtered := sg.Rules[:0]
		for _, r := range sg.Rules {
			if _, drop := toDel[r.ID]; drop {
				continue
			}
			filtered = append(filtered, r)
		}
		sg.Rules = filtered
	}
	sg.Rules = append(sg.Rules, add...)
	// Потолок НАКОПЛЕННОГО набора — зеркало pg-стороны: глагол аддитивен,
	// поэтому набор растёт серией формально законных запросов, и проверять его
	// обязан тот, кто пишет итог. Без зеркала ни один unit-тест не мог бы
	// добраться до этого предусловия.
	if len(sg.Rules) > domain.MaxSecurityGroupRules {
		return nil, repo.ErrFailedPrecondition
	}
	cp := *sg
	return &cp, nil
}

func (sw *securityGroupWriter) UpdateRule(_ context.Context, sgID, ruleID, description string, labels map[string]string, mask []string) (*kacho.SecurityGroupRecord, error) {
	if _, deleted := sw.w.deletedSGIDs[sgID]; deleted {
		return nil, repo.ErrNotFound
	}
	sg, ok := sw.w.localSGs[sgID]
	if !ok {
		return nil, repo.ErrNotFound
	}
	applyMask := len(mask) > 0
	maskSet := map[string]struct{}{}
	for _, m := range mask {
		maskSet[m] = struct{}{}
	}
	found := false
	for i := range sg.Rules {
		if sg.Rules[i].ID != ruleID {
			continue
		}
		found = true
		if !applyMask {
			sg.Rules[i].Description = domain.RcDescription(description)
			sg.Rules[i].Labels = labels
		} else {
			if _, ok := maskSet["description"]; ok {
				sg.Rules[i].Description = domain.RcDescription(description)
			}
			if _, ok := maskSet["labels"]; ok {
				sg.Rules[i].Labels = labels
			}
		}
		break
	}
	if !found {
		return nil, repo.ErrNotFound
	}
	cp := *sg
	return &cp, nil
}

// Assertion: securityGroupReader/Writer implements iface.
var (
	_ kacho.SecurityGroupReaderIface = (*securityGroupReader)(nil)
	_ kacho.SecurityGroupWriterIface = (*securityGroupWriter)(nil)
)
