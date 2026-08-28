// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package opsproxy_test

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/anypb"

	rpcstatus "google.golang.org/genproto/googleapis/rpc/status"

	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"

	"github.com/PRO-Robotech/kacho/gateway/internal/opsproxy"
	"github.com/PRO-Robotech/kacho/gateway/internal/principalmeta"
	"github.com/PRO-Robotech/kacho/pkg/operations"
	"github.com/PRO-Robotech/kacho/pkg/operations/operationspb"
)

// Какой исход даёт полоса «отмена ЧУЖОЙ операции» — установлено ОПЫТОМ.
//
// # Зачем проба, если есть чтение кода
//
// Кейс `AZD-OP-CANCEL-NON-CREATOR-DENIED` набора nlb принимал `oneOf([400, 403,
// 404])`, а заголовок обещал `PERMISSION_DENIED`. Допуск на исход, которого
// полоса не производит, не краснеет НИКОГДА (`e2e-flow.md` §3), и чтением кода
// это не закрывалось: вопрос был о ПОРЯДКЕ проверок на крае, а порядок —
// свойство исполнения, не текста. Задача #1403 предполагала прогон на стенде;
// проба даёт то же утверждение детерминированно, входит в конвейер и не зависит
// ни от окна материализации прав, ни от занятости стенда.
//
// # Почему полоса собрана целиком, а не заглушкой
//
// Соседняя проба (`cancel_authorizes_before_mutating_test.go`) держит дублёр
// бэкенда, чей `Get` отдаёт операцию КОМУ УГОДНО. Для её предмета (край не зовёт
// Cancel до решения о доступе) этого довольно, а здесь такой дублёр
// СНИСХОДИТЕЛЬНЕЕ продукта ровно в том месте, о котором вопрос: он не сужает
// чтение по владельцу, и полоса свернула бы не туда. Поэтому бэкендом здесь
// стоит НАСТОЯЩИЙ общий обработчик `operationspb.Handler`, а предикат владения
// живёт в репозитории — там же, где он живёт у продукта (в SQL `WHERE`).
//
// # Что проба утверждает
//
//  1. Чужому субъекту приходит `NOT_FOUND`, а не `PERMISSION_DENIED`: чтение
//     операции сужено по владельцу и отказывает ДО проверки владения на крае.
//     Значит 403 на этом шаге производителя не имеет.
//  2. `FAILED_PRECONDITION` «уже завершена» тоже недостижим: до `Cancel` дело не
//     доходит — и это проверяется счётчиком, а не кодом ответа.
//  3. Текст отказа побайтово равен тому, что видит владелец несуществующей
//     операции: различимый отказ здесь был бы оракулом существования
//     (`security.md` §Hardening #6).
//  4. Положительный контроль: СОЗДАТЕЛЬ ту же операцию отменяет успешно — иначе
//     отрицание зеленело бы на полосе, где отмена сломана для всех.

// opsBackendDomain — домен, под которым край держит соединение к бэкенду этой
// полосы. Он НЕ равен префиксу идентификатора операции, и различие несущее:
// первая редакция пробы подключила бэкенд под именем префикса, край не нашёл
// соединения и ответил `NotFound` СВОЕЙ веткой «бэкенд не подключён» — тем же
// кодом и тем же текстом, что и отказ владельца. Отрицательная проба зеленела,
// не дойдя до предмета вовсе; вскрыл это ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ ниже, а не
// внимательность.
const opsBackendDomain = "loadbalancer"

// ownedOpsRepo — репозиторий с предикатом владения. Реализует ту часть
// контракта `operations.Repo` и `operations.OwnedOperationRepo`, которой
// пользуется общий обработчик; предикат — та же пара (тип, идентификатор)
// принципала, что уходит в SQL `WHERE` у продукта.
type ownedOpsRepo struct {
	ops         map[string]*operations.Operation
	owners      map[string]operations.Owner
	cancelCalls atomic.Int64
}

