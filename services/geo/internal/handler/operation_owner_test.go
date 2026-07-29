// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package handler_test

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/anypb"

	geov1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/geo/v1"
	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"
	"github.com/PRO-Robotech/kacho/pkg/operations"

	"github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/shared/lro"
	"github.com/PRO-Robotech/kacho/services/geo/internal/handler"
	"github.com/PRO-Robotech/kacho/services/geo/internal/repo/kacho/repomock"
)

// seedOwnedOp кладёт в мок незавершённую операцию, созданную principal'ом owner
// (через ctx-WithPrincipal → NewFromContext переносит principal в op.Principal,
// мок сохраняет его). Возвращает op-id.
func seedOwnedOp(t *testing.T, ops *repomock.OpsRepo, owner operations.Principal) string {
	t.Helper()
	ctx := operations.WithPrincipal(context.Background(), owner)
	op, err := operations.NewFromContext(ctx, lro.OperationPrefix, "create region",
		&geov1.CreateRegionMetadata{RegionId: "region-1"})
	if err != nil {
		t.Fatalf("NewFromContext: %v", err)
	}
	if err := ops.Create(ctx, op); err != nil {
		t.Fatalf("ops.Create: %v", err)
	}
	return op.ID
}

// systemRecordedOp кладёт в мок операцию, записанную БЕЗ принципала в ctx —
// ровно так её пишет любой путь, где личности нет (фоновая задача, вызов, не
// прошедший через principal-extract). Строка при этом получает системный
// принципал-fallback {system, bootstrap}: так делает и pgRepo.Create, и мок.
//
// Именно такая строка делает вопрос «владеет ли безымянный запрос» наблюдаемым:
// ключ, выведенный из личности-по-умолчанию, совпал бы с принципалом этой строки
// — и совпал бы с принципалом КАЖДОЙ строки, записанной тем же путём.
func systemRecordedOp(t *testing.T, ops *repomock.OpsRepo) string {
	t.Helper()
	op, err := operations.NewFromContext(context.Background(), lro.OperationPrefix, "create region",
		&geov1.CreateRegionMetadata{RegionId: "region-sys"})
	if err != nil {
		t.Fatalf("NewFromContext: %v", err)
	}
	if err := ops.Create(context.Background(), op); err != nil {
		t.Fatalf("ops.Create: %v", err)
	}
	stored, err := ops.Get(context.Background(), op.ID)
	if err != nil {
		t.Fatalf("ops.Get: %v", err)
	}
	// Предпосылка теста: строка действительно несёт системный принципал-fallback.
	// Если репозиторий однажды начнёт писать сюда что-то другое, тест ниже
	// перестанет спрашивать то, ради чего написан, и обязан сказать об этом сам.
	if want := operations.SystemPrincipal(); stored.Principal.Type != want.Type || stored.Principal.ID != want.ID {
		t.Fatalf("предпосылка теста не выполнена: строка операции несёт принципал %+v, ожидался системный fallback %+v — "+
			"без него запрос без личности не с чем сравнивать и проверка ничего не спрашивает", stored.Principal, want)
	}
	return op.ID
}

// anonymousRecordedOp кладёт в мок операцию, чьи колонки принципала несут САМ
// ЯРЛЫК анонимности (`{system, anonymous}`) — так строка выглядит, когда её
// записали от имени запроса без credential'а. Ключ такой строки совпадает с
// ключом ЛЮБОГО другого безымянного запроса, поэтому без отсечения анонимности
// один безымянный читал и отменял бы операции другого.
func anonymousRecordedOp(t *testing.T, ops *repomock.OpsRepo) string {
	t.Helper()
	op, err := operations.New(lro.OperationPrefix, "create region",
		&geov1.CreateRegionMetadata{RegionId: "region-anon"})
	if err != nil {
		t.Fatalf("operations.New: %v", err)
	}
	if err := ops.CreateWithPrincipal(context.Background(), op, anonymousPrincipal); err != nil {
		t.Fatalf("ops.CreateWithPrincipal: %v", err)
	}
	stored, err := ops.Get(context.Background(), op.ID)
	if err != nil {
		t.Fatalf("ops.Get: %v", err)
	}
	if stored.Principal != anonymousPrincipal {
		t.Fatalf("предпосылка теста не выполнена: строка несёт принципал %+v, ожидался %+v",
			stored.Principal, anonymousPrincipal)
	}
	return op.ID
}

var (
	adminA = operations.Principal{Type: "user", ID: "usr_adminA"}
	adminB = operations.Principal{Type: "user", ID: "usr_adminB"}
	// anonymousPrincipal — «неизвестно кто»: ярлык, а не личность.
	anonymousPrincipal = operations.Principal{Type: "system", ID: operations.AnonymousPrincipalID}
)

