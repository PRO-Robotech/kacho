// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// licensemap_injection_test.go — доказательство способности гейта
// license_test.go упасть и смолчать.
//
// Гейт судит одну строку в шапке файла, и вакуумным его сделать проще всего:
// достаточно, чтобы отображение перестало различать уровни — и он молча
// пропустит любую лицензию где угодно. Поэтому каждая ось проверяется В ОБЕ
// СТОРОНЫ, а рядом с каждым отрицанием стоит ЗАКОННЫЙ БЛИЗНЕЦ той же формы:
// файл того же уровня, того же расширения, отличающийся ровно одним фактом —
// идентификатором лицензии.
//
// Инъекция идёт по СИНТЕТИЧЕСКОМУ корпусу: гейт разложен на чистую функцию и
// обход, поэтому доказательство не пишет в живое дерево и не зависит от того,
// что в нём сегодня лежит.
package repohygiene

import (
	"fmt"
	"strings"
	"testing"
)

// injHeader — шапка файла Go с заданным идентификатором лицензии.
func injHeader(spdx string) string {
	return "// Copyright (c) PRO-Robotech\n// SPDX-License-Identifier: " + spdx +
		"\n\npackage foo\n"
}

type injHeaderCorpus map[string]string

func (c injHeaderCorpus) scan() ([]licenseHeaderFinding, licenseHeaderCensus) {
	paths := make([]string, 0, len(c))
	for k := range c {
		paths = append(paths, k)
	}
	// Порядок обхода на вердикт не влияет — находки сортируются, — но пусть он
	// будет устойчив: недетерминированный вход делает красное невоспроизводимым.
	for i := 1; i < len(paths); i++ {
		for j := i; j > 0 && paths[j] < paths[j-1]; j-- {
			paths[j], paths[j-1] = paths[j-1], paths[j]
		}
	}
	return scanLicenseHeaders(paths, func(rel string) ([]byte, error) {
		body, ok := c[rel]
		if !ok {
			return nil, fmt.Errorf("нет такого файла")
		}
		return []byte(body), nil
	})
}

// ── сторона (а): дефект краснеет и называет координату И ОБЕ лицензии ────────

// Несущая ось перехода: файл фундамента несёт ЧУЖУЮ лицензию. Именно этот
// случай прежний гейт не распознавал ВОВСЕ — он держал одну константу и читал
// чужой заголовок как отсутствующий.
//
// Чужой здесь берётся AGPL, и это уже НЕ уровень дерева: приставка вынесенного
// продукта ушла из карты вместе с каталогом службы доступа. Идентификатор
// оставлен намеренно — ось утверждает «заголовок не тот, что ждёт уровень», и
// для неё существенно лишь то, что лицензия отличается от ожидаемой; взяв
// вместо неё BUSL, проба слилась бы с соседней осью «монорепо на фундаменте».
func TestLicenseHeaderGate_RedsWhenFoundationCarriesAnAlienLicense(t *testing.T) {
	t.Parallel()
	findings, census := injHeaderCorpus{
		"pkg/ids/ids.go": injHeader(licenseAGPL),
	}.scan()
	if len(findings) != 1 {
		t.Fatalf("ожидалась одна находка, получено %d (перепись: осмотрено %d)",
			len(findings), census.indexed)
	}
	got := findings[0].String()
	for _, want := range []string{"pkg/ids/ids.go", licenseApache, licenseAGPL, "фундамент"} {
		if !strings.Contains(got, want) {
			t.Errorf("находка не называет %q — без него разбор уводит не туда: %s", want, got)
		}
	}
}

// Обратное направление той же оси: файл УМОЛЧАНИЯ несёт лицензию фундамента.
// Без этой оси гейт мог бы судить только уровни с приставкой, а самый населённый
// уровень дерева — тот, у которого приставки нет, — оставался бы вне суждения.
//
// Прежде эта ось стояла на паре «вынесенный продукт ↔ монорепо». Уровень
// вынесенного продукта ушёл из карты вместе с каталогом службы доступа, и путь
// под снятой приставкой стал разрешаться в умолчание: заголовок BUSL совпадал с
// ожидаемым, находок становилось ноль, и проба краснела на исправном гейте.
// Пара заменена на живую; проверяемое свойство — «судится больше одного уровня,
// и находка называет ОБЕ лицензии» — то же самое.
func TestLicenseHeaderGate_RedsWhenTheMonorepoCarriesTheFoundationLicense(t *testing.T) {
	t.Parallel()
	findings, _ := injHeaderCorpus{
		"gateway/internal/restmux/mux.go": injHeader(licenseApache),
	}.scan()
	if len(findings) != 1 {
		t.Fatalf("ожидалась одна находка, получено %d", len(findings))
	}
	got := findings[0].String()
	for _, want := range []string{licenseBUSL, licenseApache, "монорепо"} {
		if !strings.Contains(got, want) {
			t.Errorf("находка не называет %q: %s", want, got)
		}
	}
}

