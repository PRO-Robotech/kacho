// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy_test

// iam_module_manifest_delivery_test.go — манифест модуля обязан ДОЕЗЖАТЬ до
// работающей службы, и путь у него один: посадка монтирует каталог, процесс
// читает тот же каталог.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Манифест модуля объявлен источником истины о том, что у платформы существует.
// В дереве он есть — по одному на модуль закрытого набора, — а способа доставить
// его работающей службе не было НИ ОДНОГО: ни ручки каталога в конфигурации iam,
// ни монтирования чартом (#1875). Пока доставки нет, сверка каталога со
// снятым модулем невыразима: второго операнда у неё не существует (#1861).
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ИМЕННО ЗДЕСЬ ПРОВЕРЯЕТСЯ — СОГЛАСИЕ ДВУХ ПОЛОВИН, А НЕ НАЛИЧИЕ КАЖДОЙ
//
// Половин у доставки две, и по отдельности каждая защитима: под монтирует
// каталог, процесс читает каталог. Ломается их СТЫК — когда каталог пода и
// каталог процесса объявлены ДВУМЯ литералами. Тогда правка одного не двигает
// второй, процесс читает пустой путь, и это не отличается снаружи ни от «модулей
// нет», ни от «манифесты не доехали». Поэтому обе половины обязаны выводиться из
// ОДНОГО ключа значений — `manifests.mountPath`, — и проверяется именно это.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ОБЪЯВЛЕНИЯ, А НЕ РЕНДЕР
//
// Та же причина, что у соседей (`token_shape_test.go`, `service_config_sections_test.go`):
// проверке не нужны ни `helm`, ни скачанные зависимости чартов, поэтому она не
// умеет пропуститься. Рендер этих имён не меняет — они литералы шаблона.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ СУДИТСЯ ИСПОЛНЯЕМАЯ ЧАСТЬ, А НЕ ТЕКСТ
//
// Об этом предмете в тех же файлах написана ПРОЗА, и она называет те же ключи.
// Предикат по слову зачёл бы собственное объяснение проверяемого за исполнение —
// класс, на котором уже обжигались. Поэтому комментарии (`#` и `{{/* */}}`)
// снимаются ДО поиска, а инъекция это доказывает: ключ, оставленный только в
// комментарии, обязан читаться как отсутствующий.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// iamChartDir — вендоренная копия чарта службы прав внутри умбреллы. Ставят
// стеки именно её (см. соседний iam_pool_declaration_parity_test.go); второй
// чарт (services/iam/deploy) не ставит ни один стек и здесь не судится.
const iamChartDir = "helm/umbrella/charts/kacho-iam"

// manifestDeliveryDecls — объявления, из которых складывается доставка.
//
// Тексты, а не пути: разбор отделён от чтения, чтобы инъекция подавала ему
// изменённые объявления, не трогая дерево.
type manifestDeliveryDecls struct {
	values     string
	deployment string
	configmap  string
}

// manifestDeliveryCensus — объём осмотренного. Печатается ВСЕГДА: «ноль находок»
// обязано быть отличимо от «ноль прочитанного».
type manifestDeliveryCensus struct {
	FilesRead    int
	BytesRead    int
	BytesJudged  int // после снятия комментариев — то, что реально судилось
	KeysRequired int
}

// commentedYAMLLine — строка, целиком являющаяся комментарием YAML.
var commentedYAMLLine = regexp.MustCompile(`(?m)^[ \t]*#.*$`)

// templateCommentBlock — блок комментария Go-шаблона.
var templateCommentBlock = regexp.MustCompile(`(?s)\{\{-?/\*.*?\*/-?\}\}`)

// stripDeclarationComments снимает то, что шаблон НЕ исполняет.
//
// Иначе проверка зачла бы за объявление собственное объяснение: об этих ключах в
// тех же файлах написана проза, и она называет их дословно.
func stripDeclarationComments(s string) string {
	s = templateCommentBlock.ReplaceAllString(s, "")
	return commentedYAMLLine.ReplaceAllString(s, "")
}