// TestOperationHandler_Get_foreignPrincipal_NotFound — BOLA-гейт: caller B не
// владелец операции A → NotFound (no-leak, неотличимо от «нет такой»), НЕ отдаёт
// чужую операцию.
func TestOperationHandler_Get_foreignPrincipal_NotFound(t *testing.T) {
	ops := repomock.NewOpsRepo()
	opID := seedOwnedOp(t, ops, adminA)
	oh := handler.NewOperationHandler(ops)

	ctxB := operations.WithPrincipal(context.Background(), adminB)
	_, err := oh.Get(ctxB, &operationpb.GetOperationRequest{OperationId: opID})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("foreign Get: want NOT_FOUND, got %v", err)
	}
}

// TestOperationHandler_Get_owner_ok — владелец A читает свою операцию.
func TestOperationHandler_Get_owner_ok(t *testing.T) {
	ops := repomock.NewOpsRepo()
	opID := seedOwnedOp(t, ops, adminA)
	oh := handler.NewOperationHandler(ops)

	ctxA := operations.WithPrincipal(context.Background(), adminA)
	op, err := oh.Get(ctxA, &operationpb.GetOperationRequest{OperationId: opID})
	if err != nil {
		t.Fatalf("owner Get err = %v", err)
	}
	if op.GetId() != opID {
		t.Fatalf("owner Get id = %q, want %q", op.GetId(), opID)
	}
}

// TestOperationHandler_Cancel_foreignPrincipal_NotFound — caller B не может
// отменить in-flight операцию A → NotFound (integrity-гейт control-plane).
func TestOperationHandler_Cancel_foreignPrincipal_NotFound(t *testing.T) {
	ops := repomock.NewOpsRepo()
	opID := seedOwnedOp(t, ops, adminA)
	oh := handler.NewOperationHandler(ops)

	ctxB := operations.WithPrincipal(context.Background(), adminB)
	_, err := oh.Cancel(ctxB, &operationpb.CancelOperationRequest{OperationId: opID})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("foreign Cancel: want NOT_FOUND, got %v", err)
	}
	// Операция A должна остаться НЕ отменённой (foreign cancel — no-op).
	ctxA := operations.WithPrincipal(context.Background(), adminA)
	op, err := oh.Get(ctxA, &operationpb.GetOperationRequest{OperationId: opID})
	if err != nil {
		t.Fatalf("owner Get after foreign cancel err = %v", err)
	}
	if op.GetDone() {
		t.Fatalf("operation was cancelled by foreign principal — BOLA integrity breach")
	}
}

// TestOperationHandler_Cancel_owner_ok — владелец A отменяет свою in-flight
// операцию (done=true, CANCELLED).
func TestOperationHandler_Cancel_owner_ok(t *testing.T) {
	ops := repomock.NewOpsRepo()
	opID := seedOwnedOp(t, ops, adminA)
	oh := handler.NewOperationHandler(ops)

	ctxA := operations.WithPrincipal(context.Background(), adminA)
	op, err := oh.Cancel(ctxA, &operationpb.CancelOperationRequest{OperationId: opID})
	if err != nil {
		t.Fatalf("owner Cancel err = %v", err)
	}
	if !op.GetDone() {
		t.Fatalf("owner Cancel: op must be done=true")
	}
}

// TestOperationHandler_Cancel_terminalSuccess_FailedPrecondition — владелец A
// отменяет УЖЕ ЗАВЕРШЁННУЮ УСПЕХОМ (done=true, response set, no error) операцию →
// FailedPrecondition ("already completed"). Проверяет негативную ветку публичного
// RPC (rule #12): CancelOwned → ErrAlreadyDone → codes.FailedPrecondition, а
// терминальное SUCCESS-состояние НЕ перезаписывается на CANCELLED.
func TestOperationHandler_Cancel_terminalSuccess_FailedPrecondition(t *testing.T) {
	ops := repomock.NewOpsRepo()
	opID := seedOwnedOp(t, ops, adminA)

	// Финализируем как SUCCESS (worker.MarkDone) — терминал, не CANCELLED.
	resp, err := anypb.New(&geov1.Region{Id: "region-1"})
	if err != nil {
		t.Fatalf("anypb.New: %v", err)
	}
	if err := ops.MarkDone(context.Background(), opID, resp); err != nil {
		t.Fatalf("MarkDone: %v", err)
	}

	ctxA := operations.WithPrincipal(context.Background(), adminA)
	_, err = handler.NewOperationHandler(ops).Cancel(ctxA, &operationpb.CancelOperationRequest{OperationId: opID})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("Cancel on terminal-SUCCESS op: want FAILED_PRECONDITION, got %v", err)
	}

	// Терминальное состояние не тронуто: остаётся SUCCESS (response set, no error).
	got, gerr := ops.Get(context.Background(), opID)
	if gerr != nil {
		t.Fatalf("Get after failed Cancel: %v", gerr)
	}
	if got.Error != nil {
		t.Fatalf("terminal SUCCESS was overwritten to error — LRO terminal-state integrity breach")
	}
	if got.Response == nil {
		t.Fatalf("terminal SUCCESS response was cleared — integrity breach")
	}
}

