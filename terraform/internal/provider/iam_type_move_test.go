// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package provider

// Переезд состояния арендатора под новое имя типа — по КАЖДОМУ переименованному типу.
//
// # Предмет: не «объявлен ли переезд», а «пережило ли его состояние»
//
// Имя типа — внешне адресуемая координата записи в состоянии оператора (ban #15).
// Переименование без переезда означает для арендатора не ошибку, а УДАЛЕНИЕ живой записи и
// создание новой. Проверять поэтому надо ИСХОД: то, что вошло в переезд, обязано из него
// выйти — целиком и тем же.
//
// Утверждение «у типа объявлен метод переезда» этого не даёт. Метод, возвращающий пустой
// перечень, объявлен так же, как работающий; метод, отдающий состояние с обнулёнными
// полями, объявлен так же, как отдающий его целиком. Оба прошли бы проверку наличия и оба
// осиротили бы запись арендатора. Обе формы прогоняются инъекцией
// (iam_type_move_injection_test.go), и вторая — та, ради которой предмет назван исходом.
//
// # Почему через сервер протокола, а не вызовом метода
//
// Вызов метода вернул бы перечень объявлений, и судить пришлось бы их — то есть снова
// объявление. Сервер протокола (providerserver.NewProtocol6) исполняет ту же цепочку, что
// исполнитель: находит целевой тип в реестре, спрашивает его схему, разбирает присланное
// состояние по объявленной источником схеме, прогоняет объявления переезда по порядку и
// собирает ответ. Всё, что стоит между запросом и состоянием, здесь настоящее.
//
// # Почему вердикт вынесен ФУНКЦИЕЙ, а не разложен по пробам
//
// Способность пробы упасть доказывается инъекцией, а внести дефект в СОБРАННЫЙ провайдер
// проба не может. Поэтому вердикт выносит moveProbeAudit — она принимает провайдера и
// возвращает находки. Проба ниже зовёт её с настоящим; инъекция зовёт её с провайдером,
// у которого переезд одного типа испорчен одним названным фактом.
//
// # Чем это дополняет приёмку и чем от неё отличается
//
// acceptance_iam_type_move_test.go гоняет ПОЛНЫЙ цикл исполнителя для ОДНОГО типа: план,
// применение, повторный план, сверка идентификатора. Это глубина, и цена её такова, что
// девять таких циклов пришлось бы оплачивать девятью поддельными краями и девятью
// настройками; под `-short` они не гоняются вовсе, поэтому в джобе `go` переезд сегодня не
// сторожится ничем.
//
// Здесь — ШИРИНА: тот же запрос переезда по каждому переименованному типу, без исполнителя
// и без края, за миллисекунды и без `-short`-пропуска. Глубина и ширина друг друга не
// заменяют: приёмка отвечает «что увидит оператор», эта проба — «у всех ли типов есть,
// чему переезжать».
//
// # Что здесь НЕ проверяется — сказано прямо
//
//   - пустота ПЛАНА и сохранность идентификатора после применения: предмет приёмки, и он
//     остаётся за одним типом;
//   - версия схемы источника, отличная от нулевой, и неразобранное состояние: у обеих
//     веток свой текст отказа, и производителя у них сегодня нет;
//   - переезд НЕЗАПОЛНЕННЫХ (null) полей: состояние ниже заполнено целиком, потому что
//     предмет утверждения — «каждое поле доехало», и незаполненное поле его ослабило бы.

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// moveProbeRegistryHosts — узлы реестра, из-под которых приходит состояние.
//
// Узел НЕ значим: провайдер сверяет у источника хвост адреса (пространство имён и тип), а
// узел зависит от того, каким исполнителем состояние писалось. Оба перечислены не для
// полноты, а потому что это РЕШЕНИЕ (iam_type_names.go, sourceProviderIsOurs) — и до сих
// пор у него не было ни одного производителя: сужение сверки хвоста до сверки целого
// адреса не роняло ничего.
var moveProbeRegistryHosts = []string{"registry.opentofu.org", "registry.terraform.io"}

// moveProbeForeignSourceType — имя типа, которого никто не объявлял снятым.
//
// Приставки продукта в нём нет намеренно: она сделала бы строку похожей на действующее имя
// для всякого, кто читает дерево поиском, — и для переписи остатка имён в том числе.
const moveProbeForeignSourceType = "unknown_source_type"