// Контракты — отдельный уровень, а не «часть фундамента»: у них свой префикс.
func TestLicenseHeaderGate_RedsWhenContractsCarryTheMonorepoLicense(t *testing.T) {
	t.Parallel()
	findings, _ := injHeaderCorpus{
		"proto/kaname/cloud/iam/v1/iam.proto": "// SPDX-License-Identifier: " + licenseBUSL + "\n",
	}.scan()
	if len(findings) != 1 || !strings.Contains(findings[0].String(), "контракты") {
		t.Fatalf("расхождение уровня контрактов не распознано: %v", findings)
	}
}

// Наш заголовок на файле ТРЕТЬЕЙ СТОРОНЫ — находка: он утверждает наше
// авторство над чужим текстом.
func TestLicenseHeaderGate_RedsWhenThirdPartyCarriesOurHeader(t *testing.T) {
	t.Parallel()
	findings, _ := injHeaderCorpus{
		"proto/google/api/http.proto": "// SPDX-License-Identifier: " + licenseApache + "\n",
		"proto/google/api/other.proto": "// Copyright 2026 Google LLC\n" +
			"// Licensed under the Apache License, Version 2.0\n",
	}.scan()
	if len(findings) != 1 {
		t.Fatalf("ожидалась ровно одна находка (второй файл — законный близнец): %v", findings)
	}
	if findings[0].file != "proto/google/api/http.proto" {
		t.Fatalf("названа не та координата: %v", findings)
	}
	if !strings.Contains(findings[0].String(), "третьей стороны") {
		t.Fatalf("находка не называет причину: %s", findings[0])
	}
}

// Отсутствие заголовка вовсе остаётся находкой — и называет ожидаемую лицензию,
// а не только факт отсутствия.
func TestLicenseHeaderGate_RedsWhenHeaderIsAbsentAndNamesTheExpectedLicense(t *testing.T) {
	t.Parallel()
	findings, _ := injHeaderCorpus{
		"pkg/ids/ids.go": "package ids\n",
	}.scan()
	if len(findings) != 1 || !strings.Contains(findings[0].String(), "заголовка нет вовсе") {
		t.Fatalf("отсутствие заголовка не распознано: %v", findings)
	}
	if !strings.Contains(findings[0].String(), licenseApache) {
		t.Fatalf("находка не называет ожидаемую лицензию: %s", findings[0])
	}
}

// ── сторона (б): законный близнец обязан молчать ─────────────────────────────

// Верное дерево всех ЧЕТЫРЁХ живых уровней сразу — фундамент, контракты, третья
// сторона и умолчание. Гейт, краснеющий на верном дереве, отключают первым.
//
// Пятая строка — файл вынесенного продукта под AGPL — снята вместе со своим
// уровнем: приставки `services/iam/` в карте больше нет, путь разрешался бы в
// умолчание, и близнец, объявленный законным, стал находкой.
func TestLicenseHeaderGate_SilentOnACorrectTree(t *testing.T) {
	t.Parallel()
	findings, census := injHeaderCorpus{
		"pkg/ids/ids.go":                    injHeader(licenseApache),
		"proto/kaname/cloud/iam/v1/i.proto": injHeader(licenseApache),
		"services/vpc/internal/a.go":        injHeader(licenseBUSL),
		"internal/repohygiene/x.go":         injHeader(licenseBUSL),
		"proto/google/api/http.proto":       "// Copyright 2026 Google LLC\n",
	}.scan()
	if len(findings) != 0 {
		t.Fatalf("верное дерево объявлено находкой: %v", findings)
	}
	if census.required != 4 || census.declaring != 4 {
		t.Fatalf("близнецы не дошли до предиката: обязаны нести %d, несут %d — молчание тогда "+
			"означает «не читал», а не «сошлось»", census.required, census.declaring)
	}
}

// Самый длинный префикс побеждает: `proto/google/` — третья сторона, а не
// контракты, при том что оба префикса совпадают. Без этой оси порядок записей
// в таблице стал бы несущим свойством, о котором никто не знает.
func TestLicenseHeaderGate_LongestPrefixWinsOverAShorterOne(t *testing.T) {
	t.Parallel()
	if licenseTierFor("proto/google/api/http.proto").Name != "третья сторона" {
		t.Fatalf("длинный префикс не победил: %q", licenseTierFor("proto/google/api/http.proto").Name)
	}
	if licenseTierFor("proto/kaname/cloud/iam/v1/i.proto").Name != "контракты" {
		t.Fatal("контракты потеряли свой уровень")
	}
	if licenseTierFor("services/vpc/internal/a.go").Name != "монорепо" {
		t.Fatal("сервис без своей приставки уехал не на уровень умолчания")
	}
	// Умолчание — САМЫЙ КОРОТКИЙ префикс, и побеждает оно только тогда, когда не
	// совпал ни один длиннее. Без этой строки «длинный побеждает» доказывалось бы
	// лишь на паре вложенных приставок, а вырожденный случай пустой оставался бы
	// вне суждения.
	if licenseTierFor("gateway/main.go").Name != "монорепо" {
		t.Fatal("край уехал не на уровень умолчания")
	}
}

