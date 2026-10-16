// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package dataplane

// system_class_anon_subject_test.go — плоскость данных реестра спрашивает о
// публичном объекте анонимом нового вида (приёмка kaname
// anonymous-and-authenticated-are-subject-classes.md, отпечаток fa4ff26f…bb21757e,
// SC-39; зеркало kacho#3238).
//
// Анонимный поток включён посадкой фикстуры (`WithAnonymousSubject`). Ответчик
// модели различает субъект вопроса: публикация SC-12 — это выдача классу
// `system_class:anonymous`, а не подстановке. Близнец — тот же репозиторий без
// публикации: прежний единый отказ «не найдено».

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

const scAnonClass = "system_class:anonymous"

// SC-39 — анонимное чтение публичного репозитория спрашивается субъектом
// `system_class:anonymous` и разрешено; запись тем же токеном — 403 DENIED;
// без публикации — единый 404.
func TestSystemClass_SC39_RegistryAsksAsTheAnonymousClass(t *testing.T) {
	// Близнец: без публикации ни на какой субъект — прежний единый отказ.
	twinAz := &subjectAuthz{allow: map[string]bool{}}
	twin := newAnonHandler(twinAz, &fakeBackend{exists: map[string]bool{"reg-A/public": true}}, &fakeForwarder{})
	twinRec := doReq(twin, http.MethodGet, "/v2/reg-A/public/manifests/v1", true)
	require.Equal(t, http.StatusNotFound, twinRec.Code, "SC-39 близнец: без публикации — 404")
	require.Equal(t, `{"errors":[{"code":"NAME_UNKNOWN","message":"not found"}]}`+"\n", twinRec.Body.String(),
		"SC-39 близнец: единый отказ «не найдено»")

	az := &subjectAuthz{allow: map[string]bool{
		scAnonClass + " v_get " + publicRepoObject: true, // публикация = выдача классу anonymous
	}}
	fw := &fakeForwarder{status: 200}
	be := &fakeBackend{exists: map[string]bool{"reg-A/public": true}}
	h := newAnonHandler(az, be, fw)

	rec := doReq(h, http.MethodGet, "/v2/reg-A/public/manifests/v1", true)
	require.Equal(t, http.StatusOK, rec.Code, "SC-39: анонимное чтение публичного r1 разрешено")
	require.True(t, az.hasSubject(scAnonClass), "SC-39: вопрос к модели — субъектом system_class:anonymous")
	require.False(t, az.hasSubject("user:*"), "SC-39: подстановка говорящим не бывает")

	push := doReq(h, http.MethodPut, "/v2/reg-A/public/manifests/v2", true)
	require.Equal(t, http.StatusForbidden, push.Code, "SC-39: запись тем же токеном — 403")
	require.Equal(t, "DENIED", pushDenyCode(t, push), "SC-39: код ошибки реестра")
}
