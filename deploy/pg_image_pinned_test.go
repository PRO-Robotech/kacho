// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy

// pg_image_pinned_test.go — образ Postgres припинен и ОДИНАКОВ у всех инстансов.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ЭТО ЛОВИТ И ПОЧЕМУ ЭТОГО НЕ ВИДНО БЕЗ ГЕЙТА
//
// Инстансов Postgres в стенде десять, и тег образа выписывается У КАЖДОГО СВОЙ
// строкой. Два инстанса тег имели, восемь ехали на умолчании чарта — и это
// выглядело нормально ровно до кластера, где умолчание не поднялось: его initdb
// отказывается стартовать, не сумев разрешить собственный идентификатор
// пользователя, а образ записи о нём не несёт. Симптом при этом был немой:
// контейнер завершался кодом 2 БЕЗ единой строки в журнале, потому что вывод
// initdb проглатывался обёрткой.
//
// Существенно не то, какой именно тег правильный, а что их РОВНО ОДИН. Стенд, в
// котором часть баз на одной версии, а часть на другой, не отвечает на вопрос
// «на чём мы проверили»: зелёный прогон относится к смеси, которой нет ни в
// одном развёртывании.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ГЕЙТ ЧИТАЕТ ОБЪЯВЛЕНИЕ, А НЕ ОТРЕНДЕРЕННЫЙ ШАБЛОН
//
// Отрендеренный шаблон требует установленного helm и сети; такая проверка
// пропускается там, где её нет, — то есть ровно там, где никто не заметит.
// Объявление читается всегда и одинаково.

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// pgProfiles — профили, объявляющие инстансы Postgres.
//
// Перечень ВЫВОДИТСЯ из каталога, а не выписывается: выписанный отстанет от
// дерева на первом же новом профиле, и отставание будет незаметным.
func pgProfiles(t *testing.T) []string {
	t.Helper()
	matches, err := filepath.Glob("helm/umbrella/values*.yaml")
	if err != nil {
		t.Fatalf("обход профилей: %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("профилей не найдено — гейт, не прочитавший предмет, обязан падать, а не зеленеть")
	}
	sort.Strings(matches)
	return matches
}

func TestPostgresImageTagIsPinnedAndUniform(t *testing.T) {
	profiles := pgProfiles(t)

	// Тег на инстанс, по всем профилям вместе: профили НАКЛАДЫВАЮТСЯ, и инстанс,
	// припиненный в одном и неприпиненный в другом, — законная композиция.
	// Незаконно другое: инстанс, которого не припинил НИ ОДИН профиль, и два
	// разных тега на один стенд.
	pinned := map[string]map[string]string{} // инстанс → тег → где объявлен
	declared := map[string]bool{}
	filesRead := 0

	for _, prof := range profiles {
		raw, err := os.ReadFile(prof) // #nosec G304 -- путь из обхода каталога репозитория
		if err != nil {
			t.Fatalf("%s: %v", prof, err)
		}
		filesRead++
		var doc map[string]any
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("%s: разбор: %v", prof, err)
		}
		for key, val := range doc {
			if !strings.HasPrefix(key, "pg-") {
				continue
			}
			inst, ok := val.(map[string]any)
			if !ok {
				continue
			}
			declared[key] = true
			img, ok := inst["image"].(map[string]any)
			if !ok {
				continue
			}
			tag, _ := img["tag"].(string)
			if tag == "" {
				continue
			}
			if pinned[key] == nil {
				pinned[key] = map[string]string{}
			}
			pinned[key][tag] = filepath.Base(prof)
		}
	}

	if len(declared) == 0 {
		t.Fatal("не найдено ни одного инстанса Postgres — «расхождений нет» означало бы " +
			"«ничего не прочитано»")
	}

	// (1) Каждый объявленный инстанс припинен хоть где-то.
	var unpinned []string
	for inst := range declared {
		if len(pinned[inst]) == 0 {
			unpinned = append(unpinned, inst)
		}
	}
	sort.Strings(unpinned)
	if len(unpinned) > 0 {
		t.Errorf("инстансы Postgres без припиненного тега: %v. Они поедут на умолчании "+
			"чарта, которое меняется вместе с чартом и не совпадает с тем, на чём проверяли. "+
			"Один такой инстанс уже не поднялся на кластере, завершаясь кодом 2 БЕЗ строки в "+
			"журнале", unpinned)
	}

	// (2) Тег ОДИН на все инстансы.
	all := map[string][]string{}
	for inst, tags := range pinned {
		for tag := range tags {
			all[tag] = append(all[tag], inst)
		}
	}
	if len(all) > 1 {
		var lines []string
		for tag, insts := range all {
			sort.Strings(insts)
			lines = append(lines, tag+" ← "+strings.Join(insts, ", "))
		}
		sort.Strings(lines)
		t.Errorf("разные версии Postgres в одном стенде:\n  %s\nЗелёный прогон тогда "+
			"относится к смеси, которой нет ни в одном развёртывании, и «на чём проверили» "+
			"остаётся без ответа", strings.Join(lines, "\n  "))
	}

	tagNames := make([]string, 0, len(all))
	for tag := range all {
		tagNames = append(tagNames, tag)
	}
	sort.Strings(tagNames)
	t.Logf("осмотрено: профилей %d, инстансов Postgres %d, припиненных %d, различных тегов %d %v",
		filesRead, len(declared), len(pinned), len(all), tagNames)
}
