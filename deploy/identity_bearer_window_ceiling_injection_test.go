// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_bearer_window_ceiling_injection_test.go — доказательство падучести
// MAIL-51 в обе стороны.
//
// Дефект вносится в КОПИЮ тела шаблона в памяти: рабочее дерево проверка не
// заводила и не трогает. Каждая инъекция меняет РОВНО ОДИН факт против
// законного близнеца, и близнец прогоняется первым.
package deploy_test

import (
	"os"
	"strings"
	"testing"
)

// bearerWindowBody — тело действующего шаблона.
func bearerWindowBody(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(identityConfigTemplate)
	if err != nil {
		t.Fatalf("исходный шаблон не читается: %v", err)
	}
	return string(raw)
}

// bearerWindowEdit — правка копии тела; пустая правка доказывает ноль.
func bearerWindowEdit(t *testing.T, body, old, new string) string {
	t.Helper()
	out := strings.Replace(body, old, new, 1)
	if out == body {
		t.Fatalf("инъекция не изменила НИЧЕГО (образец %q не найден) — она меняет ноль "+
			"фактов и доказывает ноль", old)
	}
	return out
}

// TestMAIL51Injection_LawfulTemplateIsSilent — ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ.
func TestMAIL51Injection_LawfulTemplateIsSilent(t *testing.T) {
	t.Parallel()
	if got := bearerWindowFindings(t, bearerWindowBody(t)); len(got) != 0 {
		t.Fatalf("законный шаблон дал %d находок:\n%s", len(got), strings.Join(got, "\n"))
	}
}

// TestMAIL51Injection_RaisedCeilingIsAFinding — дефект A: срок, ограничивающий
// окно, поднят выше потолка.
func TestMAIL51Injection_RaisedCeilingIsAFinding(t *testing.T) {
	t.Parallel()
	body := bearerWindowEdit(t, bearerWindowBody(t),
		"      lifespan: 15m\n      use: code",
		"      lifespan: 72h\n      use: code")
	got := bearerWindowFindings(t, body)
	var found bool
	for _, f := range got {
		if strings.Contains(f, "selfservice.flows.verification.lifespan") &&
			strings.Contains(f, "превышает потолок") {
			found = true
		}
	}
	if !found {
		t.Fatalf("поднятый срок подтверждения НЕ найден:\n%s", strings.Join(got, "\n"))
	}
}

// TestMAIL51Injection_UnclassifiedLifespanIsAFinding — дефект Б: НОВОЕ
// объявление срока, которого нет в разборе.
//
// Это и есть механизм истечения решения Р21б: объявление краснеет, пока его не
// разобрали, — иначе окно расширяют молча.
func TestMAIL51Injection_UnclassifiedLifespanIsAFinding(t *testing.T) {
	t.Parallel()
	body := bearerWindowEdit(t, bearerWindowBody(t),
		"    # ─── Logout ──────────────────────────────────────────────────",
		"    magic:\n      lifespan: 48h\n\n    # ─── Logout ─────────────────────")
	got := bearerWindowFindings(t, body)
	var found bool
	for _, f := range got {
		if strings.Contains(f, "не отнесено") && strings.Contains(f, "magic.lifespan") {
			found = true
		}
	}
	if !found {
		t.Fatalf("неразобранное объявление срока НЕ найдено:\n%s", strings.Join(got, "\n"))
	}
}

// TestMAIL51Injection_LaneLeavingCodeIsAFinding — дефект В: полоса перестала
// быть кодовой. Предмет решения Р21б меняется, и молчание здесь означало бы,
// что довод «окно есть срок» применён к тому, что окном не ограничено.
func TestMAIL51Injection_LaneLeavingCodeIsAFinding(t *testing.T) {
	t.Parallel()
	body := bearerWindowEdit(t, bearerWindowBody(t),
		"      lifespan: 5m\n      use: code",
		"      lifespan: 5m\n      use: link")
	got := bearerWindowFindings(t, body)
	var found bool
	for _, f := range got {
		if strings.Contains(f, "объявлена как `link`") {
			found = true
		}
	}
	if !found {
		t.Fatalf("смена полосы на ссылку НЕ найдена:\n%s", strings.Join(got, "\n"))
	}
}

// TestMAIL51Injection_PathIsDerivedFromIndentNotFromTheWord — распознаватель
// восстанавливает ПУТЬ объявления по отступам, а не считает все `lifespan`
// одним предметом. Без этого три срока форм и три срока предъявителей были бы
// неразличимы, и разбор судил бы не то.
func TestMAIL51Injection_PathIsDerivedFromIndentNotFromTheWord(t *testing.T) {
	t.Parallel()
	body := "selfservice:\n" +
		"  flows:\n" +
		"    recovery:\n" +
		"      lifespan: 5m\n" +
		"      use: code\n" +
		"    verification:\n" +
		"      lifespan: 15m\n" +
		"      use: code\n" +
		"  methods:\n" +
		"    code:\n" +
		"      config:\n" +
		"        lifespan: 5m\n"
	got := readBearerWindowLifespans(t, body)
	if len(got) != 3 {
		t.Fatalf("прочитано %d объявлений, ожидалось 3: %+v", len(got), got)
	}
	want := []string{
		"selfservice.flows.recovery.lifespan",
		"selfservice.flows.verification.lifespan",
		"selfservice.methods.code.config.lifespan",
	}
	for i, w := range want {
		if got[i].Path != w {
			t.Errorf("объявление %d: путь %q, ожидался %q", i, got[i].Path, w)
		}
	}
}

// TestMAIL51Injection_ProseIsNotADeclaration — слово `lifespan` в пояснении
// объявлением не является: образец привязан к началу строки именно затем, чтобы
// гейт не читал собственное объяснение как объявление.
func TestMAIL51Injection_ProseIsNotADeclaration(t *testing.T) {
	t.Parallel()
	body := "selfservice:\n" +
		"  flows:\n" +
		"    recovery:\n" +
		"      # здесь мог бы стоять lifespan: 999h, и это была бы находка\n" +
		"      lifespan: 5m\n" +
		"      use: code\n"
	got := readBearerWindowLifespans(t, body)
	if len(got) != 1 {
		t.Fatalf("прочитано %d объявлений, ожидалось 1 — проза принята за объявление: %+v",
			len(got), got)
	}
}
