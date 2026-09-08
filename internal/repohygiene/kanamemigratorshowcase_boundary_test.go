// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"
)

// kanamemigratorshowcase_boundary_test.go — ТРИ ПРОГОНА: гейт витрины и гейт
// имени судят РАЗНОЕ, и ни один не вакуумен.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ТРЁХ ПРОГОНОВ, А НЕ ДВУХ
//
// Заводя гейт рядом с существующим, недостаточно показать, что новый краснеет
// на своём дефекте: надо показать ещё, что существующий на этом дефекте МОЛЧИТ
// (иначе новый не нужен — предмет уже держат) и что он по-прежнему краснеет на
// СВОЁМ (иначе соседняя правка его убила, а заметить это нечем: мёртвый гейт
// выглядит ровно как исправный).
//
// Отсюда три прогона, и третий обязателен:
//
//	контроль            — дерево цело: молчат ОБА
//	инъекция НОВОГО     — дефект витрины: краснеет ТОЛЬКО новый
//	инъекция СУЩЕСТВУЮЩЕГО — дефект имени вне витрины: краснеет ТОЛЬКО существующий
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕМ ПРЕДМЕТЫ РАЗЛИЧАЮТСЯ
//
//	гейт витрины   ЧТО читает арендатор Kaname — документы, шаблоны, тексты;
//	               область: поставка Kaname плюс её подчарт зонта
//	гейт имени     ЧТО собирает и зовёт сборка — путь установки, выход сборки,
//	               переменная Makefile, справка cobra; область: services + deploy,
//	               ВСЕ семь точек наката, комментарии отброшены
//
// Пересечения нет by construction: первый читает прозу и не разбирает формы
// сборки, второй отбрасывает прозу и не читает `.md`/`.mdx` вовсе.

// showcaseBoundaryNameFindings — находки гейта ИМЕНИ по одному файлу.
func showcaseBoundaryNameFindings(rel, body string) []string {
	return migratorCLINameFindings(migratorCLIMentions(rel, body))
}

func TestShowcaseAndNameGatesJudgeDisjointSubjects(t *testing.T) {
	t.Parallel()
	// ── прогон 1: контроль ───────────────────────────────────────────────────
	//
	// Оба входа законны: документ называет свой накатчик, сборка кладёт его же.
	t.Run("контроль: оба входа законны — молчат ОБА гейта", func(t *testing.T) {
		doc := "services/iam/INSTALL.md"
		docBody := "1. `kaname-migrator up` — схема.\n"
		build := "services/iam/Dockerfile"
		buildBody := "COPY --from=builder /kaname-migrator /usr/local/bin/kaname-migrator\n"

		if f, c := KanameMigratorShowcaseScan(map[string]string{doc: docBody, build: buildBody}); len(f) != 0 {
			t.Fatalf("гейт витрины краснеет на законном входе: %v (перепись %+v)", f, c)
		}
		for _, tc := range []struct{ rel, body string }{{doc, docBody}, {build, buildBody}} {
			if f := showcaseBoundaryNameFindings(tc.rel, tc.body); len(f) != 0 {
				t.Fatalf("гейт имени краснеет на законном входе %s: %v", tc.rel, f)
			}
		}
	})

	// ── прогон 2: инъекция НОВОГО предмета ───────────────────────────────────
	//
	// Дефект витрины: клиентский документ называет накатчик платформы. Гейт
	// имени `.md` не читает вовсе — значит без нового гейта это место не держал
	// НИКТО, и предмет заведён не «на всякий случай».
	t.Run("новый предмет: имя платформы в клиентском документе", func(t *testing.T) {
		rel := "services/iam/INSTALL.md"
		body := "1. `kacho-migrator up` — схема. Обычно контейнер инициализации перед подом.\n"

		findings, census := KanameMigratorShowcaseScan(map[string]string{rel: body})
		if len(findings) == 0 {
			t.Fatalf("НОВЫЙ гейт смолчал на своём дефекте — он вакуумен: %+v", census)
		}
		if got := findings[0].String(); !strings.Contains(got, "kaname-migrator") {
			t.Errorf("находка не называет, каким имя обязано быть: %s", got)
		}
		if f := showcaseBoundaryNameFindings(rel, body); len(f) != 0 {
			t.Fatalf("СУЩЕСТВУЮЩИЙ гейт покраснел на чужом предмете — предметы "+
				"пересекаются, и красное придёт от соседа: %v", f)
		}
	})

	// ── прогон 3: инъекция СУЩЕСТВУЮЩЕГО предмета ───────────────────────────
	//
	// Дефект имени ВНЕ витрины Kaname: сборка службы платформы кладёт чужой
	// накатчик. Гейт витрины туда не смотрит by construction (его область —
	// поставка Kaname), и молчать обязан. Без этого прогона молчание
	// существующего гейта было бы неотличимо от его смерти.
	t.Run("существующий предмет: чужое имя в сборке службы платформы", func(t *testing.T) {
		rel := "services/vpc/Makefile"
		body := "MIGRATOR_BIN   := migrator\n"

		f := showcaseBoundaryNameFindings(rel, body)
		if len(f) == 0 {
			t.Fatal("СУЩЕСТВУЮЩИЙ гейт смолчал на своём дефекте — переустройство его убило")
		}
		if joined := strings.Join(f, "\n"); !strings.Contains(joined, "kacho-migrator") {
			t.Errorf("находка не называет платформенное имя: %s", joined)
		}

		findings, census := KanameMigratorShowcaseScan(map[string]string{rel: body})
		if len(findings) != 0 {
			t.Fatalf("НОВЫЙ гейт покраснел на службе платформы — его область шире "+
				"объявленной, и он запрещал бы продукту законное имя: %v", findings)
		}
		// Молчание обязано прийти от ОБЛАСТИ, а не от того, что гейт ничего не
		// прочитал: иначе тот же зелёный он дал бы на любом входе.
		if census.FilesRead != 1 {
			t.Fatalf("новый гейт не читал входа — его молчание беспредметно: %+v", census)
		}
	})
}
