// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// permission_map_test.go — замки на КАРТУ ПРАВ storage: какое отношение и про
// какой объект спрашивается за каждым служимым RPC.
//
// # Почему файл переехал сюда из `internal/check`
//
// Пакет-обёртка над картой у storage был своим, седьмым по счёту, и вместе с ним
// уехал единственный источник, который эти замки могли спросить. Карта теперь
// ВЫВОДИТСЯ носителем из аннотаций дескрипторов служб, СЛУЖИМЫХ этим процессом, —
// и замки спрашивают её тем же путём.
//
// # Почему это СТРОЖЕ прежнего
//
// Прежний перечень RPC брался обходом proto-пакета `kacho.cloud.storage.v1`:
// «весь пакет» и «вся выставленная поверхность» совпадали ровно до тех пор, пока
// storage служил все службы своего пакета. Здесь набор снимается У САМИХ СЕРВЕРОВ
// после регистрации (`grpc.Server.GetServiceInfo`) — то есть у того же источника,
// который читает носитель на старте. Служить RPC и не отдать его дескриптор
// невозможно, это одна операция; значит метод, зарегистрированный в обход,
// невидимым не останется, а метод, который перестали служить, перестанет и
// требоваться.

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"google.golang.org/grpc"

	storagev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/storage/v1"
	"github.com/PRO-Robotech/kacho/pkg/authz"
	"github.com/PRO-Robotech/kacho/pkg/authz/catalogderive"
	"github.com/PRO-Robotech/kacho/pkg/operations"
	"github.com/PRO-Robotech/kacho/pkg/operations/operationspb"

	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/disktype"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/disktypebinding"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/image"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/snapshot"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/storagebackend"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/volume"
	"github.com/PRO-Robotech/kacho/services/storage/internal/handler"
)

// servedMethods — полные имена методов, которые процесс РЕАЛЬНО служит на обоих
// слушателях, снятые у самих серверов после регистрации.
//
// Use-cases собираются с нулевыми портами: ни один обработчик здесь не
// вызывается, предмет — только состав зарегистрированного.
func servedMethods(t *testing.T) []string {
	t.Helper()
	var served []string
	for _, reg := range registrarsOfBothListeners(t) {
		srv := grpc.NewServer()
		reg(srv)
		for name, info := range srv.GetServiceInfo() {
			for _, m := range info.Methods {
				served = append(served, "/"+name+"/"+m.Name)
			}
		}
	}
	sort.Strings(served)
	if len(served) == 0 {
		t.Fatal("ни один метод не зарегистрирован — всякое утверждение про карту прав было бы " +
			"верно и на пустом наборе, то есть не отличало бы исправное от сломанного")
	}
	return served
}

// permissionMap — карта прав, выведенная из доменов СЛУЖИМОГО набора: тот же путь,
// которым её выводит носитель на старте.
func permissionMap(t *testing.T) authz.RPCMap {
	t.Helper()
	domains := map[string]struct{}{}
	for _, m := range servedMethods(t) {
		svc, _, _ := strings.Cut(strings.TrimPrefix(m, "/"), "/")
		if i := strings.LastIndex(svc, "."); i > 0 {
			domains[svc[:i]] = struct{}{}
		}
	}
	var list []string
	for d := range domains {
		list = append(list, d)
	}
	sort.Strings(list)
	m, err := catalogderive.Derive(list...)
	if err != nil {
		t.Fatalf("карта прав не выводится из аннотаций доменов %v: %v", list, err)
	}
	t.Logf("осмотрено: доменов %v, строк карты %d", list, len(m))
	return m
}

