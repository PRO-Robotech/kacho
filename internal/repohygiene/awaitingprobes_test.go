// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

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

// awaitingprobes_test.go — гейт «проба, ждущая своего условия, истекает ВМЕСТЕ С
// НИМ».
//
// # Предмет
//
// Сквозная проба, чьё условие в дереве ещё не создано, не имеет права ни лежать
// в автоматическом наборе (её job входит в набор обязательных контекстов, и
// заведомо красная проба сделала бы красным каждый запрос на слияние), ни
// прятаться под пропуском (пропуск подаёт «не выполнилось» как вердикт).
//
// Предписанная форма — счётный поимённый долг в отдельном каталоге. У долга есть
// ровно одна опасность: он переживает своё условие. Появится владелец журнала —
// и пробы останутся лежать, никем не исполняемые, а перечень будет утверждать,
// что их «нельзя запустить», когда уже можно.
//
// # Как он истекает
//
// От ФАКТА В ДЕРЕВЕ, а не от чьей-то памяти, и условие у пробы состоит из ДВУХ
// половин: владельца журнала объявил хоть один профиль развёртывания И вид,
// который проба называет в оси `kinds`, кто-то из владельцев действительно
// пишет. Обе — и только тогда каталог обязан опустеть.
//
// # Почему половин две, а была одна
//
// Прежняя редакция сторожила только профиль, и это было верно ровно пока вторая
// половина не наблюдалась: пробы просили вид `compute.placement_group`, которого
// не пишет НИ ОДИН владелец (в словаре страницы его нет ни в этом написании, ни
// в каноническом). Поставка (kacho#1388) закрыла первую половину — и гейт
// потребовал перенести в исполняемый набор пробу, которая там была бы КРАСНОЙ по
// причине, дефектом продукта не являющейся. То есть истечение по одной половине
// давало ровно тот исход, ради предотвращения которого долг и заведён; об этом
// прямо сказано абзацем ниже, где выбиралась сторожимая половина.
//
// Половина вторая читается у клиентской страницы — того самого объявления, по
// которому вид выбирает и автор пробы. Страница связана с кодом в обе стороны
// (`subscriptionkindvocabulary.go`: вид владельца, не названный страницей, и вид
// страницы, не объявленный владельцем, — обе находки), поэтому второе написание
// словаря здесь не заводится.
//
// # Почему проверка предпосылки стоит здесь
//
// Гейт держится на двух фактах о дереве: каталог ожидания существует, и
// объявление владельцев читается из чартов. Исчезнет первое — гейту нечего
// охранять; исчезнет второе — он зелен всегда. Оба заявляются переписью.

// awaitingProbesDir — каталог проб, ждущих своего условия.
const awaitingProbesDir = "ui-future/e2e/specs-awaiting-journal-owner"

// sweptProbesDir — каталог, который исполняет прогонщик.
const sweptProbesDir = "ui-future/e2e/specs"

// subscriptionClientPage — клиентская страница подписки, чья таблица словаря
// называет виды, которые владельцы ПИШУТ.
//
// Та же координата стоит во входе гейта словаря видов
// (`subscriptionkindvocabulary_test.go`, поле `ClientPage`); свести оба
// объявления в одно — отдельный предмет, здесь он не решается, чтобы правка не
// пересеклась с полосами, которые тот гейт сейчас правят.
const subscriptionClientPage = "gateway/docs/content/api/subscription.mdx"

// ownersDeclarationRe — объявление владельцев журнала в профиле развёртывания.
//
// Пустое значение (`owners: ""`) означает «владелец не объявлен» и условием НЕ
// является: ровно так объявление и выглядит, пока предмета нет.
var ownersDeclarationRe = regexp.MustCompile(`(?m)^\s*owners:\s*(.*)$`)

// verifiesLinkRe — ссылка пробы на задачу, которая заведёт её условие.
var verifiesLinkRe = regexp.MustCompile(`//\s*verifies\s+#\d+`)

// probeKindsRe — виды, которые проба называет в оси `kinds` адреса потока.
//
// Ось перечислима через запятую, поэтому берётся всё значение целиком и режется
// после. Точка в наборе знаков намеренна: она НЕ каноничное написание вида, и
// именно её присутствие сделало пробу неисполнимой — распознаватель, знающий
// только каноническую форму, объявил бы «видов не названо» и молчал бы.
var probeKindsRe = regexp.MustCompile(`kinds=([A-Za-z0-9_.,]+)`)

