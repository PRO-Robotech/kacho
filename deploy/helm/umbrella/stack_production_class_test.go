// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// stack_production_class_test.go — какой стек таблицы БОЕВОЙ: чтение того, что
// стек объявляет о себе, и страж, что dev-классом читается только стек,
// названный стендом разработки.
//
// Прежде эти помощники жили в пробе транспорта административного перехода к
// поставщику личности (admin_hop_transport_test.go). Переход снят вместе с
// поставщиком (#1276): у перехода не осталось ни слушателя, ни потребителя, и
// проба его транспорта снята вместе со своим предметом. Классификация стека
// осталась — её читают живые пробы пакета (login_console_test.go,
// edge_public_listener_mtls_test.go), и её страж остался вместе с ней.
//
// WHY THIS READS DECLARATIONS. The contract is what the profiles DECLARE, it
// needs no chart dependencies, and it therefore can never skip (the same reason
// as gateway/deploy/revocation_endpoint_test.go). The umbrella's dependencies
// are not all vendored, so a render-based check here would be skipped on every
// machine that has not run `helm dep build` — which is exactly when it would be
// needed.
//
// WHY THE EXEMPTION IS DERIVED, NOT LISTED. Whether a stack is production-class
// is read from the stack's OWN declaration (the gateway's environment label and
// iam's authn mode), not from a hard-coded list of names here. A hard-coded list
// goes stale silently the moment a stand changes posture, and the stale entry
// then exempts the very stack that just started needing the check.
package umbrella_test

import (
	"strings"
	"testing"
)

// resolveStackAt merges a stack's profiles in order (the way helm does) and reads
// the string at an ABSOLUTE path in the merged tree.
//
// Its neighbour resolveStack is rooted at the gateway's own sub-tree; iam's side
// is declared under a different chart, so the absolute form is needed to read it
// without duplicating the merge.
func resolveStackAt(t *testing.T, stack []string, path ...string) (string, bool) {
	t.Helper()
	merged := map[string]any{}
	for _, profile := range stack {
		merged = mergeInto(merged, umbrellaValues(t, profile))
	}
	var cur any = merged
	for _, key := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return "", false
		}
		if cur, ok = m[key]; !ok {
			return "", false
		}
	}
	s, ok := cur.(string)
	return s, ok && strings.TrimSpace(s) != ""
}

// devClassEnvLabels — the labels the gateway's own boot guard treats as
// dev-class. Mirrored from validateProductionRevocationConfig; every other value,
// INCLUDING an empty one, is production-class.
var devClassEnvLabels = map[string]bool{"dev": true, "local": true, "test": true}

// stackIsProductionClass reports whether a stack deploys the production security
// posture, by reading what the stack declares about itself.
//
// Either process being production-class makes the stack production-class: a
// stand where one of them refuses to start is not a working stand.
func stackIsProductionClass(t *testing.T, stack []string) bool {
	t.Helper()
	if label, ok := resolveStack(t, stack, "appEnv"); ok && !devClassEnvLabels[strings.ToLower(strings.TrimSpace(label))] {
		return true
	}
	// Посадка адресуется каноном `kaname.authMode` в корне значений сервиса;
	// прежний адрес (`config.authn.mode`) читается следом, потому что шаблон чарта
	// его тоже пока принимает.
	//
	// Клауза стоит ВТОРОЙ и сегодня ничего не решает — классификацию несёт `appEnv`
	// выше. Именно поэтому её и надо было чинить: отбор по переехавшему ключу не
	// краснеет, он тихо перестаёт находить предмет, а прикрытый соседней клаузой —
	// не краснеет даже переписью. Стек без `appEnv`, объявивший посадку, ушёл бы в
	// Skip как dev-class.
	mode, ok := resolveStackAt(t, stack, "kaname", "authMode")
	if !ok {
		mode, ok = resolveStackAt(t, stack, "kaname", "config", "authn", "mode")
	}
	return ok && strings.HasPrefix(strings.TrimSpace(mode), "production")
}

