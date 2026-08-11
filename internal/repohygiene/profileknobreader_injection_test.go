// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Проба гейта «объявленный ключ обязан иметь читателя» — инъекцией в ОБЕ стороны.
//
// Одной стороны мало. Гейт, проверенный только на дефекте, ловит ФОРМУ: он бы
// краснел и на законном ключе, который шаблон читает через область видимости
// (`with .Values.mtls`), через `index .Values "сабчарт" "ключ"` или через
// `condition:` зависимости — и первый же ложный срабат его бы выключил. Поэтому
// рядом с каждым дефектом стоит законный близнец ТОЙ ЖЕ формы.

// synthKnobTree собирает минимальное дерево «родитель + сабчарт», куда вызывающий
// доливает свои файлы. Дерево отслеживается git: состав корпуса берётся из
// индекса, а не обходом диска.
func synthKnobTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	base := map[string]string{
		"deploy/umbrella/Chart.yaml": `apiVersion: v2
name: umb
version: 1.0.0
dependencies:
  - name: sub
    repository: file://../../svc/deploy
    condition: sub.enabled
  - name: postgresql
    alias: pg
    version: 13.x
    repository: https://charts.example/bitnami
`,
		"deploy/umbrella/templates/cm.yaml": "kind: ConfigMap\ndata:\n  own: {{ .Values.parentOwn.value }}\n",
		"svc/deploy/Chart.yaml":             "apiVersion: v2\nname: sub\nversion: 1.0.0\n",
		"svc/deploy/templates/cm.yaml":      "kind: ConfigMap\ndata:\n  read: {{ .Values.config.read }}\n",
		"svc/deploy/values.yaml":            "config:\n  read: yes\n",
	}
	for rel, body := range base {
		if _, taken := files[rel]; !taken {
			files[rel] = body
		}
	}
	for rel, body := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	synthTrack(t, root)
	return root
}

func knobAudit(t *testing.T, root string) ([]knobFinding, knobCensus) {
	t.Helper()
	findings, census, err := auditProfileKnobReaders(root)
	if err != nil {
		t.Fatalf("перепись синтетического дерева: %v", err)
	}
	if census.KeysEnforced == 0 {
		t.Fatalf("синтетическое дерево не осмотрено: %s", census)
	}
	return findings, census
}

func knobJoin(findings []knobFinding) string {
	parts := make([]string, 0, len(findings))
	for _, f := range findings {
		parts = append(parts, f.String())
	}
	return strings.Join(parts, "\n")
}

// (а) Сторона дефекта: профиль объявляет ключ, которого не читает никто, — гейт
// краснеет И НАЗЫВАЕТ КООРДИНАТУ (файл + путь ключа). Без координаты находка не
// действие.
//
// Форма инъекции взята с натуры: ровно так `breakglass: false` стоял у
// `kacho-nlb` в трёх профилях, не имея читателя ни в шаблоне, ни в процессе.
func TestKnobReaderGateRedOnDeclaredButUnread(t *testing.T) {
	root := synthKnobTree(t, map[string]string{
		"deploy/umbrella/values.prod.yaml": "sub:\n  config:\n    read: yes\n    breakglass: false\n",
	})
	findings, census := knobAudit(t, root)
	t.Log(census.String())

	if len(findings) == 0 {
		t.Fatalf("ключ без читателя объявлен, а гейт молчит — он не способен упасть.\n%s", census)
	}
	joined := knobJoin(findings)
	if !strings.Contains(joined, "deploy/umbrella/values.prod.yaml") {
		t.Fatalf("находка не называет профиль — чинить нечего:\n%s", joined)
	}
	if !strings.Contains(joined, "sub.config.breakglass") {
		t.Fatalf("находка не называет путь ключа:\n%s", joined)
	}
	if !strings.Contains(joined, `"sub"`) {
		t.Fatalf("находка не называет чарт-получатель — неясно, чей шаблон обязан был читать:\n%s", joined)
	}
	// Соседний ключ того же блока читается — он находкой быть не должен, иначе
	// гейт красит блок целиком и его вердикт бесполезен.
	if strings.Contains(joined, "sub.config.read") {
		t.Fatalf("прочитанный сосед объявлен находкой — гейт красит блок, а не ключ:\n%s", joined)
	}
}

// (б) Законная сторона: тот же профиль, но шаблон ключ читает — гейт молчит.
func TestKnobReaderGateSilentWhenTemplateReadsTheKey(t *testing.T) {
	root := synthKnobTree(t, map[string]string{
		"deploy/umbrella/values.prod.yaml": "sub:\n  config:\n    read: yes\n    breakglass: false\n",
		"svc/deploy/templates/cm.yaml": "kind: ConfigMap\ndata:\n" +
			"  read: {{ .Values.config.read }}\n  bg: {{ .Values.config.breakglass }}\n",
	})
	findings, census := knobAudit(t, root)
	t.Log(census.String())
	if len(findings) != 0 {
		t.Fatalf("гейт нашёл дефект там, где читатель есть — он ловит форму, а не предмет:\n%s", knobJoin(findings))
	}
}

