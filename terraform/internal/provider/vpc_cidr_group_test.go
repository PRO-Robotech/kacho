// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"
)

// ---- вспомогательное -------------------------------------------------------------------

// cidrGroupSchemaAttrs — схема, которую УВИДИТ пользователь.
//
// Утверждать надо про схему, а не про внутренние списки полей: пользователю адресована
// именно она, и разойдись она с телом запроса — расхождение увидел бы он, а не прогон.
func cidrGroupSchemaAttrs(t *testing.T) map[string]schema.Attribute {
	t.Helper()
	var resp resource.SchemaResponse
	NewVPCCidrGroupResource().Schema(context.Background(), resource.SchemaRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("схема набора префиксов не собралась: %v", resp.Diagnostics.Errors())
	}
	if len(resp.Schema.Attributes) == 0 {
		t.Fatal("схема пуста — проба зеленела бы на любом утверждении о её содержимом")
	}
	return resp.Schema.Attributes
}

// Поля, которые край ПРИНИМАЕТ на создании набора.
var cidrGroupAcceptedByCreate = []string{
	"project_id", "name", "description", "labels", "v4_cidr_blocks", "v6_cidr_blocks",
}

// Поля ответа края, которые ВЫВОДЯТСЯ им и на вход не принимаются ни одним запросом.
var cidrGroupComputedOnly = []string{"id", "created_at", "cidr_block_count", "used_by"}

// ---- схема не предлагает того, чего край не примет ---------------------------------------

// Схема ресурса и контракты края описывают ОДИН предмет — и это утверждается механически.
//
// Класс, который проба закрывает, в этой ветке уже случался: блок провайдера
// компилировался, а край поле отвергал. Компилятор такого не ловит by construction — имя
// атрибута схемы живёт строкой, а не полем структуры.
func TestCidrGroupSchemaMatchesTheContractsItSpeaksTo(t *testing.T) {
	create := &vpcv1.CreateCidrGroupRequest{}
	update := &vpcv1.UpdateCidrGroupRequest{}
	read := &vpcv1.CidrGroup{}

	// Предпосылка проверяется ЗДЕСЬ ЖЕ: перечень, переживший свой контракт, молча
	// перестал бы что-либо утверждать.
	for _, n := range cidrGroupAcceptedByCreate {
		if !hasContractField(create, n) {
			t.Fatalf("предпосылка пробы устарела: %q нет в контракте создания набора — "+
				"перечень принимаемых полей описывает не дерево, а память автора", n)
		}
	}
	for _, n := range cidrGroupComputedOnly {
		if hasContractField(create, n) {
			t.Fatalf("предпосылка пробы устарела: %q появилось в контракте СОЗДАНИЯ — "+
				"поле перестало быть выводимым, и запрет на его задание надо перечитать", n)
		}
		if !hasContractField(read, n) {
			t.Fatalf("предпосылка пробы устарела: %q нет в проекции чтения — вычисляемому "+
				"атрибуту неоткуда взять значение", n)
		}
	}
	// Состав НЕ принимается изменением: у него свои глаголы. Проверяется по контракту, а не
	// по памяти — иначе запрет ниже однажды станет ложью молча.
	for _, n := range []string{"v4_cidr_blocks", "v6_cidr_blocks"} {
		if hasContractField(update, n) {
			t.Fatalf("предпосылка пробы устарела: %q появилось в контракте ИЗМЕНЕНИЯ — "+
				"состав стало можно править маской, и приведение глаголами надо перечитать", n)
		}
	}

	attrs := cidrGroupSchemaAttrs(t)

	// (1) принимаемое краем обязано быть ЗАДАВАЕМЫМ. Иначе «поле есть» удовлетворялось бы
	// вычисляемым зеркалом — возможность объявлена и недоступна.
	for _, n := range cidrGroupAcceptedByCreate {
		a, ok := attrs[n]
		if !ok {
			t.Errorf("схема не выставляет атрибут %q, который край ПРИНИМАЕТ на создании", n)
			continue
		}
		if !a.IsRequired() && !a.IsOptional() {
			t.Errorf("атрибут %q объявлен только вычисляемым, хотя край принимает его на создании", n)
		}
	}

	// (2) выводимое краем задавать НЕЛЬЗЯ: план обещал бы применение того, что край отвергает
	// («… is immutable after CidrGroup.Create»).
	for _, n := range cidrGroupComputedOnly {
		a, ok := attrs[n]
		if !ok {
			t.Errorf("схема не выставляет вычисляемый атрибут %q — факт, который край сообщает, "+
				"пользователю не виден", n)
			continue
		}
		if a.IsRequired() || a.IsOptional() {
			t.Errorf("атрибут %q объявлен задаваемым, хотя край его на вход не принимает", n)
		}
	}

	// (3) зеркальная сверка: в схеме нет НИ ОДНОГО атрибута, которого край не знает ни на
	// входе, ни на выходе. Без неё пункты выше зеленели бы на схеме с лишним полем.
	var unknown []string
	for name := range attrs {
		if hasContractField(create, name) || hasContractField(read, name) {
			continue
		}
		unknown = append(unknown, name)
	}
	sort.Strings(unknown)
	for _, n := range unknown {
		t.Errorf("схема несёт атрибут %q, которого нет ни в контракте создания, ни в проекции "+
			"чтения: конфигурация предлагает возможность, которой у края нет", n)
	}
	t.Logf("осмотрено: атрибутов схемы %d, принимаемых краем %d, выводимых %d",
		len(attrs), len(cidrGroupAcceptedByCreate), len(cidrGroupComputedOnly))
}

