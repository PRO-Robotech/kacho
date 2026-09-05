// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// trust_anchor_claim_matches_declaration_test.go — ПОСАДКА ДОВЕРИЯ ОБЪЯВЛЯЕТСЯ
// ОТМЕТКОЙ, И ОТМЕТКА ОБЯЗАНА БЫТЬ ОБЕСПЕЧЕНА МЕХАНИЗМОМ (#1753).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Боевой профиль утверждал уверенно и без пометки: «SSL_CERT_FILE ЗАМЕЩАЕТ
// системный набор корней целиком, поэтому исходящий TLS этого пода доверяет
// ТОЛЬКО внутреннему удостоверяющему». Это неверно, и ошибка направлена в
// сторону БОЛЬШЕГО доверия, чем считал автор: библиотека замещает список
// ФАЙЛОВ, но не список КАТАЛОГОВ, а корни лежат и там и там. Набор шёл
// 123 → 124 (якорь ДОБАВЛЕН), а не 123 → 1.
//
// Объявленное свойство изоляции не выполнялось, и узнать об этом было неоткуда:
// комментарий читается как гарантия. Это тот самый класс — «misleading comment
// про security = ловушка» (`security.md` §Hardening п.5): следующий «починит»
// код под неверный текст.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ИМЕННО УТВЕРЖДАЕТСЯ — ТРИ ОСИ, КАЖДАЯ САМОСТОЯТЕЛЬНА
//
// Единица разбора — СПИСОК ПЕРЕМЕННЫХ одного контейнера (`extraEnv:` / `env:`),
// взятый по отступам. Для каждого:
//
//	(A) ПОСАДКА ОБЪЯВЛЕНА. Список, пинящий якорь доверия (SSL_CERT_FILE или
//	    SSL_CERT_DIR), обязан нести РОВНО ОДНУ отметку. Ноль — посадка не
//	    объявлена вовсе; две разные — объявление спорит само с собой.
//	(B) ИСКЛЮЧИТЕЛЬНОСТЬ ОБЕСПЕЧЕНА. Отметка «ТОЛЬКО-ВНУТРЕННЕЕ» требует, чтобы
//	    пинены были ОБЕ переменные. С одной исключительность НЕВОЗМОЖНА
//	    by construction — второй список продолжает читаться.
//	(C) ОТМЕТКА НЕ ПЕРЕЖИВАЕТ СВОЙ ПРЕДМЕТ. Отметка в списке, который якорей
//	    больше не пинит, — находка: сняли переменные, снимите и объявление.
//
// Ось (C) обратна осям (A)/(B) ровно так же, как «самоистечение» обратно
// «покрытию» у соседей по каталогу — иначе ведомость посадок пережила бы свой
// предмет и стала бы слепой зоной, выданной вперёд.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ОТМЕТКА, А НЕ ПРОЗА
//
// Сплошной обход прозы здесь НЕГОДЕН, и это не осторожность, а замер: текст,
// который ОБЪЯСНЯЕТ дефект, неизбежно содержит и слово «замещает», и слова
// «только внутреннему удостоверяющему» — эта самая шапка содержит их обоих.
// Предикат по прозе краснел бы на собственном разборе.
//
// Поэтому посадка объявляется ОТМЕТКОЙ (`ДОВЕРИЕ: ДОПОЛНЯЕТ` /
// `ДОВЕРИЕ: ТОЛЬКО-ВНУТРЕННЕЕ`), а проза не читается вовсе. То же решение и по
// той же причине принято у гейта полос входа
// (identity_method_comment_matches_declaration_test.go) и у гейта имён заданий
// конвейера.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ЗДЕСЬ НЕ УТВЕРЖДАЕТСЯ
//
//   - НЕ утверждается, что посадка выбрана ВЕРНО. Вопрос гейта — «решал ли это
//     кто-нибудь и обеспечено ли решённое», а не «правильно ли решено». На
//     «правильно» машинного предиката нет, и объявлять его было бы формой без
//     содержания.
//   - НЕ утверждается обратное к (B): «ДОПОЛНЯЕТ при обеих пинённых» находкой НЕ
//     считается. Пинить обе переменные на СИСТЕМНЫЕ пути — законный способ
//     дополнить, и отличить его от исключительности можно лишь чтением путей,
//     то есть суждением. Самоистечение здесь несёт ось (C), у которой предикат
//     airtight.
//   - НЕ проверяется существование смонтированного файла: это свойство КЛАСТЕРА,
//     а гейт читает ОБЪЯВЛЕНИЯ. Ни helm, ни кластер не нужны, поэтому проверка
//     не умеет пропускаться.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДПОСЫЛКА, ПРОВЕРЯЕМАЯ ОТДЕЛЬНО
//
// Ось (B) опирается на факт о библиотеке, а не на вкус: одна переменная правит
// только свой список. Факт измеряется соседом —
// trust_anchor_lists_are_independent_test.go. Изменится факт — покраснеет он, а
// не этот гейт, и требование будет пересмотрено, а не тихо продолжено.
//
// ─────────────────────────────────────────────────────────────────────────────
// ГРАНИЦА ПОПУЛЯЦИИ НАЗВАНА ЧИСЛОМ, А НЕ УМОЛЧАНА
//
// Гейт судит `deploy/helm/**`. Якоря доверия пинятся и вне этого каталога —
// перепись печатает их число и координаты НА КАЖДОМ ПРОГОНЕ, поэтому сужение
// популяции остаётся наблюдаемым и не может быть забыто. Расширение популяции —
// отдельная работа: она правит чужой каталог сервиса.
package deploy_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/gitenv"
)

