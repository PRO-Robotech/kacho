// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package declaredbreak

// Запись перечня прощает РОВНО ТОТ разрыв, который называет, и не прощает соседний
// разрыв того же правила в том же файле (kacho#2911; тот же класс — kaname#474).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Запись сопоставлялась с находкой по вхождению `"<symbol>"` в текст сообщения buf, и
// одна запись прощала ВСЕ находки, на которые подходила. Отсюда два независимых пути,
// которыми запись прощала больше одного разрыва:
//
//   - символ не различал соседей. У снятия значения перечисления buf печатает только
//     НОМЕР значения и имя перечисления (`enum value "4" on enum "Status"`), и запись
//     `symbol: Status` подходила к снятию любого значения этого словаря; запись с
//     именем поля (`page_size`) — к одноимённому полю любого сообщения файла; запись с
//     прежним именем типа ответа — к смене типа у любого RPC файла;
//   - запись не расходовалась. Даже при точном символе одна запись прощала сколько
//     угодно находок, которые её символ не различает.
//
// Измерено до починки на этом же входе: запись `Status` при снятых значениях 3 и 4
// сопоставлена 2 из 2, код 0; запись `page_size` при поле, снятом в двух сообщениях, —
// 2 из 2, код 0.
//
// ─────────────────────────────────────────────────────────────────────────────
// ВХОД НАСТОЯЩИЙ
//
// Каждая фикстура `testdata/neighbour/*.jsonl` снята `buf breaking 1.72.0
// --error-format=json` по СОБСТВЕННЫМ контрактам этого дерева (`proto/` ревизии
// b7608fe9750): в копии `proto/` сделана одна правка, база сравнения — нетронутая
// копия. Правки:
//
//	enum-value-4-deleted                    — снято значение 4 (STOPPED) перечисления Instance.Status
//	enum-values-3-4-deleted                 — и значение 3 (STOPPING) того же перечисления
//	enum-value-4-renamed                    — значение 4 переименовано
//	enum-values-3-4-renamed                 — и значение 3
//	field-deleted-in-one-message            — снято поле page_size в ListNetworksRequest
//	field-deleted-in-two-messages           — и одноимённое поле в ListNetworkOperationsRequest
//	field-2-renamed                         — поле 2 ListNetworksRequest переименовано
//	fields-2-3-renamed-in-one-message       — и поле 3 того же сообщения
//	rpc-response-type-changed-in-one-rpc    — NetworkService.Create отвечает Network вместо операции
//	rpc-response-type-changed-in-two-rpcs   — и NetworkService.Update
//	reserved-enum-name-deleted              — снято зарезервированное имя FAILED у NetworkInterface.Status
//	reserved-enum-names-deleted             — и имя DELETING
//	reserved-message-name-and-range-deleted — сняты `reserved 21; reserved "apply_state";` у NetworkInterface
//	required-field-2-deleted                — снято обязательное поле 2 сообщения RequiredProbe
//	required-fields-1-2-deleted             — сняты обязательные поля 1 и 2
//	accessor-dropped-in-one-message         — no_standard_descriptor_accessor в ListNetworksRequest
//	accessor-dropped-in-two-messages        — и в ListNetworkOperationsRequest
//
// Обязательных полей в контрактах дерева нет (proto3), а форма сообщения buf о них
// своя — контейнер стоит ПЕРВЫМ. Для этой пары и в базу, и в правку добавлен один и
// тот же файл `kacho/cloud/vpc/v1/required_probe.proto` (proto2, сообщение
// RequiredProbe с полями `required string a = 1; required string b = 2;`); правка
// снимает поля.
//
// Пара «один разрыв / он же и сосед» отличается РОВНО ОДНИМ фактом — вторым снятым
// соседом. Фикстуры пары сняты ОТДЕЛЬНЫМИ прогонами, а не вырезаны одна из другой:
// строка одного и того же разрыва в двух прогонах у buf может различаться концом
// диапазона (`end_line`), и вырезанная копия была бы сочинённой.
//
// Имена файла пробы и каталога фикстур НЕ совпадают с одноимённой пробой службы доступа
// (kaname#474): там фикстуры сняты с её контрактов, здесь — с контрактов этого дерева,
// и общий путь в двух стволах гейт парных файлов воркспейса назвал бы парой без решения.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	instanceProto         = "kacho/cloud/compute/v1/instance.proto"
	networkServiceProto   = "kacho/cloud/vpc/v1/network_service.proto"
	networkInterfaceProto = "kacho/cloud/vpc/v1/network_interface.proto"
	requiredProbeProto    = "kacho/cloud/vpc/v1/required_probe.proto"
)

