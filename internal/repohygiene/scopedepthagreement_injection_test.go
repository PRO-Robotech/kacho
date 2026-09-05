// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// scopedepthagreement_injection_test.go — доказательство в ОБЕ стороны.
//
// (а) верни дефект — сверка краснеет и НАЗЫВАЕТ ОБЕ координаты;
// (б) поставь рядом ЗАКОННУЮ конструкцию той же формы — сверка молчит.
//
// # Утверждение об ОХВАТЕ снято ВМЕСТЕ со своим предметом
//
// Предмет #918 — не сравнение чисел, а ГРАНИЦА ОБХОДА: прежняя редакция искала
// третью величину только под `services/iam/internal`, а объявлена она была в
// КОРНЕВОМ `internal/authzplan` — в поддереве, куда обход не заходил by
// construction. Здесь стояла подпроба, требовавшая от настоящего состава дерева
// ровно одного: величина обязана находиться ВНЕ прежнего поддерева. Условие было
// несущим — найдись она внутри, прежнего охвата хватило бы, и утверждение о
// закрытой слепой зоне ничего бы не стоило.
//
// Линия выноса iam отдельным продуктом перенесла `authzplan` под
// `services/iam/internal/`: пакет потребляют только iam и его же прибор, и он
// обязан уехать вместе с сервисом. Тем самым слепой зоны БОЛЬШЕ НЕТ — а вместе с
// ней не стало и предмета у подпробы: она немедленно покраснела, сказав про себя
// же, что доказывать ей нечего. Это исход, а не поломка: утверждение снято тем же
// изменением, что сняло его предмет, и шапка правится здесь же — иначе она
// пережила бы снятую ветвь и стала бы ложью того самого рода, против которого
// написана.
//
// Свойство «обход идёт по ВСЕМУ дереву» при этом не осталось без держателя: его
// несёт сам добытчик — [findPlanDepth] строит состав `treecorpus`-ом от КОРНЯ, и
// сузить его до одного сервиса нельзя, не переписав его же.
//
// # Почему сверка вызывается напрямую, а не через настоящее дерево
//
// Уронить настоящее дерево нарочно нельзя, а гейт, который нельзя уронить, не
// отличается от гейта, который не может упасть. Поэтому суждение отделено от
// добычи (`adjudicateScopeDepth`) и здесь получает величины на вход.
package repohygiene

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

func TestScopeDepthAgreement_ProvenByInjection(t *testing.T) {
	root := repoRoot(t)

	// ── КОНТРОЛЬ. Без него краснота ниже неотличима от красноты дерева ──────
	planDepth, planFound := findPlanDepth(t, root)
	if !planFound {
		t.Fatal("предел компилятора модели в дереве НЕ НАЙДЕН — доказывать нечего.\n" +
			"    Либо величину сняли (тогда снимите и эту пробу вместе с предметом), " +
			"либо обход снова смотрит не туда — то есть вернулся дефект #918.")
	}
	t.Logf("контроль: предел компилятора модели найден, значение %d", planDepth)

	t.Run("расхождение третьей величины краснеет и называет ОБЕ координаты", func(t *testing.T) {
		found := adjudicateScopeDepth(scopeDepthTriple{
			goDepth: 4, sqlDepth: 4, planDepth: 3, planFound: true,
		})
		if len(found) != 1 {
			t.Fatalf("ожидалась 1 находка, получено %d: %v", len(found), found)
		}
		for _, want := range []string{scopeDepthPlanFile, scopeDepthConstFile, "3", "4"} {
			if !strings.Contains(found[0], want) {
				t.Fatalf("находка не называет %q — по ней не видно, что чинить:\n%s", want, found[0])
			}
		}
	})

	t.Run("расхождение первых двух краснеет и называет ОБЕ координаты", func(t *testing.T) {
		found := adjudicateScopeDepth(scopeDepthTriple{
			goDepth: 4, sqlDepth: 2, planDepth: 4, planFound: true,
		})
		if len(found) != 1 {
			t.Fatalf("ожидалась 1 находка, получено %d: %v", len(found), found)
		}
		for _, want := range []string{scopeDepthConstFile, scopeDepthMigration} {
			if !strings.Contains(found[0], want) {
				t.Fatalf("находка не называет %q:\n%s", want, found[0])
			}
		}
	})

	t.Run("расходятся все три — названы обе пары, а не первая попавшаяся", func(t *testing.T) {
		found := adjudicateScopeDepth(scopeDepthTriple{
			goDepth: 4, sqlDepth: 2, planDepth: 3, planFound: true,
		})
		if len(found) != 2 {
			t.Fatalf("ожидались 2 находки, получено %d: %v", len(found), found)
		}
	})

	// ── ЗАКОННЫЕ БЛИЗНЕЦЫ. Без них отрицание зеленело бы на всём сломанном ──
	t.Run("согласие трёх величин — сверка молчит", func(t *testing.T) {
		if found := adjudicateScopeDepth(scopeDepthTriple{
			goDepth: 4, sqlDepth: 4, planDepth: 4, planFound: true,
		}); len(found) != 0 {
			t.Fatalf("ложное срабатывание на согласии: %v", found)
		}
	})

	t.Run("ненайденная третья величина НЕ выдумывает расхождения", func(t *testing.T) {
		// planDepth здесь — нулевое значение, а не «ноль как предел». Судить по
		// нему значило бы краснеть на дереве, где величины просто нет.
		if found := adjudicateScopeDepth(scopeDepthTriple{
			goDepth: 4, sqlDepth: 4, planFound: false,
		}); len(found) != 0 {
			t.Fatalf("ненайденная величина принята за расхождение: %v", found)
		}
	})

	t.Run("согласие на ДРУГОМ числе — сверка по равенству, а не по литералу 4", func(t *testing.T) {
		if found := adjudicateScopeDepth(scopeDepthTriple{
			goDepth: 7, sqlDepth: 7, planDepth: 7, planFound: true,
		}); len(found) != 0 {
			t.Fatalf("сверка привязана к литералу, а не к равенству: %v", found)
		}
	})
}

