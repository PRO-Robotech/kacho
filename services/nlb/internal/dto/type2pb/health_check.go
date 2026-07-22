// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package type2pb

import (
	"time"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// healthCheckToPb — конвертер domain.HealthCheck → *lbv1.HealthCheck (NLB-1c
// redesigned shape: без `name`; 4-way oneof tcp/http/https/grpc; http/https
// несут expected_codes/host/headers; effective_port — output-only derived).
//
// tgPort — backend-порт группы (`TargetGroup.port`); нужен для резолва
// effective_port (проба без port-override наследует его).
//
// Helper-функция (не Interface[F,T] в registry), потому что HealthCheck —
// embedded value в TargetGroup, а не самостоятельная сущность с CreatedAt.
// Вызывается inline из targetGroup{}.toPb.
func healthCheckToPb(hc domain.HealthCheck, tgPort domain.LbPort) *lbv1.HealthCheck {
	// Если HC пустой (нулевой) — возвращаем nil (proto-field optional).
	if isHealthCheckZero(hc) {
		return nil
	}
	out := &lbv1.HealthCheck{
		Interval:           durationpb.New(time.Duration(hc.Interval)),
		Timeout:            durationpb.New(time.Duration(hc.Timeout)),
		UnhealthyThreshold: int64(hc.UnhealthyThreshold),
		HealthyThreshold:   int64(hc.HealthyThreshold),
		// effective_port — output-only derived: probe-override || TG.port.
		EffectivePort: int64(hc.EffectivePort(tgPort)),
	}
	switch {
	case hc.TCP != nil:
		out.Options = &lbv1.HealthCheck_Tcp{
			Tcp: &lbv1.HealthCheck_TcpOptions{Port: int64(hc.TCP.Port)},
		}
	case hc.HTTP != nil:
		out.Options = &lbv1.HealthCheck_Http{
			Http: &lbv1.HealthCheck_HttpOptions{
				Port:          int64(hc.HTTP.Port),
				Path:          hc.HTTP.Path,
				ExpectedCodes: hc.HTTP.ExpectedCodes,
				Host:          hc.HTTP.Host,
				Headers:       hc.HTTP.Headers,
			},
		}
	case hc.HTTPS != nil:
		out.Options = &lbv1.HealthCheck_Https{
			Https: &lbv1.HealthCheck_HttpsOptions{
				Port:          int64(hc.HTTPS.Port),
				Path:          hc.HTTPS.Path,
				ExpectedCodes: hc.HTTPS.ExpectedCodes,
				Host:          hc.HTTPS.Host,
				Headers:       hc.HTTPS.Headers,
			},
		}
	case hc.GRPC != nil:
		out.Options = &lbv1.HealthCheck_Grpc{
			Grpc: &lbv1.HealthCheck_GrpcOptions{
				Port:        int64(hc.GRPC.Port),
				ServiceName: hc.GRPC.ServiceName,
			},
		}
	}
	return out
}

func isHealthCheckZero(hc domain.HealthCheck) bool {
	return hc.Interval == 0 &&
		hc.Timeout == 0 &&
		hc.UnhealthyThreshold == 0 &&
		hc.HealthyThreshold == 0 &&
		hc.TCP == nil && hc.HTTP == nil && hc.HTTPS == nil && hc.GRPC == nil
}
