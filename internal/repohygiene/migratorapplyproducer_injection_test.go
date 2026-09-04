// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// migratorapplyproducer_injection_test.go — доказательство способности гейта упасть.
//
// Гейт на дереве зелёный, и зелёный сам по себе не доказывает ничего: ровно так же
// он выглядел бы, если бы вердикт не умел краснеть. Решающая часть вынесена в
// чистый предикат и проверяется подставными входами — каждый случай, который он
// ОБЯЗАН поймать, и каждый, который обязан пропустить.
//
// Инъекция трогает РОВНО одну ось за раз. Случай, ломающий заодно соседнее
// свойство, доказательством не является: красное пришло бы от соседа, а
// проверяемый предикат мог бы при этом быть вакуумным и не показать этого ничем.
package repohygiene

import (
	"strings"
	"testing"
)

func TestMigratorApplyProducerJudgeFiresAndStaysSilent(t *testing.T) {
	// Дерево, на котором свойство ДЕРЖИТСЯ. Каждый случай ниже отличается от него
	// ровно одной осью.
	migrators := []string{"services/geo/cmd/migrator", "services/vpc/cmd/migrator"}
	named := []string{"pkg/dropguard", migratorApplyProofPkg, "pkg/subscription"}

	t.Run("молчит: доказательство есть и производитель его называет", func(t *testing.T) {
		if f := judgeMigratorApplyProducer(migrators, 2, named); len(f) != 0 {
			t.Fatalf("гейт краснеет на дереве, где свойство держится: %v", f)
		}
	})

	t.Run("краснеет: проба есть, производителя нет — находка #1637", func(t *testing.T) {
		without := []string{"pkg/dropguard", "pkg/subscription"}
		f := judgeMigratorApplyProducer(migrators, 2, without)
		if len(f) != 1 {
			t.Fatalf("ожидалась ровно одна находка, получено %d: %v", len(f), f)
		}
		// Находка обязана называть ПРИЧИНУ, а не симптом: «проба без производителя»,
		// а не «пакета нет в списке». Находка, называющая симптом, посылает читателя
		// искать не там, и на неё тратят прогон, прежде чем снять гейт как непонятный.
		if !strings.Contains(f[0], migratorApplyProofPkg) ||
			!strings.Contains(f[0], "нет производителя") {
			t.Fatalf("находка не называет ни пакет, ни причину: %q", f[0])
		}
	})

	t.Run("краснеет: доказательства нет вовсе", func(t *testing.T) {
		without := []string{"pkg/dropguard"}
		f := judgeMigratorApplyProducer(migrators, 0, without)
		if len(f) != 1 || !strings.Contains(f[0], "доказательства наката нет вовсе") {
			t.Fatalf("исчезнувшее доказательство не найдено: %v", f)
		}
		// Находка обязана перечислить точки наката: без них «нет доказательства»
		// не говорит, чего именно оно касается.
		if !strings.Contains(f[0], "services/geo/cmd/migrator") {
			t.Fatalf("находка не назвала точки наката: %q", f[0])
		}
	})

	t.Run("краснеет: производитель назван, а проб нет — запись зовёт пустоту", func(t *testing.T) {
		f := judgeMigratorApplyProducer(migrators, 0, named)
		if len(f) != 1 || !strings.Contains(f[0], "зовёт пустоту") {
			t.Fatalf("запись, зовущая несуществующий пакет, не найдена: %v", f)
		}
	})

	t.Run("краснеет: обход пуст — вердикт беспредметен", func(t *testing.T) {
		f := judgeMigratorApplyProducer(nil, 2, []string{"pkg/dropguard"})
		if len(f) != 1 || !strings.Contains(f[0], "обход пуст") {
			t.Fatalf("пустой обход принят за свойство: %v", f)
		}
	})

	t.Run("краснеет ДВАЖДЫ: обход пуст, а запись производителя жива", func(t *testing.T) {
		// Самоистечение: точек наката не стало — запись обязана уйти следом, иначе
		// она достанется следующему как слепая зона.
		f := judgeMigratorApplyProducer(nil, 2, named)
		if len(f) != 2 {
			t.Fatalf("ожидались две находки (пустой обход + пережившая запись), получено %d: %v", len(f), f)
		}
		if !strings.Contains(f[1], "пережила свой предмет") {
			t.Fatalf("вторая находка не про переживший предмет: %q", f[1])
		}
	})

	t.Run("молчит: чужие записи перечня предметом гейта не являются", func(t *testing.T) {
		// Контроль узости: гейт судит СВОЮ запись, а не длину перечня. Без него он
		// краснел бы на всяком изменении соседних записей.
		other := []string{"pkg/subscription", migratorApplyProofPkg, "gateway/internal/idempotencypg"}
		if f := judgeMigratorApplyProducer(migrators, 1, other); len(f) != 0 {
			t.Fatalf("гейт вмешался в чужие записи перечня: %v", f)
		}
	})
}
