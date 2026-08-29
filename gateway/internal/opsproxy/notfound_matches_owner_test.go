// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// notfound_matches_owner_test.go — отказ «нет такой операции», произведённый
// КРАЕМ, обязан совпадать с отказом ВЛАДЕЛЬЦА побайтово.
//
// # Предмет
//
// На одном и том же адресе `/operations/{id}` клиент получает 404 из двух
// разных мест: владелец отвечает так на строку, которой у него нет (или которая
// не его — «есть, но не твоя» и «нет такой» у него намеренно неразличимы), а
// край — на id с известным префиксом, чей backend ему не подключён. Если тексты
// различаются хоть одним байтом, по этому различию отличают «нет доступа» от
// «не существует» и заодно читают, какие backend'ы край держит подключёнными,
// — то есть ровно то, что сокрытие и должно было закрыть
// (`security.md` §Hardening-инварианты, п. 6).
//
// # Почему сравниваются ИСХОДЫ, а не литералы
//
// Проба ведёт обе стороны их настоящими путями: владельца — `operationspb.Handler`
// с репозиторием, отвечающим `operations.ErrNotFound`; край — `opsproxy.Get`/
// `Cancel` на ветке «префикс известен, backend не подключён». Сравнение двух
// литералов доказывало бы согласие двух записей, а не согласие двух ответов:
// первый же посредник, меняющий текст по дороге, остался бы невидим.
//
// # Положительный контроль
//
// Утверждение об одинаковости зеленело бы и на двух пустых сообщениях, поэтому
// каждая сторона отдельно обязана отдать NotFound, непустой текст и в нём —
// тот id, который прислал вызывающий.
package opsproxy_test

import (
	"context"
	"strings"
	"testing"

	rpcstatus "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/anypb"

	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"
	"github.com/PRO-Robotech/kacho/pkg/operations"
	"github.com/PRO-Robotech/kacho/pkg/operations/operationspb"

	"github.com/PRO-Robotech/kacho/gateway/internal/opsproxy"
)

// missingRowRepo — репозиторий владельца, у которого запрошенной строки нет.
// Ownership-апгрейд реализован: без него обработчик отвечает INTERNAL, и проба
// мерила бы не тот исход.
type missingRowRepo struct{}

func (missingRowRepo) Create(context.Context, operations.Operation) error { return nil }
func (missingRowRepo) CreateWithPrincipal(context.Context, operations.Operation, operations.Principal) error {
	return nil
}
func (missingRowRepo) Get(context.Context, string) (*operations.Operation, error) {
	return nil, operations.ErrNotFound
}
func (missingRowRepo) List(context.Context, operations.ListFilter) ([]operations.Operation, string, error) {
	return nil, "", nil
}
func (missingRowRepo) MarkDone(context.Context, string, *anypb.Any) error         { return nil }
func (missingRowRepo) MarkError(context.Context, string, *rpcstatus.Status) error { return nil }
func (missingRowRepo) Cancel(context.Context, string) error                       { return nil }

func (missingRowRepo) GetOwned(context.Context, string, operations.Owner) (*operations.Operation, error) {
	return nil, operations.ErrNotFound
}

func (missingRowRepo) CancelOwned(context.Context, string, operations.Owner) (*operations.Operation, error) {
	return nil, operations.ErrNotFound
}

func (missingRowRepo) ListOwned(context.Context, operations.ListFilter, operations.Owner) ([]operations.Operation, string, error) {
	return nil, "", nil
}

// notFoundFromOwner — текст, который на этот id отдаёт НАСТОЯЩИЙ отказ владельца.
// Один обработчик обслуживает все семь доменов, поэтому «текст владельца» здесь
// не гипотеза о конкретном сервисе, а единственная форма, доступная вызывающему.
func notFoundFromOwner(t *testing.T, id string) string {
	t.Helper()
	h := operationspb.NewHandler(missingRowRepo{})
	ctx := operations.WithPrincipal(context.Background(),
		operations.Principal{Type: "user", ID: "usr-1"})
	_, err := h.Get(ctx, &operationpb.GetOperationRequest{OperationId: id})
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("владелец вернул не gRPC-ошибку: %v", err)
	}
	if st.Code() != codes.NotFound {
		t.Fatalf("владелец на отсутствующей строке ответил %s, ожидался NOT_FOUND", st.Code())
	}
	if st.Message() == "" {
		t.Fatal("владелец отдал пустой текст — сравнивать нечего")
	}
	if !strings.Contains(st.Message(), id) {
		t.Fatalf("текст владельца %q не несёт id %q — положительный контроль не выполнен", st.Message(), id)
	}
	return st.Message()
}

// edgeNotFound — исход края на ветке «префикс известен, backend не подключён».
func edgeNotFound(t *testing.T, call func(*opsproxy.OpsProxy, string) error, id string) *status.Status {
	t.Helper()
	// Подключён ТОЛЬКО compute; спрашиваем vpc-операцию — её backend отсутствует.
	computeConn := setupMockBackend(t, map[string]*operationpb.Operation{})
	proxy := opsproxy.New(map[string]*grpc.ClientConn{"compute": computeConn})

	err := call(proxy, id)
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("край вернул не gRPC-ошибку: %v", err)
	}
	if st.Code() != codes.NotFound {
		t.Fatalf("край ответил %s, ожидался NOT_FOUND", st.Code())
	}
	if st.Message() == "" {
		t.Fatal("край отдал пустой текст — сравнивать нечего")
	}
	if !strings.Contains(st.Message(), id) {
		t.Fatalf("текст края %q не несёт id %q — положительный контроль не выполнен", st.Message(), id)
	}
	return st
}

// TestEdgeNotFoundIsByteIdenticalToOwners — обе стороны говорят ОДНО И ТО ЖЕ.
//
// Сравниваются и код, и сообщение: они ломаются независимо, и различимым
// ответ становится от любого из двух.
func TestEdgeNotFoundIsByteIdenticalToOwners(t *testing.T) {
	const id = "enp0123456789abcdefg" // известный краю префикс vpc

	owner := notFoundFromOwner(t, id)

	for _, c := range []struct {
		name string
		call func(*opsproxy.OpsProxy, string) error
	}{
		{
			name: "Get",
			call: func(p *opsproxy.OpsProxy, id string) error {
				_, err := p.Get(context.Background(), &operationpb.GetOperationRequest{OperationId: id})
				return err
			},
		},
		{
			name: "Cancel",
			call: func(p *opsproxy.OpsProxy, id string) error {
				_, err := p.Cancel(context.Background(), &operationpb.CancelOperationRequest{OperationId: id})
				return err
			},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			edge := edgeNotFound(t, c.call, id)
			if edge.Message() != owner {
				t.Fatalf("отказ края отличим от отказа владельца:\n  край:     %q\n  владелец: %q",
					edge.Message(), owner)
			}
		})
	}
}