// kindsNamedByProbe — виды, которые проба просит у потока.
func kindsNamedByProbe(body string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, 2)
	for _, m := range probeKindsRe.FindAllStringSubmatch(body, -1) {
		for _, kind := range strings.Split(m[1], ",") {
			kind = strings.TrimSpace(kind)
			if kind == "" || seen[kind] {
				continue
			}
			seen[kind] = true
			out = append(out, kind)
		}
	}
	sort.Strings(out)
	return out
}

// TestProbesAwaitingTheirConditionExpireWhenItArrives — долг истекает от факта.
func TestProbesAwaitingTheirConditionExpireWhenItArrives(t *testing.T) {
	root := repoRoot(t)

	// ── половина первая: объявлен ли владелец журнала хоть где-нибудь ────────
	chartsRead := 0
	declared := map[string]string{}
	// Состав берётся у indexed-корпуса, а не обходом диска: под gateway/deploy на
	// машине, где поднимали стенд, лежат распаковки чартов и отчёты прогонов, и
	// обход по диску прочитал бы их как объявления профиля. Требование дерева —
	// TestTreeWalkersAskTheIndex; перечень исключений закрыт для пополнения.
	profiles, walkErr := treecorpus.UnderWithSuffix(filepath.Join(root, "gateway", "deploy"), ".yaml")
	if walkErr != nil {
		t.Fatalf("состав профилей развёртывания у корпуса дерева: %v", walkErr)
	}
	for _, path := range profiles {
		body, readErr := os.ReadFile(path) // #nosec G304 -- путь из индекса собственного дерева
		if readErr != nil {
			t.Fatalf("чтение профиля %s: %v", path, readErr)
		}
		text := string(body)
		if !strings.Contains(text, "subscriptionStream:") {
			continue
		}
		chartsRead++
		if m := ownersDeclarationRe.FindStringSubmatch(text); m != nil {
			value := strings.Trim(strings.TrimSpace(m[1]), `"'`)
			if value != "" {
				rel, _ := filepath.Rel(root, path)
				declared[rel] = value
			}
		}
	}

	// ── половина вторая: что лежит в каталоге ожидания ───────────────────────
	awaiting := make([]string, 0, 2)
	withoutLink := make([]string, 0, 2)
	dir := filepath.Join(root, awaitingProbesDir)
	entries, dirErr := os.ReadDir(dir)
	if dirErr != nil {
		t.Fatalf("каталог ожидания %s не читается (%v) — гейту нечего охранять, "+
			"и его молчание не означало бы отсутствия долга", awaitingProbesDir, dirErr)
	}
	// ── половина вторая-бис: какие виды кто-то из владельцев ПИШЕТ ───────────
	//
	// Источник — клиентская страница, то самое объявление, по которому вид
	// выбирает автор пробы; с кодом она связана в обе стороны отдельным гейтом,
	// поэтому второго написания словаря здесь не заводится.
	pagePath := filepath.Join(root, filepath.FromSlash(subscriptionClientPage))
	pageBody, pageErr := os.ReadFile(pagePath) // #nosec G304 -- обход собственного дерева
	if pageErr != nil {
		t.Fatalf("клиентская страница %s не читается (%v) — вторую половину условия "+
			"проверить нечем, и молчание гейта означало бы «не читали»",
			subscriptionClientPage, pageErr)
	}
	// Пустой словарь сюда не доходит by construction: разбор сам отвергает
	// таблицу, из которой не считано ни одного вида. Поэтому отдельной ветки «видов
	// ноль» здесь нет — она была бы недостижимой и лишь документировала бы то,
	// чего код не производит.
	servedKinds, kindsErr := kindsNamedByPage(string(pageBody), subscriptionClientPage)
	if kindsErr != nil {
		t.Fatalf("словарь видов страницы %s: %v — вторую половину условия судить нечем, "+
			"и всякая проба считалась бы ждущей навсегда", subscriptionClientPage, kindsErr)
	}

	bodies := map[string]string{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".spec.ts") {
			continue
		}
		awaiting = append(awaiting, e.Name())
		body, readErr := os.ReadFile(filepath.Join(dir, e.Name())) // #nosec G304
		if readErr != nil {
			t.Fatalf("чтение %s: %v", e.Name(), readErr)
		}
		bodies[e.Name()] = string(body)
		if !verifiesLinkRe.Match(body) {
			withoutLink = append(withoutLink, e.Name())
		}
	}

	verdict := judgeAwaitingProbes(bodies, len(declared) > 0, servedKinds)
	expired, unserved, unrecognized := verdict.expired, verdict.unserved, verdict.unrecognized

	t.Logf("перепись: профилей с объявлением подписки %d · из них назвали владельца %d %v · "+
		"проб в ожидании %d %v · видов пишут владельцы %d · проб с неписанным видом %d %v · "+
		"проб, чьё условие создано целиком %d %v",
		chartsRead, len(declared), declared, len(awaiting), awaiting,
		len(servedKinds), len(unserved), unserved, len(expired), expired)

	if chartsRead == 0 {
		t.Fatal("ни один профиль не объявляет подписку — гейт ничего не читал, " +
			"и его зелёное неотличимо от пустого обхода")
	}
	for _, name := range unrecognized {
		t.Errorf("в пробе %s не разобрано ни одного вида (ось `kinds` адреса потока): "+
			"предикат перестал узнавать её форму, и «условие не создано» стало бы "+
			"свойством распознавателя, а не дерева", name)
	}

	if len(expired) > 0 {
		t.Errorf("владелец журнала ОБЪЯВЛЕН (%v) и вид, который просят пробы %v, кто-то "+
			"пишет.\nУсловие создано ЦЕЛИКОМ — долг истёк: перенеси их в %s, иначе перечень "+
			"утверждает «нельзя запустить» там, где уже можно, и никто этого не заметит",
			declared, expired, sweptProbesDir)
	}

	if len(withoutLink) > 0 {
		t.Errorf("пробы в ожидании без ссылки на задачу, которая заведёт их условие: %v.\n"+
			"Долг без предмета неотличим от брошенной работы: снять его будет некому", withoutLink)
	}
}

