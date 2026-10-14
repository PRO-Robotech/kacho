// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package handler_test

// public_surface_test.go — гейт «публичная поверхность geo не несёт infra°»
// (приёмка ADM-1 geo, сценарий 13 и §Р3; security.md two-projection).
//
// Судится КОНТРАКТ, а не ответ одного вызова: обходятся дескрипторы публичных
// RegionService/ZoneService — вход каждого метода и всё, что он выдаёт наружу
// (выход метода и тип, объявленный ответом операции). На входе запрещён блок
// infra°; на выходе — и infra°, и сырой статус (`status` — только во внутренней
// проекции). Обход транзитивен по полям-сообщениям: вложенное infra° — та же
// утечка, что и поле верхнего уровня.
//
// Способность упасть доказана настоящим входом из дерева: те же правила на
// внутреннем близнеце (InternalZoneService) находят infra° по имени метода.

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	apiv1 "github.com/PRO-Robotech/corelib/api/corelib/api/v1"
	geov1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/geo/v1"
)

var infraTypes = map[protoreflect.FullName]bool{
	"kacho.cloud.geo.v1.RegionInfra": true,
	"kacho.cloud.geo.v1.ZoneInfra":   true,
}

// leaks обходит сообщение и возвращает пути полей, запрещённых на этой стороне.
func leaks(md protoreflect.MessageDescriptor, path string, forbidStatus bool, seen map[protoreflect.FullName]bool, out *[]string) {
	if seen[md.FullName()] {
		return
	}
	seen[md.FullName()] = true
	defer delete(seen, md.FullName())
	fields := md.Fields()
	for i := 0; i < fields.Len(); i++ {
		f := fields.Get(i)
		p := path + "." + string(f.Name())
		if f.Name() == "infra" {
			*out = append(*out, p)
			continue
		}
		if f.Message() != nil && infraTypes[f.Message().FullName()] {
			*out = append(*out, p)
			continue
		}
		if forbidStatus && f.Enum() != nil && f.Enum().FullName() == "kacho.cloud.geo.v1.GeoStatus" {
			*out = append(*out, p)
			continue
		}
		if f.Message() != nil && !strings.HasPrefix(string(f.Message().FullName()), "google.protobuf.") {
			leaks(f.Message(), p, forbidStatus, seen, out)
		}
	}
}

func operationResponse(t *testing.T, m protoreflect.MethodDescriptor) protoreflect.MessageDescriptor {
	t.Helper()
	op, ok := proto.GetExtension(m.Options(), apiv1.E_Operation).(*apiv1.Operation)
	if !ok || op == nil || op.GetResponse() == "" {
		return nil
	}
	name := op.GetResponse()
	if !strings.Contains(name, ".") {
		name = string(m.ParentFile().Package()) + "." + name
	}
	d, err := protoregistry.GlobalFiles.FindDescriptorByName(protoreflect.FullName(name))
	if err != nil {
		t.Fatalf("%s: operation response %q does not resolve: %v", m.FullName(), name, err)
	}
	return d.(protoreflect.MessageDescriptor)
}

// surfaceLeaks — все находки по службе: «метод: сторона путь».
func surfaceLeaks(t *testing.T, sd protoreflect.ServiceDescriptor) (findings []string, methods int) {
	t.Helper()
	ms := sd.Methods()
	for i := 0; i < ms.Len(); i++ {
		m := ms.Get(i)
		methods++
		var in, outs []string
		leaks(m.Input(), "in", false, map[protoreflect.FullName]bool{}, &in)
		leaks(m.Output(), "out", true, map[protoreflect.FullName]bool{}, &outs)
		if r := operationResponse(t, m); r != nil {
			leaks(r, "response", true, map[protoreflect.FullName]bool{}, &outs)
		}
		for _, l := range append(in, outs...) {
			findings = append(findings, fmt.Sprintf("%s: %s", m.FullName(), l))
		}
	}
	sort.Strings(findings)
	return findings, methods
}

// TestPublicGeoSurface_ADM1GEO13_CarriesNoInfraAndNoRawStatus — сценарий 13 на
// уровне контракта.
func TestPublicGeoSurface_ADM1GEO13_CarriesNoInfraAndNoRawStatus(t *testing.T) {
	total := 0
	for _, sd := range []protoreflect.ServiceDescriptor{
		geov1.File_kacho_cloud_geo_v1_region_service_proto.Services().ByName("RegionService"),
		geov1.File_kacho_cloud_geo_v1_zone_service_proto.Services().ByName("ZoneService"),
	} {
		findings, n := surfaceLeaks(t, sd)
		if n < 5 {
			t.Fatalf("%s: осмотрено методов %d — ожидались чтение и три мутации; обход не дошёл до предмета", sd.FullName(), n)
		}
		total += n
		if len(findings) != 0 {
			t.Errorf("публичная служба %s выводит инфраструктуру или сырой статус:\n  %s",
				sd.FullName(), strings.Join(findings, "\n  "))
		}
	}
	t.Logf("осмотрено методов публичных служб geo: %d, находок: 0", total)
}

// TestPublicGeoSurfaceGateFindsTheInternalTwin — тот же обход на внутреннем
// близнеце обязан найти infra° (и на входе Create, и на выходе GetInternal).
func TestPublicGeoSurfaceGateFindsTheInternalTwin(t *testing.T) {
	sd := geov1.File_kacho_cloud_geo_v1_internal_catalog_service_proto.Services().ByName("InternalZoneService")
	findings, _ := surfaceLeaks(t, sd)
	joined := strings.Join(findings, "\n")
	for _, want := range []string{
		"kacho.cloud.geo.v1.InternalZoneService.Create: in.infra",
		"kacho.cloud.geo.v1.InternalZoneService.GetInternal: out.infra",
		"kacho.cloud.geo.v1.InternalZoneService.GetInternal: out.status",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("обход не нашёл %q на внутреннем близнеце — гейт выше вакуумен; находки:\n%s", want, joined)
		}
	}
}
