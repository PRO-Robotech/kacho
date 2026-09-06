// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_courier_reads_what_it_mounts_test.go — почтовый процесс службы
// личности читает ТО ЖЕ, что и основной, и не получает того, чего не читает.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// У службы личности ДВА рабочих объекта: основной процесс (Deployment) и
// почтовый (StatefulSet, `…-courier`). Чарт поставщика раздаёт им настройки
// НЕСИММЕТРИЧНО, и несимметричность эта — не наша, её надо знать наизусть:
//
//   - тома, монтирования, переменные, дополнительные и init-контейнеры почтовый
//     процесс НАСЛЕДУЕТ от основного, когда своих не объявлено;
//   - аргументы командной строки он НЕ наследует: у основного они приезжают из
//     `kratos.deployment.extraArgs`, у почтового — из `kratos.statefulSet.extraArgs`,
//     и это разные ручки.
//
// Отсюда ровно один способ ошибиться, и он выглядит как исправная работа:
// провязать нашу конфигурацию через `deployment.*` — тогда карты настроек
// ДОЕДУТ до почтового процесса томами, а указание читать их НЕ доедет. Со
// стороны кластера всё смонтировано; со стороны процесса не изменилось ничего.
//
// Цена названа предметно, а не абстрактно: письма шлёт ИМЕННО почтовый процесс,
// и раздел `courier.smtp` живёт в той самой конфигурации. Расхождение означает,
// что адрес отправителя, шлюз и шаблоны писем наша конфигурация ОБЪЯВЛЯЕТ, а
// применяются чужие умолчания — «принято-и-проигнорировано» на уровне настроек
// (`api-conventions.md` §«Принято-и-проигнорировано»). Заметить это неоткуда:
// письма продолжают уходить.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ИМЕННО УТВЕРЖДАЕТСЯ
//
//	A. ЧИТАЮТ ОДНО И ТО ЖЕ — в ОБЕ стороны. Множество файлов настроек,
//	   названных основному процессу (`--config <путь>` в
//	   `kratos.deployment.extraArgs`), СОВПАДАЕТ с множеством, названным
//	   почтовому (`kratos.statefulSet.extraArgs`). Считаются обе разности, и
//	   каждая непустая — находка.
//
//	   Направление проверяется ОБА, потому что предмет — согласие двух
//	   процессов о том, что они читают, а не «основной впереди почтового».
//	   Прежняя редакция считала одну разность: она закрывала ту половину
//	   класса, которая уже случилась, и о симметричной не утверждала ничего —
//	   файл, названный только почтовому, проходил молча. Ломается при этом
//	   зеркальное: наша конфигурация объявляет ОСНОВНОМУ процессу схему
//	   личности, потоки самообслуживания и адреса возврата, а применяются
//	   чужие умолчания поставщика.
//
//	B. НЕ ПОЛУЧАЕТ ЛИШНЕГО. Каждый том, доезжающий до почтового процесса, имеет
//	   в нём читателя — монтирование либо контейнер, который его называет. Том
//	   без читателя читается как «настройки доехали», хотя не доехали; для тома,
//	   собранного из секрета, это ещё и расширение поверхности.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ОБЪЯВЛЕНИЯ, А НЕ РЕНДЕР
//
// Та же причина, что у соседних posture_parity_test.go и
// identity_callback_transport_test.go: контракт — то, что профиль ОБЪЯВЛЯЕТ.
// Проверке не нужны ни `helm`, ни скачанные архивы чартов, поэтому она не умеет
// пропуститься. Предпосылку о ЧУЖОМ чарте (что аргументы не наследуются, а тома
// наследуются) держит отдельный файл под тегом `helmcharts` — там, где архив
// чарта материализован.
//
// ─────────────────────────────────────────────────────────────────────────────
// ГРАНИЦА ПРЕДМЕТА (названа, чтобы «зелено» не читалось шире, чем есть)
//
//   - Проверяется СОГЛАСИЕ двух ручек, а не решение профиля вообще что-то
//     провязывать. Профиль, ничего не объявивший ни одному из процессов,
//     приходит сюда законным: оба читают файл поставщика, и это один файл.
//     Незаконно РАСХОЖДЕНИЕ — в любую сторону.
//   - Проверяется наличие ЧИТАТЕЛЯ у тома, а не осмысленность содержимого. Том,
//     смонтированный и не нужный по существу, отсюда виден census-строкой, а не
//     находкой.
//   - НЕ проверяется, что переменная окружения, унаследованная почтовым
//     процессом, ему подходит. Отдельный предмет, замеренный и намеренно
//     оставленный за границей: боевой профиль объявляет основному процессу
//     `SSL_CERT_FILE` (deploy/helm/umbrella/values.prod.yaml, блок
//     `kratos.deployment.extraEnv`), почтовый наследует его вместе с томом — то
//     есть заменяет СВОЙ набор корней доверия внутренним удостоверяющим, при
//     том что его единственный исходящий peer — почтовый шлюз. «Подходит ли
//     набор корней» — суждение, а не предикат, и гейт, который бы его изображал,
//     был бы формой без содержания.
package deploy_test

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// identityProviderKey — ключ значений умбреллы, под которым живёт чарт
// поставщика личности. Имя выписано ОДИН раз и не переживёт свой предмет:
// профиль, не объявляющий этот ключ ни разу, роняет проверку ниже как
// «координата переехала», а не молчит.
const identityProviderKey = "kratos"

