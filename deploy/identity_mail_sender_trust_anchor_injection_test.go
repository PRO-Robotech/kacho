// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_mail_sender_trust_anchor_injection_test.go — доказательство того,
// что гейт якоря доверия (identity_mail_sender_trust_anchor_test.go) СПОСОБЕН
// упасть, и падает ровно на своём предмете, а на законном близнеце молчит
// (`testing.md` §«Гейт на класс», п. 2).
//
// Суждение — чистая функция над фактами, поэтому дефект вносится ВХОДОМ, а не
// правкой дерева: копия дерева здесь не заводится, состояние, которого проба не
// заводила, она не трогает. Отдельная проба ниже подтверждает, что чтение
// дерева даёт суждению те факты, о которых оно судит (распознаватель узнаёт
// настоящий стенд).
package deploy_test

import (
	"strings"
	"testing"
)

// anchorLegalStand — стенд, у которого якорь выдан ОБОИМ отправителям и лежит
// там, где смонтирован. Положительный контроль всех инъекций ниже.
func anchorLegalStand(name string) mailAnchorFacts {
	return mailAnchorFacts{
		Stack:               name,
		Lane:                "smtp://kacho-umbrella-mailpit:1025/",
		NamesRaisedReceiver: true,
		ProviderAnchor:      "/etc/kacho-mail-anchor/ca.crt",
		ProviderMounts:      []string{"/etc/kaname-identity-rendered", "/etc/kacho-mail-anchor"},
		SenderAnchor:        "/etc/kaname/tls/server/ca.crt",
		SenderMTLSEnabled:   true,
		SenderMTLSMount:     "/etc/kaname/tls",
	}
}

func TestMailAnchorFindings_SilentOnALegalStand(t *testing.T) {
	t.Parallel()
	if got := mailAnchorFindings([]mailAnchorFacts{anchorLegalStand("dev")}); len(got) != 0 {
		t.Fatalf("стенд с якорями у обоих отправителей дал находки: %v", got)
	}
}

// Законный близнец: полоса ведёт на ВНЕШНИЙ ретранслятор — его сертификат
// подписан публичным удостоверяющим, и якоря не нужно ни одному отправителю.
func TestMailAnchorFindings_SilentOnAnExternalRelay(t *testing.T) {
	t.Parallel()
	f := mailAnchorFacts{
		Stack:               "fe3455",
		Lane:                "smtp://noreply%40kacho.cloud@relay.example:587/",
		NamesRaisedReceiver: false,
	}
	if got := mailAnchorFindings([]mailAnchorFacts{f}); len(got) != 0 {
		t.Fatalf("внешний ретранслятор без якорей дал находки: %v", got)
	}
}

func TestMailAnchorFindings_RedWhenOurSenderHasNoAnchor(t *testing.T) {
	t.Parallel()
	f := anchorLegalStand("dev")
	f.SenderAnchor = "   "
	got := mailAnchorFindings([]mailAnchorFacts{f, anchorLegalStand("prod-like")})
	if len(got) != 1 {
		t.Fatalf("ожидалась ровно одна находка, получено %d: %v", len(got), got)
	}
	for _, want := range []string{`стенд "dev"`, "НАШ ОТПРАВИТЕЛЬ", "kaname.inviteMail.caBundleFile", "#142"} {
		if !strings.Contains(got[0], want) {
			t.Errorf("находка не называет %q: %s", want, got[0])
		}
	}
	if strings.Contains(got[0], `"prod-like"`) {
		t.Errorf("находка задела стенд без дефекта: %s", got[0])
	}
}

func TestMailAnchorFindings_RedWhenTheProviderHasNoAnchor(t *testing.T) {
	t.Parallel()
	f := anchorLegalStand("dev")
	f.ProviderAnchor = ""
	got := mailAnchorFindings([]mailAnchorFacts{f})
	if len(got) != 1 {
		t.Fatalf("ожидалась ровно одна находка, получено %d: %v", len(got), got)
	}
	for _, want := range []string{`стенд "dev"`, "ПОЧТОВЫЙ ПРОЦЕСС ПОСТАВЩИКА", "SSL_CERT_FILE"} {
		if !strings.Contains(got[0], want) {
			t.Errorf("находка не называет %q: %s", want, got[0])
		}
	}
}

