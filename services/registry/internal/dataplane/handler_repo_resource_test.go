// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package dataplane

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// handler_repo_resource_test.go — право записи в репозиторий и durable-след его
// регистрации. Три сцепленных свойства, которые проверяются здесь на НАБЛЮДАЕМОМ
// уровне (код ответа + что уехало в очередь намерений), а не по внутренним вызовам:
//
//  1. глагол записи выбирается по существованию РЕСУРСА (наложение ⊔ регистрация), а
//     не по наличию тегов в движке: заявленный и ещё пустой репозиторий защищён правом
//     на себя, а не общим для реестра правом «создавать репозитории»;
//  2. регистрация на первом push не может потеряться: она делается ДО отдачи 2xx, а
//     её несделанность — отказ, который клиент повторит, и повтор сходится;
//  3. отказ резолва проекта не превращается в «резолвер не сконфигурирован»:
//     деградированное намерение с пустым проектом не уезжает вовсе.

// ---- (1) существование РЕСУРСА, а не содержимого движка --------------------

// TestDataplane_DeclaredEmptyRepo_NeighbourWithRegistryCreate_Denied — репозиторий
// ЗАЯВЛЕН субъектом A через control-plane и пока пуст (ни одного тега). Сосед по
// реестру B держит только «создавать репозитории в реестре» и НЕ держит права на этот
// репозиторий.
//
// Ожидание: любая запись B — отказ, и после отказа у B нет НИ ОДНОГО пути получить
// права на чужой объект: намерение регистрации (которое несёт tuple владельца на
// пишущего) не эмитируется вовсе.
//
// RED до фикса: существование определялось наличием тегов, поэтому пустой заявленный
// репозиторий «не существовал» → выбиралась полоса создания → гейтом становилось право
// B, запись проходила, а ветка создания выписывала B ВЛАДЕЛЬЦЕМ чужого репозитория
// (владение читают все пять глаголов и отзывом чужой выдачи оно не снимается).
func TestDataplane_DeclaredEmptyRepo_NeighbourWithRegistryCreate_Denied(t *testing.T) {
	// B держит ТОЛЬКО namespace-право создавать репозитории в реестре.
	az := &fakeAuthz{allow: map[string]bool{"v_create registry_registry:reg-A": true}}
	fw := &fakeForwarder{status: 201}
	be := &fakeBackend{exists: map[string]bool{}}                     // тегов нет — движок репозитория не знает
	pr := &fakePresence{declared: map[string]bool{"reg-A/app": true}} // но РЕСУРС заявлен (A)
	rr := &fakeRepoReg{}
	h := newTestHandlerP(&fakeVerifier{subject: "sva-evil"}, az, be, pr, fw, rr)

	for _, step := range []struct {
		name, method, path string
	}{
		{"blob-upload init", http.MethodPost, "/v2/reg-A/app/blobs/uploads/"},
		{"blob finalize", http.MethodPut, "/v2/reg-A/app/blobs/uploads/u1?digest=sha256:x"},
		{"manifest PUT", http.MethodPut, "/v2/reg-A/app/manifests/v1"},
	} {
		rec := doReq(h, step.method, step.path, true)
		require.Equal(t, http.StatusForbidden, rec.Code,
			"%s в ЧУЖОЙ заявленный репозиторий отвергается", step.name)
		require.Equal(t, "DENIED", pushDenyCode(t, rec), step.name)
	}

	require.Equal(t, 0, fw.count(), "ни один отвергнутый запрос не доехал до движка")
	require.Empty(t, rr.registered(),
		"намерение регистрации НЕ эмитировано — иначе tuple владельца выписал бы нападающему "+
			"постоянные права на чужой репозиторий")

	// Гейтом обязано быть право на ЭТОТ репозиторий, а не общее право реестра.
	for _, c := range az.checkedObjects() {
		require.Equal(t, checkCall{"service_account:sva-evil", relVUpdate, "registry_repository:reg-A/app"}, c,
			"заявленный репозиторий гейтится правом на себя")
	}
}

