// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package check

import (
	"errors"
	"log/slog"
	"time"

	"google.golang.org/grpc"

	"github.com/PRO-Robotech/kacho/pkg/authz"
)

// Options — параметры для NewInterceptor.
//
// IAMConn — gRPC client-conn к kacho-iam internal-port'у (обычно
// `kacho-iam.kacho.svc.cluster.local:9091`). Если nil — фабрика возвращает
// (nil, nil), и caller обязан НЕ ставить authz-interceptor в цепочку
// (graceful start без kacho-iam в dev).
//
// Аварийного пропуска у фабрики больше нет: ручка, включавшая проход всех RPC без
// проверки, снята вместе с переходом на общий носитель контура. Носитель ставит
// звено решения о доступе БЕЗУСЛОВНО, и поля, способного его отменить, в
// дескрипторе процесса не существует — значит и здесь такому полю нечего было бы
// выражать: оно принималось бы и игнорировалось.
type Options struct {
	ServiceName string
	IAMConn     grpc.ClientConnInterface
	Logger      *slog.Logger

	// CheckTimeout — таймаут на один Check-вызов (default 2s).
	CheckTimeout time.Duration

	// DenyRateLimitPerSec — token-bucket per-Principal на denied-storm
	// (0 → отключен, default рекомендуется 100/s).
	DenyRateLimitPerSec float64

	// CacheTTL — TTL кеша положительных результатов (default 5s).
	CacheTTL time.Duration

	// AllowSystemPrincipal — system-principal (bootstrap) пропускается без
	// Check (default false). Включать для миграций / фоновых job'ов.
	AllowSystemPrincipal bool

	// Probe — existence-probe для existence-hiding (Decision 1): object-scoped
	// deny на отсутствующий vpc-ресурс → ErrNoPath (passthrough → handler 404).
	// nil → прежнее поведение (только reason-substring "no path" → passthrough).
	Probe ResourceExistenceProbe
}

// ErrIAMConnNotConfigured — IAM-conn = nil И break-glass=false. Caller'у
// нужно либо подать IAMConn, либо включить break-glass (dev).
var ErrIAMConnNotConfigured = errors.New("check: IAM connection not configured")

// NewInterceptor собирает `*authz.Interceptor` из Options. Возвращает:
//
//   - (*authz.Interceptor, nil) — успех; caller должен подвесить
//     Unary()/Stream() в цепочку interceptor'ов своего grpc.Server.
//   - (nil, ErrIAMConnNotConfigured) — IAM не сконфигурирован И break-glass=false.
//     Caller сам решает, как реагировать: в production-mode — fatal error;
//     в dev — log+continue без authz-interceptor'а.
//
// Никаких panic'ов наружу не выпускается; все invalid-options оборачиваются
// в error.
func NewInterceptor(opts Options) (*authz.Interceptor, error) {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}

	if opts.IAMConn == nil {
		return nil, ErrIAMConnNotConfigured
	}

	client := NewIAMCheckClientWithProbe(opts.IAMConn, opts.Probe)
	return authz.NewInterceptor(authz.InterceptorOptions{
		ServiceName:          opts.ServiceName,
		Map:                  PermissionMap(),
		Client:               client,
		Cache:                authz.NewCache(opts.CacheTTL),
		Logger:               opts.Logger,
		Breakglass:           false,
		DenyRateLimitPerSec:  opts.DenyRateLimitPerSec,
		CheckTimeout:         opts.CheckTimeout,
		AllowSystemPrincipal: opts.AllowSystemPrincipal,
	}), nil
}
