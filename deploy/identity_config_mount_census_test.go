// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_config_mount_census_test.go — утверждение стража о том, КТО монтирует
// конфигурацию службы личности, обязано иметь производителя.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Страж величины обратного вызова (helm/umbrella/templates/…-guard.yaml) в
// собственном блоке границ утверждал, что карту настроек службы личности «не
// монтирует ни один профиль». Замер даёт обратное: её монтируют и читают ДВА
// профиля — разработки и БОЕВОЙ; у обоих есть том с картой, монтирование
// контейнеру подстановки и довод `--config`, направляющий процесс читать
// отрендеренный файл.
//
// Это не устаревший комментарий в стороне. Неверное утверждение стояло В САМОМ
// СТРАЖЕ — то есть ровно там, куда смотрит следующий, решая, что страж покрывает,
// а что нет. Класс назван в правилах дважды: «вводящий в заблуждение комментарий
// о безопасности» (следующий чинит код под неверный комментарий) и «утверждение,
// пережившее свой предмет» (граница объявлена шире факта, и потому её никто не
// проверяет).
//
// Цена уже наблюдалась в этом дереве соседним классом: конфигурация, впервые
// ставшая ЧИТАЕМОЙ, замещает умолчание поставщика целиком (`architecture.md`).
// Страж, полагающий, что карту никто не читает, не заметит именно этого.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ИМЕННО УТВЕРЖДАЕТСЯ
//
// Число монтирующих профилей, ОБЪЯВЛЕННОЕ стражем, равно числу, ВЫВЕДЕННОМУ
// обходом профилей. Расходятся — находка, называющая оба числа и поимённо тот
// профиль, который объявление не учло.
//
// ПОЧЕМУ ЧИСЛО, А НЕ ПЕРЕЧЕНЬ. Перечень имён в прозе разошёлся бы с деревом
// молча — ровно так и возникло исправляемое утверждение. Число тоже выписано, но
// оно НЕ МОЖЕТ разойтись молча: его сверяет эта проверка, и профиль, начавший или
// переставший монтировать карту, роняет прогон вместе со строкой стража.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ИМЯ ТОМА И ИМЯ КАРТЫ ВЫВОДЯТСЯ, А НЕ ВЫПИСАНЫ
//
// Ни одно имя тома, карты настроек или профиля здесь не написано. Цепочка вывода:
//
//  1. профили объявляют довод `--config <путь>` — это файл, который читает
//     процесс службы личности;
//  2. в шаблонах нашего подчарта есть перенаправление `<источник> > <путь>` —
//     шаг подстановки, который этот файл пишет;
//  3. том, чей путь монтирования накрывает `<источник>`, и есть носитель карты
//     настроек;
//  4. профили, объявляющие том с ЭТИМ именем и источником-картой, — монтирующие.
//
// Переименуют том, карту или путь — цепочка пройдёт заново; выписанное имя
// разошлось бы.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ОБЪЯВЛЕНИЯ, А НЕ РЕНДЕР
//
// Рендер требует `helm` и скачанных зависимостей, поэтому проверка над рендером
// умеет пропуститься; пропущенная проверка не краснеет никогда. Здесь читаются
// объявления — профили и шаблоны.
package deploy_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// configArg — довод, которым профиль направляет процесс читать файл настроек.
var configArg = regexp.MustCompile(`(?m)^\s*-\s*(/\S+\.ya?ml)\s*$`)

// redirection — перенаправление вывода шага подстановки: слева источник, справа
// файл, который читает процесс.
var redirection = regexp.MustCompile(`(\S+)\s*>\s*(/\S+\.ya?ml)`)

// mountPathLine / volumeItemName — разбор списка монтирований. Имя берётся у
// элемента списка, путь — у его же ключа `mountPath`.
var mountPathLine = regexp.MustCompile(`^(\s*)mountPath:\s*(\S+)\s*$`)
var volumeItemName = regexp.MustCompile(`^(\s*)-\s*name:\s*(\S+)\s*$`)

// declaredMountingProfiles — строка объявления в страже. Число выписано, но
// разойтись молча не может: его сверяет эта проверка.
var declaredMounts = regexp.MustCompile(`МОНТИРУЮЩИХ ПРОФИЛЕЙ:\s*(\d+)`)

// ─────────────────────────────────────────────────────────────────────────────
// ФАКТЫ ДЕРЕВА

