// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// tokenclaimsinglesource_test.go — состав утверждений выданного токена
// объявлен В ОДНОМ месте, которым пользуются обе стороны выдачи
// (приёмка F2, сценарий F2-42, §2.11).
//
// # Предмет
//
// С этой фазы токен принципалу выдают ДВА пути: обратный вызов прежнего
// провайдера, пока он жив, и наш собственный эндпоинт. Требование задачи —
// множества имён и значений утверждений у обоих путей совпадают для одного и
// того же принципала.
//
// Решение §2.11 — не «сверили один раз», а ОДНО объявление: пока перечень живёт
// в двух местах, различие между ними не является ничьей находкой. Оно не
// выражено и потому не может покраснеть; первая же правка одной стороны
// разойдётся с другой молча, и разойдётся она у ПРИНЦИПАЛА, чей токен выдан не
// тем путём.
//
// # Находкой является ВТОРАЯ СБОРКА, а не второе употребление
//
// Потребителей у единственной сборки должно быть много — они и есть цель: `own
// lane` заводит не второй состав, а второй СПОСОБ ДОЙТИ до того же. Гейт,
// считающий употребления, потребовал бы от дерева одного вызывающего, то есть
// запретил бы ровно то, ради чего сборка вынесена.
//
// # Порог в три ключа — замерен, а не выбран
//
// Префикс утверждений встречается и вне состава. На пороге в ДВА ключа в
// находки попадает чтение контекста на крае (два ключа), на пороге в ТРИ —
// только настоящие сборки: пять у владельца и одна в ведомости. Порог назван
// числом, чтобы его можно было перемерить, а не поверить.
//
// # Что здесь считается деревом
//
// Индекс git — то же множество, которое увидит свежий клон и CI.
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
	// claimOwnerFile — единственный дом сборки состава.
	claimOwnerFile = "services/iam/internal/service/token_enrichment_service.go"
	// claimKeyPrefix — префикс имени утверждения платформы.
	claimKeyPrefix = "kaname_"
	// claimMinKeys — сколько РАЗНЫХ ключей делают место сборкой состава.
	claimMinKeys = 3
	// claimLanesFloor — сколько РАЗНЫХ функций обязаны звать сборщики.
	// «Обе стороны» — это две точки входа, а не два вызова из одной.
	claimLanesFloor = 2
	// claimCensusFloor — порог переписи.
	claimCensusFloor = 1000
)

// claimBuilders — сборщики состава: то, чем состав ПОТРЕБЛЯЕТСЯ.
//
// Перечень закрыт и живёт рядом с владельцем: сборщик, заведённый и сюда не
// внесённый, потребителями считаться не будет — и «обе стороны» окажется
// недосчитанным. Это видно по счётчику полос, а не молчит.
var claimBuilders = map[string]bool{
	"userClaims":      true,
	"saClaims":        true,
	"federatedClaims": true,
	"userTokenClaims": true,
	"MinimalClaims":   true,
}

// claimDebtEntry — вторая сборка состава, ещё не сведённая к единственной.
type claimDebtEntry struct {
	// File — путь сборки относительно корня дерева.
	File string
	// Func — функция, внутри которой лежит сборка.
	Func string
	// Why — почему сборка ещё живёт и кто это решает.
	Why string
	// Until — предикат снятия: наблюдаемое условие, при котором записи здесь
	// больше не место.
	Until string
}

// claimDebt — ведомость вторых сборок.
//
// Замер на день заведения гейта: сборок состава в дереве ШЕСТЬ — пять у
// владельца и одна ниже. Составы двух сторон УЖЕ разошлись: у записи ниже
// двенадцать ключей против пятнадцати у сборщика того же вида принципала.
// Это и есть предмет §2.11, и он найден переписью, а не обзором диффа.
// Сегодня ведомость ПУСТА, и это исход, к которому гейт вёл: сборок состава вне
// владельца в дереве ноль. Единственная запись покрывала обратный вызов
// обновления сессии прежнего провайдера, и её предикат снятия («файл перестаёт
// собирать состав сам») выполнен — сборка из файла ушла, запись ушла следом.
// Пустая ведомость гейт не роняет: падение на достижении собственной цели
// заставляло бы держать запись ради зелёного.
var claimDebt = []claimDebtEntry{}

