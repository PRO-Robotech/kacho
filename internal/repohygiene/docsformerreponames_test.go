// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// docsformerreponames_test.go — прежнее имя репозитория в документе всегда стоит
// рядом с нынешней координатой кода.
//
// # Предмет
//
// `kacho-proto` и `kacho-corelib` — имена ПРЕЖНИХ полирепозиториев. Сегодня это
// каталоги `proto/` и `pkg/` одного дерева. Страницы сервисов продолжали писать
// их в настоящем времени — «источник истины в kacho-proto», «переиспользуемое
// приходит из kacho-corelib», «id-префикс — kacho-corelib/validate», — и читатель,
// ищущий контракт или общий пакет, уходил в репозиторий, где разработка не
// ведётся с середины июля.
//
// Устаревание такой прозы молчит by construction: у неё нет ни конфликта при
// слиянии, ни красного в сборке — сборка сайта ловит битые ссылки, а не ложные
// утверждения о том, где лежит код.
//
// # Предикат: координата рядом, а не время глагола
//
// Гейт НЕ судит время глагола. Судить его пришлось бы словарём, а корпус
// двуязычный (проза русская, координаты английские) — такой предикат
// недобирает молча и мерит язык, а не предмет.
//
// Судится другое, и это норма, уже выбранная деревом: **называешь прежнее имя —
// назови нынешнее место**. Эталон оставлен намеренно при закрытии #1448:
//
//	`pkg/` монорепо — `ids`, `operations`, … (прежде отдельный репозиторий `kacho-corelib`).
//	`proto/` + сгенерённые стабы `pkg/api/...` (прежде `kacho-proto`).
//
// Тогда читатель приземляется верно при ЛЮБОМ времени глагола, а история
// остаётся историей. Находка — вхождение, рядом с которым нынешней координаты
// нет.
//
// # Почему только эти два имени
//
// Ни одно из них не бывает ничем, кроме имени репозитория, — проверено:
// каталога `services/kacho-proto` нет, чарта с таким `name:` нет, SPIFFE-личности
// с ним нет. Поэтому у них ровно два состояния: история с координатой либо
// находка.
//
// `kacho-api-gateway` и `kacho-deploy` сюда НЕ включены намеренно: у первого есть
// живое рантайм-употребление (SPIFFE-личность круга доверенных отправителей),
// и слепая правка сломала бы документированный круг. Их адъюдикация — ручная.
//
// # Окно — АБЗАЦ, а не расстояние в строках
//
// Единица соседства здесь — абзац: максимальный непрерывный блок непустых строк.
// Прежде окном были строка вхождения и две соседние, и этого не хватило по
// причине, названной в самом же обосновании окна: проза жёстко переносится по
// ширине. Предложение «Прежняя редакция называла `kacho-proto` … и эти имена
// стали каталогами `proto/`, `pkg/`, `gateway/`» переносится НА ТРИ строки, и
// координата оказывается на третьей — вне окна ±1.
//
// Число в окне не увеличено с одного до двух намеренно: расстояние в строках
// неограниченно сверху (четырёхстрочный перенос потребовал бы ±3) и, что важнее,
// НЕУСТОЙЧИВО. Ответ такого предиката меняется от простого перепереноса абзаца,
// которого никто не замечает и никто не поддерживает, — то есть он измеряет
// перенос, а не соседство. Абзац переносом не меняется by construction.
//
// Цена расширения названа, а не умолчана: абзац шире окна ±1, и вхождение,
// закрытое координатой из дальнего конца длинного списка, гейт пропустит.
// Поэтому перепись печатает форму закрытия ОТДЕЛЬНО по каждой — своя строка ·
// соседняя · тот же абзац, — и рост слабейшей виден на каждом прогоне.
//
// # Перепись
//
// Печатается: документов осмотрено, вхождений каждого имени, сколько закрыто
// координатой на своей строке и сколько — соседней. Ноль вхождений — ОТКАЗ, а не
// успех: гейт, чей предмет отсутствие, молчит одинаково и когда предмета нет, и
// когда сломан обход. Условие самоистекающее: исчезнут прежние имена из
// документов вовсе — гейт попросит себя перечитать и снять вместе с предметом.
package repohygiene

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// formerRepoName — прежнее имя и координаты, которыми оно сегодня заменяется.
type formerRepoName struct {
	name    string
	current []string
}