// TestDataplane_DeclaredEmptyRepo_Mount_UsesRepositoryVerb — тот же предикат на пути
// монтирования блоба: существующий по РЕСУРСУ dst-репозиторий гейтится правом на себя.
// Без этого сосед монтировал бы слои в чужой заявленный репозиторий по общему для
// реестра праву.
func TestDataplane_DeclaredEmptyRepo_Mount_UsesRepositoryVerb(t *testing.T) {
	az := &fakeAuthz{allow: map[string]bool{
		"v_get registry_repository:reg-A/src": true, // src читать можно
		"v_create registry_registry:reg-A":    true, // и создавать репозитории в реестре
	}}
	fw := &fakeForwarder{}
	be := &fakeBackend{blobs: map[string]bool{"reg-A/src|sha256:layer": true}} // тегов у dst нет
	pr := &fakePresence{declared: map[string]bool{"reg-A/dst": true}}          // dst заявлен другим
	h := newTestHandlerP(&fakeVerifier{subject: "sva-evil"}, az, be, pr, fw, &fakeRepoReg{})

	rec := doReq(h, http.MethodPost, "/v2/reg-A/dst/blobs/uploads/?mount=sha256:layer&from=reg-A/src", true)
	require.Equal(t, http.StatusForbidden, rec.Code, "mount в чужой заявленный dst отвергается")
	require.Equal(t, 0, fw.count())
}

// TestDataplane_RepoPresenceUnavailable_FailClosed — предикат существования недоступен
// (БД не отвечает). Решение о праве принять нельзя ⇒ 503, а НЕ «считать, что ресурса
// нет» (это вернуло бы полосу создания и ровно ту дыру, ради которой предикат заведён).
func TestDataplane_RepoPresenceUnavailable_FailClosed(t *testing.T) {
	az := &fakeAuthz{} // allow-all: отказ обязан прийти ДО authz-решения
	fw := &fakeForwarder{}
	pr := &fakePresence{err: errors.New("db down")}
	h := newTestHandlerP(&fakeVerifier{subject: "sva-ci"}, az, &fakeBackend{}, pr, fw, &fakeRepoReg{})

	rec := doReq(h, http.MethodPost, "/v2/reg-A/app/blobs/uploads/", true)
	require.Equal(t, http.StatusServiceUnavailable, rec.Code, "предикат недоступен → fail-closed 503")
	require.Equal(t, 0, fw.count())
}

// ---- (2) регистрация на первом push не теряется ----------------------------