func siblingFindings(t *testing.T, name string) []Finding {
	t.Helper()
	p := filepath.Join("testdata", "neighbour", name+".jsonl")
	f, err := os.Open(p)
	if err != nil {
		t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: фикстура настоящего вывода buf %s не прочитана: %v", p, err)
	}
	defer f.Close()
	got, err := ParseFindings(f)
	if err != nil {
		t.Fatalf("настоящий вывод buf %s не разобран: %v", p, err)
	}
	if len(got) == 0 {
		t.Fatalf("фикстура %s пуста — инъекция беспредметна", p)
	}
	return got
}

func siblingDecl(rule, path, symbol string) Declaration {
	return Declaration{
		Rule: rule, Path: path, Symbol: symbol,
		Reason: "разрыв объявлен пробой kacho#2911 ровно для этого символа",
		Issue:  "kacho#2911",
	}
}

type siblingCase struct {
	name string
	// one — вывод buf с объявленным разрывом, two — тот же разрыв плюс сосед.
	one, two string
	// decls — записи в форме символа, которую называет шапка перечня.
	decls []Declaration
	// sibling — записи, которые назвали бы соседей: на `two` вне перечня обязаны
	// остаться ровно те находки, которые они прощают.
	sibling []Declaration
	// former — те же записи в ПРЕЖНЕЙ форме символа, которой дефект и прощал соседа.
	// nil — прежняя форма совпадает с нынешней, и дефект был только в том, что запись
	// не расходовалась.
	former []Declaration
}

var siblingCases = []siblingCase{
	{
		name: "снятие значения перечисления",
		one:  "enum-value-4-deleted", two: "enum-values-3-4-deleted",
		decls:   []Declaration{siblingDecl("ENUM_VALUE_NO_DELETE", instanceProto, "Status.4")},
		sibling: []Declaration{siblingDecl("ENUM_VALUE_NO_DELETE", instanceProto, "Status.3")},
		former:  []Declaration{siblingDecl("ENUM_VALUE_NO_DELETE", instanceProto, "Status")},
	},
	{
		name: "переименование значения перечисления",
		one:  "enum-value-4-renamed", two: "enum-values-3-4-renamed",
		decls:   []Declaration{siblingDecl("ENUM_VALUE_SAME_NAME", instanceProto, "Status.4")},
		sibling: []Declaration{siblingDecl("ENUM_VALUE_SAME_NAME", instanceProto, "Status.3")},
		former:  []Declaration{siblingDecl("ENUM_VALUE_SAME_NAME", instanceProto, "Status")},
	},
	{
		name: "снятие одноимённого поля соседнего сообщения",
		one:  "field-deleted-in-one-message", two: "field-deleted-in-two-messages",
		decls:   []Declaration{siblingDecl("FIELD_NO_DELETE", networkServiceProto, "ListNetworksRequest.2")},
		sibling: []Declaration{siblingDecl("FIELD_NO_DELETE", networkServiceProto, "ListNetworkOperationsRequest.2")},
		former:  []Declaration{siblingDecl("FIELD_NO_DELETE", networkServiceProto, "page_size")},
	},
	{
		name: "переименование соседнего поля того же сообщения",
		one:  "field-2-renamed", two: "fields-2-3-renamed-in-one-message",
		decls: []Declaration{
			siblingDecl("FIELD_SAME_JSON_NAME", networkServiceProto, "ListNetworksRequest.2"),
			siblingDecl("FIELD_SAME_NAME", networkServiceProto, "ListNetworksRequest.2"),
		},
		sibling: []Declaration{
			siblingDecl("FIELD_SAME_JSON_NAME", networkServiceProto, "ListNetworksRequest.3"),
			siblingDecl("FIELD_SAME_NAME", networkServiceProto, "ListNetworksRequest.3"),
		},
		former: []Declaration{
			siblingDecl("FIELD_SAME_JSON_NAME", networkServiceProto, "ListNetworksRequest"),
			siblingDecl("FIELD_SAME_NAME", networkServiceProto, "ListNetworksRequest"),
		},
	},
	{
		// Прецедент шапки перечня: 44 записи на 121 находку, потому что запись с
		// прежним именем типа прощала смену типа ответа у любого RPC своего файла.
		name: "смена типа ответа соседнего RPC",
		one:  "rpc-response-type-changed-in-one-rpc", two: "rpc-response-type-changed-in-two-rpcs",
		decls:   []Declaration{siblingDecl("RPC_SAME_RESPONSE_TYPE", networkServiceProto, "NetworkService.Create")},
		sibling: []Declaration{siblingDecl("RPC_SAME_RESPONSE_TYPE", networkServiceProto, "NetworkService.Update")},
		former:  []Declaration{siblingDecl("RPC_SAME_RESPONSE_TYPE", networkServiceProto, "corelib.operation.Operation")},
	},
	{
		name: "снятие соседнего зарезервированного имени",
		one:  "reserved-enum-name-deleted", two: "reserved-enum-names-deleted",
		decls:   []Declaration{siblingDecl("RESERVED_ENUM_NO_DELETE", networkInterfaceProto, "Status.FAILED")},
		sibling: []Declaration{siblingDecl("RESERVED_ENUM_NO_DELETE", networkInterfaceProto, "Status.DELETING")},
		former:  []Declaration{siblingDecl("RESERVED_ENUM_NO_DELETE", networkInterfaceProto, "Status")},
	},
	{
		name: "снятие соседнего обязательного поля",
		one:  "required-field-2-deleted", two: "required-fields-1-2-deleted",
		decls: []Declaration{
			siblingDecl("FIELD_NO_DELETE", requiredProbeProto, "RequiredProbe.2"),
			siblingDecl("MESSAGE_SAME_REQUIRED_FIELDS", requiredProbeProto, "RequiredProbe.2"),
		},
		sibling: []Declaration{
			siblingDecl("FIELD_NO_DELETE", requiredProbeProto, "RequiredProbe.1"),
			siblingDecl("MESSAGE_SAME_REQUIRED_FIELDS", requiredProbeProto, "RequiredProbe.1"),
		},
		former: []Declaration{
			siblingDecl("FIELD_NO_DELETE", requiredProbeProto, "RequiredProbe"),
			siblingDecl("MESSAGE_SAME_REQUIRED_FIELDS", requiredProbeProto, "RequiredProbe"),
		},
	},
	{
		name: "параметр сообщения, чьё имя buf не печатает",
		one:  "accessor-dropped-in-one-message", two: "accessor-dropped-in-two-messages",
		decls: []Declaration{siblingDecl("MESSAGE_NO_REMOVE_STANDARD_DESCRIPTOR_ACCESSOR",
			networkServiceProto, "no_standard_descriptor_accessor")},
		sibling: []Declaration{siblingDecl("MESSAGE_NO_REMOVE_STANDARD_DESCRIPTOR_ACCESSOR",
			networkServiceProto, "no_standard_descriptor_accessor")},
	},
}

