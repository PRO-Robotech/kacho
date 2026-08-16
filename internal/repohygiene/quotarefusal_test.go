// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// quotarefusal_test.go — гейт дерева: отказ учёта производится из ОДНОГО
// источника у всех владельцев.
//
// ЗАЧЕМ ОН СУЩЕСТВУЕТ. Тон и форма отказа — часть контракта: арендатор читает
// текст, клиент ключуется на SQLSTATE. У каждого владельца своя база, поэтому
// физически функция обязана быть у каждого своя — «одно место» недостижимо by
// construction. Достижим один ИСТОЧНИК, и держать его может только проверка:
// пять копий уже разошлись однажды (два владельца называли носителя, три
// вписывали слово «project» литералом), и увидеть это можно было, только положив
// копии рядом, чего не делает ни обзор изменения, ни прогон.
//
// ЧТО СЧИТАЕТСЯ НАХОДКОЙ. Файл миграции владельца, отличающийся от рендера
// шаблона хотя бы байтом, — в любую сторону: правка копии руками и забытая
// перегенерация после правки шаблона выглядят для гейта одинаково, и это верно,
// потому что последствие у них одно.
package repohygiene

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/quota"
)

// TestQuotaRefusalIsRenderedFromOneSource — файл каждого владельца совпадает с
// рендером шаблона побайтово.
func TestQuotaRefusalIsRenderedFromOneSource(t *testing.T) {
	root := repoRoot(t)

	owners := quota.RefusalOwners()
	if len(owners) == 0 {
		t.Fatal("перечень владельцев пуст: гейту нечего рассматривать, " +
			"и его зелёный цвет означал бы отсутствие предмета, а не отсутствие находок")
	}

	checked := 0
	var findings []string
	for _, o := range owners {
		want, err := quota.RenderRefusalMigration(o)
		if err != nil {
			t.Fatalf("рендер для %s: %v", o.Service, err)
		}

		path := filepath.Join(root, "services", o.Service, "internal", "migrations", o.Migration)
		got, err := os.ReadFile(path)
		if err != nil {
			findings = append(findings, o.Service+": файла миграции нет — "+
				"владелец объявлен, а отказ ему не сгенерирован ("+o.Migration+")")
			continue
		}
		checked++

		if string(got) != want {
			findings = append(findings, o.Service+": "+o.Migration+
				" разошёлся с шаблоном — "+firstDiff(string(got), want)+
				". Перегенерировать: go run ./tools/quota-refusal-migration")
		}
	}

	t.Logf("перепись: владельцев %d, файлов прочитано %d", len(owners), checked)

	if checked == 0 {
		t.Fatal("не прочитано ни одного файла: «ноль расхождений» здесь означало бы " +
			"«ноль прочитанного», а это разные утверждения")
	}
	if len(findings) > 0 {
		t.Fatalf("отказ учёта разошёлся с единым источником (%d):\n  %s",
			len(findings), strings.Join(findings, "\n  "))
	}
}

// TestQuotaRefusalGate_NamesTheLineItDisagreesOn — доказательство того, что
// сравнение выше способно упасть И назвать координату.
//
// Без него «расхождений нет» неотличимо от «сравнение всегда согласно»: гейт,
// сличающий строку саму с собой, зеленел бы при любом состоянии дерева.
func TestQuotaRefusalGate_NamesTheLineItDisagreesOn(t *testing.T) {
	// Инъекция: подан текст, отличающийся ровно одной строкой.
	base := "первая\nвторая\nтретья\n"
	drifted := "первая\nВТОРАЯ\nтретья\n"

	got := firstDiff(drifted, base)
	if !strings.Contains(got, "строка 2") {
		t.Fatalf("расхождение обязано называть НОМЕР строки, получено: %q", got)
	}

	// Законный близнец: одинаковый текст расхождением не объявляется.
	if same := firstDiff(base, base); same != "" {
		t.Fatalf("одинаковый текст не может быть расхождением, получено: %q", same)
	}

	// И длина тоже считается расхождением: текст, оборванный на середине,
	// совпадает с началом эталона и обязан быть найден.
	if cut := firstDiff("первая\n", base); cut == "" {
		t.Fatal("оборванный файл обязан быть расхождением, а не совпадением префикса")
	}
}

// firstDiff называет первую расходящуюся строку.
//
// Именно строку, а не «файлы различаются»: вердикт без координаты заставляет
// читателя сличать два файла глазами, и на файле в полторы сотни строк это ровно
// та работа, ради устранения которой гейт и написан.
func firstDiff(got, want string) string {
	if got == want {
		return ""
	}
	g := strings.Split(got, "\n")
	w := strings.Split(want, "\n")
	for i := 0; i < len(g) && i < len(w); i++ {
		if g[i] != w[i] {
			return "строка " + strconv.Itoa(i+1) + ": в файле " + quoteShort(g[i]) +
				", в шаблоне " + quoteShort(w[i])
		}
	}
	return "строка " + strconv.Itoa(minInt(len(g), len(w))+1) + ": в файле строк " +
		strconv.Itoa(len(g)) + ", в шаблоне " + strconv.Itoa(len(w))
}

func quoteShort(s string) string {
	const max = 72
	s = strings.TrimRight(s, " \t")
	if len(s) > max {
		s = s[:max] + "…"
	}
	return "«" + s + "»"
}

// minInt — своё имя, потому что в пакете уже есть `min` с другим предметом.
// Тень над существующим именем прошла бы сборку и означала бы другое.
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
