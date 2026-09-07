// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// releasewiring_test.go — у операции «поставить версию» обязан быть ЗОВУЩИЙ.
//
// # Предмет, и чем он отличается от соседнего гейта
//
// `TestMonorepoCarriesAVersionPublisher` судит, что механизмы линии выпуска
// СУЩЕСТВУЮТ и что их доказательства падучести исполняются. Он был зелен и
// тогда, когда механизм публикации лежал в дереве, а звал его НОЛЬ объявлений —
// ни один процесс конвейера, ни один рецепт сборки. Замер, из которого гейт
// выведен: `git grep -l publish-version.sh -- .github Makefile` → 0.
//
// Механизм без зовущего не отличается от отсутствующего. Разница между «команда
// напечатана» и «команда выполнена» есть разница между «поставим версию, когда
// надо» и «версия ставится», а первое означает «никогда»: за операцией, которая
// делается по памяти, никто не отвечает.
//
// # Что здесь утверждается — четыре структурных факта
//
//  1. объявление производителя существует и разбирается;
//  2. у него НЕТ ни одного автоматического триггера. Это не оформление:
//     автоматический выпуск обещал бы совместимость, которой никто не проверял,
//     а расход ранеров в накопительном режиме оплачивается один раз, на MR в
//     ствол. Ручной запуск не задевает ни того, ни другого by construction;
//  3. необратимый шаг не может произойти случайно: объявлены и подтверждение
//     намерения, и отдельная ручка самого шага;
//  4. каждый механизм, названный в теле шагов, СУЩЕСТВУЕТ и исполняем, и среди
//     них есть производитель. Провязка в пустоту — та же форма без содержания:
//     шаг зовёт то, чего нет, и узнаётся это на первом же выпуске.
//
// # Чего гейт НЕ утверждает — названо, чтобы его не читали шире
//
// Он не судит, ВЕРНО ли производитель отказывает и создаёт ли ровно одну
// ссылку: это поведение, и его доказывает `scripts/release/publish-tag-inject.sh`
// прогоном настоящего механизма на синтетических репозиториях. Он не требует
// опубликованной версии: публикация — необратимое внешнее действие владельца, и
// гейт дерева, требующий её, был бы красным до нажатия, то есть блокировал бы
// всё остальное по причине, к дереву не относящейся.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// releaseProducerFile — объявление производителя. Координата ОДНА и выписана, а
// не выведена обходом каталога: обход был бы тождественно-истинным («что лежит,
// то и должно лежать») и промолчал бы на пустом каталоге — то есть ровно в том
// случае, ради которого гейт заведён.
const releaseProducerFile = ".github/workflows/release.yml"

// releaseProducerMechanism — механизм, который производитель обязан звать.
// Именно он делает необратимый шаг; прочие скрипты линии он зовёт сам.
const releaseProducerMechanism = "scripts/release/publish-tag.sh"

// releaseAutomaticTriggers — события, порождаемые площадкой без участия
// человека. Перечень закрытый: он и есть определение «автоматического».
var releaseAutomaticTriggers = []string{
	"push", "pull_request", "pull_request_target",
	"schedule", "workflow_run", "workflow_call", "repository_dispatch",
}

// releaseWiringAudit — исход осмотра. Возвращается СТРУКТУРОЙ, а не печатается
// на месте: инъекция обязана гонять тот же код, а не его пересказ.
type releaseWiringAudit struct {
	triggers   []string // объявленные события
	scripts    []string // механизмы, названные в теле шагов
	inputs     []string // объявленные входы ручного запуска
	filesRead  int
	assertions int
	findings   []string
}

