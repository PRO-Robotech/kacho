// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// quotaadmitkinds_test.go — репо-широкий гейт: вид, о котором СПРАШИВАЕТ
// совещательная полоса, обязан быть видом, который СПИСЫВАЕТ триггер.
//
// ЗАЧЕМ ОН СУЩЕСТВУЕТ. Полос учёта две: совещательная отвечает арендатору
// синхронно (`quota.Guard.Admit`, вид передаётся литералом из use-case'а), а
// авторитетная списывает место триггером (вид приезжает аргументом
// `TG_ARGV[0]`). Литералы написаны в РАЗНЫХ файлах и на разных языках, поэтому
// разъехаться они могут молча — и разъедутся не там, где это заметно.
//
// ЧТО ПРОИСХОДИТ ПРИ РАСХОЖДЕНИИ, ЕСЛИ ГЕЙТА НЕТ. Use-case, спрашивающий про
// вид, которого триггер не знает, получает «потолок не назван» ВСЕГДА: строки
// такого вида не заводит никто, потому что материализация заводит виды, которые
// назвал владелец величин, а он называет виды каталога. То есть опечатка в одном
// литерале выключает создание целого ресурса — наглухо, для всех арендаторов, и
// выглядит это как «платформа не назвала потолок», а не как опечатка.
//
// Обратное расхождение тише и хуже: вид, который триггер списывает, а полоса про
// него не спрашивает, теряет РАННИЙ отказ. Арендатор перестаёт получать 429 и
// узнаёт об исчерпании из отказа операции — поведение, которое приёмка называет
// негодным (§1.8), и которое ничем себя не выдаёт, потому что предел при этом
// продолжает соблюдаться.
//
// ЧЕГО ГЕЙТ НЕ ЛОВИТ, И ЭТО СКАЗАНО ЧЕСТНО. Он читает литералы в тексте; вид,
// собранный в рантайме, ему не виден. В этом дереве виды пишутся литералами
// именно потому, что они координаты, а не значения, — первое же отступление
// станет находкой для читателя.
package repohygiene

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// quotaTriggerKindsFile — миграция, объявляющая, на каких таблицах и под какими
// видами висят триггеры учёта. Координата выписана: вывести её из дерева можно
// только тем же поиском, который гейт и делает.
const quotaTriggerKindsFile = "services/vpc/internal/migrations/0040_project_resource_quotas.sql"

// quotaAdmitCallDir — дерево use-case'ов, чьи Create спрашивают совещательную
// полосу.
const quotaAdmitCallDir = "services/vpc/internal/apps/kacho/api"

var (
	// вид в объявлении триггера: EXECUTE FUNCTION kacho_quota_count('vpc.network' …)
	quotaTriggerKindRe = regexp.MustCompile(`kacho_quota_count\('([a-zA-Z0-9.]+)'`)
	// вид в вызове полосы: u.quota.Admit(ctx, string(x.ProjectID), "vpc.network")
	//
	// `.` вместо `[^)]`: аргумент проекта сам содержит скобки
	// (`string(n.ProjectID)`), и класс «всё кроме закрывающей» до вида не
	// дотягивается. Первая редакция была написана именно так — и проверка
	// предпосылки этого гейта её же и поймала, отказавшись судить пустоту.
	// Перенос строки `.` не покрывает, а вызов пишется одной строкой.
	quotaAdmitKindRe = regexp.MustCompile(`\.Admit\(ctx,.*"([a-zA-Z0-9.]+)"\)`)
)

func TestQuotaAdvisoryBandAsksAboutTheKindsTheTriggerCharges(t *testing.T) {
	root := repoRoot(t)

	// Сторона авторитетной полосы.
	migration, err := os.ReadFile(filepath.Join(root, quotaTriggerKindsFile))
	if err != nil {
		t.Fatalf("миграция учёта не прочитана (%s): %v", quotaTriggerKindsFile, err)
	}
	charged := map[string]bool{}
	for _, m := range quotaTriggerKindRe.FindAllStringSubmatch(string(migration), -1) {
		charged[m[1]] = true
	}
	// Предпосылка гейта проверяется ИМ САМИМ: ноль найденных видов означал бы, что
	// изменилась форма объявления триггеров, а не что расхождений нет.
	if len(charged) == 0 {
		t.Fatalf("предпосылка гейта не выполнена: в %s не найдено ни одного вида — "+
			"форма объявления триггеров учёта изменилась, и гейт судит пустоту",
			quotaTriggerKindsFile)
	}

	// Сторона совещательной полосы.
	asked := map[string][]string{}
	// Состав берётся у индекса дерева, а не обходом диска: под `services/` на
	// машине, где поднимали стенд, лежат распаковки чартов и отчёты прогонов, и
	// обход диска прочитал бы их. Требование держит гейт
	// `TestTreeWalkersAskTheIndex`.
	goFiles, err := treecorpus.UnderWithSuffix(filepath.Join(root, quotaAdmitCallDir), ".go")
	if err != nil {
		t.Fatalf("состав дерева под %s: %v", quotaAdmitCallDir, err)
	}
	filesSeen := 0
	for _, path := range goFiles {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			t.Fatalf("чтение %s: %v", path, rerr)
		}
		filesSeen++
		for _, m := range quotaAdmitKindRe.FindAllStringSubmatch(string(b), -1) {
			rel, _ := filepath.Rel(root, path)
			asked[m[1]] = append(asked[m[1]], filepath.ToSlash(rel))
		}
	}
	if len(asked) == 0 {
		t.Fatalf("предпосылка гейта не выполнена: в %s не найдено ни одного вызова полосы — "+
			"форма вызова изменилась, и гейт судит пустоту", quotaAdmitCallDir)
	}

	// Перепись осмотренного: «ноль находок» обязано быть отличимо от «ноль
	// прочитанного».
	t.Logf("осмотрено: файлов use-case'ов %d; видов списывается %d; видов спрашивается %d",
		filesSeen, len(charged), len(asked))

	var strayAsk []string
	for kind, files := range asked {
		if !charged[kind] {
			strayAsk = append(strayAsk, kind+" ← "+strings.Join(files, ", "))
		}
	}
	sort.Strings(strayAsk)
	if len(strayAsk) > 0 {
		t.Errorf("полоса спрашивает про вид, который триггер НЕ списывает (%d). "+
			"Создание такого ресурса отвергается «потолок не назван» ВСЕГДА: строк "+
			"этого вида не заводит никто:\n  %s", len(strayAsk), strings.Join(strayAsk, "\n  "))
	}

	var uncovered []string
	for kind := range charged {
		if _, ok := asked[kind]; !ok {
			uncovered = append(uncovered, kind)
		}
	}
	sort.Strings(uncovered)
	if len(uncovered) > 0 {
		t.Errorf("триггер списывает вид, о котором полоса НЕ спрашивает (%d). "+
			"Арендатор теряет ранний отказ: исчерпание приезжает отказом операции "+
			"вместо синхронного 429, и это ничем себя не выдаёт:\n  %s",
			len(uncovered), strings.Join(uncovered, "\n  "))
	}
}
