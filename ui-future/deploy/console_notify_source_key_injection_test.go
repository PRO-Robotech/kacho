// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// console_notify_source_key_injection_test.go — доказательство, что гейт ключа
// модуля каталога СПОСОБЕН упасть, СПОСОБЕН смолчать и роняет ТОЛЬКО своё.
//
// Вход НАСТОЯЩИЙ: текст `subjects.ts` и словари журналов берутся у дерева, а
// инъекция меняет в тексте РОВНО ОДИН факт против законного близнеца — сам
// неизменённый текст. Подмена идёт в памяти: доказательство, трогающее дерево,
// испортило бы рабочую копию.
package deploy_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// realNotifySourceInput — текст карты и словари журналов дерева.
func realNotifySourceInput(t *testing.T) (string, map[string][]string) {
	t.Helper()
	root := repoRootFromTest(t)
	raw, err := os.ReadFile(filepath.Join(root, consoleSubjectsRel)) // #nosec G304 -- корень этого дерева
	if err != nil {
		t.Fatalf("карта предметов консоли не читается: %v", err)
	}
	dict, _ := journalDictionaries(t, root)
	return string(raw), dict
}

// judgeNotifySourceText — вердикт гейта на тексте через ТЕ ЖЕ функции.
func judgeNotifySourceText(t *testing.T, src string, dict map[string][]string,
	external map[string]string) (notifySourceVerdict, []string) {
	t.Helper()
	entries, unknown, found := notifySourceEntriesOf(src)
	if !found {
		t.Fatal("объявление NOTIFY_SOURCE_BY_OWNER не найдено в тексте инъекции")
	}
	return judgeNotifySourceKeys(consoleStreamSubjectsOf(src), dict, external, entries), unknown
}

// injectOnce заменяет ровно одно вхождение и падает, если вхождений не одно:
// инъекция, не попавшая в текст, дала бы зелёный по неверной причине.
func injectOnce(t *testing.T, src, from, to string) string {
	t.Helper()
	if n := strings.Count(src, from); n != 1 {
		t.Fatalf("инъекция %q → %q: вхождений %d, ожидалось одно — дельта против близнеца "+
			"не одно-фактна", from, to, n)
	}
	return strings.Replace(src, from, to, 1)
}

// TestConsoleNotifySourceKeyInjection_LawfulTwinIsSilent — близнец: неизменённый
// текст дерева. Молчание здесь — контроль для каждой инъекции ниже.
func TestConsoleNotifySourceKeyInjection_LawfulTwinIsSilent(t *testing.T) {
	src, dict := realNotifySourceInput(t)
	v, unknown := judgeNotifySourceText(t, src, dict, journalsOutsideThisTree)
	if len(v.Findings) != 0 || len(unknown) != 0 {
		t.Fatalf("гейт краснеет на законном дереве — контроль недействителен: %v %v", v.Findings, unknown)
	}
	if v.Owners == 0 {
		t.Fatal("владельцев 0 — молчание близнеца беспредметно")
	}
}

// TestConsoleNotifySourceKeyInjection_OwnerNameAsKeyIsFound — инъекция
// приёмки (Р10а): ключ балансировщика записан именем владельца.
func TestConsoleNotifySourceKeyInjection_OwnerNameAsKeyIsFound(t *testing.T) {
	src, dict := realNotifySourceInput(t)
	injected := injectOnce(t, src, `loadbalancer: "nlb"`, `loadbalancer: "loadbalancer"`)
	v, unknown := judgeNotifySourceText(t, injected, dict, journalsOutsideThisTree)
	t.Logf("находок на инъекции: %d", len(v.Findings))
	for _, f := range v.Findings {
		t.Logf("находка: %s", f)
	}
	if len(unknown) != 0 {
		t.Errorf("инъекция задела распознавание формы: %v", unknown)
	}
	if len(v.Findings) != 1 {
		t.Fatalf("находок %d, ожидалась одна — инъекция меняет один факт", len(v.Findings))
	}
	for _, want := range []string{`"loadbalancer"`, `"nlb"`, "services/nlb/internal/subscriptionjournal"} {
		if !strings.Contains(v.Findings[0], want) {
			t.Errorf("находка не называет %s: %s", want, v.Findings[0])
		}
	}
}

