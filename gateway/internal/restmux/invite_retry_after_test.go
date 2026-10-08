// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package restmux

// invite_retry_after_test.go — срок повтора отказа приглашения доходит до
// вызывающего заголовком `Retry-After` (приёмка NTF-2, сценарий NTF2-69 (е);
// замысел `issue-2917` З24 (2); полоса E1b задачи kacho#2917).
//
// ПРЕДМЕТ. Служба доступа на отказе `INVITATION_RATE_LIMITED` кладёт срок
// повтора в метаданные ответа gRPC под ключом `retry-after` (целое число
// секунд, `grpc.SetHeader` до возврата статуса — полоса S5 kaname#484).
// `POST /iam/v1/users:invite` край отдаёт шлюзом gRPC, и обработчик ошибок
// шлюза переносит метаданные ответа в HTTP-заголовки сопоставителем исходящих
// заголовков мультиплексора. Умолчание шлюза пишет КАЖДЫЙ ключ с приставкой
// `Grpc-Metadata-`, поэтому без своего сопоставителя вызывающий получает
// `Grpc-Metadata-Retry-After`, а заголовка `Retry-After`, который обещает
// приёмка, нет ни в одном ответе края на этом пути.
//
// ЧТО УТВЕРЖДАЕТСЯ. Край с боевым мультиплексором (`NewMux`) против
// службы-дублёра:
//
//  1. Отказ `RESOURCE_EXHAUSTED` с метаданными `retry-after: 7` → HTTP `429`
//     (статус по-прежнему из `runtime.HTTPStatusFromCode` — своего обработчика
//     ошибок край не заводит), тело — `code` 8, дословный текст отказа и
//     `reason: INVITATION_RATE_LIMITED`, заголовок `Retry-After: 7` ровно одним
//     значением, а прежней формы `Grpc-Metadata-Retry-After` нет: у срока
//     повтора одно представление.
//  2. ЗАКОННЫЙ БЛИЗНЕЦ — тот же отказ, у которого изменён ровно один факт: ключ
//     метаданных другой. Он обязан уйти прежней формой `Grpc-Metadata-<ключ>`,
//     а заголовка `Retry-After` быть не должно: переносится РОВНО ключ
//     `retry-after`, а не «все метаданные без приставки».
//  3. ВТОРОЙ БЛИЗНЕЦ — пропущенное приглашение без ключа: `200` с телом
//     `Operation`, заголовка `Retry-After` нет.
//
// ГРАНИЦА. Что служба кладёт в метаданные и как считает секунды — её приёмка
// и её пробы (NTF2-69 (а), (б) на П6-k). Сквозной исход через собранный край
// на стенде — NTF2-69 (е) уровнем E на П1 (коллекция newman полосы X1).

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	operationv1 "github.com/PRO-Robotech/corelib/api/corelib/operation"
	iampb "github.com/PRO-Robotech/kaname/pkg/api/kaname/cloud/iam/v1"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	// ntf269eRefusalText — текст отказа из «Тогда» NTF2-69 (е), дословно.
	ntf269eRefusalText = "invitation limit of the account is exhausted"
	// ntf269eReason — причина отказа из «Тогда» NTF2-69 (е).
	ntf269eReason = "INVITATION_RATE_LIMITED"
	// ntf269eErrorDomain — домен ErrorInfo дублёра: заведомо не настоящий,
	// предмет пробы — перенос срока, а не домен службы.
	ntf269eErrorDomain = "ntf269e.probe.invalid"
	// ntf269eRetrySeconds — срок повтора, который кладёт дублёр.
	ntf269eRetrySeconds = "7"
	// ntf269eOtherKey — ключ метаданных законного близнеца.
	ntf269eOtherKey = "x-ntf269e-probe"

	ntf269eOperationID = "iop0ntf269e0edge0e1b"

	// Адреса тела выбирают ответ дублёра; по одному на исход.
	ntf269eEmailOverCap   = "ntf2-69e-over-cap@probe.invalid"
	ntf269eEmailOtherKey  = "ntf2-69e-other-key@probe.invalid"
	ntf269eEmailWithinCap = "ntf2-69e-within-cap@probe.invalid"
)

// inviteRefusalDouble — дублёр службы доступа: на `Invite` отвечает тем, что
// служба отдаёт на исчерпанном потолке приглашений (метаданные ответа ставятся
// `grpc.SetHeader` ДО возврата статуса — как в замысле З24 (1)), либо
// операцией для приглашения в пределах потолков.
type inviteRefusalDouble struct {
	iampb.UnimplementedUserServiceServer
}

func (inviteRefusalDouble) Invite(ctx context.Context, in *iampb.InviteUserRequest) (*operationv1.Operation, error) {
	var md metadata.MD
	switch in.GetEmail() {
	case ntf269eEmailOverCap:
		md = metadata.Pairs("retry-after", ntf269eRetrySeconds)
	case ntf269eEmailOtherKey:
		md = metadata.Pairs(ntf269eOtherKey, ntf269eRetrySeconds)
	case ntf269eEmailWithinCap:
		return &operationv1.Operation{Id: ntf269eOperationID}, nil
	default:
		return nil, status.Errorf(codes.InvalidArgument, "ntf269e double: unexpected email %q", in.GetEmail())
	}
	if err := grpc.SetHeader(ctx, md); err != nil {
		return nil, status.Errorf(codes.Internal, "ntf269e double: SetHeader: %v", err)
	}
	st, err := status.New(codes.ResourceExhausted, ntf269eRefusalText).WithDetails(&errdetails.ErrorInfo{
		Reason: ntf269eReason,
		Domain: ntf269eErrorDomain,
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "ntf269e double: WithDetails: %v", err)
	}
	return nil, st.Err()
}

