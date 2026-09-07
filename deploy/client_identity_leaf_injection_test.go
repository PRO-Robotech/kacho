// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// client_identity_leaf_injection_test.go — доказательство того, что проверка
// клиентской личности СПОСОБНА упасть и способна смолчать.
//
// Вход подаётся СИНТЕТИЧЕСКИЙ, а не подделкой дерева: подделка дерева трогает
// общий клон, а вердикт обязан доказываться на входе, который построен здесь и
// целиком виден читателю. Тот же порядок, что у соседнего
// kaname_listener_knobs_injection_test.go.
//
// Каждый случай меняет РОВНО ОДИН факт против законного близнеца: иначе
// неизвестно, который из двух дал вердикт.
//
// Доказываются ТРИ вещи, и третья — не украшение: распознаватель обязан знать
// ОБЕ законные формы записи предмета и обязан НЕ считать предметом серверный
// TLS слушателя, объявленный вне блока `mtls`. Форма, о которой распознаватель
// не знает, не даёт ни красного, ни зелёного — она молчит, и всё записанное в
// ней оказывается вне наблюдения (testing.md §«Гейт на класс», п.7).
package deploy_test

import (
	"strings"
	"testing"
)

// legalCertScope — законная раскладка: лист слушателя и личность исходящего
// вызова живут в РАЗНЫХ каталогах, секрет клиента объявлен.
func legalCertScope() certScope {
	return certScope{
		chart:        "чарт",
		workload:     true,
		clientSecret: "сосед-client-tls",
		client: []certDecl{{
			file: "templates/deployment.yaml", line: 10,
			name: "KACHO_СОСЕД_GEO_MTLS_CERTFILE",
			path: `printf "%s/tls.crt" $cli`, form: "env",
		}},
		server: []certDecl{{
			file: "templates/deployment.yaml", line: 4,
			name: "KACHO_СОСЕД_PUBLIC_SERVER_MTLS_CERTFILE",
			path: `printf "%s/tls.crt" $srv`, form: "env",
		}},
	}
}

func TestClientIdentityJudgement_CanFailAndStaysSilent(t *testing.T) {
	cases := []struct {
		name   string
		scopes []certScope
		want   []string // подстроки, которые находка обязана назвать
		silent bool
		why    string
	}{
		{
			name:   "законный близнец: клиентский лист, секрет клиента объявлен",
			scopes: []certScope{legalCertScope()},
			silent: true,
			why:    "верная посадка обязана молчать, иначе первый ложный срабат снимет проверку",
		},
		{
			name: "ЛИЧНОСТЬ ПРЕДЪЯВЛЯЕТ СЕРВЕРНЫЙ ЛИСТ — находка с координатой",
			scopes: []certScope{func() certScope {
				sc := legalCertScope()
				sc.client[0].path = `printf "%s/tls.crt" $srv` // единственное отличие
				return sc
			}()},
			want: []string{
				kindClientPresentsServerLeaf,
				"KACHO_СОСЕД_GEO_MTLS_CERTFILE",
				"templates/deployment.yaml:10",
				"KACHO_СОСЕД_PUBLIC_SERVER_MTLS_CERTFILE",
			},
			why: "серверный лист выписан без `client auth` — слушатель отвергнет его на рукопожатии " +
				"предупреждением `bad certificate`, и фронт вернёт это арендатору как 503",
		},
		{
			name: "СЕКРЕТ КЛИЕНТА НЕ ОБЪЯВЛЕН — блок сертификата не срабатывает никогда",
			scopes: []certScope{func() certScope {
				sc := legalCertScope()
				sc.clientSecret = "" // единственное отличие
				return sc
			}()},
			want: []string{kindClientSecretNotDeclared, "KACHO_СОСЕД_GEO_MTLS_CERTFILE"},
			why:  "путь назван верно, а файла в поде не будет — «починка» без секрета зеленела бы вхолостую",
		},
		{
			name: "ЧАРТ БЕЗ РАБОЧЕЙ НАГРУЗКИ не спрашивается о секрете",
			scopes: []certScope{func() certScope {
				sc := legalCertScope()
				sc.workload = false // единственное отличие
				sc.clientSecret = ""
				return sc
			}()},
			silent: true,
			why: "секрет объявляет тот, чья нагрузка его монтирует; профиль умбреллы, " +
				"называющий путь чужой личности, своей нагрузки не несёт",
		},
		{
			name: "ЛИЧНОСТЬ БЕЗ ЛИСТА СЛУШАТЕЛЯ в том же чарте — не находка",
			scopes: []certScope{func() certScope {
				sc := legalCertScope()
				sc.server = nil // единственное отличие
				return sc
			}()},
			silent: true,
			why:    "сравнивать не с чем: край объявляет свой серверный TLS другим ключом",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			findings := judgeClientIdentityLeaves(tc.scopes)
			if tc.silent {
				if len(findings) != 0 {
					t.Fatalf("законный близнец обязан молчать (%s), а сказано: %v", tc.why, findings)
				}
				return
			}
			if len(findings) == 0 {
				t.Fatalf("проверка обязана найти (%s), а она смолчала", tc.why)
			}
			var said strings.Builder
			for _, f := range findings {
				said.WriteString(f.chart + " " + f.kind + " " + f.detail + "\n")
			}
			for _, w := range tc.want {
				if !strings.Contains(said.String(), w) {
					t.Errorf("находка обязана назвать %q (%s); сказано:\n%s", w, tc.why, said.String())
				}
			}
		})
	}
}

