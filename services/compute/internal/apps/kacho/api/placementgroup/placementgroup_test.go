// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package placementgroup

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	rpcstatus "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/PRO-Robotech/kacho/pkg/operations"
	"github.com/PRO-Robotech/kacho/pkg/ownerregister"

	"github.com/PRO-Robotech/kacho/services/compute/internal/domain"
	"github.com/PRO-Robotech/kacho/services/compute/internal/ports"
)

const someGroup = "plg-abcdefghjkmnpqrst"

type fakeGroupRepo struct {
	inserted  *domain.PlacementGroup
	all       []*domain.PlacementGroup
	updateArg ports.PlacementGroupUpdate
	updated   bool
}

func (f *fakeGroupRepo) Get(context.Context, string) (*domain.PlacementGroup, error) {
	return &domain.PlacementGroup{ID: someGroup, ProjectID: "prj1"}, nil
}

func (f *fakeGroupRepo) List(context.Context, string, string, ports.Pagination) ([]*domain.PlacementGroup, string, error) {
	return nil, "", nil
}

// Insert несёт UNIQUE(project_id, name) схемы (миграция 0033_placement_groups).
//
// Дублёр обязан выполнять контракт настоящего: молча приняв вторую строку с тем
// же именем в том же проекте, он скрыл бы ровно тот класс, ради которого его
// подставляют, — умолчание имени, производное НЕ от идентификатора, столкнулось
// бы здесь и больше нигде, а проба про имена осталась бы зелёной.
func (f *fakeGroupRepo) Insert(_ context.Context, g *domain.PlacementGroup) (*domain.PlacementGroup, []ownerregister.Registration, error) {
	for _, x := range f.all {
		if x.ProjectID == g.ProjectID && x.Name == g.Name {
			return nil, nil, ports.ErrAlreadyExists
		}
	}
	f.inserted = g
	f.all = append(f.all, g)
	return g, nil, nil
}

func (f *fakeGroupRepo) Update(_ context.Context, _ string, u ports.PlacementGroupUpdate) (*domain.PlacementGroup, error) {
	f.updateArg, f.updated = u, true
	return &domain.PlacementGroup{ID: someGroup, ProjectID: "prj1"}, nil
}

func (f *fakeGroupRepo) Delete(context.Context, string) error { return nil }

type fakeGeo struct{ zoneErr, regionErr error }

func (f fakeGeo) GetZone(context.Context, string) error                { return f.zoneErr }
func (f fakeGeo) RegionOfZone(context.Context, string) (string, error) { return "ru-central1", nil }
func (f fakeGeo) GetRegion(context.Context, string) error              { return f.regionErr }

type fakeProj struct{}

func (fakeProj) Exists(context.Context, string) (bool, error) { return true, nil }

// memOps — очередь операций в памяти; дублёр обязан выполнять контракт
// настоящего, иначе он скроет именно то, ради чего подставлен.
type memOps struct {
	mu   sync.Mutex
	done map[string]*anypb.Any
	errs map[string]*rpcstatus.Status
}

func newMemOps() *memOps {
	return &memOps{done: map[string]*anypb.Any{}, errs: map[string]*rpcstatus.Status{}}
}

func (m *memOps) Create(context.Context, operations.Operation) error { return nil }
func (m *memOps) CreateWithPrincipal(context.Context, operations.Operation, operations.Principal) error {
	return nil
}

func (m *memOps) Get(context.Context, string) (*operations.Operation, error) {
	return nil, operations.ErrNotFound
}

func (m *memOps) List(context.Context, operations.ListFilter) ([]operations.Operation, string, error) {
	return nil, "", nil
}
func (m *memOps) Cancel(context.Context, string) error { return nil }

func (m *memOps) MarkDone(_ context.Context, id string, resp *anypb.Any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.done[id] = resp
	return nil
}

func (m *memOps) MarkError(_ context.Context, id string, st *rpcstatus.Status) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.errs[id] = st
	return nil
}

