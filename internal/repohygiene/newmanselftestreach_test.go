// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// newmanselftestreach_test.go — ГЕЙТ СУИТЫ, КОТОРЫЙ НИКТО НЕ ЗОВЁТ, НЕОТЛИЧИМ
// ОТ ОТСУТСТВУЮЩЕГО.
//
// # Предмет
//
// Самопроверка честности утверждений суиты (`selftest-assertions.js`) находит
// ровно те классы, которых прогон суиты не находит by construction: кейс, для
// которого успех и отказ неразличимы; опрос операции чужим субъектом; обёртка
// ожидания без ведомости. Стенда она не требует и стоит секунды.
//
// Пока её никто не запускает, эти классы въезжают в ствол молча, а зелёный
// прогон суиты читается как вердикт о свойстве, которое никто не спрашивал.
// Форма беды особенно тиха: скрипт в дереве ЕСТЬ, цель сборки объявлена, при
// желании он даже проходит — и всё это не значит ничего, потому что желания
// никто не изъявляет.
//
// # Почему предполётно из прогонщика суиты, а не шагом конвейера
//
// Решение принято здесь и записано, чтобы следующий сервис не выбирал заново
// (три сервиса решали этот вопрос тремя способами, и различие никем не
// решалось, #1427). Прогонщик суиты — единственное место, через которое
// проходит ВСЯКИЙ, кто гоняет суиту: конвейер, стенд разработчика, отладка
// одной коллекции. Шаг конвейера покрыл бы только первого, а самопроверка
// нужнее всего именно локально — до того, как ранер потрачен.
//
// Порядок «предполётно» тоже не косметика: самопроверка судит СГЕНЕРИРОВАННЫЕ
// коллекции, поэтому её отказ означает «прогон не вынесет вердикта о продукте»
// и обязан наступить ДО того, как поднят стенд и потрачены минуты.
//
// # Чего гейт НЕ делает
//
// Он не судит СОДЕРЖИМОЕ предикатов самопроверки: их правит своя линия. Здесь —
// только достижимость. И он не требует, чтобы самопроверка была у каждого
// сервиса: заводить её решает владелец суиты, а гейт связывает лишь того, кто
// уже завёл, — иначе он предписывал бы работу, а не стерёг свойство.
package repohygiene

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Имя самопроверки и имя прогонщика — соглашение, а не совпадение: оба
// выводятся из дерева обходом индекса, поэтому новый сервис попадает под гейт
// сам, без правки перечня.
const (
	newmanSelftestBase = "selftest-assertions.js"
	newmanRunnerBase   = "run.sh"
)

// Вызов самопроверки из прогонщика. Признак — ИСПОЛНЯЕМЫЙ вызов node по этому
// файлу, а не упоминание имени: имя стоит и в объясняющем комментарии рядом с
// вызовом, и гейт по подстроке остался бы зелёным на снятом вызове, покраснев
// на собственном объяснении (`testing.md` §«Гейт на класс», п.4).
var reNewmanSelftestCall = regexp.MustCompile(
	`(?m)^[^#\n]*\bnode\b[^#\n]*` + regexp.QuoteMeta(newmanSelftestBase))

// newmanSelftestSuite — одна суита: где лежит самопроверка, есть ли прогонщик и
// зовёт ли он её.
type newmanSelftestSuite struct {
	Owner      string // `services/nlb`, `gateway` — владелец суиты
	RunnerPath string // путь прогонщика относительно корня; пусто — прогонщика нет
	Calls      int    // вызовов самопроверки из прогонщика
}

// newmanSelftestExempt — ведомость: суита, чья самопроверка намеренно НЕ зовётся
// прогонщиком, с причиной. Ключ — владелец суиты.
//
// Ведомость ИСТЕКАЕТ САМА: запись, которой больше нечего прощать (суита исчезла
// либо вызов появился), — находка, а не безобидный остаток. Слепую зону
// наследует следующий, кто её не заводил.
// ВЕДОМОСТЬ ПУСТА, И ЭТО ЦЕЛЬ, А НЕ ПОЛОМКА. Здесь стояла одна запись — registry,
// чья самопроверка была красна 21 находкой (#1453/#1519). Все двадцать одна имели
// ОДИН источник и не были находками о суите: подделка `pm` не умела
// `pm.execution.skipRequest` — санкционированной формы «утвердить, назвав переменную,
// и не отправлять», — и роняла такой шаг исключением ещё в пред-скрипте; двадцать
// первая не знала встроенной величины песочницы `CryptoJS`. То есть гейт объявлял
// находкой ровно ту конструкцию, которой требует сам. Подделку научили обеим (с парной
// пробой: пропуск обязан ДЕЙСТВОВАТЬ, иначе немой пропуск перестал бы находиться), и
// вызов встал в прогонщик registry.
//
// Гейт на пустой ведомости ПРОХОДИТ: пустая ведомость есть достигнутая цель, а проба,
// падающая на достижении своей цели, толкает держать запись ради зелёного
// (`testing.md`). Способность гейта упасть доказывает не наличие записи, а
// adjudicate-проба на синтетике.
var newmanSelftestExempt = map[string]string{}

