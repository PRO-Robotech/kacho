// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// token_revocation_transport_declared_test.go — ВТОРОЙ страж приёма токена:
// стенд, читающий отзыв, объявляет учётные данные ребра к авторитету отзыва.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Сосед по каталогу (token_acceptance_declared_test.go) спрашивает у профиля то,
// что спросит `Config.TokenAcceptance`: кого принимаем, откуда берём набор
// ключей, объявлен ли авторитет отзыва. Этого достаточно, чтобы объявление
// РАЗОБРАЛОСЬ, и недостаточно, чтобы процесс ПОДНЯЛСЯ: приняв наш издатель,
// сборка проверяющего строит читателя отзыва, а он требует учётных данных ребра.
// Их даёт вторая, независимая ручка — `mtls.edges.tokenRevocation`.
//
// Два объявления, два стража, одна посадка. Профиль вправе объявить первое и не
// объявить второе — и тогда отказ приходит ВТОРЫМ, уже после того, как первый
// страж пропустил. Так и наблюдалось: приём издателей приняли, следующий отказ
// пришёл на отсутствии клиентской пары.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ЭТО ЗАКРЫВАЕТ (замер, а не предположение)
//
// Снятие `mtls.edges.tokenRevocation` из профиля посадки не роняло в дереве
// НИЧЕГО: `go test ./deploy/... ./services/registry/deploy/...` выходил нулём,
// при том что настоящий читатель на том же объявлении отвечает отказом
// «revocation authority is reached over https but its client credentials are not
// declared». То есть отказ был верен, воспроизводим и невидим до кластера.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ОБЪЯВЛЕНИЕ, А НЕ РЕНДЕР
//
// Та же причина, что у соседей в deploy/: контракт — то, что профиль
// ОБЪЯВЛЯЕТ, и проверке не нужны ни `helm`, ни скачанные подчарты, поэтому она
// не умеет пропуститься. Рендер здесь и не помог бы: при невыданной ручке
// манифест выезжает синтаксически безупречным — в нём просто НЕТ пяти
// переменных, и увидеть это можно только зная, что они обязаны быть.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ЗОВЁТСЯ НАСТОЯЩИЙ ЧИТАТЕЛЬ И ЧТО ЗАМЕЩЕНО
//
// Вердикт выносит `jwks.NewIntrospectionReader` — ТА ЖЕ сборка, которую делает
// процесс при старте. Второй предикат, сформулированный здесь заново, разошёлся
// бы с первым молча.
//
// Замещено ровно одно и это названо: СОДЕРЖИМОЕ смонтированных файлов. Якорь и
// пара — свойство кластера (их выпускает cert-manager), а не дерева; поэтому
// проверка кладёт настоящие файлы во временный каталог и подставляет ЕГО
// вместо `mtls.mountPath`. Всё, что решается ОБЪЯВЛЕНИЕМ — включена ли ручка,
// закрыт ли адрес, названо ли имя сервера, полна ли пара, — читатель решает до
// первого обращения к файлу, поэтому подстановка ничего не ослабляет. Что
// `mountPath` при этом непуст, утверждается отдельно: пустой путь дал бы
// непустые имена файлов, то есть объявление выглядело бы полным.
package deploy_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/services/registry/internal/clients/jwks"
)

// revocationEdgeEnvNames — пять переменных ребра отзыва. Шаблон выдаёт их
// ВМЕСТЕ либо не выдаёт вовсе: половина пары хуже отсутствия обеих, потому что
// выглядит настроенной.
var revocationEdgeEnvNames = []string{
	"KACHO_REGISTRY_TOKEN_REVOCATION_MTLS_ENABLE",
	"KACHO_REGISTRY_TOKEN_REVOCATION_MTLS_CERTFILE",
	"KACHO_REGISTRY_TOKEN_REVOCATION_MTLS_KEYFILE",
	"KACHO_REGISTRY_TOKEN_REVOCATION_MTLS_CAFILES",
	"KACHO_REGISTRY_TOKEN_REVOCATION_MTLS_SERVERNAME",
}