func (m *memOps) awaitFinished(t *testing.T, id string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		_, okD := m.done[id]
		_, okE := m.errs[id]
		m.mu.Unlock()
		if okD || okE {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("операция %s не завершилась за 2s", id)
}

func svcWith(r Repo, geo fakeGeo) (*Service, *memOps) {
	ops := newMemOps()
	return NewService(r, ops, fakeProj{}, geo, geo), ops
}

func zonalReq() CreateReq {
	return CreateReq{
		ProjectID: "prj1", Name: "spread-a", Strategy: domain.PlacementStrategySpread,
		PlacementType: domain.PlacementTypeZonal, ZoneID: "ru-central1-a",
	}
}

// TestCreate_AnchorIsExclusiveAndNamedByField — якорь взаимоисключающий, и
// нарушение называется ПО ИМЕНИ ПОЛЯ.
//
// Схема то же самое держит ограничением и держит по-настоящему — мимо неё не
// проходит ни один путь. Но её отказ поля не называет, и вызывающий узнал бы,
// что «что-то не так со строкой», вместо того что он задал две координаты сразу.
func TestCreate_AnchorIsExclusiveAndNamedByField(t *testing.T) {
	cases := []struct {
		name  string
		mutFn func(*CreateReq)
		field string
	}{
		{"зональная без зоны", func(r *CreateReq) { r.ZoneID = "" }, "zoneId"},
		{"зональная с регионом", func(r *CreateReq) { r.RegionID = "ru-central1" }, "regionId"},
		{"якорь не назван", func(r *CreateReq) { r.PlacementType = domain.PlacementTypeUnspecified }, "placementType"},
		{"стратегия не названа", func(r *CreateReq) { r.Strategy = domain.PlacementStrategyUnspecified }, "strategy"},
		{"проект не назван", func(r *CreateReq) { r.ProjectID = "" }, "projectId"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := zonalReq()
			tc.mutFn(&req)
			repo := &fakeGroupRepo{}
			svc, _ := svcWith(repo, fakeGeo{})
			_, err := svc.Create(context.Background(), req)
			if status.Code(err) != codes.InvalidArgument {
				t.Fatalf("код = %v, ожидался InvalidArgument", status.Code(err))
			}
			if !strings.Contains(status.Convert(err).Message(), tc.field) {
				t.Errorf("сообщение %q не называет поле %q", status.Convert(err).Message(), tc.field)
			}
			if repo.inserted != nil {
				t.Error("отвергнутая группа не должна доезжать до хранилища")
			}
		})
	}

	// Положительные контроли — ОБА якоря. Без них отрицания выше зеленели бы на
	// реализации, отвергающей любую группу, и региональная ветвь, у которой зоны
	// нет by construction, выглядела бы рабочей, будучи мёртвой.
	t.Run("зональная группа проходит", func(t *testing.T) {
		repo := &fakeGroupRepo{}
		svc, ops := svcWith(repo, fakeGeo{})
		op, err := svc.Create(context.Background(), zonalReq())
		if err != nil {
			t.Fatalf("законная зональная группа обязана пройти, получено %v", err)
		}
		ops.awaitFinished(t, op.ID)
		if repo.inserted == nil || repo.inserted.ZoneID != "ru-central1-a" || repo.inserted.RegionID != "" {
			t.Errorf("якорь искажён по пути: %+v", repo.inserted)
		}
	})

	t.Run("региональная группа проходит", func(t *testing.T) {
		repo := &fakeGroupRepo{}
		svc, ops := svcWith(repo, fakeGeo{})
		req := CreateReq{
			ProjectID: "prj1", Name: "pack-a", Strategy: domain.PlacementStrategyPack,
			PlacementType: domain.PlacementTypeRegional, RegionID: "ru-central1",
		}
		op, err := svc.Create(context.Background(), req)
		if err != nil {
			t.Fatalf("законная региональная группа обязана пройти, получено %v", err)
		}
		ops.awaitFinished(t, op.ID)
		if repo.inserted == nil || repo.inserted.RegionID != "ru-central1" || repo.inserted.ZoneID != "" {
			t.Errorf("якорь искажён по пути: %+v", repo.inserted)
		}
	})
}

