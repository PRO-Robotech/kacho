// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// legitimateService — сервис, у которого всё на месте: поверхность поднята,
// живость и готовность разведены, зависимость названа. Он стоит в КАЖДОМ
// синтетическом дереве вторым сервисом и обязан молчать всегда.
//
// Без него доказательство было бы односторонним: гейт, краснеющий на всём,
// проходит любую инъекцию «находка» и отключается первым же ложным
// срабатыванием.
const legitimateService = `package main

import (
	"context"
	"net/http"

	"github.com/PRO-Robotech/kacho/pkg/observability/health"
)

func checkers(ping func(context.Context) error) []health.Checker {
	return []health.Checker{{Name: "database", Check: ping}}
}

func mux(agg *health.Aggregator, m http.Handler) *http.ServeMux {
	s := http.NewServeMux()
	s.Handle("GET /metrics", m)
	s.Handle("GET /healthz", agg.LiveHandler())
	s.Handle("GET /readyz", agg.ReadyHandler())
	return s
}
`

// Способность гейта «каждый сервис СТРОИТ готовность» упасть и смолчать —
// доказана инъекцией, а не прочтением.
//
// У каждой оси стоит ЗАКОННЫЙ БЛИЗНЕЦ: конструкция той же формы, на которой гейт
// обязан молчать. Инъекция снимает НОВОЕ свойство у элемента, чьи прочие свойства
// на месте, — иначе красное приходило бы от соседней оси, и вакуумность
// проверяемой осталась бы незамеченной (`testing.md` §«Гейт на класс», п.2в).
func TestReadinessBuiltInjectionCutsBothWays(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		find  bool
		says  []string
	}{
		{
			name: "НАХОДКА: поверхность поднята, готовности нет вовсе",
			files: map[string]string{
				"services/broken/cmd/broken/diag.go": `package main

import "net/http"

func mux(m http.Handler) *http.ServeMux {
	s := http.NewServeMux()
	s.Handle("GET /metrics", m)
	s.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
	return s
}
`,
			},
			find: true,
			says: []string{"broken", "готовности у сервиса НЕ СУЩЕСТВУЕТ"},
		},
		{
			name: "НАХОДКА: готовность и живость — одно выражение обработчика",
			files: map[string]string{
				"services/broken/cmd/broken/diag.go": `package main

import (
	"context"
	"net/http"

	"github.com/PRO-Robotech/kacho/pkg/observability/health"
)

func checkers(ping func(context.Context) error) []health.Checker {
	return []health.Checker{{Name: "database", Check: ping}}
}

func mux(agg *health.Aggregator, m http.Handler) *http.ServeMux {
	s := http.NewServeMux()
	s.Handle("GET /metrics", m)
	s.Handle("GET /healthz", agg.LiveHandler())
	s.Handle("GET /readyz", agg.LiveHandler())
	return s
}
`,
			},
			find: true,
			says: []string{"broken", "ОДНИМ И ТЕМ ЖЕ выражением"},
		},
		{
			name: "НАХОДКА: готовность объявлена, ни одной именованной зависимости",
			files: map[string]string{
				"services/broken/cmd/broken/diag.go": `package main

import "net/http"

func mux(m http.Handler) *http.ServeMux {
	s := http.NewServeMux()
	s.Handle("GET /metrics", m)
	s.HandleFunc("GET /healthz", live)
	s.HandleFunc("GET /readyz", stubReady)
	return s
}

func live(w http.ResponseWriter, _ *http.Request)      { w.WriteHeader(200) }
func stubReady(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }
`,
			},
			find: true,
			says: []string{"broken", "ни одной ИМЕНОВАННОЙ проверки"},
		},
		{
			name: "МОЛЧИТ: маршрут БЕЗ приставки метода — та же регистрация другой формой",
			files: map[string]string{
				"services/quiet/internal/handler/mux.go": `package handler

import "net/http"

func NewMux(live, ready http.HandlerFunc, m http.Handler) *http.ServeMux {
	s := http.NewServeMux()
	s.Handle("/metrics", m)
	s.HandleFunc("/healthz", live)
	s.HandleFunc("/readyz", ready)
	return s
}
`,
				"services/quiet/cmd/quiet/wire.go": `package main

import "context"

type checker struct {
	Name  string
	Check func(context.Context) error
}

func deps(ping func(context.Context) error) []checker {
	return []checker{{Name: "database", Check: ping}}
}
`,
			},
			find: false,
		},
		{
			name: "МОЛЧИТ: /readyz только в комментарии, а регистрация на месте",
			files: map[string]string{
				"services/quiet/cmd/quiet/diag.go": "// Готовность отдаётся на /readyz, живость на /healthz — разные вопросы.\n" + legitimateService,
			},
			find: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			files := map[string]string{"services/legit/cmd/legit/diag.go": legitimateService}
			for rel, src := range tc.files {
				files[rel] = src
			}
			var rels []string
			for rel, src := range files {
				abs := filepath.Join(root, rel)
				if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
					t.Fatalf("создание каталога %s: %v", rel, err)
				}
				if err := os.WriteFile(abs, []byte(src), 0o600); err != nil {
					t.Fatalf("запись %s: %v", rel, err)
				}
				rels = append(rels, rel)
			}
			sort.Strings(rels)

			findings, cen, err := auditReadinessBuilt(root, rels)
			if err != nil {
				t.Fatalf("обход: %v", err)
			}
			if cen.FilesRead != len(rels) {
				t.Fatalf("разобрано %d файлов из %d — обход неполон, вердикт беспредметен",
					cen.FilesRead, len(rels))
			}

			var got []string
			for _, f := range findings {
				got = append(got, f.Service+": "+f.Why)
			}
			joined := strings.Join(got, "\n")

			// Законный близнец обязан молчать в КАЖДОМ прогоне.
			if strings.Contains(joined, "legit:") {
				t.Fatalf("законный близнец объявлен нарушителем — гейт ловит форму, а не существо:\n%s", joined)
			}
			if tc.find && len(findings) == 0 {
				t.Fatalf("гейт смолчал на возвращённом дефекте — падать он не способен\nперепись: %+v", cen)
			}
			if !tc.find && len(findings) != 0 {
				t.Fatalf("гейт покраснел на законной конструкции:\n%s", joined)
			}
			for _, want := range tc.says {
				if !strings.Contains(joined, want) {
					t.Errorf("находка не называет %q — читатель пойдёт искать не там:\n%s", want, joined)
				}
			}
		})
	}
}

