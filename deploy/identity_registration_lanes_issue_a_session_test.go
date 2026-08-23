// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_registration_lanes_issue_a_session_test.go — КАЖДАЯ полоса
// регистрации обязана выдавать сессию.
//
// # Предмет
//
// У потока регистрации несколько полос (способов первого входа), и настройки
// объявляют их по отдельности. Полосы одного потока обязаны сходиться в том,
// ЧЕМ поток заканчивается: либо регистрация даёт рабочую сессию, либо нет.
// Разойдясь, они дают состояние, которое не выбирал никто: часть арендаторов
// заводится и входит, часть заводится и остаётся снаружи.
//
// # Почему это не ловится ничем другим
//
// Рендер чарта такую полосу принимает — она синтаксически законна. Страж старта
// службы личности её принимает — настройка валидна. Расхождение видно только
// при сравнении полос МЕЖДУ СОБОЙ, и только у того, кто знает, что они об одном
// предмете. Наблюдалось: полоса пароля доводила регистрацию до конца, показывала
// экран подтверждения почты и НЕ ставила печенья сессии, тогда как соседняя
// полоса того же потока сессию выдавала и своим комментарием это объявляла.
//
// # Что здесь НЕ утверждается
//
// Не утверждается, что подтверждение почты не нужно: экран подтверждения
// остаётся, и полоса вправе его показывать. Утверждается ровно одно — показ
// подтверждения не отменяет выдачу сессии, потому что это РАЗНЫЕ вещи, и
// соседняя полоса делает обе.
//
// Читается ОБЪЯВЛЕНИЕ, а не рендер: ни helm, ни кластер не нужны, поэтому
// проверка не умеет пропускаться.
package deploy_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// registrationLaneHooks возвращает «полоса → её перечень хуков» для потока
// регистрации, разбирая объявление по отступам.
func registrationLaneHooks(t *testing.T) map[string][]string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Clean(identityConfigTemplate))
	if err != nil {
		t.Fatalf("объявление настроек личности не прочитано (%s): %v", identityConfigTemplate, err)
	}
	lines := strings.Split(string(raw), "\n")

	indentOf := func(s string) int { return len(s) - len(strings.TrimLeft(s, " ")) }

	lanes := map[string][]string{}
	inRegistrationAfter := false
	afterIndent := -1
	lane := ""
	for i, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if !inRegistrationAfter {
			// `after:` потока регистрации — тот, что идёт ПОСЛЕ `registration:`
			// и до следующего ключа того же уровня.
			if trimmed == "after:" {
				for j := i - 1; j >= 0; j-- {
					p := strings.TrimSpace(lines[j])
					if p == "" || strings.HasPrefix(p, "#") {
						continue
					}
					if strings.HasPrefix(p, "registration:") {
						inRegistrationAfter, afterIndent = true, indentOf(ln)
					}
					if strings.HasSuffix(p, ":") && indentOf(lines[j]) <= indentOf(ln)-2 {
						break
					}
				}
			}
			continue
		}
		if indentOf(ln) <= afterIndent && !strings.HasPrefix(trimmed, "- ") {
			break // вышли из `after:` потока регистрации
		}
		if indentOf(ln) == afterIndent+2 && strings.HasSuffix(trimmed, ":") {
			lane = strings.TrimSuffix(trimmed, ":")
			if _, ok := lanes[lane]; !ok {
				lanes[lane] = nil
			}
			continue
		}
		if lane != "" && strings.HasPrefix(trimmed, "- hook:") {
			h := strings.TrimSpace(strings.TrimPrefix(trimmed, "- hook:"))
			if idx := strings.Index(h, "#"); idx >= 0 {
				h = strings.TrimSpace(h[:idx])
			}
			lanes[lane] = append(lanes[lane], h)
		}
	}
	return lanes
}

func TestIdentity_EveryRegistrationLaneIssuesASession(t *testing.T) {
	lanes := registrationLaneHooks(t)
	if len(lanes) == 0 {
		t.Fatal("полос регистрации не разобрано ни одной — вердикта нет: " +
			"«ноль находок» здесь неотличимо от «ноль прочитанного». " +
			"Либо поток переехал, либо разбор перестал его видеть")
	}

	withSession := 0
	for lane, hooks := range lanes {
		if len(hooks) == 0 {
			t.Errorf("полоса %q не объявила ни одного хука — регистрация по ней "+
				"ничем не заканчивается", lane)
			continue
		}
		has := false
		for _, h := range hooks {
			if h == "session" {
				has = true
				break
			}
		}
		if !has {
			t.Errorf("полоса регистрации %q не выдаёт сессию (хуки: %v). Полосы одного "+
				"потока обязаны сходиться в том, чем поток заканчивается: арендатор, "+
				"заведённый по этой полосе, остаётся снаружи, тогда как соседняя полоса "+
				"того же потока сессию выдаёт. Показ подтверждения почты выдачу НЕ "+
				"заменяет — это разные вещи", lane, hooks)
			continue
		}
		withSession++
	}
	t.Logf("перепись: полос регистрации %d · выдают сессию %d", len(lanes), withSession)
}