// awaitingVerdict — то, что гейт вычисляет по каталогу ожидания.
type awaitingVerdict struct {
	// expired — пробы, чьё условие создано ЦЕЛИКОМ: им место в исполняемом наборе.
	expired []string
	// unserved — проба → виды, которых не пишет ни один владелец.
	unserved map[string][]string
	// unrecognized — пробы, у которых не разобрано ни одного вида.
	unrecognized []string
}

// judgeAwaitingProbes — ЕДИНСТВЕННОЕ место, где выносится суждение «долг истёк».
//
// Вынесена отдельной функцией затем, чтобы доказательство способности гейта
// упасть прогоняло ЭТУ функцию, а не её пересказ: проверка, воспроизводящая
// суждение своей копией, доказывает свойство копии и остаётся зелёной, когда
// судящую ветку снимают. Класс найден приёмкой 2026-08-29 на гейте поставки.
func judgeAwaitingProbes(
	bodies map[string]string, ownerDeclared bool, servedKinds map[string]struct{},
) awaitingVerdict {
	names := make([]string, 0, len(bodies))
	for name := range bodies {
		names = append(names, name)
	}
	sort.Strings(names)

	out := awaitingVerdict{
		expired:      make([]string, 0, len(names)),
		unserved:     map[string][]string{},
		unrecognized: make([]string, 0, len(names)),
	}
	for _, name := range names {
		kinds := kindsNamedByProbe(bodies[name])
		if len(kinds) == 0 {
			// Ноль видов — это «предикат перестал узнавать форму», а не «проба
			// ничего не просит»: молчать здесь значит сузить гейт до нуля.
			out.unrecognized = append(out.unrecognized, name)
			continue
		}
		missing := make([]string, 0, len(kinds))
		for _, kind := range kinds {
			if _, ok := servedKinds[kind]; !ok {
				missing = append(missing, kind)
			}
		}
		if len(missing) > 0 {
			out.unserved[name] = missing
			continue
		}
		if ownerDeclared {
			out.expired = append(out.expired, name)
		}
	}
	return out
}