// claimDebtDefects — что не так с самой ведомостью, безотносительно дерева.
func claimDebtDefects(entries []claimDebtEntry) []string {
	var out []string
	seen := map[string]bool{}
	for _, d := range entries {
		if d.File == "" || d.Func == "" {
			out = append(out, fmt.Sprintf("запись без координаты: %+v", d))
			continue
		}
		key := d.File + "\x00" + d.Func
		if seen[key] {
			out = append(out, fmt.Sprintf("дубль в ведомости: %s / %s", d.File, d.Func))
		}
		seen[key] = true
		if strings.TrimSpace(d.Why) == "" {
			out = append(out, fmt.Sprintf("%s: %s — запись без письменного обоснования",
				d.File, d.Func))
		}
		if strings.TrimSpace(d.Until) == "" {
			out = append(out, fmt.Sprintf("%s: %s — запись без предиката снятия: послабление, "+
				"которое не умеет истечь, переживает свой предмет", d.File, d.Func))
		}
	}
	sort.Strings(out)
	return out
}

// claimDebtStale — записи, которым больше нечего исключать.
func claimDebtStale(entries []claimDebtEntry, live map[string]bool) []string {
	var stale []string
	for _, d := range entries {
		if !live[d.File+"\x00"+d.Func] {
			stale = append(stale, fmt.Sprintf("%s: %s", d.File, d.Func))
		}
	}
	sort.Strings(stale)
	return stale
}

// claimTreeScan — сборки и потребители по всему дереву.
type claimTreeScan struct {
	Assemblies []ClaimAssembly
	Calls      []ClaimBuilderCall
	Parsed     int
	Census     ClaimCensus
}

func scanClaimTree(t *testing.T) claimTreeScan {
	t.Helper()
	root := repoRoot(t)
	tt := newTrackedTree(t, root)

	var rels []string
	for rel := range tt.files {
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") || skipPath(rel) {
			continue
		}
		rels = append(rels, rel)
	}
	sort.Strings(rels)

	var out claimTreeScan
	for _, rel := range rels {
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		out.Parsed++
		as, c1, err := ScanClaimAssemblies(rel, src, claimKeyPrefix, claimMinKeys)
		if err != nil {
			t.Fatalf("разбор сборок %s: %v", rel, err)
		}
		cs, c2, err := ScanClaimBuilderCalls(rel, src, claimBuilders)
		if err != nil {
			t.Fatalf("разбор вызовов %s: %v", rel, err)
		}
		out.Assemblies = append(out.Assemblies, as...)
		out.Calls = append(out.Calls, cs...)
		out.Census.MapLiterals += c1.MapLiterals
		out.Census.EmptyMapLiterals += c1.EmptyMapLiterals
		out.Census.KeyedLiterals += c1.KeyedLiterals
		out.Census.Calls += c2.Calls
	}
	return out
}