// TestPermissionMapCoversEveryServedRPC — гейт КЛАССА «служимый RPC без записи в
// карте» (security.md инв. 4 «permission-catalog полон и в синхроне»).
//
// Звено решения о доступе fail-closed: RPC без записи → `PermissionDenied
// "permission denied (rpc not mapped)"` на КАЖДЫЙ вызов, независимо от грантов и
// транспорта. Класс стрелял дважды: публичный ImageService (108 падений e2e) и
// InternalImageService/GetInternal (зарегистрирован, запись в каталоге края есть, в
// карте сервиса не было).
//
// Носитель то же самое отвергает на старте (О2). Проба его не дублирует, а делает
// отказ ВИДИМЫМ В ПРОГОНЕ: О2 сработает при подъёме процесса, то есть на стенде, а
// здесь — в сборке, и назовёт метод.
func TestPermissionMapCoversEveryServedRPC(t *testing.T) {
	m := permissionMap(t)
	served := servedMethods(t)

	// Домен storage — единственный, чью ФОРМУ записи судит этот файл. LRO-конверт
	// (`kacho.cloud.operation`) поднимает каждый сервис платформы, форма его
	// записей — предмет владельца конверта, а не storage; утверждать про неё здесь
	// значило бы завести седьмое место об одном предмете. Присутствие в карте
	// требуется ОТ ВСЕГО служимого набора — это и есть то, из-за чего класс стрелял.
	const ownDomain = "/kacho.cloud.storage.v1."
	own, foreign := 0, 0

	for _, fullMethod := range served {
		entry, ok := m[fullMethod]
		if !ok {
			t.Errorf("%s служится, но записи в карте прав нет: звено отвергнет его как "+
				"незамапленный на каждый вызов", fullMethod)
			continue
		}
		if !strings.HasPrefix(fullMethod, ownDomain) {
			foreign++
			continue
		}
		own++
		if entry.Permission == "" && !entry.Public {
			t.Errorf("%s: строка права пуста — аудит kaname не отличит этот вызов ни от чего", fullMethod)
		}

		// `scope_filtered` — авторизация ПЕРЕЕХАЛА на данные (пообъектное сужение
		// прочитанной страницы), а не исчезла: единичного объекта, про который
		// можно спросить заранее, у такого RPC нет. Поэтому отношение и извлекатель
		// у него не просто «не заполнены» — их наличие означало бы возврат к
		// единичному вопросу. Требуем обратного явно, и запрещаем совмещение с
		// exempt: это разные исходы.
		if entry.ScopeFiltered {
			if entry.Public {
				t.Errorf("%s: запись одновременно scope_filtered и exempt — это разные исходы", fullMethod)
			}
			if entry.Relation != "" {
				t.Errorf("%s: scope_filtered-запись несёт отношение %q — вопрос, который никто "+
					"не задаёт", fullMethod, entry.Relation)
			}
			if entry.Extract != nil {
				t.Errorf("%s: scope_filtered-запись несёт извлекатель объекта", fullMethod)
			}
			continue
		}

		// `<exempt>` — проверка прав СНЯТА осознанно, и запись это говорит прямо:
		// глобальный каталог, читаемый любым аутентифицированным (authN при этом
		// обязателен — его требует ветка `<exempt>` на крае). Отношения и
		// извлекателя у такой записи быть не может: они означали бы вопрос,
		// который никто не задаёт. Требуем этого явно, а не пропускаем молча, —
		// иначе снятая по ошибке проверка была бы неотличима от снятой по решению.
		if entry.Public {
			if entry.Relation != "" {
				t.Errorf("%s: запись `<exempt>` несёт отношение %q — вопрос, который никто "+
					"не задаёт", fullMethod, entry.Relation)
			}
			if entry.Extract != nil {
				t.Errorf("%s: запись `<exempt>` несёт извлекатель объекта", fullMethod)
			}
			continue
		}

		if entry.Relation == "" {
			t.Errorf("%s: required_relation не задано", fullMethod)
		}
		if entry.Extract == nil {
			t.Errorf("%s: запись не несёт извлекателя объекта", fullMethod)
		}
	}
	if own == 0 {
		t.Fatalf("среди %d служимых методов нет НИ ОДНОГО из домена storage — форму записи "+
			"не проверил никто, и «ноль находок» здесь означало бы «ноль прочитанного»", len(served))
	}
	t.Logf("осмотрено служимых методов: %d (домена storage — %d, чужих доменов — %d)",
		len(served), own, foreign)
}

