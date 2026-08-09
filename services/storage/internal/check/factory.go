// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package check

import (
	"time"

	"errors"
	"log/slog"

	"google.golang.org/grpc"

	"github.com/PRO-Robotech/kacho/pkg/authz"
)

// Options — параметры для NewInterceptor.
type Options struct {
	ServiceName string
	IAMConn     grpc.ClientConnInterface
	Breakglass  bool
	Logger      *slog.Logger

	// CacheTTL — окно кеша положительных вердиктов, оно же ОКНО ОТЗЫВА: столько
	// субъект, у которого право уже отобрали, продолжает проходить. Отрицательные
	// вердикты не кешируются никогда, поэтому свежая выдача видна сразу.
	//
	// Величина приходит из конфигурации, а не берётся умолчанием библиотеки:
	// параметр безопасности, которого никто не выбирал, нельзя ни обсудить, ни
	// сузить на конкретной посадке. Значение не изменилось — изменилось то, что
	// оно теперь выбрано. Ноль означает «беру объявленную политику»
	// (pkg/authz.RevocationPolicy.Default).
	CacheTTL time.Duration
}

// ErrIAMConnNotConfigured — IAM conn = nil И Breakglass=false.
var ErrIAMConnNotConfigured = errors.New("check: IAM connection not configured and Breakglass=false")

// NewInterceptor строит authz-интерсептор storage. Возвращает:
//   - (*authz.Interceptor, nil) — успех; вызывающий навешивает Unary()/Stream().
//   - (nil, ErrIAMConnNotConfigured) — IAM не сконфигурирован И Breakglass=false.
//     Решение за вызывающим: production → fatal; dev → пропустить интерсептор.
func NewInterceptor(opts Options) (*authz.Interceptor, error) {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	var client authz.CheckClient
	if !opts.Breakglass {
		if opts.IAMConn == nil {
			return nil, ErrIAMConnNotConfigured
		}
		client = NewIAMCheckClient(opts.IAMConn)
	}
	// Cache передаём NewCache(0): corelib резолвит ttl≤0 в дефолтный 5s
	// positive-result-кеш (кешируется только allowed=true; miss всегда безопасен).
	return authz.NewInterceptor(authz.InterceptorOptions{
		ServiceName: opts.ServiceName,
		Map:         PermissionMap(),
		Client:      client,
		Cache:       authz.NewCache(opts.CacheTTL),
		Logger:      opts.Logger,
		Breakglass:  opts.Breakglass,
	}), nil
}
