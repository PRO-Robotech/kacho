// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// clienttruth_deploy_readiness_probed_injection_test.go — доказательство того, что
// соседний гейт способен упасть, способен смолчать и читает то, что kubelet
// ИСПОЛНИТ, а не то, что рядом написано.
//
// Две оси, и обе не умозрительные. Первая — комментарий: над каждой починенной
// пробой в этом дереве теперь стоит абзац, объясняющий, почему готовность
// спрашивается по `/readyz`, и он содержит это самое слово. Гейт, читающий текст,
// зеленел бы на объяснении при `tcpSocket` строкой ниже — то есть ровно на той
// форме, ради которой заведён. Вторая — соседний блок: `livenessProbe` часто
// стоит следом и у нескольких чартов ссылается на те же пути; гейт, берущий
// первое попавшееся `path:` после начала блока готовности, принял бы живость за
// готовность.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTemplate кладёт кусок шаблона во временный файл и возвращает путь.
func writeTemplate(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "deployment.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return p
}

func TestReadinessBlockReaderJudgesTheProbeNotTheProseAroundIt(t *testing.T) {
	cases := []struct {
		name string
		body string
		want []string
	}{
		{
			name: "провязано по /readyz — путь виден",
			body: `        spec:
          readinessProbe:
            httpGet:
              path: /readyz
              port: metrics
            periodSeconds: 10
`,
			want: []string{"/readyz"},
		},
		{
			name: "tcpSocket под абзацем про /readyz — путь ПУСТ (комментарий не проба)",
			body: `        spec:
          # ГОТОВНОСТЬ СПРАШИВАЕТСЯ ПО ТОМУ, ЧТО СЕРВИС СТРОИТ: /readyz собирает
          # чекеры базы и канала к iam. Ниже — то, что исполняется на самом деле.
          #   path: /readyz
          readinessProbe:
            tcpSocket:
              port: grpc
            periodSeconds: 10
`,
			want: []string{""},
		},
		{
			name: "/readyz стоит ТОЛЬКО в живости — готовность пуста (блоки не смешиваются)",
			body: `        spec:
          readinessProbe:
            tcpSocket:
              port: grpc
            periodSeconds: 10
          livenessProbe:
            httpGet:
              path: /readyz
              port: metrics
            periodSeconds: 30
`,
			want: []string{""},
		},
		{
			name: "готовность спрашивает безусловный /healthz — виден он, а не /readyz",
			body: `        spec:
          readinessProbe:
            httpGet:
              path: /healthz
              port: metrics
`,
			want: []string{"/healthz"},
		},
		{
			name: "блока готовности нет вовсе — пустой список",
			body: `        spec:
          livenessProbe:
            tcpSocket:
              port: grpc
`,
			want: nil,
		},
		{
			name: "два контейнера — обе пробы видны по отдельности",
			body: `        spec:
          readinessProbe:
            httpGet:
              path: /readyz
              port: metrics
          x: y
          readinessProbe:
            tcpSocket:
              port: grpc
`,
			want: []string{"/readyz", ""},
		},
		{
			name: "путь в кавычках — кавычки снимаются",
			body: `        spec:
          readinessProbe:
            httpGet:
              path: "/readyz"
              port: metrics
`,
			want: []string{"/readyz"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := readinessProbesPath(t, writeTemplate(t, tc.body))
			if len(got) != len(tc.want) {
				t.Fatalf("блоков готовности найдено %d (%q), ожидалось %d (%q)",
					len(got), strings.Join(got, "|"), len(tc.want), strings.Join(tc.want, "|"))
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("блок %d: путь %q, ожидался %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestReadinessDetectorSeesTheRouteWhereverItIsRegistered — распознаватель
// «сервис обслуживает /readyz».
//
// Формы регистрации в этом дереве ДВЕ, и обе законны: в корне команды рядом с
// диагностической поверхностью и в `internal/handler` рядом с приёмом вебхуков.
// Первая редакция гейта знала одну и не видела iam вовсе — сервис, строящий
// готовность и не пробируемый по ней, оказывался не нарушителем, а невидимкой.
func TestReadinessDetectorSeesTheRouteWhereverItIsRegistered(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, "internal", "handler", "hooks")
	if err := os.MkdirAll(deep, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeGoFile(t, deep, "http_server.go",
		"package hooks\n\nfunc NewMux(m *http.ServeMux) {\n\tm.HandleFunc(\"/readyz\", readinessHandler(nil))\n}")

	found, filesRead := servesReadyz(t, root)
	if !found {
		t.Error("маршрут, зарегистрированный не в корне команды, не найден — та самая слепота")
	}
	if filesRead != 1 {
		t.Errorf("прочитано %d файлов, ожидался 1", filesRead)
	}

	t.Run("проза и /healthz — не находка", func(t *testing.T) {
		other := t.TempDir()
		writeGoFile(t, other, "serve.go",
			"package main\n\n// Здесь мог бы стоять /readyz, но поверхность отдаёт только живость.\nfunc s(m *http.ServeMux) {\n\tm.HandleFunc(\"GET /healthz\", live)\n}")
		found, read := servesReadyz(t, other)
		if found {
			t.Error("комментарий про /readyz принят за обслуживаемый маршрут")
		}
		if read != 1 {
			t.Errorf("прочитано %d файлов, ожидался 1", read)
		}
	})

	t.Run("маршрут только в пробе — не в счёт", func(t *testing.T) {
		other := t.TempDir()
		writeGoFile(t, other, "serve_test.go",
			"package main\n\nfunc TestX() {\n\t_ = \"/readyz\"\n}")
		found, read := servesReadyz(t, other)
		if found || read != 0 {
			t.Errorf("проба учтена как реализация: found=%v read=%d", found, read)
		}
	})
}