// Неизменяемое поле объявлено неизменяемым и В СХЕМЕ.
//
// Иначе план обещает правку, которую край отвергает: проект у набора не меняется, и
// молчаливое «применится» здесь означает отказ на apply вместо замены в плане.
func TestCidrGroupImmutableAttributesRequireReplace(t *testing.T) {
	attrs := cidrGroupSchemaAttrs(t)

	a, ok := attrs["project_id"].(schema.StringAttribute)
	if !ok {
		t.Fatalf("project_id не строковый атрибут: %T", attrs["project_id"])
	}
	if !requiresReplaceOnChange(a.PlanModifiers) {
		t.Error("project_id не помечен пересоздающим: план пообещал бы правку владения, " +
			"которую край отвергает")
	}
	// Парный положительный контроль: изменяемое поле пересоздающим НЕ помечено — иначе
	// проба выше зеленела бы на схеме, где пересоздаёт всё подряд, а правка имени сносила
	// бы живой набор вместе со ссылками на него.
	n, ok := attrs["name"].(schema.StringAttribute)
	if !ok {
		t.Fatalf("name не строковый атрибут: %T", attrs["name"])
	}
	if requiresReplaceOnChange(n.PlanModifiers) {
		t.Error("name помечен пересоздающим, хотя край меняет его маской изменения")
	}
}

// requiresReplaceOnChange — ИСХОД правки, а не объявление модификатора.
//
// Первая редакция читала прозу `Description()` и искала в ней слово «replace»; фреймворк
// пишет там «destroy and recreate», поэтому проверка отвечала «не помечен» на верно
// помеченном атрибуте. Здесь модификатор ИСПОЛНЯЕТСЯ на паре различающихся значений, и
// утверждение делается о том, что он сделает с планом.
func requiresReplaceOnChange(mods []planmodifier.String) bool {
	ctx := context.Background()
	// Ни состояние, ни план не должны быть null: у null фреймворк читает создание и
	// уничтожение, а предмет проверки — ПРАВКА существующего.
	nonNull := tftypes.NewValue(
		tftypes.Object{AttributeTypes: map[string]tftypes.Type{}},
		map[string]tftypes.Value{})
	for _, m := range mods {
		req := planmodifier.StringRequest{
			State:       tfsdk.State{Raw: nonNull},
			Plan:        tfsdk.Plan{Raw: nonNull},
			StateValue:  types.StringValue("было"),
			PlanValue:   types.StringValue("стало"),
			ConfigValue: types.StringValue("стало"),
		}
		var resp planmodifier.StringResponse
		m.PlanModifyString(ctx, req, &resp)
		if resp.RequiresReplace {
			return true
		}
	}
	return false
}

