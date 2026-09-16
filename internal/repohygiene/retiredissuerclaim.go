// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// retiredissuerclaim.go — ГЕЙТ КЛАССА: утверждение, что прежний внешний
// OAuth-сервер ОСТАЁТСЯ подписантом либо издателем (или что чеканящая служба
// ничего не чеканит), законно только в форме надгробия (эпик #2564, линия B).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Своя чеканка действует на пяти стендах из шести (`deploy/stacks.txt`,
// держатель — `deploy/own_minting_census_test.go`), край и реестр принимают
// нашего издателя записью «издатель → набор ключей». Прозу об этом писали в
// разное время, и в дереве живут ДВЕ редакции одного утверждения:
//
//	«fetches docker-Bearer verification keys here (Hydra stays signer/issuer).»
//	«„Hydra remains the signer/issuer“ — утверждение, пережившее свой предмет»
//
// Первая — утверждение, пережившее предмет (в дереве оно стояло без кавычек;
// здесь процитировано, потому что этот файл гейт читает тоже). Вторая —
// надгробие: та же фраза, названная ПРЕЖНЕЙ и опровергнутая. Читатель первой принимает её за
// действующее ограничение: «раз подписант чужой — издателя платформы здесь не
// объявлять, ключницу не ротировать».
//
// Цена измерена в этом же дереве: в день, когда утверждение опровергли в одном
// профиле (`values.dev-prod.yaml`), оно продолжало читаться как норма ещё в
// ЧЕТЫРЁХ файлах — чарте службы, боевом профиле площадки, чарте и профиле
// реестра. Нашла их эта перепись, а не обзор диффа: половины лежат в разных
// файлах, и дифф одного не показывает остальных.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО СУДИТСЯ — РОД УТВЕРЖДЕНИЯ, А НЕ СЛОВО
//
// Слово «Hydra» в дереве законно во множестве мест: ручки посадки, имя
// подчарта, маршрут проброса в консоли, разбор переезда. Гейт по слову краснел
// бы на исправном дереве и был бы снят первым же обходом (та же граница, что у
// `providersurface.go`). Судится РОД утверждения — что прежний издатель
// остаётся (stays / remains / является) подписантом или издателем, что ключи
// проверки «его», что служба ничего не чеканит. Формы перечислены в
// `retiredIssuerClaimForms` поимённо, и инъекция гоняет КАЖДУЮ.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЗАКОННЫЙ БЛИЗНЕЦ — НАДГРОБИЕ, ДВА ПРИЗНАКА, ЛЮБОГО ДОСТАТОЧНО
//
//  1. утверждение стоит В КАВЫЧКАХ на той же строке («…», "…", “…”) — так
//     корпус цитирует снятое: «ЗДЕСЬ СТОЯЛО „…“»;
//  2. на той же строке маркер прошедшего: «стояло», «It said», «пережившее»,
//     «no longer», «больше не».
//
// Надгробие, разнесённое так, что ни маркер, ни кавычка не попали на строку с
// утверждением, гейт назовёт находкой — и верно: оно читается как утверждение с
// той же вероятностью, с какой его не узнал распознаватель.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕГО ГЕЙТ НЕ ВИДИТ — НАЗВАНО ВСЛУХ
//
// Описание полосы по имени прежнего издателя («Hydra-issued token», «via
// Hydra») под ось НЕ подпадает: это происхождение, а не утверждение о том, кто
// подписывает сегодня. Часть таких описаний тоже пережила предмет, и правится
// она обзором. Пробы, e2e-наборы и порождённые стабы из обхода исключены: там
// прежний издатель законно стоит фикстурой.

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// retiredIssuerName — имя снимаемого компонента, как его пишут в дереве.
const retiredIssuerName = "hydra"

// errRetiredIssuerEmptyCorpus — обход не принёс ни одного файла.
var errRetiredIssuerEmptyCorpus = errors.New("обход пуст: прочитано ноль файлов")

// retiredIssuerClaimForm — одна форма утверждения «прежний издатель действует».
type retiredIssuerClaimForm struct {
	Name string
	Re   *regexp.Regexp
}

// retiredIssuerClaimForms — перечень закрыт и гоняется инъекцией поимённо:
// форма, о которой распознаватель не знает, даёт не красное и не зелёное, а
// молчание.
var retiredIssuerClaimForms = []retiredIssuerClaimForm{
	{"остаётся (stays/remains)", regexp.MustCompile(`(?i)\bhydra\s+(stays|remains)\b`)},
	{"является издателем/подписантом", regexp.MustCompile(
		`(?i)\bhydra\s+is\s+(still\s+)?the\s+(token\s+)?(issuer|signer)\b|\b(issuer|signer)\s+is\s+hydra\b`)},
	{"ключи проверки — его", regexp.MustCompile(`(?i)\b(keys?|kids?)\s+(are|is)\s+hydra's`)},
	{"служба не чеканит", regexp.MustCompile(
		`(?i)\b(iam|kaname|shim|service|it)\s+(mints\s+nothing|does\s+not\s+mint|doesn't\s+mint)\b|\(hydra\s+does\)|\b(iam|kaname|служба|шим)\s+(ничего\s+не\s+чеканит|не\s+чеканит\s+ничего)`)},
	{"остаётся (по-русски)", regexp.MustCompile(
		`(?i)\bhydra\s+оста[её]тся\b|(издател|подписант)[а-яё]*\s*[—–-]+\s*hydra\b|\bhydra\s*[—–-]+\s*(единственн[а-яё]+\s+)?(издател|подписант)`)},
}