func startInviteRefusalDouble(t *testing.T) string {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	gs := grpc.NewServer()
	iampb.RegisterUserServiceServer(gs, inviteRefusalDouble{})
	go func() { _ = gs.Serve(lis) }()
	t.Cleanup(gs.Stop)
	return lis.Addr().String()
}

// inviteThroughEdge — `POST /iam/v1/users:invite` через боевой мультиплексор
// края, бэкенд службы доступа — дублёр.
func inviteThroughEdge(t *testing.T, email string) *httptest.ResponseRecorder {
	t.Helper()
	addrs := geoMuxAddrs()
	addrs["iam"] = startInviteRefusalDouble(t)
	h, err := NewMux(context.Background(), addrs, nil, nil)
	if err != nil {
		t.Fatalf("NewMux: %v", err)
	}
	body := `{"accountId":"acc0ntf269e0edge0e1b","email":"` + email + `"}`
	req := httptest.NewRequest(http.MethodPost, "/iam/v1/users:invite", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// refusalBody — то, что вызывающий читает в теле отказа края.
type refusalBody struct {
	Code    int              `json:"code"`
	Message string           `json:"message"`
	Details []map[string]any `json:"details"`
}

// assertInviteRefusalShape — форма отказа NTF2-69 (е) без заголовка срока:
// `429`, `code` 8, дословный текст, `reason`. Общая для предмета и близнеца:
// они различаются только ключом метаданных.
func assertInviteRefusalShape(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("http %d, want 429 (runtime.HTTPStatusFromCode(RESOURCE_EXHAUSTED)); body %s", w.Code, w.Body.String())
	}
	var b refusalBody
	if err := json.Unmarshal(w.Body.Bytes(), &b); err != nil {
		t.Fatalf("тело отказа не JSON: %v; body %s", err, w.Body.String())
	}
	if b.Code != int(codes.ResourceExhausted) {
		t.Errorf("code %d, want %d; body %s", b.Code, codes.ResourceExhausted, w.Body.String())
	}
	if b.Message != ntf269eRefusalText {
		t.Errorf("message %q, want %q", b.Message, ntf269eRefusalText)
	}
	reasons := 0
	for _, d := range b.Details {
		if d["reason"] == ntf269eReason {
			reasons++
		}
	}
	if reasons != 1 {
		t.Errorf("reason %s в details %d раз, want 1; body %s", ntf269eReason, reasons, w.Body.String())
	}
}

// TestUserInvite_NTF269e_RetryAfterReachesTheHeader — предмет: срок повтора
// отказа приглашения доходит до вызывающего заголовком `Retry-After`.
func TestUserInvite_NTF269e_RetryAfterReachesTheHeader(t *testing.T) {
	w := inviteThroughEdge(t, ntf269eEmailOverCap)
	assertInviteRefusalShape(t, w)

	got := w.Header().Values("Retry-After")
	if len(got) != 1 || got[0] != ntf269eRetrySeconds {
		t.Errorf("заголовок Retry-After %q, want ровно [%q]: срок повтора из метаданных ответа службы "+
			"(ключ retry-after) не перенесён в заголовок — у публичного мультиплексора края нет "+
			"сопоставителя исходящих заголовков; заголовки ответа: %v",
			got, ntf269eRetrySeconds, w.Header())
	}
	if prefixed := w.Header().Values("Grpc-Metadata-Retry-After"); len(prefixed) != 0 {
		t.Errorf("срок повтора отдан прежней формой Grpc-Metadata-Retry-After %q: перенос в Retry-After "+
			"обязан её заменить, а не дополнить — у одного значения одно представление", prefixed)
	}
}

// TestUserInvite_NTF269e_OtherMetadataKeyKeepsItsPrefixedForm — законный
// близнец: тот же отказ, иной ключ метаданных. Переносится РОВНО `retry-after`;
// прочие ключи остаются прежней формой `Grpc-Metadata-<ключ>`.
func TestUserInvite_NTF269e_OtherMetadataKeyKeepsItsPrefixedForm(t *testing.T) {
	w := inviteThroughEdge(t, ntf269eEmailOtherKey)
	assertInviteRefusalShape(t, w)

	if got := w.Header().Values("Retry-After"); len(got) != 0 {
		t.Errorf("заголовок Retry-After %q на ключе %q: переносится не ровно ключ retry-after", got, ntf269eOtherKey)
	}
	if got := w.Header().Values("Grpc-Metadata-" + ntf269eOtherKey); len(got) != 1 || got[0] != ntf269eRetrySeconds {
		t.Errorf("ключ %q ушёл не прежней формой Grpc-Metadata-<ключ>: %q; заголовки ответа: %v",
			ntf269eOtherKey, got, w.Header())
	}
}

// TestUserInvite_NTF269e_AdmittedInviteCarriesNoRetryAfter — второй близнец:
// приглашение в пределах потолков — `200` с телом `Operation`, срока повтора
// нет.
func TestUserInvite_NTF269e_AdmittedInviteCarriesNoRetryAfter(t *testing.T) {
	w := inviteThroughEdge(t, ntf269eEmailWithinCap)
	if w.Code != http.StatusOK {
		t.Fatalf("http %d, want 200; body %s", w.Code, w.Body.String())
	}
	var op struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &op); err != nil {
		t.Fatalf("тело не JSON: %v; body %s", err, w.Body.String())
	}
	if op.ID != ntf269eOperationID {
		t.Errorf("Operation.id %q, want %q", op.ID, ntf269eOperationID)
	}
	if got := w.Header().Values("Retry-After"); len(got) != 0 {
		t.Errorf("заголовок Retry-After %q на пропущенном приглашении", got)
	}
}