// TestPermissionMapObjectAndProjectScope — регрессия против
// https://github.com/PRO-Robotech/kacho/issues/62.
//
// Внутрисервисный пол авторизации гейтил КАЖДЫЙ тенантский RPC на кластерном
// синглтоне через статический извлекатель, поэтому project-scoped `editor` —
// у которого ЕСТЬ `project:<p>#viewer/editor`, но нет кластерного гранта — получал
// 403 на List/Get/Create/Update/Delete СВОЕГО проекта (край те же вызовы уже
// разрешал). Посев с кластерным админом это маскировал.
//
// Ось — SCOPE, а не глагол в имени метода: то, что адресует САМ ресурс, спрашивает
// его глагол; то, что анкерится на родительском проекте, спрашивает ярус проекта.
func TestPermissionMapObjectAndProjectScope(t *testing.T) {
	m := permissionMap(t)

	type want struct{ objType, objID string }
	cases := map[string]struct {
		req  any
		want want
	}{
		// ---- VolumeService: List/Create → project; object-self → storage_volume ----
		"/kacho.cloud.storage.v1.VolumeService/List": {
			req: &storagev1.ListVolumesRequest{ProjectId: "prj_a"}, want: want{"project", "prj_a"},
		},
		"/kacho.cloud.storage.v1.VolumeService/Create": {
			req: &storagev1.CreateVolumeRequest{ProjectId: "prj_a"}, want: want{"project", "prj_a"},
		},
		"/kacho.cloud.storage.v1.VolumeService/Get": {
			req: &storagev1.GetVolumeRequest{VolumeId: "vol_1"}, want: want{"storage_volume", "vol_1"},
		},
		"/kacho.cloud.storage.v1.VolumeService/Update": {
			req: &storagev1.UpdateVolumeRequest{VolumeId: "vol_1"}, want: want{"storage_volume", "vol_1"},
		},
		"/kacho.cloud.storage.v1.VolumeService/Delete": {
			req: &storagev1.DeleteVolumeRequest{VolumeId: "vol_1"}, want: want{"storage_volume", "vol_1"},
		},
		"/kacho.cloud.storage.v1.VolumeService/ListOperations": {
			req: &storagev1.ListVolumeOperationsRequest{VolumeId: "vol_1"}, want: want{"storage_volume", "vol_1"},
		},

		// ---- SnapshotService ----
		"/kacho.cloud.storage.v1.SnapshotService/List": {
			req: &storagev1.ListSnapshotsRequest{ProjectId: "prj_a"}, want: want{"project", "prj_a"},
		},
		"/kacho.cloud.storage.v1.SnapshotService/Create": {
			req: &storagev1.CreateSnapshotRequest{ProjectId: "prj_a"}, want: want{"project", "prj_a"},
		},
		"/kacho.cloud.storage.v1.SnapshotService/Get": {
			req: &storagev1.GetSnapshotRequest{SnapshotId: "snp_1"}, want: want{"storage_snapshot", "snp_1"},
		},
		"/kacho.cloud.storage.v1.SnapshotService/Update": {
			req: &storagev1.UpdateSnapshotRequest{SnapshotId: "snp_1"}, want: want{"storage_snapshot", "snp_1"},
		},
		"/kacho.cloud.storage.v1.SnapshotService/Delete": {
			req: &storagev1.DeleteSnapshotRequest{SnapshotId: "snp_1"}, want: want{"storage_snapshot", "snp_1"},
		},

		// ---- ImageService ----
		"/kacho.cloud.storage.v1.ImageService/List": {
			req: &storagev1.ListImagesRequest{ProjectId: "prj_a"}, want: want{"project", "prj_a"},
		},
		"/kacho.cloud.storage.v1.ImageService/Create": {
			req: &storagev1.CreateImageRequest{ProjectId: "prj_a"}, want: want{"project", "prj_a"},
		},
		"/kacho.cloud.storage.v1.ImageService/Get": {
			req: &storagev1.GetImageRequest{ImageId: "img_1"}, want: want{"storage_image", "img_1"},
		},
		"/kacho.cloud.storage.v1.ImageService/Update": {
			req: &storagev1.UpdateImageRequest{ImageId: "img_1"}, want: want{"storage_image", "img_1"},
		},
		"/kacho.cloud.storage.v1.ImageService/Delete": {
			req: &storagev1.DeleteImageRequest{ImageId: "img_1"}, want: want{"storage_image", "img_1"},
		},
		"/kacho.cloud.storage.v1.ImageService/ListOperations": {
			req: &storagev1.ListImageOperationsRequest{ImageId: "img_1"}, want: want{"storage_image", "img_1"},
		},

		// ---- InternalVolumeService (:9091, координация привязки) — пообъектно ----
		//
		// Записи ниже покрывают ТОЛЬКО том. Attach/Detach двухобъектные: запрос
		// называет ещё и машину, у которой другой владелец, и право на неё
		// спрашивает use-case (volume.requireInstanceControl) — у записи карты
		// второго слота нет и быть не должно, это ответ на один вопрос. Не читать
		// эти строки как «здесь весь гейт»: замок второго вопроса —
		// volume.TestEveryStorageRPCNamingAnInstanceAsksTheModelAboutIt.
		"/kacho.cloud.storage.v1.InternalVolumeService/Attach": {
			req: &storagev1.AttachVolumeRequest{VolumeId: "vol_1"}, want: want{"storage_volume", "vol_1"},
		},
		"/kacho.cloud.storage.v1.InternalVolumeService/Detach": {
			req: &storagev1.DetachVolumeRequest{VolumeId: "vol_1"}, want: want{"storage_volume", "vol_1"},
		},
		"/kacho.cloud.storage.v1.InternalVolumeService/GetInternal": {
			req: &storagev1.GetInternalVolumeRequest{VolumeId: "vol_1"}, want: want{"storage_volume", "vol_1"},
		},

		// ---- InternalImageService (:9091, инфра-проекция) — пообъектно ----
		"/kacho.cloud.storage.v1.InternalImageService/GetInternal": {
			req: &storagev1.GetInternalImageRequest{ImageId: "img_1"}, want: want{"storage_image", "img_1"},
		},
	}

	for fullMethod, tc := range cases {
		entry, ok := m[fullMethod]
		if !ok {
			t.Errorf("%s: записи в карте прав нет", fullMethod)
			continue
		}
		if entry.Extract == nil {
			t.Errorf("%s: запись не несёт извлекателя объекта", fullMethod)
			continue
		}
		objType, objID, err := entry.Extract(tc.req)
		if err != nil {
			t.Errorf("%s: извлекатель вернул ошибку: %v", fullMethod, err)
			continue
		}
		if objType != tc.want.objType || objID != tc.want.objID {
			t.Errorf("%s: вопрос задаётся про %s:%s, ожидалось %s:%s (зеркало каталога края)",
				fullMethod, objType, objID, tc.want.objType, tc.want.objID)
		}
	}
}

