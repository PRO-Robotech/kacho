// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

//go:build helmcharts

// identity_file_keys_survive_the_environment_test.go — ключ, объявленный НАШИМ
// файлом настроек, обязан этим файлом и решаться: переменная окружения на том же
// процессе перебивает файл, и перебивает МОЛЧА.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Служба личности переопределяет ключи конфигурации ПЕРЕМЕННЫМИ ОКРУЖЕНИЯ по
// пути ключа: `courier.smtp.connection_uri` читается из
// `COURIER_SMTP_CONNECTION_URI`, `log.level` — из `LOG_LEVEL`, и так далее.
// Переменная сильнее файла. Значит о каждом таком ключе высказываются ДВА места
// — наш файл и окружение пода, — и выигрывает не наше.
//
// Наблюдаемости у расхождения нет НИКАКОЙ: чарт рендерится, страж старта
// доволен, карта настроек в кластере лежит и содержит ровно то, что мы написали.
// Не совпадает только исполняемое. Это «принято-и-проигнорировано»
// (`api-conventions.md`) на слое посадки: правка нашего файла не меняет ничего,
// а следующий читатель чинит поведение там, где оно не решается.
//
// ─────────────────────────────────────────────────────────────────────────────
// КЛАСС ШИРЕ ОДНОЙ ПЕРЕМЕННОЙ — И ЭТО ИЗМЕРЕНО, А НЕ ПРЕДПОЛОЖЕНО
//
// Задача #1733 назвала ОДНУ переменную (`COURIER_SMTP_CONNECTION_URI`). Перепись
// по рендеру на ревизии заведения нашла ПЯТЬ ключей на каждом из шести стеков:
//
//	courier.smtp.connection_uri  ← COURIER_SMTP_CONNECTION_URI  (dev · fe3455 · prorobotech · a8f60d · dev-prod)
//	secrets.cookie               ← SECRETS_COOKIE               (все шесть)
//	secrets.cipher               ← SECRETS_CIPHER               (все шесть)
//	log.level                    ← LOG_LEVEL                    (все шесть, почтовый процесс)
//	log.format                   ← LOG_FORMAT                   (все шесть, почтовый процесс)
//
// Две находки этой переписи стоит назвать отдельно, потому что чинить их
// пришлось РАЗНО:
//
//   - `LOG_LEVEL` чарт поставщика ставит БЕЗУСЛОВНО на почтовый процесс, из
//     `statefulSet.log.level`, чьё умолчание — `trace`. То есть боевой профиль
//     объявлял `log.level: info`, а почтовый процесс — тот самый, что рассылает
//     ссылки восстановления, — работал на `trace`. Условия «объявили и не
//     заметили» здесь не было вовсе: заметить неоткуда;
//   - `secrets.cookie`/`secrets.cipher` наш файл объявлял литералом
//     `${KRATOS_SECRETS_COOKIE}`, а подстановки этих величин в поде нет
//     (init-контейнер подставляет ТОЛЬКО величину обратного вызова и проверяет
//     ровно её). То есть объявление было не только перебито — оно было
//     НЕИСПОЛНИМО, и неисполнимость свою маскировало тем самым перебиванием.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ИМЕННО УТВЕРЖДАЕТСЯ
//
// Перепись в ДВЕ колонки, по каждому объявленному стеку (`stacks.txt` — там
// цепочка `-f` объявлена единожды, здесь она не выписывается):
//
//	A — ключей объявляет НАШ файл настроек;
//	B — из них перебиваются переменной на процессе, который этот файл ЧИТАЕТ.
//
// Требуется B == 0. Печатаются ОБЕ величины: «ноль находок» обязано быть
// отличимо от «ноль прочитанного».
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ РЕНДЕР, А НЕ ОБЪЯВЛЕНИЕ
//
// Имя переменной НЕ ВСТРЕЧАЕТСЯ В НАШЕМ ДЕРЕВЕ НИ РАЗУ: его производит чарт
// поставщика из СВОЕГО ключа значений. Предикат, ищущий имя переменной у нас
// (а именно так и был записан признак в #1733), даёт ноль на дереве, где
// перебиваются пять ключей, — то есть измеряет не предмет, а наш словарь.
// Единственное место, где обе стороны видны сразу, — отрендеренный под.
//
// Отсюда тег `helmcharts`: рендер умбреллы требует материализованных архивов
// зависимостей, а git их не отслеживает (deploy/.gitignore). Тот же раскол и та
// же цена, что у identity_courier_arg_premise_test.go; достижимость тега держат
// TestChartPremiseIsActuallyInvoked и TestChartArchivesAreStillUntracked в
// нетегированной части, здесь они не дублируются.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕГО ЭТОТ ГЕЙТ НЕ ДОКАЗЫВАЕТ — НАЗВАНО, ЧТОБЫ НЕ ЧИТАЛОСЬ ШИРЕ
//
//  1. Он судит переменные, ВИДИМЫЕ В РЕНДЕРЕ. Форма `envFrom: secretRef` вносит
//     в окружение ключи чужого секрета, и имён этих ключей в рендере нет by
//     construction — перепись по ним неразрешима. Поэтому такая форма на
//     процессе-субъекте есть ОТКАЗ, а не тишина: «B = 0» на ней было бы
//     утверждением, которого гейт не измерял.
//  2. Он не судит, ВЕРНО ЛИ значение переменной. Совпадающее значение класс не
//     закрывает: решает всё равно окружение, и следующая правка нашего файла
//     снова не доедет.
//  3. Он не судит конфигурацию поставщика — только НАШУ (метка
//     `kacho.cloud/component: kratos-config`, которую ставит наш подчарт).
//
// Способность упасть и смолчать доказана инъекцией НАСТОЯЩИМ входом через
// настоящие ручки чарта — identity_file_keys_survive_the_environment_injection_test.go.
package deploy_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// ourIdentityConfigLabel — метка, которой НАШ подчарт метит свою карту настроек
// службы личности. Карта поставщика её не несёт, поэтому «наш файл» здесь
// определён признаком, а не именем ресурса и не совпадением префикса релиза.
const (
	ourIdentityConfigLabelKey   = "kacho.cloud/component"
	ourIdentityConfigLabelValue = "kratos-config"
	identityConfigDataKey       = "kratos.yaml"
)