// mountFacts — то, что проверка вывела из дерева.
type mountFacts struct {
	sourceVolume string   // имя тома, который читает шаг подстановки
	configPaths  []string // файлы настроек, названные доводом профилей
	mounting     []string // профили, объявляющие том-источник картой настроек
	withConfig   []string // из них те, что вдобавок несут довод `--config`
	mapNames     []string // имена карт настроек, названные источником тома
	profiles     int      // всего профилей осмотрено
	declared     int      // число, объявленное стражем
	declaredAt   string   // координата объявления
	// Шаг подстановки — ОДНО объявление на дерево, найденное по признаку, а не
	// по имени. Его читают два гейта, и потому оно выводится здесь однажды:
	// два расчёта одного и того же разъезжаются молча.
	stepCoord string
	stepBody  string
}

// volumeMounts — пары (имя тома, путь монтирования) из текста шаблона.
func volumeMounts(body string) map[string]string {
	lines := strings.Split(body, "\n")
	out := map[string]string{}
	name, nameIndent := "", -1
	for _, ln := range lines {
		if m := volumeItemName.FindStringSubmatch(ln); m != nil {
			name, nameIndent = m[2], len(m[1])
			continue
		}
		m := mountPathLine.FindStringSubmatch(ln)
		if m == nil || name == "" {
			continue
		}
		if len(m[1]) <= nameIndent {
			name = "" // вышли из элемента списка
			continue
		}
		out[name] = m[2]
	}
	return out
}

// configMapVolumes — тома, объявленные профилем: имя тома → имя карты настроек.
func configMapVolumes(body string) map[string]string {
	lines := strings.Split(body, "\n")
	out := map[string]string{}
	for i, ln := range lines {
		m := volumeItemName.FindStringSubmatch(ln)
		if m == nil {
			continue
		}
		indent := len(m[1])
		inCM := false
		for j := i + 1; j < len(lines); j++ {
			cur := lines[j]
			if strings.TrimSpace(cur) == "" {
				continue
			}
			ci := len(cur) - len(strings.TrimLeft(cur, " "))
			if ci <= indent {
				break
			}
			if strings.TrimSpace(cur) == "configMap:" {
				inCM = true
				continue
			}
			if !inCM {
				continue
			}
			if s := scalarLine.FindStringSubmatch(cur); s != nil && s[2] == "name" {
				out[m[2]] = strings.Trim(s[3], `"'`)
				break
			}
		}
	}
	return out
}

func identityMountFacts(t *testing.T) mountFacts {
	t.Helper()
	f := mountFacts{declared: -1}

	profiles, err := filepath.Glob(filepath.Join(umbrellaDir, "values*.yaml"))
	if err != nil {
		t.Fatalf("обход профилей: %v", err)
	}
	sort.Strings(profiles)
	f.profiles = len(profiles)

	profileBody := map[string]string{}
	cfgPaths := map[string]bool{}
	for _, p := range profiles {
		b, rerr := os.ReadFile(p) // #nosec G304 -- путь получен обходом собственного дерева
		if rerr != nil {
			t.Fatalf("чтение %s: %v", p, rerr)
		}
		profileBody[p] = string(b)
		for _, m := range configArg.FindAllStringSubmatch(string(b), -1) {
			cfgPaths[m[1]] = true
		}
	}
	for p := range cfgPaths {
		f.configPaths = append(f.configPaths, p)
	}
	sort.Strings(f.configPaths)

	// (2)+(3): шаг подстановки объявлен ОДНИМ именованным шаблоном подчарта. Он
	// находится по признаку — объявляет контейнер и называет тот самый файл,
	// который читает процесс. Ни имени объявления, ни имени тома здесь нет.
	tpls, err := filepath.Glob(filepath.Join(umbrellaDir, "charts", "*", "templates", "*"))
	if err != nil {
		t.Fatalf("обход шаблонов подчартов: %v", err)
	}
	sort.Strings(tpls)
	for _, p := range tpls {
		b, rerr := os.ReadFile(p) // #nosec G304 -- путь получен обходом собственного дерева
		if rerr != nil {
			continue
		}
		for name, blk := range defineBodies(string(b)) {
			if !strings.Contains(blk, "image:") || !strings.Contains(blk, "args:") {
				continue // это не объявление контейнера
			}
			named := false
			for path := range cfgPaths {
				if strings.Contains(blk, path) {
					named = true
				}
			}
			if !named {
				continue
			}
			if f.stepBody != "" {
				t.Errorf("объявлений шага подстановки больше одного (%s и %s:%s) — "+
					"два места об одном предмете разойдутся молча", f.stepCoord, p, name)
			}
			f.stepCoord, f.stepBody = p+":"+name, blk
		}
	}
	// Том-источник — тот, чей путь монтирования НЕ накрывает читаемый процессом
	// файл: второй том шага и есть тот, куда он пишет.
	if f.stepBody != "" {
		for vol, path := range volumeMounts(f.stepBody) {
			covers := false
			for cfg := range cfgPaths {
				if strings.HasPrefix(cfg, strings.TrimRight(path, "/")+"/") {
					covers = true
				}
			}
			if !covers {
				f.sourceVolume = vol
			}
		}
	}

	// (4): монтирующие профили.
	if f.sourceVolume != "" {
		maps := map[string]bool{}
		for _, p := range profiles {
			vols := configMapVolumes(profileBody[p])
			cm, ok := vols[f.sourceVolume]
			if !ok {
				continue
			}
			f.mounting = append(f.mounting, filepath.Base(p))
			maps[cm] = true
			if configArg.MatchString(profileBody[p]) {
				f.withConfig = append(f.withConfig, filepath.Base(p))
			}
		}
		for m := range maps {
			f.mapNames = append(f.mapNames, m)
		}
		sort.Strings(f.mapNames)
	}

	// Объявление стража.
	guards, err := filepath.Glob(filepath.Join(umbrellaDir, "templates", "*.yaml"))
	if err != nil {
		t.Fatalf("обход шаблонов умбреллы: %v", err)
	}
	sort.Strings(guards)
	for _, p := range guards {
		b, rerr := os.ReadFile(p) // #nosec G304 -- путь получен обходом собственного дерева
		if rerr != nil {
			continue
		}
		for i, ln := range strings.Split(string(b), "\n") {
			m := declaredMounts.FindStringSubmatch(ln)
			if m == nil {
				continue
			}
			if f.declared >= 0 {
				t.Errorf("объявление числа монтирующих профилей встречается дважды "+
					"(%s и %s:%d) — два места об одном предмете разойдутся молча",
					f.declaredAt, p, i+1)
			}
			f.declared = atoiOrFatal(t, m[1])
			f.declaredAt = fmt.Sprintf("%s:%d", p, i+1)
		}
	}
	return f
}