// TestOperationHandler_Cancel_idempotentReCancel_ok — владелец A отменяет
// in-flight операцию, затем ПОВТОРНО её отменяет: второй Cancel идемпотентен —
// возвращает ту же операцию (done=true) без ошибки (CancelOwned распознаёт уже-
// CANCELLED). Проверяет идемпотентную ветку, ранее не покрытую.
func TestOperationHandler_Cancel_idempotentReCancel_ok(t *testing.T) {
	ops := repomock.NewOpsRepo()
	opID := seedOwnedOp(t, ops, adminA)
	oh := handler.NewOperationHandler(ops)
	ctxA := operations.WithPrincipal(context.Background(), adminA)

	first, err := oh.Cancel(ctxA, &operationpb.CancelOperationRequest{OperationId: opID})
	if err != nil {
		t.Fatalf("first Cancel err = %v", err)
	}
	if !first.GetDone() {
		t.Fatalf("first Cancel: op must be done=true")
	}

	second, err := oh.Cancel(ctxA, &operationpb.CancelOperationRequest{OperationId: opID})
	if err != nil {
		t.Fatalf("re-Cancel must be idempotent (no error), got %v", err)
	}
	if !second.GetDone() {
		t.Fatalf("re-Cancel: op must remain done=true")
	}
	if second.GetId() != opID {
		t.Fatalf("re-Cancel returned id %q, want same op %q", second.GetId(), opID)
	}
}

// TestOperationHandler_GetCancel_requestWithoutPrincipal_ownsNothing — запрос,
// который никого не называет, не владелец НИЧЕГО: ни читать, ни отменять чужую
// операцию он не вправе, и отказ — тот же no-leak NotFound, что на чужой.
//
// Проверяется именно ветка «личности нет» (`OwnerFromContext` → ok=false), а не
// «такой операции нет»: операция СУЩЕСТВУЕТ, и её принципал — ровно тот, с
// которым безымянный запрос совпал бы, выводи мы ключ владения из
// личности-по-умолчанию (`PrincipalFromContext`) или не отсекай мы анонимность.
// Тогда он прочитал бы и отменил чужую строку. Прежний тест «несуществующий id →
// NotFound» это НЕ ловит: там NotFound приходит от отсутствия строки и остаётся
// зелёным при снятой ветке.
//
// Все безымянные случаи обязаны вести себя одинаково (pkg/operations
// Principal.IsAnonymous): ctx вообще без принципала, ctx с явно снятым
// принципалом (transport снял носителя у недоверенного отправителя) и ctx с
// именованной анонимностью `{system, anonymous}`, которую edge выдаёт запросу
// без credential'а. Последний опаснее прочих: пара непуста, поэтому как ключ
// владения она выглядит настоящей и совпадает САМА С СОБОЙ — любые два
// безымянных запроса делили бы один ключ.
//
// У каждого случая своя посевная строка — ровно та, с которой его ключ совпал
// бы при снятой ветке. Иначе подслучай зеленел бы вакуумно: ключ, которому не с
// чем совпасть, отвергается и без всякой проверки.
func TestOperationHandler_GetCancel_requestWithoutPrincipal_ownsNothing(t *testing.T) {
	anonymous := map[string]struct {
		ctx context.Context
		// seed кладёт строку, чей принципал совпал бы с ключом этого ctx, если
		// бы ключ выводили из личности-по-умолчанию / без отсечения анонимности.
		seed func(*testing.T, *repomock.OpsRepo) string
	}{
		"ctx без принципала": {
			ctx:  context.Background(),
			seed: systemRecordedOp,
		},
		"ctx с явно снятым принципалом": {
			ctx:  operations.WithoutPrincipal(operations.WithPrincipal(context.Background(), adminA)),
			seed: systemRecordedOp,
		},
		"ctx с именованной анонимностью": {
			ctx: operations.WithPrincipal(context.Background(), anonymousPrincipal),
			// Строка, чьи колонки принципала несут сам ярлык анонимности. Такую
			// строку пишет любой путь, передающий принципал явно, — и именно на
			// ней видно, что «неизвестно кто» совпадает с «неизвестно кем».
			seed: anonymousRecordedOp,
		},
	}

	for name, tc := range anonymous {
		t.Run(name, func(t *testing.T) {
			ops := repomock.NewOpsRepo()
			opID := tc.seed(t, ops)
			ctx := tc.ctx
			oh := handler.NewOperationHandler(ops)

			got, err := oh.Get(ctx, &operationpb.GetOperationRequest{OperationId: opID})
			if status.Code(err) != codes.NotFound {
				t.Errorf("Get(%s) вернул %v, ожидался NOT_FOUND: запрос, который никого не называет, "+
					"не владеет уже записанной операцией %s", name, err, opID)
			}
			if got != nil {
				t.Errorf("Get(%s) отдал тело чужой операции: %+v", name, got)
			}

			gotCancel, err := oh.Cancel(ctx, &operationpb.CancelOperationRequest{OperationId: opID})
			if status.Code(err) != codes.NotFound {
				t.Errorf("Cancel(%s) вернул %v, ожидался NOT_FOUND", name, err)
			}
			if gotCancel != nil {
				t.Errorf("Cancel(%s) отдал тело чужой операции: %+v", name, gotCancel)
			}

			// Наблюдаемое следствие, а не только код ответа: операция осталась
			// незавершённой — безымянный запрос её не отменил.
			after, gerr := ops.Get(context.Background(), opID)
			if gerr != nil {
				t.Fatalf("ops.Get после отказа: %v", gerr)
			}
			if after.Done {
				t.Errorf("операция %s отменена запросом (%s), который никого не называет — integrity-брешь control-plane", opID, name)
			}
		})
	}
}