// envNameLine — имя переменной окружения шаблона, взятое ЦЕЛИКОМ.
//
// Привязка к концу имени здесь не украшение: проверка по подстроке зеленеет на
// переименованной переменной (`..._CERTFILE_XX` содержит `..._CERTFILE`), то
// есть перестаёт измерять то, ради чего заведена. Этот класс в дереве уже
// ловили, и самопроверка ниже показывает обе стороны на одном входе.
var envNameLine = regexp.MustCompile(`^\s*-\s+name:\s+([A-Za-z0-9_]+)\s*$`)

// templateAction — ОДНО действие шаблона. Обход считает вложенность, чтобы
// ответить не «переменная есть», а «переменная выдаётся ПРИ ТОМ ЖЕ условии»:
// переменная, уехавшая на уровень выше, выдаётся всегда — и тогда объявление
// стенда без ребра выглядит полным.
//
// Считаются ДЕЙСТВИЯ, а не строки: в шаблоне есть строка, несущая `if`, `else` и
// `end` разом, и построчный счёт на ней ошибается на единицу. Обход это не
// проглотил, а отказался работать — см. проверку остатка ниже.
var templateAction = regexp.MustCompile(`\{\{-?\s*(.*?)\s*-?\}\}`)

const (
	guardEnableCondition = ".Values.mtls.enable"
	guardEdgeCondition   = ".Values.mtls.edges.tokenRevocation"
)

// revocationEdgeTemplatePremise — предпосылка, на которой стоит эта проверка.
//
// Проверка строит учётные данные из ОБЪЯВЛЕННОГО так, как их строит шаблон.
// Значит форма шаблона — часть предмета, а не деталь: изменится она — вердикт
// станет относиться к посадке, которой не существует, и узнать об этом надо
// здесь, а не по красному стенду.
//
// Возвращает пояснение (для переписи) либо причину отказа.
func revocationEdgeTemplatePremise(text string) (string, error) {
	lines := strings.Split(text, "\n")

	// (1) Каждое имя встречается РОВНО раз, и сравнение идёт по имени целиком.
	occurrences := map[string]int{}
	for _, ln := range lines {
		m := envNameLine.FindStringSubmatch(ln)
		if m == nil {
			continue
		}
		occurrences[m[1]]++
	}
	for _, name := range revocationEdgeEnvNames {
		switch occurrences[name] {
		case 1:
		case 0:
			return "", fmt.Errorf("предпосылка изменилась: переменной %s в шаблоне нет — "+
				"ребро отзыва больше не выдаётся, и предмет этой проверки надо переопределить", name)
		default:
			return "", fmt.Errorf("предпосылка изменилась: %s объявлена %d раз(а) вместо одного — "+
				"ребро стало условным, и «выдаётся вместе» перестало быть свойством шаблона",
				name, occurrences[name])
		}
	}

	// (2) Все пять стоят под ОДНОЙ парой условий: mtls.enable и edges.tokenRevocation.
	var stack []string
	depthAt := map[string][]string{}
	for _, ln := range lines {
		if m := envNameLine.FindStringSubmatch(ln); m != nil {
			depthAt[m[1]] = append([]string(nil), stack...)
		}
		for _, act := range templateAction.FindAllStringSubmatch(ln, -1) {
			body := act[1]
			word := body
			if i := strings.IndexAny(word, " \t"); i >= 0 {
				word = word[:i]
			}
			switch word {
			case "if", "range", "with", "define", "block":
				stack = append(stack, body)
			case "end":
				if len(stack) == 0 {
					return "", fmt.Errorf("предпосылка изменилась: шаблон не разбирается этим обходом "+
						"(закрытие ветвления без открытия, строка %q) — вложенность посчитать нельзя",
						strings.TrimSpace(ln))
				}
				stack = stack[:len(stack)-1]
			}
		}
	}
	if len(stack) != 0 {
		return "", fmt.Errorf("предпосылка изменилась: шаблон не разбирается этим обходом "+
			"(%d незакрытых ветвлений) — вложенность посчитать нельзя", len(stack))
	}
	for _, name := range revocationEdgeEnvNames {
		// Сравнение ТОЧНОЕ, а не по вхождению, и это та же граница имени, что у
		// переменных. `.Values.mtls.enable` входит подстрокой и в `if or
		// .Values.mtls.enable (and …)` — условие ЗАМЕТНО более слабое: под ним
		// переменные выезжают и без взаимного TLS. Проверка по вхождению приняла
		// бы такой переезд за прежнюю посадку.
		guards := map[string]bool{}
		for _, g := range depthAt[name] {
			guards[g] = true
		}
		if !guards["if "+guardEnableCondition] || !guards["if "+guardEdgeCondition] {
			return "", fmt.Errorf("предпосылка изменилась: %s выдаётся НЕ под парой условий "+
				"%s + %s (её ветвления: %v) — учётные данные ребра перестали приезжать вместе, "+
				"и половина пары выглядела бы настройкой",
				name, guardEnableCondition, guardEdgeCondition, depthAt[name])
		}
	}
	return fmt.Sprintf("переменных ребра — %d, все под %s + %s",
		len(revocationEdgeEnvNames), guardEnableCondition, guardEdgeCondition), nil
}

