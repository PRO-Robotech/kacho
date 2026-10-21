// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// iam_authorize_checker.go — adapter making IAMAuthorizeClient satisfy
// middleware.AuthorizeChecker.
//
// The clients package already imports `middleware` (iam_subject_client.go
// uses middleware.Subject), so the dependency direction
// `clients → middleware` is established. The middleware package CANNOT
// import `clients` — that would close the cycle.
//
// To keep the wiring straightforward main.go calls
// `clients.NewAuthzChecker(client)` and passes the result into
// `middleware.AuthzMiddlewareConfig.Checker`, which is typed as
// `middleware.AuthorizeChecker` (interface). The adapter below satisfies
// that interface.
package clients

import (
	"context"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// AuthzChecker — middleware-side shape (`middleware.AuthorizeChecker`) over
// `*IAMAuthorizeClient`.
type AuthzChecker struct {
	inner AuthorizeClient
}

// NewAuthzChecker wraps the IAM AuthorizeService gRPC client into the
// middleware.AuthorizeChecker shape.
func NewAuthzChecker(inner AuthorizeClient) *AuthzChecker {
	return &AuthzChecker{inner: inner}
}

// Check forwards a middleware-shaped request through the gRPC client.
func (a *AuthzChecker) Check(ctx context.Context, in middleware.AuthzCheckInput) (middleware.AuthzCheckResult, error) {
	res, err := a.inner.Check(ctx, AuthorizeCheckInput{
		Subject: in.Subject,
		Action:  in.Action,
		// Явное отношение из каталога прав передаётся дальше. Без него служба прав
		// вывела бы отношение из глагола: RPC только для администратора
		// (required_relation=system_admin) с глаголом `list`/`get` получил бы
		// `viewer` и прошёл бы по системной выдаче
		// `cluster.viewer=system_class:authenticated` (повышение привилегий), а
		// глаголы вне CRUD отказывали бы закрыто.
		RequiredRelation: in.RequiredRelation,
		ResourceType:     in.ResourceType,
		ResourceID:       in.ResourceID,
		Context:          in.Context,
		TraceID:          in.TraceID,
	})
	if err != nil {
		return middleware.AuthzCheckResult{}, err
	}
	return middleware.AuthzCheckResult{
		Allowed:     res.Allowed,
		DenyReasons: res.DenyReasons,
		CheckedAt:   res.CheckedAt,
	}, nil
}

// Ensure AuthzChecker satisfies the middleware interface at compile time.
var _ middleware.AuthorizeChecker = (*AuthzChecker)(nil)