func (r *ownedOpsRepo) Create(context.Context, operations.Operation) error { return nil }
func (r *ownedOpsRepo) CreateWithPrincipal(context.Context, operations.Operation, operations.Principal) error {
	return nil
}

func (r *ownedOpsRepo) Get(_ context.Context, id string) (*operations.Operation, error) {
	op, ok := r.ops[id]
	if !ok {
		return nil, operations.ErrNotFound
	}
	return op, nil
}

func (r *ownedOpsRepo) List(context.Context, operations.ListFilter) ([]operations.Operation, string, error) {
	return nil, "", nil
}

// Несуженные MarkDone/MarkError/Cancel общий обработчик на этой полосе не зовёт
// — они есть в контракте репозитория ради фоновых путей. Реализованы отказом:
// молчаливый успех сделал бы дублёр СНИСХОДИТЕЛЬНЕЕ продукта ровно в том месте,
// про которое проба (`e2e-flow.md` §5).
func (r *ownedOpsRepo) MarkDone(context.Context, string, *anypb.Any) error {
	return errors.New("MarkDone на этой полосе не вызывается")
}

func (r *ownedOpsRepo) MarkError(context.Context, string, *rpcstatus.Status) error {
	return errors.New("MarkError на этой полосе не вызывается")
}

// Cancel — НЕСУЖЕННЫЙ, без предиката владения. Его вызов на арендаторской полосе
// и есть та дыра, ради которой заведён `CancelOwned`; поэтому здесь он отказывает
// громко, а не выполняет отмену.
func (r *ownedOpsRepo) Cancel(context.Context, string) error {
	return errors.New("несуженный Cancel на арендаторской полосе — обход владения")
}

// GetOwned — чужая ИЛИ несуществующая операция дают ОДИН И ТОТ ЖЕ `ErrNotFound`:
// «есть, но не твоя» обязано быть неотличимо от «нет такой».
func (r *ownedOpsRepo) GetOwned(_ context.Context, id string, owner operations.Owner) (*operations.Operation, error) {
	op, ok := r.ops[id]
	if !ok || r.owners[id] != owner {
		return nil, operations.ErrNotFound
	}
	return op, nil
}

func (r *ownedOpsRepo) CancelOwned(_ context.Context, id string, owner operations.Owner) (*operations.Operation, error) {
	r.cancelCalls.Add(1)
	op, ok := r.ops[id]
	if !ok || r.owners[id] != owner {
		return nil, operations.ErrNotFound
	}
	if op.Done {
		return nil, operations.ErrAlreadyDone
	}
	cancelled := *op
	cancelled.Done = true
	return &cancelled, nil
}

func (r *ownedOpsRepo) ListOwned(context.Context, operations.ListFilter, operations.Owner) ([]operations.Operation, string, error) {
	return nil, "", nil
}

// principalFromMetadata — то же, что делает интерсептор сервиса: переносит
// переданную личность из метаданных запроса в контекст. Без него общий
// обработчик увидел бы безымянного вызывающего и отказал бы ВСЕМ — проба
// зеленела бы, ничего не проверив.
func principalFromMetadata(
	ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler,
) (any, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	get := func(k string) string {
		if v := md.Get(k); len(v) > 0 {
			return v[0]
		}
		return ""
	}
	t, id := get(principalmeta.MetaPrincipalType), get(principalmeta.MetaPrincipalID)
	if t != "" && id != "" {
		ctx = operations.WithPrincipal(ctx, operations.Principal{Type: t, ID: id})
	}
	return handler(ctx, req)
}

