// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package restmux

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	operationv1 "github.com/PRO-Robotech/corelib/api/corelib/operation"
	iampb "github.com/PRO-Robotech/kaname/pkg/api/kaname/cloud/iam/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// account_create_id_passes_test.go — край доносит поле `id` тела
// `POST /iam/v1/accounts` до службы доступа (kacho#2984; служба —
// PRO-Robotech/kaname#549, приёмка службы
// `account-id-may-be-supplied-at-create.md`, стадия S2, AID-K1).
//
// ПРЕДМЕТ. Край разбирает тело в message запроса стабами ПИНЕННОГО модуля
// службы и неизвестный ключ отбрасывает (`DiscardUnknown`, strict_enum.go).
// Пин без поля `id` в `CreateAccountRequest` — указанный идентификатор молча
// пропадает, служба получает пустое поле и чеканит свой: клиент видит `200`
// и чужой идентификатор. Отказа нет ни на одном шаге, поэтому увидеть это
// можно только здесь — на шве «тело → message».
//
// ПОЧЕМУ ПОЛЕ ЧИТАЕТСЯ ОТРАЖЕНИЕМ ПО ИМЕНИ, а не геттером: геттер на прежнем
// пине не компилируется, и проба не отличала бы «поля нет» от «пакет сломан».
// Отражение даёт честный красный — «в контракте пиненной службы поля нет».
//
// ЗАКОННЫЙ БЛИЗНЕЦ — то же тело без `id`: служба получает пустое поле, то есть
// ветку генератора. Меняется ровно один факт — есть ли ключ в теле.
//
// ГРАНИЦА. Проба судит край с боевым маршаллером (`NewMux`) против
// службы-дублёра. Что служба записывает присланный идентификатор, решает
// право и форму — её приёмка; сквозной исход через край на стенде держит
// коллекция `gateway/tests/newman/cases/iam-account-id-at-create.py`.

const suppliedAccountID = "acc0kach02984edge0k1"

// accountCreateRecorder — дублёр службы: запоминает присланный `id` и отвечает
// операцией. Ничего больше `Create` не делает — предмет только шов края.
type accountCreateRecorder struct {
	iampb.UnimplementedAccountServiceServer
	got chan protoreflect.Message
}

func (r *accountCreateRecorder) Create(_ context.Context, in *iampb.CreateAccountRequest) (*operationv1.Operation, error) {
	r.got <- in.ProtoReflect()
	return &operationv1.Operation{Id: "iop0kach02984edge0k1"}, nil
}

func startAccountRecorder(t *testing.T) (string, *accountCreateRecorder) {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	gs := grpc.NewServer()
	rec := &accountCreateRecorder{got: make(chan protoreflect.Message, 1)}
	iampb.RegisterAccountServiceServer(gs, rec)
	go func() { _ = gs.Serve(lis) }()
	t.Cleanup(gs.Stop)
	return lis.Addr().String(), rec
}

// TestEdgeCarriesTheSuppliedAccountIdToTheService — тело с `id` доходит до
// службы с тем же значением; близнец без `id` доходит с пустым полем.
func TestEdgeCarriesTheSuppliedAccountIdToTheService(t *testing.T) {
	idField := (&iampb.CreateAccountRequest{}).ProtoReflect().Descriptor().Fields().ByName("id")
	if idField == nil {
		t.Fatalf("CreateAccountRequest пиненной службы не несёт поля `id`: край отбросит " +
			"указанный идентификатор (DiscardUnknown), служба отчеканит свой — пин " +
			"github.com/PRO-Robotech/kaname в go.mod ниже ревизии с полем (kaname#549)")
	}

	addr, rec := startAccountRecorder(t)
	addrs := geoMuxAddrs()
	addrs["iam"] = addr
	h, err := NewMux(context.Background(), addrs, nil, nil)
	if err != nil {
		t.Fatalf("NewMux: %v", err)
	}

	cases := []struct {
		name, body, want string
	}{
		{"supplied", `{"id":"` + suppliedAccountID + `","name":"aidk1-edge"}`, suppliedAccountID},
		{"generator-twin", `{"name":"aidk1-edge-twin"}`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/iam/v1/accounts", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("POST /iam/v1/accounts %s: http %d, body %s", tc.body, w.Code, w.Body.String())
			}
			var got protoreflect.Message
			select {
			case got = <-rec.got:
			default:
				t.Fatalf("POST /iam/v1/accounts %s: AccountService.Create не вызван", tc.body)
			}
			if v := got.Get(idField).String(); v != tc.want {
				t.Errorf("POST /iam/v1/accounts %s: служба получила id %q, want %q", tc.body, v, tc.want)
			}
		})
	}
}
