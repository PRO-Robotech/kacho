// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package proxy

import (
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/contractroot"
)

// Продукт называет себя ДВУМЯ корнями контракта одновременно: платформа —
// `kacho.cloud.*`, служба доступа после KAN-PKG-1 — `kaname.cloud.*`. Пока
// допустимый корень был литералом, он был ОДИН, и второй корень не
// маршрутизировался вовсе.
//
// Литералов этих в прод-коде края было ДВА — в резолвере и в функции вывода
// ключа бэкенда, — и порядок между ними несущий: страж резолвера замыкается
// раньше, чем зовётся второй. Значит перевод одного из двух оставил бы путь
// мёртвым, а прогон — зелёным на той половине, которая до второго не доходит.
// Поэтому проверяется не «работает новый корень», а то, что оба стража читают
// ОДНО объявленное множество.

func TestContractRoots_DeclaredSetCarriesBothProductRoots(t *testing.T) {
	if len(ContractRoots) == 0 {
		t.Fatal("перечень допустимых корней пуст: край не маршрутизировал бы ничего")
	}
	for _, want := range []string{"kacho.cloud.", "kaname.cloud."} {
		if !hasRoot(want) {
			t.Errorf("корень %q не объявлен в перечне %v", want, ContractRoots)
		}
	}
	t.Logf("перепись: корней объявлено %d — %v", len(ContractRoots), ContractRoots)
}

// Позиционный вывод ключа бэкенда — предмет замера §3.1 приёмки: трёхсегментный
// корень дал бы ключ `v1`, которого в карте бэкендов нет вовсе, и метод
// разрешался бы в никуда, оставаясь в перечне разрешённых. Утверждается ОБЕ
// стороны: свой корень даёт домен, чужой не даёт ничего.
func TestRoutableDomain_DerivesTheSameKeyUnderBothRoots(t *testing.T) {
	for _, tc := range []struct {
		method string
		want   string
		ok     bool
	}{
		{"/kacho.cloud.vpc.v1.NetworkService/Get", "vpc", true},
		{"/kaname.cloud.iam.v1.AccountService/Get", "iam", true},
		// Прежнее написание службы доступа: корень продукта сменился, и
		// вызывающий прежней сборки не должен резолвиться никуда.
		{"/kaname.cloud.iam.v1.AccountService/Get", "iam", true},
		// Чужой корень — не наш словарь.
		{"/example.cloud.iam.v1.AccountService/Get", "", false},
		// Трёхсегментная форма: ключ выводиться НЕ должен.
		{"/kaname.iam.v1.AccountService/Get", "", false},
		// Пакет фундамента: ключ — ВТОРОЙ сегмент, то же имя, каким служба
		// операций называлась облачным доменом до kacho#2601.
		{"/corelib.operation.OperationService/Get", "operation", true},
		// Граница корня — точка: чужой корень, начинающийся тем же словом, не наш.
		{"/corelibx.operation.OperationService/Get", "", false},
		// Корень фундамента без пакета: ключа нет.
		{"/corelib.OperationService/Get", "", false},
	} {
		got, ok := RoutableDomain(tc.method)
		if ok != tc.ok || got != tc.want {
			t.Errorf("RoutableDomain(%q) = (%q, %v), ожидалось (%q, %v)",
				tc.method, got, ok, tc.want, tc.ok)
		}
	}
}

// Пустой перечень означает «никого», а не «не сужаем». Это перечень
// РАЗРЕШАЮЩЕГО вида (тот же разбор, что у перечня владельцев журнала
// подписки), и безопасен он by construction только при таком чтении.
func TestContractRoots_EmptySetMeansNobody(t *testing.T) {
	saved := ContractRoots
	t.Cleanup(func() { ContractRoots = saved })

	ContractRoots = nil
	if _, ok := RoutableDomain("/kacho.cloud.vpc.v1.NetworkService/Get"); ok {
		t.Error("на пустом перечне метод резолвится — пустое прочитано как «не сужаем»")
	}
	resolve := Resolver(Backends{"vpc": nil})
	if _, _, ok := resolve("/kacho.cloud.vpc.v1.NetworkService/Get"); ok {
		t.Error("на пустом перечне резолвер пропускает вызов")
	}
}

// Положительный контроль к предыдущему: на НЕпустом перечне тот же вызов
// проходит стража корня. Без него отрицание зеленело бы на резолвере,
// отвергающем всё.
func TestContractRoots_NonEmptySetStillAdmits(t *testing.T) {
	if _, ok := RoutableDomain("/kacho.cloud.vpc.v1.NetworkService/Get"); !ok {
		t.Error("на объявленном перечне свой корень не проходит — отрицание выше вакуумно")
	}
}

func hasRoot(want string) bool {
	for _, r := range ContractRoots {
		if strings.EqualFold(r, want) {
			return true
		}
	}
	return false
}

// TestOldContractName_ResolvesNowhere — KAN-PKG-04. Прежнее полное имя службы
// доступа не резолвится НИ ПРИ КАКОМ состоянии карты бэкендов.
//
// Отказ приходит НЕ от стража корня: корень `kacho.cloud.` остаётся законным —
// под ним живёт вся платформа. Отказывает перечень разрешённых методов, потому
// что записи прежнего написания сняты вместе с контрактом. Это надо утверждать
// отдельно: иначе «окна двух имён нет» держалось бы на догадке о том, какой
// именно страж сработал, а сработать он мог по совсем другой причине.
func TestOldContractName_ResolvesNowhere(t *testing.T) {
	// Карта бэкендов НЕ пуста и содержит ключ, который вывела бы форма имени, —
	// иначе отказ был бы объясним отсутствием бэкенда, а не снятым именем.
	resolve := Resolver(Backends{"iam": nil})

	const old = "/kacho.cloud.iam.v1.AccountService/Get"
	if _, _, ok := resolve(old); ok {
		t.Errorf("прежнее полное имя %q резолвится — окно двух имён открыто", old)
	}
	// Положительный контроль: под тем же корнем живая платформенная служба
	// резолвится. Без него отрицание зеленело бы на резолвере, который снял
	// корень `kacho.cloud.` целиком.
	if _, ok := RoutableDomain("/kacho.cloud.vpc.v1.NetworkService/Get"); !ok {
		t.Error("корень платформы перестал резолвиться — снято больше, чем предмет")
	}
	// И зеркально: форма имени прежней записи по-прежнему РАЗБИРАЕТСЯ (домен
	// выводится), то есть отказ даёт именно перечень, а не форма.
	if d, ok := RoutableDomain(old); !ok || d != "iam" {
		t.Errorf("прежнее имя перестало разбираться (%q, %v) — тогда утверждение выше "+
			"доказывает не снятие записи, а поломку разбора", d, ok)
	}
}

// TestFoundationRoots_AreDeclaredContractRoots — объявление корней фундамента в
// крае не расходится со словарём корней: запись, которой в словаре нет, открыла
// бы маршрут корню, которого дерево не знает.
func TestFoundationRoots_AreDeclaredContractRoots(t *testing.T) {
	if len(FoundationRoots) == 0 {
		t.Fatal("корней фундамента не объявлено: служба операций под именем фундамента не маршрутизировалась бы")
	}
	declared := map[string]bool{}
	for _, r := range contractroot.Roots {
		declared[r] = true
	}
	for _, r := range FoundationRoots {
		if !declared[r] {
			t.Errorf("корень фундамента %q не объявлен словарём корней %v", r, contractroot.Roots)
		}
	}
	t.Logf("перепись: корней фундамента %d — %v; словарь %v", len(FoundationRoots), FoundationRoots, contractroot.Roots)
}
