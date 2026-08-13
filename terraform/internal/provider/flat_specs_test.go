// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package provider

import (
	"context"
	"sort"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
)

// ---- вспомогательное -------------------------------------------------------------------

func hasContractField(m proto.Message, name string) bool {
	return m.ProtoReflect().Descriptor().Fields().ByName(protoreflect.Name(name)) != nil
}

func specFieldNames(spec flatSpec) map[string]fieldSpec {
	out := map[string]fieldSpec{}
	for _, f := range spec.fields {
		out[f.name] = f
	}
	return out
}

// flatSchemaAttrs — схема, которую УВИДИТ пользователь.
//
// Проверять один список полей недостаточно: пользователю адресована схема, и утверждать
// надо про неё. Список и схема сегодня выводятся друг из друга, но это свойство каркаса,
// а не входное условие пробы.
func flatSchemaAttrs(t *testing.T, spec flatSpec) map[string]schema.Attribute {
	t.Helper()
	var resp resource.SchemaResponse
	newFlatResource(spec)().Schema(context.Background(), resource.SchemaRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("%s: схема не собралась: %v", spec.tfName, resp.Diagnostics.Errors())
	}
	return resp.Schema.Attributes
}

// registeredFlatSpecs — описания, ДОСТИЖИМЫЕ из реестра провайдера.
//
// Направление обхода то же, что у гейта паритета в корневом модуле: от реестра к
// описаниям, а не наоборот. Описание, не провязанное в реестр, для пользователя не
// существует; перечисленное руками — переживает своё дерево.
func registeredFlatSpecs(t *testing.T) []flatSpec {
	t.Helper()
	var out []flatSpec
	for _, ctor := range New().Resources(context.Background()) {
		if fr, ok := ctor().(*flatResource); ok {
			out = append(out, fr.spec)
		}
	}
	if len(out) == 0 {
		t.Fatal("в реестре провайдера не найдено ни одного плоского описания — обходчик " +
			"сломан или каркас переехал; ноль здесь означает непрочитанное дерево, а не чистое")
	}
	sort.Slice(out, func(i, j int) bool { return out[i].tfName < out[j].tfName })
	return out
}

// ---- поле, которое край отвергает БЕЗУСЛОВНО --------------------------------------------

// Поля создания интерфейса, отвергаемые краем на этом пути синхронно и с именем поля.
//
// Перечень назван здесь, а не выведен из кода сервиса: сервис лежит в другом дереве, и
// проба провайдера о его внутренностях судить не вправе. Зато ПРЕДПОСЫЛКА перечня
// проверяется механически — по контракту, который провайдеру доступен (ниже).
var networkInterfaceRefusedByEdge = []string{"instance_id", "index"}

// Поля, которые край на этом пути ПРИНИМАЕТ. Парный положительный контроль: без него
// проба выше зеленела бы на описании, из которого вынесли всё подряд.
var networkInterfaceAcceptedByEdge = []string{
	"project_id", "name", "subnet_id", "security_group_ids", "v4_address_ids", "v6_address_ids",
}