// ---- маска изменения не несёт состава ----------------------------------------------------

// Маска изменения НИКОГДА не несёт состава — его правят глаголы.
//
// Край отвергает `v4_cidr_blocks` в маске синхронно и с именем поля; попади оно туда,
// пользователь получал бы отказ на правку, которую провайдер обязан был выполнить другим
// вызовом.
func TestCidrGroupUpdateMaskCarriesCosmeticsOnly(t *testing.T) {
	ctx := context.Background()
	state := cidrGroupModel{
		ID:           types.StringValue("cdg-0123456789abcdefg"),
		Name:         types.StringValue("office"),
		Description:  types.StringValue("было"),
		Labels:       mapToTF(ctx, map[string]string{"a": "1"}),
		V4CidrBlocks: listFromStrings(ctx, []string{"203.0.113.0/24"}),
		V6CidrBlocks: listFromStrings(ctx, nil),
	}
	plan := state
	plan.Description = types.StringValue("стало")
	plan.V4CidrBlocks = listFromStrings(ctx, []string{"198.51.100.0/24"})

	body, paths := cidrGroupUpdateBody(ctx, plan, state)
	sort.Strings(paths)
	if strings.Join(paths, ",") != "description" {
		t.Fatalf("маска: %v, ожидалось ровно [description] — состав в маску не попадает", paths)
	}
	if body.GetCidrGroupId() != state.ID.ValueString() {
		t.Errorf("тело изменения не называет ресурс: %q", body.GetCidrGroupId())
	}

	// Парный положительный контроль: все три косметических поля В маску попадают. Без него
	// утверждение выше зеленело бы на функции, которая не возвращает ничего никогда.
	plan = state
	plan.Name = types.StringValue("office-2")
	plan.Description = types.StringValue("стало")
	plan.Labels = mapToTF(ctx, map[string]string{"a": "2"})
	_, paths = cidrGroupUpdateBody(ctx, plan, state)
	sort.Strings(paths)
	if strings.Join(paths, ",") != "description,labels,name" {
		t.Fatalf("маска косметики: %v, ожидались все три поля", paths)
	}

	// Правка ОДНОГО состава не даёт запроса изменения вовсе — но даёт дельту глаголам.
	plan = state
	plan.V6CidrBlocks = listFromStrings(ctx, []string{"2001:db8::/32"})
	if _, paths = cidrGroupUpdateBody(ctx, plan, state); len(paths) != 0 {
		t.Fatalf("правка одного состава дала маску %v — запрос изменения был бы отвергнут краем", paths)
	}
	var d cidrDelta
	d.addV6, d.removeV6 = cidrFamilyDiff(ctx, plan.V6CidrBlocks, state.V6CidrBlocks)
	if len(d.addV6) != 1 {
		t.Fatalf("дельта состава пуста: %+v — правка не доехала бы ни маской, ни глаголом", d)
	}
}

// ---- приведение состава ------------------------------------------------------------------

