// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_step_survives_kubernetes_expansion_test.go — текст, объявленный в
// `command`/`args` контейнера, обязан ДОЕЗЖАТЬ ДО ОБОЛОЧКИ ДОСЛОВНО.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ — ЧУЖАЯ ПОДСТАНОВКА МЕЖДУ ЧАРТОМ И ОБОЛОЧКОЙ
//
// Kubernetes подставляет в `command`/`args` СВОИ ссылки — `$(ИМЯ)` по объявленным
// переменным контейнера — и трактует `$$` как ЭКРАН для одного `$`. То есть между
// тем, что написал чарт, и тем, что прочитала оболочка, стоит ЕЩЁ ОДИН
// подстановщик, о котором в объявлении не сказано ничего.
//
// Наблюдалось (задача #1786, ревизия bb11485ec, оба рабочих объекта личности,
// семь попыток подряд). Чарт объявлял:
//
//	eval "KACHO_SUBST_TOKEN=\$$n"
//
// Kubelet схлопнул `$$` в `$`, и оболочка получила `eval "KACHO_SUBST_TOKEN=\$n"`,
// то есть присвоение ИМЕНИ переменной вместо её ВЕЛИЧИНЫ. Шаг подстановки
// аккуратно записал в конфигурацию строку `KANAME_HOOK_TOKEN` на месте каждой
// ссылки — и сам же это поймал стражем своего выхода, отказав в старте. Стенд не
// поднялся, семь проверок линии были красны.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ЭТОГО НЕ ПОЙМАЛ НИ ОДИН ПРОГОН НА МАШИНЕ РАЗРАБОТЧИКА
//
// Потому что ВСЕ они — и доказательство инъекцией, и ручное воспроизведение —
// подают извлечённый из рендера скрипт прямо в `sh`. Подстановки Kubernetes в
// этом пути НЕТ by construction, поэтому дефект в них не воспроизводится
// НИКОГДА: харнесс снисходительнее продукта (`e2e-flow.md` §5). Замер: тот же
// скрипт на той же карте настроек в том же образе `oryd/kratos:v26.2.0` — зелёный
// при прямом запуске и красный, если перед запуском применить схлопывание `$$`.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ИМЕННО СУДИТСЯ
//
// Свойство названо ОДНИМ утверждением: подстановка Kubernetes на этом тексте —
// ТОЖДЕСТВО. Оно покрывает обе формы разом (`$$` и `$(ИМЯ)` по объявленной
// переменной) и не требует перечня запрещённых последовательностей, который рос
// бы вместе с платформой и не рос бы вместе с деревом.
//
// Подстановщик здесь ВОСПРОИЗВЕДЁН, а не описан: разбор повторяет
// k8s.io/kubernetes/third_party/forked/golang/expansion дословно, включая обе
// ветви `tryReadVariableName` и оборачивание нерезолвящейся ссылки обратно в
// `$(ИМЯ)`. Проверка по образцу «нет ли `$$`» была бы вторым местом об одном
// предмете и разошлась бы с платформой молча.
//
// ГРАНИЦА НАЗВАНА ЧЕСТНО:
//
//   - популяция — та же, что у соседа identity_step_declaration_parses_test.go
//     (объявление, несущее и образ, и запускаемую команду), и берётся ТЕМ ЖЕ
//     кодом: две популяции об одном предмете разошлись бы молча;
//   - контейнеры, объявленные не через `define`, не судятся — у соседа замерено,
//     что сплошной разбор всех объявлений чарта красен на исправном дереве;
//   - гейт запрещает подстановку Kubernetes в НАШИХ командах намеренно: величины
//     сюда доставляет оболочка, читая переменные окружения. Понадобится обратное
//     — это решение, и меняется вместе с этим гейтом, а не мимо него.
package deploy_test

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// ─────────────────────────────────────────────────────────────────────────────
// ПОДСТАНОВЩИК KUBERNETES — воспроизведён дословно
//
// Источник: k8s.io/kubernetes/third_party/forked/golang/expansion (Expand +
// tryReadVariableName) и kubelet MappingFuncFor, оборачивающий неизвестное имя
// обратно в `$(ИМЯ)`.

const k8sOperator = '$'

// k8sTryReadVariableName — ветви ровно те же, что у платформы:
// `$$` — экран (возвращается ОДИН `$`), `$(…)` — ссылка до первого `)`,
// всё прочее — `$` вместе со следующим знаком, дословно.
func k8sTryReadVariableName(input string) (read string, isVar bool, advance int) {
	switch input[0] {
	case k8sOperator:
		return input[0:1], false, 1
	case '(':
		for i := 1; i < len(input); i++ {
			if input[i] == ')' {
				return input[1:i], true, i + 1
			}
		}
		return "$(", false, 1
	default:
		return "$" + string(input[0]), false, 1
	}
}

