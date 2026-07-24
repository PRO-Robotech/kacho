// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// visibility_gate_test.go — regression-lock any-path-to-PUBLIC admin-gate (D-6) на
// FULL-OBJECT PATCH пути (пустой update_mask). api-conventions.md: «mask пустой →
// применяются все mutable-поля» — значит визибилити РЕАЛЬНО применяется и по этому
// пути, поэтому admin-gate обязан срабатывать одинаково для mask-пути и full-object
// PATCH. До фикса гейт был keyed на maskContains(...) → пустой mask его обходил, и
// не-admin с v_update публиковал реестр/репозиторий (раскрытие приватных образов).
//
// Ассерты — поведенческие (код + контракт-текст сообщения), не только тип ошибки.
package handler

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	registryv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/registry/v1"
)

// RG-1-B10-fullpatch — UpdateRegistry БЕЗ update_mask (full-object PATCH) с
// defaultRepositoryVisibility=PUBLIC от не-admin (v_update есть, admin нет) →
// PERMISSION_DENIED с тем же контракт-текстом, что и mask-путь. RED до фикса:
// гейт срабатывал только при поле в mask → пустой mask возвращал Operation (реестр
// становился публичным).
func TestRegistryHandler_D6_FullObjectPatchToPublic_NonAdmin_Denied(t *testing.T) {
	az := relAuthz{allow: map[string]bool{}} // admin НЕ выдан
	h := newTestHandler(&fakeZotH{}, az)

	_, err := h.Update(carolCtx(), &registryv1.UpdateRegistryRequest{
		RegistryId:                  validReg,
		DefaultRepositoryVisibility: registryv1.Visibility_PUBLIC,
		// UpdateMask отсутствует → full-object PATCH (mutable-поля применяются все).
	})

	require.Equal(t, codes.PermissionDenied, codeOf(t, err))
	require.Equal(t, "changing default visibility to public requires registry admin", statusMsg(err))
}

// RG-1-B11-fullpatch (positive control) — тот же full-object PATCH от admin →
// гейт пройден, возвращается Operation (гейт не «запретил всем»).
func TestRegistryHandler_D6_FullObjectPatchToPublic_Admin_Allowed(t *testing.T) {
	az := relAuthz{allow: map[string]bool{"admin " + regRef(): true}}
	h := newTestHandler(&fakeZotH{}, az)

	op, err := h.Update(carolCtx(), &registryv1.UpdateRegistryRequest{
		RegistryId:                  validReg,
		DefaultRepositoryVisibility: registryv1.Visibility_PUBLIC,
	})

	require.NoError(t, err)
	require.NotNil(t, op)
}

// RG-1-B12-fullpatch (boundary, анти-оверфикс) — full-object PATCH БЕЗ visibility
// (UNSPECIFIED) не трогает default_visibility (resolveUpdateMask применяет его только
// при заданном значении) → admin-gate НЕ срабатывает: не-admin editor обновляет
// description/labels пустым mask'ом как раньше.
func TestRegistryHandler_D6_FullObjectPatchWithoutVisibility_NotGated(t *testing.T) {
	az := relAuthz{allow: map[string]bool{}} // admin НЕ выдан
	h := newTestHandler(&fakeZotH{}, az)

	op, err := h.Update(carolCtx(), &registryv1.UpdateRegistryRequest{
		RegistryId:  validReg,
		Description: "just a description patch",
		// visibility UNSPECIFIED, mask пуст → визибилити не применяется.
	})

	require.NoError(t, err, "full-object PATCH без visibility не требует admin")
	require.NotNil(t, op)
}

// RG-1-B12-mask-noop (boundary) — непустой mask БЕЗ visibility-поля, но с PUBLIC в
// теле: поле не применяется (mask-дисциплина) → гейт не срабатывает.
func TestRegistryHandler_D6_MaskWithoutVisibilityField_NotGated(t *testing.T) {
	az := relAuthz{allow: map[string]bool{}}
	h := newTestHandler(&fakeZotH{}, az)

	op, err := h.Update(carolCtx(), &registryv1.UpdateRegistryRequest{
		RegistryId:                  validReg,
		Description:                 "d",
		DefaultRepositoryVisibility: registryv1.Visibility_PUBLIC, // в теле, но НЕ в mask
		UpdateMask:                  &fieldmaskpb.FieldMask{Paths: []string{"description"}},
	})

	require.NoError(t, err, "поле вне mask не применяется → gate не нужен")
	require.NotNil(t, op)
}

// RG-1-B02-fullpatch — UpdateRepository БЕЗ update_mask (full-object PATCH) с
// visibility=PUBLIC от не-admin (v_update на repo есть, admin на реестре нет) →
// PERMISSION_DENIED. Тот же класс обхода: resolveRepoUpdateMask на пустом mask
// выставляет ApplyVisibility=true, а handler-gate был keyed на maskContains.
func TestRepositoryHandler_B02_FullObjectPatchToPublic_NonAdmin_Denied(t *testing.T) {
	az := relAuthz{allow: map[string]bool{"v_update " + repoRef("public/img"): true}}
	h := newTestHandler(&fakeZotH{}, az)

	_, err := h.UpdateRepository(carolCtx(), &registryv1.UpdateRepositoryRequest{
		RegistryId: validReg,
		Repository: "public/img",
		Visibility: registryv1.Visibility_PUBLIC,
		// UpdateMask отсутствует → full-object PATCH.
	})

	require.Equal(t, codes.PermissionDenied, codeOf(t, err))
	require.Equal(t, "changing repository visibility requires registry admin", statusMsg(err))
}

// RG-1-B02-fullpatch-admin (positive control) — тот же PATCH от admin → гейт пройден.
func TestRepositoryHandler_B02_FullObjectPatchToPublic_Admin_Allowed(t *testing.T) {
	az := relAuthz{allow: map[string]bool{
		"v_update " + repoRef("public/img"): true,
		"admin " + regRef():                 true,
	}}
	h := newTestHandler(&fakeZotH{}, az)

	op, err := h.UpdateRepository(carolCtx(), &registryv1.UpdateRepositoryRequest{
		RegistryId: validReg,
		Repository: "public/img",
		Visibility: registryv1.Visibility_PUBLIC,
	})

	require.NoError(t, err)
	require.NotNil(t, op)
}
