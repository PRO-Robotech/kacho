// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// migrationnameproducer_injection_test.go — доказательство в ОБЕ стороны.
//
// (а) верни вызов — разбор краснеет и НАЗЫВАЕТ координату (файл и строку);
// (б) поставь рядом ЗАКОННУЮ конструкцию той же формы — разбор молчит.
//
// Законный близнец здесь обязателен вдвойне: слова `goose.Create` встречаются в
// этом дереве в прозе — в шапке самого гейта, в тексте его отказа, в разборе
// решения. Текстовый предикат краснел бы на собственном объяснении.
package repohygiene

import (
	"go/token"
	"strings"
	"testing"
)

// srcWithCall — исходник, который ВЫЗЫВАЕТ производителя имени.
const srcWithCall = `package widget

import "github.com/pressly/goose/v3"

func createMigration(dir, name string) error {
	return goose.Create(nil, dir, name, "sql")
}
`

// srcLegit — законный близнец ТОЙ ЖЕ формы: goose импортирован и используется,
// имя `Create` присутствует в комментарии, в строковом литерале и у ЧУЖОГО
// пакета. Ни одно из этого производителем имени не является.
const srcLegit = `package widget

import (
	"github.com/pressly/goose/v3"

	"example.com/widget/store"
)

// goose.Create здесь только в прозе: он выдаёт метку времени, поэтому вызывать
// его нельзя.
const banned = "goose.Create"

func up(dir string) error {
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	_ = banned
	_, _ = store.Create(dir)
	return goose.Up(nil, dir)
}
`

// srcAliased — тот же вызов под ПСЕВДОНИМОМ импорта. Запрет, который обходится
// переименованием импорта, ничего не запрещает.
const srcAliased = `package widget

import g "github.com/pressly/goose/v3"

func createMigration(dir, name string) error {
	return g.Create(nil, dir, name, "sql")
}
`

// srcBlank — слепой импорт (регистрация драйвера). Вызовов не порождает и
// находкой быть не должен, иначе гейт краснел бы на половине дерева.
const srcBlank = `package widget

import _ "github.com/pressly/goose/v3"

func nothing() {}
`

func TestMigrationNameProducer_ProvenByInjection(t *testing.T) {
	scan := func(t *testing.T, name, src string) ([]nameProducerFinding, bool, int) {
		t.Helper()
		got, imports, checked, err := scanNameProducers(token.NewFileSet(), name, src)
		if err != nil {
			t.Fatalf("разбор %s: %v", name, err)
		}
		return got, imports, checked
	}

	t.Run("вызов возвращён — краснеет и называет координату", func(t *testing.T) {
		got, imports, _ := scan(t, "widget.go", srcWithCall)
		if !imports {
			t.Fatal("файл импортирует goose, а разбор этого не увидел")
		}
		if len(got) != 1 {
			t.Fatalf("ожидалась 1 находка, получено %d", len(got))
		}
		if got[0].File != "widget.go" || got[0].Line != 6 {
			t.Fatalf("находка не называет координату: %+v", got[0])
		}
		if !strings.Contains(got[0].String(), "goose.Create") {
			t.Fatalf("текст находки не называет вызов: %s", got[0].String())
		}
	})

	t.Run("псевдоним импорта не спасает — краснеет", func(t *testing.T) {
		got, _, _ := scan(t, "aliased.go", srcAliased)
		if len(got) != 1 {
			t.Fatalf("вызов под псевдонимом пропущен: находок %d", len(got))
		}
		if !strings.Contains(got[0].String(), "g.Create") {
			t.Fatalf("находка называет не тот вызов: %s", got[0].String())
		}
	})

	t.Run("проза, литерал и чужой Create — разбор молчит", func(t *testing.T) {
		got, imports, checked := scan(t, "legit.go", srcLegit)
		if len(got) != 0 {
			t.Fatalf("ложное срабатывание на законной конструкции: %+v", got)
		}
		if !imports {
			t.Fatal("законный близнец обязан импортировать goose, иначе он не той же формы")
		}
		if checked == 0 {
			t.Fatal("в законном близнеце обязаны быть вызовы goose.*, иначе он ничего " +
				"не различает: молчание объяснялось бы отсутствием вызовов вовсе")
		}
	})

	t.Run("слепой импорт — не находка", func(t *testing.T) {
		got, imports, _ := scan(t, "blank.go", srcBlank)
		if len(got) != 0 {
			t.Fatalf("слепой импорт принят за производителя имени: %+v", got)
		}
		if !imports {
			t.Fatal("слепой импорт goose обязан считаться импортом — перепись иначе занижена")
		}
	})
}