// k8sExpand применяет подстановку Kubernetes к тексту команды.
func k8sExpand(input string, env map[string]string) string {
	var buf strings.Builder
	checkpoint := 0
	for cursor := 0; cursor < len(input); cursor++ {
		if input[cursor] != k8sOperator || cursor+1 >= len(input) {
			continue
		}
		buf.WriteString(input[checkpoint:cursor])
		read, isVar, advance := k8sTryReadVariableName(input[cursor+1:])
		switch {
		case isVar:
			if v, ok := env[read]; ok {
				buf.WriteString(v)
			} else {
				buf.WriteString("$(" + read + ")")
			}
		default:
			buf.WriteString(read)
		}
		cursor += advance
		checkpoint = cursor + 1
	}
	return buf.String() + input[checkpoint:]
}

// ─────────────────────────────────────────────────────────────────────────────

type expansionEnvEntry struct {
	Name string `yaml:"name"`
}

type expansionContainer struct {
	Name    string              `yaml:"name"`
	Command []string            `yaml:"command"`
	Args    []string            `yaml:"args"`
	Env     []expansionEnvEntry `yaml:"env"`
}

// firstDifference — номер строки и обе редакции первой разошедшейся строки.
// Отказ обязан называть КООРДИНАТУ, а не только факт расхождения.
func firstDifference(before, after string) (line int, was, became string) {
	b := strings.Split(before, "\n")
	a := strings.Split(after, "\n")
	for i := 0; i < len(b) && i < len(a); i++ {
		if b[i] != a[i] {
			return i + 1, strings.TrimSpace(b[i]), strings.TrimSpace(a[i])
		}
	}
	return 0, "", ""
}

// stepExpansionFindings — находки по объявлениям-контейнерам под корнем root
// плюс объём осмотренного: «ноль находок» обязано быть отличимо от «ноль
// прочитанного».
func stepExpansionFindings(t *testing.T, root string) (findings []string, defs, containers, strs int) {
	t.Helper()
	bodies := containerDefines(t, root)
	coords := make([]string, 0, len(bodies))
	for c := range bodies {
		coords = append(coords, c)
	}
	sort.Strings(coords)

	for _, coord := range coords {
		defs++
		var steps []expansionContainer
		if err := yaml.Unmarshal([]byte(neutraliseHelmActions(bodies[coord])), &steps); err != nil {
			// Разбираемость объявления — предмет соседнего гейта; здесь она
			// пропускается, но остаётся ВИДНОЙ в переписи (defs > containers).
			continue
		}
		for _, s := range steps {
			containers++
			env := map[string]string{}
			for _, e := range s.Env {
				if e.Name != "" {
					env[e.Name] = "«ВЕЛИЧИНА ПЕРЕМЕННОЙ " + e.Name + "»"
				}
			}
			for _, f := range []struct {
				field string
				vals  []string
			}{{"command", s.Command}, {"args", s.Args}} {
				for i, v := range f.vals {
					strs++
					got := k8sExpand(v, env)
					if got == v {
						continue
					}
					ln, was, became := firstDifference(v, got)
					findings = append(findings, fmt.Sprintf(
						"%s: контейнер %q, %s[%d]: текст НЕ ДОЕЗЖАЕТ до оболочки дословно — "+
							"Kubernetes подставляет в `command`/`args` свои ссылки `$(ИМЯ)` и "+
							"схлопывает `$$` в один `$` ДО запуска оболочки. Строка %d "+
							"объявлена как %q, а оболочка получит %q. Величины сюда доставляет "+
							"оболочка, читая окружение: пиши `${ИМЯ}` (эту форму Kubernetes не "+
							"трогает), а не `$$ИМЯ`. Тот же класс уронил стенд задачей #1786: "+
							"подстановка записала в конфигурацию ИМЯ переменной вместо величины",
						coord, s.Name, f.field, i, ln, was, became))
				}
			}
		}
	}
	return findings, defs, containers, strs
}

// TestIdentityStepSurvivesKubernetesExpansion — подстановка Kubernetes на
// объявленных нами командах есть ТОЖДЕСТВО.
func TestIdentityStepSurvivesKubernetesExpansion(t *testing.T) {
	findings, defs, containers, strs := stepExpansionFindings(t, umbrellaDir)
	if defs == 0 {
		t.Fatalf("обход ПУСТ: объявлений-контейнеров не найдено ни одного — вердикт " +
			"беспредметен, а не зелёный")
	}
	if strs == 0 {
		t.Fatalf("осмотрено объявлений %d, контейнеров %d, а строк команд НОЛЬ — судить "+
			"нечего, и это отказ, а не успех", defs, containers)
	}
	for _, f := range findings {
		t.Errorf("%s", f)
	}
	t.Logf("перепись: объявлений-контейнеров %d · контейнеров разобрано %d · "+
		"строк команд осмотрено %d · находок %d", defs, containers, strs, len(findings))
}
