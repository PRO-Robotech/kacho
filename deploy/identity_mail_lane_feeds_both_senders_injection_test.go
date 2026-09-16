// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_mail_lane_feeds_both_senders_injection_test.go — доказательство
// падучести MAIL-48 в обе стороны.
//
// Дефект вносится в КОПИЮ обоих шаблонов в t.TempDir(): рабочее дерево
// проверка не заводила и не трогает. Каждая инъекция меняет РОВНО ОДИН факт
// против законного близнеца, и близнец прогоняется первым — без него красное
// доказывало бы лишь то, что разбор что-то находит.
package deploy_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mailFeedCopyTree — копия обоих шаблонов под временным корнем.
func mailFeedCopyTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, rel := range []string{identityConfigTemplate, mailSenderConfigTemplate} {
		raw, err := os.ReadFile(filepath.FromSlash(rel))
		if err != nil {
			t.Fatalf("исходный шаблон %s не читается: %v", rel, err)
		}
		dst := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dst, raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// mailFeedEdit — правка одного файла копии.
func mailFeedEdit(t *testing.T, root, rel string, fn func(string) string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	raw, err := os.ReadFile(p) // #nosec G304 -- путь заведён этой же пробой
	if err != nil {
		t.Fatal(err)
	}
	out := fn(string(raw))
	if out == string(raw) {
		t.Fatalf("правка %s ничего не изменила — инъекция меняет НОЛЬ фактов и "+
			"доказывает ноль", rel)
	}
	if err := os.WriteFile(p, []byte(out), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestMAIL48Injection_LawfulTreeIsSilent — ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ на копии.
// Проверка, краснеющая на исправном дереве, отличима от исправной только здесь.
func TestMAIL48Injection_LawfulTreeIsSilent(t *testing.T) {
	t.Parallel()
	if got := mailLaneFeedFindings(t, mailFeedCopyTree(t)); len(got) != 0 {
		t.Fatalf("законная копия дала %d находок, ожидалось ноль:\n%s",
			len(got), strings.Join(got, "\n"))
	}
}

// TestMAIL48Injection_SenderFedFromAnotherNode — дефект A: наш отправитель
// питается ДРУГИМ узлом значений. Находка обязана назвать ОБА пути: у дефекта
// нет «главного» места, и починка одной стороны без второй ничего не решает.
func TestMAIL48Injection_SenderFedFromAnotherNode(t *testing.T) {
	t.Parallel()
	root := mailFeedCopyTree(t)
	mailFeedEdit(t, root, mailSenderConfigTemplate, func(s string) string {
		return strings.Replace(s,
			"{{- $mailNode := (((.Values.global).kacho).identity).smtp | default dict }}",
			"{{- $mailNode := .Values.inviteMail | default dict }}", 1)
	})
	got := mailLaneFeedFindings(t, root)
	var hit string
	for _, f := range got {
		if strings.Contains(f, "РАЗЛИЧНЫ") {
			hit = f
		}
	}
	if hit == "" {
		t.Fatalf("расхождение путей питания НЕ найдено:\n%s", strings.Join(got, "\n"))
	}
	for _, want := range []string{"global.kacho.identity.smtp", "inviteMail"} {
		if !strings.Contains(hit, want) {
			t.Errorf("находка не называет путь %q:\n%s", want, hit)
		}
	}
}

// TestMAIL48Injection_CourierFedFromAnotherNode — та же ось с ДРУГОЙ стороны.
// Односторонняя проверка молчала бы на расхождении, заведённом поставщиком.
func TestMAIL48Injection_CourierFedFromAnotherNode(t *testing.T) {
	t.Parallel()
	root := mailFeedCopyTree(t)
	mailFeedEdit(t, root, identityConfigTemplate, func(s string) string {
		s = strings.Replace(s,
			"{{- $mailSmtp := .Values.global.kacho.identity.smtp }}",
			"{{- $mailSmtp := .Values.kratos.courierSmtp }}", 1)
		s = strings.ReplaceAll(s, ".Values.global.kacho.identity.smtp.fromAddress",
			".Values.kratos.courierSmtp.fromAddress")
		return strings.ReplaceAll(s, ".Values.global.kacho.identity.smtp.fromName",
			".Values.kratos.courierSmtp.fromName")
	})
	got := mailLaneFeedFindings(t, root)
	var found bool
	for _, f := range got {
		if strings.Contains(f, "РАЗЛИЧНЫ") && strings.Contains(f, "kratos.courierSmtp") {
			found = true
		}
	}
	if !found {
		t.Fatalf("расхождение, заведённое стороной поставщика, НЕ найдено:\n%s",
			strings.Join(got, "\n"))
	}
}

// TestMAIL48Injection_SenderSectionRemoved — дефект Б: раздел нашего
// отправителя не рендерится вовсе. Это НАХОДКА, а не сорванная предпосылка:
// отправитель без объявленной полосы не отправляет ничего, и его отказ
// неотличим от «почты на этой установке не бывает». Ровно это состояние дерево
// и несло до правки, которой заведён гейт.
func TestMAIL48Injection_SenderSectionRemoved(t *testing.T) {
	t.Parallel()
	root := mailFeedCopyTree(t)
	mailFeedEdit(t, root, mailSenderConfigTemplate, func(s string) string {
		return strings.Replace(s, "\n    invite-mail:\n", "\n    invite-mail-disabled:\n", 1)
	})
	got := mailLaneFeedFindings(t, root)
	var found bool
	for _, f := range got {
		if strings.Contains(f, "не рендерится шаблоном") && strings.Contains(f, "invite-mail") {
			found = true
		}
	}
	if !found {
		t.Fatalf("снятие раздела нашего отправителя НЕ дало находки:\n%s",
			strings.Join(got, "\n"))
	}
}

// TestMAIL48Injection_RecognizerKnowsBothSpellingsOfAValuesReference — разбор
// знает ОБА законных написания ссылки на узел значений: прямое и защищённое
// скобками. Написание, которого распознаватель не знает, уводит шаблон
// ИЗ-ПОД НАБЛЮДЕНИЯ — не давая ни красного, ни зелёного.
func TestMAIL48Injection_RecognizerKnowsBothSpellingsOfAValuesReference(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		body string
		want string
	}{
		{"{{- $a := .Values.global.kacho.identity.smtp }}", "global.kacho.identity.smtp"},
		{"{{- $a := (((.Values.global).kacho).identity).smtp | default dict }}", "global.kacho.identity.smtp"},
	} {
		feeds := mailVarFeeds(tc.body)
		if !feeds["a"][tc.want] {
			t.Errorf("написание %q не опознано: получено %v", tc.body, feeds["a"])
		}
	}
}

// TestMAIL48Injection_RecognizerResolvesAChainOfVariables — разбор идёт по
// цепочке переменных до неподвижной точки. Раздел `courier.smtp` берёт адрес
// узла не прямой ссылкой, а через четыре присваивания подряд: предикат,
// читающий только прямые ссылки, объявил бы у него питание из ДВУХ величин
// вместо трёх — то есть вывел бы путь, которого никто не объявлял.
func TestMAIL48Injection_RecognizerResolvesAChainOfVariables(t *testing.T) {
	t.Parallel()
	body := strings.Join([]string{
		"{{- $one := .Values.global.kacho.identity.smtp }}",
		`{{- $two := tpl (toString ($one.connectionURI | default "")) . }}`,
		`{{- $three := trimPrefix "smtp://" $two }}`,
		`{{- $four := printf "%s" $three }}`,
	}, "\n")
	feeds := mailVarFeeds(body)
	if !feeds["four"]["global.kacho.identity.smtp"] {
		t.Fatalf("цепочка из четырёх присваиваний не разрешена: %v", feeds["four"])
	}
}