var formerRepoNames = []formerRepoName{
	{name: "kacho-proto", current: []string{"proto/", "pkg/api"}},
	{name: "kacho-corelib", current: []string{"pkg/"}},
}

type formerRepoFinding struct {
	doc  string
	line int
	name string
	text string
}

func (f formerRepoFinding) String() string {
	t := strings.TrimSpace(f.text)
	if len(t) > 96 {
		t = t[:96] + "…"
	}
	return fmt.Sprintf("%s:%d — %s: %s", f.doc, f.line, f.name, t)
}

type formerRepoCensus struct {
	docs      int
	lines     int
	hits      map[string]int
	sameLine  map[string]int
	neighbour map[string]int
	samePara  map[string]int
}

func (c formerRepoCensus) String() string {
	var parts []string
	for _, f := range formerRepoNames {
		parts = append(parts, fmt.Sprintf("%s: вхождений %d (координата на своей строке %d, соседней %d, тем же абзацем %d)",
			f.name, c.hits[f.name], c.sameLine[f.name], c.neighbour[f.name], c.samePara[f.name]))
	}
	return fmt.Sprintf("документов %d, строк %d; %s", c.docs, c.lines, strings.Join(parts, "; "))
}

// maskFormerNames убирает из строки сами прежние имена, чтобы `proto/` внутри
// `kacho-proto/...` не засчитывался за нынешнюю координату. Без этого документ
// закрывал бы предикат собственным дефектом.
func maskFormerNames(line string) string {
	for _, f := range formerRepoNames {
		line = strings.ReplaceAll(line, f.name, "«прежнее-имя»")
	}
	return line
}

func namesCurrentCoordinate(line string, current []string) bool {
	masked := maskFormerNames(line)
	for _, c := range current {
		if strings.Contains(masked, c) {
			return true
		}
	}
	return false
}

// paragraphBounds — границы абзаца, которому принадлежит строка i.
//
// Абзац — максимальный непрерывный блок непустых строк. Это единица ПРОЗЫ, и
// именно она устойчива к переносу по ширине: перенос меняет, на какой строке
// окажется координата, и НЕ меняет, в каком абзаце она стоит.
func paragraphBounds(lines []string, i int) (from, to int) {
	from = i
	for from > 0 && strings.TrimSpace(lines[from-1]) != "" {
		from--
	}
	to = i
	for to+1 < len(lines) && strings.TrimSpace(lines[to+1]) != "" {
		to++
	}
	return from, to
}

// scanFormerRepoNames судит каждое вхождение прежнего имени против АБЗАЦА, в
// котором оно стоит, и различает три формы закрытия — своя строка, соседняя, тот
// же абзац. Различение не украшение: слабейшая форма самая широкая, и её рост
// обязан быть виден, а не растворён в общем «закрыто».
func scanFormerRepoNames(docs []string, read func(rel string) ([]byte, error)) ([]formerRepoFinding, formerRepoCensus, error) {
	census := formerRepoCensus{
		docs:      len(docs),
		hits:      map[string]int{},
		sameLine:  map[string]int{},
		neighbour: map[string]int{},
		samePara:  map[string]int{},
	}
	var findings []formerRepoFinding
	for _, rel := range docs {
		body, err := read(rel)
		if err != nil {
			return nil, census, fmt.Errorf("%s: %w — документ не прочитан, а непрочитанный "+
				"документ обязан быть отказом, а не молчаливым нулём", rel, err)
		}
		lines := strings.Split(string(body), "\n")
		census.lines += len(lines)
		for i, line := range lines {
			for _, f := range formerRepoNames {
				if !strings.Contains(line, f.name) {
					continue
				}
				census.hits[f.name]++
				if namesCurrentCoordinate(line, f.current) {
					census.sameLine[f.name]++
					continue
				}
				closed := false
				for _, j := range []int{i - 1, i + 1} {
					if j >= 0 && j < len(lines) && namesCurrentCoordinate(lines[j], f.current) {
						closed = true
						break
					}
				}
				if closed {
					census.neighbour[f.name]++
					continue
				}
				from, to := paragraphBounds(lines, i)
				for j := from; j <= to && !closed; j++ {
					if j != i && namesCurrentCoordinate(lines[j], f.current) {
						closed = true
					}
				}
				if closed {
					census.samePara[f.name]++
					continue
				}
				findings = append(findings, formerRepoFinding{rel, i + 1, f.name, line})
			}
		}
	}
	return findings, census, nil
}