// moveProbeForeignProvider — адрес заведомо чужого провайдера.
const moveProbeForeignProvider = "registry.terraform.io/hashicorp/null"

// ---- вердикт --------------------------------------------------------------------------

// moveProbeFinding — одна находка, привязанная к ТИПУ.
//
// Тип назван в каждой находке, и это несущее: носителей семь, типов девять, один носитель
// обслуживает три типа сразу. Общий счёт находок оставил бы два типа за спиной соседа по
// реализации — ровно тот случай, ради которого проба заведена.
type moveProbeFinding struct {
	TypeName string
	What     string
}

// moveProbeCensus — объём осмотренного. Величины ВРОЗЬ и из разных источников: «объявлено»
// читается из словаря снятых имён, «сторожится» набирается обходом реестра с доведением
// вердикта. Одно число не отличило бы «обошли всё» от «обошли столько, сколько нашли».
type moveProbeCensus struct {
	Registered      int // типов в реестре провайдера
	DeclaredRenames int // переездов объявлено словарём снятых имён
	GuardedMove     int // типов, по которым вердикт о переезде доведён
	GuardedRefusal  int // типов, по которым доведён вердикт об отказе принять чужое
	WithoutRename   int // типов, имени не менявших
	SharedCarrier   int // из них делят реализацию с переименованным
	SharedExample   string
}

