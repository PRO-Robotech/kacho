// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dsschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/PRO-Robotech/kacho/terraform/internal/client"
)

var dsAll = map[string]func() datasource.DataSource{
	"kacho_geo_region":            NewGeoRegionDataSource,
	"kacho_geo_regions":           NewGeoRegionsDataSource,
	"kacho_geo_zone":              NewGeoZoneDataSource,
	"kacho_geo_zones":             NewGeoZonesDataSource,
	"kacho_storage_disk_type":     NewStorageDiskTypeDataSource,
	"kacho_storage_disk_types":    NewStorageDiskTypesDataSource,
	"kacho_compute_machine_type":  NewComputeMachineTypeDataSource,
	"kacho_compute_machine_types": NewComputeMachineTypesDataSource,
	"kacho_vpc_network_by_name":   NewVPCNetworkByNameDataSource,
	"kacho_vpc_subnet_by_name":    NewVPCSubnetByNameDataSource,
}

func dsSchemaOf(t *testing.T, ds datasource.DataSource) dsschema.Schema {
	t.Helper()
	var sr datasource.SchemaResponse
	ds.Schema(context.Background(), datasource.SchemaRequest{}, &sr)
	if sr.Diagnostics.HasError() {
		t.Fatalf("schema diags: %v", sr.Diagnostics.Errors())
	}
	return sr.Schema
}

func TestDataSourceSchemasValid(t *testing.T) {
	ctx := context.Background()
	for want, ctor := range dsAll {
		ds := ctor()
		var mr datasource.MetadataResponse
		ds.Metadata(ctx, datasource.MetadataRequest{ProviderTypeName: "kacho"}, &mr)
		if mr.TypeName != want {
			t.Errorf("type name = %q, want %q", mr.TypeName, want)
		}
		s := dsSchemaOf(t, ds)
		if d := s.ValidateImplementation(ctx); d.HasError() {
			t.Errorf("%s: ValidateImplementation: %v", want, d.Errors())
		}
		if d := s.Validate(); d.HasError() { //nolint:staticcheck // проверяем обе стороны
			t.Errorf("%s: Validate: %v", want, d.Errors())
		}
		// Обязательное+вычисляемое фреймворк НЕ ловит (измерено инъекцией), поэтому
		// проверяется здесь явно.
		for n, a := range s.GetAttributes() {
			if a.IsRequired() && a.IsComputed() {
				t.Errorf("%s.%s: обязательное поле объявлено вычисляемым", want, n)
			}
			if !a.IsRequired() && !a.IsOptional() && !a.IsComputed() {
				t.Errorf("%s.%s: поле не объявлено ни одним из трёх", want, n)
			}
		}
	}
}

func dsConfigOf(ctx context.Context, s dsschema.Schema, set map[string]tftypes.Value) tftypes.Value {
	objType := s.Type().TerraformType(ctx).(tftypes.Object)
	vals := map[string]tftypes.Value{}
	for name, tp := range objType.AttributeTypes {
		if v, ok := set[name]; ok {
			vals[name] = v
			continue
		}
		vals[name] = tftypes.NewValue(tp, nil)
	}
	return tftypes.NewValue(objType, vals)
}

func dsReadOnce(t *testing.T, ds datasource.DataSource, srv *httptest.Server, set map[string]tftypes.Value) (tfsdk.State, []string) {
	t.Helper()
	ctx := context.Background()
	c, err := client.New(client.Config{Endpoint: srv.URL, Token: "t"})
	if err != nil {
		t.Fatal(err)
	}
	var cr datasource.ConfigureResponse
	ds.(datasource.DataSourceWithConfigure).Configure(ctx,
		datasource.ConfigureRequest{ProviderData: c}, &cr)
	if cr.Diagnostics.HasError() {
		t.Fatalf("configure: %v", cr.Diagnostics.Errors())
	}
	s := dsSchemaOf(t, ds)
	raw := dsConfigOf(ctx, s, set)
	req := datasource.ReadRequest{Config: tfsdk.Config{Schema: s, Raw: raw}}
	resp := datasource.ReadResponse{State: tfsdk.State{Schema: s, Raw: raw.Copy()}}
	ds.Read(ctx, req, &resp)
	msgs := []string{}
	for _, d := range resp.Diagnostics.Errors() {
		msgs = append(msgs, d.Summary()+" | "+d.Detail())
	}
	return resp.State, msgs
}

func dsStr(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }
func dsBoolV(b bool) tftypes.Value { return tftypes.NewValue(tftypes.Bool, b) }
func dsNum(n int64) tftypes.Value  { return tftypes.NewValue(tftypes.Number, n) }

// ---- фальшивый край ---------------------------------------------------------------------