// TestScopeDepthPlanFileCoordinateIsAlive — координата в тексте находки обязана
// существовать.
//
// Она не участвует в обходе (тот ищет по всему дереву), поэтому её устаревание
// ничего не роняет само по себе: находка просто начнёт посылать читателя в
// несуществующий файл. Ровно этот класс #918 и был — сообщение о границе,
// пережившее свою границу.
func TestScopeDepthPlanFileCoordinateIsAlive(t *testing.T) {
	root := repoRoot(t)
	tree, err := treecorpus.NewTree(root)
	if err != nil {
		t.Fatalf("состав дерева взять неоткуда: %v", err)
	}
	if !tree.HasFile(scopeDepthPlanFile) {
		t.Fatalf("координата третьей величины %q в составе дерева отсутствует: находка "+
			"послала бы читателя в файл, которого нет. Осмотрено файлов: %d",
			scopeDepthPlanFile, tree.Count())
	}
	t.Logf("ОБЪЁМ ОСМОТРЕННОГО: состав дерева %d файлов, координата %s жива",
		tree.Count(), scopeDepthPlanFile)
}

// TestScopeDepthRecogniserKnowsBothLegalFormsOfTheBound — распознаватель обязан
// знать КАЖДУЮ законную форму записи своего предмета.
//
// # Почему форм две и обе законны
//
// Границу пишет человек в миграции — `depth BETWEEN 1 AND 4`. Сведённую схему
// печатает `pg_dump`, а он печатает то, что хранит каталог, и хранит он
// разложенную форму — `((depth >= 1) AND (depth <= 4))`. Это одно и то же
// ограничение, записанное двумя способами; выбирает способ не автор, а инструмент.
//
// # Почему это доказывается, а не подразумевается
//
// Форма, о которой распознаватель не знает, не даёт ни красного, ни зелёного — она
// даёт МОЛЧАНИЕ (`testing.md` §«Гейт на класс», п. 7). Здесь молчание оказалось
// громким по счастливой случайности (`singleNumber` требует ровно одного
// совпадения и падает на нуле), но у следующего сведённого сервиса тот же промах
// пришёл бы снова — предикат чинили бы координатой, а не формой.
func TestScopeDepthRecogniserKnowsBothLegalFormsOfTheBound(t *testing.T) {
	// ── КАЖДАЯ форма обязана быть узнана и отдать своё число ────────────────
	for _, c := range []struct {
		what string
		body string
		want string
	}{
		{"форма миграции (пишет человек)", "    CHECK (depth BETWEEN 1 AND 4),", "4"},
		{"форма свода (печатает pg_dump)", "CONSTRAINT rpe_depth_bounded CHECK (((depth >= 1) AND (depth <= 4))),", "4"},
		{"форма свода с квалификатором", "((pe.depth >= 1) AND (pe.depth <= 7))", "7"},
	} {
		ms := reDepthCheck.FindAllStringSubmatch(c.body, -1)
		if len(ms) != 1 {
			t.Errorf("%s: совпадений %d, ожидалось 1 — форма распознавателю неизвестна, "+
				"и записанное в ней прошло бы ВНЕ наблюдения", c.what, len(ms))
			continue
		}
		got := ""
		for _, g := range ms[0][1:] {
			if g != "" {
				got = g
				break
			}
		}
		if got != c.want {
			t.Errorf("%s: извлечено %q, ожидалось %q", c.what, got, c.want)
		}
	}

	// ── ЗАКОННЫЕ БЛИЗНЕЦЫ. Без них расширение формы куплено ложными находками ─
	for _, c := range []struct {
		what string
		body string
	}{
		{"чужое поле с тем же хвостом имени", "CHECK ((max_depth <= 9))"},
		{"нижняя граница отдельно — не предмет", "CHECK ((depth >= 1))"},
		{"проза о границе, а не сама граница", "-- глубина ограничена значением 4"},
	} {
		if ms := reDepthCheck.FindAllStringSubmatch(c.body, -1); len(ms) != 0 {
			t.Errorf("%s: распознан как объявление границы (%d совпадений) — расширение "+
				"формы куплено ложным срабатыванием, а гейт с ложными находками снимают первым",
				c.what, len(ms))
		}
	}

	// ── И то, ради чего всё это: в НАСТОЯЩЕМ файле совпадение ровно одно ────
	//
	// Контроль в обе стороны на живом дереве: расширенный предикат обязан найти
	// величину и не найти второй. Ноль означал бы, что форма опять не та;
	// больше одного — что граница слова не отсекла однофамильцев.
	root := repoRoot(t)
	body := readFileForDepth(t, filepath.Join(root, scopeDepthMigration))
	ms := reDepthCheck.FindAllStringSubmatch(body, -1)
	t.Logf("ОБЪЁМ ОСМОТРЕННОГО: %s, совпадений границы глубины %d", scopeDepthMigration, len(ms))
	if len(ms) != 1 {
		t.Fatalf("в %s совпадений %d, ожидалось ровно 1", scopeDepthMigration, len(ms))
	}
}
