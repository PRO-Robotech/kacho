// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Инъекция гейта «страница называет каждое поле контракта».
//
// Вход НАСТОЯЩИЙ по форме и синтетический по содержанию: дерево собирается в
// t.TempDir(), поэтому вердикты не зависят ни от состояния репозитория, ни от
// порядка прогонов. Каждая ось проверяется В ОБЕ СТОРОНЫ: законный близнец
// обязан молчать, и он стоит ПЕРВЫМ — иначе отрицание зеленело бы на
// анализаторе, который находит нарушение в любом дереве.

const fldcovThingProto = `syntax = "proto3";
package kacho.cloud.demo.v1;

// Thing — сообщение верхнего уровня. Имя message встречается и в этом
// комментарии: разбор читает ОБЪЯВЛЕНИЕ, а не упоминание.
message Thing {
  string id = 1;
  string display_name = 2;
  reserved 7;
  // Снятое с поверхности поле: продукт просит им не пользоваться, и страница
  // не обязана его называть.
  string retired_field = 8 [deprecated = true];
  oneof spec {
    string a_spec = 3;
  }
  // Part — вложенное сообщение. Его поле судится вместе с НИМ, а не с Thing.
  message Part {
    string part_only_field = 1;
  }
  Part part = 4;
}
`

func fldcovWriteTree(t *testing.T, domain, service string, page string) string {
	t.Helper()
	root := t.TempDir()
	protoDir := filepath.Join(root, "proto", "kacho", "cloud", domain, "v1")
	if err := os.MkdirAll(protoDir, 0o750); err != nil {
		t.Fatalf("каталог контрактов: %v", err)
	}
	if err := os.WriteFile(filepath.Join(protoDir, "thing.proto"), []byte(fldcovThingProto), 0o600); err != nil {
		t.Fatalf("контракт: %v", err)
	}
	apiDir := filepath.Join(root, "services", service, "docs", "content", "api")
	if err := os.MkdirAll(apiDir, 0o750); err != nil {
		t.Fatalf("каталог страниц: %v", err)
	}
	if err := os.WriteFile(filepath.Join(apiDir, "thing.mdx"), []byte(page), 0o600); err != nil {
		t.Fatalf("страница: %v", err)
	}
	return root
}

func fldcovOpts(root string, aliases map[string]string) ClientDocsFieldCoverageOptions {
	return ClientDocsFieldCoverageOptions{
		Root:          root,
		ProtoRoot:     "proto/kacho/cloud",
		ServicesRoot:  "services",
		DomainAliases: aliases,
	}
}

func fldcovRun(t *testing.T, root string, aliases map[string]string) (
	[]ClientDocsFieldCoverageFinding, ClientDocsFieldCoverageCensus,
) {
	t.Helper()
	var log strings.Builder
	findings, census, err := AuditClientDocsFieldCoverage(fldcovOpts(root, aliases), &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v\n%s", err, log.String())
	}
	return findings, census
}

// Полная страница: называет все три поля Thing и НЕ называет поле вложенного
// Part. Гейт обязан молчать — включая ось «вложенное поле не приписывается
// объемлющему».
const fldcovPageComplete = "<code>id</code> <code>displayName</code> <code>aSpec</code> <code>part</code>\n"

func TestInjectionFieldCoverageCompletePageIsSilent(t *testing.T) {
	root := fldcovWriteTree(t, "demo", "demo", fldcovPageComplete)
	findings, census := fldcovRun(t, root, nil)
	if len(findings) != 0 {
		t.Fatalf("законный близнец даёт находки — отрицание зеленело бы на всём сломанном: %v", findings)
	}
	if census.PagesJudged != 1 {
		t.Fatalf("страниц рассужено %d, ожидалась одна: инъекция не дошла до предмета", census.PagesJudged)
	}
	// Полей ровно четыре: id · display_name · ветвь a_spec · part.
	// part_only_field вложенного Part среди них быть НЕ должно, retired_field —
	// тоже: он снят с поверхности и считается отдельно.
	if census.FieldsJudged != 4 {
		t.Fatalf("полей рассужено %d, ожидалось 4 (id, display_name, ветвь a_spec, part): "+
			"разбор считает не то", census.FieldsJudged)
	}
	if census.FieldsRetired != 1 {
		t.Fatalf("снятых с поверхности полей насчитано %d, ожидалось 1: граница deprecated "+
			"перестала измеряться, и её молчание сказано ни о чём", census.FieldsRetired)
	}
}