// TestDataplane_FirstPush_RegistrationFails_ClientGetsError_RetryReEmits — эмиссия
// намерения подавлена (сбой БД). Ожидание: клиент получает НЕ-2xx (значит повторит), а
// повторный push ПЕРЕЭМИТИТ намерение, а не проглотит его.
//
// RED до фикса: манифест проксировался стримингом, 2xx уходил клиенту ПЕРВЫМ, а сбой
// эмиссии оставался строкой в логе. Строки в очереди при этом нет вовсе, поэтому
// переигрывать её нечему, а повтор клиента шёл полосой изменения (в движке уже есть
// теги) и упирался в отказ — репозиторий оставался без прав навсегда.
//
// Вторая половина кейса — ЛОВУШКА, ради которой предикат и переехал на ресурс: повтор
// обязан снова идти полосой СОЗДАНИЯ, потому что durable-регистрации так и не появилось.
func TestDataplane_FirstPush_RegistrationFails_ClientGetsError_RetryReEmits(t *testing.T) {
	az := &fakeAuthz{allow: map[string]bool{"v_create registry_registry:reg-A": true}}
	fw := &fakeForwarder{status: 201}
	// Движок принимает манифест и на первой попытке: после неё теги ЕСТЬ — ровно то
	// состояние, в котором прежний предикат переключал глагол и ломал повтор.
	be := &fakeBackend{exists: map[string]bool{}}
	pr := &fakePresence{} // регистрация не закоммичена ⇒ ресурса нет
	rr := &fakeRepoReg{err: errors.New("outbox insert failed")}
	h := newTestHandlerP(&fakeVerifier{subject: "sva-ci"}, az, be, pr, fw, rr)

	first := doReq(h, http.MethodPut, "/v2/reg-A/app/manifests/v1", true)
	require.NotEqual(t, http.StatusCreated, first.Code,
		"регистрацию сделать durable не удалось → 2xx отдавать нельзя")
	require.GreaterOrEqual(t, first.Code, 500, "сбой durable-работы → fail-closed")

	// Клиент повторяет. Теги в движке после первой попытки уже есть — предикат обязан
	// смотреть не на них.
	be.exists["reg-A/app"] = true
	rr.err = nil
	second := doReq(h, http.MethodPut, "/v2/reg-A/app/manifests/v1", true)
	require.Equal(t, http.StatusCreated, second.Code, "повтор сходится: регистрация закоммичена, 2xx отдан")

	require.Len(t, rr.registered(), 2, "намерение переэмитировано на повторе (а не проглочено)")
	for _, c := range az.checkedObjects() {
		require.Equal(t, relVCreate, c.relation,
			"обе попытки гейтятся правом создания — durable-регистрации ещё не было")
	}
}

// TestDataplane_FirstPush_NilRegistrar_FailClosed — регистратор не подан. Записать
// durable-признак существования нечем ⇒ первый push отвергается, а не принимается «в
// no-op режиме»: 2xx на репозиторий, который никогда не станет ресурсом, — это тот же
// молчаливо потерянный след, только по причине настройки, а не сбоя.
func TestDataplane_FirstPush_NilRegistrar_FailClosed(t *testing.T) {
	az := &fakeAuthz{allow: map[string]bool{"v_create registry_registry:reg-A": true}}
	fw := &fakeForwarder{status: 201}
	h := newTestHandlerFull(&fakeVerifier{subject: "sva-ci"}, az, &fakeBackend{}, &fakePresence{},
		fw, nil, &fakeRegistryLookup{}, &fakeUploadRecorder{}, &fakePushGrantRecorder{})

	rec := doReq(h, http.MethodPut, "/v2/reg-A/app/manifests/v1", true)
	require.Equal(t, http.StatusServiceUnavailable, rec.Code,
		"нечем сделать регистрацию durable → первый push отвергается")
}

// TestDataplane_RePush_ExistingResource_StreamsAndDoesNotReRegister — репозиторий уже
// существует как ресурс: манифест проксируется как прежде (стримингом), намерение
// повторно не эмитируется, глагол — право на репозиторий. Контроль-кейс к предыдущему:
// буферизация и fail-closed введены ТОЛЬКО для первого push, обычная перезапись не
// платит за них ничем.
func TestDataplane_RePush_ExistingResource_StreamsAndDoesNotReRegister(t *testing.T) {
	az := &fakeAuthz{allow: map[string]bool{"v_update registry_repository:reg-A/app": true}}
	fw := &fakeForwarder{status: 201}
	be := &fakeBackend{exists: map[string]bool{"reg-A/app": true}}
	pr := &fakePresence{declared: map[string]bool{"reg-A/app": true}}
	rr := &fakeRepoReg{err: errors.New("registrar would fail if called")}
	pgr := &fakePushGrantRecorder{}
	h := newTestHandlerFull(&fakeVerifier{subject: "sva-ci"}, az, be, pr, fw, rr,
		&fakeRegistryLookup{}, &fakeUploadRecorder{}, pgr)

	rec := doReq(h, http.MethodPut, "/v2/reg-A/app/manifests/v1", true)
	require.Equal(t, http.StatusCreated, rec.Code, "перезапись существующего репозитория проходит")
	require.Empty(t, rr.registered(), "существующий ресурс не регистрируется повторно")
	require.Len(t, pgr.recordedKeys(), 1, "push-ownership фиксируется и на перезаписи")
}

