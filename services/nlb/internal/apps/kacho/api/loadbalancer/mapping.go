// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package loadbalancer

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"
	"github.com/PRO-Robotech/kacho/pkg/operations"
	"github.com/PRO-Robotech/kacho/pkg/operations/operationspb"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/apps/kacho/api/shared"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/dto"
	kachorepo "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// mapDomainErr транслирует sentinel-ошибки `domain`/`kacho` (repo) и peer-client
// ошибки в gRPC-status. Делегирует единому мапперу `shared.MapDomainErr` (один
// источник истины для всех use-case пакетов).
func mapDomainErr(err error) error {
	return shared.MapDomainErr(err)
}

// operationToProto — прослойка к общему слою: перевод строки операции в контракт
// объявлен в дереве ОДИН раз (`pkg/operations/operationspb`).
//
// Здесь стояло «делегатор к единому `shared.OperationToProto`» — звено, снятое
// выпрямлением цепочки: комментарий пережил свой предмет ровно на одну правку.
func operationToProto(op *operations.Operation) *operationpb.Operation {
	return operationspb.ToProto(op)
}

// lbRecordToProto — repo-entity LoadBalancerRecord → proto NetworkLoadBalancer
// через зарегистрированный DTO transfer (`internal/dto/type2pb/loadbalancer.go`).
func lbRecordToProto(rec *kachorepo.LoadBalancerRecord) (*lbv1.NetworkLoadBalancer, error) {
	if rec == nil {
		return nil, status.Error(codes.Internal, "nil load_balancer record")
	}
	var dst *lbv1.NetworkLoadBalancer
	if err := dto.Transfer(dto.FromTo(*rec, &dst)); err != nil {
		return nil, mapDomainErr(err)
	}
	return dst, nil
}

// errInvalidArg — тонкий делегатор к единому `shared.ErrInvalidArg`.
func errInvalidArg(field, msg string) error {
	return shared.ErrInvalidArg(field, msg)
}