// ── гейт на дереве ───────────────────────────────────────────────────────────

func TestFormerRepositoryNamesInDocsNameTheCurrentTree(t *testing.T) {
	root := repoRoot(t)
	docs := trackedProseDocs(t, root)

	osRoot, err := os.OpenRoot(root)
	if err != nil {
		t.Fatalf("открыть корень %s: %v", root, err)
	}
	defer func() { _ = osRoot.Close() }()

	findings, census, err := scanFormerRepoNames(docs, osRoot.ReadFile)
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	t.Logf("перепись: %s", census)

	total := 0
	for _, f := range formerRepoNames {
		total += census.hits[f.name]
	}
	if total == 0 {
		t.Fatalf("предпосылка не выполняется: в %d документах не найдено ни одного прежнего "+
			"имени репозитория. Либо их не осталось вовсе (тогда гейт снимается вместе с "+
			"предметом), либо сломан обход — и тогда корпус молча не читается, ровно тот "+
			"дефект, ради которого гейт заведён", census.docs)
	}

	if len(findings) > 0 {
		var b strings.Builder
		for _, f := range findings {
			b.WriteString("\n  " + f.String())
		}
		t.Fatalf("%d вхождений прежнего имени репозитория не называют нынешнюю координату:%s"+
			"\n\n`kacho-proto` — это каталог `proto/` (стабы `pkg/api/...`), `kacho-corelib` — "+
			"каталог `pkg/` ЭТОГО репозитория. Называешь прежнее имя — назови рядом нынешнее "+
			"место: тогда читатель приземляется верно при любом времени глагола.\nПерепись: %s",
			len(findings), b.String(), census)
	}
}

// trackedProseDocs — отслеживаемая ПРЕДПИСЫВАЮЩАЯ проза дерева.
//
// Из предмета выведен один вид документа — ДАТИРОВАННОЕ СВИДЕТЕЛЬСТВО О
// СДЕЛАННОМ: приёмка (docs/specs) и отчёт прогона (tests/**/RESULTS.md). Такой
// документ описывает состояние на свою дату и правкой обесценивается: он
// перестаёт свидетельствовать о том, что наблюдалось. Прежнее имя в нём —
// история по построению, и один из отчётов это прямо говорит («отдельного
// репозитория контрактов в дереве нет — имена не воспроизводятся»).
//
// Это ГРАНИЦА ПРЕДМЕТА, а не послабление: исключение не прощает ни одного
// живого утверждения о том, где лежит код. Признак, по которому его надо будет
// пересмотреть, — появление в датированном документе указания читателю ИДТИ
// куда-то; тогда документ перестал быть свидетельством и стал инструкцией.
func trackedProseDocs(t *testing.T, root string) []string {
	t.Helper()
	all, _ := trackedDocsAndFiles(t, root)
	var docs []string
	for _, p := range all {
		if strings.HasPrefix(p, "docs/specs/") || strings.HasSuffix(p, "/RESULTS.md") {
			continue
		}
		docs = append(docs, p)
	}
	if len(docs) == 0 {
		t.Fatal("отслеживаемой прозы ноль — гейту нечего осматривать")
	}
	return docs
}
