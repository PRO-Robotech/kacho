// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// protocitedholder.go — координата, названная комментарием КОНТРАКТА, обязана
// резолвиться в дереве.
//
// # Предмет
//
// Контракт читают снаружи: по нему пишут клиента, по нему сверяют поведение, и
// он же обещает читателю ДЕРЖАТЕЛЯ своих инвариантов («enforced by …»).
// Координата, за которой в дереве ничего нет, обещает проверку, которой не
// существует, — форма проверки без содержания, и обнаруживается она ровно
// тогда, когда на неё положились.
//
// Наблюдалось: три комментария надгробий iam объявляли дисциплину
// зарезервированных номеров удержанной скриптом `scripts/tombstone_enforce.sh`.
// В дереве такого файла нет ни одного; настоящий держатель — адъюдикация
// `buf breaking` против ствола (`.github/workflows/ci.yaml`, `scripts/ci-local.sh`),
// то есть обещание было не ложным по существу, а ложным по адресу: читатель
// шёл искать скрипт и не находил ни его, ни того, что вместо него.
//
// # Что судится и чего гейт НЕ судит
//
// Судится РАЗРЕШИМОСТЬ координаты: путь, названный комментарием, лежит в
// составе дерева. Не судится, ПРАВДУ ли говорит проза о названном файле —
// «держит ли он это свойство» машинного предиката не имеет. Разрешимость от
// прозы отделима, и её отсутствие уже достаточно дорого.
//
// # Состав берётся ИНДЕКСОМ, а не диском
//
// И для обхода контрактов, и для проверки существования: иначе вердикт зависел
// бы от того, что лежит в рабочем каталоге у прогоняющего.
package repohygiene

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// ProtoCitedHolder — одна координата, названная комментарием контракта.
type ProtoCitedHolder struct {
	File string // контракт, который цитирует
	Line int
	Path string // цитируемая координата
}

// ProtoCitationCensus — перепись обхода. Печатается целиком: «находок 0»
// обязано быть отличимо от «прочитано 0».
type ProtoCitationCensus struct {
	Contracts int // прочитано файлов контрактов
	Citations int // всего координат, названных комментариями
	Resolved  int // из них разрешились в составе дерева
	Dangling  []ProtoCitedHolder
	Findings  []string
}

// citedPathRe — координата дерева в комментарии контракта.
//
// # Почему перечень корней ЗАКРЫТ, а не «любой путь со слэшем»
//
// Открытая форма считала бы координатой всякое `foo/bar.md` из прозы, включая
// имена чужих спецификаций и адреса вне репозитория, и первое же ложное
// срабатывание сняло бы гейт. Корни перечислены по составу монорепо и
// сокращаются вместе с ним.
var citedPathRe = regexp.MustCompile(
	`\b(?:scripts|tools|internal|services|deploy|pkg|gateway|proto|ui-future|terraform)/[A-Za-z0-9._/-]+\.(?:sh|py|go|yaml|yml|sql|md|proto|ts|tsx)\b`)

// SurveyProtoCitedHolders сводит координаты, названные комментариями
// контрактов, с составом дерева.
func SurveyProtoCitedHolders(tree *treecorpus.Tree) (ProtoCitationCensus, error) {
	var c ProtoCitationCensus

	files := make([]string, 0, 64)
	for rel := range tree.Files() {
		if strings.HasPrefix(rel, "proto/") && strings.HasSuffix(rel, ".proto") {
			files = append(files, rel)
		}
	}
	sort.Strings(files)

	for _, rel := range files {
		body, err := os.ReadFile(filepath.Join(tree.Root(), filepath.FromSlash(rel)))
		if err != nil {
			return c, fmt.Errorf("контракт %s не читается: %w", rel, err)
		}
		c.Contracts++
		for i, ln := range strings.Split(string(body), "\n") {
			comment := strings.Index(ln, "//")
			if comment < 0 {
				continue
			}
			for _, p := range citedPathRe.FindAllString(ln[comment:], -1) {
				c.Citations++
				if tree.HasFile(p) {
					c.Resolved++
					continue
				}
				c.Dangling = append(c.Dangling, ProtoCitedHolder{File: rel, Line: i + 1, Path: p})
			}
		}
	}

	for _, d := range c.Dangling {
		c.Findings = append(c.Findings, fmt.Sprintf(
			"%s:%d цитирует %s, которого в составе дерева НЕТ\n"+
				"    контракт обещает читателю держателя, за которым никого нет; "+
				"исходов два — завести названное либо назвать действующего держателя",
			d.File, d.Line, d.Path))
	}
	if c.Contracts == 0 {
		c.Findings = append(c.Findings, "обход пуст: контрактов не прочитано ни одного — "+
			"«находок 0» неотличимо от «прочитано 0»")
	}
	return c, nil
}