// trustAnchorVars — переменные, которыми пинится набор корней. Обе, и это
// существенно: ось (B) целиком про то, что их две.
var trustAnchorVars = map[string]bool{
	"SSL_CERT_FILE": true,
	"SSL_CERT_DIR":  true,
}

// trustPostureMarker — отметка посадки. Читается ТОЛЬКО из строки-комментария.
var trustPostureMarker = regexp.MustCompile(`#\s*ДОВЕРИЕ:\s*(ДОПОЛНЯЕТ|ТОЛЬКО-ВНУТРЕННЕЕ)\s*$`)

// trustEnvListStart — начало списка переменных контейнера.
// Флаг `(?m)` обязателен: без него `^`/`$` в Go означают границы ВСЕГО текста,
// а не строки, и подсчёт по телу файла давал бы ноль на непустом дереве.
// Проверка предпосылки этот дефект и поймала при заведении гейта.
var trustEnvListStart = regexp.MustCompile(`(?m)^([ ]*)(extraEnv|env):[ ]*$`)

// trustEnvEntry — объявление переменной внутри списка.
var trustEnvEntry = regexp.MustCompile(`^\s*-\s*name:\s*"?([A-Za-z_][A-Za-z0-9_]*)"?\s*$`)

// trustEnvList — один разобранный список переменных.
type trustEnvList struct {
	File    string
	Line    int // строка ключа `extraEnv:` / `env:`
	Anchors []string
	Markers []string
}

func trustIndentOf(s string) int { return len(s) - len(strings.TrimLeft(s, " ")) }

// scanTrustEnvLists разбирает файл по отступам и возвращает списки переменных.
//
// Разбор строчный, а не через YAML-библиотеку: профили несут подстановки
// Go-шаблона и валидным YAML не являются ни на одной ревизии. Тот же приём у
// соседних проверок этого каталога.
func scanTrustEnvLists(path string, body string) []trustEnvList {
	lines := strings.Split(body, "\n")
	var out []trustEnvList

	for i := 0; i < len(lines); i++ {
		m := trustEnvListStart.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		base := len(m[1])
		list := trustEnvList{File: path, Line: i + 1}

		for j := i + 1; j < len(lines); j++ {
			raw := lines[j]
			trimmed := strings.TrimSpace(raw)
			if trimmed == "" {
				continue
			}
			// Комментарий, выровненный по телу списка, принадлежит списку;
			// выровненный левее — уже следующему разделу.
			if trustIndentOf(raw) <= base && !strings.HasPrefix(trimmed, "#") {
				break
			}
			if strings.HasPrefix(trimmed, "#") {
				if mm := trustPostureMarker.FindStringSubmatch(raw); mm != nil {
					list.Markers = append(list.Markers, mm[1])
				}
				continue
			}
			if em := trustEnvEntry.FindStringSubmatch(raw); em != nil && trustAnchorVars[em[1]] {
				list.Anchors = append(list.Anchors, em[1])
			}
		}

		if len(list.Anchors) > 0 || len(list.Markers) > 0 {
			sort.Strings(list.Anchors)
			out = append(out, list)
		}
	}
	return out
}