// declaredRevocationTransport — учётные данные ребра, КАК ИХ ВЫДАЁТ ШАБЛОН из
// объявленного профилями. mountDir подставляется вместо mtls.mountPath: см.
// «что замещено» в шапке файла.
func declaredRevocationTransport(reg map[string]any, mountDir string) jwks.RevocationTransport {
	enable := scalar(digOpt(reg, "mtls", "enable")) == "true" &&
		scalar(digOpt(reg, "mtls", "edges", "tokenRevocation")) == "true"
	if !enable {
		// Блок шаблона не выдаётся вовсе — ни одной из пяти переменных нет.
		return jwks.RevocationTransport{}
	}
	client := filepath.Join(mountDir, "client")
	return jwks.RevocationTransport{
		Enable:     true,
		CAFiles:    []string{filepath.Join(client, "ca.crt")},
		CertFile:   filepath.Join(client, "tls.crt"),
		KeyFile:    filepath.Join(client, "tls.key"),
		ServerName: scalar(digOpt(reg, "mtls", "serverName", "tokenRevocation")),
	}
}

// supplierOf — какой профиль цепочки объявил значение последним.
//
// Ради этой строки проверка и печатает перепись: «ноль попаданий в файле
// профиля» и «профиль не объявляет» — РАЗНЫЕ утверждения, и их путали. Профиль
// посадки на управляемый кластер объявляет только образы, а приём наследует у
// слоя под собой; поиск по одному файлу этого не видит и читает наследование
// как забывчивость. Здесь ответ называется именем профиля.
func supplierOf(t *testing.T, chain []string, path ...string) string {
	t.Helper()
	supplier := "(никто — умолчание чарта)"
	for _, profile := range chain {
		sub, ok := umbrellaValues(t, profile)["registry"].(map[string]any)
		if !ok {
			continue
		}
		if _, found := digOpt(sub, path...); found {
			supplier = profile
		}
	}
	return supplier
}

