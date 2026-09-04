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
//
// # ДВЕ СВЕРКИ, А НЕ ОДНА, И НИ ОДИН ВЛАДЕЛЕЦ НЕ ОСВОБОЖДЁН
//
// Сверка файла судит ОДИН файл — и потому имеет предмет ровно там, где этот файл
// рендерится. У владельца со сведённой цепью его нет by construction: отказ стоит
// в первичной миграции, а отрендерить ему отдельный файл значило бы завести
// версию ниже уже применённой, то есть версию, которую мигратор не применит.
//
// Вывод «значит у него не проверяем» был бы послаблением без срока, поэтому вторая
// сверка идёт у ВСЕХ шести: тело функции отказа, взятое ПОСЛЕДНИМ по цепи, обязано
// совпадать с телом из рендера шаблона. Это референт, который дерево производит
// независимо от своей формы — `pg_dump` печатает тело дословно, каким его создала
// миграция.
//
// Вторая сверка вдобавок СИЛЬНЕЕ первой у тех пятерых, у кого файл есть: поздняя
// миграция с `CREATE OR REPLACE`, переписавшая отказ своими словами, сверке файла
// невидима — отрендеренный файл она не трогает. Поэтому обе и остаются.
//
// Перепись печатает обе величины отдельно: владельцев, файлов сверено, тел
// сверено. Одно число скрыло бы ровно тот случай, ради которого вторая сверка
// заведена.
package repohygiene

import (
	"os"
	"path/filepath"
	"sort"
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

	filesChecked, bodiesChecked := 0, 0
	var findings []string
	for _, o := range owners {
		want, err := quota.RenderRefusalMigration(o)
		if err != nil {
			t.Fatalf("рендер для %s: %v", o.Service, err)
		}
		dir := filepath.Join(root, "services", o.Service, "internal", "migrations")

		// ── СВЕРКА ПЕРВАЯ: файл целиком, там где он рендерится ─────────────
		if o.RendersOwnFile() {
			path := filepath.Join(dir, o.Migration)
			got, rerr := os.ReadFile(path) // #nosec G304 -- путь из перечня владельцев
			switch {
			case rerr != nil:
				findings = append(findings, o.Service+": файла миграции нет — "+
					"владелец объявлен, а отказ ему не сгенерирован ("+o.Migration+")")
			default:
				filesChecked++
				if string(got) != want {
					findings = append(findings, o.Service+": "+o.Migration+
						" разошёлся с шаблоном — "+firstDiff(string(got), want)+
						". Перегенерировать: go run ./tools/quota-refusal-migration")
				}
			}
		}

		// ── СВЕРКА ВТОРАЯ: тело функции, у ВСЕХ владельцев ─────────────────
		wantBodies, berr := quota.ExpectedRefusalBodies(o)
		if berr != nil {
			// Эталон не извлёкся — сравнивать не с чем, и молчание здесь
			// означало бы согласие с любым деревом.
			t.Fatalf("%v", berr)
		}
		gotBodies, files, gerr := lastRefusalBodiesInChain(dir)
		if gerr != nil {
			t.Fatalf("%s: цепь миграций не читается: %v", o.Service, gerr)
		}
		if files == 0 {
			t.Fatalf("%s: в цепи не прочитано ни одного файла миграции — «тела совпали» "+
				"означало бы «тел не искали»", o.Service)
		}
		for _, fn := range quota.RefusalFunctionNames() {
			got, ok := gotBodies[fn]
			if !ok {
				findings = append(findings, o.Service+": функция "+fn+
					" не объявлена ни одной миграцией цепи — отказ учёта у этого владельца "+
					"производить некому")
				continue
			}
			bodiesChecked++
			if got != wantBodies[fn] {
				findings = append(findings, o.Service+": тело "+fn+
					" в цепи разошлось с шаблоном — "+firstDiff(got, wantBodies[fn])+
					". Тело берётся последним по цепи: расхождение здесь значит, что отказ "+
					"переписан миграцией, а не шаблоном")
			}
		}
	}

	// Перепись — ТРИ величины отдельно. Одно число не отличило бы владельца,
	// у которого сверен только файл, от владельца, у которого сверено только тело.
	t.Logf("перепись: владельцев %d, файлов сверено %d, тел функций сверено %d",
		len(owners), filesChecked, bodiesChecked)

	if bodiesChecked == 0 {
		t.Fatal("не сверено ни одного тела функции: «ноль расхождений» здесь означало бы " +
			"«ноль прочитанного», а это разные утверждения")
	}
	if len(findings) > 0 {
		t.Fatalf("отказ учёта разошёлся с единым источником (%d):\n  %s",
			len(findings), strings.Join(findings, "\n  "))
	}
}

// lastRefusalBodiesInChain — тела функций отказа, какими их оставляет ЦЕПЬ.
//
// Файлы обходятся в порядке имён (он же порядок версий у goose), и позднейшее
// определение затирает раннее — ровно так, как это сделает база. Прежняя сверка
// смотрела в один отрендеренный файл и не увидела бы `CREATE OR REPLACE` в
// миграции, легшей после него.
//
// Возвращает также число прочитанных файлов: «тел не нашлось» и «файлов не
// прочиталось» — разные состояния, и вызывающий обязан их различить.
func lastRefusalBodiesInChain(dir string) (map[string]string, int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, 0, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	out := map[string]string{}
	read := 0
	for _, n := range names {
		body, rerr := os.ReadFile(filepath.Join(dir, n)) // #nosec G304 -- путь из обхода каталога миграций
		if rerr != nil {
			return nil, read, rerr
		}
		read++
		for fn, b := range quota.RefusalFunctionBodies(string(body)) {
			out[fn] = b
		}
	}
	return out, read, nil
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
