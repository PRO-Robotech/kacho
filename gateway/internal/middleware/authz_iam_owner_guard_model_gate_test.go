// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware_test

// authz_iam_owner_guard_model_gate_test.go — the model-side guard-rail for the
// twelve iam RPCs that used to carry the hand-rolled
// `authzguard.RequireOwnerMatchesPrincipal` owner-equality check next to the
// model (security.md — "Авторизация живёт в МОДЕЛИ, а не в самодельных
// проверках").
//
// The in-service check was removed because the model already decides these
// twelve. This file is the proof that removing it did NOT open access: for
// every one of the twelve FQNs it asserts, against the REAL embedded permission
// catalog (not a hand-written fixture),
//
//  1. the catalog carries an entry — a miss is fail-closed AUTHZ_DENIED, but a
//     silently-dropped entry would turn the RPC into "denied for everyone",
//     which is a different failure than "gated";
//  2. the entry pins the expected per-object `required_relation`
//     (v_delete / v_update) and a `scope_extractor` that resolves the target
//     object from the request (anti-BOLA — the Check is against the object the
//     caller named, not against the method);
//  3. a subject the model denies gets PermissionDenied — the handler is never
//     reached. This is the assertion that matters: "принципал без гранта
//     по-прежнему получает отказ";
//  4. a subject the model allows reaches the handler, and the relation+object
//     actually sent to the PDP are the ones the catalog pinned.
//
// (3) and (4) are driven through the real AuthzMiddleware with a programmable
// checker, so this locks OBSERVABLE behaviour (the RPC's outcome), not the
// shape of a JSON file — see testing.md "Regression-lock … на уровне
// ОБСЕРВАБЛА".

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kaname/cloud/iam/v1"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// ownerGuardRPC — one of the twelve RPCs the removed guard used to double-gate.
type ownerGuardRPC struct {
	fullMethod string
	// relation / objectType / requestField — what the catalog MUST require.
	relation    string
	objectType  string
	scopeField  string
	req         any
	targetObjID string
}