func dsFakeEdge(t *testing.T, routes map[string]string) *httptest.Server {
	t.Helper()
	seen := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Path
		if q := r.URL.RawQuery; q != "" {
			key += "?" + q
		}
		seen[key]++
		body, ok := routes[key]
		if !ok {
			t.Logf("НЕОЖИДАННЫЙ запрос: %s", key)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"code":5,"message":"not found"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(body, "404:") {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(strings.TrimPrefix(body, "404:")))
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestDataSourceGeoZoneOne(t *testing.T) {
	srv := dsFakeEdge(t, map[string]string{
		"/geo/v1/zones/ru-central1-a": `{"id":"ru-central1-a","regionId":"ru-central1",
			"name":"Зона A","createdAt":"2026-01-01T00:00:00Z","openForPlacement":true,
			"placementBlockedReason":"NONE"}`,
	})
	st, errs := dsReadOnce(t, NewGeoZoneDataSource(), srv, map[string]tftypes.Value{
		"id": dsStr("ru-central1-a"),
	})
	if len(errs) != 0 {
		t.Fatalf("errors: %v", errs)
	}
	var region, reason types.String
	var open types.Bool
	st.GetAttribute(context.Background(), path.Root("region_id"), &region)
	st.GetAttribute(context.Background(), path.Root("placement_blocked_reason"), &reason)
	st.GetAttribute(context.Background(), path.Root("open_for_placement"), &open)
	if region.ValueString() != "ru-central1" || reason.ValueString() != "NONE" || !open.ValueBool() {
		t.Fatalf("region=%v reason=%v open=%v", region, reason, open)
	}
}

func TestDataSourceGeoZoneAbsenceNamesCatalog(t *testing.T) {
	srv := dsFakeEdge(t, map[string]string{
		"/geo/v1/zones/ru-central1-z": `404:{"code":5,"message":"Zone ru-central1-z not found"}`,
		"/geo/v1/zones?pageSize=500":  `{"zones":[{"id":"ru-central1-a"},{"id":"ru-central1-b"}]}`,
	})
	_, errs := dsReadOnce(t, NewGeoZoneDataSource(), srv, map[string]tftypes.Value{
		"id": dsStr("ru-central1-z"),
	})
	if len(errs) != 1 {
		t.Fatalf("errors: %v", errs)
	}
	if !strings.Contains(errs[0], "ru-central1-a, ru-central1-b") {
		t.Fatalf("отказ не назвал существующие зоны: %s", errs[0])
	}
}

func TestDataSourceGeoZoneAbsenceUnconfirmed(t *testing.T) {
	srv := dsFakeEdge(t, map[string]string{
		"/geo/v1/zones/ru-central1-z": `404:{"code":5,"message":"Zone not found"}`,
		"/geo/v1/zones?pageSize=500":  `{}`,
	})
	_, errs := dsReadOnce(t, NewGeoZoneDataSource(), srv, map[string]tftypes.Value{
		"id": dsStr("ru-central1-z"),
	})
	if len(errs) != 1 || !strings.Contains(errs[0], "Отсутствие не подтверждено") {
		t.Fatalf("errors: %v", errs)
	}
}

func TestDataSourceGeoZonesFilterAndPaging(t *testing.T) {
	srv := dsFakeEdge(t, map[string]string{
		"/geo/v1/zones?pageSize=500&regionId=ru-central1":              `{"zones":[{"id":"ru-central1-a","regionId":"ru-central1"}],"nextPageToken":"p2"}`,
		"/geo/v1/zones?pageSize=500&pageToken=p2&regionId=ru-central1": `{"zones":[{"id":"ru-central1-b","regionId":"ru-central1"}]}`,
	})
	st, errs := dsReadOnce(t, NewGeoZonesDataSource(), srv, map[string]tftypes.Value{
		"region_id": dsStr("ru-central1"),
	})
	if len(errs) != 0 {
		t.Fatalf("errors: %v", errs)
	}
	var list types.List
	st.GetAttribute(context.Background(), path.Root("zones"), &list)
	if len(list.Elements()) != 2 {
		t.Fatalf("обход курсора собрал %d записей, ждали 2", len(list.Elements()))
	}
}

func TestDataSourceFilterFalseRefused(t *testing.T) {
	srv := dsFakeEdge(t, map[string]string{})
	_, errs := dsReadOnce(t, NewGeoZonesDataSource(), srv, map[string]tftypes.Value{
		"open_for_placement": dsBoolV(false),
	})
	if len(errs) != 1 || !strings.Contains(errs[0], "умеет только сужать") {
		t.Fatalf("errors: %v", errs)
	}
	// и то же на этапе проверки настройки
	ds := NewGeoZonesDataSource()
	s := dsSchemaOf(t, ds)
	raw := dsConfigOf(context.Background(), s, map[string]tftypes.Value{
		"open_for_placement": dsBoolV(false),
	})
	var vr datasource.ValidateConfigResponse
	ds.(datasource.DataSourceWithValidateConfig).ValidateConfig(context.Background(),
		datasource.ValidateConfigRequest{Config: tfsdk.Config{Schema: s, Raw: raw}}, &vr)
	if !vr.Diagnostics.HasError() {
		t.Fatal("ValidateConfig промолчал на несужающем фильтре")
	}
	// положительный контроль: true проходит обе стороны
	var vr2 datasource.ValidateConfigResponse
	raw2 := dsConfigOf(context.Background(), s, map[string]tftypes.Value{
		"open_for_placement": dsBoolV(true),
	})
	ds.(datasource.DataSourceWithValidateConfig).ValidateConfig(context.Background(),
		datasource.ValidateConfigRequest{Config: tfsdk.Config{Schema: s, Raw: raw2}}, &vr2)
	if vr2.Diagnostics.HasError() {
		t.Fatalf("законный фильтр отвергнут: %v", vr2.Diagnostics.Errors())
	}
}

func TestDataSourceMachineTypesNestedAndInt64AsString(t *testing.T) {
	srv := dsFakeEdge(t, map[string]string{
		"/compute/v1/machineTypes?minGpus=4&pageSize=500": `{"machineTypes":[{"id":"mt-abc","name":"gpu-a100-4",
			"family":"GPU","status":"AVAILABLE","labels":{"k":"v"},"availableZones":["ru-central1-a"],
			"createdAt":"2026-01-01T00:00:00Z",
			"effectiveResources":{"vCpu":32,"memoryMib":"262144","gpus":4,"gpuType":"a100-80g"}}]}`,
	})
	st, errs := dsReadOnce(t, NewComputeMachineTypesDataSource(), srv, map[string]tftypes.Value{
		"min_gpus": dsNum(4),
	})
	if len(errs) != 0 {
		t.Fatalf("errors: %v", errs)
	}
	var list types.List
	st.GetAttribute(context.Background(), path.Root("machine_types"), &list)
	if len(list.Elements()) != 1 {
		t.Fatalf("записей %d", len(list.Elements()))
	}
	obj := list.Elements()[0].(types.Object)
	eff := obj.Attributes()["effective_resources"].(types.Object)
	if got := eff.Attributes()["memory_mib"].(types.Int64).ValueInt64(); got != 262144 {
		t.Fatalf("memory_mib=%d — 64-битное число приезжает строкой и должно разбираться", got)
	}
	if got := eff.Attributes()["v_cpu"].(types.Int64).ValueInt64(); got != 32 {
		t.Fatalf("v_cpu=%d", got)
	}
}

func TestDataSourceMachineTypeNoEffectiveResourcesIsNull(t *testing.T) {
	srv := dsFakeEdge(t, map[string]string{
		"/compute/v1/machineTypes/mt-abc": `{"id":"mt-abc","name":"std-v3-2"}`,
	})
	st, errs := dsReadOnce(t, NewComputeMachineTypeDataSource(), srv, map[string]tftypes.Value{
		"id": dsStr("mt-abc"),
	})
	if len(errs) != 0 {
		t.Fatalf("errors: %v", errs)
	}
	var eff types.Object
	st.GetAttribute(context.Background(), path.Root("effective_resources"), &eff)
	if !eff.IsNull() {
		t.Fatalf("объект без данных обязан быть null, получено %v", eff)
	}
}

func TestDataSourceNetworkByNameFound(t *testing.T) {
	srv := dsFakeEdge(t, map[string]string{
		`/vpc/v1/networks?filter=name%3D%22prod%22&pageSize=500&projectId=prj-1`: `{"networks":[
			{"id":"net123","projectId":"prj-1","name":"prod","description":"d",
			 "labels":{"env":"prod"},"createdAt":"2026-01-01T00:00:00Z",
			 "defaultSecurityGroupId":"sg1","defaultRouteTableId":"rt1",
			 "ipv4CidrBlocks":["10.0.0.0/8"]}]}`,
	})
	st, errs := dsReadOnce(t, NewVPCNetworkByNameDataSource(), srv, map[string]tftypes.Value{
		"project_id": dsStr("prj-1"),
		"name":       dsStr("prod"),
	})
	if len(errs) != 0 {
		t.Fatalf("errors: %v", errs)
	}
	var id types.String
	st.GetAttribute(context.Background(), path.Root("id"), &id)
	if id.ValueString() != "net123" {
		t.Fatalf("id=%v", id)
	}
	var blocks types.List
	st.GetAttribute(context.Background(), path.Root("ipv4_cidr_blocks"), &blocks)
	if len(blocks.Elements()) != 1 {
		t.Fatalf("blocks=%v", blocks)
	}
	// вход не переписан эхом края — он остался тем, что задал пользователь
	var name types.String
	st.GetAttribute(context.Background(), path.Root("name"), &name)
	if name.ValueString() != "prod" {
		t.Fatalf("name=%v", name)
	}
}

// Фильтр края перестал сужать: страница отдаёт ЧУЖУЮ строку. Провайдер обязан её не принять.
func TestDataSourceNetworkByNameRejectsWrongName(t *testing.T) {
	srv := dsFakeEdge(t, map[string]string{
		`/vpc/v1/networks?filter=name%3D%22prod%22&pageSize=500&projectId=prj-1`: `{"networks":[
			{"id":"netZZZ","projectId":"prj-1","name":"staging"}]}`,
		// ConfirmAbsence: по имени пусто, контрольная страница непуста ⇒ Gone
		`/vpc/v1/networks?filter=name%3D%22prod%22&pageSize=100&projectId=prj-1`: `{}`,
		`/vpc/v1/networks?pageSize=100&projectId=prj-1`:                          `{"networks":[{"id":"netZZZ"}]}`,
	})
	_, errs := dsReadOnce(t, NewVPCNetworkByNameDataSource(), srv, map[string]tftypes.Value{
		"project_id": dsStr("prj-1"),
		"name":       dsStr("prod"),
	})
	if len(errs) != 1 || !strings.Contains(errs[0], "не найдена") {
		t.Fatalf("errors: %v", errs)
	}
}

func TestDataSourceSubnetByNameAbsenceDenied(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.RawQuery, "pageSize=500") {
			_, _ = w.Write([]byte(`{}`))
			return
		}
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"code":7,"message":"permission denied"}`))
	}))
	defer srv.Close()
	_, errs := dsReadOnce(t, NewVPCSubnetByNameDataSource(), srv, map[string]tftypes.Value{
		"project_id": dsStr("prj-1"),
		"name":       dsStr("app"),
	})
	if len(errs) != 1 || !strings.Contains(errs[0], "событие ПРАВ") {
		t.Fatalf("errors: %v", errs)
	}
}

func TestDataSourceDiskTypesEmptyZoneIdsIsEmptyList(t *testing.T) {
	srv := dsFakeEdge(t, map[string]string{
		"/storage/v1/diskTypes/network-ssd": `{"id":"network-ssd","name":"SSD","performanceTier":"high"}`,
	})
	st, errs := dsReadOnce(t, NewStorageDiskTypeDataSource(), srv, map[string]tftypes.Value{
		"id": dsStr("network-ssd"),
	})
	if len(errs) != 0 {
		t.Fatalf("errors: %v", errs)
	}
	var zones types.List
	st.GetAttribute(context.Background(), path.Root("zone_ids"), &zones)
	if zones.IsNull() || len(zones.Elements()) != 0 {
		t.Fatalf("zone_ids=%v — ждали пустой список, не null", zones)
	}
}

func TestDataSourceRegionsNoFilter(t *testing.T) {
	srv := dsFakeEdge(t, map[string]string{
		"/geo/v1/regions?pageSize=500": `{"regions":[{"id":"ru-central1","name":"Центр",
			"countryCode":"RU","openForPlacement":true,"openZoneCountHint":"3"}]}`,
	})
	st, errs := dsReadOnce(t, NewGeoRegionsDataSource(), srv, nil)
	if len(errs) != 0 {
		t.Fatalf("errors: %v", errs)
	}
	var list types.List
	st.GetAttribute(context.Background(), path.Root("regions"), &list)
	obj := list.Elements()[0].(types.Object)
	if got := obj.Attributes()["open_zone_count_hint"].(types.Int64).ValueInt64(); got != 3 {
		t.Fatalf("hint=%d", got)
	}
}

// Край вернул объект с другим идентификатором — выдавать его за запрошенный нельзя.
func TestDataSourceEchoMismatchRefused(t *testing.T) {
	srv := dsFakeEdge(t, map[string]string{
		"/geo/v1/regions/ru-central1": `{"id":"ru-central2","name":"Другой"}`,
	})
	_, errs := dsReadOnce(t, NewGeoRegionDataSource(), srv, map[string]tftypes.Value{
		"id": dsStr("ru-central1"),
	})
	if len(errs) != 1 || !strings.Contains(errs[0], "не тот объект") {
		t.Fatalf("errors: %v", errs)
	}
}