func atoiOrFatal(t *testing.T, s string) int {
	t.Helper()
	n := 0
	for _, r := range s {
		n = n*10 + int(r-'0')
	}
	return n
}

// ─────────────────────────────────────────────────────────────────────────────
// ЯДРО — чистая функция над фактами.

func scanMountClaim(f mountFacts) []string {
	var out []string
	if f.declared < 0 {
		out = append(out, "страж не объявляет числа монтирующих профилей — "+
			"утверждение о границе не имеет производителя, и разойтись с деревом оно может молча")
		return out
	}
	if f.declared != len(f.mounting) {
		out = append(out, fmt.Sprintf(
			"%s объявляет МОНТИРУЮЩИХ ПРОФИЛЕЙ: %d, а обход даёт %d (%s). "+
				"Утверждение стража о собственной границе разошлось с деревом: "+
				"либо профиль начал монтировать карту настроек службы личности, либо перестал",
			f.declaredAt, f.declared, len(f.mounting), strings.Join(f.mounting, ", ")))
	}
	for _, p := range f.mounting {
		if !listHas(f.withConfig, p) {
			out = append(out, fmt.Sprintf(
				"профиль %s объявляет том с картой настроек, но НЕ несёт довода `--config` — "+
					"карта смонтирована и не читается, то есть объявленное поведение не исполняется", p))
		}
	}
	return out
}

