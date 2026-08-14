// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package toproto_test

import (
	"sort"
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
)

// network_interface_status_contract_test.go — статус интерфейса НЕ претендует на
// то, чего не производит (APPLY-01).
//
// # Предмет
//
// Перечисление `NetworkInterface.Status` объявляло шесть значений, а
// производилось два: `AVAILABLE` ставит создание и отвязка, `ACTIVE` — привязка.
// У `PROVISIONING`, `FAILED` и `DELETING` производителя в не-тестовом дереве нет
// НИ ОДНОГО, при том что их комментарии заявляли программирование датаплейна —
// то есть ровно тот предмет, на который отвечает поле состояния применения.
//
// Контракт, отвечающий на один вопрос двумя способами, из которых один мёртв,
// заставляет арендатора выбирать между ними без правильного ответа. Значения
// сняты; номера и имена зарезервированы, чтобы слот не вернулся под другой
// смысл.
//
// # Почему дескриптор, а не поиск по .proto
//
// Текстовый предикат нашёл бы имя в комментарии — в том числе в комментарии,
// объясняющем само снятие, — и объявил бы значение живым. Дескриптор собран из
// дерева и содержит ровно то, что уедет на провод.
//
// # Положительный контроль обязателен
//
// Без него «трёх значений нет» неотличимо от «дескриптор не прочитан»: пустой
// обход даёт тот же зелёный, что и правильный.

// retiredNICStatusNames — имена, снятые с контракта.
//
// Перечень выписан, а не выведен: он и есть предмет утверждения. Резервирование
// проверяется по нему же — незарезервированное имя открывает слот под другой
// смысл, и следующий читатель прочтёт старое значение как новое.
var retiredNICStatusNames = []string{"PROVISIONING", "FAILED", "DELETING"}

// retiredNICStatusNumbers — номера, снятые с контракта.
var retiredNICStatusNumbers = []protoreflect.EnumNumber{1, 4, 5}

// TestNetworkInterfaceStatusClaimsOnlyWhatItProduces — состав перечисления равен
// множеству значений, у которых есть производитель.
func TestNetworkInterfaceStatusClaimsOnlyWhatItProduces(t *testing.T) {
	ed := vpcv1.NetworkInterface_STATUS_UNSPECIFIED.Descriptor()

	var got []string
	values := ed.Values()
	for i := 0; i < values.Len(); i++ {
		got = append(got, string(values.Get(i).Name()))
	}
	sort.Strings(got)

	want := []string{"ACTIVE", "AVAILABLE", "STATUS_UNSPECIFIED"}
	if len(got) != len(want) {
		t.Fatalf("состав NetworkInterface.Status: %v, ожидалось %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("состав NetworkInterface.Status: %v, ожидалось %v", got, want)
		}
	}
	t.Logf("осмотрено %d значение(й) перечисления NetworkInterface.Status", values.Len())

	// ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ: обход действительно читает дескриптор. Без него
	// «снятых значений нет» было бы верно и на непрочитанном дескрипторе.
	if values.ByName("ACTIVE") == nil {
		t.Fatal("обход не нашёл живое значение ACTIVE — проверка читает не дескриптор")
	}
}

// TestRetiredNetworkInterfaceStatusSlotsStayShut — снятые номера и имена
// зарезервированы.
//
// Отдельная проба, а не ветка предыдущей: «значения нет» и «слот закрыт» —
// разные утверждения, и первое выполняется даже тогда, когда номер свободен для
// повторного использования под другой смысл.
func TestRetiredNetworkInterfaceStatusSlotsStayShut(t *testing.T) {
	ed := vpcv1.NetworkInterface_STATUS_UNSPECIFIED.Descriptor()

	reservedNames := map[string]struct{}{}
	rn := ed.ReservedNames()
	for i := 0; i < rn.Len(); i++ {
		reservedNames[string(rn.Get(i))] = struct{}{}
	}
	for _, name := range retiredNICStatusNames {
		if _, ok := reservedNames[name]; !ok {
			t.Errorf("имя %q не зарезервировано — слот открыт под другой смысл", name)
		}
	}

	ranges := ed.ReservedRanges()
	covered := func(n protoreflect.EnumNumber) bool {
		for i := 0; i < ranges.Len(); i++ {
			r := ranges.Get(i)
			if n >= r[0] && n <= r[1] {
				return true
			}
		}
		return false
	}
	for _, num := range retiredNICStatusNumbers {
		if !covered(num) {
			t.Errorf("номер %d не зарезервирован — слот открыт под другой смысл", num)
		}
	}

	t.Logf("осмотрено %d зарезервированное(ых) имя(ён) и %d диапазон(ов) номеров",
		rn.Len(), ranges.Len())

	// ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ: живой номер НЕ должен попадать в резерв. Иначе
	// «всё зарезервировано» прошло бы на перечислении, закрытом целиком.
	if covered(protoreflect.EnumNumber(vpcv1.NetworkInterface_ACTIVE.Number())) {
		t.Fatal("номер живого значения ACTIVE объявлен зарезервированным — перечисление противоречит себе")
	}
}
