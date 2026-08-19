// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package guestaccesskey

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

// ed25519-ключ в форме, которую понимает гостевая система. Взят настоящим, а не
// собран из похожих байт: разбор — предмет проверки, и правдоподобная подделка
// прошла бы мимо неё.
const validMaterial = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIP1hkYQGqU/1oPRHMbSCPB8Vm+Ap0cJI4KX7BLpFVeMy tenant@example"

// validKeyName — имя ключа, законное по единой форме имени ресурса
// (corevalidate.NameForm).
//
// Здесь стояло «рабочий». Оно было законно, пока имя проверялось ТОЛЬКО длиной,
// — и это ожидание было ошибочным: у соседних ресурсов того же сервиса кириллица
// в имени не принимается. Пробы ниже про разбор материала ключа и про доставку
// маски; имя в них — фикстура, и негодная фикстура роняла бы их по причине, к их
// предмету отношения не имеющей. Форма имени утверждается отдельно, в
// name_canon_test.go.
const validKeyName = "work"

type fakeKeyRepo struct {
	inserted  *domain.GuestAccessKey
	updateArg ports.GuestAccessKeyUpdate
	updated   bool
}

func (f *fakeKeyRepo) Get(context.Context, string) (*domain.GuestAccessKey, error) {
	return &domain.GuestAccessKey{ID: "gak-abcdefghjkmnpqrst", ProjectID: "prj1"}, nil
}

func (f *fakeKeyRepo) List(context.Context, string, string, ports.Pagination) ([]*domain.GuestAccessKey, string, error) {
	return nil, "", nil
}

func (f *fakeKeyRepo) Insert(_ context.Context, k *domain.GuestAccessKey) (*domain.GuestAccessKey, []ownerregister.Registration, error) {
	f.inserted = k
	return k, nil, nil
}

func (f *fakeKeyRepo) Update(_ context.Context, _ string, u ports.GuestAccessKeyUpdate) (*domain.GuestAccessKey, error) {
	f.updateArg, f.updated = u, true
	return &domain.GuestAccessKey{ID: "gak-abcdefghjkmnpqrst", ProjectID: "prj1"}, nil
}

func (f *fakeKeyRepo) Delete(context.Context, string) error { return nil }

// memOps — очередь операций в памяти. Дублёр обязан выполнять контракт
// настоящего: RunOp записывает операцию и запускает работу, поэтому дублёр,
// молча глотающий запись, скрыл бы именно то, ради чего его подставляют.
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

