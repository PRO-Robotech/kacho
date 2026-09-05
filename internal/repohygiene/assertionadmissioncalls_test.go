// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// assertionadmissioncalls_test.go — допуск однократности обращается к базе
// РОВНО ОДИН раз (приёмка F2, сценарий F2-28).
//
// # Предмет
//
// «Не предъявлялось ли уже» и «погасить» неделимы, и неделимыми их делает
// первичный ключ таблицы. Пара «посмотреть — потом записать» ровно та
// реализация, которую запрещает ban #10: два одновременных предъявления одного
// утверждения промахиваются ОБА мимо чужой ещё не записанной строки и проходят
// ОБА.
//
// Обзор диффа этого не различает: обе формы читаются как «проверили и
// записали», и обе зелены на всех последовательных пробах — окна между чтением
// и записью при последовательном прогоне не существует. Различает ЧИСЛО
// обращений, и стеречь его больше нечем.
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
	// assertionAdmissionFile — файл владельца хранилища однократности.
	assertionAdmissionFile = "services/iam/internal/repo/kaname/pg/client_assertion_replay_repo.go"
	// assertionAdmissionFunc — функция ДОПУСКА: та, что решает, принять ли
	// предъявление.
	assertionAdmissionFunc = "ClientAssertionReplayRepo.Redeem"
	// assertionReaperFunc — законный близнец: сборщик. Он не решает, принять
	// ли предъявление, поэтому неделимости от него не требуется, и число
	// обращений ему не предписано.
	assertionReaperFunc = "ClientAssertionReplayRepo.Reap"
	// assertionAdmissionCallBudget — сколько обращений к базе допуску положено.
	assertionAdmissionCallBudget = 1
)

// sortedFuncNames — имена разобранных функций для текста отказа: без них
// «функция не найдена» неотличимо от «разбор сломался».
func sortedFuncNames(byFunc map[string]FunctionDatabaseCalls) []string {
	out := make([]string, 0, len(byFunc))
	for name := range byFunc {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// TestAssertionAdmissionIsASingleDatabaseCall — сам гейт.
func TestAssertionAdmissionIsASingleDatabaseCall(t *testing.T) {
	root := repoRoot(t)
	tt := newTrackedTree(t, root)

	// (1) Предпосылка: файл владельца лежит в СОСТАВЕ дерева, а не на диске.
	// Хранилище, снятое из индекса, оставило бы гейт зелёным навсегда.
	if !tt.hasFile(assertionAdmissionFile) {
		t.Fatalf("файла владельца однократности (%s) в составе дерева НЕТ. Либо хранилище "+
			"снято — тогда снимается и этот гейт вместе с ним, — либо оно переехало, и "+
			"гейт стережёт координату, которой больше не существует.", assertionAdmissionFile)
	}

	src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(assertionAdmissionFile)))
	if err != nil {
		t.Fatalf("чтение %s: %v", assertionAdmissionFile, err)
	}
	byFunc, census, err := ScanDatabaseCallsByFunction(assertionAdmissionFile, src)
	if err != nil {
		t.Fatalf("разбор %s: %v", assertionAdmissionFile, err)
	}

	t.Logf("перепись: файл %s, объявлений структур %d, полей-носителей соединения %d (%s), "+
		"функций осмотрено %d, вызовов осмотрено %d, из них обращений к базе %d",
		assertionAdmissionFile, census.Structs, len(census.Handles),
		strings.Join(census.Handles, ", "), census.Functions, census.Calls, census.DBCalls)

	// (2) Предпосылка разбора: носитель соединения опознан. Ноль носителей
	// означает, что признак «обращение к базе» не производится ничем, и число
	// вызовов у ЛЮБОЙ функции вышло бы нулевым — гейт молчал бы и на паре
	// «посмотреть — записать».
	if len(census.Handles) == 0 {
		t.Fatalf("в %s не опознано ни одного поля-носителя соединения: тип драйвера в "+
			"объявлении структуры не назван ни одним из признаков %v. Признак «обращение "+
			"к базе» не производится, поэтому ноль обращений сказано ни о чём.",
			assertionAdmissionFile, databaseDriverMarkers)
	}
	if census.DBCalls == 0 {
		t.Fatalf("в %s осмотрено %d вызовов и признано обращениями к базе НОЛЬ — хранилище "+
			"перестало ходить в базу либо разбор перестал видеть предмет. И то и другое "+
			"делает молчание гейта утверждением ни о чём.",
			assertionAdmissionFile, census.Calls)
	}

	// (3) Предпосылка: обе функции на месте. Допуск — предмет гейта, сборщик —
	// его законный близнец; исчезновение любого меняет смысл молчания.
	admission, ok := byFunc[assertionAdmissionFunc]
	if !ok {
		t.Fatalf("функция допуска %s в %s не найдена; разобраны: %v",
			assertionAdmissionFunc, assertionAdmissionFile, sortedFuncNames(byFunc))
	}
	reaper, ok := byFunc[assertionReaperFunc]
	if !ok {
		t.Fatalf("сборщик %s в %s не найден; разобраны: %v.\n\n"+
			"Сборщик обязан существовать: строка погашения живёт до истечения утверждения "+
			"— не дольше и не короче. Короче нельзя (повтор становится законным), дольше "+
			"нельзя (хранилище растёт без границы, и темп роста выбирает предъявитель).",
			assertionReaperFunc, assertionAdmissionFile, sortedFuncNames(byFunc))
	}

	// (4) Находка: число обращений допуска отличается от одного.
	if len(admission.Calls) != assertionAdmissionCallBudget {
		var where []string
		for _, c := range admission.Calls {
			where = append(where, fmt.Sprintf("%s:%d  %s через %s", c.File, c.Line, c.Verb, c.Handle))
		}
		t.Fatalf("допуск однократности (%s:%d) обращается к базе %d раз(а) при бюджете %d:\n  %s\n\n"+
			"Допуск обязан быть ОДНИМ оператором: «не предъявлялось ли уже» и «погасить» "+
			"неделимы, и неделимыми их делает первичный ключ таблицы. Разнесённые на два "+
			"обращения, они дают ровно тот check-then-act, который запрещает ban #10: два "+
			"одновременных предъявления одного утверждения промахиваются ОБА мимо чужой ещё "+
			"не записанной строки и проходят ОБА. Такая реализация проходит все "+
			"последовательные пробы и остаётся сломанной ровно там, где однократность и нужна.\n"+
			"Снятие: один INSERT … ON CONFLICT DO NOTHING, повтор читается по числу "+
			"затронутых строк.",
			admission.Name, admission.Line, len(admission.Calls), assertionAdmissionCallBudget,
			strings.Join(where, "\n  "))
	}

	t.Logf("допуск %s:%d — обращений к базе %d (бюджет %d)",
		assertionAdmissionFile, admission.Line, len(admission.Calls), assertionAdmissionCallBudget)
	t.Logf("законный близнец %s:%d — обращений к базе %d, и число ему НЕ предписано: "+
		"сборщик не решает, принять ли предъявление",
		assertionAdmissionFile, reaper.Line, len(reaper.Calls))
}
