// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package nodeownership

import (
	"context"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/services/compute/internal/repo"
)

const someInstance = "ins-abcdefghjkmnpqrst"

type fakeOwnership struct {
	got      repo.NodeBinding
	grace    time.Duration
	called   bool
	claimErr error
	relErr   error
	released [2]string
}

func (f *fakeOwnership) ClaimInstance(_ context.Context, b repo.NodeBinding, grace time.Duration) (*repo.NodeBinding, error) {
	f.got, f.grace, f.called = b, grace, true
	if f.claimErr != nil {
		return nil, f.claimErr
	}
	return &b, nil
}

func (f *fakeOwnership) ReleaseInstance(_ context.Context, instanceID, nodeID string) error {
	f.released = [2]string{instanceID, nodeID}
	return f.relErr
}

func claimReq() ClaimReq {
	return ClaimReq{
		InstanceID: someInstance,
		NodeID:     "узел-1",
		SequenceNo: 5,
		LeaseUntil: time.Now().Add(30 * time.Second),
	}
}

// TestClaim_RejectsWhatWouldMakeAnInstanceUnmovable — вход, делающий машину
// невытесняемой либо невыразимой, отвергается ЗДЕСЬ и по имени поля.
func TestClaim_RejectsWhatWouldMakeAnInstanceUnmovable(t *testing.T) {
	cases := []struct {
		name  string
		mutFn func(*ClaimReq)
		field string
	}{
		{"машина не названа как машина", func(r *ClaimReq) { r.InstanceID = "не идентификатор" }, ""},
		{"узел не назван", func(r *ClaimReq) { r.NodeID = "" }, "nodeId"},
		{"узел сверх предела длины", func(r *ClaimReq) { r.NodeID = strings.Repeat("у", maxNodeIDLen+1) }, "nodeId"},
		{"номер события нулевой", func(r *ClaimReq) { r.SequenceNo = 0 }, "deliverySequenceNo"},
		{"аренда не названа", func(r *ClaimReq) { r.LeaseUntil = time.Time{} }, "leaseUntil"},
		{"аренда уже истекла", func(r *ClaimReq) { r.LeaseUntil = time.Now().Add(-time.Second) }, "leaseUntil"},
		{"аренда на годы вперёд", func(r *ClaimReq) { r.LeaseUntil = time.Now().Add(24 * time.Hour) }, "leaseUntil"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := claimReq()
			tc.mutFn(&req)
			fake := &fakeOwnership{}
			_, err := NewService(fake).Claim(context.Background(), req)
			if status.Code(err) != codes.InvalidArgument {
				t.Fatalf("код = %v, ожидался InvalidArgument", status.Code(err))
			}
			if tc.field != "" && !strings.Contains(status.Convert(err).Message(), tc.field) {
				t.Errorf("сообщение %q не называет поле %q", status.Convert(err).Message(), tc.field)
			}
			if fake.called {
				t.Error("отвергнутый обмен не должен доезжать до хранилища")
			}
		})
	}

	// Положительный контроль: законный обмен доезжает целиком и несёт запас.
	t.Run("законный обмен доезжает и несёт запас сверх истечения", func(t *testing.T) {
		fake := &fakeOwnership{}
		req := claimReq()
		got, err := NewService(fake).Claim(context.Background(), req)
		if err != nil {
			t.Fatalf("законный обмен обязан пройти, получено %v", err)
		}
		if !fake.called {
			t.Fatal("законный обмен обязан доехать до хранилища")
		}
		if fake.grace != GraceAfterExpiry {
			t.Errorf("запас = %v, ожидался %v — перехват без запаса даёт двух писателей",
				fake.grace, GraceAfterExpiry)
		}
		if fake.got.NodeID != req.NodeID || fake.got.ClaimedSeqNo != req.SequenceNo {
			t.Errorf("вход искажён по пути: %+v", fake.got)
		}
		if got.NodeID != req.NodeID {
			t.Errorf("ответ не описывает действующую привязку: %+v", got)
		}
	})
}

// TestClaim_HeldByAnotherNodeIsStateNotInput — проигранная гонка отвечает
// состоянием и НЕ называет держателя.
//
// Это два разных утверждения, и оба нужны. Отказ по вводу заставил бы агента
// чинить запрос, который верен; названный держатель раскрыл бы, какие машины
// стоят на одном железе, — а спрашивающий узел уже знает, что проиграл.
func TestClaim_HeldByAnotherNodeIsStateNotInput(t *testing.T) {
	fake := &fakeOwnership{claimErr: repo.ErrHeldByAnotherNode}
	_, err := NewService(fake).Claim(context.Background(), claimReq())
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("код = %v, ожидался FailedPrecondition", status.Code(err))
	}
	msg := status.Convert(err).Message()
	if strings.Contains(msg, "узел") || strings.Contains(msg, "node-") {
		t.Errorf("сообщение %q называет держателя — идентификатор узла инфра-чувствителен", msg)
	}

	// Положительный контроль: без проигранной гонки тот же запрос проходит.
	if _, err := NewService(&fakeOwnership{}).Claim(context.Background(), claimReq()); err != nil {
		t.Fatalf("тот же обмен без чужой аренды обязан пройти, получено %v", err)
	}
}

// TestRelease_IdempotentAndScopedToOwnBinding — отпускание идемпотентно и
// касается ТОЛЬКО своей привязки.
func TestRelease_IdempotentAndScopedToOwnBinding(t *testing.T) {
	fake := &fakeOwnership{}
	svc := NewService(fake)

	if err := svc.Release(context.Background(), someInstance, "узел-1"); err != nil {
		t.Fatalf("отпускание обязано пройти, получено %v", err)
	}
	if fake.released != [2]string{someInstance, "узел-1"} {
		t.Errorf("отпускание ушло не по своей привязке: %v", fake.released)
	}

	// Повтор — успех, а не отказ: агент, повторяющий после обрыва ответа, не
	// должен получать ошибку на исполненное действие.
	if err := svc.Release(context.Background(), someInstance, "узел-1"); err != nil {
		t.Fatalf("повторное отпускание обязано быть успехом, получено %v", err)
	}

	t.Run("непригодный идентификатор машины отвергается разбором", func(t *testing.T) {
		if err := svc.Release(context.Background(), "не идентификатор", "узел-1"); status.Code(err) != codes.InvalidArgument {
			t.Errorf("код = %v, ожидался InvalidArgument", status.Code(err))
		}
	})

	t.Run("узел без имени отвергается по имени поля", func(t *testing.T) {
		err := svc.Release(context.Background(), someInstance, "")
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("код = %v, ожидался InvalidArgument", status.Code(err))
		}
		if !strings.Contains(status.Convert(err).Message(), "nodeId") {
			t.Errorf("сообщение %q не называет поле", status.Convert(err).Message())
		}
	})
}