// Генерённый файл заголовок нести НЕ обязан — но если несёт, обязан нести
// верный: стабы порождаются из контрактов и наследуют их шапку дословно.
func TestLicenseHeaderGate_GeneratedIsExemptFromDutyButNotFromConformance(t *testing.T) {
	t.Parallel()
	gen := func(spdx string) string {
		return "// Copyright (c) PRO-Robotech\n// SPDX-License-Identifier: " + spdx +
			"\n\n// Code generated by protoc-gen-go. DO NOT EDIT.\n\npackage api\n"
	}
	findings, census := injHeaderCorpus{
		"pkg/api/kacho/a.pb.go": gen(licenseApache),
		"pkg/api/kacho/b.pb.go": "// Code generated by protoc-gen-go. DO NOT EDIT.\n\npackage api\n",
	}.scan()
	if len(findings) != 0 {
		t.Fatalf("генерённые близнецы объявлены находкой: %v", findings)
	}
	if census.byTier["фундамент"].generated != 2 {
		t.Fatalf("генерённые не опознаны: %d", census.byTier["фундамент"].generated)
	}
	bad, _ := injHeaderCorpus{"pkg/api/kacho/a.pb.go": gen(licenseBUSL)}.scan()
	if len(bad) != 1 || !strings.Contains(bad[0].String(), licenseApache) {
		t.Fatalf("генерённый файл с ЧУЖОЙ лицензией не распознан: %v — стабы наследуют "+
			"шапку контракта, значит лгут ровно так же", bad)
	}
}

// Markdown заголовок нести не обязан, но НЕСУЩИЙ обязан нести верный: иначе 32
// страницы документации остались бы с лицензией прежнего уровня молча.
func TestLicenseHeaderGate_VoluntaryCarrierIsJudgedTooButNotRequired(t *testing.T) {
	t.Parallel()
	silent, census := injHeaderCorpus{
		"services/vpc/docs/a.md": "<!--\nSPDX-License-Identifier: " + licenseBUSL + "\n-->\n",
		"services/vpc/docs/b.md": "# без заголовка вовсе\n",
	}.scan()
	if len(silent) != 0 {
		t.Fatalf("верные добровольные носители объявлены находкой: %v", silent)
	}
	if census.required != 0 {
		t.Fatalf("документация попала в область ОБЯЗАННОСТИ (%d) — заголовок от неё не требуется",
			census.required)
	}
	if census.declaring != 1 {
		t.Fatalf("добровольный носитель не дошёл до предиката: несут %d", census.declaring)
	}
	bad, _ := injHeaderCorpus{
		"services/vpc/docs/a.md": "<!--\nSPDX-License-Identifier: " + licenseApache + "\n-->\n",
	}.scan()
	if len(bad) != 1 {
		t.Fatalf("добровольный носитель с чужой лицензией не распознан: %v", bad)
	}
}

// Заголовок берётся ПЕРВЫМ вхождением в шапке: тот же текст встречается ниже
// прозой — в разборе класса «шаблонизатор съел перевод строки». Без этой оси
// распознаватель, читающий последнее вхождение, выглядел бы исправным.
func TestLicenseHeaderGate_ReadsTheFirstOccurrenceNotTheProse(t *testing.T) {
	t.Parallel()
	body := "# Copyright (c) PRO-Robotech\n# SPDX-License-Identifier: " + licenseApache +
		"\n#\n# Симптом класса: `# SPDX-License-Identifier: BUSL-1.1apiVersion: v1`.\n"
	findings, _ := injHeaderCorpus{"pkg/quota/x.sh": body}.scan()
	if len(findings) != 0 {
		t.Fatalf("проза о заголовке принята за заголовок: %v", findings)
	}
}

// ── перепись: «ноль находок» отличимо от «ноль прочитанного» ─────────────────

func TestLicenseHeaderGate_CensusSeparatesCleanFromUnread(t *testing.T) {
	t.Parallel()
	_, empty := injHeaderCorpus{"README.md": "нет тут заголовка\n"}.scan()
	if empty.declaring != 0 || empty.required != 0 {
		t.Fatalf("перепись насчитала носителей там, где их нет: несут %d, обязаны %d",
			empty.declaring, empty.required)
	}
	if empty.indexed == 0 {
		t.Fatal("перепись не считает осмотренное вовсе")
	}
	_, seen := injHeaderCorpus{"pkg/a.go": injHeader(licenseApache)}.scan()
	if seen.declaring != 1 || seen.byTier["фундамент"].files != 1 {
		t.Fatalf("перепись не отличает прочитанное от пустого: несут %d, файлов уровня %d",
			seen.declaring, seen.byTier["фундамент"].files)
	}
}
