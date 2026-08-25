// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// retentionsweeper_test.go — ОБЪЯВЛЕННЫЙ УБОРЩИК ОБЯЗАН ИМЕТЬ ПРОД-ВЫЗЫВАЮЩЕГО
// (задача #1292, приёмка `retention-sweep-has-a-caller.md`, RET-SWP-10…14).
//
// # Предмет
//
// Уборщик без вызывающего — это не «мёртвый код». Это таблица, растущая без
// ограничения, и утверждения дерева о работающем сборщике, ставшие ложью. В iam
// таких уборщиков было два, прод-вызывающих — ноль у обоих, и у одного не было
// даже пробы; при этом ВОСЕМЬ мест дерева говорили в настоящем времени, что
// сборщик убирает.
//
// Провязка закрывает это один раз. Свойство впредь держит гейт: текст, ставший
// истинным однажды, снова становится ложью молча.
//
// # Почему единица счёта — «служба», а не «каталог, который импортирует»
//
// Первое, что просится, — засчитывать вызывающего из своего каталога либо из
// каталога, который его ИМПОРТИРУЕТ. На этом дереве такое правило неверно:
// уборщик провязывается ЧЕРЕЗ ПОРТ, и каталог реестра уборки хранилища не
// импортирует вовсе — он принимает интерфейс. Правило объявило бы находкой
// живую провязку.
//
// Поэтому граница — СЛУЖБА (`services/<имя>`, `gateway`, `pkg`, `internal`).
// Она решает то, ради чего заводилась: имя `Reap` в дереве носят два разных
// типа, и без границы вызывающий шлюза покрывал бы уборщика iam. Остаток назван
// прямо: однофамильцы ВНУТРИ одной службы гейтом не различаются.
package repohygiene