// Виды находок. Направление расхождения — ЧАСТЬ вида, а не подробность текста:
// «назван основному и не назван почтовому» и «назван почтовому и не назван
// основному» ломают разное и чинятся разными ручками, поэтому один вид на оба
// сделал бы направление нарушения неразличимым в отчёте.
const (
	kindConfigNotInCourier  = "файл настроек, не названный почтовому процессу"
	kindConfigNotInMain     = "файл настроек, не названный основному процессу"
	kindVolumeWithoutReader = "том без читателя"
)

// courierFinding — одна находка с координатой, по которой её можно открыть.
type courierFinding struct {
	profile string
	kind    string // один из kind* выше
	subject string // путь файла настроек либо имя тома
	detail  string
}

func (f courierFinding) String() string {
	return fmt.Sprintf("%s: %s %q — %s", f.profile, f.kind, f.subject, f.detail)
}

// courierCensus — что проверка увидела в одном профиле. Печатается всегда:
// «ноль находок» обязано быть отличимо от «ноль прочитанного».
type courierCensus struct {
	profile        string
	declares       bool
	mainConfigs    []string
	courierConfigs []string
	onlyMain       []string // названы основному и НЕ названы почтовому
	onlyCourier    []string // названы почтовому и НЕ названы основному
	volumes        []string // тома, доезжающие до почтового процесса
	secretVolumes  []string // из них собранные из секрета
	inherited      bool     // тома пришли наследованием от основного процесса
	unresolved     []string // вызовы шаблонов, не объявленных в НАШЕМ дереве
}

