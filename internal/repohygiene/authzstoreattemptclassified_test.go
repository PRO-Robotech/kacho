// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/gitenv"
)

// TestAuthzStoreCallsClassifyTheirOutcome — каждое обращение к общему транспорту
// хранилища прав объявляет ПРИЧИНУ своего исхода в той же функции, где стоит.
//
// Разбор класса — в шапке authzstoreattemptclassified.go; здесь только обход
// дерева. Гейт обходит ВСЁ дерево, а не один каталог: скопируют адаптер рядом —
// свойство обязано требоваться и от копии.
func TestAuthzStoreCallsClassifyTheirOutcome(t *testing.T) {
	root := repoRoot(t)
	sources := map[string]string{}

	// Состав дерева спрашивается У ИНДЕКСА, а не у диска: обход диска не знает
	// правил игнорирования и потому судит чужой рабочий каталог — произведённые
	// файлы, чужие копии, остатки прогонов. Требование держит гейт
	// `TestTreeWalkersAskTheIndex`; его перечень долга закрыт для пополнения,
	// поэтому новый обход переводится сразу, а не вписывается исключением.
	out, err := gitenv.Command(root, "ls-files", "-z", "--", "*.go").Output()
	if err != nil {
		t.Fatalf("git ls-files: %v — состав дерева не установлен, и «ноль находок» "+
			"здесь означало бы «ноль прочитанного»", err)
	}
	for _, rel := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
		if rel == "" || skipPath(rel) {
			continue
		}
		b, readErr := os.ReadFile(filepath.Join(root, rel)) // #nosec G304 -- путь из индекса своего дерева
		if readErr != nil {
			t.Fatalf("чтение %s: %v", rel, readErr)
		}
		sources[rel] = string(b)
	}

	findings, census, err := FindUnclassifiedAuthzStoreCalls(sources)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}

	t.Logf("осмотрено файлов Go: %d; обращений к транспорту хранилища прав: %d; из них классифицировано: %d",
		census.Files, census.Calls, census.Classified)

	// Предпосылка гейта: он обязан ОТКАЗЫВАТЬ на беспредметности, а не молчать.
	// Ноль прочитанных файлов и ноль найденных обращений выглядят снаружи как
	// «нарушений нет» — и это ровно тот класс, который гейт и ловит.
	if census.Files == 0 {
		t.Fatal("осмотрено ноль файлов — гейт не читал дерева, и его молчание ничего не значит")
	}
	if census.Calls == 0 {
		t.Fatalf("в дереве ноль обращений к %s.Do — предмета у гейта нет: "+
			"либо транспорт переименован (тогда правь %s вместе с ним), либо гейт "+
			"смотрит не туда", AuthzStoreTransportSelector, "AuthzStoreTransportSelector")
	}

	for _, f := range findings {
		t.Errorf("%s:%d: функция %s зовёт %s.Do и НЕ классифицирует исход (%s). "+
			"Без причины отказ неотличим от соседних: «хранилища нет», «хранилище молчит» "+
			"и «оборвалось соединение из пула» приходят вызывающему одним и тем же, "+
			"а решение о повторе принимается вслепую (#720)",
			f.File, f.Line, f.Func, AuthzStoreTransportSelector, AuthzStoreClassifier)
	}
}
