// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// Доказательство способности анализатора падать И молчать.
//
// Инъекция берёт НАСТОЯЩИЙ дефект из истории дерева — отказ подсети, называвший
// доменное имя `v4_cidr_blocks`, — и НАСТОЯЩЕГО законного близнеца рядом:
// группа CIDR называет `v4_cidr_blocks` законно, потому что у НЕЁ это поле
// контракта есть (`kacho.cloud.vpc.v1.CidrGroup.v4_cidr_blocks`). Одна и та же
// строка, два ресурса, разные вердикты — если анализатор судит существо, а не
// форму.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// injectRefusalTree — синтетическое дерево с одним сервисом и двумя ресурсами.
func injectRefusalTree(t *testing.T, subnetField, cidrGroupField string) string {
	t.Helper()
	root := t.TempDir()
	mk := func(res, field string) {
		dir := filepath.Join(root, "services", "vpc", "internal", "apps", "kacho", "api", res)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		src := "package " + res + "\n\n" +
			"// serviceerr.InvalidArg упомянут в комментарии — по тексту это неотличимо\n" +
			"// от вызова, поэтому разбор идёт по AST.\n" +
			"func check() error {\n" +
			"\treturn serviceerr.InvalidArg(\"" + field + "\", \"boom\")\n" +
			"}\n"
		if err := os.WriteFile(filepath.Join(dir, "create.go"), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mk("subnet", subnetField)
	mk("cidrgroup", cidrGroupField)
	return root
}

func auditInjectedRefusalFields(t *testing.T, root string) ([]RefusalFieldNameFinding, RefusalFieldNameCensus) {
	t.Helper()
	opts := DefaultRefusalFieldNameOptions(root)
	f, c, err := AuditRefusalFieldNames(opts, nil)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	return f, c
}

// TestRefusalFieldNameInjectionCatchesTheRealDefect — КРАСНОЕ на настоящем дефекте.
func TestRefusalFieldNameInjectionCatchesTheRealDefect(t *testing.T) {
	// Подсеть называет доменное имя; группа CIDR — своё законное поле.
	findings, census := auditInjectedRefusalFields(t, injectRefusalTree(t, "v4_cidr_blocks", "v4_cidr_blocks"))
	if census.Judged != 2 {
		t.Fatalf("рассужено имён %d, ожидалось 2 — инъекция не дошла до разбора", census.Judged)
	}
	if len(findings) != 1 {
		t.Fatalf("находок %d, ожидалась ровно одна (подсеть): %v", len(findings), findings)
	}
	if findings[0].Resource != "subnet" {
		t.Fatalf("находка приписана ресурсу %q, а дефект у подсети", findings[0].Resource)
	}
	// Находка обязана НАЗЫВАТЬ координату и имя: находка, называющая симптом,
	// посылает читателя искать не там.
	msg := findings[0].String()
	for _, want := range []string{"subnet/create.go", "v4_cidr_blocks", "subnet"} {
		if !strings.Contains(msg, want) {
			t.Errorf("в находке нет %q: %s", want, msg)
		}
	}
}

// TestRefusalFieldNameInjectionStaysSilentOnTheLegitimateTwin — ЗЕЛЁНОЕ на
// законной конструкции той же формы.
//
// Без этой стороны анализатор ловил бы строку, а не существо: `v4_cidr_blocks`
// у группы CIDR — настоящее поле её контракта.
func TestRefusalFieldNameInjectionStaysSilentOnTheLegitimateTwin(t *testing.T) {
	findings, census := auditInjectedRefusalFields(t, injectRefusalTree(t, "ipv4_cidr_primary", "v4_cidr_blocks"))
	if census.Judged != 2 {
		t.Fatalf("рассужено имён %d, ожидалось 2", census.Judged)
	}
	if len(findings) != 0 {
		t.Fatalf("законные имена объявлены находками: %v", findings)
	}
}

// TestRefusalFieldNameAcceptsOneofNames — ONEOF тоже законное имя.
//
// Первая редакция распознавателя знала только поля и объявила находками два
// ВЕРНЫХ отказа (`gateway`, `disk`) — оба называли ветвь oneof. Сторона
// проверяется отдельно, потому что именно её отсутствие давало обвинение наугад.
func TestRefusalFieldNameAcceptsOneofNames(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "services", "vpc", "internal", "apps", "kacho", "api", "gateway")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := "package gateway\n\nfunc check() error {\n" +
		"\treturn serviceerr.InvalidArg(\"gateway\", \"gateway: required\")\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "create.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	findings, census := auditInjectedRefusalFields(t, root)
	if census.Judged != 1 {
		t.Fatalf("рассужено имён %d, ожидалось 1", census.Judged)
	}
	if len(findings) != 0 {
		t.Fatalf("имя ветви oneof объявлено находкой: %v", findings)
	}
}

// TestRefusalFieldNameCountsWhatItCannotJudge — не-литеральное имя НЕ судится и
// попадает в перепись.
//
// Без отдельного числа «находок ноль» было бы неотличимо от «судить было нечего».
func TestRefusalFieldNameCountsWhatItCannotJudge(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "services", "vpc", "internal", "apps", "kacho", "api", "subnet")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := "package subnet\n\nvar f = \"whatever\"\n\nfunc check() error {\n" +
		"\treturn serviceerr.InvalidArg(f, \"boom\")\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "create.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	findings, census := auditInjectedRefusalFields(t, root)
	if len(findings) != 0 {
		t.Fatalf("не-литеральное имя объявлено находкой: %v", findings)
	}
	if census.Calls != 1 || census.Judged != 0 || census.NotLiteral != 1 {
		t.Fatalf("перепись не различает «не судимо»: вызовов %d, рассужено %d, не литерал %d",
			census.Calls, census.Judged, census.NotLiteral)
	}
}

// TestRefusalFieldNameFailsOnAnEmptyWalk — пустой обход не выдаёт себя за чистый.
func TestRefusalFieldNameFailsOnAnEmptyWalk(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "services"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, census := auditInjectedRefusalFields(t, root)
	if census.Packages != 0 || census.Judged != 0 {
		t.Fatalf("пустое дерево дало непустую перепись: %+v", census)
	}
	// Вердикт о пустом обходе выносит проба дерева (премисы в
	// `clienttruth_refusal_fieldname_test.go`), а не анализатор: он честно
	// сообщает нули, и именно они там роняют прогон.
}
