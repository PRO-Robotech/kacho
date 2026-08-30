// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// registryPublicService — публичная служба домена registry. Внутренняя
// (`InternalRegistryService`) сюда НЕ входит намеренно: ban #6 держит её вне
// внешней поверхности, и требовать от неё клиентской страницы значило бы требовать
// документировать то, чего арендатор не достигает.
var registryPublicService = map[string]struct{}{"RegistryService": {}}

const (
	registryServiceProto = "proto/kacho/cloud/registry/v1/registry_service.proto"
	registryClientAPIDir = "services/registry/docs/content/api"
)

// TestRegistryPublicRPCIsNamedOnAClientPage — каждый публичный глагол registry
// назван на клиентской странице.
//
// # Почему это гейт, а не вычитка
//
// Расхождение заводится НЕ правкой страницы, а её НЕПРАВКОЙ: контракт растёт своим
// изменением, страница остаётся прежней, и ни одна проверка от этого не краснеет.
// Так дожили до #1593 шесть операций сразу — весь жизненный цикл репозитория. Пока
// вычитка страниц была единственным механизмом, класс возвращался: первая редакция
// страницы описывала ОДНУ операцию из пятнадцати и выглядела законченной.
//
// # Граница охвата названа числом, а не умолчанием
//
// Гейт связывает ОДИН домен — registry. Это не заготовка «расширим потом»: у
// каждого домена свой корпус страниц и свои общие адреса, и включить соседей не
// глядя значит завести красноту, которую снимут ведомостью исключений — то есть
// послаблением, которое переживёт свой предмет. Сосед добавляется вместе со своей
// сверкой, а не строкой в перечне.
func TestRegistryPublicRPCIsNamedOnAClientPage(t *testing.T) {
	root := repoRoot(t)

	protoBytes, err := os.ReadFile(filepath.Join(root, registryServiceProto))
	if err != nil {
		t.Fatalf("контракт не прочитан — вердикт беспредметен: %v", err)
	}
	ops, rpcCount, restCount := ParseContractOperations(string(protoBytes), registryPublicService)
	if len(ops) == 0 {
		t.Fatalf("в %s не разобрано НИ ОДНОГО метода службы %v — обход пуст, вердикт беспредметен",
			registryServiceProto, keysOf(registryPublicService))
	}

	pages, err := readClientPages(filepath.Join(root, registryClientAPIDir))
	if err != nil {
		t.Fatalf("клиентские страницы не прочитаны — вердикт беспредметен: %v", err)
	}
	if len(pages) == 0 {
		t.Fatalf("в %s нет ни одной страницы — обход пуст, вердикт беспредметен", registryClientAPIDir)
	}

	missing, census := UndocumentedOperations(ops, pages)
	census.Services = len(registryPublicService)
	census.RPCs = rpcCount
	census.RESTOperations = restCount
	t.Log(census.String())

	// Предпосылка анализатора: он знает форму записи объявления. Расхождение между
	// встреченными тегами и разобранными означает вторую форму — и всё, записанное
	// ею, лежит ВНЕ наблюдения, не будучи ни находкой, ни зеленью.
	if census.OperationTags != census.OperationsRead {
		t.Errorf("предпосылка анализатора нарушена: тегов <ApiOperation встречено %d, разобрано %d.\n"+
			"Разница — объявления, записанные формой, которой анализатор не знает: они не судятся ВОВСЕ.\n"+
			"Чинится расширением apiOperationRe вместе с инъекцией на новую форму, а не подгонкой числа.",
			census.OperationTags, census.OperationsRead)
	}

	for _, op := range missing {
		t.Errorf("публичный глагол не назван ни на одной клиентской странице: %s\n"+
			"  Для клиента, который не читает контракт, этой возможности НЕ СУЩЕСТВУЕТ.\n"+
			"  Исходов три: описать на странице в %s · снять с контракта · объявить внутренним (%s).",
			op, registryClientAPIDir, "ban #6")
	}
}

// readClientPages читает страницы клиентского сайта домена (`.mdx` каталога `api`).
func readClientPages(dir string) (map[string]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	pages := map[string]string{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".mdx") {
			continue
		}
		b, rerr := os.ReadFile(filepath.Join(dir, e.Name()))
		if rerr != nil {
			return nil, rerr
		}
		pages[e.Name()] = string(b)
	}
	return pages, nil
}

func keysOf(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
