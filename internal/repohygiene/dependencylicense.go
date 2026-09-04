// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// dependencylicense.go — разбор зависимостей на предмет ЛИЦЕНЗИИ у модуля,
// который дерево закрепляет и распространяет.
//
// # Предмет
//
// Отсутствие лицензии у чужого модуля означает «все права защищены»: закон не
// даёт разрешения по умолчанию, его даёт лицензия. Значит каждый выпущенный
// образ и каждый клон ПУБЛИЧНОГО репозитория, несущего такой пин, распространяет
// чужой код без разрешения — и нарушение действует каждый день, независимо от
// того, компилируется ли этот модуль сегодня в какой-нибудь бинарь.
//
// # Предмет гейта — КЛАСС, а не имя
//
// Судится ОТСУТСТВИЕ ЛИЦЕНЗИИ у модуля, а не членство имени в перечне.
// Перечень имён охранял бы имена: следующий модуль без лицензии приехал бы под
// другим именем и остался бы вне наблюдения молча. Здесь наоборот — новый пин
// обязан САМ доказать, что несёт лицензию, иначе он находка by construction.
//
// Прощённых записей у гейта нет ни одной, и заводить их не следует: запись
// «этому модулю лицензия не нужна» есть утверждение о чужом праве, которого мы
// делать не вправе.
//
// # Чем доказывается лицензия
//
// Двумя способами, и оба — конвенции экосистемы, а не наш вкус:
//
//	файл лицензии в корне модуля  — LICENSE / LICENCE / COPYING / NOTICE
//	                                (любой регистр, любое расширение)
//	заголовок SPDX в корневом файле — SPDX-License-Identifier
//
// Второй способ — не украшение: модуль вправе не заводить отдельного файла и
// объявить лицензию заголовками. Замер по этому дереву: из 141 разрешившегося
// модуля файл в корне несут 140, и ровно один не несёт НИЧЕГО — ни файла на
// любой глубине, ни SPDX ни в одном из 236 своих файлов.
//
// # Граница, названная прямо
//
// Гейт судит ФАКТ наличия лицензии, а не её СОВМЕСТИМОСТЬ с BUSL-1.1: «модуль
// под AGPL» и «модуль под MIT» для него одинаково licensed. Совместимость —
// решение человека, и подменять его предикатом значило бы обещать проверку,
// которой нет.
//
// Модуль, не извлечённый в кэш, гейт прочитать не может — и такой НЕ считается
// проверенным: он попадает в отдельную графу переписи и называется по имени.
// «Ноль находок» обязано быть отличимо от «ноль прочитанного», поэтому пустой
// разбор go.mod и разбор, не прочитавший НИ ОДНОГО модуля, роняют гейт.

// licenseFileBases — основы имён файла лицензии, в нижнем регистре. Сравнение
// идёт по ПРЕФИКСУ основы: `LICENSE`, `LICENSE.md`, `LICENSE-MIT`, `COPYING.txt`.
var licenseFileBases = []string{"license", "licence", "copying", "notice"}

// spdxTag — заголовок, которым модуль объявляет лицензию, не заводя файла.
// Собирается из частей, чтобы этот исходник не опознал сам себя: у него в шапке
// стоит такой же заголовок, и целиковый литерал сделал бы гейт зелёным на
// собственном тексте.
const spdxTag = "SPDX-License-" + "Identifier"

// spdxScanExts — расширения корневых файлов, в которых ищется заголовок SPDX.
var spdxScanExts = map[string]bool{".go": true, ".md": true, ".txt": true, "": true}

// spdxScanCap — потолок читаемого файла. Заголовок стоит в шапке; читать
// многомегабайтные данные ради него незачем.
const spdxScanCap = 64 << 10

// DirectDependency — одна запись `require` из go.mod: путь модуля, версия и
// строка, в которой она объявлена. Строка — координата находки: чинить надо
// именно её.
type DirectDependency struct {
	Path    string
	Version string
	Line    int
}

// LicenseEvidence — что удалось узнать о каталоге модуля.
type LicenseEvidence struct {
	// Resolved — каталог модуля нашёлся и прочитан.
	Resolved bool
	// Licensed — лицензия доказана.
	Licensed bool
	// Marker — ЧЕМ доказана: имя файла лицензии либо "<файл>: <заголовок SPDX>".
	Marker string
	// FilesRead — сколько записей каталога осмотрено.
	FilesRead int
}

// LicenseProbe — источник свидетельства о каталоге модуля. Отдельным типом,
// чтобы инъекция подавала синтетику, не трогая кэш модулей.
type LicenseProbe func(dep DirectDependency) LicenseEvidence

// UnlicensedFinding — модуль, у которого лицензии нет.
type UnlicensedFinding struct {
	Dep       DirectDependency
	FilesRead int
}

func (f UnlicensedFinding) String() string {
	return "go.mod:" + strconv.Itoa(f.Dep.Line) + " " + f.Dep.Path + " " + f.Dep.Version +
		" — лицензии нет ни файлом, ни заголовком (осмотрено записей: " + strconv.Itoa(f.FilesRead) + ")"
}

