// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
)

// vendorCeilingGrant — РЕШЕНИЕ ВЛАДЕЛЬЦА поднять потолок дерева ровно на одну
// названную привязку, до снятия её предмета.
//
// Потолок гейта — число базы, и записи, которую ветка могла бы поднять, у гейта не
// было: принять рост значило изменить сам гейт отдельным решением. Это решение и
// записано здесь — не числом «+1», а КОНКРЕТНОЙ строкой: файл и отпечаток её текста.
// Число без адреса поднимало бы потолок для любой следующей привязки; строка с
// отпечатком извиняет только себя.
//
// Текст строки здесь не выписан, выписан отпечаток: выписанный текст нёс бы имя
// издателя и сам стал бы привязкой, то есть поднимал бы потолок, который извиняет.
//
// Грант самоистекает В ДВЕ СТОРОНЫ:
//   - строки с этим отпечатком в файле больше нет — грант без привязки, находка
//     «снять грант»;
//   - строка есть, а архив подчарта, который она называет (`<архив>:` в начале
//     пути записи), из `SubjectDir` ушёл — предмет снят, находка «снять запись и
//     грант». Так держится «уходит вместе с подчартом»: релиз снятия, убрав архив,
//     получит красное здесь и у гейта исключений IaC-скана
//     (`deploy/scripts/assert-iac-exclusions-still-have-a-subject.py`, запись без
//     пути в дереве), а не тихо оставшуюся строку.
type vendorCeilingGrant struct {
	Tree       string
	File       string
	TextSHA256 string
	// SubjectDir — каталог, где обязан лежать архив, названный строкой.
	SubjectDir string
	Decision   string
	Removal    string
}

var retiredVendorCeilingGrants = []vendorCeilingGrant{{
	Tree:       "kacho",
	File:       ".trivyignore.yaml",
	TextSHA256: "8220983ac2b455f2f1033653d7e43c47928683957f1b7cc5405d4cf287c18326",
	SubjectDir: "deploy/helm/umbrella/charts",
	Decision: "решение владельца 2026-10-01 «+1 к потолку до релиза»: одна запись ведомости " +
		"IaC-скана (#2980) о находке KSV-0041 в подчарте снимаемого издателя",
	Removal: "вместе с подчартом в релизе отказа от издателя (kacho#2824)",
}}

// vendorGrantArchive — архив, который называет строка записи: `"<архив>:<путь>"`.
var vendorGrantArchive = regexp.MustCompile(`"([^"/:]+\.tgz):`)

func vendorTextSHA256(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// vendorGrantFinding — грант пережил свою привязку либо её предмет.
const vendorFindingGrantStale = "решение владельца о потолке пережило свой предмет"

// vendorApplyGrants поднимает потолки на привязки, извинённые грантами, и
// убирает их из прироста. → находки самоистечения и строки переписи грантов.
func vendorApplyGrants(
	grants []vendorCeilingGrant, head map[string]vendorTreeCorpus, byTree map[string][]vendorBinding,
	ceilings map[string]int, deltas map[string]vendorTreeDelta, census map[string]vendorTreeCensus,
) []vendorCeilingFinding {
	var findings []vendorCeilingFinding
	for _, g := range grants {
		var hit *vendorBinding
		for i, b := range byTree[g.Tree] {
			if b.File == g.File && vendorTextSHA256(b.Text) == g.TextSHA256 {
				hit = &byTree[g.Tree][i]
				break
			}
		}
		if hit == nil {
			findings = append(findings, vendorCeilingFinding{Tree: g.Tree, Kind: vendorFindingGrantStale,
				Note: fmt.Sprintf("дерево %s: %s — в %s нет строки с отпечатком %s; привязки нет, "+
					"и грант («%s») снимается: снимите его из retiredVendorCeilingGrants",
					g.Tree, vendorFindingGrantStale, g.File, g.TextSHA256[:12], g.Decision)})
			continue
		}
		m := vendorGrantArchive.FindStringSubmatch(hit.Text)
		subject := ""
		if m != nil {
			subject = g.SubjectDir + "/" + m[1]
		}
		if subject == "" || !vendorTreeHas(head[g.Tree], subject) {
			findings = append(findings, vendorCeilingFinding{Tree: g.Tree, Kind: vendorFindingGrantStale,
				Note: fmt.Sprintf("дерево %s: %s — строка %s:%d называет архив %q, которого в дереве "+
					"нет: подчарт снят (%s), снимите запись и грант",
					g.Tree, vendorFindingGrantStale, hit.File, hit.Line, subject, g.Removal)})
			continue
		}
		ceilings[g.Tree]++
		d := deltas[g.Tree]
		for i, b := range d.Added {
			if vendorBindingKey(b) == vendorBindingKey(*hit) {
				d.Added = append(d.Added[:i:i], d.Added[i+1:]...)
				break
			}
		}
		deltas[g.Tree] = d
		c := census[g.Tree]
		c.Granted++
		census[g.Tree] = c
	}
	return findings
}

// vendorTreeHas — путь отслеживается деревом (тело, двоичный файл или архив).
func vendorTreeHas(c vendorTreeCorpus, rel string) bool {
	if _, ok := c.Bodies[rel]; ok {
		return true
	}
	if _, ok := c.Blobs[rel]; ok {
		return true
	}
	for _, a := range c.Archives {
		if a.Name == rel {
			return true
		}
	}
	return false
}