// TestTokenClaimsAreAssembledInOnePlace — сам гейт.
func TestTokenClaimsAreAssembledInOnePlace(t *testing.T) {
	scan := scanClaimTree(t)

	lanes := map[string]bool{}
	for _, c := range scan.Calls {
		lanes[c.Func] = true
	}
	var ownerAssemblies, outside []ClaimAssembly
	for _, a := range scan.Assemblies {
		if a.File == claimOwnerFile {
			ownerAssemblies = append(ownerAssemblies, a)
			continue
		}
		outside = append(outside, a)
	}

	t.Logf("перепись: не-тестовых файлов Go разобрано %d, литералов отображения прочитано %d "+
		"(из них пустых %d, с ключами состава %d), вызовов осмотрено %d; сборок состава "+
		"(>=%d ключей с префиксом %q) найдено %d — у владельца %d, вне его %d; полос, "+
		"зовущих сборщики, %d; ведомость вторых сборок: %d записей",
		scan.Parsed, scan.Census.MapLiterals, scan.Census.EmptyMapLiterals,
		scan.Census.KeyedLiterals, scan.Census.Calls, claimMinKeys, claimKeyPrefix,
		len(scan.Assemblies), len(ownerAssemblies), len(outside), len(lanes), len(claimDebt))

	if scan.Parsed < claimCensusFloor {
		t.Fatalf("перепись обвалилась: разобрано %d файлов при пороге %d", scan.Parsed, claimCensusFloor)
	}
	if scan.Census.KeyedLiterals == 0 {
		t.Fatalf("на %d файлах не найдено НИ ОДНОГО литерала с ключами состава — разбор "+
			"перестал видеть предмет, и его молчание сказано ни о чём", scan.Parsed)
	}

	// (1) Предпосылка: единственное объявление существует. Ноль сборок у
	// владельца означает, что состав перестал быть выражен, и «вторая сборка»
	// перестала быть отличима от первой.
	if len(ownerAssemblies) == 0 {
		t.Fatalf("сборок состава у владельца (%s) НОЛЬ при %d сборках в дереве — состав "+
			"переехал, и гейт стережёт координату, которой больше не существует",
			claimOwnerFile, len(scan.Assemblies))
	}

	// (2) Предпосылка: единственным объявлением пользуются ОБЕ стороны. Одна
	// полоса означает, что второй путь выдачи собирает состав сам либо не
	// существует, — и требование §2.11 нечем проверить.
	if len(lanes) < claimLanesFloor {
		var where []string
		for l := range lanes {
			where = append(where, l)
		}
		sort.Strings(where)
		t.Fatalf("сборщики состава зовутся из %d полосы (%s) при пороге %d.\n\n"+
			"«Объявлено в одном месте, которым пользуются ОБЕ стороны» проверяемо только "+
			"тогда, когда сторон две: одна полоса означает, что второй путь выдачи собирает "+
			"состав сам либо его нет вовсе.", len(lanes), strings.Join(where, ", "), claimLanesFloor)
	}

	// (3) Находка: вторая сборка вне владельца, не названная ведомостью.
	debt := map[string]bool{}
	for _, d := range claimDebt {
		debt[d.File+"\x00"+d.Func] = true
	}
	var fresh []string
	for _, a := range outside {
		if debt[a.File+"\x00"+a.Func] {
			continue
		}
		fresh = append(fresh, fmt.Sprintf("%s:%d  %s — %d ключей", a.File, a.Line, a.Func, len(a.Keys)))
	}
	sort.Strings(fresh)
	if len(fresh) > 0 {
		t.Fatalf("состав утверждений собирается ВНЕ %s и не назван ведомостью — %d место(а):\n  %s\n\n"+
			"Пока перечень живёт в двух местах, различие между ними не является ничьей "+
			"находкой: оно НЕ ВЫРАЖЕНО и потому не может покраснеть. Первая же правка одной "+
			"стороны разойдётся с другой молча — и разойдётся у ПРИНЦИПАЛА, чей токен выдан "+
			"не тем путём. Проверка «есть поле X» этого не ловит: она зелена на составе, "+
			"потерявшем поле Y.\n"+
			"Исходов три: звать сборщик единственного объявления · снять сборку вместе с "+
			"путём, который её подпирал · завести запись ведомости с причиной и предикатом "+
			"снятия.", claimOwnerFile, len(fresh), strings.Join(fresh, "\n  "))
	}

	for _, a := range ownerAssemblies {
		t.Logf("единственное объявление состава: %s:%d %s (%d ключей)",
			a.File, a.Line, a.Func, len(a.Keys))
	}
	var lanesList []string
	for l := range lanes {
		lanesList = append(lanesList, l)
	}
	sort.Strings(lanesList)
	t.Logf("полосы, пользующиеся единственным объявлением (их сколько угодно): %s",
		strings.Join(lanesList, ", "))
}

// TestClaimDebtIsWellFormed — каждая запись ведомости несёт координату,
// обоснование и предикат снятия.
func TestClaimDebtIsWellFormed(t *testing.T) {
	t.Logf("ведомость: %d записей", len(claimDebt))
	for _, bad := range claimDebtDefects(claimDebt) {
		t.Error(bad)
	}
}

// TestClaimDebtExpiresOnItsOwn — запись, которой больше нечего исключать, роняет
// прогон.
func TestClaimDebtExpiresOnItsOwn(t *testing.T) {
	scan := scanClaimTree(t)
	live := map[string]bool{}
	for _, a := range scan.Assemblies {
		if a.File == claimOwnerFile {
			continue
		}
		live[a.File+"\x00"+a.Func] = true
	}
	t.Logf("перепись: сборок вне владельца %d, записей ведомости %d", len(live), len(claimDebt))

	stale := claimDebtStale(claimDebt, live)
	if len(stale) > 0 {
		t.Fatalf("в ведомости %d записей, которым больше нечего исключать:\n%s\n\n"+
			"Сборка сведена к единственной или снята — запись обязана уйти тем же изменением. "+
			"Оставленная, она объявляет живым закрытый долг и освобождает место новому "+
			"дефекту с тем же путём.", len(stale), strings.Join(stale, "\n"))
	}
}
