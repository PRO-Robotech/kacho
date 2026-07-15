// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package check_test

import (
	stderrors "errors"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/PRO-Robotech/kacho/services/geo/internal/check"
)

// TestNewInterceptor_breakglass_noConn — breakglass=true строит интерсептор даже
// при IAMConn=nil (полный аварийный обход authz+mTLS). Security-critical ветка.
func TestNewInterceptor_breakglass_noConn(t *testing.T) {
	intr, err := check.NewInterceptor(check.Options{
		ServiceName: "kacho-geo",
		IAMConn:     nil,
		Breakglass:  true,
	})
	if err != nil {
		t.Fatalf("breakglass NewInterceptor err = %v, want nil", err)
	}
	if intr == nil {
		t.Fatal("breakglass NewInterceptor = nil, want non-nil interceptor")
	}
}

// TestNewInterceptor_noConn_noBreakglass_errConfigured — IAMConn=nil И
// breakglass=false → ErrIAMConnNotConfigured (fail-closed, вызывающий решает).
func TestNewInterceptor_noConn_noBreakglass_errConfigured(t *testing.T) {
	intr, err := check.NewInterceptor(check.Options{
		ServiceName: "kacho-geo",
		IAMConn:     nil,
		Breakglass:  false,
	})
	if !stderrors.Is(err, check.ErrIAMConnNotConfigured) {
		t.Fatalf("err = %v, want ErrIAMConnNotConfigured", err)
	}
	if intr != nil {
		t.Fatalf("interceptor = %v, want nil on error", intr)
	}
}

// TestNewInterceptor_conn_noBreakglass_ok — реальный conn (lazy, без дозвона) →
// non-nil интерсептор, nil err.
func TestNewInterceptor_conn_noBreakglass_ok(t *testing.T) {
	// grpc.NewClient ленивый: TCP-дозвона нет, conn годится как ClientConnInterface.
	conn, err := grpc.NewClient("passthrough:///iam-bufnet",
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient err = %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	intr, err := check.NewInterceptor(check.Options{
		ServiceName: "kacho-geo",
		IAMConn:     conn,
		Breakglass:  false,
	})
	if err != nil {
		t.Fatalf("NewInterceptor err = %v, want nil", err)
	}
	if intr == nil {
		t.Fatal("NewInterceptor = nil, want non-nil interceptor")
	}
}
