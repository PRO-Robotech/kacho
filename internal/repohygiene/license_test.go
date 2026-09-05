// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package repohygiene — репо-широкие гигиенические гейты. Живёт в КОРНЕ, а не внутри
// services/compute: гейт проверяет ВЕСЬ репозиторий, и его прописка в одном из сервисов
// была рудиментом polyrepo (в kacho-compute он был локальным).
//
// ВЕРДИКТ ЭТОГО ПАКЕТА НЕДЕЙСТВИТЕЛЕН БЕЗ ОТКЛЮЧЕНИЯ КЕША `go test`. Проверки
// здесь судят ДЕРЕВО, а состав дерева берут из индекса git подпроцессом, которого
// инструмент не видит: правка в чужом каталоге кеш не инвалидирует, и над красным
// деревом печатается `ok (cached)`. Поэтому пакет ОТКАЗЫВАЕТСЯ работать на
// прогоне, результат которого пойдёт в кеш (TestMain в cachedverdictmain_test.go);
// прогонять — с `-count=1` либо целями Makefile, которые его уже несут. Разбор
// класса и замеры — pkg/treecorpus, cachedverdict.go.
//
// license_test.go — лицензионный заголовок файла отвечает УРОВНЮ, на котором файл
// лежит. Отображение путь→лицензия — licensemap.go, здесь его только применяют.
//
// # Два разных вопроса, и различать их обязательно
//
//	ОБЯЗАННОСТЬ  — какие файлы обязаны нести заголовок вообще (inScope);
//	СООТВЕТСТВИЕ — если заголовок есть, он обязан совпасть с уровнем пути.
//
// Слить их в один нельзя ни в одну сторону. Требовать заголовок от всего —
// значит требовать его от Markdown, JSON и вендоренного текста третьей стороны.
// Проверять соответствие только там, где заголовок ОБЯЗАН быть, — значит не
// заметить `Dockerfile`, `CONTRIBUTING.md` и `.tmpl`, которые заголовок несут
// добровольно: они остались бы с лицензией прежнего уровня молча. Замер
// 2026-09-04: таких носителей вне области обязанности — 6 под `pkg/` и
// `services/iam/` (предикат в шапке licensemap.go неприменим, здесь считалось
// `git grep -l 'SPDX-License-Identifier' -- pkg services/iam | grep -vE
// '\.(go|sql|sh|py|yaml|yml|proto)$' | grep -v /docs/`), плюс 32 страницы
// документации iam.
package repohygiene

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// licenseHeaderRe — заголовок лицензии в шапке файла. Идентификатор берётся
// ПЕРВЫМ вхождением в шапке: тот же текст встречается ниже прозой — в разборе
// класса «шаблонизатор съел перевод строки» он приведён дословно, вместе с
// приклеенным хвостом (`BUSL-1.1apiVersion`). Настоящий заголовок стоит в
// первых строках, поэтому первое вхождение — всегда он.
var licenseHeaderRe = regexp.MustCompile(`SPDX-License-Identifier:\s*([A-Za-z0-9.+-]+)`)

// licenseHeaderWindow — сколько байт от начала файла считается шапкой.
const licenseHeaderWindow = 1024

// repoRoot — поднимаемся от каталога теста до каталога с go.mod (корень репо).
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod not found above %s", dir)
		}
		dir = parent
	}
}

// skipPath — пути вне области ОБЯЗАННОСТИ нести хедер: VCS, синканная
// AI-оснастка, каталоги документации, вендоренное и build-артефакты. Принимает
// REL-путь (обход идёт по индексу git, где имена каталогов отдельно не приходят).
//
// На проверку СООТВЕТСТВИЯ не влияет: документ, заголовок несущий, обязан нести
// верный — иначе 32 страницы документации iam остались бы с чужой лицензией.
func skipPath(rel string) bool {
	for _, seg := range strings.Split(rel, "/") {
		switch seg {
		case ".git", ".claude", "docs", "node_modules", "vendor", "bin":
			return true
		}
	}
	return false
}

// inScope — файлы, ОБЯЗАННЫЕ нести SPDX-хедер. Markdown/JSON/lock/Dockerfile и
// сгенерированный код — вне области (см. licensing-and-comments.md).
//
// `.proto` внесён 2026-09-04 вместе с отображением путь→лицензия. До этого
// контракты не наблюдались ВОВСЕ: 124 файла под `proto/` заголовок несли, и
// гейт о них не знал — то есть перелицензирование контрактов прошло бы мимо
// него молча, ровно тот класс, который распознаватель обязан закрывать
// (предикат: `git ls-files -- '*.proto' | wc -l` → 129).
func inScope(rel string) bool {
	base := filepath.Base(rel)
	if base == "Makefile" {
		return true
	}
	switch filepath.Ext(rel) {
	case ".go", ".sql", ".sh", ".py", ".yaml", ".yml", ".proto":
		return true
	}
	return false
}