// trustTrackedYAML — файлы профилей и шаблонов, ОТСЛЕЖИВАЕМЫЕ git.
//
// Единица счёта — отслеживаемый git-элемент, а не то, что лежит на диске:
// скачанные зависимости чарта не отслеживаются, поэтому популяция не раздувается
// чужим деревом и вердикт не зависит от того, делали ли `helm dep update`.
// Пути возвращаются относительно корня репозитория.
func trustTrackedYAML(t *testing.T, prefix string) []string {
	t.Helper()
	args := []string{"ls-files"}
	if prefix != "" {
		args = append(args, "--", prefix)
	}
	// Помощник, а не прямой вызов: он задаёт окружение git ЯВНО. Прямой вызов
	// наследует окружение прогона, и `cmd.Dir` тогда не выбирает репозиторий —
	// перепись пошла бы по чужому дереву, а вердикт молча стал бы свойством
	// рабочего каталога.
	out, err := gitenv.Command("..", args...).Output()
	if err != nil {
		t.Fatalf("перепись отслеживаемых файлов не удалась: %v", err)
	}
	var files []string
	for _, p := range strings.Split(string(out), "\n") {
		p = strings.TrimSpace(p)
		if strings.HasSuffix(p, ".yaml") || strings.HasSuffix(p, ".yml") {
			files = append(files, p)
		}
	}
	sort.Strings(files)
	return files
}

// trustAxisCode — какая ось сработала. Нужна инъекции: доказательство обязано
// показывать, что дефект уронил ИМЕННО СВОЮ ось, а не «что-нибудь покраснело».
type trustAxisCode string

const (
	trustAxisPostureUndeclared trustAxisCode = "A: посадка не объявлена"
	trustAxisSelfContradicting trustAxisCode = "A: объявление спорит само с собой"
	trustAxisClaimUnbacked     trustAxisCode = "B: исключительность не обеспечена"
	trustAxisMarkerOutlived    trustAxisCode = "C: отметка пережила предмет"
)

// trustFinding — одна находка с осью, по которой она получена.
type trustFinding struct {
	Axis trustAxisCode
	Text string
}

// adjudicateTrustLists — ЕДИНСТВЕННЫЙ судья посадки доверия. Его зовёт и гейт,
// и доказательство инъекцией: подставной судья доказывал бы способность упасть
// у копии, а не у продукта.
func adjudicateTrustLists(lists []trustEnvList) []trustFinding {
	var findings []trustFinding
	for _, l := range lists {
		where := l.File + ":" + itoa(l.Line)

		// ── (A) ПОСАДКА ОБЪЯВЛЕНА ──
		if len(l.Anchors) > 0 && len(l.Markers) == 0 {
			findings = append(findings, trustFinding{trustAxisPostureUndeclared, where +
				": пинит якорь доверия (" + strings.Join(l.Anchors, ", ") +
				"), но посадка НЕ ОБЪЯВЛЕНА — добавьте `# ДОВЕРИЕ: ДОПОЛНЯЕТ` " +
				"либо `# ДОВЕРИЕ: ТОЛЬКО-ВНУТРЕННЕЕ` в этот список"})
		}
		if len(l.Markers) > 1 && !trustAllSame(l.Markers) {
			findings = append(findings, trustFinding{trustAxisSelfContradicting, where +
				": объявление спорит само с собой — отметок несколько и они разные: " +
				strings.Join(l.Markers, ", ")})
		}

		// ── (B) ИСКЛЮЧИТЕЛЬНОСТЬ ОБЕСПЕЧЕНА ──
		if contains(l.Markers, "ТОЛЬКО-ВНУТРЕННЕЕ") &&
			!(contains(l.Anchors, "SSL_CERT_FILE") && contains(l.Anchors, "SSL_CERT_DIR")) {
			findings = append(findings, trustFinding{trustAxisClaimUnbacked, where +
				": объявлено ТОЛЬКО-ВНУТРЕННЕЕ, но пинено лишь [" + strings.Join(l.Anchors, ", ") +
				"]. Исключительность так НЕДОСТИЖИМА: каждая переменная замещает только свой " +
				"список, второй продолжает читаться, и якорь ДОБАВЛЯЕТСЯ к публичным. " +
				"Либо пиньте обе (SSL_CERT_FILE и SSL_CERT_DIR), либо объявляйте ДОПОЛНЯЕТ. " +
				"Механизм измерен: trust_anchor_lists_are_independent_test.go"})
		}

		// ── (C) ОТМЕТКА НЕ ПЕРЕЖИВАЕТ СВОЙ ПРЕДМЕТ ──
		if len(l.Markers) > 0 && len(l.Anchors) == 0 {
			findings = append(findings, trustFinding{trustAxisMarkerOutlived, where +
				": посадка доверия объявлена (" + strings.Join(l.Markers, ", ") +
				"), но якорей в списке НЕТ — отметка пережила свой предмет, снимите её"})
		}
	}
	return findings
}