// TestDeclarationForgivesItsOwnBreakOnly — ЗАКОННЫЙ БЛИЗНЕЦ и ИНЪЕКЦИЯ на одной записи.
//
//   - снятие без записи в перечне — красное;
//   - близнец: объявлен ровно тот разрыв, который наступил, — зелёное;
//   - инъекция: наступил ещё и сосед того же правила в том же файле — сосед обязан
//     остаться вне перечня, названный своим символом, а запись — простить ровно свой
//     разрыв.
func TestDeclarationForgivesItsOwnBreakOnly(t *testing.T) {
	var findingsRead int
	for _, c := range siblingCases {
		t.Run(c.name, func(t *testing.T) {
			one := siblingFindings(t, c.one)
			two := siblingFindings(t, c.two)
			findingsRead += len(one) + len(two)

			if bare := Adjudicate(one, nil); bare.Clean() || len(bare.Undeclared) != len(one) {
				t.Errorf("разрыв без записи в перечне не красен:\n%s", bare.Report())
			}

			twin := Adjudicate(one, c.decls)
			if !twin.Clean() || twin.Matched != len(one) {
				t.Errorf("законный близнец покраснел: объявлен ровно наступивший разрыв\n%s", twin.Report())
			}

			inj := Adjudicate(two, c.decls)
			if inj.Clean() {
				t.Fatalf("соседний разрыв прощён записью, которая его не называет: записей %d, "+
					"сопоставлено %d\n%s", len(c.decls), inj.Matched, inj.Report())
			}
			if inj.Matched != len(c.decls) || len(inj.Expired) != 0 || len(inj.SymbolMismatch) != 0 {
				t.Errorf("запись обязана простить ровно свой разрыв: записей %d, сопоставлено %d, "+
					"истекло %d, символ не совпал %d\n%s", len(c.decls), inj.Matched, len(inj.Expired),
					len(inj.SymbolMismatch), inj.Report())
			}
			// Вне перечня — РОВНО сосед: остаток прощается записями соседа целиком, и
			// ни одна из них не остаётся без предмета.
			rest := Adjudicate(inj.Undeclared, c.sibling)
			if !rest.Clean() || rest.Matched != len(c.sibling) || len(inj.Undeclared) != len(c.sibling) {
				t.Errorf("вне перечня обязан остаться ровно сосед %v:\n%s", c.sibling, inj.Report())
			}
			rep := inj.Report()
			for _, s := range c.sibling {
				if want := "symbol=\"" + s.Symbol + "\""; !strings.Contains(rep, want) {
					t.Errorf("сосед не назван символом, которым его объявляют (%s):\n%s", want, rep)
				}
			}
		})
	}
	t.Logf("перепись: пар %d, находок настоящего вывода buf прочитано %d", len(siblingCases), findingsRead)
	if findingsRead == 0 {
		t.Fatal("не прочитано ни одной находки — проба беспредметна")
	}
}