// isGenerated — файл произведён генератором (protoc/buf/mockgen/…), поэтому SPDX-хедер
// с него не требуется: его пишет генератор, а не человек.
//
// Детект — по КАНОНИЧНОМУ Go-маркеру (`^// Code generated .* DO NOT EDIT\.$`,
// https://go.dev/s/generatedcode), а НЕ по пути. Прежде исключение было захардкожено
// как префикс `proto/gen/` — путь polyrepo. При переезде в монорепу он протух МОЛЧА
// (стабы теперь в pkg/api/), и гейт вывалил 78 генерённых .pb.gw.go. Маркер переживает
// любую смену раскладки; путь — нет.
func isGenerated(rel string, body []byte) bool {
	if filepath.Ext(rel) != ".go" {
		return false
	}
	sc := bufio.NewScanner(bytes.NewReader(body))
	// Маркер обязан стоять до объявления package — хватит первых строк.
	for i := 0; i < 10 && sc.Scan(); i++ {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "// Code generated") && strings.HasSuffix(line, "DO NOT EDIT.") {
			return true
		}
		if strings.HasPrefix(line, "package ") {
			return false
		}
	}
	return false
}

// declaredLicense — идентификатор из шапки файла. Второй результат false —
// заголовка в шапке нет вовсе.
func declaredLicense(body []byte) (string, bool) {
	head := body
	if len(head) > licenseHeaderWindow {
		head = head[:licenseHeaderWindow]
	}
	m := licenseHeaderRe.FindSubmatch(head)
	if m == nil {
		return "", false
	}
	return string(m[1]), true
}

// licenseTierCensus — объём осмотренного ПО ОДНОМУ уровню. «Ноль находок»
// обязано быть отличимо от «ноль прочитанного», а на дереве с несколькими
// уровнями — ещё и по уровням: полный обход при нулевом одном уровне читается
// как чистота, хотя означает, что уровень исчез.
type licenseTierCensus struct {
	files     int // записей индекса, попавших на уровень
	required  int // из них ОБЯЗАННЫХ нести заголовок
	declaring int // из них заголовок НЕСУЩИХ (шире предыдущего: сюда входят добровольные)
	generated int
}

type licenseHeaderCensus struct {
	indexed   int
	required  int
	declaring int
	byTier    map[string]*licenseTierCensus
}

// licenseHeaderFinding — одна находка. Несёт ОБЕ лицензии: без ожидаемой
// находка не отличима от «заголовка нет», а без найденной — не отличима от
// «заголовок какой-то не тот».
type licenseHeaderFinding struct {
	file string
	tier string
	kind string // "missing" | "mismatch"
	want string
	got  string
}

func (f licenseHeaderFinding) String() string {
	if f.kind == "missing" {
		return fmt.Sprintf("%s — уровень %q ожидает %s, а заголовка нет вовсе", f.file, f.tier, f.want)
	}
	return fmt.Sprintf("%s — уровень %q ожидает %s, файл объявляет %s", f.file, f.tier, f.want, f.got)
}

// scanLicenseHeaders — чистая функция над перечнем путей и читателем: обе
// проверки сразу, чтобы доказательство способности упасть шло по
// синтетическому корпусу и НЕ писало в живое дерево.
func scanLicenseHeaders(paths []string, read func(string) ([]byte, error)) ([]licenseHeaderFinding, licenseHeaderCensus) {
	census := licenseHeaderCensus{indexed: len(paths), byTier: map[string]*licenseTierCensus{}}
	for _, name := range licenseTierNames() {
		census.byTier[name] = &licenseTierCensus{}
	}
	var findings []licenseHeaderFinding

	for _, rel := range paths {
		tier := licenseTierFor(rel)
		tc := census.byTier[tier.Name]
		tc.files++

		body, err := read(rel)
		if err != nil {
			findings = append(findings, licenseHeaderFinding{file: rel, tier: tier.Name,
				kind: "mismatch", want: tier.SPDX, got: "не прочитан: " + err.Error()})
			continue
		}
		generated := isGenerated(rel, body)
		got, declares := declaredLicense(body)

		// СООТВЕТСТВИЕ: судится всякий носитель заголовка — обязанный,
		// добровольный и генерённый, — потому что лжёт он одинаково.
		if declares {
			tc.declaring++
			census.declaring++
			if got != tier.SPDX {
				want := tier.SPDX
				if want == "" {
					want = "никакого: файл третьей стороны под собственным уведомлением"
				}
				findings = append(findings, licenseHeaderFinding{file: rel, tier: tier.Name,
					kind: "mismatch", want: want, got: got})
			}
		}

		// ОБЯЗАННОСТЬ: только область покрытия, без генерённого и без третьей
		// стороны — требовать от неё наш заголовок значило бы утверждать наше
		// авторство над чужим текстом.
		if skipPath(rel) || !inScope(rel) || tier.SPDX == "" {
			continue
		}
		if generated {
			tc.generated++
			continue
		}
		tc.required++
		census.required++
		if !declares {
			findings = append(findings, licenseHeaderFinding{file: rel, tier: tier.Name,
				kind: "missing", want: tier.SPDX})
		}
	}

	sort.Slice(findings, func(i, j int) bool { return findings[i].file < findings[j].file })
	return findings, census
}