// TestEveryDeployedStackDeclaresARevocationTransportThatBoots — сама проверка.
func TestEveryDeployedStackDeclaresARevocationTransportThatBoots(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("templates", "deployment.yaml"))
	if err != nil {
		t.Fatalf("read templates/deployment.yaml: %v", err)
	}
	premise, perr := revocationEdgeTemplatePremise(string(raw))
	if perr != nil {
		t.Fatalf("%v", perr)
	}
	t.Logf("предпосылка шаблона: %s", premise)

	mountDir := materialiseEdgeCredentials(t)

	chains := deployStackChains(t)
	names := make([]string, 0, len(chains))
	for n := range chains {
		names = append(names, n)
	}
	sort.Strings(names)

	examined, withRegistry, readsRevocation, declaresTransport := 0, 0, 0, 0
	for _, name := range names {
		examined++
		reg := effectiveRegistryValues(t, chains[name])
		if enabled, ok := digOpt(reg, "enabled"); !ok || enabled != true {
			t.Logf("стенд %q: реестр не поднимается — ребра отзыва у него нет", name)
			continue
		}
		withRegistry++

		cfg := declaredAcceptance(reg)
		bindings, aerr := cfg.TokenAcceptance()
		if aerr != nil {
			// Первый страж — предмет соседней проверки; здесь это не находка, а
			// причина, по которой о втором сказать нечего.
			t.Logf("стенд %q: приём токена не разбирается (предмет соседней проверки), "+
				"о ребре отзыва судить не по чему", name)
			continue
		}
		reads := false
		for _, b := range bindings {
			reads = reads || b.ReadRevocation
		}
		mtlsSupplier := supplierOf(t, chains[name], "mtls", "edges", "tokenRevocation")
		if !reads {
			t.Logf("стенд %q: отзыв на предъявлении не читается — учётные данные ребра не нужны "+
				"(ручку ребра объявил: %s)", name, mtlsSupplier)
			continue
		}
		readsRevocation++

		if mp := strings.TrimSpace(scalar(digOpt(reg, "mtls", "mountPath"))); mp == "" {
			t.Errorf("стенд %q (цепочка %s): mtls.mountPath пуст — имена файлов ребра выродились бы "+
				"в непустые строки, и неполное объявление выглядело бы полным",
				name, strings.Join(chains[name], " → "))
			continue
		}

		if _, rerr := jwks.NewIntrospectionReader(cfg.TokenRevocationURL,
			declaredRevocationTransport(reg, mountDir)); rerr != nil {
			t.Errorf("стенд %q (цепочка %s): отзыв читается на предъявлении, но с объявленными "+
				"учётными данными ребра плоскость данных НЕ ПОДНИМЕТСЯ — сборка проверяющего отвечает:\n"+
				"    %v\n"+
				"Объяви registry.mtls.edges.tokenRevocation: true в профиле этой цепочки "+
				"(сейчас ручку объявил: %s). Отказ здесь верен: авторитет отвергает вызывающего, "+
				"чью цепочку транспорт не проверил, поэтому контроль существовал бы и не отказал "+
				"НИ РАЗУ — каждый его вызов срывался бы на рукопожатии",
				name, strings.Join(chains[name], " → "), rerr, mtlsSupplier)
			continue
		}
		declaresTransport++
		t.Logf("стенд %q: отзыв читается, учётные данные ребра объявил %s (издателей объявил %s)",
			name, mtlsSupplier, supplierOf(t, chains[name], "tokenAcceptance", "issuers"))
	}

	// «Ноль находок» обязано быть отличимо от «ноль прочитанного».
	if examined == 0 || withRegistry == 0 || readsRevocation == 0 {
		t.Fatalf("обход ничего не осмотрел: стендов=%d, из них с реестром=%d, из них читают отзыв=%d — "+
			"предикат перестал узнавать дерево, а не дерево стало чистым",
			examined, withRegistry, readsRevocation)
	}
	t.Logf("осмотрено: стендов=%d, поднимают реестр=%d, читают отзыв=%d, объявили ребро=%d",
		examined, withRegistry, readsRevocation, declaresTransport)
}

