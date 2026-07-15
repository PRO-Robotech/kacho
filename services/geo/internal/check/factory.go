// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package check

import (
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
}

// ErrIAMConnNotConfigured — IAM conn = nil И Breakglass=false.
var ErrIAMConnNotConfigured = errors.New("check: IAM connection not configured and Breakglass=false")

// NewInterceptor строит authz-интерсептор geo. Возвращает:
//   - (*authz.Interceptor, nil) — успех; вызывающий навешивает Unary()/Stream().
//   - (nil, ErrIAMConnNotConfigured) — IAM не сконфигурирован И Breakglass=false.
//     Решение за вызывающим: production → fatal; dev → пропустить интерсептор.
func NewInterceptor(opts Options) (*authz.Interceptor, error) {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	// Единственные различия между breakglass и обычным режимом — Client (nil vs
	// IAM-adapter) и флаг Breakglass. Остальные 7 полей InterceptorOptions
	// идентичны, поэтому держим ОДИН литерал: добавленное позже поле не сможет
	// разъехаться между режимами. Breakglass → Client остаётся nil-интерфейсом
	// (полный обход Check); без breakglass — fail-closed при IAMConn==nil.
	var client authz.CheckClient
	if !opts.Breakglass {
		if opts.IAMConn == nil {
			return nil, ErrIAMConnNotConfigured
		}
		client = NewIAMCheckClient(opts.IAMConn)
	}
	// CheckTimeout / DenyRateLimitPerSec / AllowSystemPrincipal не прокидываются из
	// конфига geo — corelib authz применяет свои дефолты (CheckTimeout→2s). Cache
	// передаём ЯВНО как authz.NewCache(0): corelib резолвит ttl≤0 в свой дефолтный
	// 5s positive-result-кеш (см. corelib authz/cache.go — кешируются только
	// allowed=true; miss всегда безопасен, fallback на авторитетный Check).
	//
	// ⚠ В geo кеш — TTL-ONLY (окно stale-allow ≤5s). Проактивная pg_notify-
	// инвалидация (corelib authz/listen_invalidate.go → Cache.InvalidateBySubject)
	// НЕ подключена и подключена быть не может: сигнал идёт по
	// pg_notify('kacho_iam_subjects', …) из БД kacho-iam, а при database-per-service
	// geo не имеет доступа к БД iam. Значит отозванный (revoked) subject остаётся
	// авторизованным в geo до истечения TTL (≤5s) — сознательно принятый риск ради
	// амортизации Check-нагрузки; окно узкое и ограничено positive-результатами.
	// Держим на дефолтах намеренно: отдельных operator-knob'ов для них geo не заводит.
	return authz.NewInterceptor(authz.InterceptorOptions{
		ServiceName: opts.ServiceName,
		Map:         PermissionMap(),
		Client:      client,
		Cache:       authz.NewCache(0),
		Logger:      opts.Logger,
		Breakglass:  opts.Breakglass,
	}), nil
}