func listHas(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// ─────────────────────────────────────────────────────────────────────────────
// ПРОВЕРКА ПО ДЕРЕВУ

func TestGuardBoundaryTracksWhoMountsTheIdentityConfig(t *testing.T) {
	f := identityMountFacts(t)

	t.Logf("осмотрено: профилей %d; файлов настроек по доводу `--config` %d (%s); "+
		"том-источник %q; монтируют %d (%s); из них читают %d; карт настроек %v; объявлено стражем %d",
		f.profiles, len(f.configPaths), strings.Join(f.configPaths, ", "),
		f.sourceVolume, len(f.mounting), strings.Join(f.mounting, ", "),
		len(f.withConfig), f.mapNames, f.declared)

	if f.profiles == 0 {
		t.Fatalf("профилей не найдено ни одного — обход пуст, и это отказ, а не успех")
	}
	if len(f.configPaths) == 0 {
		t.Fatalf("ни один профиль не называет файл настроек доводом — цепочка вывода " +
			"оборвалась на первом звене; «ноль находок» здесь неотличимо от «ноль прочитанного»")
	}
	if f.stepBody == "" {
		t.Fatalf("шаг подстановки, называющий читаемый процессом файл (%s), в шаблонах не найден — "+
			"вывести носитель карты настроек не из чего", strings.Join(f.configPaths, ", "))
	}
	if f.sourceVolume == "" {
		t.Fatalf("у шага подстановки %s нет тома, отличного от того, куда он пишет — "+
			"источник карты настроек вывести не из чего", f.stepCoord)
	}
	if len(f.mounting) == 0 {
		t.Fatalf("том %q не объявлен ни одним профилем — либо монтирование снято целиком, "+
			"либо разбор ослеп; и то и другое требует решения, а не молчания", f.sourceVolume)
	}

	for _, msg := range scanMountClaim(f) {
		t.Errorf("%s", msg)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// САМОПРОВЕРКА — инъекция в обе стороны на синтетическом входе.

func TestScanMountClaim_SelfTest(t *testing.T) {
	base := mountFacts{
		sourceVolume: "том-источник",
		configPaths:  []string{"/etc/х/у.yaml"},
		mounting:     []string{"values.а.yaml", "values.б.yaml"},
		withConfig:   []string{"values.а.yaml", "values.б.yaml"},
		profiles:     6,
		declared:     2,
		declaredAt:   "страж.yaml:36",
	}

	// (0) КОНТРОЛЬ: объявление совпадает с обходом — молчание.
	if got := scanMountClaim(base); len(got) != 0 {
		t.Errorf("(0) согласованное объявление обязано молчать: %v", got)
	}

	// (A) ИНЪЕКЦИЯ: профиль начал монтировать карту, объявление за ним не пошло.
	grew := base
	grew.mounting = append(append([]string{}, base.mounting...), "values.новый.yaml")
	grew.withConfig = grew.mounting
	got := scanMountClaim(grew)
	switch {
	case len(got) == 0:
		t.Errorf("(A) новый монтирующий профиль ПРОПУЩЕН — гейт вакуумен")
	case !strings.Contains(got[0], "values.новый.yaml") ||
		!strings.Contains(got[0], "2") || !strings.Contains(got[0], "3"):
		t.Errorf("(A) находка не называет оба числа и профиль поимённо: %s", got[0])
	case !strings.Contains(got[0], "страж.yaml:36"):
		t.Errorf("(A) находка не называет координату объявления: %s", got[0])
	}

	// (B) ИНЪЕКЦИЯ в обратную сторону: профиль перестал монтировать.
	shrank := base
	shrank.mounting = []string{"values.а.yaml"}
	shrank.withConfig = shrank.mounting
	if got := scanMountClaim(shrank); len(got) == 0 {
		t.Errorf("(B) снятое монтирование ПРОПУЩЕНО — объявление пережило бы свой предмет")
	}

	// (C) ИНЪЕКЦИЯ: страж вовсе не объявляет числа — утверждение без производителя.
	silent := base
	silent.declared = -1
	silent.declaredAt = ""
	if got := scanMountClaim(silent); len(got) == 0 {
		t.Errorf("(C) отсутствие объявления ПРОПУЩЕНО — граница снова стала непроверяемой")
	}

	// (D) ИНЪЕКЦИЯ: карта смонтирована, но не читается.
	unread := base
	unread.withConfig = []string{"values.а.yaml"}
	got = scanMountClaim(unread)
	if len(got) == 0 || !strings.Contains(got[0], "values.б.yaml") {
		t.Errorf("(D) смонтированная и нечитаемая карта ПРОПУЩЕНА: %v", got)
	}
}

// TestMountPredicates_RecogniseTheRealTree — предпосылки разбора верны на
// настоящем дереве. Без этого самопроверка выше доказывала бы работоспособность
// ядра на входе, которого не бывает.
func TestMountPredicates_RecogniseTheRealTree(t *testing.T) {
	// Разбор списка монтирований обязан узнавать НАСТОЯЩУЮ форму, а не только
	// синтетическую: имя стоит у элемента списка, путь — ключом внутри него.
	sample := "" +
		"  volumeMounts:\n" +
		"    - name: том-один\n" +
		"      mountPath: /путь/один\n" +
		"      readOnly: true\n" +
		"    - name: том-два\n" +
		"      mountPath: /путь/два\n"
	got := volumeMounts(sample)
	if got["том-один"] != "/путь/один" || got["том-два"] != "/путь/два" {
		t.Errorf("разбор монтирований не узнаёт обычную форму: %v", got)
	}

	vols := configMapVolumes("" +
		"    extraVolumes:\n" +
		"      - name: том-карты\n" +
		"        configMap:\n" +
		"          name: карта-настроек\n" +
		"      - name: том-пустышка\n" +
		"        emptyDir: {}\n")
	if vols["том-карты"] != "карта-настроек" {
		t.Errorf("разбор томов не узнаёт источник-карту: %v", vols)
	}
	if _, ok := vols["том-пустышка"]; ok {
		t.Errorf("разбор томов принял пустой том за карту настроек: %v", vols)
	}
}