// Якорь объявлен, но лежит вне каталога, который поду монтируют, — файл не
// прочтётся, и процесс откажет в старте. Гейт обязан назвать это ДО выкатки.
func TestMailAnchorFindings_RedWhenOurAnchorIsNotMounted(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		mutate func(*mailAnchorFacts)
		want   string
	}{
		{"путь вне каталога mtls", func(f *mailAnchorFacts) { f.SenderAnchor = "/etc/kacho-mail-anchor/ca.crt" }, "лежит вне каталога"},
		{"сосед по префиксу — не под каталогом", func(f *mailAnchorFacts) { f.SenderAnchor = "/etc/kaname/tls-x/ca.crt" }, "лежит вне каталога"},
		{"mtls выключен — каталога нет", func(f *mailAnchorFacts) { f.SenderMTLSEnabled = false }, "kaname.mtls.enable"},
		{"поставщику путь без монтирования", func(f *mailAnchorFacts) { f.ProviderMounts = []string{"/etc/kaname-identity-rendered"} }, "не монтируют"},
	} {
		f := anchorLegalStand("dev")
		tc.mutate(&f)
		got := mailAnchorFindings([]mailAnchorFacts{f})
		if len(got) != 1 || !strings.Contains(got[0], tc.want) {
			t.Errorf("%s: ожидалась одна находка со словами %q, получено %v", tc.name, tc.want, got)
		}
	}
}

// Шаблон рабочего объекта: ручка без читателя и переменная под другим именем
// — находки; упоминание в КОММЕНТАРИИ читателем не является.
func TestSenderTemplateReadsTheKnob_Injection(t *testing.T) {
	t.Parallel()
	legal := `
            {{- with .Values.inviteMail.caBundleFile }}
            - name: KANAME_INVITE_MAIL__CA_BUNDLE_FILE
              value: {{ . | quote }}
            {{- end }}
`
	if got := senderTemplateReadsTheKnob(legal); len(got) != 0 {
		t.Fatalf("законный шаблон дал находки: %v", got)
	}
	onlyProse := `
            {{- /* .Values.inviteMail.caBundleFile рендерится ниже */}}
            # - name: KANAME_INVITE_MAIL__CA_BUNDLE_FILE
            - name: KANAME_HYDRA_ADMIN_CA_FILE
              value: "x"
`
	got := senderTemplateReadsTheKnob(onlyProse)
	if len(got) != 2 {
		t.Fatalf("шаблон, называющий ручку и переменную только прозой, обязан дать две находки, получено %d: %v", len(got), got)
	}
	wrongName := `
            {{- with .Values.inviteMail.caBundleFile }}
            - name: KANAME_INVITE_MAIL_CA_BUNDLE_FILE
              value: {{ . | quote }}
            {{- end }}
`
	got = senderTemplateReadsTheKnob(wrongName)
	if len(got) != 1 || !strings.Contains(got[0], "KANAME_INVITE_MAIL__CA_BUNDLE_FILE") {
		t.Fatalf("плоское имя без `__` процесс не читает; ожидалась одна находка об имени, получено %v", got)
	}
}

// Распознаватель узнаёт НАСТОЯЩИЙ стенд: чтение дерева даёт суждению факты о
// стенде, ведущем полосу на приёмник, с якорями у обоих отправителей. Без этой
// пробы инъекции выше доказывали бы суждение, а не гейт.
func TestMailAnchorFacts_RecogniseTheRealTree(t *testing.T) {
	facts := mailAnchorFactsOf(t)
	var dev *mailAnchorFacts
	for i := range facts {
		if facts[i].Stack == "dev" {
			dev = &facts[i]
		}
	}
	if dev == nil {
		t.Fatal("стенд `dev` не прочитан из stacks.txt — распознаватель ослеп")
	}
	if !dev.NamesRaisedReceiver {
		t.Fatalf("стенд `dev` не распознан как ведущий полосу на поднимаемый приёмник (полоса %q)", dev.Lane)
	}
	if dev.ProviderAnchor == "" || len(dev.ProviderMounts) == 0 {
		t.Fatalf("якорь поставщика не прочитан: %q, монтирования %v", dev.ProviderAnchor, dev.ProviderMounts)
	}
	if dev.SenderAnchor == "" || !dev.SenderMTLSEnabled || dev.SenderMTLSMount == "" {
		t.Fatalf("якорь нашего отправителя не прочитан: %q, mtls %t, каталог %q",
			dev.SenderAnchor, dev.SenderMTLSEnabled, dev.SenderMTLSMount)
	}
}
