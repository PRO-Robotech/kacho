// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"errors"
	"testing"

	"google.golang.org/grpc"

	"github.com/PRO-Robotech/kacho/services/geo/internal/check"
)

// TestAuthzIAMConn_nilYieldsTrueNil — при nil *grpc.ClientConn helper обязан
// вернуть ИСТИННЫЙ nil интерфейса, а не typed-nil (interface wrapping (*T)(nil)),
// иначе guard `if opts.IAMConn == nil` в check.NewInterceptor никогда не сработает
// и ErrIAMConnNotConfigured-ветка станет мёртвой (CWE-476 latent nil-conn).
func TestAuthzIAMConn_nilYieldsTrueNil(t *testing.T) {
	if got := authzIAMConn(nil); got != nil {
		t.Fatalf("authzIAMConn(nil) must be a true-nil interface, got non-nil %#v", got)
	}
}

// TestAuthzIAMConn_nonNilPassthrough — реальный conn проходит как есть.
func TestAuthzIAMConn_nonNilPassthrough(t *testing.T) {
	conn := &grpc.ClientConn{}
	if got := authzIAMConn(conn); got == nil {
		t.Fatal("authzIAMConn(non-nil) must pass the conn through")
	}
}

// TestNewInterceptor_trueNilConn_ErrIAMConnNotConfigured — с true-nil conn и
// Breakglass=false фабрика возвращает ErrIAMConnNotConfigured (чистый
// startup-fail), а не строит клиент поверх nil-conn (который паникнул бы на
// первом Check). Это и есть ветка, которую оживляет фикс footgun'а.
func TestNewInterceptor_trueNilConn_ErrIAMConnNotConfigured(t *testing.T) {
	_, err := check.NewInterceptor(check.Options{
		ServiceName: "kacho-geo",
		IAMConn:     authzIAMConn(nil),
		Breakglass:  false,
		Logger:      quietLogger(),
	})
	if !errors.Is(err, check.ErrIAMConnNotConfigured) {
		t.Fatalf("want ErrIAMConnNotConfigured for true-nil conn, got %v", err)
	}
}