func TestInjectionFieldCoverageMissingFieldIsAFinding(t *testing.T) {
	root := fldcovWriteTree(t, "demo", "demo", "<code>id</code> <code>aSpec</code> <code>part</code>\n")
	findings, _ := fldcovRun(t, root, nil)
	if len(findings) != 1 {
		t.Fatalf("ожидалась ровно одна находка, получено %d: %v", len(findings), findings)
	}
	f := findings[0]
	if f.Field != "displayName" || f.Message != "Thing" ||
		!strings.HasSuffix(f.Page, "api/thing.mdx") {
		t.Fatalf("находка не называет координату: %+v", f)
	}
}

// Ветвь `oneof` — такое же поле контракта: вызывающий получает её тем же чтением.
func TestInjectionFieldCoverageOneofArmIsJudged(t *testing.T) {
	root := fldcovWriteTree(t, "demo", "demo", "<code>id</code> <code>displayName</code> <code>part</code>\n")
	findings, _ := fldcovRun(t, root, nil)
	if len(findings) != 1 || findings[0].Field != "aSpec" {
		t.Fatalf("ветвь oneof не судится: %v", findings)
	}
}

// Проза упоминанием ПОЛЯ не является: гейт, принимавший бы её, зеленел бы на
// любой странице, где слово встречается в предложении.
func TestInjectionFieldCoverageProseIsNotANaming(t *testing.T) {
	root := fldcovWriteTree(t, "demo", "demo",
		"<code>id</code> <code>aSpec</code> <code>part</code>\nПоле displayName описано прозой и только прозой.\n")
	findings, _ := fldcovRun(t, root, nil)
	if len(findings) != 1 || findings[0].Field != "displayName" {
		t.Fatalf("проза зачтена за упоминание поля: %v", findings)
	}
}

// Исходная форма контракта тоже засчитывается: страница вправе называть поле
// так, как оно объявлено.
func TestInjectionFieldCoverageContractFormCounts(t *testing.T) {
	root := fldcovWriteTree(t, "demo", "demo",
		"<code>id</code> <code>display_name</code> `aSpec` <code>part</code>\n")
	findings, _ := fldcovRun(t, root, nil)
	if len(findings) != 0 {
		t.Fatalf("исходная форма имени не зачтена: %v", findings)
	}
}

// Страница, которой не отвечает сообщение домена, ВНЕ ОХВАТА, а не находка:
// обзор, операции, пределы. Число печатает перепись.
func TestInjectionFieldCoveragePageWithoutAMessageIsOutOfScope(t *testing.T) {
	root := fldcovWriteTree(t, "demo", "demo", fldcovPageComplete)
	apiDir := filepath.Join(root, "services", "demo", "docs", "content", "api")
	if err := os.WriteFile(filepath.Join(apiDir, "overview.mdx"), []byte("обзор\n"), 0o600); err != nil {
		t.Fatalf("страница обзора: %v", err)
	}
	findings, census := fldcovRun(t, root, nil)
	if len(findings) != 0 {
		t.Fatalf("страница без сообщения объявлена находкой: %v", findings)
	}
	if census.PagesOutside != 1 || len(census.OutsidePages) != 1 {
		t.Fatalf("перепись не назвала страницу вне охвата: %+v", census)
	}
}