// TestNoTenantDataOnTheClusterSingleton — на синглтон `cluster` можно вешать
// справочник и админ-поверхность, но НЕ данные конкретных тенантов.
//
// # Что здесь на самом деле опасно
//
// Опасен не тип объекта, а ОТНОШЕНИЕ. `cluster.viewer` объявлен как
// `[user, user:*, service_account] or …`, и бутстрап кластера НАМЕРЕННО пишет
// `cluster:<root>#viewer@user:*`, чтобы глобальный справочник (регионы, зоны, типы
// дисков) читал любой аутентифицированный субъект. Поэтому кластерный `viewer` на
// RPC, отдающем данные конкретных тенантов, — не проверка, а её видимость:
// пропускает всех. Ровно так перечисление привязок отдавало строки том↔машина для
// любых названных машин, из чужих проектов и аккаунтов.
//
// Админ-отношения того же типа (`system_admin`, `editor`, `admin`) подстановкой
// НЕ выполнимы: в их объявлении `user:*` нет. Кластерный синглтон для них — верный
// предмет вопроса: администрируемая сущность одна на кластер, и спрашивать про неё
// пообъектно не о чем.
//
// # Гейт проверяет СВОЮ предпосылку
//
// Разделение выше верно ровно до тех пор, пока `user:*` стоит там, где стоит
// сейчас. Модель — чужой файл, она меняется без нас, и молчаливое добавление
// подстановки в админ-отношение обессмыслило бы всю проверку, оставив её зелёной.
// Поэтому перечень выполнимых подстановкой отношений ВЫВОДИТСЯ ИЗ МОДЕЛИ, а не
// выписывается здесь.
func TestNoTenantDataOnTheClusterSingleton(t *testing.T) {
	wildcard := wildcardSatisfiableClusterRelations(t)
	if len(wildcard) != 1 || !wildcard["viewer"] {
		t.Fatalf("предпосылка гейта изменилась: подстановкой на cluster выполнимы %v, "+
			"а разделение ниже построено на том, что это ровно `viewer`", keysOf(wildcard))
	}

	// Записи, для которых кластерный синглтон — правильный предмет вопроса ПРИ
	// выполнимом подстановкой отношении: глобальный admin-curated каталог,
	// одинаковый для всех тенантов и читаемый каждым.
	//
	// НЕ ПУСТО, И ЭТО РЕШЕНИЕ (#893/#895). Чтение каталога типов дисков стоит
	// здесь по существу: ответ — администрируемый платформой инвентарь, одинаковый
	// для всех арендаторов, без единой арендаторской строки.
	//
	// ИСТОРИЯ ДВУХ ПОЛОВИННЫХ ПРАВД. Сначала запись гейтилась `viewer`, которого не
	// производил ни один посев, — проверка отвечала отказом каждому, и том создать
	// было нельзя. Потом (#892) её перевели на `<exempt>` — отказ ушёл, но доступ
	// стал невидим: освобождение не показывается перечислением выдач и не
	// отзывается ничем, кроме выкатки. Теперь у `viewer` на кластере есть
	// производитель — СИСТЕМНАЯ ВЫДАЧА с подстановочным субъектом, — поэтому
	// отношение выполняется за всякого аутентифицированного, и при этом доступ
	// виден на поверхности выдач и закрывается операцией администратора.
	//
	// Анти-вакуум по этой полосе держится сравнением ниже: справочных записей две,
	// и увиденных обязано быть две.
	clusterCatalog := map[string]bool{
		"/kacho.cloud.storage.v1.DiskTypeService/Get":  true,
		"/kacho.cloud.storage.v1.DiskTypeService/List": true,
	}
	// Отношения админ-поверхности: подстановкой не выполнимы, поэтому кластерный
	// синглтон для них законен без поимённого перечня.
	adminTier := map[string]bool{"system_admin": true, "editor": true, "admin": true}

	var seenWildcard, seenAdmin int
	for fullMethod, entry := range permissionMap(t) {
		// Тип объекта восстанавливается вызовом извлекателя на НУЛЕВОМ запросе
		// метода (тот же приём, которым сверяется каталог края) — сам извлекатель
		// хранится замыканием и прочитан быть не может.
		objType, ok := catalogderive.ScopeObjectType(fullMethod, entry)
		if !ok || objType != "cluster" {
			continue // пообъектная запись: её извлекатель читает поле запроса
		}
		switch {
		case wildcard[entry.Relation]:
			seenWildcard++
			if !clusterCatalog[fullMethod] {
				t.Errorf("%s спрашивает на кластерном синглтоне отношение %q, выполнимое "+
					"подстановкой `user:*`, — такая проверка пропускает КАЖДОГО "+
					"аутентифицированного субъекта. Это позволено только глобальному "+
					"справочнику; RPC, отдающий тенантские строки, обязан быть пообъектным "+
					"либо scope_filtered", fullMethod, entry.Relation)
			}
		case adminTier[entry.Relation]:
			seenAdmin++
		default:
			t.Errorf("%s спрашивает на кластерном синглтоне отношение %q, которое не "+
				"объявлено ни справочным, ни админским — классифицируй его явно, а не "+
				"полагайся на умолчание", fullMethod, entry.Relation)
		}
	}

	// Анти-вакуум по ОБЕИМ полосам: восстановление типа идёт через proto-реестр и
	// на неудаче молча отдаёт «нет ответа». Если бы оно перестало работать, обход
	// не проверил бы НИЧЕГО и остался зелёным.
	if seenWildcard != len(clusterCatalog) {
		t.Fatalf("обход увидел %d справочных кластерных записей вместо %d — проверка ничего не утверждает",
			seenWildcard, len(clusterCatalog))
	}
	if seenAdmin == 0 {
		t.Fatal("обход не увидел ни одной админской кластерной записи — вторая полоса проверки пуста")
	}
	t.Logf("осмотрено кластерных записей: справочных %d, админских %d", seenWildcard, seenAdmin)
}

