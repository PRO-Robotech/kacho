// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package domain

import (
	"time"

	coreerrors "github.com/PRO-Robotech/kacho/pkg/errors"
	"go.uber.org/multierr"
)

// HealthCheck — desired-конфигурация health-check. Embedded value-object
// TargetGroup (NLB-1c: снят `name`/id — HC не самостоятельный ресурс). В
// control-plane-only фазе не исполняется; реальные пробы — NLB-2.
//
// Probe-тип — 4-way oneof: exactly one of TCP/HTTP/HTTPS/GRPC. Каждый probe
// несёт ОПЦИОНАЛЬНЫЙ `Port`-override; при 0 проба наследует backend-порт группы
// (`TargetGroup.Port`) — резолв через EffectivePort.
type HealthCheck struct {
	Interval           LbDuration
	Timeout            LbDuration
	UnhealthyThreshold int32
	HealthyThreshold   int32
	TCP                *HealthCheckTCP
	HTTP               *HealthCheckHTTP
	HTTPS              *HealthCheckHTTPS
	GRPC               *HealthCheckGRPC
}

// HealthCheckTCP — TCP-probe; полезной нагрузки нет, только опциональный
// port-override.
type HealthCheckTCP struct {
	Port LbPort
}

// HealthCheckHTTP — HTTP-probe.
type HealthCheckHTTP struct {
	Port LbPort
	Path string
	// ExpectedCodes — healthy HTTP-коды: список и/или inclusive-диапазоны через
	// запятую, напр. "200-299" или "200,204". Пусто → «любой 2xx».
	ExpectedCodes string
	// Host — опциональный override заголовка Host в probe-запросе.
	Host string
	// Headers — опциональные доп. заголовки probe-запроса.
	Headers map[string]string
}

// HealthCheckHTTPS — HTTPS-probe (как HTTP, но поверх TLS).
type HealthCheckHTTPS struct {
	Port          LbPort
	Path          string
	ExpectedCodes string
	Host          string
	Headers       map[string]string
}

// HealthCheckGRPC — gRPC health-probe.
type HealthCheckGRPC struct {
	Port        LbPort
	ServiceName string
}

// EffectivePort — резолв probe-порта: override пробы если задан (>0), иначе
// backend-порт группы `tgPort` (наследование отсутствием). Output-only derived,
// проецируется в `HealthCheck.effective_port`.
func (h HealthCheck) EffectivePort(tgPort LbPort) LbPort {
	if p := h.probePort(); p > 0 {
		return p
	}
	return tgPort
}

// probePort — port-override выбранной пробы (0 если проба не задана или без
// override).
func (h HealthCheck) probePort() LbPort {
	switch {
	case h.TCP != nil:
		return h.TCP.Port
	case h.HTTP != nil:
		return h.HTTP.Port
	case h.HTTPS != nil:
		return h.HTTPS.Port
	case h.GRPC != nil:
		return h.GRPC.Port
	}
	return 0
}

// Validate — exactly-one-of TCP/HTTP/HTTPS/GRPC + bound checks (interval,
// timeout, thresholds). `name` снят в NLB-1c.
func (h HealthCheck) Validate() error {
	probeErr := h.validateProbeOneOf()

	intervalErr := error(nil)
	if h.Interval < HealthIntervalMin || h.Interval > HealthIntervalMax {
		intervalErr = coreerrors.InvalidArgument().
			AddFieldViolation("health_check.interval",
				"health_check.interval must be in range [1s, 600s]").
			Err()
	}

	timeoutErr := error(nil)
	switch {
	case h.Timeout < HealthTimeoutMin:
		timeoutErr = coreerrors.InvalidArgument().
			AddFieldViolation("health_check.timeout",
				"health_check.timeout must be positive (>= 1ms)").
			Err()
	case h.Interval > 0 && time.Duration(h.Timeout) > time.Duration(h.Interval):
		// timeout не может превышать interval — иначе probe overlap'ит сам себя.
		timeoutErr = coreerrors.InvalidArgument().
			AddFieldViolation("health_check.timeout",
				"health_check.timeout must be <= health_check.interval").
			Err()
	}

	unhealthyErr := error(nil)
	if h.UnhealthyThreshold < HealthThresholdMin || h.UnhealthyThreshold > HealthThresholdMax {
		unhealthyErr = coreerrors.InvalidArgument().
			AddFieldViolation("health_check.unhealthy_threshold",
				"unhealthy_threshold must be in range [2, 10]").
			Err()
	}

	healthyErr := error(nil)
	if h.HealthyThreshold < HealthThresholdMin || h.HealthyThreshold > HealthThresholdMax {
		healthyErr = coreerrors.InvalidArgument().
			AddFieldViolation("health_check.healthy_threshold",
				"healthy_threshold must be in range [2, 10]").
			Err()
	}

	return multierr.Combine(
		probeErr,
		intervalErr,
		timeoutErr,
		unhealthyErr,
		healthyErr,
	)
}

// validateProbeOneOf — exactly-one-of TCP/HTTP/HTTPS/GRPC + port-range на
// probe-override (0 = inherit, валиден; non-zero → [1,65535]). Фиксированный
// текст: `"health_check must specify exactly one of: tcp, http, https, grpc"`.
func (h HealthCheck) validateProbeOneOf() error {
	count := 0
	if h.TCP != nil {
		count++
	}
	if h.HTTP != nil {
		count++
	}
	if h.HTTPS != nil {
		count++
	}
	if h.GRPC != nil {
		count++
	}
	if count != 1 {
		return coreerrors.InvalidArgument().
			AddFieldViolation("health_check",
				"health_check must specify exactly one of: tcp, http, https, grpc").
			Err()
	}
	// probe-port override опционален: 0 → inherit TG.port (валиден); non-zero
	// валидируется как обычный порт.
	return validateProbePort(h.probePort())
}

// validateProbePort — 0 (inherit) валиден; иначе [PortMin, PortMax].
func validateProbePort(p LbPort) error {
	if p == 0 {
		return nil
	}
	return p.Validate()
}
