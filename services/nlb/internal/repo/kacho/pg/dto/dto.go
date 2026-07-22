// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package dto — pgmodel ↔ domain.X конвертация для kacho-nlb.
//
// Расположение: `internal/repo/kacho/pg/dto/`. Domain-пакет ничего не знает про
// JSONB-сериализацию (workspace CLAUDE.md «Чистая архитектура»); этот пакет
// единственное место, где доменные типы (HealthCheck, LbLabels) превращаются
// в JSONB-tape и обратно.
//
// Используется repo-impl'ом в `internal/repo/kacho/pg/`.
package dto

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/H-BF/corlib/pkg/option"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// DurationToSeconds — domain.LbDuration → integer seconds for the
// `deregistration_delay_seconds` / `slow_start_seconds` int columns (NLB-1c B8:
// domain type is Duration; DB storage stays integer seconds, keeping the drain
// runner's `make_interval(secs => …)` SQL intact). Bounds are validated in the
// domain [0s..3600s]/[0s..900s], so the truncation to whole seconds is exact.
func DurationToSeconds(d domain.LbDuration) int32 {
	return int32(time.Duration(d) / time.Second)
}

// SecondsToDuration — inverse of DurationToSeconds (int seconds column →
// domain.LbDuration).
func SecondsToDuration(sec int32) domain.LbDuration {
	return domain.LbDuration(time.Duration(sec) * time.Second)
}

// LabelsToJSONB — domain.LbLabels → JSONB bytes. nil-map → `{}`.
func LabelsToJSONB(labels domain.LbLabels) ([]byte, error) {
	m := domain.LabelsToMap(labels)
	if m == nil {
		return []byte(`{}`), nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("marshal labels: %w", err)
	}
	return b, nil
}

// LabelsFromJSONB — JSONB bytes → domain.LbLabels.
//
// nil/empty → пустой LbLabels (т.к. domain считает empty == nil-map).
// jsonb-`null` → пустой LbLabels (паритет с psql NULL handling).
func LabelsFromJSONB(b []byte) (domain.LbLabels, error) {
	var labels domain.LbLabels
	if len(b) == 0 {
		return labels, nil
	}
	// jsonb может прийти как 'null' (4 байта) — это валидный JSON; интерпретируем
	// как пустой.
	if string(b) == "null" {
		return labels, nil
	}
	m := map[string]string{}
	if err := json.Unmarshal(b, &m); err != nil {
		return labels, fmt.Errorf("unmarshal labels: %w", err)
	}
	return domain.LabelsFromMap(m), nil
}

// HealthCheckJSONB — wire-форма HealthCheck (JSONB-tape в
// `target_groups.health_check`). Зеркалит redesigned proto HealthCheck (NLB-1c:
// снят `name`; 4-way oneof tcp/http/https/grpc; http/https несут
// expected_codes/host/headers). Durations — как ns-int (interval_ns/timeout_ns).
type HealthCheckJSONB struct {
	IntervalNs         int64            `json:"interval_ns,omitempty"`
	TimeoutNs          int64            `json:"timeout_ns,omitempty"`
	UnhealthyThreshold int32            `json:"unhealthy_threshold,omitempty"`
	HealthyThreshold   int32            `json:"healthy_threshold,omitempty"`
	TCP                *HCPortOnly      `json:"tcp,omitempty"`
	HTTP               *HCHTTP          `json:"http,omitempty"`
	HTTPS              *HCHTTP          `json:"https,omitempty"`
	GRPC               *HCPortServiceNm `json:"grpc,omitempty"`
}

// HCPortOnly — TCP-probe (опциональный port-override).
type HCPortOnly struct {
	Port int32 `json:"port,omitempty"`
}

// HCHTTP — HTTP/HTTPS-probe. NLB-1c: expected_codes (строка) + host + headers.
type HCHTTP struct {
	Port          int32             `json:"port,omitempty"`
	Path          string            `json:"path,omitempty"`
	ExpectedCodes string            `json:"expected_codes,omitempty"`
	Host          string            `json:"host,omitempty"`
	Headers       map[string]string `json:"headers,omitempty"`
}

// HCPortServiceNm — gRPC-probe.
type HCPortServiceNm struct {
	Port        int32  `json:"port,omitempty"`
	ServiceName string `json:"service_name,omitempty"`
}

