// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package kachomock

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
)

// In-memory CidrGroup reader/writer для kachomock.
//
// ДУБЛЁР НЕ СНИСХОДИТЕЛЬНЕЕ НАСТОЯЩЕГО — это требование, а не украшение: если бы
// mock принимал состав сверх потолка и удалял набор с живой ссылкой, он делал бы
// невидимым ровно тот дефект, ради которого его подставляют. Поэтому здесь
// исполнены оба отказа, и текст первого собирается ТОЙ ЖЕ функцией, что в
// pg-реализации, — через `repo.ErrFailedPrecondition` с той же формой сообщения.
//
// Чего mock НЕ моделирует и почему это записано: блокировок строк нет, поэтому
// сериализация конкурентных писателей проверяется интеграционной пробой на
// настоящем Postgres, а не здесь.

// ---- CidrGroup reader ----

type cidrGroupReader struct {
	snap map[string]*kacho.CidrGroupRecord
	// sgSnap — группы правил того же снимка: по ним выводится `UsedBy`, ровно как
	// pg-реализация выводит его из проекции ссылок.
	sgSnap map[string]*kacho.SecurityGroupRecord
}

func (r *cidrGroupReader) Get(_ context.Context, id string) (*kacho.CidrGroupRecord, error) {
	g, ok := r.snap[id]
	if !ok {
		return nil, fmt.Errorf("%w: CidrGroup %s not found", repo.ErrNotFound, id)
	}
	cp := copyCidrGroup(g)
	cp.UsedBy = referrersFromSGs(r.sgSnap, id)
	return cp, nil
}

func (r *cidrGroupReader) List(_ context.Context, f kacho.CidrGroupFilter, p kacho.Pagination) ([]*kacho.CidrGroupRecord, string, error) {
	if err := checkPagination(p); err != nil {
		return nil, "", err
	}
	var result []*kacho.CidrGroupRecord
	for _, g := range r.snap {
		if (f.ProjectID != "" && g.ProjectID != f.ProjectID) ||
			(f.Name != "" && string(g.Name) != f.Name) {
			continue
		}
		cp := copyCidrGroup(g)
		cp.UsedBy = referrersFromSGs(r.sgSnap, g.ID)
		result = append(result, cp)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.Before(result[j].CreatedAt) })
	return result, "", nil
}

func (r *cidrGroupReader) ReferrersFor(_ context.Context, groupIDs []string) (map[string][]kacho.CidrGroupReferrer, error) {
	out := make(map[string][]kacho.CidrGroupReferrer, len(groupIDs))
	for _, id := range groupIDs {
		if refs := referrersFromSGs(r.sgSnap, id); len(refs) > 0 {
			out[id] = refs
		}
	}
	return out, nil
}

// ---- CidrGroup writer ----

type cidrGroupWriter struct {
	w *writerImpl
}

func (gw *cidrGroupWriter) Get(_ context.Context, id string) (*kacho.CidrGroupRecord, error) {
	if _, deleted := gw.w.deletedCGIDs[id]; deleted {
		return nil, fmt.Errorf("%w: CidrGroup %s not found", repo.ErrNotFound, id)
	}
	g, ok := gw.w.localCGs[id]
	if !ok {
		return nil, fmt.Errorf("%w: CidrGroup %s not found", repo.ErrNotFound, id)
	}
	cp := copyCidrGroup(g)
	cp.UsedBy = referrersFromSGs(gw.w.localSGs, id)
	return cp, nil
}

// GetForUpdate — mock не моделирует row-lock; семантика = Get. Сериализацию
// проверяет интеграционная проба на настоящем Postgres.
func (gw *cidrGroupWriter) GetForUpdate(ctx context.Context, id string) (*kacho.CidrGroupRecord, error) {
	return gw.Get(ctx, id)
}