// ownerGuardModelGatedRPCs — the exact twelve call sites of the removed
// authzguard.RequireOwnerMatchesPrincipal, with the model gate each one relies
// on now that the hand-rolled check is gone.
func ownerGuardModelGatedRPCs() []ownerGuardRPC {
	const (
		accID  = "acc0000000000000aaaa"
		projID = "prj0000000000000pppp"
		usrID  = "usr0000000000000uuuu"
		grpID  = "grp0000000000000gggg"
		roleID = "rol0000000000000rrrr"
		saID   = "sva0000000000000ssss"
	)
	return []ownerGuardRPC{
		{
			fullMethod: "/kacho.cloud.iam.v1.AccountService/Delete",
			relation:   "v_delete", objectType: "account", scopeField: "account_id",
			req: &iamv1.DeleteAccountRequest{AccountId: accID}, targetObjID: accID,
		},
		{
			fullMethod: "/kacho.cloud.iam.v1.ProjectService/Delete",
			relation:   "v_delete", objectType: "project", scopeField: "project_id",
			req: &iamv1.DeleteProjectRequest{ProjectId: projID}, targetObjID: projID,
		},
		{
			// #1102: правка записи человека ушла с `v_update` на `record_writer` —
			// отношение БЕЗ источников уровня аккаунта. Пообъектный гейт края от
			// этого никуда не делся, и именно его перепись здесь и утверждает:
			// меняется имя спрашиваемого отношения, а не наличие вопроса.
			fullMethod: "/kacho.cloud.iam.v1.UserService/Update",
			relation:   "record_writer", objectType: "iam_user", scopeField: "user_id",
			req: &iamv1.UpdateUserRequest{UserId: usrID}, targetObjID: usrID,
		},
		{
			// #1131: снятие строки личности ушло с `v_delete` на `identity_remover`
			// — отношение БЕЗ источников уровня аккаунта, ровно как соседняя правка
			// записи ушла на `record_writer` (#1102). Утверждение перевёрнуто, а не
			// снято: пообъектный гейт края никуда не делся, и его перепись здесь и
			// утверждается — меняется ИМЯ спрашиваемого отношения, а не наличие
			// вопроса. Удалить строку значило бы перестать стеречь то, что она
			// стерегла: что край спрашивает PDP про объект, который назвал
			// вызывающий (анти-BOLA).
			//
			// Что распорядителю аккаунта осталось вместо удаления — исключение из
			// аккаунта (`UserService/RemoveFromAccount`, #1127): оно снимает
			// членство и не трогает глобальную строку.
			fullMethod: "/kacho.cloud.iam.v1.UserService/Delete",
			relation:   "identity_remover", objectType: "iam_user", scopeField: "user_id",
			req: &iamv1.DeleteUserRequest{UserId: usrID}, targetObjID: usrID,
		},
		{
			fullMethod: "/kacho.cloud.iam.v1.GroupService/Update",
			relation:   "v_update", objectType: "iam_group", scopeField: "group_id",
			req: &iamv1.UpdateGroupRequest{GroupId: grpID}, targetObjID: grpID,
		},
		{
			fullMethod: "/kacho.cloud.iam.v1.GroupService/Delete",
			relation:   "v_delete", objectType: "iam_group", scopeField: "group_id",
			req: &iamv1.DeleteGroupRequest{GroupId: grpID}, targetObjID: grpID,
		},
		{
			fullMethod: "/kacho.cloud.iam.v1.GroupService/AddMember",
			relation:   "v_update", objectType: "iam_group", scopeField: "group_id",
			req: &iamv1.AddGroupMemberRequest{GroupId: grpID}, targetObjID: grpID,
		},
		{
			fullMethod: "/kacho.cloud.iam.v1.GroupService/RemoveMember",
			relation:   "v_update", objectType: "iam_group", scopeField: "group_id",
			req: &iamv1.RemoveGroupMemberRequest{GroupId: grpID}, targetObjID: grpID,
		},
		{
			fullMethod: "/kacho.cloud.iam.v1.RoleService/Update",
			relation:   "v_update", objectType: "iam_role", scopeField: "role_id",
			req: &iamv1.UpdateRoleRequest{RoleId: roleID}, targetObjID: roleID,
		},
		{
			fullMethod: "/kacho.cloud.iam.v1.RoleService/Delete",
			relation:   "v_delete", objectType: "iam_role", scopeField: "role_id",
			req: &iamv1.DeleteRoleRequest{RoleId: roleID}, targetObjID: roleID,
		},
		{
			fullMethod: "/kacho.cloud.iam.v1.ServiceAccountService/Update",
			relation:   "v_update", objectType: "iam_service_account", scopeField: "service_account_id",
			req: &iamv1.UpdateServiceAccountRequest{ServiceAccountId: saID}, targetObjID: saID,
		},
		{
			fullMethod: "/kacho.cloud.iam.v1.ServiceAccountService/Delete",
			relation:   "v_delete", objectType: "iam_service_account", scopeField: "service_account_id",
			req: &iamv1.DeleteServiceAccountRequest{ServiceAccountId: saID}, targetObjID: saID,
		},
	}
}

// embeddedCatalog loads the REAL build-time catalog the gateway ships with.
func embeddedCatalog(t *testing.T) *middleware.PermissionCatalog {
	t.Helper()
	c := middleware.NewPermissionCatalog()
	require.NoError(t, c.LoadFromBytes(middleware.EmbeddedPermissionCatalogJSON()))
	return c
}

// TestIAMOwnerGuardRPCs_ModelCarriesThePerObjectGate — (1)+(2): the catalog, not
// a Go check, is what decides these twelve, and it decides per OBJECT.
func TestIAMOwnerGuardRPCs_ModelCarriesThePerObjectGate(t *testing.T) {
	cat := embeddedCatalog(t)

	for _, rpc := range ownerGuardModelGatedRPCs() {
		t.Run(rpc.fullMethod, func(t *testing.T) {
			// The catalog is keyed on the dotted FQN (no leading slash); gRPC
			// FullMethod carries one.
			e, ok := cat.Lookup(strings.TrimPrefix(rpc.fullMethod, "/"))
			require.True(t, ok,
				"%s has no permission-catalog entry: the model does not gate it, so "+
					"removing the in-service owner check would leave it ungated", rpc.fullMethod)

			assert.NotEqual(t, "<exempt>", e.Permission,
				"%s must not be authz-exempt — it is a privileged mutation", rpc.fullMethod)
			assert.Equal(t, rpc.relation, e.RequiredRelation,
				"%s must require the per-object %s relation", rpc.fullMethod, rpc.relation)
			assert.Equal(t, rpc.objectType, e.ScopeExtractor.ObjectType,
				"%s must be scoped to object-type %s", rpc.fullMethod, rpc.objectType)
			assert.Equal(t, rpc.scopeField, e.ScopeExtractor.FromRequestField,
				"%s must resolve its target from request field %q (anti-BOLA: the Check "+
					"must target the object the caller named)", rpc.fullMethod, rpc.scopeField)
		})
	}
}

