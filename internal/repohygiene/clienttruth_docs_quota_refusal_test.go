// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/treecorpus"
	"github.com/PRO-Robotech/kacho/pkg/quota"
)

// TestDocsDescribeQuotaRefusalExactlyAtItsOwners — отказ по исчерпанию предела
// описан таблицей кодов у каждого владельца учёта и ни у кого более; поверхность
// чтения пределов названа дословным путём из контракта.
//
// Предмет, обе формы подачи таблицы и границы — в шапке
// clienttruth_docs_quota_refusal.go. Способность падать и молчать доказана
// инъекцией: clienttruth_docs_quota_refusal_injection_test.go.
func TestDocsDescribeQuotaRefusalExactlyAtItsOwners(t *testing.T) {
	root := repoRoot(t)

	// Состав дерева — ИНДЕКС git, а не обход диска: под services/ на машине, где
	// поднимали стенд, лежат сборочные каталоги сайтов (node_modules, .docusaurus),
	// и вердикт, собранный обходом файловой системы, стал бы свойством рабочего
	// каталога, а не коммита.
	tree, err := treecorpus.NewTree(root)
	if err != nil {
		t.Fatalf("состав дерева: %v", err)
	}

	// Перечень владельцев ВЫВОДИТСЯ из того же объявления, из которого рендерятся
	// файлы миграций отказа. Выписанная копия разошлась бы с деревом молча.
	owners := make([]string, 0, len(quota.RefusalOwners()))
	for _, o := range quota.RefusalOwners() {
		owners = append(owners, o.Service)
	}

	c, err := collectDocsQuotaRefusal(tree, owners)
	if err != nil {
		t.Fatalf("обход дерева: %v", err)
	}

	var shownPaths, contractPaths, paramPaths int
	var sites []string
	for _, s := range c.Sites {
		contractPaths += len(s.ContractPaths)
		paramPaths += len(s.ParamPaths)
		shownPaths += len(s.PathShownAt)
		role := "без учёта"
		if s.Owner {
			role = "владелец"
		}
		sites = append(sites, s.Service+"("+role+","+s.Form.String()+")")
	}
	t.Logf("перепись: контрактов квот %d · сайтов %d (владельцев %d · без учёта %d) · "+
		"страниц прочитано %d · словарей кодов %d",
		c.ProtoFiles, len(c.Sites), c.Owners, c.NonOwners, c.DocFiles, c.DictFiles)
	t.Logf("перепись форм подачи таблицы: литералом %d · компонентом %d · "+
		"строку отказа несут %d из %d владельцев",
		c.FormLiteral, c.FormComponent, c.Described, c.Owners)
	t.Logf("перепись поверхности чтения: путей контракта %d (с подстановкой %d — "+
		"в требование не идут) · названо дословно %d",
		contractPaths, paramPaths, shownPaths)
	t.Logf("сайты: %s", strings.Join(sites, " "))

	// Проверка собственной предпосылки: пустой обход обесценивает вердикт, и
	// «ноль находок» обязано быть отличимо от «ноль прочитанного».
	if c.ProtoFiles == 0 || c.DocFiles == 0 {
		t.Fatalf("обход пуст — вердикт беспредметен: контрактов %d, страниц %d",
			c.ProtoFiles, c.DocFiles)
	}
	if c.Owners == 0 {
		t.Fatal("перечень владельцев учёта пуст — сверять не с кем " +
			"(quota.RefusalOwners перестал их называть?)")
	}
	if c.NonOwners == 0 {
		t.Fatal("сайтов доменов БЕЗ учёта не найдено ни одного — зеркальная полоса " +
			"гейта не проверяется ничем, и её молчание ничего не значит")
	}
	// Положительный контроль распознавателя: обе формы подачи обязаны быть
	// ПРОЧИТАНЫ. Ноль по любой из них означает, что распознаватель перестал
	// видеть свой предмет (п.7 §«Гейт на класс»), а не что дерево чисто.
	if c.FormLiteral == 0 {
		t.Fatal("литеральной формы таблицы кодов не найдено ни на одном сайте — " +
			"распознаватель ослеп к форме, которой поданы iam, nlb и registry")
	}
	if c.FormComponent == 0 {
		t.Fatal("компонентной формы таблицы кодов не найдено ни на одном сайте — " +
			"распознаватель ослеп к форме, которой поданы vpc, compute и storage; " +
			"именно она не содержит литерала RESOURCE_EXHAUSTED и потому невидима " +
			"наивному поиску по .mdx")
	}
	if contractPaths == 0 {
		t.Fatal("из контрактов квот не выведено ни одного пути чтения — сверять не с чем")
	}

	for _, f := range docsQuotaRefusalFindings(c) {
		t.Errorf("отказ по исчерпанию предела: %s", f)
	}
}