func setupOwnedBackend(t *testing.T, repo *ownedOpsRepo) *grpc.ClientConn {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := grpc.NewServer(grpc.UnaryInterceptor(principalFromMetadata))
	operationpb.RegisterOperationServiceServer(srv, operationspb.NewHandler(repo))
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.GracefulStop)

	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func TestCancelOfAnotherSubjectsOperationYieldsNotFoundOnly(t *testing.T) {
	const opID = "nlb00000000000000001"
	repo := &ownedOpsRepo{
		ops:    map[string]*operations.Operation{opID: {ID: opID, Done: false, Principal: operations.Principal{Type: "user", ID: "usr-a"}}},
		owners: map[string]operations.Owner{opID: {PrincipalType: "user", PrincipalID: "usr-a"}},
	}
	conn := setupOwnedBackend(t, repo)
	proxy := opsproxy.New(map[string]*grpc.ClientConn{opsBackendDomain: conn})

	// Субъект B — не создатель операции.
	ctxB := metadata.NewIncomingContext(
		operations.WithPrincipal(context.Background(), operations.Principal{Type: "user", ID: "usr-b"}),
		metadata.Pairs(
			principalmeta.MetaPrincipalType, "user",
			principalmeta.MetaPrincipalID, "usr-b",
		))

	_, err := proxy.Cancel(ctxB, &operationpb.CancelOperationRequest{OperationId: opID})
	if err == nil {
		t.Fatal("чужой субъект отменил операцию — запрет не работает вовсе")
	}
	st := status.Convert(err)
	if st.Code() != codes.NotFound {
		t.Fatalf("исход полосы — %s (%q), а кейс объявлял законными ещё PERMISSION_DENIED и "+
			"FAILED_PRECONDITION. Если код сменился, допуск кейса "+
			"AZD-OP-CANCEL-NON-CREATOR-DENIED обязан смениться вместе с ним",
			st.Code(), st.Message())
	}
	// Побайтовое равенство отказу владельца несуществующей операции: различимый
	// текст здесь — оракул существования.
	if want := "operation " + opID + " not found"; st.Message() != want {
		t.Fatalf("текст отказа %q, а отказ «нет такой операции» звучит %q — по различию "+
			"отличают «нет доступа» от «не существует»", st.Message(), want)
	}
	// FAILED_PRECONDITION «уже завершена» недостижим не потому, что операция
	// не завершена, а потому, что до отмены дело НЕ ДОХОДИТ. Утверждается это
	// счётчиком: код ответа об этом не говорит ничего.
	if n := repo.cancelCalls.Load(); n != 0 {
		t.Fatalf("отмена дошла до репозитория %d раз — решение о доступе принято ПОСЛЕ мутации", n)
	}
}

// Положительный контроль: создатель ту же операцию отменяет. Без него отрицание
// выше зеленело бы на полосе, где отмена сломана для всех.
func TestCancelByTheCreatorSucceedsOnTheSameLane(t *testing.T) {
	const opID = "nlb00000000000000001"
	repo := &ownedOpsRepo{
		ops:    map[string]*operations.Operation{opID: {ID: opID, Done: false, Principal: operations.Principal{Type: "user", ID: "usr-a"}}},
		owners: map[string]operations.Owner{opID: {PrincipalType: "user", PrincipalID: "usr-a"}},
	}
	conn := setupOwnedBackend(t, repo)
	proxy := opsproxy.New(map[string]*grpc.ClientConn{opsBackendDomain: conn})

	ctxA := metadata.NewIncomingContext(
		operations.WithPrincipal(context.Background(), operations.Principal{Type: "user", ID: "usr-a"}),
		metadata.Pairs(
			principalmeta.MetaPrincipalType, "user",
			principalmeta.MetaPrincipalID, "usr-a",
		))

	if _, err := proxy.Cancel(ctxA, &operationpb.CancelOperationRequest{OperationId: opID}); err != nil {
		t.Fatalf("создатель не смог отменить СВОЮ операцию: %v — тогда отрицание выше "+
			"не различает запрет и поломку", err)
	}
	if n := repo.cancelCalls.Load(); n != 1 {
		t.Fatalf("отмена создателя дошла до репозитория %d раз, ожидался 1", n)
	}
}
