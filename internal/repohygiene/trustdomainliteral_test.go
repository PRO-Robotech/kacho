// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// trustdomainliteral_test.go — домен доверия объявляет УСТАНОВКА, а не сборка
// (приёмка KAN-WIRE-1, сценарий KAN-W4-01, предмет `ПР-4`).
//
// Разбор, формы записи и границы распознавателя — в шапке
// `trustdomainliteral.go`; здесь не пересказываются.
//
// # Чем этот гейт НЕ является
//
// Он не судит, ВЕРЕН ли домен: это вопрос к установке, а не к числу его
// объявлений. И он не судит согласия выпускающей стороны с принимающей — у того
// предмета свой держатель (`trustdomainposture_test.go`, сценарий KAN-W4-04).
package repohygiene

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// trustDomainCensusFloor — порог переписи: ниже него «ноль находок» означало бы
// «ноль прочитанного».
const trustDomainCensusFloor = 1000

// TestTrustDomainIsDeclaredNotCompiled — сам гейт.
func TestTrustDomainIsDeclaredNotCompiled(t *testing.T) {
	root := repoRoot(t)
	tt := newTrackedTree(t, root)

	var rels []string
	for rel := range tt.files {
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
			continue
		}
		if skipPath(rel) {
			continue
		}
		rels = append(rels, rel)
	}
	sort.Strings(rels)

	var (
		parsed  int
		census  TrustDomainCensus
		shape   []string
		reports []string
	)
	for _, rel := range rels {
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		sites, c, perr := ScanTrustDomainLiterals(rel, src)
		if perr != nil {
			t.Fatalf("разбор %s: %v", rel, perr)
		}
		parsed++
		census.Literals += c.Literals
		census.Calls += c.Calls
		census.Imports += c.Imports
		census.DotImports += c.DotImports
		for _, s := range sites {
			// ФОРМА личности домена не несёт и установку ни к чему не обязывает:
			// `spiffe://`, `spiffe://%s/ns/`, `spiffe://<домен>/ns/…`. Она
			// законна везде, и запрещать её значило бы запрещать разбор личности
			// вместе с прозой о нём. Находка — КОНКРЕТНЫЙ домен.
			if !s.AuthorityIsConcrete {
				if strings.HasPrefix(rel, TrustDomainOwnerDir) {
					shape = append(shape, fmt.Sprintf("%s:%d %q", s.File, s.Line, s.Value))
				}
				continue
			}
			reports = append(reports, fmt.Sprintf("%s:%d  форма=%s  власть=%q  значение=%q",
				s.File, s.Line, s.Form, s.Authority, s.Value))
		}
	}

	t.Logf("перепись: не-тестовых файлов Go разобрано %d, строковых литералов осмотрено %d, "+
		"вызовов осмотрено %d, импортов %d, точечных импортов владельца %d, "+
		"объявлений ФОРМЫ личности у владельца (%s) %d, находок %d",
		parsed, census.Literals, census.Calls, census.Imports, census.DotImports,
		TrustDomainOwnerDir, len(shape), len(reports))

	if parsed < trustDomainCensusFloor {
		t.Fatalf("перепись обвалилась: разобрано %d файлов при пороге %d — на таком объёме "+
			"«ноль находок» означало бы «ноль прочитанного»", parsed, trustDomainCensusFloor)
	}
	if census.Literals == 0 {
		t.Fatalf("осмотрено ноль строковых литералов на %d файлах — разбор перестал видеть "+
			"предмет, и его молчание сказано ни о чём", parsed)
	}

	sort.Strings(reports)
	if len(reports) > 0 {
		t.Fatalf("домен доверия объявлен КОДОМ — %d место(а):\n  %s\n\n"+
			"Домен доверия — то, чьи сертификаты установка признаёт своими, то есть круг тех, кто "+
			"вправе говорить за пользователя. Пока он скомпилирован, установка меняет его только "+
			"пересборкой: правка величины профиля даёт сертификаты нового домена, а принимающая "+
			"сторона остаётся прежней — и расходятся они МОЛЧА, отказом законному отправителю, "+
			"неотличимым от отсутствия личности.\n"+
			"Снятие: брать домен у %s (тип TrustDomain) из величины установки, а не писать его "+
			"своей рукой.",
			len(reports), strings.Join(reports, "\n  "), TrustDomainOwnerDir)
	}

	// Предпосылка проверяется ПОСЛЕ находок и ровно затем, зачем она заведена:
	// отличить «прочитал и не нашёл» от «искать стало нечего». Ноль объявлений
	// формы у владельца означает, что личность сертификата больше никто не
	// разбирает, — и тогда гейт молчал бы и при исчезнувшем предмете.
	if len(shape) == 0 {
		t.Fatalf("у владельца %s не осталось ни одного объявления ФОРМЫ личности (схема без "+
			"конкретной власти) — гейт беспредметен: он молчит и тогда, когда разбирать "+
			"личность сертификата стало некому", TrustDomainOwnerDir)
	}
	for _, s := range shape {
		t.Logf("форма личности объявлена владельцем: %s", s)
	}
}