func (gw *cidrGroupWriter) List(_ context.Context, f kacho.CidrGroupFilter, p kacho.Pagination) ([]*kacho.CidrGroupRecord, string, error) {
	if err := checkPagination(p); err != nil {
		return nil, "", err
	}
	var result []*kacho.CidrGroupRecord
	for id, g := range gw.w.localCGs {
		if _, deleted := gw.w.deletedCGIDs[id]; deleted {
			continue
		}
		if (f.ProjectID != "" && g.ProjectID != f.ProjectID) ||
			(f.Name != "" && string(g.Name) != f.Name) {
			continue
		}
		cp := copyCidrGroup(g)
		cp.UsedBy = referrersFromSGs(gw.w.localSGs, g.ID)
		result = append(result, cp)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.Before(result[j].CreatedAt) })
	return result, "", nil
}

func (gw *cidrGroupWriter) ReferrersFor(_ context.Context, groupIDs []string) (map[string][]kacho.CidrGroupReferrer, error) {
	out := make(map[string][]kacho.CidrGroupReferrer, len(groupIDs))
	for _, id := range groupIDs {
		if refs := referrersFromSGs(gw.w.localSGs, id); len(refs) > 0 {
			out[id] = refs
		}
	}
	return out, nil
}

func (gw *cidrGroupWriter) Insert(ctx context.Context, g *domain.CidrGroup) (*kacho.CidrGroupRecord, error) {
	if _, exists := gw.w.localCGs[g.ID]; exists {
		return nil, repo.ErrAlreadyExists
	}
	if string(g.Name) != "" {
		for id, other := range gw.w.localCGs {
			if _, deleted := gw.w.deletedCGIDs[id]; deleted {
				continue
			}
			if other.ProjectID == g.ProjectID && other.Name == g.Name {
				return nil, repo.ErrAlreadyExists
			}
		}
	}
	rec := &kacho.CidrGroupRecord{CidrGroup: *g, CreatedAt: time.Now().UTC()}
	rec.V4CidrBlocks = nil
	rec.V6CidrBlocks = nil
	gw.w.localCGs[g.ID] = rec
	if len(g.V4CidrBlocks) == 0 && len(g.V6CidrBlocks) == 0 {
		return copyCidrGroup(rec), nil
	}
	return gw.AddBlocks(ctx, g.ID, g.V4CidrBlocks, g.V6CidrBlocks)
}

func (gw *cidrGroupWriter) Update(ctx context.Context, g *domain.CidrGroup) (*kacho.CidrGroupRecord, error) {
	cur, ok := gw.w.localCGs[g.ID]
	if !ok {
		return nil, fmt.Errorf("%w: CidrGroup %s not found", repo.ErrNotFound, g.ID)
	}
	updated := *cur
	updated.Name = g.Name
	updated.Description = g.Description
	updated.Labels = g.Labels
	gw.w.localCGs[g.ID] = &updated
	return gw.Get(ctx, g.ID)
}

// AddBlocks — идемпотентное добавление с потолком НА СЕМЕЙСТВО. Потолок проверен
// на ИТОГОВОМ составе после дедупликации: повтор уже присутствующего члена не
// «съедает» предел, ровно как в pg-реализации.
func (gw *cidrGroupWriter) AddBlocks(ctx context.Context, id string, v4, v6 []string) (*kacho.CidrGroupRecord, error) {
	cur, ok := gw.w.localCGs[id]
	if !ok {
		return nil, fmt.Errorf("%w: CidrGroup %s not found", repo.ErrNotFound, id)
	}
	// Потолок проверяется по ЗАПРОШЕННОМУ (как условный инкремент счётчика в
	// базе), а не по итогу дедупликации: иначе mock принимал бы запрос, который
	// настоящий писатель отвергает предикатом UPDATE.
	if len(cur.V4CidrBlocks)+len(v4) > domain.MaxCidrGroupBlocks ||
		len(cur.V6CidrBlocks)+len(v6) > domain.MaxCidrGroupBlocks {
		return nil, cidrGroupCapErr(id, cur, len(v4), len(v6))
	}
	updated := *cur
	updated.V4CidrBlocks = mergeBlocks(cur.V4CidrBlocks, v4)
	updated.V6CidrBlocks = mergeBlocks(cur.V6CidrBlocks, v6)
	gw.w.localCGs[id] = &updated
	return gw.Get(ctx, id)
}