// moveProbeAudit судит переезд у провайдера p и возвращает находки с переписью.
//
// По каждому зарегистрированному типу задаётся ровно один из двух вопросов, и оба — про
// ИСХОД, а не про объявление:
//
//	имя менялось    → состояние обязано ПЕРЕЖИТЬ переезд с прежнего имени, и обязано быть
//	                  ОТВЕРГНУТО, если источник чужой (чужой тип, чужой провайдер);
//	имя не менялось → переезд обязан быть ОТВЕРГНУТ: переезжать неоткуда.
func moveProbeAudit(p provider.Provider) ([]moveProbeFinding, moveProbeCensus) {
	ctx := context.Background()
	srv := providerserver.NewProtocol6(p)()
	ours := moveProbeRegistryHosts[0] + "/" + providerSourceAddress

	var findings []moveProbeFinding
	census := moveProbeCensus{DeclaredRenames: len(retiredResourceTypeNames)}
	add := func(typeName, format string, args ...any) {
		findings = append(findings, moveProbeFinding{TypeName: typeName, What: fmt.Sprintf(format, args...)})
	}

	type entry struct {
		name    string
		retired string
		carrier string
		res     resource.Resource
	}
	var registry []entry
	for _, ctor := range p.Resources(ctx) {
		r := ctor()
		name := typeNameOfResource(r)
		registry = append(registry, entry{
			name: name, retired: retiredResourceTypeNames[name],
			carrier: typeOfResourceImpl(r), res: r,
		})
	}
	sort.Slice(registry, func(i, j int) bool { return registry[i].name < registry[j].name })
	census.Registered = len(registry)

	renamedCarriers := map[string]string{}
	for _, e := range registry {
		if e.retired != "" {
			renamedCarriers[e.carrier] = e.name
		}
	}

	for _, e := range registry {
		state, typ, err := moveProbeSourceState(e.res)
		if err != nil {
			// Оснастка, не собравшая состояние, оставляет тип БЕЗ вердикта. Это
			// находка, а не пропуск: иначе перепись сказала бы «сторожится», не
			// спросив ни разу.
			add(e.name, "состояние источника не собрано (%s): %v", e.carrier, err)
			continue
		}

		if e.retired == "" {
			census.WithoutRename++
			resp, err := moveProbeCall(srv, e.name, moveProbeForeignSourceType, ours, state)
			if err != nil {
				add(e.name, "запрос переезда не исполнен: %v", err)
				continue
			}
			if len(moveProbeErrors(resp)) == 0 || resp.TargetState != nil {
				add(e.name, "тип ПРИНЯЛ переезд, хотя имени не менял (%s).\n"+
					"    Приём состояния с имени, которого у типа не было, означает, что "+
					"объявление переезда спрашивает НОСИТЕЛЯ, а не словарь снятых имён: "+
					"тогда всякий сосед по реализации получает чужой переезд даром.", e.carrier)
				continue
			}
			census.GuardedRefusal++
			if renamed, ok := renamedCarriers[e.carrier]; ok {
				census.SharedCarrier++
				census.SharedExample = e.name + " ↔ " + renamed
			}
			continue
		}

		want, err := (&tfprotov6.RawState{JSON: state}).Unmarshal(typ)
		if err != nil {
			add(e.name, "состояние источника не разобрано по схеме: %v", err)
			continue
		}

		moved := true
		for _, host := range moveProbeRegistryHosts {
			addr := host + "/" + providerSourceAddress
			resp, err := moveProbeCall(srv, e.name, e.retired, addr, state)
			if err != nil {
				add(e.name, "запрос переезда не исполнен: %v", err)
				moved = false
				continue
			}
			if errs := moveProbeErrors(resp); len(errs) > 0 {
				add(e.name, "переезд ОТВЕРГНУТ (%s, источник %s):\n    %s\n"+
					"    Тип сменил имя, значит запись арендатора лежит под прежним адресом. "+
					"Отказ переезда означает для оператора не ошибку настройки, а "+
					"осиротевшую запись живого ресурса.",
					e.carrier, addr, strings.Join(errs, "\n    "))
				moved = false
				continue
			}
			if resp.TargetState == nil {
				add(e.name, "переезд НЕ ОТДАЛ состояния (%s, источник %s): запись арендатора "+
					"после переезда пуста", e.carrier, addr)
				moved = false
				continue
			}
			got, err := resp.TargetState.Unmarshal(typ)
			if err != nil {
				add(e.name, "состояние после переезда не разбирается по схеме типа (%s): %v",
					e.carrier, err)
				moved = false
				continue
			}
			if !want.Equal(got) {
				add(e.name, "состояние переезд НЕ ПЕРЕЖИЛО (%s, источник %s).\n"+
					"    было:  %v\n    стало: %v\n"+
					"    Переезд обязан менять АДРЕС записи, а не её содержимое: потерянное "+
					"здесь поле оператор недосчитается в живом ресурсе.",
					e.carrier, addr, want, got)
				moved = false
			}
		}
		if moved {
			census.GuardedMove++
		}

		// Не всеядность. Без неё сверка выше была бы наполовину бессодержательной:
		// переезд, отдающий присланное состояние кому угодно, прошёл бы её целиком — и
		// принял бы в свою запись содержимое чужой.
		refused := true
		for _, c := range []struct{ what, sourceType, sourceAddr, why string }{
			{"чужой тип-источник", moveProbeForeignSourceType, ours,
				"приняв состояние типа, которого никто не объявлял снятым, переезд втянул " +
					"бы в запись этого ресурса содержимое чужой"},
			{"чужой провайдер", e.retired, moveProbeForeignProvider,
				"имя типа у чужого провайдера может совпадать с нашим прежним; принятое от " +
					"него состояние собрано по ЧУЖОЙ схеме"},
		} {
			resp, err := moveProbeCall(srv, e.name, c.sourceType, c.sourceAddr, state)
			if err != nil {
				add(e.name, "запрос переезда не исполнен: %v", err)
				refused = false
				continue
			}
			if len(moveProbeErrors(resp)) == 0 || resp.TargetState != nil {
				add(e.name, "%s ПРИНЯТ (источник %s @ %s, %s).\n    %s",
					c.what, c.sourceType, c.sourceAddr, e.carrier, c.why)
				refused = false
			}
		}
		if refused {
			census.GuardedRefusal++
		}
	}

	return findings, census
}

// ---- оснастка состояния ------------------------------------------------------------------

