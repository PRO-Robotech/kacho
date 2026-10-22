// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package dataplane

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// subject_form_test.go — kacho#3305: субъект проверенного токена неизвестной формы
// получает на плоскости данных явный отказ, а не субъект какого-либо класса.
//
// Отказ — тот же вызов, что у непрошедшего проверку токена (401 c
// error="invalid_token"), и он ставится ДО маршрута: ни Check службы прав, ни
// пересылки в движок такой запрос не порождает. Законный близнец меняет ровно один
// факт — `sub` порождённой формы — и проходит тот же путь до ответа движка.

// formSubjectSA — служебная учётка порождённой формы (законный близнец).
const formSubjectSA = "sva0123456789abcdefg"

// subjectFormRoutes — по запросу на каждый маршрут плоскости данных, включая
// рукопожатие `GET /v2/`: отказ стоит до разбора маршрута, и ни один не исключён.
var subjectFormRoutes = []struct{ method, target string }{
	{http.MethodGet, "/v2/"},
	{http.MethodGet, "/v2/_catalog"},
	{http.MethodGet, "/v2/reg-A/app/manifests/v1"},
	{http.MethodHead, "/v2/reg-A/app/manifests/v1"},
	{http.MethodGet, "/v2/reg-A/app/blobs/sha256:x"},
	{http.MethodGet, "/v2/reg-A/app/tags/list"},
	{http.MethodPost, "/v2/reg-A/app/blobs/uploads/"},
	{http.MethodPut, "/v2/reg-A/app/manifests/v1"},
}

func TestDataplane_3305_UnknownSubjectForm_RefusedFailClosed(t *testing.T) {
	unknown := []struct{ name, sub string }{
		// Прежний дефект: неизвестный префикс сводился к service_account.
		{"unknown-prefix", "grp_admins"},
		// Порождённая форма, но вид не субъектный.
		{"generated-form-non-subject-kind", "grp0123456789abcdefg"},
		// Известный префикс, не порождённая форма.
		{"known-prefix-unknown-form", "sva-ci"},
		{"user-prefix-unknown-form", "usr_alice"},
		{"model-subject-spelling", "service_account:" + formSubjectSA},
		{"trailing-space", formSubjectSA + " "},
	}
	for _, u := range unknown {
		for _, rt := range subjectFormRoutes {
			t.Run(u.name+" "+rt.method+" "+rt.target, func(t *testing.T) {
				az := &fakeAuthz{} // allow-all: отказ обязан прийти не от службы прав
				fw := &fakeForwarder{status: http.StatusOK}
				be := &fakeBackend{exists: map[string]bool{"reg-A/app": true}, blobs: map[string]bool{"reg-A/app|sha256:x": true}}
				h := newTestHandler(&fakeVerifier{subject: u.sub}, az, be, fw, &fakeRepoReg{})

				rec := doReq(h, rt.method, rt.target, true)

				require.Equal(t, http.StatusUnauthorized, rec.Code, "sub %q must be refused", u.sub)
				require.Contains(t, rec.Header().Get("WWW-Authenticate"), `error="invalid_token"`)
				require.Empty(t, az.checkedObjects(), "no authz Check for a subject of unknown form")
				require.Zero(t, fw.count(), "no forward for a subject of unknown form")
			})
		}
	}
}

// Законный близнец: та же арена, `sub` порождённой формы — запрос доходит до
// движка (pull) и до службы прав с субъектом service_account.
func TestDataplane_3305_GeneratedSubjectForm_Passes(t *testing.T) {
	az := &fakeAuthz{}
	fw := &fakeForwarder{status: http.StatusOK}
	be := &fakeBackend{exists: map[string]bool{"reg-A/app": true}}
	h := newTestHandler(&fakeVerifier{subject: formSubjectSA}, az, be, fw, &fakeRepoReg{})

	rec := doReq(h, http.MethodGet, "/v2/reg-A/app/manifests/v1", true)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, 1, fw.count())
	calls := az.checkedObjects()
	require.NotEmpty(t, calls)
	for _, c := range calls {
		require.Equal(t, "service_account:"+formSubjectSA, c.subject)
	}
}

// Анонимный принципал формы порождённого не имеет (kaname#908 запрещает ему её) и
// сводится к классу anonymous ДО суждения о форме: включённый анонимный поток
// отказом не задет. Выключенный — тот же `sub` судится формой и получает отказ
// (TestDataplane_RG1_AnonDisabled_TokenRefusedByForm).
func TestDataplane_3305_AnonymousSubject_ResolvedBeforeTheFormRefusal(t *testing.T) {
	az := &subjectAuthz{allow: map[string]bool{"system_class:anonymous v_get " + publicRepoObject: true}}
	fw := &fakeForwarder{status: http.StatusOK}
	be := &fakeBackend{exists: map[string]bool{"reg-A/public": true}}

	h := newAnonHandler(az, be, fw)
	rec := doReq(h, http.MethodGet, "/v2/reg-A/public/manifests/v1", true)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, 1, fw.count())
	require.True(t, az.hasSubject("system_class:anonymous"))
}

// Аварийный режим без проверки токена (verifier==nil) проверенного субъекта не
// имеет: суждение о форме — о `sub` ПРОВЕРЕННОГО токена, и breakglass им не задет.
func TestDataplane_3305_Breakglass_NotJudgedByForm(t *testing.T) {
	fw := &fakeForwarder{status: http.StatusOK}
	h := New(nil, nil, &fakeBackend{}, &fakePresence{}, fw, &fakeRepoReg{},
		&fakeRegistryLookup{}, &fakeUploadRecorder{}, &fakePushGrantRecorder{},
		"https://api.kacho.local/iam/token", "registry.kacho.local", nil)

	rec := doReq(h, http.MethodGet, "/v2/reg-A/app/manifests/v1", false)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, 1, fw.count())
}