// materialiseEdgeCredentials кладёт настоящие якорь и пару во временный
// каталог. Замещается СОДЕРЖИМОЕ файлов — свойство кластера, а не дерева.
func materialiseEdgeCredentials(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	client := filepath.Join(dir, "client")
	if err := os.MkdirAll(client, 0o750); err != nil {
		t.Fatalf("mkdir %s: %v", client, err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "kacho-registry"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	for name, body := range map[string][]byte{"ca.crt": certPEM, "tls.crt": certPEM, "tls.key": keyPEM} {
		if err := os.WriteFile(filepath.Join(client, name), body, 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return dir
}

// ─────────────────────────────────────────────────────────────────────────────
// САМОПРОВЕРКА — инъекция в обе стороны.
//
// Без положительного контроля отрицание зеленеет на предикате, отвергающем всё:
// «объявление негодно» и «предикат сломан» дают одинаково красное.

// TestRevocationEdgeTemplatePremise_SelfTest — предпосылка шаблона.
func TestRevocationEdgeTemplatePremise_SelfTest(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("templates", "deployment.yaml"))
	if err != nil {
		t.Fatalf("read templates/deployment.yaml: %v", err)
	}
	actual := string(raw)

	// (б) положительный контроль: настоящий шаблон предпосылке отвечает.
	if _, perr := revocationEdgeTemplatePremise(actual); perr != nil {
		t.Fatalf("настоящий шаблон отвергнут предпосылкой: %v", perr)
	}

	renamed := strings.Replace(actual,
		"- name: KACHO_REGISTRY_TOKEN_REVOCATION_MTLS_CERTFILE\n",
		"- name: KACHO_REGISTRY_TOKEN_REVOCATION_MTLS_CERTFILE_XX\n", 1)
	if renamed == actual {
		t.Fatalf("фикстура не создала условие: строку с объявлением CERTFILE подменить не удалось — " +
			"форма объявления в шаблоне изменилась, и самопроверка перестала проверять переименование")
	}
	movedOut := strings.Replace(actual,
		"            {{- if .Values.mtls.edges.tokenRevocation }}\n",
		"", 1)
	if movedOut == actual {
		t.Fatalf("фикстура не создала условие: ветвление ребра отзыва не найдено")
	}
	// Ветвление ребра, ослабленное до условия, которое выполняется и без
	// взаимного TLS: переменные выдаются, а учётных данных под ними нет.
	weakened := strings.Replace(actual,
		"            {{- if .Values.mtls.edges.tokenRevocation }}\n",
		"            {{- if or .Values.mtls.edges.tokenRevocation .Values.zot.enabled }}\n", 1)
	if weakened == actual {
		t.Fatalf("фикстура не создала условие: ветвление ребра отзыва не найдено для ослабления")
	}

	// Ветвление снято — закрытие осталось лишним; вернём баланс, чтобы дефект
	// был именно «переменная выдаётся не под тем условием», а не «шаблон не
	// разбирается»: иначе проба зеленела бы по НЕ ТОЙ причине.
	movedOut = strings.Replace(movedOut,
		"            - name: KACHO_REGISTRY_TOKEN_REVOCATION_MTLS_SERVERNAME\n"+
			"              value: {{ .Values.mtls.serverName.tokenRevocation | quote }}\n"+
			"            {{- end }}\n",
		"            - name: KACHO_REGISTRY_TOKEN_REVOCATION_MTLS_SERVERNAME\n"+
			"              value: {{ .Values.mtls.serverName.tokenRevocation | quote }}\n", 1)

	cases := []struct {
		name    string
		text    string
		wantErr string // подстрока, которую обязано назвать сообщение; "" = обязан молчать
	}{
		{"настоящий шаблон", actual, ""},
		{"переменная переименована", renamed, "KACHO_REGISTRY_TOKEN_REVOCATION_MTLS_CERTFILE"},
		{"переменные выехали из-под условия ребра", movedOut, guardEdgeCondition},
		{"ветвление ребра ослаблено до `or`", weakened, guardEdgeCondition},
		{"переменная объявлена дважды", actual + "\n            - name: KACHO_REGISTRY_TOKEN_REVOCATION_MTLS_KEYFILE\n", "вместо одного"},
		{"шаблон не разбирается", actual + "\n{{- end }}\n", "не разбирается этим обходом"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, perr := revocationEdgeTemplatePremise(tc.text)
			if tc.wantErr == "" {
				if perr != nil {
					t.Fatalf("законная форма отвергнута: %v", perr)
				}
				return
			}
			if perr == nil {
				t.Fatalf("внесённый дефект не пойман (ждали упоминания %q)", tc.wantErr)
			}
			if !strings.Contains(perr.Error(), tc.wantErr) {
				t.Fatalf("находка без координаты: сообщение %q не называет %q", perr.Error(), tc.wantErr)
			}
		})
	}

	// ГРАНИЦА ИМЕНИ — названа отдельным утверждением, а не подразумевается.
	//
	// Проверка по подстроке на переименованной переменной ЗЕЛЕНЕЕТ: `..._CERTFILE_XX`
	// содержит `..._CERTFILE`. Этот класс в дереве уже ловили, поэтому здесь
	// показаны ОБЕ стороны на одном входе — иначе «предикат привязан к имени
	// целиком» осталось бы утверждением о намерении.
	t.Run("граница имени: подстрока не отличает переименованную переменную", func(t *testing.T) {
		const name = "KACHO_REGISTRY_TOKEN_REVOCATION_MTLS_CERTFILE"
		if !strings.Contains(renamed, name) {
			t.Fatalf("фикстура не создала условие: переименованный текст не содержит %q как подстроку, "+
				"и контроль ничего не показывает", name)
		}
		if _, perr := revocationEdgeTemplatePremise(renamed); perr == nil {
			t.Fatalf("привязка к имени целиком не работает: предикат принял переименованную переменную")
		}
	})
}

// TestDeclaredRevocationTransport_SelfTest — объявление профиля против
// НАСТОЯЩЕЙ сборки читателя отзыва.
func TestDeclaredRevocationTransport_SelfTest(t *testing.T) {
	mountDir := materialiseEdgeCredentials(t)
	const (
		secureURL = "https://kaname-internal.kacho.svc:9097/internal/tokens/introspect"
		plainURL  = "http://127.0.0.1:9097/internal/tokens/introspect"
	)
	reg := func(enable, edge bool, serverName string) map[string]any {
		return map[string]any{
			"mtls": map[string]any{
				"enable":     enable,
				"edges":      map[string]any{"tokenRevocation": edge},
				"serverName": map[string]any{"tokenRevocation": serverName},
				"mountPath":  "/etc/kacho-registry/tls",
			},
		}
	}
	const sn = "kaname-internal.kacho.svc"

	cases := []struct {
		name    string
		url     string
		reg     map[string]any
		wantErr string
	}{
		// (а) внесённый дефект — ровно тот, что оставил кластер на прежнем образе.
		{
			"ручка ребра не объявлена, авторитет закрыт",
			secureURL, reg(true, false, sn),
			"client credentials are not declared",
		},
		{
			"взаимный TLS выключен целиком, авторитет закрыт",
			secureURL, reg(false, true, sn),
			"client credentials are not declared",
		},
		{
			"ребро объявлено, имя сервера не названо",
			secureURL, reg(true, true, ""),
			"no server name is declared",
		},
		{
			"ребро объявлено, авторитет открыт",
			plainURL, reg(true, true, sn),
			"client credentials mean nothing without TLS",
		},

		// (б) законные объявления ТОЙ ЖЕ формы — обязан молчать.
		{
			"боевая посадка: ребро объявлено, авторитет закрыт",
			secureURL, reg(true, true, sn), "",
		},
		{
			"посадка разработки: авторитет открыт, учётных данных нет",
			plainURL, reg(false, false, sn), "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := jwks.NewIntrospectionReader(tc.url, declaredRevocationTransport(tc.reg, mountDir))
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("законное объявление отвергнуто: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("внесённый дефект не пойман — проверка приняла объявление, "+
					"с которым процесс не стартует (ждали упоминания %q)", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("находка без координаты: сообщение %q не называет %q", err.Error(), tc.wantErr)
			}
		})
	}
}
