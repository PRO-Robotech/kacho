// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// hooksnoticereach_injection_test.go — доказательство в ОБЕ стороны.
//
// (а) сними напоминание — гейт краснеет и НАЗЫВАЕТ, какой путь исчез;
// (б) оставь достижимым хотя бы одним путём — гейт молчит.
//
// Односторонняя проба здесь была бы особенно бесполезной: «напоминание
// достижимо» верно и для дерева, где оно достижимо ОДНИМ путём, и для дерева,
// где путей два. Красным обязано становиться исчезновение КАЖДОГО, а не только
// последнего, — иначе гейт заметит беду ровно тогда, когда она уже случилась
// целиком.
//
// Отдельно проверяется, что предикаты чтения находят СВОЙ предмет в настоящем
// дереве: предикат, разошедшийся с формой файла, объявил бы напоминание
// недостижимым на исправном дереве, и гейт сняли бы как шумный.
package repohygiene

import (
	"strings"
	"testing"
)

func TestHooksNoticeReach_ProvenByInjection(t *testing.T) {
	full := hooksNoticeReach{
		TargetDeclared: true, VarDeclared: true,
		MakeTargets: []string{"test-unit", "test-integration"}, RunnerCalls: 1,
	}

	t.Run("законный близнец: оба пути на месте — гейт молчит", func(t *testing.T) {
		if found := adjudicateHooksNoticeReach(full); len(found) != 0 {
			t.Fatalf("ложное срабатывание на исправном дереве: %v", found)
		}
	})

	t.Run("законный близнец: целей одна, а не две — этого достаточно", func(t *testing.T) {
		one := full
		one.MakeTargets = []string{"test-unit"}
		if found := adjudicateHooksNoticeReach(one); len(found) != 0 {
			t.Fatalf("гейт требует конкретного ЧИСЛА целей вместо достижимости: %v", found)
		}
	})

	t.Run("снят вызов из прогонщика — краснеет и называет прогонщик", func(t *testing.T) {
		broken := full
		broken.RunnerCalls = 0
		found := adjudicateHooksNoticeReach(broken)
		if len(found) != 1 {
			t.Fatalf("ожидалась 1 находка, получено %d: %v", len(found), found)
		}
		if !strings.Contains(found[0], hooksNoticeRunner) {
			t.Fatalf("находка не называет исчезнувший путь:\n%s", found[0])
		}
	})

	t.Run("снято со всех целей — краснеет и называет Makefile", func(t *testing.T) {
		broken := full
		broken.MakeTargets = nil
		found := adjudicateHooksNoticeReach(broken)
		if len(found) != 1 {
			t.Fatalf("ожидалась 1 находка, получено %d: %v", len(found), found)
		}
		if !strings.Contains(found[0], hooksNoticeMakefile) {
			t.Fatalf("находка не называет исчезнувший путь:\n%s", found[0])
		}
	})

	t.Run("снято ВЕЗДЕ — находка одна и говорит о недостижимости, а не о двух путях", func(t *testing.T) {
		broken := full
		broken.MakeTargets, broken.RunnerCalls = nil, 0
		found := adjudicateHooksNoticeReach(broken)
		if len(found) != 1 {
			t.Fatalf("ожидалась 1 находка о недостижимости, получено %d: %v", len(found), found)
		}
		if !strings.Contains(found[0], "НЕДОСТИЖИМО") {
			t.Fatalf("находка не называет существа беды:\n%s", found[0])
		}
	})

	t.Run("снята сама цель — краснеет отдельно", func(t *testing.T) {
		broken := full
		broken.TargetDeclared = false
		if found := adjudicateHooksNoticeReach(broken); len(found) != 1 {
			t.Fatalf("снятие цели `hooks-notice` не поймано: %v", found)
		}
	})

	t.Run("снята переменная — краснеет отдельно", func(t *testing.T) {
		broken := full
		broken.VarDeclared = false
		if found := adjudicateHooksNoticeReach(broken); len(found) != 1 {
			t.Fatalf("снятие HOOKS_NOTICE не поймано: %v", found)
		}
	})

	// ПРЕДИКАТЫ ЧТЕНИЯ НАХОДЯТ СВОЙ ПРЕДМЕТ. Без этого гейт мог бы молчать
	// оттого, что разошёлся с формой файла, — и его сняли бы как шумный ровно
	// после того, как он перестал что-либо измерять.
	t.Run("предикаты находят предмет в синтетике той же формы", func(t *testing.T) {
		mk := "hooks-notice:\n\t@bash scripts/hooks/install.sh notice\n" +
			"HOOKS_NOTICE := hooks-notice\n" +
			"test-unit: $(HOOKS_NOTICE)\n\tgo test ./...\n"
		if !reHooksNoticeTarget.MatchString(mk) {
			t.Error("предикат цели не находит объявление цели")
		}
		if !reHooksNoticeVar.MatchString(mk) {
			t.Error("предикат переменной не находит её объявление")
		}
		users := reHooksNoticeUser.FindAllStringSubmatch(mk, -1)
		if len(users) != 1 || users[0][1] != "test-unit" {
			t.Errorf("предикат пользователей вернул %v, ждали одну цель test-unit", users)
		}
		if !reRunnerNotice.MatchString(`bash "$ROOT/scripts/hooks/install.sh" notice || true`) {
			t.Error("предикат вызова из прогонщика не находит вызов")
		}
		// ЗАКОННЫЙ БЛИЗНЕЦ ПРЕДИКАТА: цель, лишь УПОМИНАЮЩАЯ переменную в теле
		// рецепта, пользователем не является — иначе гейт считал бы достижимым
		// путь, которого нет.
		notUser := "docs:\n\techo $(HOOKS_NOTICE)\n"
		if reHooksNoticeUser.MatchString(notUser) {
			t.Error("упоминание в ТЕЛЕ рецепта принято за предпосылку цели")
		}
	})
}
