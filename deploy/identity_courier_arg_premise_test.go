// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

//go:build helmcharts

// identity_courier_arg_premise_test.go — предпосылка соседнего гейта:
// НЕСИММЕТРИЧНАЯ раздача настроек двум процессам службы личности всё ещё
// такова, какой её описывает identity_courier_reads_what_it_mounts_test.go.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЗАЧЕМ ОТДЕЛЬНЫЙ ФАЙЛ И ЗАЧЕМ ТЕГ
//
// Соседний гейт требует от профиля дублировать `--config` во ВТОРУЮ ручку.
// Требование это обосновано ровно одним фактом о ЧУЖОМ дереве: почтовый процесс
// берёт аргументы из `statefulSet.extraArgs` и НЕ наследует
// `deployment.extraArgs`, при том что тома, монтирования, переменные и
// дополнительные контейнеры он наследует. Перевернётся раздача — требование
// станет бессмысленным, а гейт продолжит его предъявлять, и следующий снимет
// его как непонятный.
//
// Тег `helmcharts` — тот же раскол, что у identity_chart_default_premise_test.go:
// архивы чартов не отслеживаются git, значит условие проверки создаёт не всякое
// задание, а только то, что чарты материализует. Сам раскол удерживают
// TestChartPremiseIsActuallyInvoked и TestChartArchivesAreStillUntracked в
// нетегированной части — здесь они не дублируются.
package deploy_test

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// identityProviderArchiveGlob — архив чарта поставщика. Версия НЕ выписана:
// подъём версии не должен требовать правки этой проверки, а вот исчезновение
// архива обязано быть отказом, а не тишиной.
const identityProviderArchiveGlob = "kratos-*.tgz"

// chartArchiveMember достаёт из архива чарта файл, чей путь оканчивается на
// suffix. Возвращает содержимое и полное имя члена архива.
func chartArchiveMember(t *testing.T, archive, suffix string) (string, string) {
	t.Helper()
	f, err := os.Open(archive)
	if err != nil {
		t.Fatalf("архив чарта %s не читается (%v) — предпосылка проверки исчезла, "+
			"а не раздача настроек стала симметричной. Файл собирается под тегом "+
			"`helmcharts`, то есть его зовут ТОЛЬКО после `helm dependency build`", archive, err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("архив %s не распаковывается: %v", archive, err)
	}
	defer gz.Close()

	var seen []string
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("архив %s не читается: %v", archive, err)
		}
		name := filepath.ToSlash(h.Name)
		seen = append(seen, name)
		if !strings.HasSuffix(name, suffix) {
			continue
		}
		raw, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("%s из %s не читается: %v", name, archive, err)
		}
		return string(raw), name
	}
	sort.Strings(seen)
	t.Fatalf("в архиве %s нет файла, оканчивающегося на %q — форма чарта сменилась; "+
		"осмотрено членов архива %d", archive, suffix, len(seen))
	return "", ""
}

func identityProviderArchive(t *testing.T) string {
	t.Helper()
	m, err := filepath.Glob(filepath.Join(umbrellaDir, "charts", identityProviderArchiveGlob))
	if err != nil {
		t.Fatalf("поиск архива поставщика: %v", err)
	}
	if len(m) != 1 {
		t.Fatalf("архивов, отвечающих %q, найдено %d (%v) — предпосылка неопределима: "+
			"проверка обязана судить об ОДНОМ чарте", identityProviderArchiveGlob, len(m), m)
	}
	return m[0]
}

// TestCourierArgsAreNotInheritedFromTheDeployment — ядро предпосылки.
//
// Утверждаются ОБЕ стороны несимметричности, потому что гейт опирается на обе:
// аргументы почтовому процессу приходят СВОЕЙ ручкой, а тома — чужой.
func TestCourierArgsAreNotInheritedFromTheDeployment(t *testing.T) {
	archive := identityProviderArchive(t)
	body, member := chartArchiveMember(t, archive, "statefulset-mail.yaml")

	lines := strings.Split(body, "\n")
	t.Logf("осмотрено: архив %s, член %s, строк %d", archive, member, len(lines))

	// ── аргументы: своя ручка есть, чужой нет ──────────────────────────────
	argsOwn := regexp.MustCompile(`\.Values\.statefulSet\.extraArgs`)
	argsInherited := regexp.MustCompile(`\.Values\.deployment\.extraArgs`)

	if !argsOwn.MatchString(body) {
		t.Errorf("%s: шаблон почтового процесса не читает `statefulSet.extraArgs` — "+
			"требование соседнего гейта дублировать `--config` во вторую ручку стало "+
			"невыполнимым: ручки, в которую он велит писать, больше нет. Либо гейт "+
			"переписывается под новую раздачу, либо снимается вместе с этой предпосылкой",
			member)
	}
	if argsInherited.MatchString(body) {
		t.Errorf("%s: шаблон почтового процесса СТАЛ читать `deployment.extraArgs` — "+
			"несимметричности больше нет, и требование соседнего гейта дублировать "+
			"`--config` превратилось в лишнюю работу. Снимите требование, а не эту проверку",
			member)
	}

	// ── тома и соседи: наследование от основного процесса ──────────────────
	//
	// Ключи ВЫВОДЯТСЯ из самого шаблона, а не выписаны: чарт вправе завести
	// следующий наследуемый ключ, и он обязан попасть под перепись сам.
	ternary := regexp.MustCompile(
		`ternary\s+\.Values\.statefulSet\.([A-Za-z0-9_]+)\s+\.Values\.deployment\.([A-Za-z0-9_]+)`)
	inherited := map[string]bool{}
	for _, m := range ternary.FindAllStringSubmatch(body, -1) {
		if m[1] == m[2] {
			inherited[m[1]] = true
		}
	}
	names := make([]string, 0, len(inherited))
	for k := range inherited {
		names = append(names, k)
	}
	sort.Strings(names)
	t.Logf("осмотрено: ключей, наследуемых почтовым процессом от основного, %d: %v",
		len(names), names)

	if len(names) == 0 {
		t.Fatalf("%s: наследуемых ключей не найдено вовсе — либо форма шаблона сменилась, "+
			"либо предикат перестал её читать. «Ноль находок» здесь неотличимо от "+
			"«ноль прочитанного», поэтому это отказ, а не тишина", member)
	}

	// Ровно те ключи, на чтении которых стоит соседний гейт. Ключ, выпавший из
	// наследования, делает его перепись томов ложной.
	for _, need := range []string{
		"extraVolumes", "extraVolumeMounts", "extraContainers", "extraInitContainers",
	} {
		if !inherited[need] {
			t.Errorf("%s: `%s` больше не наследуется почтовым процессом от основного — "+
				"соседний гейт считает тома, доезжающие до почтового процесса, по этому "+
				"правилу, и его перепись стала ложной", member, need)
		}
	}

	// ── путь файла настроек по умолчанию — тот же у обоих процессов ────────
	//
	// Он и есть причина, по которой профиль, не объявивший НИЧЕГО, приходит к
	// соседнему гейту законным: оба процесса читают один файл.
	serveBody, serveMember := chartArchiveMember(t, archive, "deployment-kratos.yaml")
	defaultConfig := regexp.MustCompile(`(?m)^\s*-\s*/etc/config/kratos\.yaml\s*$`)
	if !defaultConfig.MatchString(body) || !defaultConfig.MatchString(serveBody) {
		t.Errorf("умолчание `--config` разошлось между %s и %s: сосед считает, что "+
			"профиль, не объявивший ничего, отдаёт обоим процессам ОДИН файл, — "+
			"и это перестало быть верным", member, serveMember)
	}
}