// moveProbeSourceState — состояние, какое лежало бы у оператора под ПРЕЖНИМ именем типа.
//
// Форма берётся у самого ресурса (его схема), значения — свои и РАЗНЫЕ у каждого поля:
// переезд, перепутавший два однотипных поля местами или обнуливший одно из них, обязан
// быть отличим от исправного. Одинаковые значения этого не дают.
//
// Заполняется КАЖДОЕ поле схемы. Пустое или частичное состояние сделало бы сверку
// «состояние сохранилось» тождественно истинной: пустое, доехав пустым, совпадёт само с
// собой при любом переезде, включая несуществующий.
func moveProbeSourceState(r resource.Resource) ([]byte, tftypes.Type, error) {
	ctx := context.Background()

	var sr resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sr)
	if sr.Diagnostics.HasError() {
		return nil, nil, fmt.Errorf("схема ресурса не построена: %v", sr.Diagnostics)
	}
	typ := sr.Schema.Type().TerraformType(ctx)
	obj, ok := typ.(tftypes.Object)
	if !ok {
		return nil, nil, fmt.Errorf("схема ресурса не объект (%s) — состояние по ней не собрать", typ)
	}
	if len(obj.AttributeTypes) == 0 {
		return nil, nil, fmt.Errorf("в схеме ресурса нет ни одного поля — сверять после переезда нечего")
	}

	raw := make(map[string]any, len(obj.AttributeTypes))
	for name, at := range obj.AttributeTypes {
		v, err := moveProbeValue(at, name)
		if err != nil {
			return nil, nil, err
		}
		raw[name] = v
	}
	body, err := json.Marshal(raw)
	if err != nil {
		return nil, nil, fmt.Errorf("состояние источника не собрано: %w", err)
	}

	// Положительный контроль самой оснастки: собранное обязано разбираться по схеме и
	// быть заполненным целиком. Оснастка, молча отдавшая пустое, обесценила бы сверку
	// «состояние сохранилось», оставшись при этом зелёной.
	val, err := (&tfprotov6.RawState{JSON: body}).Unmarshal(typ)
	if err != nil {
		return nil, nil, fmt.Errorf("состояние источника не разбирается по схеме: %w\n%s", err, body)
	}
	if val.IsNull() || !val.IsFullyKnown() {
		return nil, nil, fmt.Errorf("состояние источника пусто или не полностью известно — "+
			"сверка после переезда была бы тождественно истинной:\n%s", body)
	}
	return body, typ, nil
}