func (gw *cidrGroupWriter) RemoveBlocks(ctx context.Context, id string, v4, v6 []string) (*kacho.CidrGroupRecord, error) {
	cur, ok := gw.w.localCGs[id]
	if !ok {
		return nil, fmt.Errorf("%w: CidrGroup %s not found", repo.ErrNotFound, id)
	}
	updated := *cur
	updated.V4CidrBlocks = subtractBlocks(cur.V4CidrBlocks, v4)
	updated.V6CidrBlocks = subtractBlocks(cur.V6CidrBlocks, v6)
	gw.w.localCGs[id] = &updated
	return gw.Get(ctx, id)
}

// Delete — отказ по живой ссылке, как внешний ключ RESTRICT в базе.
func (gw *cidrGroupWriter) Delete(_ context.Context, id string) error {
	if _, ok := gw.w.localCGs[id]; !ok {
		return fmt.Errorf("%w: CidrGroup %s not found", repo.ErrNotFound, id)
	}
	if refs := referrersFromSGs(gw.w.localSGs, id); len(refs) > 0 {
		return fmt.Errorf("%w: CidrGroup %s is in use", repo.ErrFailedPrecondition, id)
	}
	delete(gw.w.localCGs, id)
	gw.w.deletedCGIDs[id] = struct{}{}
	return nil
}

// ---- helpers ----

func copyCidrGroup(g *kacho.CidrGroupRecord) *kacho.CidrGroupRecord {
	cp := *g
	cp.V4CidrBlocks = append([]string(nil), g.V4CidrBlocks...)
	cp.V6CidrBlocks = append([]string(nil), g.V6CidrBlocks...)
	cp.UsedBy = nil
	return &cp
}

// referrersFromSGs выводит потребителей набора из правил групп — тот же предмет,
// который в базе держит проекция ссылок, поддерживаемая триггером.
func referrersFromSGs(sgs map[string]*kacho.SecurityGroupRecord, groupID string) []kacho.CidrGroupReferrer {
	var out []kacho.CidrGroupReferrer
	for _, sg := range sgs {
		rules := 0
		for _, r := range sg.Rules {
			if r.CidrGroupID == groupID {
				rules++
			}
		}
		if rules > 0 {
			out = append(out, kacho.CidrGroupReferrer{
				SecurityGroupID:   sg.ID,
				SecurityGroupName: string(sg.Name),
				Rules:             rules,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SecurityGroupID < out[j].SecurityGroupID })
	return out
}

func cidrGroupCapErr(id string, cur *kacho.CidrGroupRecord, addV4, addV6 int) error {
	var over string
	if len(cur.V4CidrBlocks)+addV4 > domain.MaxCidrGroupBlocks {
		over = fmt.Sprintf("v4: %d present, %d requested", len(cur.V4CidrBlocks), addV4)
	}
	if len(cur.V6CidrBlocks)+addV6 > domain.MaxCidrGroupBlocks {
		if over != "" {
			over += ", "
		}
		over += fmt.Sprintf("v6: %d present, %d requested", len(cur.V6CidrBlocks), addV6)
	}
	return fmt.Errorf("%w: CidrGroup %s block limit exceeded (%s, limit %d per family)",
		repo.ErrFailedPrecondition, id, over, domain.MaxCidrGroupBlocks)
}

func mergeBlocks(existing, add []string) []string {
	seen := make(map[string]struct{}, len(existing)+len(add))
	out := make([]string, 0, len(existing)+len(add))
	for _, b := range append(append([]string{}, existing...), add...) {
		if _, dup := seen[b]; dup {
			continue
		}
		seen[b] = struct{}{}
		out = append(out, b)
	}
	return out
}

func subtractBlocks(existing, remove []string) []string {
	drop := make(map[string]struct{}, len(remove))
	for _, b := range remove {
		drop[b] = struct{}{}
	}
	out := make([]string, 0, len(existing))
	for _, b := range existing {
		if _, dropped := drop[b]; dropped {
			continue
		}
		out = append(out, b)
	}
	return out
}
