// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/PRO-Robotech/kacho/pkg/gitenv"
)

// AcceptanceEditFinding — приёмка, правленная ПОСЛЕ объявления своего состояния,
// и не объявившая этого.
type AcceptanceEditFinding struct {
	File           string
	StateLine      int
	StateChangedAt int64
	FileChangedAt  int64
}

func (f AcceptanceEditFinding) String() string {
	return fmt.Sprintf(
		"%s: строка состояния (%d) последний раз правлена %d, сам документ — %d, "+
			"а записи о правке ПОСЛЕ вердикта в нём нет. Вердикт есть утверждение о "+
			"РЕВИЗИИ, которую прочитал проверяющий: правка его не переносит и не "+
			"отзывает задним числом, поэтому шапка объявляет вердикт, молча о том, что "+
			"содержимое двигалось. Исход один — назвать правку в самом документе "+
			"(решение: services/iam/docs/engineering/architecture/"+
			"verdict-names-a-revision-not-a-file.md)",
		f.File, f.StateLine, f.StateChangedAt, f.FileChangedAt)
}

// AcceptanceEditCensus — объём осмотренного.
//
// Величин ТРИ, и печатаются все: «приёмок N · правлено после вердикта M · из них
// с записью K». Одно число здесь скрывало бы ровно тот случай, ради которого
// гейт заведён — документ, который никто не правил, и документ, чью правку никто
// не назвал, дают одинаковый ноль находок.
type AcceptanceEditCensus struct {
	DocsRead     int
	NoStateLine  []string
	NoHistory    []string
	EditedAfter  int
	CarryingNote int
}

func (c AcceptanceEditCensus) String() string {
	return fmt.Sprintf(
		"приёмок прочитано %d · правлено после объявления состояния %d · из них с "+
			"записью о правке %d · без строки состояния %d · без построчной истории %d",
		c.DocsRead, c.EditedAfter, c.CarryingNote, len(c.NoStateLine), len(c.NoHistory))
}

// acceptanceStateMarkers — начало строки, объявляющей состояние документа.
var acceptanceStateMarkers = []string{"**Статус:**", "**Status:**"}

// acceptanceEditNoteMarkers — формы, в которых правка после вердикта объявляется.
//
// Форм ДВЕ, и вторая не запасная: документ без вынесенного вердикта объявляет то
// же событие иначе («после объявления состояния»), потому что переносить нечего.
// Распознаватель, знающий одну форму, объявил бы находкой документ, который своё
// движение как раз назвал, — то есть краснел бы на верном тексте.
var acceptanceEditNoteMarkers = []string{
	"ПОСЛЕ вердикта",
	"ПОСЛЕ объявления состояния",
	"после вердикта",
}

// gitEpoch — время последней правки, в ЭПОХЕ.
//
// В эпохе, а не строкой локальной даты: в этом доме даты стоят в двух поясах, и
// строковое сравнение даёт неверный порядок.
func gitEpoch(root string, args ...string) (int64, bool) {
	out, err := gitenv.Command(root, args...).Output()
	if err != nil {
		return 0, false
	}
	line := strings.TrimSpace(strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0])
	if line == "" {
		return 0, false
	}
	v, cerr := strconv.ParseInt(line, 10, 64)
	if cerr != nil {
		return 0, false
	}
	return v, true
}

// AuditAcceptanceEditsAfterVerdict — вердикт о доме приёмок.
func AuditAcceptanceEditsAfterVerdict(root, dir string) ([]AcceptanceEditFinding, AcceptanceEditCensus, error) {
	var (
		census   AcceptanceEditCensus
		findings []AcceptanceEditFinding
	)
	abs := filepath.Join(root, filepath.FromSlash(dir))
	entries, err := os.ReadDir(abs)
	if err != nil {
		return nil, census, fmt.Errorf("дом приёмок %s не прочитан: %w — «ноль находок» "+
			"здесь означало бы «ноль прочитанного»", dir, err)
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		rel := dir + "/" + e.Name()
		body, rerr := os.ReadFile(filepath.Join(abs, e.Name())) // #nosec G304 -- путь из обхода своего дерева
		if rerr != nil {
			return nil, census, fmt.Errorf("%s: %w", rel, rerr)
		}
		census.DocsRead++

		stateLine := 0
		for i, line := range strings.Split(string(body), "\n") {
			// Звёздочка НЕ обрезается: она часть маркера (`**Статус:**`), и обрезав
			// её, распознаватель перестаёт видеть строку состояния ВООБЩЕ —
			// «без строки состояния 29» вместо «правлено после вердикта 29».
			t := strings.TrimLeft(line, "-+> \t")
			for _, m := range acceptanceStateMarkers {
				if strings.HasPrefix(t, m) {
					stateLine = i + 1
					break
				}
			}
			if stateLine > 0 {
				break
			}
		}
		if stateLine == 0 {
			// Приёмка без строки состояния — предмет ДРУГОГО держателя (машинное
			// чтение вердикта). Здесь она не находка и не тишина: названа числом.
			census.NoStateLine = append(census.NoStateLine, rel)
			continue
		}

		stateAt, okState := gitEpoch(root, "log", "-1", "--format=%ct",
			"-L", fmt.Sprintf("%d,%d:%s", stateLine, stateLine, rel))
		fileAt, okFile := gitEpoch(root, "log", "-1", "--format=%ct", "--", rel)
		if !okState || !okFile {
			// Построчной истории нет — документ не отслеживается либо только что
			// заведён. «Не знаю» не выдаётся ни за находку, ни за чистое.
			census.NoHistory = append(census.NoHistory, rel)
			continue
		}
		if fileAt <= stateAt {
			continue
		}
		census.EditedAfter++

		carries := false
		for _, m := range acceptanceEditNoteMarkers {
			if strings.Contains(string(body), m) {
				carries = true
				break
			}
		}
		if carries {
			census.CarryingNote++
			continue
		}
		findings = append(findings, AcceptanceEditFinding{
			File: rel, StateLine: stateLine,
			StateChangedAt: stateAt, FileChangedAt: fileAt,
		})
	}

	sort.Slice(findings, func(i, j int) bool { return findings[i].File < findings[j].File })
	sort.Strings(census.NoStateLine)
	sort.Strings(census.NoHistory)
	return findings, census, nil
}