// moveProbeValue — значение поля, производное от его пути в схеме.
//
// Путь входит в значение, поэтому два поля одного типа получают РАЗНЫЕ значения, и
// перестановка полей переездом перестаёт быть невидимой.
func moveProbeValue(typ tftypes.Type, path string) (any, error) {
	switch {
	case typ.Is(tftypes.String):
		return "значение-" + path, nil
	case typ.Is(tftypes.Bool):
		return true, nil
	case typ.Is(tftypes.Number):
		return len(path), nil
	}
	switch tt := typ.(type) {
	case tftypes.List:
		v, err := moveProbeValue(tt.ElementType, path+".0")
		return []any{v}, err
	case tftypes.Set:
		// Один элемент: у набора элементы обязаны быть различны, а различие ПОЛЕЙ и так
		// несёт путь.
		v, err := moveProbeValue(tt.ElementType, path+".0")
		return []any{v}, err
	case tftypes.Map:
		v, err := moveProbeValue(tt.ElementType, path+".k")
		return map[string]any{"k": v}, err
	case tftypes.Object:
		out := make(map[string]any, len(tt.AttributeTypes))
		for name, at := range tt.AttributeTypes {
			v, err := moveProbeValue(at, path+"."+name)
			if err != nil {
				return nil, err
			}
			out[name] = v
		}
		return out, nil
	case tftypes.Tuple:
		out := make([]any, 0, len(tt.ElementTypes))
		for i, et := range tt.ElementTypes {
			v, err := moveProbeValue(et, fmt.Sprintf("%s.%d", path, i))
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	}
	// Неизвестная форма — ОТКАЗ, а не пропуск: поле, которое оснастка не умеет заполнить,
	// осталось бы вне сверки, и «состояние сохранилось» читалось бы шире замеренного.
	return nil, fmt.Errorf("оснастка не умеет собрать значение поля %s типа %s — расширьте её, "+
		"иначе это поле молча выпадет из сверки переезда", path, typ)
}

// moveProbeCall исполняет запрос переезда — тот самый, что шлёт исполнитель.
func moveProbeCall(srv tfprotov6.ProviderServer, target, sourceType, sourceAddr string,
	state []byte) (*tfprotov6.MoveResourceStateResponse, error) {
	return srv.MoveResourceState(context.Background(), &tfprotov6.MoveResourceStateRequest{
		SourceProviderAddress: sourceAddr,
		SourceTypeName:        sourceType,
		SourceSchemaVersion:   0,
		SourceState:           &tfprotov6.RawState{JSON: state},
		TargetTypeName:        target,
	})
}

// moveProbeErrors — тексты отказов ответа.
func moveProbeErrors(resp *tfprotov6.MoveResourceStateResponse) []string {
	var out []string
	for _, d := range resp.Diagnostics {
		if d.Severity == tfprotov6.DiagnosticSeverityError {
			out = append(out, d.Summary+": "+d.Detail)
		}
	}
	return out
}

// moveProbeReport печатает перепись и находки. Общий у пробы и у инъекции: разойдясь, они
// назвали бы одно и то же разными числами.
func moveProbeReport(t *testing.T, findings []moveProbeFinding, c moveProbeCensus) {
	t.Helper()
	t.Logf("осмотрено: типов в реестре %d · переезд объявлен у %d · сторожится %d\n"+
		"          отказ принять чужое сторожится у %d типов; типов без переименования %d, "+
		"из них делят носителя с переименованным %d (%s)",
		c.Registered, c.DeclaredRenames, c.GuardedMove,
		c.GuardedRefusal, c.WithoutRename, c.SharedCarrier, c.SharedExample)
	for _, f := range findings {
		t.Errorf("%s: %s", f.TypeName, f.What)
	}
}

// ---- проба -------------------------------------------------------------------------------

// Состояние арендатора переживает переезд — по каждому типу, сменившему имя.
//
// # Почему по каждому, а не по одному через общего носителя
//
// Носителей семь, типов девять: один носитель обслуживает три типа сразу. Проба, взявшая
// по типу на носителя, оставила бы два типа вне наблюдения — их поломка была бы закрыта
// исправностью соседа по реализации. Здесь спрашивается ТИП, а не носитель, поэтому снятие
// переезда у любого из девяти называет его по имени.
func TestProviderCarriesTenantStateForEveryRenamedType(t *testing.T) {
	findings, census := moveProbeAudit(New())
	moveProbeReport(t, findings, census)

	if census.Registered == 0 {
		t.Fatal("реестр провайдера пуст — обход беспредметен: «переезд у всех типов цел» " +
			"означало бы «типов не прочитано ни одного»")
	}
	if census.DeclaredRenames == 0 {
		t.Fatal("снятых имён типов не объявлено ни одного — обход беспредметен.\n" +
			"Либо словарь опустел вместе с механизмом переезда — тогда эта проба снимается " +
			"ТЕМ ЖЕ изменением, что и он, — либо перепись читает не то дерево.")
	}
	if census.GuardedMove != census.DeclaredRenames {
		t.Errorf("переезд объявлен у %d типов, а вердикт доведён по %d.\n"+
			"Разница — типы, которым переезд обещан словарём снятых имён, но по которым "+
			"обход вердикта не вынес: либо реестр их больше не объявляет, либо проба до "+
			"сверки по ним не дошла. И то и другое означает, что переезд у них не "+
			"сторожится ничем.", census.DeclaredRenames, census.GuardedMove)
	}

	// Законный близнец, без которого проба выше не отличала бы «переезд у всех, кому он
	// положен» от «переезд у всех подряд».
	if census.WithoutRename == 0 {
		t.Fatal("типов без переименования в реестре нет — законного близнеца не существует, " +
			"и проверить, что переезд принимается не всеми подряд, нечем")
	}
	// Носитель у переименованного и непереименованного бывает ОБЩИЙ: плоские ресурсы
	// описываются одной реализацией, под которой и тип службы доступа, сменивший имя, и
	// полдюжины типов платформы, не менявших его. Подмена вопроса «что говорит словарь» на
	// «что умеет носитель» без такого близнеца осталась бы незамеченной.
	if census.SharedCarrier == 0 {
		t.Errorf("среди типов без переименования нет ни одного, делящего реализацию с " +
			"переименованным. Тогда близнец проверяет только чужие реализации, и переезд, " +
			"выданный носителю целиком, прошёл бы незамеченным.")
	}
}

// typeOfResourceImpl — имя Go-типа, реализующего ресурс.
//
// Носитель — не свойство контракта, а факт дерева, и спрашивается он у самой реализации:
// перечень «кто кого обслуживает», выписанный рядом, отстал бы молча при первом же новом
// плоском ресурсе.
func typeOfResourceImpl(r resource.Resource) string { return fmt.Sprintf("%T", r) }
