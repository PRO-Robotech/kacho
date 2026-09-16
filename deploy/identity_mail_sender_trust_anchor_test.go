// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_mail_sender_trust_anchor_test.go — якорь доверия к сертификату
// почтового узла получают ОБА отправителя полосы (посадка MAIL-05 приёмки
// ID-MAIL-1; задача службы доступа #142).
//
// ПРЕДМЕТ. На стенде приёмник писем предъявляет сертификат ВНУТРЕННЕГО
// удостоверяющего — того же, что и весь остальной стенд, — и требует STARTTLS
// (решение Р5, ban #16). В системных корнях образа такого якоря нет, поэтому
// проверка сертификата без объявленного якоря отвергает соединение — и
// отвергает ПРАВИЛЬНО: шифрование без проверки якоря защищает от подслушивания
// и не защищает от подмены.
//
// Отправителей у полосы ДВА (Р23): почтовый процесс поставщика личности и наш
// отправитель письма приглашения. Якорь — трастовый материал РАБОЧЕГО ОБЪЕКТА,
// а не раздел объявления полосы: у поставщика он приезжает переменной
// `SSL_CERT_FILE` его пода, у нас — переменной `KANAME_INVITE_MAIL__CA_BUNDLE_FILE`
// нашего пода, которую подчарт службы рендерит из ручки
// `kaname.inviteMail.caBundleFile`. Полоса объявлена одним местом
// (MAIL-48), а якорь выдаётся каждому рабочему объекту отдельно — значит
// выдать его одному и забыть второго можно МОЛЧА, и так и было: поставщику
// якорь выдан, нашему отправителю — ничем. Каждая попытка отправки давала бы
// «временный» отказ проверки сертификата, который повторами не лечится: десять
// попыток, отравленная строка очереди, письмо не ушло — при зелёной посадке.
//
// ЧТО ГЕЙТ УТВЕРЖДАЕТ — по каждому СТЕНДУ (цепочка из `stacks.txt`, а не файл:
// накладка вправе снять якорь, объявленный слоем ниже, и пофайловая проверка
// этого не увидит):
//
//	(1) полоса стенда названа на приёмник, который поднимает поставка ⇒
//	    поставщику выдан якорь: `SSL_CERT_FILE` у его рабочего объекта, и путь
//	    лежит в смонтированном каталоге;
//	(2) ⇒ нашему отправителю выдан якорь: `kaname.inviteMail.caBundleFile`
//	    непуст, и путь лежит под каталогом, который под службы монтирует
//	    (`kaname.mtls.mountPath` при включённом mtls — единственное место, где
//	    в этом поде есть `ca.crt` внутреннего удостоверяющего);
//	(3) шаблон рабочего объекта службы ЧИТАЕТ ручку и отдаёт её процессу под
//	    документированным именем — иначе ручка объявлена и не читается
//	    (`api-conventions.md` §«Принято-и-проигнорировано»).
//
// Стенд, чья полоса ведёт на внешний ретранслятор, под (1)–(2) не подпадает:
// его сертификат подписан публичным удостоверяющим, и системные корни ему
// годятся. Это законный близнец, и гейт на нём молчит — доказано инъекцией.
//
// ЧЕМ ЭТОТ ГЕЙТ ОТЛИЧАЕТСЯ ОТ СОСЕДЕЙ:
//   - `identity_mail_lane_feeds_both_senders_test.go` (MAIL-48) судит, что
//     узел, отправитель и удостоверение оба читателя берут из ОДНОГО узла
//     значений. Якорь в его предмет не входит — он и не величина полосы;
//   - `mail_receiver_core_test.go` судит, что названный полосой приёмник
//     поставка ПОДНИМАЕТ и что полоса до него шифрована. Он не спрашивает,
//     сможет ли отправитель ПРОВЕРИТЬ сертификат поднятого приёмника.
//
// Читаются ОБЪЯВЛЕНИЯ, а не рендер: ни helm, ни кластер не нужны, поэтому
// проверка не умеет пропускаться.
package deploy_test

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Имена, по которым якорь доезжает до КАЖДОГО из двух рабочих объектов.
const (
	// providerAnchorEnv — переменная, которой библиотека времени исполнения
	// службы личности читает дополнительный файл корней.
	providerAnchorEnv = "SSL_CERT_FILE"
	// senderAnchorEnv — переменная нашего процесса; имя ВЫВОДИТСЯ службой из
	// ключа `invite-mail.ca-bundle-file` (префикс, `.`→`__`, `-`→`_`), и что оно
	// доезжает до поля, держит проба службы доступа (config,
	// `TestDocumentedEnvName_InviteMailCABundleFile`).
	senderAnchorEnv = "KANAME_INVITE_MAIL__CA_BUNDLE_FILE"
	// senderDeploymentTemplate — шаблон рабочего объекта службы, который обязан
	// читать ручку и отдавать её процессу.
	senderDeploymentTemplate = "helm/umbrella/charts/kaname/templates/deployment.yaml"
)