// Провайдер не предлагает задать то, что край всегда отвергает.
//
// Поле, которое конфигурация принимает, а край отвергает безусловно, — обещание
// возможности, которой нет: пользователь пишет строку, план её показывает, и отказ
// приходит только применением. Хуже того, у этих двух полей нет и обратного пути:
// проекция чтения их не несёт вовсе, поэтому «вычисляемым зеркалом» они тоже быть не
// могут — в состоянии навсегда осталась бы пустая строка.
func TestNetworkInterfaceSpecOffersNothingTheEdgeRefuses(t *testing.T) {
	create := &vpcv1.CreateNetworkInterfaceRequest{}
	read := &vpcv1.NetworkInterface{}

	// Предпосылка пробы проверяется ЗДЕСЬ ЖЕ, иначе запрет однажды станет ложью молча.
	// Оба условия обязательны: поле ещё стоит в контракте создания (значит запрет
	// по-прежнему про него, а не про снятый номер) и его нет в проекции чтения (значит
	// значение не может приехать обратно).
	for _, n := range networkInterfaceRefusedByEdge {
		if !hasContractField(create, n) {
			t.Fatalf("предпосылка пробы устарела: %q больше нет в контракте создания интерфейса — "+
				"поле сняли с контракта, снимите и запись из перечня, иначе исключению нечего "+
				"исключать", n)
		}
		if hasContractField(read, n) {
			t.Fatalf("предпосылка пробы устарела: %q появилось в проекции чтения интерфейса — "+
				"край стал его отдавать, и запрет надо перечитать заново, а не держать по памяти", n)
		}
	}

	names := specFieldNames(vpcNetworkInterfaceSpec)
	attrs := flatSchemaAttrs(t, vpcNetworkInterfaceSpec)

	for _, n := range networkInterfaceRefusedByEdge {
		if _, ok := names[n]; ok {
			t.Errorf("описание интерфейса несёт поле %q, которое край отвергает синхронно на "+
				"создании: конфигурация предлагает возможность, которой нет", n)
		}
		if _, ok := attrs[n]; ok {
			t.Errorf("схема ресурса %s выставляет атрибут %q, который край отвергает синхронно "+
				"на создании", vpcNetworkInterfaceSpec.tfName, n)
		}
	}

	for _, n := range networkInterfaceAcceptedByEdge {
		if !hasContractField(create, n) {
			t.Fatalf("положительный контроль назвал поле %q, которого в контракте создания нет — "+
				"контроль негоден и зеленел бы на любом описании", n)
		}
		if _, ok := names[n]; !ok {
			t.Errorf("описание интерфейса потеряло поле %q, которое край ПРИНИМАЕТ", n)
			continue
		}
		a, ok := attrs[n]
		if !ok {
			t.Errorf("схема ресурса %s не выставляет атрибут %q, который край ПРИНИМАЕТ",
				vpcNetworkInterfaceSpec.tfName, n)
			continue
		}
		// Принимаемое краем поле обязано быть ЗАДАВАЕМЫМ. Иначе «поле есть» удовлетворялось
		// бы вычисляемым зеркалом — зеркальная форма того же дефекта: возможность объявлена
		// и недоступна.
		if !a.IsRequired() && !a.IsOptional() {
			t.Errorf("атрибут %s.%s объявлен только вычисляемым, хотя край принимает его на "+
				"создании", vpcNetworkInterfaceSpec.tfName, n)
		}
	}
}

// ---- поле описания обязано существовать в том контракте, в который его отправляют -------

// Опечатка в имени поля описания доезжает до края ТОЛЬКО когда пользователь это поле
// задал: перенос значения ищет поле контракта по имени и отказывает в момент отправки.
// Значит незаданное поле с чужим именем живёт в схеме сколько угодно и отказывает у
// пользователя, а не в прогоне. Проба закрывает это по каждому описанию из реестра.
func TestFlatSpecFieldsExistInTheContractTheyAreSentIn(t *testing.T) {
	specs := registeredFlatSpecs(t)
	checked := 0
	for _, spec := range specs {
		create, update := spec.newCreate(), spec.newUpdate()
		if !hasContractField(update, spec.updateIDField) {
			t.Errorf("%s: в контракте изменения нет поля идентификатора %q",
				spec.tfName, spec.updateIDField)
		}
		for _, f := range spec.fields {
			checked++
			switch {
			case f.computed:
				// Вычисляемое в запрос не уходит вовсе — контракту запроса оно не адресовано.
			case f.updateOnly:
				// Предпосылка самого признака: поля НЕТ в создании и оно ЕСТЬ в изменении.
				// Обе стороны обязательны — иначе признак однажды переживёт свой предмет и
				// будет молча выносить поле из создания, которое его принимает.
				if hasContractField(create, f.name) {
					t.Errorf("%s: поле %q помечено «только изменение», но контракт создания его "+
						"НЕСЁТ — признак пережил свой предмет, и значение доходит вторым запросом "+
						"там, где хватило бы первого", spec.tfName, f.name)
				}
				if !hasContractField(update, f.name) {
					t.Errorf("%s: поле %q помечено «только изменение», но контракт изменения его "+
						"не несёт — задать его нельзя ни одним запросом", spec.tfName, f.name)
				}
			default:
				if !hasContractField(create, f.name) {
					t.Errorf("%s: поле %q описания отсутствует в контракте создания %s",
						spec.tfName, f.name, create.ProtoReflect().Descriptor().FullName())
				}
				// Изменяемое поле уходит в маску изменения — значит контракт изменения обязан
				// его нести. Неизменяемое туда не попадает by construction.
				if !f.immutable && !hasContractField(update, f.name) {
					t.Errorf("%s: изменяемое поле %q отсутствует в контракте изменения %s",
						spec.tfName, f.name, update.ProtoReflect().Descriptor().FullName())
				}
			}
		}
	}
	t.Logf("осмотрено: описаний %d, полей %d", len(specs), checked)
}
