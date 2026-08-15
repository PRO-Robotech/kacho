// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package serviceerr

import (
	"errors"
	"fmt"
	"testing"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// errorInfoOf достаёт признак полосы из деталей статуса.
//
// Читается именно ДЕТАЛЬ, а не текст: клиент различает полосы машинно, и проба
// обязана утверждать то, на что он ключуется. Утверждение по прозе зеленело бы
// на статусе вовсе без признака.
func errorInfoOf(t *testing.T, err error) *errdetails.ErrorInfo {
	t.Helper()
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("не gRPC-статус: %v", err)
	}
	for _, d := range st.Details() {
		if info, ok := d.(*errdetails.ErrorInfo); ok {
			return info
		}
	}
	return nil
}

// TestMapRepoErr_QuotaBandsAreMachineDistinguishable — исчерпание и
// неназначенный потолок различимы КОДОМ и ПРИЗНАКОМ, а не только текстом.
//
// Оба исхода — законные ответы арендатору, и путать их нельзя: читающий «место
// кончилось» пойдёт искать, что понизить, там, где ничего не назначено.
func TestMapRepoErr_QuotaBandsAreMachineDistinguishable(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantCode   codes.Code
		wantReason string
		wantMsg    string
	}{
		{
			name:       "исчерпание: потолок назван и выбран",
			err:        fmt.Errorf("%w: project prj-1 has reached its limit of 5 compute.instance", ErrQuotaExceeded),
			wantCode:   codes.ResourceExhausted, // край даёт 429
			wantReason: "QUOTA_EXCEEDED",
			wantMsg:    "project prj-1 has reached its limit of 5 compute.instance",
		},
		{
			name:       "потолок не назван ни на одной области",
			err:        fmt.Errorf("%w: project prj-1 has no ceiling stated for compute.instance", ErrQuotaNotProvisioned),
			wantCode:   codes.FailedPrecondition, // край даёт 400, НЕ 412 — его край не производит вовсе
			wantReason: "QUOTA_NOT_PROVISIONED",
			wantMsg:    "project prj-1 has no ceiling stated for compute.instance",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mapped := MapRepoErr(tt.err)
			st, _ := status.FromError(mapped)

			if st.Code() != tt.wantCode {
				t.Errorf("код: получили %v, ожидали %v", st.Code(), tt.wantCode)
			}
			// Текст производителя выносится ДОСЛОВНО: он называет носителя,
			// предел и вид, и он же есть контракт обеих полос отказа.
			if st.Message() != tt.wantMsg {
				t.Errorf("текст: получили %q, ожидали %q", st.Message(), tt.wantMsg)
			}
			info := errorInfoOf(t, mapped)
			if info == nil {
				t.Fatal("признак полосы не прикреплён: клиенту нечем различать причины машинно")
			}
			if info.GetReason() != tt.wantReason {
				t.Errorf("признак: получили %q, ожидали %q", info.GetReason(), tt.wantReason)
			}
			if info.GetDomain() != "compute.kacho.cloud" {
				t.Errorf("домен признака: получили %q", info.GetDomain())
			}
		})
	}

	// Два исхода обязаны РАЗЛИЧАТЬСЯ. Без этого утверждения обе строки таблицы
	// зеленели бы на реализации, отвечающей одним и тем же на оба.
	a := MapRepoErr(fmt.Errorf("%w: x", ErrQuotaExceeded))
	b := MapRepoErr(fmt.Errorf("%w: x", ErrQuotaNotProvisioned))
	if status.Code(a) == status.Code(b) {
		t.Error("исчерпание и неназначенный потолок получили один код — причины неразличимы")
	}
	if errorInfoOf(t, a).GetReason() == errorInfoOf(t, b).GetReason() {
		t.Error("исчерпание и неназначенный потолок получили один признак")
	}
}

// TestMapRepoErr_NonQuotaErrorsAreUntouched — положительный контроль к
// предыдущей пробе.
//
// Без него ветка отказа учёта могла бы перехватывать ВСЁ, и проба выше осталась
// бы зелёной: она спрашивает только про свои два входа.
func TestMapRepoErr_NonQuotaErrorsAreUntouched(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantCode codes.Code
	}{
		{"не найдено", fmt.Errorf("%w: Instance ins-1 not found", ErrNotFound), codes.NotFound},
		{"уже существует", ErrAlreadyExists, codes.AlreadyExists},
		{"состояние не позволяет", ErrFailedPrecondition, codes.FailedPrecondition},
		{"неклассифицированное", errors.New("boom"), codes.Internal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mapped := MapRepoErr(tt.err)
			if got := status.Code(mapped); got != tt.wantCode {
				t.Errorf("код: получили %v, ожидали %v", got, tt.wantCode)
			}
			if info := errorInfoOf(t, mapped); info != nil {
				t.Errorf("не-квотному отказу приклеен признак полосы %q", info.GetReason())
			}
		})
	}

	// И отдельно: неклассифицированное НЕ выносит наружу свой текст.
	if msg := status.Convert(MapRepoErr(errors.New("pgx: host=db user=kacho"))).Message(); msg != "internal database error" {
		t.Errorf("текст неклассифицированного отказа утёк наружу: %q", msg)
	}
}