import (
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// retentionSweeperRoots — где ищутся уборщики и их вызывающие.
var retentionSweeperRoots = []string{"services", "gateway", "pkg", "internal"}

// retentionSweeperLedger — уборщики ЧУЖИХ полос, у которых вызывающего нет и
// чинит их не эта работа.
//
// Не прощение: каждая запись несёт предмет и номер задачи, а гейт РОНЯЕТ прогон
// на записи, у которой уборщик получил вызывающего либо исчез (RET-SWP-13).
// Послабление без предмета унаследует следующая слепая зона.
var retentionSweeperLedger = []struct {
	Qualified string
	Subject   string
	Issue     string
}{
	{
		Qualified: "Store.PurgeExpiredDPoPProofs",
		Subject: "уборщик защиты от повтора на крае: прод-вызывающих 0, при том что его собственная " +
			"шапка утверждает, будто уборку зовёт сборщик хранилища — а тот зовёт ДРУГОЙ метод",
		Issue: "#1293",
	},
	{
		Qualified: "targetGroupWriter.DeleteTargetsDrained",
		Subject: "мёртвый дубль: уборку дренированных целей делает свой оператор раннера, а комментарий " +
			"порта называет раннера вызывающим ЭТОГО метода. Исход иной — снять, а не провязать",
		Issue: "#1294",
	},
}

// serviceUnitOf — служба, которой принадлежит каталог.
func serviceUnitOf(dir string) string {
	parts := strings.Split(dir, "/")
	if len(parts) >= 2 && parts[0] == "services" {
		return parts[0] + "/" + parts[1]
	}
	if len(parts) > 0 {
		return parts[0]
	}
	return dir
}

// retentionSweeperVerdict — ЧИСТОЕ суждение по уже прочитанному дереву.
//
// Отделено от обхода намеренно: инъекция подаёт сюда синтетический корпус и
// проверяет, что суждение способно упасть И способно смолчать, — на настоящем
// дереве ни того ни другого не показать, не сломав его.
func retentionSweeperVerdict(
	sweepers []RetentionSweeper,
	callersByName map[string]map[string][]string, // имя метода → служба → объемлющие функции
	ledger map[string]string, // Qualified → номер задачи
) (findings []string, stale []string, wired int) {
	for _, s := range sweepers {
		unit := serviceUnitOf(s.Dir)
		self := s.Qualified()
		hasCaller := false
		for _, enclosing := range callersByName[s.Name][unit] {
			if enclosing == self {
				// Уборщик, зовущий сам себя, вызывающим себе не является:
				// иначе рекурсия объявила бы его провязанным.
				continue
			}
			hasCaller = true
			break
		}
		if hasCaller {
			wired++
			if issue, listed := ledger[self]; listed {
				stale = append(stale, s.File+":"+strconv.Itoa(s.Line)+" "+self+
					" — запись ведомости "+issue+" ПОТЕРЯЛА ПРЕДМЕТ: у уборщика появился вызывающий. "+
					"Снимите запись: послабление без предмета унаследует следующая слепая зона")
			}
			continue
		}
		if _, listed := ledger[self]; listed {
			continue
		}
		findings = append(findings, s.File+":"+strconv.Itoa(s.Line)+" "+self+
			" — объявленный уборщик по сроку БЕЗ прод-вызывающего в службе "+unit+
			". Оператор: "+s.SQL+
			". Это не мёртвый код: таблица растёт без ограничения, а всякий текст дерева, "+
			"утверждающий, что сборщик работает, становится ложью. Исходов три — провязать; "+
			"снять уборщик вместе с этими утверждениями; либо завести ПРЕДМЕТ (задачу) и запись "+
			"в ведомости retentionSweeperLedger с номером и причиной")
	}
	// Запись ведомости, чьего уборщика в дереве больше НЕТ, — тоже находка.
	seen := map[string]bool{}
	for _, s := range sweepers {
		seen[s.Qualified()] = true
	}
	for q, issue := range ledger {
		if !seen[q] {
			stale = append(stale, q+" — запись ведомости "+issue+" ПОТЕРЯЛА ПРЕДМЕТ: "+
				"уборщика с таким именем в дереве нет вовсе. Снимите запись")
		}
	}
	sort.Strings(findings)
	sort.Strings(stale)
	return findings, stale, wired
}

// TestDeclaredRetentionSweepersHaveAProductionCaller — сам гейт (RET-SWP-10).
func TestDeclaredRetentionSweepersHaveAProductionCaller(t *testing.T) {
	root := repoRoot(t)

	var (
		sweepers []RetentionSweeper
		scanned  int
		census   RetentionSweeperCensus
	)
	callersByName := map[string]map[string][]string{}

	walkOwnerRegisterGoFiles(t, root, retentionSweeperRoots, func(rel string, body []byte) {
		scanned++
		dir := path.Dir(filepath.ToSlash(rel))

		found, c, err := ScanRetentionSweepers(rel, dir, body)
		if err != nil {
			t.Fatalf("разбор %s: %v", rel, err)
		}
		census.Functions += c.Functions
		census.Literals += c.Literals
		census.Deletes += c.Deletes
		census.Sweepers += c.Sweepers
		sweepers = append(sweepers, found...)

		calls, err := ScanMethodCallNames(rel, body)
		if err != nil {
			t.Fatalf("разбор вызовов %s: %v", rel, err)
		}
		unit := serviceUnitOf(dir)
		for name, enclosings := range calls {
			if callersByName[name] == nil {
				callersByName[name] = map[string][]string{}
			}
			callersByName[name][unit] = append(callersByName[name][unit], enclosings...)
		}
	})

	ledger := map[string]string{}
	for _, e := range retentionSweeperLedger {
		ledger[e.Qualified] = e.Issue
	}
	findings, stale, wired := retentionSweeperVerdict(sweepers, callersByName, ledger)

	// Объём осмотренного — ОТДЕЛЬНОЕ утверждение: «ноль находок» обязано быть
	// отличимо от «ноль прочитанного».
	t.Logf("перепись: файлов Go прочитано %d, функций осмотрено %d, строковых литералов %d, "+
		"из них удаляют строки %d, признаны уборщиками по сроку %d; "+
		"с прод-вызывающим %d; в ведомости чужих полос %d; находок %d",
		scanned, census.Functions, census.Literals, census.Deletes, len(sweepers),
		wired, len(retentionSweeperLedger), len(findings))

	// Предпосылки разбора. Пустой обход и ноль распознанных уборщиков означают
	// не благополучие, а слепоту: гейт молчал бы и на дереве без единой уборки.
	if scanned == 0 {
		t.Fatal("прочитано ноль файлов — гейт не читал дерева, и его молчание ничего не значит")
	}
	if len(sweepers) == 0 {
		t.Fatal("уборщиков по сроку в дереве не найдено ни одного — предикат разбора разъехался с " +
			"кодом. Уборка в этом дереве есть (её объявляют iam, gateway, nlb, registry), значит " +
			"молчание гейта означает слепоту, а не отсутствие предмета")
	}

	for _, s := range stale {
		t.Errorf("ведомость: %s", s)
	}
	for _, f := range findings {
		t.Errorf("уборщик без вызывающего: %s", f)
	}
}

// TestRetentionSweeperGateIsSilentOnForeignInjectionFixtures — RET-SWP-12.
//
// Фикстуры инъекции ЧУЖИХ гейтов содержат `DELETE … WHERE expires_at <= $1` как
// законный близнец. Они лежат в `_test.go`, то есть в обход не попадают вовсе, —
// и это УТВЕРЖДАЕТСЯ, а не подразумевается: обход, однажды переставший
// пропускать пробы, объявил бы находкой чужую фикстуру, а её «починка»
// сломала бы гейт, которому она принадлежит.
func TestRetentionSweeperGateIsSilentOnForeignInjectionFixtures(t *testing.T) {
	root := repoRoot(t)
	const foreign = "internal/repohygiene/assertionadmissioncalls_injection_test.go"

	// Предпосылка: фикстура на месте и в самом деле несёт уборщика. Проба,
	// потерявшая свой предмет, молчала бы ни о чём.
	tt := newTrackedTree(t, root)
	if !tt.hasFile(foreign) {
		t.Fatalf("фикстуры чужого гейта (%s) в составе дерева нет: проба потеряла предмет "+
			"и её молчание больше ничего не утверждает", foreign)
	}

	seen := 0
	walkOwnerRegisterGoFiles(t, root, retentionSweeperRoots, func(rel string, _ []byte) {
		if filepath.ToSlash(rel) == foreign {
			seen++
		}
	})
	if seen != 0 {
		t.Errorf("обход гейта прочитал фикстуру чужого гейта (%s): её синтетический уборщик "+
			"станет находкой, а «починка» сломает гейт, которому фикстура принадлежит", foreign)
	}
}