// retiredIssuerTombstoneMarker — маркер прошедшего на той же строке.
var retiredIssuerTombstoneMarker = regexp.MustCompile(
	`(?i)(стояло|стояла|it said|formerly|пережившее|пережило|no longer|больше не|перестал)`)

// retiredIssuerClaim — одно утверждение, пережившее предмет.
type retiredIssuerClaim struct {
	File string
	Line int
	Form string
	Text string
}

func (c retiredIssuerClaim) String() string {
	return fmt.Sprintf("%s:%d утверждает, что прежний OAuth-сервер действует (%s): %q — "+
		"своя чеканка объявлена стендами, а край и реестр принимают нашего издателя; "+
		"читатель, приняв это за норму, построит на ней решение. Либо приведите к факту, "+
		"либо назовите прежним и опровергните на той же строке (надгробие: в кавычках "+
		"либо со словом «стояло»)",
		c.File, c.Line, c.Form, strings.TrimSpace(c.Text))
}

// retiredIssuerClaimCensus — объём осмотренного: «ноль находок» обязано быть
// отличимо от «ноль прочитанного».
type retiredIssuerClaimCensus struct {
	Files      int
	Mentions   int
	Claims     int
	Tombstones int
	Findings   int
}

func (c retiredIssuerClaimCensus) String() string {
	return fmt.Sprintf("перепись: файлов %d · строк с именем прежнего издателя %d · "+
		"утверждений о действующем %d · из них надгробий %d · находок %d",
		c.Files, c.Mentions, c.Claims, c.Tombstones, c.Findings)
}

// retiredIssuerProseFile — файл, чью прозу гейт судит: всё отслеживаемое, кроме
// проб, e2e-наборов, порождённых стабов, замков зависимостей и двоичного.
//
// Документация и профили ВХОДЯТ намеренно: страница оператора и комментарий
// профиля — места, где утверждение переживает предмет дольше всего, потому что
// их никто не компилирует.
func retiredIssuerProseFile(rel string) bool {
	switch {
	case strings.HasSuffix(rel, "_test.go"), strings.HasSuffix(rel, ".pb.go"),
		strings.HasSuffix(rel, ".pb.gw.go"), strings.HasSuffix(rel, "package-lock.json"),
		strings.HasSuffix(rel, ".sum"), strings.HasSuffix(rel, ".log"):
		return false
	}
	for _, seg := range strings.Split(rel, "/") {
		switch seg {
		case ".git", ".claude", "node_modules", "vendor", "bin", "tests", "testdata", "e2e":
			return false
		}
	}
	switch {
	case strings.HasSuffix(rel, ".png"), strings.HasSuffix(rel, ".jpg"),
		strings.HasSuffix(rel, ".ico"), strings.HasSuffix(rel, ".woff"),
		strings.HasSuffix(rel, ".woff2"), strings.HasSuffix(rel, ".wasm"),
		strings.HasSuffix(rel, ".pdf"), strings.HasSuffix(rel, ".gz"),
		strings.HasSuffix(rel, ".tgz"), strings.HasSuffix(rel, ".svg"):
		return false
	}
	return true
}

// judgeRetiredIssuerClaims — тело гейта над телами файлов. Вынесено, чтобы
// инъекция звала ТО ЖЕ, что исполняется на дереве. Пустой корпус — отказ.
func judgeRetiredIssuerClaims(corpus map[string]string) ([]retiredIssuerClaim, retiredIssuerClaimCensus, error) {
	census := retiredIssuerClaimCensus{Files: len(corpus)}
	if len(corpus) == 0 {
		return nil, census, fmt.Errorf("%w — судить нечего", errRetiredIssuerEmptyCorpus)
	}
	rels := make([]string, 0, len(corpus))
	for rel := range corpus {
		rels = append(rels, rel)
	}
	sort.Strings(rels)

	var out []retiredIssuerClaim
	for _, rel := range rels {
		for i, line := range strings.Split(corpus[rel], "\n") {
			lower := strings.ToLower(line)
			if !strings.Contains(lower, retiredIssuerName) &&
				!strings.Contains(lower, "чеканит") && !strings.Contains(lower, "mint") {
				continue
			}
			if strings.Contains(lower, retiredIssuerName) {
				census.Mentions++
			}
			form, loc := retiredIssuerClaimAt(line)
			if form == "" {
				continue
			}
			census.Claims++
			if retiredIssuerTombstoneMarker.MatchString(line) || retiredIssuerQuoted(line, loc[0], loc[1]) {
				census.Tombstones++
				continue
			}
			census.Findings++
			out = append(out, retiredIssuerClaim{File: rel, Line: i + 1, Form: form, Text: line})
		}
	}
	return out, census, nil
}

func retiredIssuerClaimAt(line string) (string, []int) {
	for _, f := range retiredIssuerClaimForms {
		if loc := f.Re.FindStringIndex(line); loc != nil {
			return f.Name, loc
		}
	}
	return "", nil
}

// retiredIssuerQuoted — лежит ли отрезок [s,e) внутри пары кавычек одного вида
// на той же строке: ёлочки корпуса, прямые, типографские.
func retiredIssuerQuoted(line string, s, e int) bool {
	pairs := [][2]string{{"«", "»"}, {`"`, `"`}, {"“", "”"}}
	for _, p := range pairs {
		if strings.Contains(line[:s], p[0]) && strings.Contains(line[e:], p[1]) {
			return true
		}
	}
	return false
}