// HealthCheckToJSONB — domain.HealthCheck → JSONB bytes. zero-value HC → `{}`.
func HealthCheckToJSONB(hc domain.HealthCheck) ([]byte, error) {
	if isHealthCheckZero(hc) {
		return []byte(`{}`), nil
	}
	wire := HealthCheckJSONB{
		IntervalNs:         int64(time.Duration(hc.Interval)),
		TimeoutNs:          int64(time.Duration(hc.Timeout)),
		UnhealthyThreshold: hc.UnhealthyThreshold,
		HealthyThreshold:   hc.HealthyThreshold,
	}
	switch {
	case hc.TCP != nil:
		wire.TCP = &HCPortOnly{Port: int32(hc.TCP.Port)}
	case hc.HTTP != nil:
		wire.HTTP = &HCHTTP{
			Port: int32(hc.HTTP.Port), Path: hc.HTTP.Path,
			ExpectedCodes: hc.HTTP.ExpectedCodes, Host: hc.HTTP.Host, Headers: hc.HTTP.Headers,
		}
	case hc.HTTPS != nil:
		wire.HTTPS = &HCHTTP{
			Port: int32(hc.HTTPS.Port), Path: hc.HTTPS.Path,
			ExpectedCodes: hc.HTTPS.ExpectedCodes, Host: hc.HTTPS.Host, Headers: hc.HTTPS.Headers,
		}
	case hc.GRPC != nil:
		wire.GRPC = &HCPortServiceNm{Port: int32(hc.GRPC.Port), ServiceName: hc.GRPC.ServiceName}
	}
	b, err := json.Marshal(wire)
	if err != nil {
		return nil, fmt.Errorf("marshal health_check: %w", err)
	}
	return b, nil
}

// HealthCheckFromJSONB — JSONB bytes → domain.HealthCheck. Empty/null → zero HC.
func HealthCheckFromJSONB(b []byte) (domain.HealthCheck, error) {
	var hc domain.HealthCheck
	if len(b) == 0 || string(b) == "null" || string(b) == "{}" {
		return hc, nil
	}
	var wire HealthCheckJSONB
	if err := json.Unmarshal(b, &wire); err != nil {
		return hc, fmt.Errorf("unmarshal health_check: %w", err)
	}
	hc.Interval = domain.LbDuration(time.Duration(wire.IntervalNs))
	hc.Timeout = domain.LbDuration(time.Duration(wire.TimeoutNs))
	hc.UnhealthyThreshold = wire.UnhealthyThreshold
	hc.HealthyThreshold = wire.HealthyThreshold
	switch {
	case wire.TCP != nil:
		hc.TCP = &domain.HealthCheckTCP{Port: domain.LbPort(wire.TCP.Port)}
	case wire.HTTP != nil:
		hc.HTTP = &domain.HealthCheckHTTP{
			Port: domain.LbPort(wire.HTTP.Port), Path: wire.HTTP.Path,
			ExpectedCodes: wire.HTTP.ExpectedCodes, Host: wire.HTTP.Host, Headers: wire.HTTP.Headers,
		}
	case wire.HTTPS != nil:
		hc.HTTPS = &domain.HealthCheckHTTPS{
			Port: domain.LbPort(wire.HTTPS.Port), Path: wire.HTTPS.Path,
			ExpectedCodes: wire.HTTPS.ExpectedCodes, Host: wire.HTTPS.Host, Headers: wire.HTTPS.Headers,
		}
	case wire.GRPC != nil:
		hc.GRPC = &domain.HealthCheckGRPC{Port: domain.LbPort(wire.GRPC.Port), ServiceName: wire.GRPC.ServiceName}
	}
	return hc, nil
}

// isHealthCheckZero — true если HC не имеет ни одного set-поля (используется
// при сохранении пустого HC: пишем `{}` вместо полного wire-объекта с
// нулями).
func isHealthCheckZero(hc domain.HealthCheck) bool {
	return hc.Interval == 0 &&
		hc.Timeout == 0 &&
		hc.UnhealthyThreshold == 0 &&
		hc.HealthyThreshold == 0 &&
		hc.TCP == nil && hc.HTTP == nil && hc.HTTPS == nil && hc.GRPC == nil
}

// OptString — option.ValueOf[T] → *string для DB. Some("") и None
// различаются: Some("") пишет пустую строку, None пишет NULL (для nullable
// колонок). В schemas с DEFAULT пустой строки и NOT NULL — caller использует empty-string
// для None (см. ListenerOptToStr / OptToStr ниже).
func OptString[T ~string](v option.ValueOf[T]) string {
	if s, ok := v.Maybe(); ok {
		return string(s)
	}
	return ""
}

// OptFromStr — обратное: пустая строка → None; иначе Some(T(s)).
func OptFromStr[T ~string](s string) option.ValueOf[T] {
	if s == "" {
		return option.ValueOf[T]{}
	}
	return option.MustNewOption(T(s))
}