// Состав приводится СНАЧАЛА добавлением, потом снятием — и обе семьи одним вызовом на глагол.
//
// Порядок не произволен: обратный проводит набор через состояние, где семья пуста, а
// опустошение семьи, на которую ссылается живое правило, край отвергает. Одна пара семей в
// одном вызове — потому что край принимает их одним сообщением и пишет одной транзакцией;
// два вызова дали бы две операции и промежуточное состояние между ними.
func TestCidrGroupReconcileAddsBeforeRemovingInOneCallPerVerb(t *testing.T) {
	type call struct {
		verb string
		body map[string]any
	}
	var calls []call
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(req.Body).Decode(&body)
		switch {
		case strings.HasSuffix(req.URL.Path, ":add-cidr-blocks"):
			calls = append(calls, call{verb: "add", body: body})
		case strings.HasSuffix(req.URL.Path, ":remove-cidr-blocks"):
			calls = append(calls, call{verb: "remove", body: body})
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"enp1","done":true,"metadata":{}}`))
	}))
	defer srv.Close()

	r := &cidrGroupResource{c: mustProviderClient(t, srv.URL)}
	d := cidrDelta{
		addV4: []string{"198.51.100.0/24"}, addV6: []string{"2001:db8:1::/48"},
		removeV4: []string{"203.0.113.0/24"}, removeV6: []string{"2001:db8:2::/48"},
	}
	if err := r.reconcileBlocks(context.Background(), "cdg-0123456789abcdefg", d); err != nil {
		t.Fatalf("приведение состава: %v", err)
	}
	if len(calls) != 2 || calls[0].verb != "add" || calls[1].verb != "remove" {
		t.Fatalf("вызовы: %+v, ожидалось [add remove] ровно по одному", calls)
	}
	for _, c := range calls {
		if c.body["v4CidrBlocks"] == nil || c.body["v6CidrBlocks"] == nil {
			t.Errorf("вызов %s несёт не обе семьи: %v — вторая семья уехала бы отдельной "+
				"операцией либо не уехала вовсе", c.verb, c.body)
		}
	}

	// Зеркальный контроль: пустая дельта не порождает НИ ОДНОГО вызова. Край отвергает
	// глагол, в котором обе семьи пусты, — такой запрос завалил бы apply, которому нечего
	// было делать.
	calls = nil
	if err := r.reconcileBlocks(context.Background(), "cdg-0123456789abcdefg", cidrDelta{}); err != nil {
		t.Fatalf("пустая дельта дала отказ: %v", err)
	}
	if len(calls) != 0 {
		t.Fatalf("пустая дельта породила вызовы: %+v", calls)
	}
}

// ---- чтение ------------------------------------------------------------------------------

// Ответ края доезжает в состояние целиком — включая выведенные им факты.
func TestApplyCidrGroupKeepsWhatTheEdgeAnswered(t *testing.T) {
	raw := []byte(`{"id":"cdg-0123456789abcdefg","projectId":"prj1",
		"createdAt":"2026-08-13T10:00:00Z","name":"office","description":"d",
		"labels":{"suite":"newman"},
		"v4CidrBlocks":["203.0.113.0/24","198.51.100.16/28"],"v6CidrBlocks":["2001:db8::/32"],
		"cidrBlockCount":3,
		"usedBy":[{"referrer":{"type":"vpc.securityGroup","id":"sgr1","name":"web"},
			"type":"USED_BY","owned":false}]}`)

	var m cidrGroupModel
	if err := applyCidrGroup(context.Background(), &m, raw); err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if got := stringsFromTF(context.Background(), m.V4CidrBlocks); len(got) != 2 {
		t.Errorf("состав IPv4 потерян: %v", got)
	}
	if got := stringsFromTF(context.Background(), m.V6CidrBlocks); len(got) != 1 {
		t.Errorf("состав IPv6 потерян: %v", got)
	}
	if m.CidrBlockCount.ValueInt64() != 3 {
		t.Errorf("число членов: %v, ожидалось 3", m.CidrBlockCount)
	}
	if m.UsedBy.IsNull() || len(m.UsedBy.Elements()) != 1 {
		t.Errorf("потребители набора потеряны: %v — «почему набор не удаляется» осталось бы "+
			"без ответа", m.UsedBy)
	}

	// Пустые семьи приезжают ПУСТЫМ списком, а не null: край всегда отвечает массивом, и
	// null давал бы «известно после применения» на каждом плане неизменной инфраструктуры.
	var empty cidrGroupModel
	if err := applyCidrGroup(context.Background(), &empty,
		[]byte(`{"id":"cdg-0123456789abcdefg","name":"n","cidrBlockCount":0}`)); err != nil {
		t.Fatalf("разбор пустого набора: %v", err)
	}
	if empty.V4CidrBlocks.IsNull() || empty.UsedBy.IsNull() {
		t.Errorf("пустые списки записаны как null: v4=%v usedBy=%v", empty.V4CidrBlocks, empty.UsedBy)
	}
}

// ---- импорт ------------------------------------------------------------------------------

// Идентификатор набора — ДЕФИСНОЙ формы, и проверка импорта обязана знать именно её.
//
// Проверка слитной формы (`ids.IsValid`) отвергла бы КАЖДЫЙ настоящий идентификатор набора:
// он длиннее на разделитель. Такой отказ выглядел бы как «Terraform не умеет импортировать
// этот ресурс», хотя не умела бы проверка.
func TestCidrGroupImportKnowsTheHyphenForm(t *testing.T) {
	const good = "cdg-0123456789abcdefg" // 3 + 1 + 17

	if !hyphenIDIsValid(good, ids.PrefixCidrGroupHyphen) {
		t.Fatalf("настоящий идентификатор набора %q отвергнут — импорт был бы невозможен", good)
	}
	// Контроль негодного: чужая форма, чужой префикс, короткое тело и знак вне алфавита.
	for _, bad := range []string{
		"net01234567890abcdef",  // слитная форма чужого ресурса
		"sub-0123456789abcdefg", // дефисная форма, но не наш вид
		"cdg-0123",              // тело короче
		"cdg-0123456789abcdefi", // «i» в crockford-base32 не входит
		"cdg-",
		"",
		"office",
	} {
		if hyphenIDIsValid(bad, ids.PrefixCidrGroupHyphen) {
			t.Errorf("негодная строка %q принята за идентификатор набора", bad)
		}
	}

	// Опечатка в самой таблице видов провайдера ловится здесь же: префикс, которого нет в
	// каноне платформы, не делает годным ничего.
	if hyphenIDIsValid("zzz-0123456789abcdefg", "zzz") {
		t.Error("префикс вне канона платформы принят — опечатка в провайдере прошла бы молча")
	}

	// Тот же исход виден и на настоящем пути импорта — В ОБЕ СТОРОНЫ.
	//
	// Одного отрицания здесь мало, и это измерено: пока проба утверждала только отказ на
	// чужом идентификаторе, подмена проверки на слитную форму (`importByID`) оставалась
	// зелёной — чужой отвергается обеими, а настоящий идентификатор набора отвергался бы
	// молча, и импорт ресурса стал бы невозможен.
	if diags := importDiagnostics(t, "net01234567890abcdef"); !diags {
		t.Error("импорт принял идентификатор чужого ресурса — он уехал бы на край за ответом " +
			"«не найдено», который для такой строки не значит ничего")
	}
	if diags := importDiagnostics(t, good); diags {
		t.Errorf("импорт отверг настоящий идентификатор набора %q — ресурс, заведённый вне "+
			"Terraform, нельзя было бы взять под управление вовсе", good)
	}
}

// importDiagnostics — отверг ли ИМПОРТ РЕСУРСА эту строку.
//
// Состояние собирается по настоящей схеме ресурса и непустым объектом: у null-объекта
// запись атрибута не проходит, и положительная половина пробы отвечала бы «отверг» на
// любом вводе — то есть перестала бы отличать исходы.
func importDiagnostics(t *testing.T, id string) bool {
	t.Helper()
	ctx := context.Background()

	var sresp resource.SchemaResponse
	NewVPCCidrGroupResource().Schema(ctx, resource.SchemaRequest{}, &sresp)
	objType, ok := sresp.Schema.Type().TerraformType(ctx).(tftypes.Object)
	if !ok {
		t.Fatalf("схема ресурса не объект: %T", sresp.Schema.Type().TerraformType(ctx))
	}
	nulls := make(map[string]tftypes.Value, len(objType.AttributeTypes))
	for name, at := range objType.AttributeTypes {
		nulls[name] = tftypes.NewValue(at, nil)
	}

	resp := resource.ImportStateResponse{
		State: tfsdk.State{Schema: sresp.Schema, Raw: tftypes.NewValue(objType, nulls)},
	}
	NewVPCCidrGroupResource().(resource.ResourceWithImportState).ImportState(ctx,
		resource.ImportStateRequest{ID: id}, &resp)
	return resp.Diagnostics.HasError()
}