// auditReleaseWiring — единственная реализация осмотра. Ею пользуются и гейт, и
// его инъекция; расхождение между ними невозможно by construction.
//
// root — корень дерева; producer — путь объявления относительно корня.
func auditReleaseWiring(root, producer string) releaseWiringAudit {
	var a releaseWiringAudit

	body, err := os.ReadFile(filepath.Join(root, producer))
	a.assertions++
	if err != nil {
		a.findings = append(a.findings,
			producer+": объявления производителя нет — у операции «поставить версию» нет зовущего")
		return a
	}
	a.filesRead++

	var doc yaml.Node
	a.assertions++
	if err := yaml.Unmarshal(body, &doc); err != nil {
		a.findings = append(a.findings, producer+": объявление не разбирается: "+err.Error())
		return a
	}

	// ── 1. Триггеры. Ключ `on` читается ТЕКСТОМ ключа: в YAML 1.1 он
	// разрешается как логическая истина, и обращение по строке к разобранному
	// отображению не нашло бы ничего — гейт молча считал бы, что триггеров нет
	// вовсе, то есть зеленел бы на объявлении с `push`.
	on := yamlMapValue(&doc, "on")
	a.triggers = yamlMapKeys(on)
	a.assertions++
	if len(a.triggers) == 0 {
		a.findings = append(a.findings, producer+": триггеров не объявлено — производитель не позвать ничем")
	}
	for _, t := range a.triggers {
		a.assertions++
		for _, auto := range releaseAutomaticTriggers {
			if t == auto {
				a.findings = append(a.findings,
					producer+": объявлен автоматический триггер `"+t+
						"` — выпуск обещал бы совместимость, которой никто не проверял, а расход ранеров вырос бы")
			}
		}
	}
	a.assertions++
	if !containsString(a.triggers, "workflow_dispatch") {
		a.findings = append(a.findings, producer+": ручного запуска не объявлено — позвать производителя нечем")
	}

	// ── 2. Необратимый шаг не должен происходить случайно.
	a.inputs = yamlMapKeys(yamlMapValue(yamlMapValue(on, "workflow_dispatch"), "inputs"))
	for _, need := range []string{"version", "confirm", "publish"} {
		a.assertions++
		if !containsString(a.inputs, need) {
			a.findings = append(a.findings,
				producer+": нет входа `"+need+
					"` — необратимый шаг обязан требовать и подтверждения намерения, и отдельной ручки")
		}
	}

	// ── 3. Механизмы, названные в теле шагов, обязаны существовать и быть
	// исполняемыми. Читается ИСПОЛНЯЕМАЯ часть (`run:` узла шага), а не сырой
	// текст файла: имя скрипта встречается и в комментариях этого же
	// объявления, и поиск по подстроке зеленел бы на собственном объяснении.
	jobs := yamlMapValue(&doc, "jobs")
	if jobs != nil && jobs.Kind == yaml.MappingNode {
		for i := 1; i < len(jobs.Content); i += 2 {
			steps := yamlMapValue(jobs.Content[i], "steps")
			if steps == nil || steps.Kind != yaml.SequenceNode {
				continue
			}
			for _, st := range steps.Content {
				run := yamlMapValue(st, "run")
				if run == nil {
					continue
				}
				for _, s := range namedReleaseScripts(run.Value) {
					a.scripts = append(a.scripts, s)
				}
			}
		}
	}
	a.assertions++
	if len(a.scripts) == 0 {
		a.findings = append(a.findings,
			producer+": ни один шаг не зовёт механизма линии выпуска — объявление есть, производителя нет")
	}
	for _, s := range distinctStrings(a.scripts) {
		a.assertions++
		info, err := os.Stat(filepath.Join(root, s))
		switch {
		case err != nil:
			a.findings = append(a.findings, producer+": шаг зовёт "+s+", а его в дереве нет — провязка в пустоту")
		case info.Mode().Perm()&0o111 == 0:
			a.findings = append(a.findings, producer+": "+s+" не исполняем (режим "+info.Mode().Perm().String()+")")
		default:
			a.filesRead++
		}
	}
	a.assertions++
	if !containsString(a.scripts, releaseProducerMechanism) {
		a.findings = append(a.findings,
			producer+": ни один шаг не зовёт "+releaseProducerMechanism+
				" — необратимый шаг делает только он, значит операции нет")
	}
	return a
}

// namedReleaseScripts выбирает из тела шага пути механизмов линии выпуска.
//
// Разбор по ЛЕКСЕМАМ, а не поиск подстроки: тело шага бывает многострочным и
// несёт кавычки, переносы и продолжения. Побочная выгода — имя, приклеенное к
// соседнему слову, лексемой не станет и в перечень не попадёт.
func namedReleaseScripts(run string) []string {
	const prefix = "scripts/release/"
	var out []string
	for _, f := range strings.FieldsFunc(run, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '\'' || r == '"' ||
			r == ';' || r == '(' || r == ')' || r == '&' || r == '|'
	}) {
		if i := strings.Index(f, prefix); i >= 0 && strings.HasSuffix(f, ".sh") {
			out = append(out, f[i:])
		}
	}
	return out
}

func containsString(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}

// distinctStrings — повторы прочь, порядок первого появления сохранён.
//
// Соседний `uniqueStrings` (gatetargetwiring.go) НЕ ПОДХОДИТ, и это не вопрос
// имён: он снимает лишь СОСЕДНИЕ повторы и пишет в исходный срез (`in[:0]`),
// то есть портит перечень вызывающего. Здесь перечень нужен целым — он идёт и в
// перепись, и в проверку наличия механизма.
func distinctStrings(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func TestVersionPublishingHasACallerInThePipeline(t *testing.T) {
	a := auditReleaseWiring(repoRoot(t), releaseProducerFile)

	// Перепись печатается ВСЕГДА: «ноль находок» обязано быть отличимо от
	// «ноль прочитанного». Пустой обход — красное, а не тихий успех.
	t.Logf("перепись: файлов прочитано %d, триггеров %d (%v), входов %d, механизмов позвано %d (%v), утверждений %d, находок %d",
		a.filesRead, len(a.triggers), a.triggers, len(a.inputs),
		len(distinctStrings(a.scripts)), distinctStrings(a.scripts), a.assertions, len(a.findings))

	if a.assertions == 0 {
		t.Fatalf("обход пуст: утверждений %d — вердикт беспредметен", a.assertions)
	}
	for _, f := range a.findings {
		t.Errorf("производитель версии: %s", f)
	}
}