// TestTrustAnchorClaimIsBackedByTheMechanism — несущий гейт.
func TestTrustAnchorClaimIsBackedByTheMechanism(t *testing.T) {
	const population = "deploy/helm"

	files := trustTrackedYAML(t, population)
	if len(files) == 0 {
		t.Fatalf("ВЕРДИКТ БЕСПРЕДМЕТЕН: под %s не прочитано ни одного файла — "+
			"«ноль находок» здесь неотличимо от «ноль прочитанного»", population)
	}

	var (
		lists       []trustEnvList
		envListSeen int
	)
	for _, f := range files {
		b, err := os.ReadFile(filepath.Join("..", f))
		if err != nil {
			t.Fatalf("чтение %s: %v", f, err)
		}
		body := string(b)
		envListSeen += len(trustEnvListStart.FindAllString(body, -1))
		lists = append(lists, scanTrustEnvLists(f, body)...)
	}

	if envListSeen == 0 {
		t.Fatalf("ВЕРДИКТ БЕСПРЕДМЕТЕН: списков переменных не найдено ни одного — "+
			"разбор перестал узнавать форму объявления (файлов прочитано %d)", len(files))
	}

	findings := adjudicateTrustLists(lists)
	anchored, marked := 0, 0
	for _, l := range lists {
		if len(l.Anchors) > 0 {
			anchored++
		}
		if len(l.Markers) > 0 {
			marked++
		}
	}

	// ── ПЕРЕПИСЬ: печатается ВСЕГДА ──
	t.Logf("перепись популяции %s: файлов %d · списков переменных %d · "+
		"из них пинят якорь доверия %d · несут отметку посадки %d",
		population, len(files), envListSeen, anchored, marked)

	outside := trustAnchorsOutsidePopulation(t, population)
	t.Logf("вне популяции (гейтом НЕ судятся, сужение названо, а не умолчано): %d — %s",
		len(outside), strings.Join(outside, "; "))

	if len(findings) > 0 {
		var rendered []string
		for _, f := range findings {
			rendered = append(rendered, string(f.Axis)+" — "+f.Text)
		}
		sort.Strings(rendered)
		t.Errorf("посадка доверия объявлена не тем, чем обеспечена (%d):\n  %s",
			len(findings), strings.Join(rendered, "\n  "))
	}
}

// trustAnchorsOutsidePopulation — координаты якорей доверия ВНЕ судимой
// популяции. Не вердикт, а перепись: сужение обязано быть видно на каждом
// прогоне, иначе оно превращается в слепую зону.
func trustAnchorsOutsidePopulation(t *testing.T, population string) []string {
	t.Helper()
	var out []string
	for _, f := range trustTrackedYAML(t, "") {
		if strings.HasPrefix(f, population+"/") {
			continue
		}
		b, err := os.ReadFile(filepath.Join("..", f))
		if err != nil {
			continue
		}
		for _, l := range scanTrustEnvLists(f, string(b)) {
			if len(l.Anchors) > 0 {
				out = append(out, f+":"+itoa(l.Line)+" ["+strings.Join(l.Anchors, ",")+"]")
			}
		}
	}
	sort.Strings(out)
	if len(out) == 0 {
		return []string{"нет"}
	}
	return out
}

func trustAllSame(s []string) bool {
	for _, x := range s {
		if x != s[0] {
			return false
		}
	}
	return true
}