// ---- (3) отказ резолва проекта ≠ «резолвер не сконфигурирован» -------------

// TestDataplane_FirstPush_ProjectLookupError_NoDegradedIntent — резолв owning-проекта
// реестра упал. Ожидание: намерение с ПУСТЫМ проектом НЕ уезжает, клиент получает
// не-2xx и повторит — повтор переэмитит намерение с авторитетным проектом.
//
// RED до фикса: сбой резолва возвращал "" — то же значение, которым обозначено
// «резолвер вообще не сконфигурирован». Намерение уезжало с пустым проектом, принималось
// принимающей стороной как валидное и помечалось отправленным; сопоставление шло только
// по общему для кластера уровню, поэтому участники и администратор проекта репозиторий
// не видели. Повтора не было (второй push шёл полосой изменения), переигрывать было нечего.
func TestDataplane_FirstPush_ProjectLookupError_NoDegradedIntent(t *testing.T) {
	az := &fakeAuthz{allow: map[string]bool{"v_create registry_registry:reg-A": true}}
	fw := &fakeForwarder{status: 201}
	rr := &fakeRepoReg{}
	lk := &fakeRegistryLookup{err: errors.New("registry lookup down")}
	h := newTestHandlerFull(&fakeVerifier{subject: "sva-ci"}, az, &fakeBackend{}, &fakePresence{},
		fw, rr, lk, &fakeUploadRecorder{}, &fakePushGrantRecorder{})

	rec := doReq(h, http.MethodPut, "/v2/reg-A/app/manifests/v1", true)
	require.Equal(t, http.StatusServiceUnavailable, rec.Code,
		"состояние проекта неизвестно → 2xx не отдаём, клиент повторит")
	require.Empty(t, rr.registered(),
		"деградированное намерение с пустым проектом НЕ эмитируется")

	// Резолвер починился — повтор доводит регистрацию с авторитетным проектом.
	lk.err = nil
	lk.projectByRegistry = map[string]string{"reg-A": "prj-owner"}
	retry := doReq(h, http.MethodPut, "/v2/reg-A/app/manifests/v1", true)
	require.Equal(t, http.StatusCreated, retry.Code)
	intents := rr.registered()
	require.Len(t, intents, 1, "ровно одно намерение — то, что уехало с проектом")
	require.Equal(t, "prj-owner", intents[0].ParentProjectID)
}

// TestDataplane_FirstPush_NilLookup_EmitsWithoutProject — резолвер НЕ сконфигурирован
// (порт nil). Это настройка, а не сбой, и она сохраняет прежнее поведение: намерение
// уезжает без проекта. Контроль-кейс к предыдущему — доказывает, что различаются именно
// два исхода, а не «любой пустой проект теперь запрещён».
func TestDataplane_FirstPush_NilLookup_EmitsWithoutProject(t *testing.T) {
	az := &fakeAuthz{allow: map[string]bool{"v_create registry_registry:reg-A": true}}
	fw := &fakeForwarder{status: 201}
	rr := &fakeRepoReg{}
	h := newTestHandlerFull(&fakeVerifier{subject: "sva-ci"}, az, &fakeBackend{}, &fakePresence{},
		fw, rr, nil, &fakeUploadRecorder{}, &fakePushGrantRecorder{})

	rec := doReq(h, http.MethodPut, "/v2/reg-A/app/manifests/v1", true)
	require.Equal(t, http.StatusCreated, rec.Code, "резолвер не сконфигурирован — прежнее поведение")
	intents := rr.registered()
	require.Len(t, intents, 1)
	require.Empty(t, intents[0].ParentProjectID)
}
