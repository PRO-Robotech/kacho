// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package restmux

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	"github.com/PRO-Robotech/kacho/gateway/internal/listenerorigin"
)

// catalogMuxAddrs — backend-адреса всех доменов, чьи Internal*-сервисы несут
// REST-биндинги. Без адреса Internal*-хендлер не регистрируется вовсе, и тест
// проверял бы отсутствие маршрута, а не изоляцию.
func catalogMuxAddrs() map[string]string {
	return map[string]string{
		"vpc":                  "127.0.0.1:1",
		"vpcInternal":          "127.0.0.1:1",
		"compute":              "127.0.0.1:1",
		"computeInternal":      "127.0.0.1:1",
		"iam":                  "127.0.0.1:1",
		"iamInternal":          "127.0.0.1:1",
		"loadbalancer":         "127.0.0.1:1",
		"loadbalancerInternal": "127.0.0.1:1",
		"geo":                  "127.0.0.1:1",
		"geoInternal":          "127.0.0.1:1",
		"registry":             "127.0.0.1:1",
		"registryInternal":     "127.0.0.1:1",
		"storage":              "127.0.0.1:1",
		"storageInternal":      "127.0.0.1:1",
	}
}

// diskTypeAdminRoutes — admin-CRUD справочника DiskType (kacho-storage — владелец;
// дубль compute снят вместе с блочным хранением).
// Маршруты висят на ТОМ ЖЕ пути, что публичное чтение каталога, и отличаются
// ТОЛЬКО HTTP-методом: GET — публичный DiskTypeService, POST/PATCH/DELETE —
// InternalDiskTypeService (:9091, system_admin). Классификация по одной лишь
// строке пути их различить не может — решение обязано учитывать метод.
var diskTypeAdminRoutes = []struct{ method, path string }{
	{"POST", "/storage/v1/diskTypes"},
	{"PATCH", "/storage/v1/diskTypes/network-ssd"},
	{"DELETE", "/storage/v1/diskTypes/network-ssd"},
}

// TestExternalListener_RejectsDiskTypeAdminRoutes — admin-мутации каталога
// дисков на ВНЕШНЕМ листенере обязаны вернуть 404 (existence-hiding) и НЕ дойти
// до обработчика: Internal*-методы не публикуются на external endpoint (ban #6).
func TestExternalListener_RejectsDiskTypeAdminRoutes(t *testing.T) {
	h, err := NewMux(context.Background(), catalogMuxAddrs(), nil, nil)
	if err != nil {
		t.Fatalf("NewMux: %v", err)
	}
	for _, tc := range diskTypeAdminRoutes {

		t.Run("EXT "+tc.method+" "+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader("{}"))
			req.Header.Set("Content-Type", "application/json")
			// External — fail-closed default (нет internal-origin маркера).
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusNotFound {
				t.Errorf("DiskType admin %s %s on EXTERNAL listener: got %d, want 404 (CRITICAL: admin surface exposed on external endpoint)",
					tc.method, tc.path, rec.Code)
			}
		})
	}
}

// TestInternalListener_ServesDiskTypeAdminRoutes — те же маршруты на
// ВНУТРЕННЕМ листенере обязаны остаться достижимыми (маршрут найден — не 404):
// иначе admin-tooling / UI / port-forward сломаны.
func TestInternalListener_ServesDiskTypeAdminRoutes(t *testing.T) {
	h, err := NewMux(context.Background(), catalogMuxAddrs(), nil, nil)
	if err != nil {
		t.Fatalf("NewMux: %v", err)
	}
	for _, tc := range diskTypeAdminRoutes {

		t.Run("INT "+tc.method+" "+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader("{}"))
			req.Header.Set("Content-Type", "application/json")
			req = req.WithContext(listenerorigin.WithInternal(req.Context()))
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code == http.StatusNotFound {
				t.Errorf("DiskType admin %s %s on INTERNAL listener: got 404 — route rejected, admin-tooling broken",
					tc.method, tc.path)
			}
		})
	}
}

// TestExternalListener_DiskTypePublicReadsStillServed — фиксирует, что правка
// СУЖАЕТ доступ, а не расширяет: публичное ЧТЕНИЕ каталога (GET по тем же
// путям) на внешнем листенере обязано продолжать обслуживаться. Если бы
// классификация ушла на путь целиком, GET уехал бы на internal-mux и внешние
// клиенты потеряли бы публичный каталог.
func TestExternalListener_DiskTypePublicReadsStillServed(t *testing.T) {
	h, err := NewMux(context.Background(), catalogMuxAddrs(), nil, nil)
	if err != nil {
		t.Fatalf("NewMux: %v", err)
	}
	publicReads := []struct{ method, path string }{
		{"GET", "/storage/v1/diskTypes"},
		{"GET", "/storage/v1/diskTypes/network-ssd"},
	}
	for _, tc := range publicReads {

		t.Run("EXT "+tc.method+" "+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code == http.StatusNotFound {
				t.Errorf("public catalog read %s %s on EXTERNAL listener: got 404 — public route wrongly rejected (change must narrow, not widen)",
					tc.method, tc.path)
			}
		})
	}
}