// awaitFinished ждёт завершения работы операции: RunOp запускает её отдельно,
// и проверять её след, не дождавшись, значило бы проверять гонку.
func (m *memOps) awaitFinished(t *testing.T, id string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		_, okDone := m.done[id]
		_, okErr := m.errs[id]
		m.mu.Unlock()
		if okDone || okErr {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("операция %s не завершилась за 2s", id)
}

type fakeProjects struct{ exists bool }

func (f fakeProjects) Exists(context.Context, string) (bool, error) { return f.exists, nil }

func svcWith(r Repo) (*Service, *memOps) {
	ops := newMemOps()
	return NewService(r, ops, fakeProjects{exists: true}, nil), ops
}

// TestCreate_RejectsUnreadableMaterial — материал разбирается ЗДЕСЬ, а не в машине.
//
// Отрицание стоит в паре с положительным контролем: без него «отвергнуто»
// неотличимо от «отвергается всё», и проверка зеленела бы на сломанном разборе,
// не пропускающем ни одного законного ключа.
func TestCreate_RejectsUnreadableMaterial(t *testing.T) {
	t.Run("нечитаемый материал отвергается по имени поля", func(t *testing.T) {
		repo := &fakeKeyRepo{}
		svc, _ := svcWith(repo)
		_, err := svc.Create(context.Background(), CreateReq{
			ProjectID: "prj1", Name: validKeyName, PublicKey: "не ключ вовсе",
		})
		if err == nil {
			t.Fatal("нечитаемый материал обязан быть отвергнут")
		}
		st, _ := status.FromError(err)
		if st.Code() != codes.InvalidArgument {
			t.Errorf("код = %v, ожидался InvalidArgument", st.Code())
		}
		if !strings.Contains(st.Message(), "publicKey") {
			t.Errorf("сообщение %q не называет поле — вызывающему нечего исправлять", st.Message())
		}
		if repo.inserted != nil {
			t.Error("отвергнутый ключ не должен доезжать до хранилища")
		}
	})

	// Положительный контроль: законный материал проходит разбор и получает
	// отпечаток, вычисленный НАМИ.
	t.Run("законный материал принимается и получает наш отпечаток", func(t *testing.T) {
		repo := &fakeKeyRepo{}
		svc, ops := svcWith(repo)
		op, err := svc.Create(context.Background(), CreateReq{
			ProjectID: "prj1", Name: validKeyName, PublicKey: validMaterial,
		})
		if err != nil {
			t.Fatalf("законный материал обязан пройти, получено %v", err)
		}
		ops.awaitFinished(t, op.ID)
		if repo.inserted == nil {
			t.Fatal("законный материал обязан доехать до хранилища")
		}
		if !strings.HasPrefix(repo.inserted.Fingerprint, "SHA256:") {
			t.Errorf("отпечаток %q не в той форме, которую видит арендатор у себя",
				repo.inserted.Fingerprint)
		}
	})
}

// TestUpdate_ImmutableNamedBeforeUnknown — порядок проверок маски.
//
// Неизменяемое поле обязано отвергаться СВОИМ текстом, а не общим «неизвестное
// поле»: известный набор неизменяемых полей не содержит, поэтому обратный
// порядок сказал бы вызывающему, что материала ключа не существует, вместо того
// что его нельзя менять.
func TestUpdate_ImmutableNamedBeforeUnknown(t *testing.T) {
	const id = "gak-abcdefghjkmnpqrst"

	t.Run("материал ключа отвергается как неизменяемый", func(t *testing.T) {
		svc, _ := svcWith(&fakeKeyRepo{})
		_, err := svc.Update(context.Background(), UpdateReq{
			ID: id, UpdateMask: []string{"public_key"},
		})
		if err == nil {
			t.Fatal("правка материала обязана быть отвергнута")
		}
		msg := status.Convert(err).Message()
		if !strings.Contains(msg, "immutable") {
			t.Errorf("сообщение %q не говорит о неизменяемости — вызывающий прочтёт его как «поля нет»", msg)
		}
	})

	t.Run("неизвестное поле отвергается своим текстом", func(t *testing.T) {
		svc, _ := svcWith(&fakeKeyRepo{})
		_, err := svc.Update(context.Background(), UpdateReq{
			ID: id, UpdateMask: []string{"такого_поля_нет"},
		})
		if err == nil {
			t.Fatal("неизвестное поле маски обязано быть отвергнуто")
		}
		if strings.Contains(status.Convert(err).Message(), "immutable") {
			t.Error("неизвестное поле не должно объявляться неизменяемым")
		}
	})

	// Положительный контроль: законная маска доезжает до хранилища и несёт
	// ровно названные колонки.
	t.Run("законная маска применяется и не трогает неназванное", func(t *testing.T) {
		repo := &fakeKeyRepo{}
		svc, ops := svcWith(repo)
		op, err := svc.Update(context.Background(), UpdateReq{
			ID: id, UpdateMask: []string{"name"}, Name: "other-name",
		})
		if err != nil {
			t.Fatalf("законная правка обязана пройти, получено %v", err)
		}
		ops.awaitFinished(t, op.ID)
		if !repo.updated {
			t.Fatal("правка не доехала до хранилища")
		}
		if repo.updateArg.Name == nil || *repo.updateArg.Name != "other-name" {
			t.Errorf("имя не доехало: %+v", repo.updateArg)
		}
		if repo.updateArg.LabelsSet {
			t.Error("метки не назывались маской — колонка не должна участвовать в записи")
		}
	})

	t.Run("пустая маска правит весь изменяемый набор", func(t *testing.T) {
		repo := &fakeKeyRepo{}
		svc, ops := svcWith(repo)
		op, err := svc.Update(context.Background(), UpdateReq{
			ID: id, Name: validKeyName, Labels: map[string]string{"среда": "проба"},
		})
		if err != nil {
			t.Fatalf("правка с пустой маской обязана пройти, получено %v", err)
		}
		ops.awaitFinished(t, op.ID)
		if repo.updateArg.Name == nil || !repo.updateArg.LabelsSet {
			t.Errorf("пустая маска обязана применить весь изменяемый набор: %+v", repo.updateArg)
		}
	})
}

// TestCreate_MalformedIDAndScope — идентификатор и область проверяются до работы.
func TestGet_MalformedIDRejectedFirst(t *testing.T) {
	svc, _ := svcWith(&fakeKeyRepo{})
	_, err := svc.Get(context.Background(), "не идентификатор")
	if err == nil {
		t.Fatal("непригодный идентификатор обязан быть отвергнут")
	}
	if status.Code(err) != codes.InvalidArgument {
		t.Errorf("код = %v, ожидался InvalidArgument (иначе непригодная строка вернёт «не найдено»)",
			status.Code(err))
	}

	// Положительный контроль: пригодный идентификатор доходит до хранилища.
	okSvc, _ := svcWith(&fakeKeyRepo{})
	if _, err := okSvc.Get(context.Background(), "gak-abcdefghjkmnpqrst"); err != nil {
		t.Fatalf("пригодный идентификатор обязан дойти до хранилища, получено %v", err)
	}
}