// TestCreate_AnchorIsCheckedAtItsOwner — координата якоря подтверждается у
// владельца Geography, и его отказ доезжает.
func TestCreate_AnchorIsCheckedAtItsOwner(t *testing.T) {
	t.Run("неизвестная зона отвергается", func(t *testing.T) {
		repo := &fakeGroupRepo{}
		svc, _ := svcWith(repo, fakeGeo{zoneErr: ports.ErrNotFound})
		if _, err := svc.Create(context.Background(), zonalReq()); err == nil {
			t.Fatal("зона, которой владелец не знает, обязана быть отвергнута")
		}
		if repo.inserted != nil {
			t.Error("группа с неподтверждённой зоной не должна доезжать до хранилища")
		}
	})

	t.Run("неизвестный регион отвергается", func(t *testing.T) {
		repo := &fakeGroupRepo{}
		svc, _ := svcWith(repo, fakeGeo{regionErr: ports.ErrNotFound})
		req := CreateReq{
			ProjectID: "prj1", Name: "reg-a", Strategy: domain.PlacementStrategySpread,
			PlacementType: domain.PlacementTypeRegional, RegionID: "нет-такого",
		}
		if _, err := svc.Create(context.Background(), req); err == nil {
			t.Fatal("регион, которого владелец не знает, обязан быть отвергнут")
		}
		if repo.inserted != nil {
			t.Error("группа с неподтверждённым регионом не должна доезжать до хранилища")
		}
	})
}

// TestUpdate_StrategyAndAnchorAreImmutable — стратегия и якорь неизменяемы, и
// отказ говорит именно о неизменяемости.
//
// Смена любого из них поменяла бы смысл размещения уже стоящих машин, а
// перекладывать их задним числом мы не будем.
func TestUpdate_StrategyAndAnchorAreImmutable(t *testing.T) {
	for _, f := range []string{"strategy", "placement_type", "zone_id", "region_id"} {
		t.Run(f+" отвергается как неизменяемое", func(t *testing.T) {
			svc, _ := svcWith(&fakeGroupRepo{}, fakeGeo{})
			_, err := svc.Update(context.Background(), UpdateReq{ID: someGroup, UpdateMask: []string{f}})
			if err == nil {
				t.Fatal("правка неизменяемого обязана быть отвергнута")
			}
			if !strings.Contains(status.Convert(err).Message(), "immutable") {
				t.Errorf("сообщение %q не говорит о неизменяемости — вызывающий прочтёт его как «поля нет»",
					status.Convert(err).Message())
			}
		})
	}

	t.Run("неизвестное поле маски отвергается своим текстом", func(t *testing.T) {
		svc, _ := svcWith(&fakeGroupRepo{}, fakeGeo{})
		_, err := svc.Update(context.Background(), UpdateReq{ID: someGroup, UpdateMask: []string{"такого_нет"}})
		if err == nil {
			t.Fatal("неизвестное поле обязано быть отвергнуто")
		}
		if strings.Contains(status.Convert(err).Message(), "immutable") {
			t.Error("неизвестное поле не должно объявляться неизменяемым")
		}
	})

	// Положительный контроль: законная маска доезжает и несёт ровно названное.
	t.Run("законная маска применяется и не трогает неназванное", func(t *testing.T) {
		repo := &fakeGroupRepo{}
		svc, ops := svcWith(repo, fakeGeo{})
		op, err := svc.Update(context.Background(), UpdateReq{
			ID: someGroup, UpdateMask: []string{"name"}, Name: "other-name",
		})
		if err != nil {
			t.Fatalf("законная правка обязана пройти, получено %v", err)
		}
		ops.awaitFinished(t, op.ID)
		if repo.updateArg.Name == nil || *repo.updateArg.Name != "other-name" {
			t.Errorf("имя не доехало: %+v", repo.updateArg)
		}
		if repo.updateArg.LabelsSet || repo.updateArg.Description != nil {
			t.Error("неназванные маской колонки не должны участвовать в записи")
		}
	})
}

// TestGet_MalformedIDRejectedFirst — непригодный идентификатор отвергается
// разбором, а не превращается в «не найдено».
func TestGet_MalformedIDRejectedFirst(t *testing.T) {
	svc, _ := svcWith(&fakeGroupRepo{}, fakeGeo{})
	if _, err := svc.Get(context.Background(), "не идентификатор"); status.Code(err) != codes.InvalidArgument {
		t.Errorf("код = %v, ожидался InvalidArgument", status.Code(err))
	}
	// Положительный контроль: пригодный доходит до хранилища.
	if _, err := svc.Get(context.Background(), someGroup); err != nil {
		t.Fatalf("пригодный идентификатор обязан дойти до хранилища, получено %v", err)
	}
}