// (б) Законная форма №2: читатель входит в ОБЛАСТЬ ВИДИМОСТИ поддерева
// (`with .Values.mtls`), а листья адресуются относительными именами.
//
// Без этой половины гейт краснел бы на каждом блоке, отрендеренном через
// `with`/`toYaml`, — то есть на самой распространённой законной записи чарта.
func TestKnobReaderGateSilentOnScopeEnteringReference(t *testing.T) {
	root := synthKnobTree(t, map[string]string{
		"deploy/umbrella/values.prod.yaml": "sub:\n  mtls:\n    certfile: /a\n    keyfile: /b\n",
		"svc/deploy/templates/cm.yaml": "kind: ConfigMap\ndata:\n" +
			"  read: {{ .Values.config.read }}\n" +
			"{{- with .Values.mtls }}\n  c: {{ .certfile }}\n{{- end }}\n",
	})
	findings, census := knobAudit(t, root)
	t.Log(census.String())
	if len(findings) != 0 {
		t.Fatalf("ссылка на префикс не засчитана читателем — гейт покраснеет на каждом `with`:\n%s", knobJoin(findings))
	}
}

// (б) Законная форма №3: родитель добирается до значения сабчарта через
// `index .Values "имя" "ключ"` — единственная запись, доступная ему, когда имя
// сабчарта содержит дефис. Точечная форма такое имя не разбирает вовсе.
func TestKnobReaderGateSilentOnIndexedParentReference(t *testing.T) {
	root := synthKnobTree(t, map[string]string{
		"deploy/umbrella/values.prod.yaml":  "sub:\n  federationIn:\n    enabled: true\n",
		"deploy/umbrella/templates/cm.yaml": "kind: ConfigMap\ndata:\n  own: {{ .Values.parentOwn.value }}\n  f: {{ (index .Values \"sub\" \"federationIn\").enabled }}\n",
	})
	findings, census := knobAudit(t, root)
	t.Log(census.String())
	if len(findings) != 0 {
		t.Fatalf("чтение родителем через index не засчитано — гейт объявит находкой законную плюмбовку:\n%s", knobJoin(findings))
	}
}

// (б) Законная форма №4: ключ включения сабчарта читает сам helm по
// `condition:` — шаблону он не виден by construction. Без этой половины гейт
// объявлял бы находкой каждое `<сабчарт>.enabled`.
func TestKnobReaderGateSilentOnDependencyCondition(t *testing.T) {
	root := synthKnobTree(t, map[string]string{
		"deploy/umbrella/values.prod.yaml": "sub:\n  enabled: false\n",
	})
	findings, census := knobAudit(t, root)
	t.Log(census.String())
	if len(findings) != 0 {
		t.Fatalf("ключ `condition:` объявлен находкой — гейт не знает, что его читает helm:\n%s", knobJoin(findings))
	}
}

// Не осмотрено ≠ чисто: ключ ЧУЖОГО сабчарта не даёт находки, но обязан быть
// СОСЧИТАН. Иначе «ноль находок» покрывало бы и «шаблонов этого чарта в дереве
// нет, искать читателя было негде».
func TestKnobReaderGateCountsForeignSubchartInsteadOfJudgingIt(t *testing.T) {
	root := synthKnobTree(t, map[string]string{
		"deploy/umbrella/values.prod.yaml": "pg:\n  primary:\n    persistence:\n      size: 8Gi\n",
	})
	findings, census := knobAudit(t, root)
	t.Log(census.String())
	if len(findings) != 0 {
		t.Fatalf("гейт судит чужой чарт, чьих шаблонов в дереве нет:\n%s", knobJoin(findings))
	}
	if census.KeysForeignChart == 0 {
		t.Fatal("ключ чужого сабчарта не сосчитан — «ноль находок» стало неотличимо от «ноль прочитанного»")
	}
}

// Предпосылка проверяется САМА: `import-values` копирует поддерево родителя в
// сабчарт в обход имён и отменяет вывод «шаблон не ссылается ⇒ значение никуда
// не доедет». Найдя его, гейт обязан ОТКАЗАТЬ, а не промолчать.
//
// Без этого запрет пережил бы своё обоснование: дерево изменилось бы, а
// проверка продолжала бы отвечать «чисто» с прежней уверенностью.
func TestKnobReaderGateRefusesWhenItsPremiseIsRevoked(t *testing.T) {
	root := synthKnobTree(t, map[string]string{
		"deploy/umbrella/Chart.yaml": `apiVersion: v2
name: umb
version: 1.0.0
dependencies:
  - name: sub
    repository: file://../../svc/deploy
    import-values:
      - child: exports
        parent: .
`,
		"deploy/umbrella/values.prod.yaml": "sub:\n  config:\n    breakglass: false\n",
	})
	_, _, err := auditProfileKnobReaders(root)
	if err == nil {
		t.Fatal("предпосылка отменена (import-values), а гейт продолжает выносить вердикт — " +
			"его вывод «до процесса не доедет» больше ничем не обоснован")
	}
	if !strings.Contains(err.Error(), "import-values") {
		t.Fatalf("отказ не называет отменённую предпосылку: %v", err)
	}
}

// Собственные ключи чарта (его `values.yaml`) под тем же требованием: именно там
// живут умолчания, которые переживают снятие своего читателя.
func TestKnobReaderGateCoversChartOwnValues(t *testing.T) {
	root := synthKnobTree(t, map[string]string{
		"svc/deploy/values.yaml": "config:\n  read: yes\nautoscaling:\n  minReplicas: 2\n",
	})
	findings, census := knobAudit(t, root)
	t.Log(census.String())
	joined := knobJoin(findings)
	if !strings.Contains(joined, "svc/deploy/values.yaml") ||
		!strings.Contains(joined, "autoscaling.minReplicas") {
		t.Fatalf("умолчание чарта без читателя не поймано — гейт смотрит только на профили:\n%s", joined)
	}
}