// adjudicateNewmanSelftestReach — суждение, отделённое от чтения дерева: иначе
// способность гейта упасть доказывалась бы только порчей рабочей копии.
func adjudicateNewmanSelftestReach(suites []newmanSelftestSuite, exempt map[string]string) []string {
	var out []string

	seen := make(map[string]bool, len(suites))
	for _, s := range suites {
		seen[s.Owner] = true

		if reason, ok := exempt[s.Owner]; ok {
			// Послабление законно ровно пока у него есть предмет: суита, чей
			// вызов появился, из ведомости выводится тем же заходом.
			if s.Calls > 0 {
				out = append(out, s.Owner+": запись ведомости потеряла предмет — "+
					"прогонщик зовёт самопроверку "+itoaCalls(s.Calls)+", а ведомость "+
					"прощает ей отсутствие вызова ("+reason+").\n"+
					"    Снимите запись: послабление без предмета — это слепая зона, "+
					"выданная вперёд, и следующая настоящая находка уедет под неё.")
			}
			continue
		}

		if s.RunnerPath == "" {
			out = append(out, s.Owner+": самопроверка утверждений есть, а прогонщика "+
				"суиты ("+newmanRunnerBase+") рядом нет — звать её неоткуда.\n"+
				"    Гейт без вызывающего неотличим от отсутствующего: он не краснеет "+
				"не потому, что дерево исправно, а потому, что его не спрашивали.")
			continue
		}

		if s.Calls == 0 {
			out = append(out, s.Owner+": "+s.RunnerPath+" НЕ зовёт "+newmanSelftestBase+".\n"+
				"    Самопроверка находит классы, которых прогон суиты не находит by "+
				"construction (кейс, для которого успех и отказ неразличимы; опрос "+
				"операции чужим субъектом; обёртка ожидания без ведомости) — пока её "+
				"не зовут, они въезжают в ствол молча, а зелёный прогон суиты читается "+
				"как вердикт о свойстве, которого никто не спрашивал.\n"+
				"    Форма вызова — предполётная, до подъёма стенда: самопроверка судит "+
				"сгенерированные коллекции, и её отказ означает «прогон не вынесет "+
				"вердикта о продукте». Образец — services/geo/tests/newman/scripts/"+
				newmanRunnerBase+".\n"+
				"    Намеренное отступление заводится записью в newmanSelftestExempt "+
				"с причиной — но молчаливого отступления не бывает.")
		}
	}

	// Зеркало ведомости: запись о суите, которой в дереве нет вовсе.
	var stale []string
	for owner := range exempt {
		if !seen[owner] {
			stale = append(stale, owner)
		}
	}
	sort.Strings(stale)
	for _, owner := range stale {
		out = append(out, owner+": запись ведомости потеряла предмет — самопроверки "+
			"по этому владельцу в дереве нет вовсе. Снимите запись.")
	}
	return out
}

func itoaCalls(n int) string {
	if n == 1 {
		return "один раз"
	}
	return "несколько раз"
}

// collectNewmanSelftestSuites — чтение дерева: индекс git, а не диск, потому что
// именно индекс увидит конвейер на свежем checkout'е.
func collectNewmanSelftestSuites(t *testing.T, root string) []newmanSelftestSuite {
	t.Helper()

	var suites []newmanSelftestSuite
	for _, line := range gitLsFiles(t, root) {
		tab := strings.IndexByte(line, '\t')
		if tab < 0 {
			continue
		}
		rel := line[tab+1:]
		if filepath.Base(rel) != newmanSelftestBase {
			continue
		}
		if isProbeFixture(rel) {
			continue
		}
		scripts := filepath.Dir(rel) // <владелец>/tests/newman/scripts
		owner := strings.TrimSuffix(scripts, "/tests/newman/scripts")
		if owner == scripts { // раскладка иная — предмета гейта здесь нет
			continue
		}

		s := newmanSelftestSuite{Owner: owner}
		runner := filepath.Join(scripts, newmanRunnerBase)
		if body, err := os.ReadFile(filepath.Join(root, runner)); err == nil {
			s.RunnerPath = runner
			s.Calls = len(reNewmanSelftestCall.FindAllString(string(body), -1))
		}
		suites = append(suites, s)
	}
	sort.Slice(suites, func(i, j int) bool { return suites[i].Owner < suites[j].Owner })
	return suites
}

func TestNewmanSelftestIsReachableFromItsRunner(t *testing.T) {
	root := repoRoot(t)
	suites := collectNewmanSelftestSuites(t, root)

	var names []string
	called := 0
	for _, s := range suites {
		mark := "—"
		if s.Calls > 0 {
			mark = "зовётся"
			called++
		} else if s.RunnerPath == "" {
			mark = "прогонщика нет"
		}
		names = append(names, s.Owner+" ("+mark+")")
	}
	var suppressed []string
	for owner := range newmanSelftestExempt {
		suppressed = append(suppressed, owner)
	}
	sort.Strings(suppressed)
	suppressedText := "нет"
	if len(suppressed) > 0 {
		suppressedText = strings.Join(suppressed, ", ")
	}
	t.Logf("ОБЪЁМ ОСМОТРЕННОГО: самопроверок в индексе %d, из них достижимы из своего "+
		"прогонщика %d; подавлено ведомостью %d (%s). Перечень: %s",
		len(suites), called, len(newmanSelftestExempt), suppressedText,
		strings.Join(names, ", "))

	// ПУСТОЙ ОБХОД — ОТКАЗ, А НЕ УСПЕХ. «Ноль находок» обязано быть отличимо от
	// «ноль прочитанного»: предикат, разошедшийся с раскладкой дерева, объявил бы
	// свойство выполненным, не осмотрев ничего.
	if len(suites) == 0 {
		t.Fatal("в индексе не найдено ни одной самопроверки " + newmanSelftestBase +
			" — обход пуст, и вердикт был бы о ничём. Либо предикат разошёлся с " +
			"раскладкой дерева, либо самопроверки сняты все разом; и то и другое " +
			"требует решения, а не молчаливого зелёного")
	}

	for _, finding := range adjudicateNewmanSelftestReach(suites, newmanSelftestExempt) {
		t.Error(finding)
	}
}
