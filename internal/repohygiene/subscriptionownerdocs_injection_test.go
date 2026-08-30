// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// subscriptionownerdocs_injection_test.go — доказательство, что гейт «владелец
// журнала говорит об этом в своей клиентской документации» СПОСОБЕН упасть.
//
// Каждое утверждение прогоняется дважды: на внесённом дефекте (обязано найтись, и
// находка обязана называть КООРДИНАТУ) и на законном близнеце той же формы
// (обязано смолчать). Односторонняя проба зеленела бы и на гейте, который краснеет
// всегда.
//
// Инъекция подаёт СИНТЕТИЧЕСКИЙ состав дерева, а не правит настоящее: гейт в бою
// берёт состав у индекса git, и проба, пишущая в индекс запустившего её
// репозитория, портит чужое состояние. Судящие функции при этом те же самые —
// `subscriptionOwners`, `ownerDocReports`, `ownerDocFindings`.

// syntheticLister — состав синтетического дерева: обход каталога, а не индекс.
//
// Он законен ровно здесь: у временного каталога нет ни индекса, ни игнорируемых
// путей, поэтому обход и индекс совпадают by construction. В бою гейт пользуется
// индексом — по причине, объявленной корпусом дерева.
func syntheticLister(dir string, suffixes ...string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if len(suffixes) == 0 {
			out = append(out, path)
			return nil
		}
		for _, s := range suffixes {
			if strings.HasSuffix(path, s) {
				out = append(out, path)
				return nil
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

// syntheticService кладёт в дерево сервис: регистрирует ли он глагол и называет
// ли его клиентская документация адрес ручки.
func syntheticService(t *testing.T, root, name string, registers, docsNameHandle bool) {
	t.Helper()
	cmdDir := filepath.Join(root, "services", name, "cmd", name)
	if err := os.MkdirAll(cmdDir, 0o750); err != nil {
		t.Fatalf("синтетическое дерево: %v", err)
	}
	body := "package main\n\nfunc wire(srv any, impl any) {\n"
	if registers {
		// ВЫЗОВ, а не упоминание: гейт судит узел дерева.
		body += "\tsubscriptionv1." + subscriptionVerbRegistrar + "(srv, impl)\n"
	} else {
		// Законный близнец: имя стоит в КОММЕНТАРИИ. Гейт, судящий подстроку,
		// принял бы этот сервис за владельца — и требовал бы от его документации
		// того, чего она не должна.
		body += "\t// " + subscriptionVerbRegistrar + " здесь не зовётся — только назван\n"
	}
	body += "\t_, _ = srv, impl\n}\n"
	if err := os.WriteFile(filepath.Join(cmdDir, "main.go"), []byte(body), 0o600); err != nil {
		t.Fatalf("синтетическое дерево: %v", err)
	}

	contentDir := filepath.Join(root, "services", name, "docs", "content", "api")
	if err := os.MkdirAll(contentDir, 0o750); err != nil {
		t.Fatalf("синтетическое дерево: %v", err)
	}
	page := "# обзор API\n\nМутации возвращают Operation; исход узнаётся поллом.\n"
	if docsNameHandle {
		page += "\nИзменения ресурсов доступны потоком: `GET " + subscriptionHandlePath + "?owner=" + name + "`.\n"
	}
	if err := os.WriteFile(filepath.Join(contentDir, "overview.mdx"), []byte(page), 0o600); err != nil {
		t.Fatalf("синтетическое дерево: %v", err)
	}
}

// TestOwnerDocsGateFindsAnOwnerWhoseDocsAreSilent — владелец есть, документация
// молчит.
func TestOwnerDocsGateFindsAnOwnerWhoseDocsAreSilent(t *testing.T) {
	root := t.TempDir()
	syntheticService(t, root, "compute", true, false) // дефект
	syntheticService(t, root, "nlb", true, true)      // законный владелец рядом

	owners, filesRead, unparsed, err := subscriptionOwners(root, syntheticLister)
	if err != nil {
		t.Fatalf("обход синтетического дерева: %v", err)
	}
	if len(unparsed) != 0 {
		t.Fatalf("синтетические исходники не разобрались: %v", unparsed)
	}
	if filesRead == 0 {
		t.Fatal("осмотрено ноль исходников — инъекция ничего не подала")
	}
	if len(owners) != 2 {
		t.Fatalf("владельцев найдено %d %v, ожидалось 2 — обход не находит регистрации",
			len(owners), owners)
	}

	findings := ownerDocFindings(ownerDocReports(root, owners, syntheticLister))
	if len(findings) != 1 {
		t.Fatalf("находок %d, ожидалась одна: %v — инъекция обязана ронять ТОЛЬКО проверяемое",
			len(findings), findings)
	}
	if !strings.Contains(findings[0], `"compute"`) {
		t.Errorf("находка не называет виновника: %q — по ней нечего чинить", findings[0])
	}
	if !strings.Contains(findings[0], subscriptionHandlePath) {
		t.Errorf("находка не называет адрес ручки: %q — читателю неоткуда узнать, "+
			"что именно дописать", findings[0])
	}
}

// TestOwnerDocsGateStaysSilentOnAServiceThatOnlyMENTIONSTheRegistrar — законный
// близнец: имя регистратора стоит в комментарии, вызова нет.
//
// Без этого утверждения гейт мог бы судить подстроку и требовать документации от
// сервиса, который владельцем не является, — то есть краснеть на верном дереве.
func TestOwnerDocsGateStaysSilentOnAServiceThatOnlyMENTIONSTheRegistrar(t *testing.T) {
	root := t.TempDir()
	syntheticService(t, root, "storage", false, false) // только упоминание, документация молчит
	syntheticService(t, root, "compute", true, true)   // настоящий владелец

	owners, _, _, err := subscriptionOwners(root, syntheticLister)
	if err != nil {
		t.Fatalf("обход синтетического дерева: %v", err)
	}
	if len(owners) != 1 || owners[0] != "compute" {
		t.Fatalf("владельцами объявлены %v — упоминание в комментарии владельцем НЕ делает",
			owners)
	}
	if findings := ownerDocFindings(ownerDocReports(root, owners, syntheticLister)); len(findings) != 0 {
		t.Fatalf("на согласном дереве гейт нашёл %v — проверка, краснеющая на верном, "+
			"будет снята первой", findings)
	}
}

// TestOwnerDocsGateFindsAnOwnerWithoutClientDocsAtAll — у владельца нет
// клиентских страниц вовсе.
//
// Это отдельный исход, а не частный случай молчания: требование к документации
// становится невыполнимым by construction, и молчаливый пропуск такого владельца
// сделал бы гейт зелёным ровно там, где смотреть было не на что.
func TestOwnerDocsGateFindsAnOwnerWithoutClientDocsAtAll(t *testing.T) {
	root := t.TempDir()
	syntheticService(t, root, "compute", true, true)
	// Владелец без каталога документации: кладём только исходник.
	cmdDir := filepath.Join(root, "services", "geo", "cmd", "geo")
	if err := os.MkdirAll(cmdDir, 0o750); err != nil {
		t.Fatalf("синтетическое дерево: %v", err)
	}
	body := "package main\n\nfunc wire(srv any, impl any) {\n\tsubscriptionv1." +
		subscriptionVerbRegistrar + "(srv, impl)\n\t_, _ = srv, impl\n}\n"
	if err := os.WriteFile(filepath.Join(cmdDir, "main.go"), []byte(body), 0o600); err != nil {
		t.Fatalf("синтетическое дерево: %v", err)
	}

	owners, _, _, err := subscriptionOwners(root, syntheticLister)
	if err != nil {
		t.Fatalf("обход синтетического дерева: %v", err)
	}
	findings := ownerDocFindings(ownerDocReports(root, owners, syntheticLister))
	if len(findings) != 1 || !strings.Contains(findings[0], `"geo"`) {
		t.Fatalf("находок %d %v — владелец без клиентской документации обязан быть назван",
			len(findings), findings)
	}
}