// wildcardSatisfiableClusterRelations читает МОДЕЛЬ и отвечает, какие отношения
// типа `cluster` выполнимы подстановкой `user:*`.
//
// Читается именно объявление, а не память автора: подстановка, добавленная в
// админ-отношение, обессмыслила бы разделение выше — и сделала бы это молча.
func wildcardSatisfiableClusterRelations(t *testing.T) map[string]bool {
	t.Helper()
	const modelPath = "../../../../proto/kacho/cloud/iam/v1/fga_model.fga"
	raw, err := os.ReadFile(modelPath)
	if err != nil {
		t.Fatalf("модель прав не прочитана (%s): %v", modelPath, err)
	}
	lines := strings.Split(string(raw), "\n")
	out := map[string]bool{}
	inCluster := false
	scanned := 0
	for _, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		if strings.HasPrefix(trimmed, "type ") {
			inCluster = trimmed == "type cluster"
			continue
		}
		if !inCluster || !strings.HasPrefix(trimmed, "define ") {
			continue
		}
		scanned++
		name, body, found := strings.Cut(strings.TrimPrefix(trimmed, "define "), ":")
		if !found {
			continue
		}
		if strings.Contains(body, "user:*") {
			out[strings.TrimSpace(name)] = true
		}
	}
	if scanned == 0 {
		t.Fatalf("в модели не найдено ни одного отношения типа cluster — предпосылка не прочитана, а не подтверждена")
	}
	t.Logf("предпосылка: прочитано отношений типа cluster — %d, выполнимых подстановкой — %d", scanned, len(out))
	return out
}

