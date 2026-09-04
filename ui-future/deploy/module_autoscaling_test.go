// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// module_autoscaling_test.go — модуль, отдавший число реплик автоскейлеру,
// обязан иметь автоскейлер.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Deployment каждого модуля консоли объявляет `replicas` УСЛОВНО: поле
// опускается, когда у модуля включено автомасштабирование, — иначе helm
// возвращал бы число реплик к объявленному на каждом обновлении и воевал бы с
// автоскейлером. Условие осмысленно ровно до тех пор, пока автоскейлер
// существует. Если его шаблон модуля не рендерит, включение ручки даёт
// противоположное задуманному: поля `replicas` в манифесте нет, HPA нет тоже —
// kubernetes применяет своё умолчание, ОДНУ реплику, и модуль не масштабируется
// никогда. Оператор при этом читает `autoscaling.enabled: true` как включённое
// масштабирование и распоряжается тем, чего нет.
//
// Обратная сторона — тот же предмет с другого конца: HPA у модуля, чей
// deployment `replicas` НЕ опускает, тоже неверен (helm возвращает число на
// каждом обновлении). Поэтому проверка требует РАВЕНСТВА двух множеств, а не
// покрытия в одну сторону.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ПРОБА ЧИТАЕТ
//
// Только ОБЪЯВЛЕНИЯ шаблонов — ни кластера, ни helm, ни сети, поэтому она не
// умеет пропуститься. Разбор — по исполняемой форме `{{ … }}`, а не по
// упоминанию имени в тексте: строка, где ключ назван прозой, читателем не
// является (комментарий к шаблону рассказывает о ручке, а не читает её).
package deploy_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// replicaSurrender — deployment опускает `replicas`, когда у модуля включено
// автомасштабирование: `{{- if not .Values.<модуль>.autoscaling.enabled }}`.
// Модуль-хост объявляет ручку на верхнем уровне (`.Values.autoscaling.*`) —
// его имя в этой записи пусто, и оно же служит ключом множества.
var replicaSurrender = regexp.MustCompile(`\{\{-?\s*if\s+not\s+\.Values\.((?:[A-Za-z_][\w]*\.)?)autoscaling\.enabled\s*-?\}\}`)

// hpaScaled — HPA читает границы масштабирования модуля. Признак — чтение
// `minReplicas`, а не наличие блока: блок без границ ничего не масштабирует.
var hpaScaled = regexp.MustCompile(`\{\{-?\s*\.Values\.((?:[A-Za-z_][\w]*\.)?)autoscaling\.minReplicas\s*-?\}\}`)

// moduleNames — имена модулей из совпадений; пустая группа (модуль-хост)
// называется явно, чтобы находка читалась.
func moduleNames(re *regexp.Regexp, body string) map[string]bool {
	out := map[string]bool{}
	for _, m := range re.FindAllStringSubmatch(body, -1) {
		name := strings.TrimSuffix(m[1], ".")
		if name == "" {
			name = "<host>"
		}
		out[name] = true
	}
	return out
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// uifTemplates — шаблоны чарта консоли по составу ИНДЕКСА, а не обходом диска:
// вердикт обязан быть свойством коммита, а под `ui-future/` на машине сборки
// лежит и то, чего в репозитории нет.
func uifTemplates(t *testing.T) (surrender, scaled map[string]bool, filesRead int) {
	t.Helper()
	root := repoRootFromTest(t)
	dir := filepath.Join(root, "ui-future", "deploy", "templates")
	files, err := treecorpus.Under(dir)
	if err != nil {
		t.Fatalf("состав %s: %v — без индекса «ноль находок» неотличимо от «ноль прочитанного»", dir, err)
	}
	surrender, scaled = map[string]bool{}, map[string]bool{}
	for _, abs := range files {
		if !strings.HasSuffix(abs, ".yaml") {
			continue
		}
		body, rerr := os.ReadFile(abs) // #nosec G304 -- путь пришёл из индекса git этого дерева
		if rerr != nil {
			t.Fatalf("%s: %v", abs, rerr)
		}
		filesRead++
		text := string(body)
		for name := range moduleNames(replicaSurrender, text) {
			surrender[name] = true
		}
		for name := range moduleNames(hpaScaled, text) {
			scaled[name] = true
		}
	}
	return surrender, scaled, filesRead
}

// repoRootFromTest — подъём до каталога с go.mod.
func repoRootFromTest(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("не найден корень репозитория (каталог с go.mod)")
		}
		dir = parent
	}
}