// Пути значений умбреллы, по которым читаются ОБЪЯВЛЕНИЯ.
var (
	senderAnchorPath   = []string{"kaname", "inviteMail", "caBundleFile"}
	senderMTLSEnable   = []string{"kaname", "mtls", "enable"}
	senderMTLSMount    = []string{"kaname", "mtls", "mountPath"}
	providerDeployment = []string{"kratos", "deployment"}
)

// mailAnchorFacts — прочитанное о ПОЛОСЕ ОДНОГО СТЕНДА. Тип отделён от чтения
// дерева затем, чтобы доказательство инъекцией подавало суждению ВХОД, а не
// переписывало дерево.
type mailAnchorFacts struct {
	// Stack — имя стенда из `stacks.txt`.
	Stack string
	// Lane — объявленный адрес полосы (`global.kacho.identity.smtp.connectionURI`),
	// с раскрытым выражением имени релиза.
	Lane string
	// NamesRaisedReceiver — полоса ведёт на приёмник, который поднимает поставка.
	NamesRaisedReceiver bool
	// ProviderAnchor — значение `SSL_CERT_FILE` у рабочего объекта поставщика;
	// пусто ⇒ не выдан.
	ProviderAnchor string
	// ProviderMounts — каталоги, смонтированные рабочему объекту поставщика.
	ProviderMounts []string
	// SenderAnchor — `kaname.inviteMail.caBundleFile`; пусто ⇒ не выдан.
	SenderAnchor string
	// SenderMTLSEnabled / SenderMTLSMount — есть ли в нашем поде каталог с
	// `ca.crt` внутреннего удостоверяющего и где он.
	SenderMTLSEnabled bool
	SenderMTLSMount   string
}

// mailAnchorFindings — суждение над фактами стендов. Чистая функция.
func mailAnchorFindings(facts []mailAnchorFacts) []string {
	var out []string
	for _, f := range facts {
		if !f.NamesRaisedReceiver {
			continue
		}
		switch {
		case strings.TrimSpace(f.ProviderAnchor) == "":
			out = append(out, fmt.Sprintf(
				"стенд %q: полоса %q ведёт на приёмник с сертификатом внутреннего удостоверяющего, а "+
					"ПОЧТОВЫЙ ПРОЦЕСС ПОСТАВЩИКА якоря не получил — `%s` у `kratos.deployment.extraEnv` "+
					"не объявлен. Проверка пойдёт по системным корням, и письма подтверждения и "+
					"восстановления не уйдут ни разу",
				f.Stack, f.Lane, providerAnchorEnv))
		case !mountedUnder(f.ProviderAnchor, f.ProviderMounts):
			out = append(out, fmt.Sprintf(
				"стенд %q: `%s`=%q у рабочего объекта поставщика указывает в каталог, которого ему "+
					"не монтируют (смонтировано: %v) — якорь объявлен и не читается",
				f.Stack, providerAnchorEnv, f.ProviderAnchor, f.ProviderMounts))
		}
		switch {
		case strings.TrimSpace(f.SenderAnchor) == "":
			out = append(out, fmt.Sprintf(
				"стенд %q: полоса %q ведёт на приёмник с сертификатом внутреннего удостоверяющего, а "+
					"НАШ ОТПРАВИТЕЛЬ письма приглашения якоря не получил — `%s` пуст. "+
					"Проверка сертификата пойдёт по системным корням образа, где внутреннего "+
					"удостоверяющего нет, и каждая попытка отправки даст «временный» отказ, который "+
					"повторами не лечится: письмо приглашения не уйдёт ни разу при зелёной посадке "+
					"(задача службы доступа #142)",
				f.Stack, f.Lane, strings.Join(senderAnchorPath, ".")))
		case !f.SenderMTLSEnabled:
			out = append(out, fmt.Sprintf(
				"стенд %q: `%s`=%q объявлен, а `kaname.mtls.enable` выключен — каталога с `ca.crt` "+
					"внутреннего удостоверяющего в поде службы нет, и файл по этому пути не прочтётся; "+
					"процесс откажет в старте",
				f.Stack, strings.Join(senderAnchorPath, "."), f.SenderAnchor))
		case !mountedUnder(f.SenderAnchor, []string{f.SenderMTLSMount}):
			out = append(out, fmt.Sprintf(
				"стенд %q: `%s`=%q лежит вне каталога %q, единственного места, куда под службы "+
					"монтирует `ca.crt` внутреннего удостоверяющего (`kaname.mtls.mountPath`); файл "+
					"по этому пути не прочтётся, и процесс откажет в старте",
				f.Stack, strings.Join(senderAnchorPath, "."), f.SenderAnchor, f.SenderMTLSMount))
		}
	}
	sort.Strings(out)
	return out
}