// keysOf — имена ключей для сообщения об изменившейся предпосылке.
func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestListAttachmentsIsScopeFiltered — привязки томов авторизуются на уровне
// ДАННЫХ, а не единичным вопросом.
//
// Машины называет вызывающий, а ответ касается томов, у каждого из которых свой
// владелец: одного объекта, про который можно спросить заранее, здесь нет. Прежний
// вопрос — `viewer` на синглтоне `cluster` — относился к ГЛОБАЛЬНОМУ СПРАВОЧНИКУ,
// и бутстрап намеренно делает его выполнимым подстановкой. Значит проверка
// пропускала всех.
//
// Locked здесь: кластерное отношение НЕ восстановлено, метод НЕ помечен exempt, и
// имя метода совпадает с тем, на которое дескриптор несёт проводку сужателя, —
// иначе носитель откажет в старте (О3/О4), но лучше узнать об этом в сборке.
func TestListAttachmentsIsScopeFiltered(t *testing.T) {
	e, ok := permissionMap(t)[string(listAttachmentsMethod)]
	if !ok {
		t.Fatalf("%s: записи в карте прав нет", listAttachmentsMethod)
	}
	if !e.ScopeFiltered {
		t.Error("видимость обязана решаться пообъектно в use-case, а не единичным вопросом")
	}
	if e.Public {
		t.Error("не exempt: авторизация переехала на данные, а не исчезла")
	}
	if e.Relation != "" {
		t.Errorf("кластерное отношение %q здесь пропускало каждого аутентифицированного субъекта "+
			"(`cluster:<root>#viewer@user:*` пишет бутстрап) — не восстанавливать", e.Relation)
	}
	if e.Extract != nil {
		t.Error("объекта, про который можно спросить заранее, у этого RPC нет")
	}
	if e.Permission != "storage.volumes.listAttachments" {
		t.Errorf("строка права = %q: метод обязан оставаться различимым в аудите", e.Permission)
	}
}