// TestClientIdentityScan_KnowsBothFormsAndReadsDeclarationsNotProse —
// распознаватель знает ОБЕ законные формы записи и судит ОБЪЯВЛЕНИЕ, а не текст
// рядом с ним.
//
// Без первой половины запись формы Б осталась бы вне наблюдения молча; без
// второй проверка краснела бы на собственном объяснении: имена переменных стоят
// в комментариях тех же файлов.
func TestClientIdentityScan_KnowsBothFormsAndReadsDeclarationsNotProse(t *testing.T) {
	body := strings.Join([]string{
		`# ПРОЗА: KACHO_X_GEO_MTLS_CERTFILE указывает на $srv — это комментарий, а не объявление`,
		`spec:`,
		`  containers:`,
		`    - env:`,
		`        - name: KACHO_X_PUBLIC_SERVER_MTLS_CERTFILE`,
		`          value: {{ printf "%s/tls.crt" $srv | quote }}`,
		`        - name: KACHO_X_GEO_MTLS_CERTFILE`,
		`          value: {{ printf "%s/tls.crt" $cli | quote }}`,
		`data:`,
		`  config.yaml: |`,
		`    authn:`,
		`      tls:`,
		`        cert-file: {{ .certFile | quote }}`,
		`    mtls:`,
		`      server:`,
		`        certfile: {{ printf "%s/tls.crt" $srv | quote }}`,
		`      geo:`,
		`        certfile: {{ printf "%s/tls.crt" $cli | quote }}`,
		`  values:`,
		`    KACHO_X_QUOTA_MTLS_CERTFILE: /etc/x/tls/client/tls.crt`,
		`    KACHO_X_INTERNAL_SERVER_MTLS_CERTFILE: /etc/x/tls/server/tls.crt`,
	}, "\n")

	client, server, empties := scanCertDecls("синтетика.yaml", body)

	names := func(ds []certDecl) []string {
		out := make([]string, 0, len(ds))
		for _, d := range ds {
			out = append(out, d.name)
		}
		return out
	}
	gotClient := strings.Join(names(client), " ")
	gotServer := strings.Join(names(server), " ")

	// Личности исходящих вызовов: две формы A (шаблонная пара и карта значений)
	// плюс форма Б (ребро под блоком `mtls`).
	wantClient := "KACHO_X_GEO_MTLS_CERTFILE data.mtls.geo.certfile KACHO_X_QUOTA_MTLS_CERTFILE"
	if gotClient != wantClient {
		t.Errorf("личности: %q, ожидалось %q — распознаватель либо не знает одной из форм, "+
			"либо принял прозу за объявление", gotClient, wantClient)
	}
	// Листы слушателей: форма A ×2 плюс форма Б под ключом `server`.
	wantServer := "KACHO_X_PUBLIC_SERVER_MTLS_CERTFILE data.mtls.server.certfile KACHO_X_INTERNAL_SERVER_MTLS_CERTFILE"
	if gotServer != wantServer {
		t.Errorf("листы слушателей: %q, ожидалось %q", gotServer, wantServer)
	}
	// `authn.tls.cert-file` — серверный TLS слушателя ВНЕ блока `mtls`: не предмет
	// ни с одной стороны. Попади он в личности — гейт краснел бы на исправном nlb.
	if strings.Contains(gotClient, "authn") || strings.Contains(gotServer, "authn") {
		t.Errorf("серверный TLS слушателя вне блока `mtls` принят за предмет: личности %q, листы %q",
			gotClient, gotServer)
	}
	t.Logf("осмотрено: строк %d · личностей %d · листов %d · пустых путей %d",
		strings.Count(body, "\n")+1, len(client), len(server), empties)
}