// TestConsoleNotifySourceKeyInjection_ExternalOwnerKeyIsFound — владелец с
// журналом в другом репозитории: ключ берётся у ведомости, не у имени.
func TestConsoleNotifySourceKeyInjection_ExternalOwnerKeyIsFound(t *testing.T) {
	src, dict := realNotifySourceInput(t)
	injected := injectOnce(t, src, `iam: "kaname"`, `iam: "iam"`)
	v, _ := judgeNotifySourceText(t, injected, dict, journalsOutsideThisTree)
	if len(v.Findings) != 1 || !strings.Contains(v.Findings[0], `"iam"`) ||
		!strings.Contains(v.Findings[0], "journalsOutsideThisTree") {
		t.Fatalf("ключ внешнего владельца не сверен с ведомостью: %v", v.Findings)
	}
}

// TestConsoleNotifySourceKeyInjection_MissingAndExtraEntriesAreFound — запись
// снята; запись о владельце, которого карта не называет.
func TestConsoleNotifySourceKeyInjection_MissingAndExtraEntriesAreFound(t *testing.T) {
	src, dict := realNotifySourceInput(t)

	t.Run("запись владельца снята", func(t *testing.T) {
		injected := injectOnce(t, src, `registry: "registry",`, ``)
		v, _ := judgeNotifySourceText(t, injected, dict, journalsOutsideThisTree)
		if len(v.Findings) != 1 || !strings.Contains(v.Findings[0], `"registry"`) {
			t.Fatalf("снятая запись не найдена: %v", v.Findings)
		}
	})
	t.Run("запись о владельце вне карты", func(t *testing.T) {
		injected := injectOnce(t, src, `vpc: "vpc",`, `vpc: "vpc", notify: "notify",`)
		v, _ := judgeNotifySourceText(t, injected, dict, journalsOutsideThisTree)
		if len(v.Findings) != 1 || !strings.Contains(v.Findings[0], `"notify"`) {
			t.Fatalf("лишняя запись не найдена: %v", v.Findings)
		}
	})
}

// TestConsoleNotifySourceKeyInjection_UnknownFormIsNotSilent — запись формой,
// которой разбор не знает, называется, а не выпадает из осмотренного.
func TestConsoleNotifySourceKeyInjection_UnknownFormIsNotSilent(t *testing.T) {
	src, dict := realNotifySourceInput(t)
	injected := injectOnce(t, src, `storage: "storage",`, `storage: STORAGE_KEY,`)
	_, unknown := judgeNotifySourceText(t, injected, dict, journalsOutsideThisTree)
	if len(unknown) != 1 || !strings.Contains(unknown[0], "STORAGE_KEY") {
		t.Fatalf("запись нераспознанной формы выпала молча: %v", unknown)
	}
}

// TestConsoleNotifySourceKeyInjection_ProseIsNotADeclaration — проза с парой
// «владелец: ключ» рядом с объявлением объявлением не является.
func TestConsoleNotifySourceKeyInjection_ProseIsNotADeclaration(t *testing.T) {
	src, dict := realNotifySourceInput(t)
	injected := injectOnce(t, src, `  compute: "compute",`,
		"  // loadbalancer: \"loadbalancer\" — так НЕ пишут\n  compute: \"compute\",")
	v, unknown := judgeNotifySourceText(t, injected, dict, journalsOutsideThisTree)
	if len(v.Findings) != 0 || len(unknown) != 0 {
		t.Fatalf("комментарий прочитан как запись: %v %v", v.Findings, unknown)
	}
}

// TestConsoleNotifySourceKeyInjection_DeclarationAbsentIsNotFound — объявления
// нет (дерево до F2): разбор отвечает «не найдено», а не пустым согласием.
func TestConsoleNotifySourceKeyInjection_DeclarationAbsentIsNotFound(t *testing.T) {
	src, _ := realNotifySourceInput(t)
	injected := injectOnce(t, src, "export const NOTIFY_SOURCE_BY_OWNER", "const RENAMED_SOURCE_MAP")
	if _, _, found := notifySourceEntriesOf(injected); found {
		t.Fatal("снятое объявление найдено — премиса «переход объявлен» не судится")
	}
}