func TestLicenseFileExists(t *testing.T) {
	root := repoRoot(t)
	if _, err := os.Stat(filepath.Join(root, "LICENSE")); err != nil {
		t.Fatalf("root LICENSE missing: %v", err)
	}
}

// TestLicenseTierRootsCarryTheirLicenseFile — у каждого уровня, объявившего
// собственный файл лицензии, этот файл лежит. Ось отдельная от проверки
// заголовков: заголовок ссылается на текст лицензии, и уровень, чьи файлы
// объявляют Apache-2.0 при отсутствующем `pkg/LICENSE`, ссылается в пустоту.
func TestLicenseTierRootsCarryTheirLicenseFile(t *testing.T) {
	root := repoRoot(t)
	checked := 0
	for _, tier := range licenseTiers {
		if !tier.OwnLicenseFile {
			continue
		}
		checked++
		rel := filepath.Join(tier.Prefix, "LICENSE")
		if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
			t.Errorf("уровень %q (%s) объявляет свой файл лицензии, а %s нет: %v",
				tier.Name, tier.SPDX, rel, err)
		}
	}
	t.Logf("осмотрено: уровней со своим файлом лицензии %d", checked)
	if checked == 0 {
		t.Fatal("ни один уровень не объявляет своего файла лицензии — отображение " +
			"разошлось с проверкой. Это отказ, а не чистота")
	}
}

// TestLicenseHeadersMatchTheirTier — обе проверки над деревом.
func TestLicenseHeadersMatchTheirTier(t *testing.T) {
	root := repoRoot(t)

	// Ходим по ИНДЕКСУ git, а не по диску (filepath.WalkDir). Причина: на диске лежат
	// gitignored-файлы, которых в репозитории нет и быть не должно — напр.
	// values.fe3455-ory.yaml (креды кластера, локальный артефакт). Обход диска требовал
	// бы от НИХ SPDX-хедер, что бессмысленно: гейт про содержимое РЕПОЗИТОРИЯ.
	// Индекс — ровно то, что уедет в чистый клон и в CI.
	var paths []string
	for _, line := range gitLsFiles(t, root) {
		if _, rel, ok := parseLsFiles(line); ok {
			paths = append(paths, rel)
		}
	}

	findings, census := scanLicenseHeaders(paths, func(rel string) ([]byte, error) {
		return os.ReadFile(filepath.Join(root, rel))
	})

	t.Logf("осмотрено: записей индекса %d, в области обязанности %d, заголовок несут %d; находок %d",
		census.indexed, census.required, census.declaring, len(findings))
	for _, name := range licenseTierNames() {
		c := census.byTier[name]
		t.Logf("  уровень %-20s файлов %5d, обязаны нести %5d, заголовок несут %5d, генерённых пропущено %4d",
			name, c.files, c.required, c.declaring, c.generated)
	}

	if census.indexed == 0 {
		t.Fatal("индекс git пуст — обход не дошёл ни до одного файла. Это отказ, а не чистота")
	}
	if census.required == 0 {
		t.Fatal("в области обязанности не оказалось НИ ОДНОГО файла: предикат inScope разошёлся " +
			"с деревом. Это отказ, а не чистота")
	}
	if census.declaring == 0 {
		t.Fatal("заголовок не несёт НИ ОДИН файл индекса: распознаватель разошёлся с деревом. " +
			"Это отказ, а не чистота")
	}
	// Уровень, под который не попал ни один файл, — послабление без предмета: он
	// либо исчез из дерева, либо его префикс протух. Молча такое не наблюдается
	// ничем (правило самоистечения послаблений).
	for _, name := range licenseTierNames() {
		if census.byTier[name].files == 0 {
			t.Errorf("уровню %q в дереве не отвечает НИ ОДИН файл: записи нечего покрывать — "+
				"снимите её из licenseTiers либо почините префикс", name)
		}
	}

	if len(findings) > 0 {
		var b strings.Builder
		for _, f := range findings {
			b.WriteString("\n  " + f.String())
		}
		t.Errorf("%d файл(ов) расходятся с лицензией своего уровня:%s", len(findings), b.String())
	}
}