// TestInterceptorAsksNothingForScopeFilteredAndStillAsksForTheNeighbour —
// поведенческая пара на одном звене.
//
// Первая половина: за `scope_filtered`-RPC звено НЕ задаёт единичного вопроса и
// пропускает вызов к обработчику, который сузит ответ пообъектно. `calls==0` ловит
// регрессию, при которой сюда вернули бы кластерный вопрос (он снова пропускал бы
// всех).
//
// Вторая обязательна: без неё «вопрос не задаётся» зеленело бы и на звене, которое
// не спрашивает НИ ЗА ЧТО. Соседний internal RPC того же сервиса (объект в запросе
// ЕСТЬ) обязан спросить модель и отвергнуть вызов на её «нет».
func TestInterceptorAsksNothingForScopeFilteredAndStillAsksForTheNeighbour(t *testing.T) {
	newLink := func(allow bool) (grpc.UnaryServerInterceptor, *int) {
		calls := 0
		return authz.NewInterceptor(authz.InterceptorOptions{
			Cache:       authz.NewCache(0),
			ServiceName: "kacho-storage-probe",
			Map:         permissionMap(t),
			Client: authz.CheckClientFunc(func(context.Context, string, string, string) (bool, error) {
				calls++
				return allow, nil
			}),
		}).Unary(), &calls
	}
	principal := operations.WithPrincipal(context.Background(), operations.Principal{
		Type: "user", ID: "usr_alice", DisplayName: "probe",
	})

	t.Run("scope_filtered пропускается к обработчику без вопроса", func(t *testing.T) {
		link, calls := newLink(false) // отказ: если вопрос задан, до обработчика не дойдём
		reached := false
		_, err := link(principal,
			&storagev1.ListAttachmentsRequest{InstanceIds: []string{"ins-x"}},
			&grpc.UnaryServerInfo{FullMethod: string(listAttachmentsMethod)},
			func(context.Context, any) (any, error) {
				reached = true
				return &storagev1.ListAttachmentsResponse{}, nil
			})
		if err != nil {
			t.Fatalf("вызов отвергнут: %v", err)
		}
		if !reached {
			t.Fatal("обработчик не отработал — он и есть точка авторизации этого RPC")
		}
		if *calls != 0 {
			t.Fatalf("единичный вопрос задан %d раз(а): спрашивать здесь нечего", *calls)
		}
	})

	t.Run("пообъектный сосед по-прежнему спрашивает", func(t *testing.T) {
		link, calls := newLink(false)
		reached := false
		_, err := link(principal,
			&storagev1.GetInternalVolumeRequest{VolumeId: "vol00000000000000001"},
			&grpc.UnaryServerInfo{FullMethod: "/kacho.cloud.storage.v1.InternalVolumeService/GetInternal"},
			func(context.Context, any) (any, error) {
				reached = true
				return &storagev1.VolumeInternal{}, nil
			})
		if err == nil {
			t.Fatal("модель ответила «нет», а вызов прошёл")
		}
		if reached {
			t.Fatal("обработчик отработал вопреки отказу модели")
		}
		if *calls != 1 {
			t.Fatalf("модель спрошена %d раз(а), ожидался ровно один вопрос", *calls)
		}
	})
}

// TestScopeFilteredRPCsAreBackedByTheProductionBootGuard — марка `scope_filtered`
// снимает per-RPC вопрос, поэтому единственной защитой такого RPC остаётся
// пообъектный сужатель. Значит сужатель не может быть просто ручкой, которую
// выключили и не заметили: пока в карте есть хотя бы одна такая запись, остаток
// собственного стража ОБЯЗАН отказывать в старте при выключенном сужателе.
//
// Проба связывает два артефакта наблюдаемо: карту (какие RPC остались без вопроса)
// и `config.Validate` (что именно отвергается на старте). Уберут стражу —
// покраснеет здесь, где видно, ЧТО осталось без защиты, а не только в конфиг-тестах.
func TestScopeFilteredRPCsAreBackedByTheProductionBootGuard(t *testing.T) {
	var scopeFiltered []string
	for fullMethod, e := range permissionMap(t) {
		if e.ScopeFiltered {
			scopeFiltered = append(scopeFiltered, fullMethod)
		}
	}
	sort.Strings(scopeFiltered)
	if len(scopeFiltered) == 0 {
		t.Fatal("карта прав не несёт НИ ОДНОЙ записи scope_filtered: либо полоса ушла из каталога " +
			"(тогда снимите и стражу, и эту пробу), либо карта не вывелась — и тогда проба молчит " +
			"не потому, что защищать нечего")
	}

	cfg := bootConfig(t, map[string]string{
		"KACHO_STORAGE_AUTH_MODE":              "production",
		"KACHO_STORAGE_DB_SSLMODE":             "require",
		"KACHO_STORAGE_IAM_CLIENT_MTLS_ENABLE": "true",
		"KACHO_STORAGE_LIST_FILTER_ENABLED":    "false", // ослаблено ровно одно измерение
		// Плоскость данных — часть боевой посадки, и без неё старт отказывает по
		// СВОЕЙ причине. Здесь она объявлена, чтобы отказ ниже относился ровно к
		// сужателю: проба, где отвергается сразу два измерения, не отличает одно
		// от другого и зеленела бы при снятии любого из них.
		"KACHO_STORAGE_BLOCK_BACKEND_KIND":               "CEPH_RBD",
		"KACHO_STORAGE_BLOCK_BACKEND_INSTALL_PREFIX":     "kc7f",
		"KACHO_STORAGE_BLOCK_BACKEND_CREDENTIALS_DIR":    "/etc/kacho/storage/credentials",
		"KACHO_STORAGE_BLOCK_BACKEND_CALL_TIMEOUT":       "30s",
		"KACHO_STORAGE_BLOCK_BACKEND_RECONCILE_INTERVAL": "15s",
		"KACHO_STORAGE_BLOCK_BACKEND_RECONCILE_BATCH":    "100",
	})
	err := cfg.Validate()
	if err == nil {
		t.Fatalf("боевая посадка с выключенным сужателем обязана отказать в старте: %v остались "+
			"без per-RPC вопроса и полагаются на него целиком", scopeFiltered)
	}
	if !strings.Contains(err.Error(), "LIST_FILTER_ENABLED") {
		t.Fatalf("отказ обязан назвать ручку, получено: %v", err)
	}

	// Обратная сторона: та же посадка со включённым сужателем стартует — страж
	// отвергает именно это измерение, а не «всё подряд».
	cfg.ListFilterEnabled = true
	if err := cfg.Validate(); err != nil {
		t.Fatalf("та же посадка со включённым сужателем не стартует: %v", err)
	}
	t.Log(fmt.Sprintf("осмотрено scope_filtered-записей: %d (%s)",
		len(scopeFiltered), strings.Join(scopeFiltered, ", ")))
}