// Домен контракта назван иначе, чем каталог сервиса: пара берётся из карты
// псевдонимов, и без неё страница молча ушла бы вне охвата.
func TestInjectionFieldCoverageDomainAliasIsHonoured(t *testing.T) {
	root := fldcovWriteTree(t, "otherdomain", "shortname", "<code>id</code> <code>aSpec</code> <code>part</code>\n")

	if findings, census := fldcovRun(t, root, map[string]string{"shortname": "otherdomain"}); len(findings) != 1 ||
		census.PagesJudged != 1 {
		t.Fatalf("псевдоним не применён: находок %d, страниц рассужено %d", len(findings), census.PagesJudged)
	}
	// Обратный контроль: без псевдонима та же страница вне охвата — то есть
	// карта действительно решает, а не просто присутствует. Обход при этом
	// пуст, и гейт обязан отказать, а не промолчать.
	var log strings.Builder
	_, census, err := AuditClientDocsFieldCoverage(fldcovOpts(root, nil), &log)
	if err == nil {
		t.Fatalf("без псевдонима рассужено %d страниц — гейт молчит о пустом обходе", census.PagesJudged)
	}
}

// Пустой обход — ОТКАЗ, а не «находок ноль»: иначе переезд каталога читался бы
// как чистое дерево.
func TestInjectionFieldCoverageEmptyWalkRefuses(t *testing.T) {
	t.Run("страниц ноль", func(t *testing.T) {
		root := fldcovWriteTree(t, "demo", "demo", fldcovPageComplete)
		if err := os.Remove(filepath.Join(root, "services", "demo", "docs", "content", "api", "thing.mdx")); err != nil {
			t.Fatalf("снятие страницы: %v", err)
		}
		if _, _, err := AuditClientDocsFieldCoverage(fldcovOpts(root, nil), nil); err == nil {
			t.Fatal("обход без единой страницы вынес вердикт")
		}
	})
	t.Run("контрактов ноль", func(t *testing.T) {
		root := fldcovWriteTree(t, "demo", "demo", fldcovPageComplete)
		if err := os.Remove(filepath.Join(root, "proto", "kacho", "cloud", "demo", "v1", "thing.proto")); err != nil {
			t.Fatalf("снятие контракта: %v", err)
		}
		if _, _, err := AuditClientDocsFieldCoverage(fldcovOpts(root, nil), nil); err == nil {
			t.Fatal("обход без единого контракта вынес вердикт")
		}
	})
}

// Граница `deprecated` проверяется В ОБЕ СТОРОНЫ: то же самое поле БЕЗ пометки
// обязано стать находкой. Без этой оси молчание гейта на снятом поле было бы
// неотличимо от молчания гейта, не видящего полей вовсе.
func TestInjectionFieldCoverageRetiredMarkerIsWhatBuysTheSilence(t *testing.T) {
	root := fldcovWriteTree(t, "demo", "demo", fldcovPageComplete)
	proto := filepath.Join(root, "proto", "kacho", "cloud", "demo", "v1", "thing.proto")
	raw, err := os.ReadFile(proto) // #nosec G304 -- путь построен этим же тестом
	if err != nil {
		t.Fatalf("контракт: %v", err)
	}
	without := strings.Replace(string(raw),
		"string retired_field = 8 [deprecated = true];", "string retired_field = 8;", 1)
	if without == string(raw) {
		t.Fatal("фикстура не содержит снятого поля — ось проверяет не то, что объявляет")
	}
	if err := os.WriteFile(proto, []byte(without), 0o600); err != nil {
		t.Fatalf("контракт: %v", err)
	}

	findings, census := fldcovRun(t, root, nil)
	if len(findings) != 1 || findings[0].Field != "retiredField" {
		t.Fatalf("поле без пометки снятия не стало находкой: %v", findings)
	}
	if census.FieldsRetired != 0 {
		t.Fatalf("снятых насчитано %d при снятой пометке", census.FieldsRetired)
	}
}