// DependencyLicenseCensus — объём осмотренного. Печатается ВСЕГДА: без него
// «находок 0» неотличимо от «не прочитано ничего».
type DependencyLicenseCensus struct {
	Requires   int
	Resolved   int
	Licensed   int
	Unresolved []string
	FilesRead  int
}

func (c DependencyLicenseCensus) String() string {
	s := "перепись: записей require " + strconv.Itoa(c.Requires) +
		" · каталогов прочитано " + strconv.Itoa(c.Resolved) +
		" · с лицензией " + strconv.Itoa(c.Licensed) +
		" · записей каталогов осмотрено " + strconv.Itoa(c.FilesRead) +
		" · НЕ прочитано " + strconv.Itoa(len(c.Unresolved))
	if len(c.Unresolved) > 0 {
		s += " (" + strings.Join(c.Unresolved, ", ") + ")"
	}
	return s
}

// ParseGoModRequires — записи `require` из текста go.mod: и блочная форма, и
// однострочная. Возвращает ВСЕ записи, а не только прямые: распространяется
// всё, что дерево закрепляет, — косвенная зависимость приезжает в клон и в
// образ наравне с прямой.
func ParseGoModRequires(body string) []DirectDependency {
	var out []DirectDependency
	inBlock := false
	for i, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		if idx := strings.Index(line, "//"); idx >= 0 {
			line = strings.TrimSpace(line[:idx])
		}
		switch {
		case line == "require (":
			inBlock = true
			continue
		case inBlock && line == ")":
			inBlock = false
			continue
		}
		fields := strings.Fields(line)
		if inBlock {
			if len(fields) < 2 {
				continue
			}
			out = append(out, DirectDependency{Path: fields[0], Version: fields[1], Line: i + 1})
			continue
		}
		if len(fields) == 3 && fields[0] == "require" {
			out = append(out, DirectDependency{Path: fields[1], Version: fields[2], Line: i + 1})
		}
	}
	return out
}

// EscapeModulePath — кодировка пути модуля для кэша: заглавная буква
// становится восклицательным знаком со строчной (`H-BF` → `!h-!b!f`). Правило
// экосистемы, а не наше: файловые системы бывают регистронезависимы, и без него
// два разных модуля делили бы один каталог.
func EscapeModulePath(p string) string {
	var b strings.Builder
	for _, r := range p {
		if r >= 'A' && r <= 'Z' {
			b.WriteByte('!')
			b.WriteRune(r + ('a' - 'A'))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// ModuleCacheDir — каталог модуля в кэше.
func ModuleCacheDir(cache string, dep DirectDependency) string {
	return filepath.Join(cache, filepath.FromSlash(EscapeModulePath(dep.Path))+"@"+dep.Version)
}

// hasLicenseFileName — основа имени объявляет файл лицензией.
func hasLicenseFileName(name string) bool {
	low := strings.ToLower(name)
	for _, base := range licenseFileBases {
		if strings.HasPrefix(low, base) {
			return true
		}
	}
	return false
}

// DiskLicenseProbe — свидетельство с диска: корень модуля в кэше.
func DiskLicenseProbe(cache string) LicenseProbe {
	return func(dep DirectDependency) LicenseEvidence {
		dir := ModuleCacheDir(cache, dep)
		entries, err := os.ReadDir(dir)
		if err != nil {
			return LicenseEvidence{}
		}
		ev := LicenseEvidence{Resolved: true, FilesRead: len(entries)}
		var spdxCandidates []fs.DirEntry
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if hasLicenseFileName(e.Name()) {
				ev.Licensed, ev.Marker = true, e.Name()
				return ev
			}
			if spdxScanExts[strings.ToLower(filepath.Ext(e.Name()))] {
				spdxCandidates = append(spdxCandidates, e)
			}
		}
		for _, e := range spdxCandidates {
			info, ierr := e.Info()
			if ierr != nil || info.Size() > spdxScanCap {
				continue
			}
			body, rerr := os.ReadFile(filepath.Join(dir, e.Name()))
			if rerr != nil {
				continue
			}
			if strings.Contains(string(body), spdxTag) {
				ev.Licensed, ev.Marker = true, e.Name()+": "+spdxTag
				return ev
			}
		}
		return ev
	}
}

// ScanDependencyLicenses — разбор. Находка — модуль, чей каталог прочитан и
// лицензии в нём нет. Модуль, чей каталог прочитать не удалось, находкой НЕ
// становится и в «проверенные» НЕ засчитывается: он уходит в отдельную графу
// переписи под своим именем.
func ScanDependencyLicenses(deps []DirectDependency, probe LicenseProbe) ([]UnlicensedFinding, DependencyLicenseCensus) {
	census := DependencyLicenseCensus{Requires: len(deps)}
	var findings []UnlicensedFinding
	for _, dep := range deps {
		ev := probe(dep)
		census.FilesRead += ev.FilesRead
		if !ev.Resolved {
			census.Unresolved = append(census.Unresolved, dep.Path+" "+dep.Version)
			continue
		}
		census.Resolved++
		if ev.Licensed {
			census.Licensed++
			continue
		}
		findings = append(findings, UnlicensedFinding{Dep: dep, FilesRead: ev.FilesRead})
	}
	sort.Slice(findings, func(i, j int) bool { return findings[i].Dep.Path < findings[j].Dep.Path })
	return findings, census
}