// renderStack рендерит умбреллу цепочкой профилей стека.
//
// Отсутствие helm — жёсткий провал при CI, а не пропуск: гейт, молча ставший
// инертным на задании, гейтящем мёрж, гейтом не является. Та же дисциплина, что
// у renderIdentitySubchart.
func renderStack(t *testing.T, chain []string, sets ...string) (string, error) {
	t.Helper()
	if _, err := exec.LookPath("helm"); err != nil {
		if os.Getenv("CI") != "" {
			t.Fatalf("helm не в PATH при CI — рендер-гейт обязан исполняться, а не пропускаться")
		}
		t.Skip("helm не в PATH — рендер-гейт пропущен")
	}
	args := []string{"template", "kacho-umbrella", umbrellaDir, "-n", "kacho"}
	for _, p := range chain {
		args = append(args, "-f", filepath.Join(umbrellaDir, p))
	}
	for _, s := range sets {
		args = append(args, "--set", s)
	}
	out, err := exec.Command("helm", args...).CombinedOutput() // #nosec G204 -- фиксированный бинарь, аргументы из дерева
	return string(out), err
}

// renderedDoc — один документ рендера, разобранный настолько, насколько нужен
// этой переписи.
//
// ИМЕННО ПСЕВДОНИМ, а не новый тип: yaml.v3 разбирает вложенные отображения В
// ТОТ ЖЕ тип, что у цели, поэтому у именованного типа `metadata` приезжает как
// `renderedDoc`, а приведение к `map[string]any` не проходит — и перепись молча
// не находит НИЧЕГО. Ровно это и случилось при заведении: гейт объявил обход
// пустым на дереве, где карт настроек шесть из шести.
type renderedDoc = map[string]any

func decodeRender(t *testing.T, rendered string) []renderedDoc {
	t.Helper()
	var docs []renderedDoc
	dec := yaml.NewDecoder(strings.NewReader(rendered))
	for {
		var d renderedDoc
		if err := dec.Decode(&d); err != nil {
			break
		}
		if d != nil {
			docs = append(docs, d)
		}
	}
	return docs
}

func str(m map[string]any, k string) string {
	s, _ := m[k].(string)
	return s
}

func submap(m map[string]any, k string) map[string]any {
	s, _ := m[k].(map[string]any)
	return s
}

func slice(m map[string]any, k string) []any {
	s, _ := m[k].([]any)
	return s
}

// configKeyPaths — ВСЕ пути ключей тела настроек, включая промежуточные узлы:
// переменная родительского пути перебивает и потомков, поэтому судить только
// листья значило бы сузить перепись.
func configKeyPaths(node any, prefix string, out *[]string) {
	m, ok := node.(map[string]any)
	if !ok {
		return
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		p := k
		if prefix != "" {
			p = prefix + "." + k
		}
		*out = append(*out, p)
		configKeyPaths(m[k], p, out)
	}
}

// envNamesFor — имена переменных, любая из которых перебивает ключ `path`:
// имя самого пути и имена всех его префиксов.
func envNamesFor(path string) []string {
	segs := strings.Split(path, ".")
	out := make([]string, 0, len(segs))
	for i := 1; i <= len(segs); i++ {
		out = append(out, strings.ToUpper(strings.Join(segs[:i], "_")))
	}
	return out
}