// TestPublicListenerServesNoInternalService — ban #6 на уровне НАБОРА, а не обзора.
//
// Комментарий у `registerInternal` обещает, что разделение «Internal* только на
// внутреннем слушателе» проверяется через `grpc.Server.GetServiceInfo`. Обещание
// было ложным: единственный потребитель обоих регистраторов сливал их наборы в один
// список и различия между ними не утверждал никогда. Комментарий о защите, которой
// нет, — ловушка (`security.md` §Hardening инв. 5): следующий контрибьютор добавит
// `Internal*`-службу в публичный регистратор, получит зелёное и прочтёт комментарий
// как подтверждение.
//
// Проба спрашивает наборы ПОРОЗНЬ и утверждает про каждый своё: в публичном нет ни
// одной службы с `Internal` в имени, во внутреннем — есть хотя бы одна. Вторая
// половина не украшение: без неё проба зеленела бы и на пустом внутреннем наборе,
// то есть «Internal* нигде не служится» читалось бы как «разделение соблюдено».
func TestPublicListenerServesNoInternalService(t *testing.T) {
	volumeUC := volume.New(nil, nil, nil, nil, nil, nil)
	snapshotUC := snapshot.New(nil, nil, nil, nil)
	imageUC := image.New(nil, nil, nil, nil, nil, nil)
	diskTypeUC := disktype.New(nil)
	opHandler := operationspb.NewHandler(operations.NewRepo(nil, "kacho_storage"))

	names := func(reg func(grpc.ServiceRegistrar)) []string {
		srv := grpc.NewServer()
		reg(srv)
		var out []string
		for name := range srv.GetServiceInfo() {
			out = append(out, name)
		}
		sort.Strings(out)
		return out
	}

	public := names(func(r grpc.ServiceRegistrar) {
		registerPublic(r, volumeUC, snapshotUC, imageUC, diskTypeUC, handler.NewQuotaHandler(nil), opHandler)
	})
	internal := names(func(r grpc.ServiceRegistrar) {
		registerInternal(r, volumeUC, imageUC, diskTypeUC,
			storagebackend.New(nil), disktypebinding.New(nil, nil), opHandler,
			probeSubscriptionServer(t))
	})

	if len(public) == 0 || len(internal) == 0 {
		t.Fatalf("пустой набор делает утверждение вакуумным: публичных служб %d, внутренних %d",
			len(public), len(internal))
	}
	for _, n := range public {
		if strings.Contains(n, ".Internal") {
			t.Errorf("служба %q зарегистрирована на ПУБЛИЧНОМ слушателе: `Internal*` "+
				"не публикуется на внешнем endpoint (ban #6)", n)
		}
	}
	var internalFound bool
	for _, n := range internal {
		if strings.Contains(n, ".Internal") {
			internalFound = true
			break
		}
	}
	if !internalFound {
		t.Errorf("во внутреннем наборе нет ни одной службы `Internal*` (%v) — положительный "+
			"контроль не выполнен, и отрицание выше зеленело бы на пустоте", internal)
	}
	t.Logf("перепись: публичных служб %d, внутренних %d", len(public), len(internal))
}
