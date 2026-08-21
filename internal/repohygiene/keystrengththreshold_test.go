// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// keystrengththreshold_test.go — нижний порог стойкости подписного ключа объявлен
// ЧИСЛОМ и ровно в одном месте дерева (приёмка F1, сценарий F1-02).
//
// # Предмет
//
// Порог решает, принять ключ или отвергнуть. Пока он объявлен один раз, вопрос
// «какой у нас порог» имеет ответ; как только объявлений становится два, ответа
// нет ни у кого, а расхождение между ними не является ничьей находкой — оно не
// выражено.
//
// Расходятся такие пары всегда в одну сторону. Ужесточить порог значит отвергнуть
// ключ, который вчера принимался, — это видно сразу и стоит разговора. Ослабить
// значит принять ключ, который вчера отвергался, — это не видно вообще.
//
// # Почему числа мало, нужна РОЛЬ
//
// Гейт, ищущий число, объявил бы находкой и длину порождаемого ключа, и размер
// ключа обёртки, и сообщение об измеренной стойкости. Все три — законные соседи,
// и первый же ложный срабат на них гейт бы и отключил.
//
// Порогом является СРАВНЕНИЕ измеренной стойкости с литералом. Сравнение с
// объявленным порогом (`bits < alg.MinBits()`) порогом не является — оно его
// ЧИТАТЕЛЬ, и именно так и должно выглядеть каждое место, кроме одного.
//
// # Что здесь считается деревом
//
// Индекс git — то же множество, что увидит свежий клон и CI.
package repohygiene

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const (
	// keyStrengthDeclName — имя единственного объявления порога.
	//
	// Имя, а не признак: «функция, возвращающая порог» синтаксического признака
	// не имеет, и придуманный под один случай признак дал бы уверенность,
	// которой нет. Одно имя читается и опровергается целиком.
	keyStrengthDeclName = "MinBits"
	// keyStrengthCensusFloor — порог переписи.
	keyStrengthCensusFloor = 500
)

// TestKeyStrengthFloorIsDeclaredExactlyOnce — сам гейт.
func TestKeyStrengthFloorIsDeclaredExactlyOnce(t *testing.T) {
	root := repoRoot(t)
	tt := newTrackedTree(t, root)

	var rels []string
	for rel := range tt.files {
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
			continue
		}
		if skipPath(rel) {
			continue
		}
		rels = append(rels, rel)
	}
	sort.Strings(rels)

	var (
		parsed, funcs, comparisons int
		decls                      []KeyStrengthDeclaration
		second                     []KeyStrengthComparison
	)
	for _, rel := range rels {
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		d, c, census, err := ScanKeyStrengthThresholds(rel, src, keyStrengthDeclName)
		if err != nil {
			t.Fatalf("разбор %s: %v", rel, err)
		}
		parsed++
		funcs += census.Funcs
		comparisons += census.Comparisons
		decls = append(decls, d...)
		second = append(second, c...)
	}

	t.Logf("перепись: не-тестовых файлов Go разобрано %d, функций осмотрено %d, "+
		"сравнений осмотрено %d, объявлений порога найдено %d, сравнений стойкости "+
		"с литералом найдено %d",
		parsed, funcs, comparisons, len(decls), len(second))

	if parsed < keyStrengthCensusFloor {
		t.Fatalf("перепись обвалилась: разобрано %d файлов при пороге %d — "+
			"«ноль находок» на таком объёме означало бы «ноль прочитанного»",
			parsed, keyStrengthCensusFloor)
	}
	if comparisons == 0 {
		t.Fatalf("осмотрено ноль сравнений на %d файлах — разбор перестал видеть предмет, "+
			"и его молчание сказано ни о чём", parsed)
	}

	// (1) Объявление РОВНО ОДНО.
	if len(decls) != 1 {
		var where []string
		for _, d := range decls {
			where = append(where, fmt.Sprintf("%s:%d", d.File, d.Line))
		}
		sort.Strings(where)
		t.Fatalf("объявлений порога стойкости (%s) в дереве %d, обязано быть 1: %s\n\n"+
			"Ноль означает, что порога нет вовсе и любой ключ проходит. Больше одного — "+
			"два места об одном предмете; расходятся такие пары в сторону «принимаем "+
			"слабее», потому что ослабление невидимо, а ужесточение сразу заметно.",
			keyStrengthDeclName, len(decls), strings.Join(where, ", "))
	}

	// (2) Порог объявлен ЧИСЛОМ. Объявление, вернувшееся вычисляемым выражением,
	// сверять не с чем, и молчать гейт права не имеет.
	if len(decls[0].Literals) == 0 {
		t.Fatalf("объявление порога %s:%d не возвращает ни одного числового литерала — "+
			"порог обязан быть объявлен ЧИСЛОМ (F1-02). Вычисляемый порог не с чем "+
			"сверить: ни документации, ни соседней реализации, ни этому гейту.",
			decls[0].File, decls[0].Line)
	}
	t.Logf("объявление порога: %s:%d, числа %v", decls[0].File, decls[0].Line, decls[0].Literals)

	// (3) Второго объявления нет. Сравнения ВНУТРИ единственного объявления
	// находкой не являются — там порог и живёт.
	var findings []string
	for _, c := range second {
		if c.File == decls[0].File {
			continue
		}
		findings = append(findings, fmt.Sprintf("%s:%d  %s %s %d",
			c.File, c.Line, c.Expr, c.Op, c.Literal))
	}
	sort.Strings(findings)
	if len(findings) > 0 {
		t.Fatalf("стойкость ключа сравнивается с ЛИТЕРАЛОМ вне единственного объявления "+
			"порога — %d место(а):\n  %s\n\n"+
			"Каждое такое сравнение есть второе объявление порога: оно решает «принять или "+
			"отвергнуть» своим числом и разойдётся с объявленным при первой же правке.\n"+
			"Снятие: сравнивать с объявленным порогом (%s:%d), а не со своим числом.",
			len(findings), strings.Join(findings, "\n  "), decls[0].File, decls[0].Line)
	}
}