// identitySubject — контейнер, ЧИТАЮЩИЙ наш файл настроек.
type identitySubject struct {
	Workload  string
	Container string
	Env       map[string]bool
	EnvFrom   bool
}

// ourIdentityConfigBodies — тела настроек, объявленные НАШИМ подчартом, по
// имени карты.
func ourIdentityConfigBodies(docs []renderedDoc) map[string]string {
	out := map[string]string{}
	for _, d := range docs {
		if str(d, "kind") != "ConfigMap" {
			continue
		}
		meta := submap(d, "metadata")
		labels := submap(meta, "labels")
		if str(labels, ourIdentityConfigLabelKey) != ourIdentityConfigLabelValue {
			continue
		}
		data := submap(d, "data")
		body, ok := data[identityConfigDataKey].(string)
		if !ok {
			continue
		}
		out[str(meta, "name")] = body
	}
	return out
}

// identitySubjects — контейнеры, читающие наш файл: под монтирует НАШУ карту
// настроек, а контейнер принимает файл настроек аргументом.
//
// Признак ВЫВОДИТСЯ из рендера, а не выписан путём: путь файла — величина
// профиля, и выписанный здесь он разошёлся бы с профилем молча.
func identitySubjects(docs []renderedDoc, ourConfigMaps map[string]string) []identitySubject {
	var out []identitySubject
	for _, d := range docs {
		kind := str(d, "kind")
		if kind != "Deployment" && kind != "StatefulSet" {
			continue
		}
		name := str(submap(d, "metadata"), "name")
		podSpec := submap(submap(submap(d, "spec"), "template"), "spec")
		if podSpec == nil {
			continue
		}

		// Монтирует ли под НАШУ карту настроек — вопрос к томам пода целиком.
		mountsOurs := false
		for _, v := range slice(podSpec, "volumes") {
			vm, _ := v.(map[string]any)
			cm := submap(vm, "configMap")
			if cm == nil {
				continue
			}
			if _, ok := ourConfigMaps[str(cm, "name")]; ok {
				mountsOurs = true
			}
		}
		if !mountsOurs {
			continue
		}

		for _, c := range slice(podSpec, "containers") {
			cm, _ := c.(map[string]any)
			takesConfig := false
			for _, a := range append(slice(cm, "command"), slice(cm, "args")...) {
				if s, ok := a.(string); ok && strings.Contains(s, "--config") {
					takesConfig = true
				}
			}
			if !takesConfig {
				continue
			}
			env := map[string]bool{}
			for _, e := range slice(cm, "env") {
				em, _ := e.(map[string]any)
				if n := str(em, "name"); n != "" {
					env[n] = true
				}
			}
			out = append(out, identitySubject{
				Workload:  name,
				Container: str(cm, "name"),
				Env:       env,
				EnvFrom:   len(slice(cm, "envFrom")) > 0,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Workload != out[j].Workload {
			return out[i].Workload < out[j].Workload
		}
		return out[i].Container < out[j].Container
	})
	return out
}

// overrideFinding — один ключ, перебиваемый переменной.
type overrideFinding struct {
	Stack     string
	ConfigMap string
	Key       string
	Env       string
	Workload  string
	Container string
}

// scanIdentityEnvOverrides — ЯДРО гейта, чистое от helm и файловой системы:
// перепись и находки по одному стеку. Отдельная функция затем, что инъекция
// проверяет её на входах, которых в дереве нет, не поднимая рендера.
func scanIdentityEnvOverrides(stack string, bodies map[string]map[string]any,
	subjects []identitySubject,
) (declared int, findings []overrideFinding) {
	names := make([]string, 0, len(bodies))
	for n := range bodies {
		names = append(names, n)
	}
	sort.Strings(names)

	seen := map[string]bool{}
	for _, cmName := range names {
		var paths []string
		configKeyPaths(bodies[cmName], "", &paths)
		declared += len(paths)
		for _, p := range paths {
			for _, s := range subjects {
				for _, env := range envNamesFor(p) {
					if !s.Env[env] {
						continue
					}
					k := cmName + "|" + p + "|" + env + "|" + s.Workload + "/" + s.Container
					if seen[k] {
						continue
					}
					seen[k] = true
					findings = append(findings, overrideFinding{
						Stack: stack, ConfigMap: cmName, Key: p, Env: env,
						Workload: s.Workload, Container: s.Container,
					})
					break
				}
			}
		}
	}
	sort.Slice(findings, func(i, j int) bool {
		a, b := findings[i], findings[j]
		if a.Key != b.Key {
			return a.Key < b.Key
		}
		return a.Workload+a.Container < b.Workload+b.Container
	})
	return declared, findings
}

// TestIdentityFileKeysAreNotOverriddenByTheEnvironment — ядро гейта.
func TestIdentityFileKeysAreNotOverriddenByTheEnvironment(t *testing.T) {
	stacks := deployStacks(t)

	names := make([]string, 0, len(stacks))
	for n := range stacks {
		names = append(names, n)
	}
	sort.Strings(names)

	var (
		allFindings []overrideFinding
		subjectsSum int
		bodiesSum   int
	)
	for _, name := range names {
		rendered, err := renderStack(t, stacks[name])
		if err != nil {
			t.Fatalf("стек %q не рендерится (%v) — вердикта НЕТ: «перебиваемых ключей не найдено» "+
				"здесь неотличимо от «не прочитано ничего». Вывод helm:\n%s", name, err, rendered)
		}
		docs := decodeRender(t, rendered)

		bodiesRaw := ourIdentityConfigBodies(docs)
		if len(bodiesRaw) == 0 {
			t.Fatalf("стек %q: в рендере нет ни одной карты настроек с меткой %s=%s и ключом %q — "+
				"либо метку сняли, либо наш файл перестал рендериться. Обход пуст, вердикт беспредметен "+
				"(документов разобрано %d)", name, ourIdentityConfigLabelKey, ourIdentityConfigLabelValue,
				identityConfigDataKey, len(docs))
		}
		bodies := map[string]map[string]any{}
		for cmName, body := range bodiesRaw {
			var cfg map[string]any
			if err := yaml.Unmarshal([]byte(body), &cfg); err != nil {
				t.Fatalf("стек %q: тело настроек карты %s не разбирается как YAML: %v", name, cmName, err)
			}
			bodies[cmName] = cfg
		}
		bodiesSum += len(bodies)

		subjects := identitySubjects(docs, bodiesRaw)
		if len(subjects) == 0 {
			t.Fatalf("стек %q: не найдено ни одного контейнера, читающего наш файл настроек — "+
				"перепись переменных не о чем вести, и «B = 0» означало бы «ноль прочитанного». "+
				"Проверьте, что процесс всё ещё принимает файл аргументом", name)
		}
		subjectsSum += len(subjects)

		for _, s := range subjects {
			if s.EnvFrom {
				t.Errorf("стек %q: %s/%s получает окружение формой `envFrom` — имён её ключей в рендере "+
					"НЕТ by construction, поэтому перепись неразрешима, а не пуста. Гейт не вправе "+
					"объявить «ни один ключ не перебивается», не измерив этого. Либо перечислите "+
					"переменные поимённо (`env`), либо снимите объявление ключей, которые этот секрет "+
					"может перебить", name, s.Workload, s.Container)
			}
		}

		declared, findings := scanIdentityEnvOverrides(name, bodies, subjects)
		t.Logf("осмотрено: стек %-12s · документов %3d · наших карт настроек %d · "+
			"процессов-читателей %d · A(ключей объявлено) %3d · B(перебиваются) %d",
			name, len(docs), len(bodies), len(subjects), declared, len(findings))
		allFindings = append(allFindings, findings...)
	}

	t.Logf("итого осмотрено: стеков %d · наших карт настроек %d · процессов-читателей %d · B(всего) %d",
		len(names), bodiesSum, subjectsSum, len(allFindings))

	if bodiesSum == 0 || subjectsSum == 0 {
		t.Fatalf("обход пуст (карт %d, читателей %d) — вердикт беспредметен", bodiesSum, subjectsSum)
	}

	for _, f := range allFindings {
		t.Errorf("стек %q: ключ %s карты %s объявлен НАШИМ файлом и перебивается переменной %s "+
			"на %s/%s — решает окружение, а не файл, и правка файла не наблюдаема.\n"+
			"    Исходов ТРИ, четвёртого нет: (1) убрать ключ из нашего файла, отдав величину "+
			"владельцу переменной; (2) убрать значение, из-за которого чарт поставщика производит "+
			"эту переменную; (3) если переменная безусловна — снять ключ у нас и объявить величину "+
			"ручкой чарта. Совпадающее значение исходом НЕ является: решает всё равно окружение.",
			f.Stack, f.Key, f.ConfigMap, f.Env, f.Workload, f.Container)
	}
}

// TestEnvNamesForCoversPathAndItsPrefixes — предпосылка ядра, проверяемая без
// рендера: перебивает ключ не только переменная его полного пути.
func TestEnvNamesForCoversPathAndItsPrefixes(t *testing.T) {
	got := envNamesFor("courier.smtp.connection_uri")
	want := []string{"COURIER", "COURIER_SMTP", "COURIER_SMTP_CONNECTION_URI"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("имена переменных для пути: получили %v, ожидали %v — перепись сузилась бы "+
			"до полного пути, и переменная родителя стала бы невидимой", got, want)
	}
	t.Logf("осмотрено: путей 1, имён переменных %d", len(got))
}