// TestOperationHandler_GetCancel_explicitSystemPrincipal_ownsItsOperations —
// та же ФОРМА (системный принципал), но установленный ЯВНО на доверенном
// internal-пути. Он — настоящая bootstrap-личность и остаётся владельцем своих
// операций: гейт сужает анонимность, а НЕ bootstrap-личность как таковую.
//
// Этот кейс — вторая сторона проверки выше: без него запрет было бы «легче»
// удовлетворить, отвергая любой системный принципал, и гейт ловил бы форму
// вместо существа.
func TestOperationHandler_GetCancel_explicitSystemPrincipal_ownsItsOperations(t *testing.T) {
	ops := repomock.NewOpsRepo()
	opID := systemRecordedOp(t, ops)
	oh := handler.NewOperationHandler(ops)

	ctxSys := operations.WithPrincipal(context.Background(), operations.SystemPrincipal())

	op, err := oh.Get(ctxSys, &operationpb.GetOperationRequest{OperationId: opID})
	if err != nil {
		t.Fatalf("явно установленный системный принципал обязан читать СВОЮ операцию, получено: %v", err)
	}
	if op.GetId() != opID {
		t.Fatalf("Get вернул id %q, want %q", op.GetId(), opID)
	}

	cancelled, err := oh.Cancel(ctxSys, &operationpb.CancelOperationRequest{OperationId: opID})
	if err != nil {
		t.Fatalf("явно установленный системный принципал обязан отменять СВОЮ операцию, получено: %v", err)
	}
	if !cancelled.GetDone() {
		t.Fatalf("Cancel: операция обязана стать done=true")
	}
}

// bareOpsRepo реализует ТОЛЬКО operations.Repo (не OwnedOperationRepo) — модель
// wiring-ошибки. Handler обязан fail-closed (INTERNAL), а не молча пропустить
// ownership-проверку.
type bareOpsRepo struct{ operations.Repo }

// TestOperationHandler_Get_repoWithoutOwned_failClosed — если repo не
// реализует OwnedOperationRepo → INTERNAL (не silent-bypass ownership).
func TestOperationHandler_Get_repoWithoutOwned_failClosed(t *testing.T) {
	oh := handler.NewOperationHandler(bareOpsRepo{Repo: repomock.NewOpsRepo()})
	_, err := oh.Get(context.Background(), &operationpb.GetOperationRequest{OperationId: "x"})
	if status.Code(err) != codes.Internal {
		t.Fatalf("bare repo Get: want INTERNAL (fail-closed), got %v", err)
	}
	_, err = oh.Cancel(context.Background(), &operationpb.CancelOperationRequest{OperationId: "x"})
	if status.Code(err) != codes.Internal {
		t.Fatalf("bare repo Cancel: want INTERNAL (fail-closed), got %v", err)
	}
}
