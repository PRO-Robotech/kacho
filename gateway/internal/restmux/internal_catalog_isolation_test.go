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
// дисков на ВНЕШНЕМ листенере не доходят до обработчика (ban #6) и неотличимы
// от запроса, называющего то, чего этот листенер не обслуживает.
//
// ПОЧЕМУ НЕ «== 404», КАК БЫЛО. Эти три маршрута висят на ТОМ ЖЕ пути, что
// публичное чтение каталога, и отличаются только методом. Значит запрос
// `PUT /storage/v1/diskTypes` — метод, которого не обслуживает НИКТО, — получает
// от grpc-gateway 501 Method Not Allowed. Пришпиленный здесь 404 на POST/PATCH/
// DELETE был не «сокрытием», а РАЗЛИЧИЕМ: по коду ответа внешний вызывающий
// отличал «этот метод здесь административный» от «этого метода нет вовсе», то
// есть перечислял админ-поверхность без единого удостоверения. Утверждение
// заменено на то, ради чего 404 писался: ответ производит тот же производитель и
// СОВПАДАЕТ с ответом на неподдерживаемый метод того же пути.
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
			got := answerShape{
				code:   rec.Code,
				ctype:  rec.Header().Get("Content-Type"),
				body:   rec.Body.String(),
				nosnif: rec.Header().Get("X-Content-Type-Options"),
			}
			want := askExternal(t, h, unservedMethod, tc.path)
			if got != want {
				t.Errorf("DiskType admin %s %s on EXTERNAL listener отвечает НЕ ТАК, как %s по тому же "+
					"пути — метод, которого не обслуживает никто. Различие в ответе перечисляет "+
					"админ-поверхность (CRITICAL: existence-oracle).\n  админ-метод : %s\n  которого нет: %s",
					tc.method, tc.path, unservedMethod, got, want)
			}
		})
	}
}

// TestExternalListener_DiskTypeAdminNeverReachesTheAdminBackend — то же
// свойство, но проверенное по АДРЕСУ, а не по форме ответа: административный
// бэкенд разведён с публичным на отдельный мёртвый литерал, и тело ответа
// grpc-gateway цитирует тот, до которого он дозванивался.
//
// Нужно отдельно от сверки формы: совпадение формы доказывает, что вызывающий не
// РАЗЛИЧАЕТ, а этот тест — что запрос вообще не ДОШЁЛ.
func TestExternalListener_DiskTypeAdminNeverReachesTheAdminBackend(t *testing.T) {
	addrs, adminLiteral := splitAddrs(t)
	h, err := NewMux(context.Background(), addrs, nil, nil)
	if err != nil {
		t.Fatalf("NewMux: %v", err)
	}
	// Премиса: на ВНУТРЕННЕМ листенере литерал появляется — иначе дискриминатор
	// не работает и «ноль снаружи» получено даром.
	seen := 0
	for _, tc := range diskTypeAdminRoutes {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(listenerorigin.WithInternal(req.Context()))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if strings.Contains(rec.Body.String(), adminLiteral) {
			seen++
		}
	}
	if seen != len(diskTypeAdminRoutes) {
		t.Fatalf("административный литерал %q виден на внутреннем листенере лишь у %d из %d "+
			"маршрутов — дискриминатор ненадёжен", adminLiteral, seen, len(diskTypeAdminRoutes))
	}
	for _, tc := range diskTypeAdminRoutes {

		t.Run("EXT "+tc.method+" "+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader("{}"))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if strings.Contains(rec.Body.String(), adminLiteral) {
				t.Errorf("DiskType admin %s %s ДОШЁЛ до административного бэкенда с ВНЕШНЕГО "+
					"листенера (CRITICAL, ban #6): %s", tc.method, tc.path, rec.Body.String())
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
		if !underDeclaredRoot(string(fd.Package())) {
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