// resolveStackBoolAt reads a boolean at an ABSOLUTE path in the merged stack.
// Its neighbour resolveStackBool is rooted at the gateway's own sub-tree; this
// one reads an ABSOLUTE path in the merged tree.
func resolveStackBoolAt(t *testing.T, stack []string, path ...string) (bool, bool) {
	t.Helper()
	merged := map[string]any{}
	for _, profile := range stack {
		merged = mergeInto(merged, umbrellaValues(t, profile))
	}
	var cur any = merged
	for _, key := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return false, false
		}
		if cur, ok = m[key]; !ok {
			return false, false
		}
	}
	b, ok := cur.(bool)
	return b, ok
}

// devClassStackNames — стеки, которым РАЗРЕШЕНО быть dev-класса. Перечень
// закрыт и назван по имени, а не выведен из состава: именно вывод из состава и
// подвёл.
var devClassStackNames = map[string]bool{"dev": true}

// TestStacks_OnlyNamedDevStacksAreDevClass — стек, не объявленный dev по имени,
// обязан читаться боевым.
//
// # Предмет
//
// Проверки, отбирающие стеки этим классом (консоль входа, транспорт края),
// условны: они спрашивают «стек объявил боевую посадку?» и пропускают dev-класс. Условие правильное — dev-стенд
// вправе не нести требований, — но оно опирается на СОСТАВ стека, а состав
// собирается здесь же, рукой.
//
// Так и вышло: накладка образов `values.prorobotech.yaml` описывает только
// образы и наследует безопасность у слоя под собой, о чём прямо говорит её
// собственная шапка («средняя строка НЕ опциональна»). В наборе она была
// собрана без этого слоя — и стек, которым продукт поднимают на управляемом
// кластере, читался dev-классом и уходил из-под проверок транспорта.
// Пропуск при этом выглядел законным и печатал «dev-class by its own
// declaration», хотя накладка не объявляет посадку ВООБЩЕ: «объявил dev» и «не
// объявил ничего» — разные состояния, и первое здесь было выводом набора, а не
// заявлением файла.
//
// # Почему страж по ИМЕНИ, а не по составу
//
// Свойство, которое нужно удержать, — «пропуск полагается только тому, кто
// заявлен стендом разработки». Имя стека заявляет намерение автора набора;
// состав — то, что из намерения получилось. Сверять получившееся с самим собой
// бессмысленно, поэтому страж читает намерение и требует, чтобы состав ему
// соответствовал.
func TestStacks_OnlyNamedDevStacksAreDevClass(t *testing.T) {
	if len(deployableStacks(t)) == 0 {
		t.Fatal("набор стеков пуст — «все боевые» здесь означало бы «ни одного не смотрели»")
	}
	devFound := 0
	stacks := deployableStacks(t)
	for _, name := range sortedStackNames(stacks) {
		stack := stacks[name]
		production := stackIsProductionClass(t, stack)
		if devClassStackNames[name] {
			devFound++
			if production {
				t.Errorf("%s назван стендом разработки, но состав объявляет боевую посадку — "+
					"перечень devClassStackNames пережил свой предмет", name)
			}
			continue
		}
		if !production {
			t.Errorf("%s читается dev-классом, хотя стендом разработки не назван: проверки, "+
				"отбирающие стеки боевым классом, его ПРОПУСТЯТ, и пропуск будет выглядеть "+
				"законным. Составьте стек так, как предписывает шапка его собственной накладки "+
				"(слой с посадкой не опционален), либо внесите имя в devClassStackNames — "+
				"осознанно и с причиной", name)
		}
	}
	if devFound == 0 {
		t.Error("ни один стек перечня devClassStackNames не встретился в наборе — перечню " +
			"больше нечего разрешать, и он остался бы верным при любом дереве")
	}
	t.Logf("осмотрено: стеков %d, из них разрешённых dev-класса %d", len(deployableStacks(t)), devFound)
}