// TestEveryModuleThatSurrendersReplicasHasAnAutoscaler — два множества совпадают.
func TestEveryModuleThatSurrendersReplicasHasAnAutoscaler(t *testing.T) {
	surrender, scaled, filesRead := uifTemplates(t)

	t.Logf("осмотрено: шаблонов чарта консоли %d; отдают число реплик автоскейлеру %d %v; "+
		"имеют автоскейлер %d %v",
		filesRead, len(surrender), sortedKeys(surrender), len(scaled), sortedKeys(scaled))

	if filesRead == 0 {
		t.Fatal("прочитано ноль шаблонов — предмета не было, и молчание проверки " +
			"не является утверждением о чарте")
	}
	if len(surrender) == 0 {
		t.Fatal("ни один deployment не отдаёт число реплик автоскейлеру — предпосылка " +
			"проверки исчезла: сравнивать не с чем, а «всё сошлось» здесь означало бы " +
			"«нечему было сходиться»")
	}

	for _, name := range sortedKeys(surrender) {
		if !scaled[name] {
			t.Errorf("модуль %q опускает `replicas`, когда включено автомасштабирование, "+
				"но HorizontalPodAutoscaler для него не рендерится. Включённая ручка тогда "+
				"даёт ОДНУ реплику навсегда: поля `replicas` в манифесте нет, автоскейлера "+
				"нет тоже — применяется умолчание kubernetes. Границы `minReplicas`/"+
				"`maxReplicas`/`targetCPUUtilizationPercentage` при этом объявлены в профиле "+
				"и не читаются ничем.", name)
		}
	}
	for _, name := range sortedKeys(scaled) {
		if !surrender[name] {
			t.Errorf("у модуля %q есть HorizontalPodAutoscaler, но его deployment объявляет "+
				"`replicas` безусловно — helm возвращает число реплик к объявленному на "+
				"каждом обновлении и отменяет решение автоскейлера", name)
		}
	}
}

// TestAutoscalingDiscriminatorCutsBothWays — признак обязан ловить то, что
// должен, и МОЛЧАТЬ на законной конструкции той же формы.
//
// Законная форма здесь не выдумана: модуль вправе объявлять `replicas`
// безусловно и автоскейлера не иметь — тогда упоминание автомасштабирования в
// его шаблоне отсутствует, и требовать HPA не с чего. Отдельно проверяется, что
// признак читает исполняемую форму: имя ручки, названное в комментарии к
// шаблону, читателем не делает — иначе гейт зеленел бы от рассказа о ручке.
func TestAutoscalingDiscriminatorCutsBothWays(t *testing.T) {
	const deployments = `
{{/* Модуль ghost отдаёт число реплик автоскейлеру. */}}
spec:
  {{- if not .Values.ghost.autoscaling.enabled }}
  replicas: {{ .Values.ghost.replicas }}
  {{- end }}
---
{{/* Модуль pinned держит число реплик сам. */}}
spec:
  replicas: {{ .Values.pinned.replicas }}
---
{{/* Модуль mentioned лишь УПОМЯНУТ: .Values.mentioned.autoscaling.enabled —
     это текст комментария, а не исполняемая часть. */}}
spec:
  replicas: {{ .Values.mentioned.replicas }}
`
	const hpa = `
{{- if .Values.orphan.autoscaling.enabled }}
  minReplicas: {{ .Values.orphan.autoscaling.minReplicas }}
{{- end }}
{{- if .Values.shape.autoscaling.enabled }}
{{/* Блок без границ: рендерится, но ничего не масштабирует. */}}
{{- end }}
`
	surrender := moduleNames(replicaSurrender, deployments)
	scaled := moduleNames(hpaScaled, hpa)

	// (а) КРАСНОЕ НАПРАВЛЕНИЕ.
	if !surrender["ghost"] {
		t.Errorf("отдача числа реплик автоскейлеру не опознана: %v", sortedKeys(surrender))
	}
	if !scaled["orphan"] {
		t.Errorf("чтение границ масштабирования не опознано: %v", sortedKeys(scaled))
	}

	// (б) МОЛЧАЛИВОЕ НАПРАВЛЕНИЕ — законные конструкции той же формы.
	for _, name := range []string{"pinned", "mentioned"} {
		if surrender[name] {
			t.Errorf("модуль %q принят за отдавшего число реплик: признак ловит УПОМИНАНИЕ "+
				"ручки, а не её чтение — тогда гейт зеленеет от комментария, объясняющего "+
				"ручку, при снятом шаблоне", name)
		}
	}
	if scaled["shape"] {
		t.Errorf("блок без границ принят за автоскейлер — признак требует формы, а не " +
			"содержания: такой HPA рендерится и не масштабирует")
	}
	if len(surrender) != 1 || len(scaled) != 1 {
		t.Errorf("лишние совпадения: отдают %v, масштабируются %v",
			sortedKeys(surrender), sortedKeys(scaled))
	}
}