// auditManifestDelivery — находки по объявлениям. Пусто = доставка объявлена.
//
// Каждая находка называет КООРДИНАТУ и предмет: читатель обязан узнать не только
// что не так, но и чем это чинить.
func auditManifestDelivery(d manifestDeliveryDecls) ([]string, manifestDeliveryCensus) {
	census := manifestDeliveryCensus{
		FilesRead: 3,
		BytesRead: len(d.values) + len(d.deployment) + len(d.configmap),
	}
	deployment := stripDeclarationComments(d.deployment)
	configmap := stripDeclarationComments(d.configmap)
	census.BytesJudged = len(deployment) + len(configmap) + len(d.values)

	var findings []string

	// (1) ЗНАЧЕНИЯ объявляют каталог доставки. Незаданный каталог означает «не
	// сужаем», и подставлять за оператора умолчание чарт не вправе.
	var tree map[string]any
	if err := yaml.Unmarshal([]byte(d.values), &tree); err != nil {
		findings = append(findings, iamChartDir+"/values.yaml: не разбирается: "+err.Error())
	}
	mountPath, _ := nestedString(tree, "manifests", "mountPath")
	cmName, cmDeclared := nestedString(tree, "manifests", "configMapName")
	if strings.TrimSpace(mountPath) == "" {
		findings = append(findings, iamChartDir+
			"/values.yaml: `manifests.mountPath` не объявлен — каталога доставки нет, "+
			"и манифест модуля не доезжает до процесса ни при какой посадке (kacho#1875)")
	}
	if !cmDeclared {
		findings = append(findings, iamChartDir+
			"/values.yaml: `manifests.configMapName` не объявлен — источник манифестов не назван; "+
			"пустое значение законно (доставка выключена), отсутствие ключа — нет")
	}
	_ = cmName

	// (2) ПОД монтирует именно этот каталог, и имя источника берёт из значений.
	//
	// Оба ключа ищутся в ИСПОЛНЯЕМОЙ части: ключ, оставшийся только в прозе,
	// читается как отсутствующий.
	census.KeysRequired = 4
	for _, req := range []struct{ key, why string }{
		{".Values.manifests.configMapName",
			"под не берёт источник манифестов из значений — доставка либо запечена, " +
				"либо не заведена вовсе"},
		{".Values.manifests.mountPath",
			"под не берёт каталог монтирования из значений — каталог пода и каталог " +
				"процесса становятся ДВУМЯ объявлениями одного предмета и разойдутся молча"},
	} {
		if !strings.Contains(deployment, req.key) {
			findings = append(findings, fmt.Sprintf("%s/templates/deployment.yaml: `%s` не читается: %s",
				iamChartDir, req.key, req.why))
		}
	}

	// (3) ПРОЦЕСС читает ТОТ ЖЕ каталог: `manifests.dir` конфигурации выводится
	// из того же ключа значений, а не пишется вторым литералом.
	if !strings.Contains(configmap, "manifests:") {
		findings = append(findings, iamChartDir+
			"/templates/configmap.yaml: секции `manifests:` нет — ручка каталога не доезжает "+
			"до процесса, и объявленная посадкой доставка остаётся невидимой службе")
	}
	if !strings.Contains(configmap, ".Values.manifests.mountPath") {
		findings = append(findings, iamChartDir+
			"/templates/configmap.yaml: `manifests.dir` не выводится из `.Values.manifests.mountPath` — "+
			"каталог процесса и каталог пода объявлены порознь")
	}

	sort.Strings(findings)
	return findings, census
}

// nestedString — значение по пути в разобранном дереве значений и признак того,
// что ключ ОБЪЯВЛЕН. Пустое объявленное значение и отсутствие ключа — разные
// утверждения, и различать их обязан вызывающий.
func nestedString(tree map[string]any, path ...string) (string, bool) {
	cur := any(tree)
	for _, seg := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return "", false
		}
		cur, ok = m[seg]
		if !ok {
			return "", false
		}
	}
	if cur == nil {
		return "", true
	}
	s, ok := cur.(string)
	if !ok {
		return fmt.Sprint(cur), true
	}
	return s, true
}

// readManifestDeliveryDecls читает три объявления из дерева.
func readManifestDeliveryDecls(t *testing.T) manifestDeliveryDecls {
	t.Helper()
	read := func(rel string) string {
		p := filepath.Join(repoRoot, "deploy", iamChartDir, rel)
		raw, err := os.ReadFile(p) //nolint:gosec // путь собран из констант этого файла
		if err != nil {
			t.Fatalf("%s: объявление не прочитано: %v — непрочитанное есть НАХОДКА, "+
				"а не «проверять нечего»", p, err)
		}
		if len(raw) == 0 {
			t.Fatalf("%s: объявление пусто — судить нечего", p)
		}
		return string(raw)
	}
	return manifestDeliveryDecls{
		values:     read("values.yaml"),
		deployment: read("templates/deployment.yaml"),
		configmap:  read("templates/configmap.yaml"),
	}
}

// TestIAMChartDeliversModuleManifestsToTheProcess — доставка объявлена, и обе её
// половины выведены из одного ключа.
func TestIAMChartDeliversModuleManifestsToTheProcess(t *testing.T) {
	findings, census := auditManifestDelivery(readManifestDeliveryDecls(t))
	t.Logf("осмотрено: объявлений %d · байт прочитано %d · байт после снятия комментариев %d · "+
		"ключей требуется %d · находок %d",
		census.FilesRead, census.BytesRead, census.BytesJudged, census.KeysRequired, len(findings))
	if census.BytesJudged == 0 {
		t.Fatal("судить нечего: после снятия комментариев не осталось ни байта — " +
			"вердикт беспредметен")
	}
	for _, f := range findings {
		t.Errorf("доставка манифеста модуля: %s", f)
	}
}