// Способность гейта «слот живости не спрашивает готовность» упасть и смолчать.
func TestLivenessSlotInjectionCutsBothWays(t *testing.T) {
	const legitTemplate = `spec:
  template:
    spec:
      containers:
        - name: legit
          readinessProbe:
            httpGet:
              path: /readyz
              port: metrics
          livenessProbe:
            httpGet:
              path: /healthz
              port: metrics
`
	cases := []struct {
		name string
		body string
		find bool
		says []string
	}{
		{
			name: "НАХОДКА: в слоте живости стоит вопрос готовности",
			body: `spec:
  template:
    spec:
      containers:
        - name: broken
          readinessProbe:
            httpGet:
              path: /readyz
              port: metrics
          livenessProbe:
            httpGet:
              path: /readyz
              port: metrics
`,
			find: true,
			says: []string{"слот ЖИВОСТИ спрашивает /readyz", "шторм перезапусков"},
		},
		{
			name: "МОЛЧИТ: живость открытым сокетом — законный ответ о процессе",
			body: `spec:
  template:
    spec:
      containers:
        - name: quiet
          readinessProbe:
            httpGet:
              path: /readyz
              port: metrics
          livenessProbe:
            tcpSocket:
              port: grpc
`,
			find: false,
		},
		{
			name: "МОЛЧИТ: /readyz назван КОММЕНТАРИЕМ над слотом живости",
			body: `spec:
  template:
    spec:
      containers:
        - name: quiet
          readinessProbe:
            httpGet:
              path: /readyz
              port: metrics
          # Живость намеренно НЕ спрашивает /readyz: блип зависимости не смерть
          # процесса.
          livenessProbe:
            httpGet:
              path: /healthz
              port: metrics
`,
			find: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			rels := []string{"charts/legit/templates/deployment.yaml", "charts/subject/templates/deployment.yaml"}
			for i, body := range []string{legitTemplate, tc.body} {
				abs := filepath.Join(root, rels[i])
				if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
					t.Fatalf("создание каталога: %v", err)
				}
				if err := os.WriteFile(abs, []byte(body), 0o600); err != nil {
					t.Fatalf("запись: %v", err)
				}
			}

			findings, cen, err := auditLivenessSlotStaysLiveness(root, rels)
			if err != nil {
				t.Fatalf("обход: %v", err)
			}
			if cen.Templates != len(rels) {
				t.Fatalf("прочитано %d шаблонов из %d — обход неполон", cen.Templates, len(rels))
			}

			var got []string
			for _, f := range findings {
				got = append(got, f.Template+": "+f.Why)
			}
			joined := strings.Join(got, "\n")
			if strings.Contains(joined, "charts/legit/") {
				t.Fatalf("законный близнец объявлен нарушителем:\n%s", joined)
			}
			if tc.find && len(findings) == 0 {
				t.Fatalf("гейт смолчал на возвращённом дефекте\nперепись: %+v", cen)
			}
			if !tc.find && len(findings) != 0 {
				t.Fatalf("гейт покраснел на законной конструкции:\n%s", joined)
			}
			for _, want := range tc.says {
				if !strings.Contains(joined, want) {
					t.Errorf("находка не называет %q:\n%s", want, joined)
				}
			}
		})
	}
}