// mountedUnder — файл лежит под одним из каталогов. Сравнение по СЕГМЕНТАМ:
// `/etc/kaname/tls-x/ca.crt` не лежит под `/etc/kaname/tls`.
func mountedUnder(file string, dirs []string) bool {
	clean := path.Clean(strings.TrimSpace(file))
	for _, d := range dirs {
		d = path.Clean(strings.TrimSpace(d))
		if d == "" || d == "." {
			continue
		}
		if clean == d || strings.HasPrefix(clean, d+"/") {
			return true
		}
	}
	return false
}

// providerAnchorOf — `SSL_CERT_FILE` и монтирования рабочего объекта поставщика
// из эффективных значений стенда.
func providerAnchorOf(effective map[string]any) (anchor string, mounts []string) {
	dep, ok := lookup(effective, providerDeployment...)
	if !ok {
		return "", nil
	}
	m, _ := dep.(map[string]any)
	if envs, ok := m["extraEnv"].([]any); ok {
		for _, e := range envs {
			em, _ := e.(map[string]any)
			if n, _ := em["name"].(string); n == providerAnchorEnv {
				anchor, _ = em["value"].(string)
			}
		}
	}
	if vms, ok := m["extraVolumeMounts"].([]any); ok {
		for _, v := range vms {
			vm, _ := v.(map[string]any)
			if p, _ := vm["mountPath"].(string); p != "" {
				mounts = append(mounts, p)
			}
		}
	}
	return anchor, mounts
}

