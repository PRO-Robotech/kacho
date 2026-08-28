// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// awaitingprobes_test.go — гейт «проба, ждущая своего условия, истекает ВМЕСТЕ С
// НИМ».
//
// # Предмет
//
// Сквозная проба, чьё условие в дереве ещё не создано, не имеет права ни лежать
// в автоматическом наборе (её job входит в набор обязательных контекстов, и
// заведомо красная проба сделала бы красным каждый запрос на слияние), ни
// прятаться под пропуском (пропуск подаёт «не выполнилось» как вердикт).
//
// Предписанная форма — счётный поимённый долг в отдельном каталоге. У долга есть
// ровно одна опасность: он переживает своё условие. Появится владелец журнала —
// и пробы останутся лежать, никем не исполняемые, а перечень будет утверждать,
// что их «нельзя запустить», когда уже можно.
//
// # Как он истекает
//
// От ФАКТА В ДЕРЕВЕ, а не от чьей-то памяти: как только хоть один профиль
// развёртывания объявит владельца журнала, каталог обязан опустеть.
//
// # Почему проверка предпосылки стоит здесь
//
// Гейт держится на двух фактах о дереве: каталог ожидания существует, и
// объявление владельцев читается из чартов. Исчезнет первое — гейту нечего
// охранять; исчезнет второе — он зелен всегда. Оба заявляются переписью.

// awaitingProbesDir — каталог проб, ждущих своего условия.
const awaitingProbesDir = "ui-future/e2e/specs-awaiting-journal-owner"

// sweptProbesDir — каталог, который исполняет прогонщик.
const sweptProbesDir = "ui-future/e2e/specs"

// ownersDeclarationRe — объявление владельцев журнала в профиле развёртывания.
//
// Пустое значение (`owners: ""`) означает «владелец не объявлен» и условием НЕ
// является: ровно так объявление и выглядит, пока предмета нет.
var ownersDeclarationRe = regexp.MustCompile(`(?m)^\s*owners:\s*(.*)$`)

// verifiesLinkRe — ссылка пробы на задачу, которая заведёт её условие.
var verifiesLinkRe = regexp.MustCompile(`//\s*verifies\s+#\d+`)

// TestProbesAwaitingTheirConditionExpireWhenItArrives — долг истекает от факта.
func TestProbesAwaitingTheirConditionExpireWhenItArrives(t *testing.T) {
	root := repoRoot(t)

	// ── половина первая: объявлен ли владелец журнала хоть где-нибудь ────────
	chartsRead := 0
	declared := map[string]string{}
	walkErr := filepath.WalkDir(filepath.Join(root, "gateway", "deploy"),
		func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".yaml") {
				return nil
			}
			body, readErr := os.ReadFile(path) // #nosec G304 -- обход собственного дерева
			if readErr != nil {
				return readErr
			}
			text := string(body)
			if !strings.Contains(text, "subscriptionStream:") {
				return nil
			}
			chartsRead++
			if m := ownersDeclarationRe.FindStringSubmatch(text); m != nil {
				value := strings.Trim(strings.TrimSpace(m[1]), `"'`)
				if value != "" {
					rel, _ := filepath.Rel(root, path)
					declared[rel] = value
				}
			}
			return nil
		})
	if walkErr != nil {
		t.Fatalf("обход профилей развёртывания: %v", walkErr)
	}

	// ── половина вторая: что лежит в каталоге ожидания ───────────────────────
	awaiting := make([]string, 0, 2)
	withoutLink := make([]string, 0, 2)
	dir := filepath.Join(root, awaitingProbesDir)
	entries, dirErr := os.ReadDir(dir)
	if dirErr != nil {
		t.Fatalf("каталог ожидания %s не читается (%v) — гейту нечего охранять, "+
			"и его молчание не означало бы отсутствия долга", awaitingProbesDir, dirErr)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".spec.ts") {
			continue
		}
		awaiting = append(awaiting, e.Name())
		body, readErr := os.ReadFile(filepath.Join(dir, e.Name())) // #nosec G304
		if readErr != nil {
			t.Fatalf("чтение %s: %v", e.Name(), readErr)
		}
		if !verifiesLinkRe.Match(body) {
			withoutLink = append(withoutLink, e.Name())
		}
	}

	t.Logf("перепись: профилей с объявлением подписки %d · из них назвали владельца %d %v · "+
		"проб в ожидании %d %v", chartsRead, len(declared), declared, len(awaiting), awaiting)

	if chartsRead == 0 {
		t.Fatal("ни один профиль не объявляет подписку — гейт ничего не читал, " +
			"и его зелёное неотличимо от пустого обхода")
	}

	if len(declared) > 0 && len(awaiting) > 0 {
		t.Errorf("владелец журнала ОБЪЯВЛЕН (%v), а пробы всё ещё лежат в ожидании: %v.\n"+
			"Условие создано — долг истёк: перенеси их в %s, иначе перечень утверждает "+
			"«нельзя запустить» там, где уже можно, и никто этого не заметит",
			declared, awaiting, sweptProbesDir)
	}

	if len(withoutLink) > 0 {
		t.Errorf("пробы в ожидании без ссылки на задачу, которая заведёт их условие: %v.\n"+
			"Долг без предмета неотличим от брошенной работы: снять его будет некому", withoutLink)
	}
}