// scanCourierWiring — ядро проверки, вынесенное отдельной функцией, чтобы
// самопроверка ниже подавала ему синтетический вход, а не подделывала дерево.
//
// Правило наследования взято у чарта поставщика ДОСЛОВНО: своё значение
// почтового процесса, если оно непусто, иначе значение основного. Аргументы в
// это правило не входят — у них своя, несимметричная раздача.
func scanCourierWiring(profiles map[string]map[string]any, defs map[string]string) ([]courierFinding, []courierCensus) {
	var findings []courierFinding
	var census []courierCensus

	names := make([]string, 0, len(profiles))
	for n := range profiles {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, name := range names {
		tree := profiles[name]
		c := courierCensus{profile: name}

		provider, ok := lookup(tree, identityProviderKey)
		if !ok {
			census = append(census, c)
			continue
		}
		providerMap, ok := provider.(map[string]any)
		if !ok {
			census = append(census, c)
			continue
		}
		c.declares = true

		dep, _ := providerMap["deployment"].(map[string]any)
		sts, _ := providerMap["statefulSet"].(map[string]any)

		// ── A. читают ОДНО И ТО ЖЕ ─────────────────────────────────────────
		// Предмет — СОГЛАСИЕ двух процессов о том, что они читают, а не
		// «основной впереди почтового». Поэтому считаются ОБЕ разности
		// множеств: односторонняя проверка закрывает ту половину класса,
		// которая уже случилась, и молчит о симметричной.
		c.mainConfigs = configPaths(stringList(dep["extraArgs"]))
		c.courierConfigs = configPaths(stringList(sts["extraArgs"]))
		c.onlyMain = missingFrom(c.mainConfigs, c.courierConfigs)
		c.onlyCourier = missingFrom(c.courierConfigs, c.mainConfigs)

		for _, path := range c.onlyMain {
			findings = append(findings, courierFinding{
				profile: name, kind: kindConfigNotInCourier, subject: path,
				detail: "назван основному процессу (`" + identityProviderKey +
					".deployment.extraArgs`) и НЕ назван почтовому (`" + identityProviderKey +
					".statefulSet.extraArgs`). Аргументы почтовый процесс не наследует: " +
					"карты доедут томами, указание читать их — нет, и раздел `courier.smtp` " +
					"нашей конфигурации останется без читателя",
			})
		}
		for _, path := range c.onlyCourier {
			findings = append(findings, courierFinding{
				profile: name, kind: kindConfigNotInMain, subject: path,
				detail: "назван почтовому процессу (`" + identityProviderKey +
					".statefulSet.extraArgs`) и НЕ назван основному (`" + identityProviderKey +
					".deployment.extraArgs`). Процессы читают РАЗНОЕ: то, что наша " +
					"конфигурация объявляет основному процессу — схему личности, потоки " +
					"самообслуживания, адреса возврата, — применяются чужие умолчания " +
					"поставщика, и заметить это неоткуда: процесс поднимается и отвечает. " +
					"Либо назовите файл обоим, либо снимите его у почтового",
			})
		}

		// ── B. не получает лишнего ─────────────────────────────────────────
		volumes, inherited := inheritedValue(sts["extraVolumes"], dep["extraVolumes"])
		mounts, _ := inheritedValue(sts["extraVolumeMounts"], dep["extraVolumeMounts"])
		initC, _ := inheritedValue(sts["extraInitContainers"], dep["extraInitContainers"])
		sideC, _ := inheritedValue(sts["extraContainers"], dep["extraContainers"])
		c.inherited = inherited

		readers := namesOf(mounts)
		containerText, unresolved := resolveIncludes(asText(initC)+"\n"+asText(sideC), defs)
		// Неразрешённый вызов НАХОДКОЙ не является: контейнер вправе звать шаблон
		// ЧУЖОГО чарта (образ, метки), которого в нашем дереве нет и не будет.
		// Но и молчать о нём нельзя — он сужает то, что проверка вообще видела,
		// поэтому имя уезжает в перепись и в текст находки ниже.
		c.unresolved = unresolved

		for _, v := range entries(volumes) {
			vname, _ := v["name"].(string)
			if vname == "" {
				continue
			}
			c.volumes = append(c.volumes, vname)
			if _, fromSecret := v["secret"]; fromSecret {
				c.secretVolumes = append(c.secretVolumes, vname)
			}
			if contains(readers, vname) || strings.Contains(containerText, vname) {
				continue
			}
			what := "том"
			if _, fromSecret := v["secret"]; fromSecret {
				what = "том из СЕКРЕТА"
			}
			detail := what + " доезжает до почтового процесса и не читается им ни " +
				"монтированием, ни контейнером. Читается как «настройки доехали», хотя " +
				"не доехали; либо провяжите читателя, либо объявите тома почтовому " +
				"процессу отдельно (`" + identityProviderKey + ".statefulSet.extraVolumes`)"
			if len(unresolved) > 0 {
				detail += ". Осмотр был неполон: не разрешились вызовы " +
					strings.Join(unresolved, ", ") + " — если читатель объявлен там, " +
					"разрешите вызов, а не снимайте проверку"
			}
			findings = append(findings, courierFinding{
				profile: name, kind: kindVolumeWithoutReader, subject: vname, detail: detail,
			})
		}
		census = append(census, c)
	}
	return findings, census
}

// inheritedValue — правило раздачи чарта поставщика: своё, если непусто, иначе
// чужое. Второй результат говорит, что значение пришло НАСЛЕДОВАНИЕМ.
func inheritedValue(own, from any) (any, bool) {
	if !isEmptyValue(own) {
		return own, false
	}
	return from, true
}

// isEmptyValue — «пусто» в том смысле, в каком его понимает шаблонизатор чарта.
func isEmptyValue(v any) bool {
	switch t := v.(type) {
	case nil:
		return true
	case string:
		return t == ""
	case []any:
		return len(t) == 0
	case map[string]any:
		return len(t) == 0
	default:
		return false
	}
}

// entries — список отображений (тома, монтирования) из значения профиля.
func entries(v any) []map[string]any {
	list, _ := v.([]any)
	out := make([]map[string]any, 0, len(list))
	for _, it := range list {
		if m, ok := it.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func namesOf(v any) []string {
	var out []string
	for _, m := range entries(v) {
		if n, _ := m["name"].(string); n != "" {
			out = append(out, n)
		}
	}
	return out
}

// asText — дополнительные контейнеры объявляются СТРОКОЙ шаблона; их разбор в
// YAML тут не нужен и был бы ложной точностью — нужно лишь знать, называют ли
// они том.
func asText(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if list, ok := v.([]any); ok {
		var b strings.Builder
		for _, it := range list {
			fmt.Fprintf(&b, "%v\n", it)
		}
		return b.String()
	}
	return fmt.Sprintf("%v", v)
}

// includeAction — вызов именованного шаблона внутри строки дополнительного
// контейнера. Читателем тома вполне может быть контейнер, собранный ТАКИМ
// вызовом: тогда имя тома стоит не в профиле, а в теле шаблона нашего подчарта.
// Не разрешив вызов, проверка объявила бы находкой исправную провязку — а гейт,
// у которого находки ложные, перестают читать.
var includeAction = regexp.MustCompile(`(?:include|template)\s+"([^"]+)"`)

// resolveIncludes — подставляет тела именованных шаблонов вместо их вызовов.
// Возвращает разрешённый текст и имена шаблонов, которых в дереве нет.
func resolveIncludes(text string, defs map[string]string) (string, []string) {
	var missing []string
	seen := map[string]bool{}
	// Глубина ограничена: тело шаблона вправе звать следующий, но цикл вызовов
	// не должен превращать проверку в бесконечную.
	for depth := 0; depth < 8; depth++ {
		names := includeAction.FindAllStringSubmatch(text, -1)
		if len(names) == 0 {
			break
		}
		grew := false
		for _, m := range names {
			if seen[m[1]] {
				continue
			}
			seen[m[1]] = true
			body, ok := defs[m[1]]
			if !ok {
				missing = append(missing, m[1])
				continue
			}
			text += "\n" + body
			grew = true
		}
		if !grew {
			break
		}
	}
	sort.Strings(missing)
	return text, missing
}

// templateDefinition — объявление именованного шаблона. Тело берётся до конца
// файла: точное сопоставление `define`/`end` тут не нужно — проверке важно лишь
// то, называет ли шаблон имя тома, а лишний хвост находок не создаёт (он может
// лишь ПРОМОЛЧАТЬ там, где сосед по файлу называет тот же том, и это надёжнее
// ложной находки).
var templateDefinition = regexp.MustCompile(`\{\{-?\s*define\s+"([^"]+)"\s*-?\}\}`)

// collectTemplateDefinitions — тела именованных шаблонов НАШИХ подчартов и
// умбреллы. Выводится обходом дерева: новый шаблон попадает под проверку без
// правки этого файла.
func collectTemplateDefinitions(t *testing.T) map[string]string {
	t.Helper()
	dirs := []string{umbrellaDir}
	for _, d := range subchartDirs(t) {
		dirs = append(dirs, d)
	}
	out := map[string]string{}
	for _, d := range dirs {
		for _, f := range chartTemplateFiles(t, d) {
			raw, err := os.ReadFile(f)
			if err != nil {
				t.Fatalf("read %s: %v", f, err)
			}
			body := string(raw)
			for _, m := range templateDefinition.FindAllStringSubmatchIndex(body, -1) {
				out[body[m[2]:m[3]]] = body[m[1]:]
			}
		}
	}
	return out
}

func stringList(v any) []string {
	list, _ := v.([]any)
	out := make([]string, 0, len(list))
	for _, it := range list {
		out = append(out, fmt.Sprintf("%v", it))
	}
	return out
}

// configPaths — пути файлов настроек, названные в аргументах. Читается ПАРА
// «--config <путь>», а не вхождение слова: аргумент, случайно содержащий эту
// подстроку, файлом настроек не является.
func configPaths(args []string) []string {
	var out []string
	for i := 0; i < len(args); i++ {
		a := strings.TrimSpace(args[i])
		if a == "--config" || a == "-c" {
			if i+1 < len(args) {
				out = append(out, strings.TrimSpace(args[i+1]))
			}
			continue
		}
		if v, ok := strings.CutPrefix(a, "--config="); ok {
			out = append(out, strings.TrimSpace(v))
		}
	}
	return out
}

// missingFrom — элементы `want`, которых нет в `have`. Одна функция на оба
// направления: две рукописные петли разошлись бы молча — ровно так односторонняя
// проверка и появилась.
func missingFrom(want, have []string) []string {
	var out []string
	for _, w := range want {
		if !contains(have, w) {
			out = append(out, w)
		}
	}
	return out
}

func contains(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}

// ─────────────────────────────────────────────────────────────────────────────

// TestIdentityCourierReadsWhatItMounts — основная проверка по дереву.
func TestIdentityCourierReadsWhatItMounts(t *testing.T) {
	profiles := map[string]map[string]any{}
	for _, f := range profileFiles(t) {
		profiles[f] = readYAML(t, f)
	}
	if len(profiles) == 0 {
		t.Fatal("осмотрено: профилей 0 — судить нечего, и «зелено» здесь означало бы " +
			"«ничего не читал», а не согласие ручек")
	}

	defs := collectTemplateDefinitions(t)
	t.Logf("осмотрено: именованных шаблонов наших подчартов %d", len(defs))
	if len(defs) == 0 {
		t.Fatal("осмотрено: именованных шаблонов 0 — читатель тома, собранный вызовом " +
			"шаблона, стал бы находкой на исправном дереве")
	}

	findings, census := scanCourierWiring(profiles, defs)

	var declaring, withVolumes, withSecrets, onlyMain, onlyCourier int
	for _, c := range census {
		if c.declares {
			declaring++
		}
		if len(c.volumes) > 0 {
			withVolumes++
		}
		if len(c.secretVolumes) > 0 {
			withSecrets++
		}
		onlyMain += len(c.onlyMain)
		onlyCourier += len(c.onlyCourier)
	}
	t.Logf("осмотрено: профилей %d, объявляют `%s` %d, доносят тома до почтового процесса %d, "+
		"из них тома из секретов %d", len(profiles), identityProviderKey, declaring, withVolumes, withSecrets)
	// Величины по НАПРАВЛЕНИЯМ, а не одной суммой: сумма 0 не отличает
	// «расхождений нет» от «есть по одному в каждую сторону», и направление
	// нарушения по ней не читается.
	t.Logf("осмотрено: расхождений файлов настроек — названо только основному %d, "+
		"только почтовому %d", onlyMain, onlyCourier)

	if declaring == 0 {
		t.Fatalf("ни один профиль не объявляет ключ `%s` — координата переехала, и проверка "+
			"молча перестала читать свой предмет", identityProviderKey)
	}

	for _, c := range census {
		if !c.declares {
			continue
		}
		src := "объявлены почтовому процессу"
		if c.inherited {
			src = "УНАСЛЕДОВАНЫ от основного процесса"
		}
		t.Logf("%s: файлы настроек основному %v, почтовому %v (только основному %v, только почтовому %v); "+
			"тома почтового %v (%s), из секретов %v, неразрешённых вызовов шаблонов %v",
			c.profile, c.mainConfigs, c.courierConfigs, c.onlyMain, c.onlyCourier,
			c.volumes, src, c.secretVolumes, c.unresolved)
	}

	for _, f := range findings {
		t.Error(f.String())
	}
}

// TestScanCourierWiring_SelfTest — способность проверки упасть И смолчать,
// доказанная инъекцией, а не прочтением. По каждой оси — обе стороны.
func TestScanCourierWiring_SelfTest(t *testing.T) {
	const ourConfig = "/etc/kaname-identity-rendered/kratos.yaml"

	profile := func(dep, sts map[string]any) map[string]any {
		p := map[string]any{}
		if dep != nil {
			p["deployment"] = dep
		}
		if sts != nil {
			p["statefulSet"] = sts
		}
		return map[string]any{identityProviderKey: p}
	}
	args := func(path string) []any { return []any{"--config", path} }
	vol := func(name, backing string) map[string]any {
		return map[string]any{"name": name, backing: map[string]any{"name": "x"}}
	}
	mount := func(name string) map[string]any {
		return map[string]any{"name": name, "mountPath": "/x"}
	}

	cases := []struct {
		what     string
		tree     map[string]any
		defs     map[string]string
		wantKind string // "" ⇒ находок быть не должно
		wantSubj string
	}{
		{
			what:     "дефект: файл назван основному и не назван почтовому",
			tree:     profile(map[string]any{"extraArgs": args(ourConfig)}, nil),
			wantKind: kindConfigNotInCourier, wantSubj: ourConfig,
		},
		{
			what:     "дефект: файл назван ТОЛЬКО почтовому и не назван основному",
			tree:     profile(nil, map[string]any{"extraArgs": args(ourConfig)}),
			wantKind: kindConfigNotInMain, wantSubj: ourConfig,
		},
		{
			what: "законный близнец: тот же файл назван обоим",
			tree: profile(map[string]any{"extraArgs": args(ourConfig)},
				map[string]any{"extraArgs": args(ourConfig)}),
		},
		{
			what: "законный близнец: аргумент основного не является файлом настроек",
			tree: profile(map[string]any{"extraArgs": []any{"--dev"}}, nil),
		},
		{
			what: "законный близнец: аргумент почтового не является файлом настроек",
			tree: profile(nil, map[string]any{"extraArgs": []any{"--watch-courier"}}),
		},
		{
			what: "дефект: том из секрета доезжает и не читается",
			tree: profile(map[string]any{
				"extraVolumes": []any{vol("ca", "secret")},
			}, nil),
			wantKind: kindVolumeWithoutReader, wantSubj: "ca",
		},
		{
			what: "законный близнец: тот же том смонтирован",
			tree: profile(map[string]any{
				"extraVolumes":      []any{vol("ca", "secret")},
				"extraVolumeMounts": []any{mount("ca")},
			}, nil),
		},
		{
			what: "законный близнец: том читает init-контейнер, а не монтирование",
			tree: profile(map[string]any{
				"extraVolumes":        []any{vol("src", "configMap")},
				"extraInitContainers": "- name: render\n  volumeMounts:\n    - name: src\n",
			}, nil),
		},
		{
			what: "законный близнец: том читает контейнер, собранный ВЫЗОВОМ шаблона",
			tree: profile(map[string]any{
				"extraVolumes":        []any{vol("src", "configMap")},
				"extraInitContainers": `{{- include "kacho.identity.render" . | nindent 0 }}`,
			}, nil),
			defs: map[string]string{
				"kacho.identity.render": "- name: render\n  volumeMounts:\n    - name: src\n",
			},
		},
		{
			what: "дефект остаётся находкой, даже когда часть вызовов не разрешилась",
			tree: profile(map[string]any{
				"extraVolumes":        []any{vol("src", "configMap")},
				"extraInitContainers": `{{- include "provider.image" . | nindent 0 }}`,
			}, nil),
			defs:     map[string]string{},
			wantKind: kindVolumeWithoutReader, wantSubj: "src",
		},
		{
			what: "законный близнец: вызов чужого шаблона не разрешён, но том читается",
			tree: profile(map[string]any{
				"extraVolumes":        []any{vol("src", "configMap")},
				"extraVolumeMounts":   []any{mount("src")},
				"extraInitContainers": `{{- include "provider.image" . | nindent 0 }}`,
			}, nil),
			defs: map[string]string{},
		},
		{
			what: "законный близнец: почтовый процесс объявил СВОИ тома — наследования нет",
			tree: profile(map[string]any{
				"extraVolumes": []any{vol("ca", "secret")},
			}, map[string]any{
				"extraVolumes":      []any{vol("own", "configMap")},
				"extraVolumeMounts": []any{mount("own")},
			}),
		},
		{
			what: "законный близнец: профиль не объявляет поставщика вовсе",
			tree: map[string]any{"something-else": map[string]any{}},
		},
	}

	for _, tc := range cases {
		got, _ := scanCourierWiring(map[string]map[string]any{"injected": tc.tree}, tc.defs)
		if tc.wantKind == "" {
			if len(got) != 0 {
				t.Errorf("%s: ожидалось молчание, получено %v", tc.what, got)
			}
			continue
		}
		if len(got) != 1 {
			t.Errorf("%s: ожидалась ровно одна находка, получено %v", tc.what, got)
			continue
		}
		if got[0].kind != tc.wantKind || got[0].subject != tc.wantSubj || got[0].profile != "injected" {
			t.Errorf("%s: находка не называет предмет: %+v", tc.what, got[0])
		}
	}
}

// TestConfigPaths_ReadsThePairNotTheWord — разбор аргументов утверждается
// отдельно: именно он отличает файл настроек от строки, которая на него похожа.
func TestConfigPaths_ReadsThePairNotTheWord(t *testing.T) {
	for _, tc := range []struct {
		in   []string
		want []string
	}{
		{[]string{"--config", "/a.yaml"}, []string{"/a.yaml"}},
		{[]string{"--config=/b.yaml"}, []string{"/b.yaml"}},
		{[]string{"-c", "/c.yaml"}, []string{"/c.yaml"}},
		{[]string{"--config", "/a.yaml", "--config", "/b.yaml"}, []string{"/a.yaml", "/b.yaml"}},
		{[]string{"--dev"}, nil},
		{[]string{"--expose-metrics-port", "4434"}, nil},
		{[]string{"--config"}, nil}, // висячий ключ: пути нет
		{[]string{"--configure-something"}, nil},
	} {
		got := configPaths(tc.in)
		if len(got) != len(tc.want) {
			t.Errorf("%v: получено %v, ожидалось %v", tc.in, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("%v: получено %v, ожидалось %v", tc.in, got, tc.want)
				break
			}
		}
	}
}