// TestAllInternalRESTBindings_ClassifiedInternal — дрейф-гейт на ВСЕ домены.
// Перечисляет по слинкованным в бинарь proto-дескрипторам каждый REST-биндинг
// каждого Internal*-сервиса и требует, чтобы маршрутизатор классифицировал его
// как внутренний. Любой будущий Internal-RPC с REST-аннотацией, чей путь не
// несёт сегмента `/internal`, ловится здесь, а не ревью дизайна.
func TestAllInternalRESTBindings_ClassifiedInternal(t *testing.T) {
	bindings := internalServiceRESTBindings(t)
	if len(bindings) == 0 {
		t.Fatal("no Internal*Service REST bindings discovered — descriptor walk broken, gate would be vacuous")
	}
	t.Logf("discovered %d Internal*Service REST bindings", len(bindings))
	for _, b := range bindings {

		t.Run(b.service+" "+b.method+" "+b.path, func(t *testing.T) {
			concrete := concreteRESTPath(b.path)
			if !isInternalRoute(b.method, concrete) {
				t.Errorf("Internal binding %s.%s (%s %s) classified PUBLIC by the router (concrete path %q) — admin surface reachable on the external listener",
					b.service, b.rpc, b.method, b.path, concrete)
			}
		})
	}
}

// --- descriptor walk helpers ---

type restBinding struct {
	service string
	rpc     string
	method  string
	path    string
}

// internalServiceRESTBindings собирает REST-биндинги (включая
// additional_bindings) всех сервисов, чьё имя следует одной из двух принятых в
// kacho-proto конвенций: префикс `Internal<X>Service` либо суффикс
// `<X>InternalService`.
func internalServiceRESTBindings(t *testing.T) []restBinding {
	t.Helper()
	var out []restBinding
	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		if !strings.HasPrefix(string(fd.Package()), "kacho.") {
			return true
		}
		svcs := fd.Services()
		for i := 0; i < svcs.Len(); i++ {
			svc := svcs.Get(i)
			name := string(svc.Name())
			isInternal := strings.HasSuffix(name, "InternalService") ||
				(strings.HasPrefix(name, "Internal") && strings.HasSuffix(name, "Service"))
			if !isInternal {
				continue
			}
			mts := svc.Methods()
			for j := 0; j < mts.Len(); j++ {
				m := mts.Get(j)
				rule, _ := proto.GetExtension(m.Options(), annotations.E_Http).(*annotations.HttpRule)
				if rule == nil {
					continue
				}
				for _, r := range append([]*annotations.HttpRule{rule}, rule.GetAdditionalBindings()...) {
					verb, path := httpRuleVerbPath(r)
					if path == "" {
						continue
					}
					out = append(out, restBinding{
						service: string(svc.FullName()),
						rpc:     string(m.Name()),
						method:  verb,
						path:    path,
					})
				}
			}
		}
		return true
	})
	return out
}

func httpRuleVerbPath(r *annotations.HttpRule) (string, string) {
	switch p := r.GetPattern().(type) {
	case *annotations.HttpRule_Get:
		return "GET", p.Get
	case *annotations.HttpRule_Post:
		return "POST", p.Post
	case *annotations.HttpRule_Put:
		return "PUT", p.Put
	case *annotations.HttpRule_Patch:
		return "PATCH", p.Patch
	case *annotations.HttpRule_Delete:
		return "DELETE", p.Delete
	case *annotations.HttpRule_Custom:
		return strings.ToUpper(p.Custom.GetKind()), p.Custom.GetPath()
	}
	return "", ""
}

// concreteRESTPath подставляет в шаблон конкретное значение вместо каждого
// `{var}` — маршрутизатор видит в рантайме именно такой путь.
func concreteRESTPath(tmpl string) string {
	segs := strings.Split(tmpl, "/")
	for i, s := range segs {
		open := strings.IndexByte(s, '{')
		if open < 0 {
			continue
		}
		closeIdx := strings.IndexByte(s, '}')
		if closeIdx < open {
			continue
		}
		segs[i] = s[:open] + "sample-id" + s[closeIdx+1:]
	}
	return strings.Join(segs, "/")
}