// TestIAMOwnerGuardRPCs_UngrantedSubjectStillDenied — (3), the assertion that
// matters. A principal the model does not grant must never reach the handler.
// This must hold for a machine principal exactly as for a human one: the point
// of removing the guard was to let a GRANTED machine through, not any machine.
func TestIAMOwnerGuardRPCs_UngrantedSubjectStillDenied(t *testing.T) {
	for _, rpc := range ownerGuardModelGatedRPCs() {
		for _, principal := range []struct{ id, ptype string }{
			{"usr0000000000000nope", "user"},
			{"sva0000000000000nope", "service_account"},
		} {
			t.Run(rpc.fullMethod+"/"+principal.ptype, func(t *testing.T) {
				checker := &fakeChecker{allowed: false, reasons: []string{"no path"}}
				mw := buildAuthzMiddleware(t, embeddedCatalog(t), checker)

				handlerReached := false
				_, err := mw.Unary()(
					withTokenMD(principal.id, principal.ptype), rpc.req,
					&grpc.UnaryServerInfo{FullMethod: rpc.fullMethod},
					func(ctx context.Context, req any) (any, error) {
						handlerReached = true
						return "ok", nil
					})

				require.Error(t, err, "%s must be denied for an ungranted %s", rpc.fullMethod, principal.ptype)
				st, _ := status.FromError(err)
				assert.Equal(t, codes.PermissionDenied, st.Code(),
					"an authenticated-but-ungranted subject must get PermissionDenied(7)")
				assert.False(t, handlerReached,
					"%s reached the handler despite a model DENY — the in-service owner "+
						"check is gone, so this would be an open door", rpc.fullMethod)
			})
		}
	}
}

// TestIAMOwnerGuardRPCs_GrantedSubjectReachesHandler — (4): with the model
// allowing, the RPC proceeds, and the PDP was asked the pinned question. A
// service_account subject is used deliberately: under the removed guard a
// machine principal could never satisfy `principal == account.owner_user_id`,
// so these twelve were unreachable for any machine client by construction.
func TestIAMOwnerGuardRPCs_GrantedSubjectReachesHandler(t *testing.T) {
	for _, rpc := range ownerGuardModelGatedRPCs() {
		t.Run(rpc.fullMethod, func(t *testing.T) {
			checker := &fakeChecker{allowed: true}
			mw := buildAuthzMiddleware(t, embeddedCatalog(t), checker)

			handlerReached := false
			_, err := mw.Unary()(
				withTokenMD("sva0000000000000ssss", "service_account"), rpc.req,
				&grpc.UnaryServerInfo{FullMethod: rpc.fullMethod},
				func(ctx context.Context, req any) (any, error) {
					handlerReached = true
					return "ok", nil
				})

			require.NoError(t, err)
			assert.True(t, handlerReached,
				"a model-granted machine principal must reach %s", rpc.fullMethod)

			last := checker.lastInput.Load()
			require.NotNil(t, last, "the PDP must actually have been consulted")
			assert.Equal(t, "service_account:sva0000000000000ssss", last.Subject)
			assert.Equal(t, rpc.relation, last.RequiredRelation,
				"the PDP must be asked for the catalog-pinned relation")
			assert.Equal(t, rpc.objectType, last.ResourceType)
			assert.Equal(t, rpc.targetObjID, last.ResourceID,
				"the PDP must be asked about the object the request named, not a wildcard")
		})
	}
}
