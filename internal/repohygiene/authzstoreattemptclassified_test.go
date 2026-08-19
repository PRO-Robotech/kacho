// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if info.IsDir() {
			if skipPath(rel) || info.Name() == ".git" || info.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		b, readErr := os.ReadFile(path) // #nosec G304 -- обход собственного дерева
		if readErr != nil {
			return readErr
		}
		sources[rel] = string(b)
		return nil
	})
	if err != nil {
		t.Fatalf("обход дерева: %v", err)
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
