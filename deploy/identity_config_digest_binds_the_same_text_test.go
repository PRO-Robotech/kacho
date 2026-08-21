// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_config_digest_binds_the_same_text_test.go — отпечаток, привязывающий
// шаблон пода к настройкам, обязан считаться по ТОМУ ЖЕ тексту, из которого
// рендерится сама карта настроек.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Настройки приезжают в под томом и читаются процессом ОДИН РАЗ — на старте.
// Правка карты не меняет шаблон пода, значит под не перекатывается и процесс
// живёт со старым содержимым. Лечится отпечатком содержимого в шаблоне пода.
//
// Соседний гейт (deploy/tests/helm/config-rollout-binding-test.sh) считает, что
// привязок НЕ МЕНЬШЕ, чем потребляемых карт, и честно оговаривает свою границу:
// «она не доказывает, что привязка относится именно к своему объекту». Эта
// проверка закрывает ровно ту границу — но не рендером (рендер требует
// скачанных зависимостей и потому пропускается там, где их нет, а пропущенная
// проверка не краснеет никогда), а СВОЙСТВОМ, из которого соответствие следует
// по построению: и карта, и отпечаток зовут ОДИН И ТОТ ЖЕ именованный шаблон.
//
// Что ловится:
//   - завели карту настроек, а отпечаток не завели — правка не перекатит под;
//   - отпечаток считают по ДРУГОМУ тексту (копии, соседнему шаблону) — привязка
//     становится ложной ровно тогда, когда она нужна: содержимое изменилось,
//     отпечаток нет.
//
// ПОЧЕМУ ПЕРЕЧЕНЬ ВЫВОДИТСЯ. Ни имена карт, ни имена шаблонов здесь не выписаны:
// они находятся обходом дерева. Выписанный перечень разошёлся бы при заведении
// следующей карты молча — а именно этот случай проверка и стережёт.
package deploy_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// dataFromNamedTemplate — ключ карты настроек, чьё содержимое рендерится
// именованным шаблоном. Именно такая форма делает соответствие доказуемым.
var dataFromNamedTemplate = regexp.MustCompile(`(?m)^\s*([A-Za-z0-9._-]+):\s*\|\s*\n\s*\{\{-?\s*include\s+"([^"]+)"`)

// digestOverNamedTemplate — вызов именованного шаблона внутри вычисления
// отпечатка. Требуется именно `sha256sum`: имя переменной подделать легко,
// вычисление — нет.
var digestOverNamedTemplate = regexp.MustCompile(`include\s+"([^"]+)"`)

func TestIdentityConfigDigestIsComputedOverTheVeryTextTheConfigMapRenders(t *testing.T) {
	tpls, err := filepath.Glob(filepath.Join(umbrellaDir, "charts", "*", "templates", "*.yaml"))
	if err != nil {
		t.Fatalf("обход шаблонов подчартов: %v", err)
	}
	profiles, err := filepath.Glob(filepath.Join(umbrellaDir, "values*.yaml"))
	if err != nil {
		t.Fatalf("обход профилей: %v", err)
	}
	if len(tpls) == 0 || len(profiles) == 0 {
		t.Fatalf("корпус пуст: шаблонов %d, профилей %d — проверять нечего, "+
			"и это отказ, а не успех", len(tpls), len(profiles))
	}

	// ── что рендерится именованным шаблоном: имя карты → шаблоны её ключей ──
	byConfigMap := map[string][]string{}
	for _, p := range tpls {
		b, rerr := os.ReadFile(p) // #nosec G304 -- пути получены обходом собственного дерева
		if rerr != nil {
			t.Fatalf("чтение %s: %v", p, rerr)
		}
		body := string(b)
		if !strings.Contains(body, "kind: ConfigMap") {
			continue
		}
		tail := ""
		for _, m := range configMapNameDecl.FindAllStringSubmatch(body, -1) {
			if t2 := nameTail.FindStringSubmatch(m[1]); t2 != nil {
				tail = t2[1]
				break
			}
		}
		if tail == "" {
			continue
		}
		for _, m := range dataFromNamedTemplate.FindAllStringSubmatch(body, -1) {
			byConfigMap[tail] = append(byConfigMap[tail], m[2])
		}
	}
	if len(byConfigMap) == 0 {
		t.Fatalf("ни одна карта настроек не рендерится именованным шаблоном — " +
			"либо форма сменилась, либо обход слеп; «ноль находок» здесь " +
			"неотличимо от «ноль прочитанного», поэтому это отказ")
	}

	// ── чей отпечаток объявлен в профиле ─────────────────────────────────────
	checkedProfiles, checkedPairs := 0, 0
	for _, prof := range profiles {
		b, rerr := os.ReadFile(prof) // #nosec G304 -- пути получены обходом собственного дерева
		if rerr != nil {
			t.Fatalf("чтение %s: %v", prof, rerr)
		}
		body := string(b)

		digested := map[string]bool{}
		for _, line := range strings.Split(body, "\n") {
			if !strings.Contains(line, "sha256sum") {
				continue
			}
			for _, m := range digestOverNamedTemplate.FindAllStringSubmatch(line, -1) {
				digested[m[1]] = true
			}
		}

		// Карта считается смонтированной этим профилем, если он называет её
		// источником тома. Профиль, ничего не монтирующий, под проверку не
		// подпадает — и это не послабление: монтировать нечего.
		mounted := map[string]bool{}
		for _, name := range volumeSourceNames(body) {
			mounted[name] = true
		}
		touched := false
		for tail, names := range byConfigMap {
			hit := ""
			for name := range mounted {
				if strings.HasSuffix(name, tail) {
					hit = name
					break
				}
			}
			if hit == "" {
				continue
			}
			touched = true
			for _, nt := range names {
				checkedPairs++
				if digested[nt] {
					continue
				}
				t.Errorf("%s монтирует карту настроек %q, чьё содержимое рендерит "+
					"именованный шаблон %q, но ОТПЕЧАТКА этого шаблона в профиле нет: "+
					"правка настроек не изменит шаблон пода, под не перекатится и "+
					"процесс останется со старым содержимым",
					filepath.Base(prof), hit, nt)
			}
		}
		if touched {
			checkedProfiles++
		}
	}

	tails := make([]string, 0, len(byConfigMap))
	for k := range byConfigMap {
		tails = append(tails, k)
	}
	sort.Strings(tails)
	t.Logf("осмотрено: шаблонов подчартов %d, профилей %d; карт, рендерящихся именованным "+
		"шаблоном %d (%s); профилей, монтирующих такие карты %d; сверено пар «карта↔отпечаток» %d",
		len(tpls), len(profiles), len(byConfigMap), strings.Join(tails, ", "),
		checkedProfiles, checkedPairs)

	if checkedPairs == 0 {
		t.Fatalf("ни одна пара «карта↔отпечаток» не сверена — либо профили перестали " +
			"монтировать такие карты, либо обход слеп; пустой результат здесь " +
			"НЕ означает «всё хорошо»")
	}
}