// TestFormerSymbolFormForgivesNothing — ПРЕЖНЯЯ форма символа (перечисление без номера
// значения, поле без сообщения, сообщение без поля, прежнее имя типа) не называет,
// КАКОЙ разрыв прощён, и потому не прощает ни одного: ни единственного, ни пары. Отчёт
// при этом называет форму, в которой разрыв объявляется, — иначе автор записи остался
// бы с красным и без подсказки.
func TestFormerSymbolFormForgivesNothing(t *testing.T) {
	var checked int
	for _, c := range siblingCases {
		if c.former == nil {
			continue
		}
		checked++
		t.Run(c.name, func(t *testing.T) {
			for _, fx := range []string{c.one, c.two} {
				res := Adjudicate(siblingFindings(t, fx), c.former)
				if res.Clean() || res.Matched != 0 {
					t.Errorf("%s: записи прежней формы простили %d разрывов — символ не называет, "+
						"какой именно разрыв прощён\n%s", fx, res.Matched, res.Report())
				}
				if want := "symbol=\"" + c.decls[0].Symbol + "\""; !strings.Contains(res.Report(), want) {
					t.Errorf("%s: отчёт не называет форму символа, в которой разрыв объявляется (%s):\n%s",
						fx, want, res.Report())
				}
			}
		})
	}
	t.Logf("перепись: пар с прежней формой символа %d из %d", checked, len(siblingCases))
	if checked == 0 {
		t.Fatal("ни у одной пары нет прежней формы — проба беспредметна")
	}
}

// TestEachDeclarationForgivesExactlyOneBreak — расходование записи. Где buf
// объемлющего символа не печатает, два разрыва неразличимы, и каждый объявляется СВОЕЙ
// записью: две одинаковые записи прощают ровно два разрыва (законный близнец), одна —
// ровно один (инъекция).
func TestEachDeclarationForgivesExactlyOneBreak(t *testing.T) {
	two := siblingFindings(t, "accessor-dropped-in-two-messages")
	d := siblingDecl("MESSAGE_NO_REMOVE_STANDARD_DESCRIPTOR_ACCESSOR", networkServiceProto,
		"no_standard_descriptor_accessor")

	both := Adjudicate(two, []Declaration{d, d})
	if !both.Clean() || both.Matched != 2 {
		t.Errorf("две записи на два неразличимых разрыва обязаны простить оба:\n%s", both.Report())
	}

	single := Adjudicate(two, []Declaration{d})
	if single.Clean() || single.Matched != 1 || len(single.Undeclared) != 1 {
		t.Fatalf("одна запись обязана простить ровно один разрыв из двух:\n%s", single.Report())
	}
	// ТЕКСТ НАХОДКИ — часть свойства: символ второго разрыва совпадает с записью
	// перечня, и без объяснения читатель увидел бы «необъявленный» у объявленного
	// символа и искал бы опечатку.
	if rep := single.Report(); !strings.Contains(rep, "запись прощает ровно один разрыв") {
		t.Errorf("находка не объясняет, почему объявленный символ остался вне перечня:\n%s", rep)
	}
}

// TestRepeatedDeclarationIsNamedAsRepeat — повтор записи при ОДНОМ разрыве красен, и
// его текст не лжёт: разрыв есть, его простила другая запись. «Разрыва больше нет» здесь
// было бы неправдой и увело бы читателя проверять базу сравнения.
func TestRepeatedDeclarationIsNamedAsRepeat(t *testing.T) {
	one := siblingFindings(t, "enum-value-4-deleted")
	d := siblingDecl("ENUM_VALUE_NO_DELETE", instanceProto, "Status.4")

	res := Adjudicate(one, []Declaration{d, d})
	if res.Clean() || res.Matched != 1 || len(res.Expired) != 1 {
		t.Fatalf("повтор записи обязан краснеть ровно одной лишней записью:\n%s", res.Report())
	}
	rep := res.Report()
	if !strings.Contains(rep, "уже простила другая запись") {
		t.Errorf("повтор не назван повтором:\n%s", rep)
	}
	if strings.Contains(rep, "разрыва больше нет") {
		t.Errorf("о повторе сказано «разрыва больше нет», а разрыв есть:\n%s", rep)
	}

	// Законный близнец: запись, чьего разрыва действительно нет, по-прежнему названа
	// истёкшей тем же текстом, что и до починки.
	gone := Adjudicate(nil, []Declaration{d})
	if !strings.Contains(gone.Report(), "разрыва больше нет") {
		t.Errorf("истёкшая запись потеряла свой текст:\n%s", gone.Report())
	}
}