// stringAt — строка по пути значений либо пусто.
func stringAt(tree map[string]any, p ...string) string {
	v, ok := lookup(tree, p...)
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

// mailAnchorFactsOf — факты по всем стендам дерева.
func mailAnchorFactsOf(t *testing.T) []mailAnchorFacts {
	t.Helper()
	suffix := receiverServiceSuffix(t)
	releases := stackReleaseNames(t)
	if len(releases) == 0 {
		t.Fatal("в рецепте не найдено ни одного имени релиза умбреллы — раскрыть " +
			"выражение полосы нечем, и «расхождений нет» означало бы «не судили»")
	}
	chains := deployStacks(t)
	base := readYAML(t, filepath.Join(umbrellaDir, "values.yaml"))
	// Умолчания подчарта службы — часть эффективных значений: `mtls.mountPath`
	// объявлен там, и стенд его не переписывает.
	subchart := readYAML(t, filepath.Join(umbrellaDir, "charts", "kaname", "values.yaml"))

	names := make([]string, 0, len(chains))
	for n := range chains {
		names = append(names, n)
	}
	sort.Strings(names)

	var out []mailAnchorFacts
	for _, name := range names {
		effective := mergeValues(map[string]any{}, base)
		effective = mergeValues(effective, map[string]any{"kaname": mergeValues(map[string]any{}, subchart)})
		for _, p := range chains[name] {
			effective = mergeValues(effective, readYAML(t, filepath.Join(umbrellaDir, p)))
		}
		f := mailAnchorFacts{Stack: name}
		f.Lane = expandedLane(declaredMailURI(effective), releases[0], suffix)
		if left := unknownLaneExpression(f.Lane); left != "" {
			t.Fatalf("стенд %q: полоса несёт выражение, которого гейт не знает: %s — судить нечем", name, left)
		}
		host := mailHostOf(f.Lane)
		f.NamesRaisedReceiver = f.Lane != "" && inClusterHost(host) &&
			strings.HasSuffix(strings.SplitN(host, ".", 2)[0], suffix)
		f.ProviderAnchor, f.ProviderMounts = providerAnchorOf(effective)
		f.SenderAnchor = stringAt(effective, senderAnchorPath...)
		if v, ok := lookup(effective, senderMTLSEnable...); ok {
			f.SenderMTLSEnabled, _ = v.(bool)
		}
		f.SenderMTLSMount = stringAt(effective, senderMTLSMount...)
		out = append(out, f)
	}
	return out
}

// senderTemplateReadsTheKnob — утверждение (3): находки о шаблоне рабочего
// объекта службы; пусто ⇒ ручка прочитана и отдана процессу под нужным именем.
//
// Судится ИСПОЛНЯЕМАЯ часть: строки-комментарии YAML и блоки `{{/* … */}}`
// сняты до сопоставления, иначе проза, объясняющая переменную, читалась бы как
// её объявление (`testing.md` §«Гейт на класс», п. 4).
func senderTemplateReadsTheKnob(body string) []string {
	exec := stripTemplateProse(body)
	var out []string
	knob := "." + strings.Join(senderAnchorPath[1:], ".")
	if !strings.Contains(exec, ".Values"+knob) {
		out = append(out, fmt.Sprintf(
			"%s не читает ручку `%s` — объявленная ручка без читателя выглядит настроенной и не "+
				"настраивает ничего", senderDeploymentTemplate, strings.Join(senderAnchorPath, ".")))
	}
	if !regexp.MustCompile(`(?m)^\s*-\s*name:\s*` + regexp.QuoteMeta(senderAnchorEnv) + `\s*$`).MatchString(exec) {
		out = append(out, fmt.Sprintf(
			"%s не отдаёт процессу переменную `%s` — процесс читает якорь только под этим именем "+
				"(ключ `invite-mail.ca-bundle-file` службы)", senderDeploymentTemplate, senderAnchorEnv))
	}
	return out
}

var (
	templateBlockCommentRe = regexp.MustCompile(`(?s)\{\{-?\s*/\*.*?\*/\s*-?\}\}`)
	yamlLineCommentRe      = regexp.MustCompile(`(?m)^\s*#.*$`)
)

// stripTemplateProse снимает комментарии шаблона и YAML.
func stripTemplateProse(body string) string {
	return yamlLineCommentRe.ReplaceAllString(templateBlockCommentRe.ReplaceAllString(body, ""), "")
}

// TestMAIL05BothMailSendersAreGivenTheTrustAnchor — сам гейт.
func TestMAIL05BothMailSendersAreGivenTheTrustAnchor(t *testing.T) {
	facts := mailAnchorFactsOf(t)
	withReceiver, providerGiven, senderGiven := 0, 0, 0
	for _, f := range facts {
		if !f.NamesRaisedReceiver {
			continue
		}
		withReceiver++
		if strings.TrimSpace(f.ProviderAnchor) != "" {
			providerGiven++
		}
		if strings.TrimSpace(f.SenderAnchor) != "" {
			senderGiven++
		}
	}
	t.Logf("перепись: стендов прочитано %d · из них полоса ведёт на поднимаемый приёмник %d · "+
		"якорь выдан поставщику %d · нашему отправителю %d",
		len(facts), withReceiver, providerGiven, senderGiven)
	if len(facts) == 0 {
		t.Fatal("прочитано ноль стендов — «находок нет» означало бы «ничего не прочитано»")
	}
	if withReceiver == 0 {
		t.Fatal("ни один стенд не ведёт полосу на поднимаемый приёмник — предмет гейта отсутствует, " +
			"и его молчание ничего не значит (профиль стенда объявляет приёмник и полосу до него)")
	}
	for _, f := range mailAnchorFindings(facts) {
		t.Error(f)
	}

	raw := readFileOrFatal(t, senderDeploymentTemplate)
	for _, f := range senderTemplateReadsTheKnob(raw) {
		t.Error(f)
	}
}

func readFileOrFatal(t *testing.T, file string) string {
	t.Helper()
	raw, err := os.ReadFile(file) // #nosec G304 -- координата из константы
	if err != nil {
		t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: %s не читается: %v", file, err)
	}
	return string(raw)
}
