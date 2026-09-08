// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// vendorednotice.go — вендоренный ЧУЖОЙ файл контракта обязан нести уведомление
// своего первоисточника, а рядом обязана лежать копия его лицензии.
//
// # Предмет
//
// Apache-2.0 §4(a) требует передавать получателю копию лицензии, §4(c) —
// СОХРАНЯТЬ в производной работе уведомления первоисточника. Вырезанное
// уведомление — нарушение, действующее в момент раздачи, а не когда-нибудь: оно
// не отличимо от исправного состояния ничем, потому что файл компилируется,
// генерация проходит, и `buf lint` о лицензиях ничего не знает by construction.
//
// Замер, из которого гейт выведен (дерево на `0c3b3313a7`, единица счёта —
// отслеживаемый файл `.proto`): вендоренных чужих файлов 4, уведомление несли 2
// (`proto/google/api/annotations.proto`, `proto/google/api/http.proto`), у двух
// остальных файл начинался сразу с объявления синтаксиса; копии лицензии рядом
// не было вовсе.
//
// # Чем это НЕ является — шов с соседним гейтом назван с обеих сторон
//
// [TestSPDXHeadersPresent] (license_test.go) требует НАШЕГО заголовка
// (`SPDX-License-Identifier: BUSL-1.1`) от НАШИХ файлов. Здесь предмет обратный:
// ЧУЖОЙ файл обязан нести ЧУЖОЕ уведомление, и наш заголовок ему приписывать
// нельзя — он утверждал бы наше правообладание на чужой текст. Пересечения
// между гейтами нет by construction: `inScope` соседа не признаёт расширение
// `.proto` вовсе.
//
// # Как отличается чужой файл от нашего — по СОДЕРЖИМОМУ, не по перечню путей
//
// Файл судится по ОБЪЯВЛЕННОМУ им пакету, а не по каталогу, в котором лежит.
// Пакет, начинающийся с `kacho`, — наш; любой другой — вендоренный. Новое
// вендоренное пространство (`envoy`, `grpc`, `opentelemetry`) попадает под гейт
// само, без правки перечня; наш новый домен — не попадает.
//
// Объявление берётся из ОПЕРАТОРА, а не из подстроки: слово `package` стоит и в
// комментариях (в том числе в этом файле), поэтому текст комментариев снимается
// до разбора. Гейт, судящий подстроку, краснел бы на собственном объяснении.
//
// # Корень вендоренного пространства — тоже выводится
//
// Копия лицензии обязана лежать в корне пространства, а не рядом с каждым
// файлом: `proto/google/LICENSE` накрывает и `api/`, и `rpc/`. Корень
// вычисляется из объявленного пакета: первый его сегмент отыскивается сегментом
// пути, и путь обрезается по нему включительно (`package google.rpc` +
// `proto/google/rpc/status.proto` → `proto/google`). Не нашёлся — корнем
// считается каталог самого файла, и перепись это НАЗЫВАЕТ: вывод по умолчанию
// не должен быть неотличим от вывода по правилу.
//
// # Три оси находки
//
//  1. `notice-missing` — в головном блоке комментария нет строки копирайта.
//     Это §4(c) в чистом виде;
//  2. `license-copy-missing` — в корне пространства нет копии лицензии. Это §4(a);
//  3. `notice-license-mismatch` — уведомление есть, копия лежит, но уведомление
//     не называет ТУ ЖЕ лицензию, что лежит рядом. Название берётся из самой
//     копии (её заглавная строка), а не выписывается: гейт с зашитым словом
//     «Apache» судил бы будущий BSD-файл по чужой мерке.
//
// # Чего разбор НЕ видит — названо, а не спрятано
//
// Ось 3 не выносится, когда копия лицензии не несёт заглавной строки (у ряда
// лицензий её нет by construction — BSD, MIT). Тогда пара «файл + корень» идёт
// в счётчик [VendoredNoticeCensus.LicenseTitleUnknown], и перепись печатает его
// отдельно: «вердикт не вынесен» обязано быть отличимо от «нарушений нет».
//
// Гейт не сверяет ТЕЛО вендоренного файла с первоисточником — предмет здесь
// уведомление, а не дословность кода. Расхождение тела с первоисточником —
// отдельный предмет, и у него отдельный держатель.
package repohygiene

import (
	"regexp"
	"strings"

	"github.com/PRO-Robotech/kacho/pkg/contractroot"
)

// VendoredNoticeFinding — координата находки.
type VendoredNoticeFinding struct {
	// File — путь файла относительно корня репозитория.
	File string
	// Package — объявленный файлом пакет (пусто, если объявления нет).
	Package string
	// VendorRoot — корень вендоренного пространства, где ожидается копия лицензии.
	VendorRoot string
	// Kind — ось: `notice-missing`, `license-copy-missing`, `notice-license-mismatch`.
	Kind string
	// Detail — что именно не сошлось.
	Detail string
}

// VendoredNoticeCensus — объём осмотренного. «Ноль находок» обязано быть
// отличимо от «ноль прочитанного», поэтому счётчики печатаются всегда.
type VendoredNoticeCensus struct {
	// FilesRead — сколько файлов контракта прочитано.
	FilesRead int
	// Ours — из них объявили наш пакет.
	Ours int
	// Vendored — из них объявили чужой пакет.
	Vendored int
	// PackageUndeclared — файлов без оператора объявления пакета.
	PackageUndeclared int
	// VendorRoots — сколько различных корней вендоренных пространств найдено.
	VendorRoots int
	// RootsDerivedFromPath — из них корней, выведенных по сегменту пути (правило).
	RootsDerivedFromPath int
	// RootsFellBackToDir — из них корней, взятых каталогом файла (умолчание).
	RootsFellBackToDir int
	// NoticesFound — вендоренных файлов, несущих строку копирайта.
	NoticesFound int
	// LicenseTitleUnknown — пар «файл + корень», где ось 3 НЕ вынесена: копия
	// лицензии не несёт заглавной строки. Ноль здесь означает «ось вынесена
	// везде», а не «сверять было нечего».
	LicenseTitleUnknown int
	// MismatchChecked — пар, где ось 3 действительно вынесена.
	MismatchChecked int
}

// isOurPackage — признаётся ли объявленный пакет НАШИМ.
//
// Первый сегмент сверяется с ОБЪЯВЛЕННЫМ множеством корней, а не с одним
// литералом. Литерал здесь особенно опасен: всё, что ему не совпало, гейт
// объявляет ВЕНДОРЕННЫМ — то есть требует уведомления об авторстве и копии
// чужой лицензии. После переезда службы доступа под собственный корень
// (KAN-PKG-1) её контракт стал бы «чужим кодом» в собственном дереве, и гейт
// потребовал бы приложить к нему копию Apache-2.0.
//
// Имена корней строчные: имена пакетов контрактов в этом дереве строчные
// (`kacho.cloud.<domain>.v1`, `kaname.cloud.iam.v1`).
func isOurPackage(pkg string) bool {
	for _, root := range contractroot.Roots {
		if pkg == root || strings.HasPrefix(pkg, root+".") {
			return true
		}
	}
	return false
}

// packageStmt — оператор объявления пакета. Судится СТРОКА оператора уже после
// снятия комментариев, поэтому слово `package` из прозы сюда не доходит.
var packageStmt = regexp.MustCompile(`(?m)^\s*package\s+([A-Za-z_][A-Za-z0-9_.]*)\s*;`)

// copyrightLine — строка уведомления о правообладании. Ищется в ГОЛОВНОМ блоке
// комментария, а не по всему файлу: уведомление §4(c) стоит в шапке, а слово
// «copyright» встречается и в теле (в описании полей, в примерах).
var copyrightLine = regexp.MustCompile(`(?i)\bcopyright\b`)

// StripProtoComments — снимает из текста контракта комментарии обеих форм,
// сохраняя разбивку на строки. Нужно, чтобы оператор объявления пакета судился
// как оператор: в прозе этого дерева `package google.api` встречается штатно.
func StripProtoComments(src string) string {
	var b strings.Builder
	b.Grow(len(src))
	const (
		code = iota
		lineComment
		blockComment
		str
	)
	state := code
	for i := 0; i < len(src); i++ {
		c := src[i]
		switch state {
		case code:
			switch {
			case c == '/' && i+1 < len(src) && src[i+1] == '/':
				state = lineComment
				i++
			case c == '/' && i+1 < len(src) && src[i+1] == '*':
				state = blockComment
				i++
			case c == '"':
				state = str
				b.WriteByte(c)
			default:
				b.WriteByte(c)
			}
		case lineComment:
			if c == '\n' {
				state = code
				b.WriteByte(c)
			}
		case blockComment:
			if c == '*' && i+1 < len(src) && src[i+1] == '/' {
				state = code
				i++
				continue
			}
			if c == '\n' {
				b.WriteByte(c)
			}
		case str:
			b.WriteByte(c)
			if c == '\\' && i+1 < len(src) {
				i++
				b.WriteByte(src[i])
				continue
			}
			if c == '"' {
				state = code
			}
		}
	}
	return b.String()
}

// DeclaredPackage — пакет, объявленный контрактом. Пустая строка означает, что
// объявления нет вовсе (такой файл в вендоренные не записывается: судить его
// происхождение нечем).
func DeclaredPackage(src string) string {
	m := packageStmt.FindStringSubmatch(StripProtoComments(src))
	if m == nil {
		return ""
	}
	return m[1]
}

// IsOurPackage — объявленный пакет принадлежит нам.
func IsOurPackage(pkg string) bool {
	return isOurPackage(pkg)
}

// LeadingComment — головной блок комментария файла: всё до первого оператора.
// Уведомление первоисточника стоит именно здесь, и искать его дальше нельзя —
// слово «copyright» ниже по файлу принадлежит уже описанию полей.
func LeadingComment(src string) string {
	var b strings.Builder
	for _, line := range strings.Split(src, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "//") || strings.HasPrefix(t, "/*") ||
			strings.HasPrefix(t, "*") {
			b.WriteString(line)
			b.WriteByte('\n')
			continue
		}
		break
	}
	return b.String()
}

// HasCopyrightNotice — головной блок несёт строку правообладания.
func HasCopyrightNotice(src string) bool {
	return copyrightLine.MatchString(LeadingComment(src))
}

// VendorRootFor — корень вендоренного пространства для файла. Выводится из
// объявленного пакета: первый его сегмент отыскивается сегментом пути. Второе
// возвращаемое значение — выведен ли корень ПО ПРАВИЛУ (иначе взят каталог
// файла, и перепись обязана это назвать отдельно).
func VendorRootFor(rel, pkg string) (root string, derived bool) {
	segs := strings.Split(rel, "/")
	head, _, _ := strings.Cut(pkg, ".")
	if head != "" {
		// Ищем с конца: путь может нести сегмент-омоним выше по дереву.
		for i := len(segs) - 2; i >= 0; i-- {
			if segs[i] == head {
				return strings.Join(segs[:i+1], "/"), true
			}
		}
	}
	if len(segs) < 2 {
		return ".", false
	}
	return strings.Join(segs[:len(segs)-1], "/"), false
}

// licenseTitle — заглавная строка копии лицензии: первая непустая строка,
// оканчивающаяся словом «License». У части лицензий её нет — тогда ось сверки
// не выносится, и это печатается.
var licenseTitle = regexp.MustCompile(`(?im)^\s*(.{0,80}?\bLicense)\s*$`)

// LicenseTitle — как называет себя копия лицензии. Пустая строка означает, что
// заглавной строки нет и ось 3 по этому корню не выносится.
func LicenseTitle(licenseText string) string {
	m := licenseTitle.FindStringSubmatch(licenseText)
	if m == nil {
		return ""
	}
	return strings.Join(strings.Fields(m[1]), " ")
}

// normalizeNotice — приводит текст к виду, в котором сравнение названия лицензии
// не зависит от переносов, запятых и разметки комментария.
func normalizeNotice(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + ('a' - 'A'))
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
		default:
			b.WriteByte(' ')
		}
	}
	return " " + strings.Join(strings.Fields(b.String()), " ") + " "
}

// NoticeNamesLicense — уведомление файла называет ту же лицензию, что лежит
// копией рядом.
func NoticeNamesLicense(src, licenseTitleText string) bool {
	if licenseTitleText == "" {
		return true
	}
	return strings.Contains(normalizeNotice(LeadingComment(src)),
		strings.TrimSuffix(normalizeNotice(licenseTitleText), " ")+" ")
}

// VendorRoots — корни вендоренных пространств набора. Деривация живёт ЗДЕСЬ в
// единственном экземпляре: её зовёт и держатель уведомлений, и держатель
// предмета лицензии (licensesubject_test.go). Две копии одного вывода разошлись
// бы молча — и разошлись бы ровно там, где расхождение не видно: на новом
// вендоренном пространстве.
func VendorRoots(files []VendoredFile) map[string]bool {
	roots := map[string]bool{}
	for _, f := range files {
		pkg := DeclaredPackage(f.Source)
		if pkg == "" || IsOurPackage(pkg) {
			continue
		}
		root, _ := VendorRootFor(f.Rel, pkg)
		roots[root] = true
	}
	return roots
}

// VendoredFile — вход разбора: один прочитанный файл контракта.
type VendoredFile struct {
	// Rel — путь относительно корня репозитория.
	Rel string
	// Source — содержимое файла.
	Source string
}

// ScanVendoredNotices — разбирает набор файлов контракта и возвращает находки с
// переписью. `licenseAt` отвечает на вопрос «что лежит копией лицензии в этом
// корне»: пустая строка означает, что копии нет. Обращение к дереву вынесено в
// вызывающего намеренно — разбор обязан быть проверяем на синтетике.
func ScanVendoredNotices(files []VendoredFile, licenseAt func(root string) string) ([]VendoredNoticeFinding, VendoredNoticeCensus) {
	var out []VendoredNoticeFinding
	var c VendoredNoticeCensus

	roots := map[string]bool{}
	// Отсутствие копии лицензии — свойство КОРНЯ, а не каждого файла под ним:
	// иначе один недостающий файл давал бы столько находок, сколько контрактов
	// в пространстве, и перечень читался бы как четыре разных предмета.
	rootReported := map[string]bool{}

	for _, f := range files {
		c.FilesRead++
		pkg := DeclaredPackage(f.Source)
		if pkg == "" {
			c.PackageUndeclared++
			continue
		}
		if IsOurPackage(pkg) {
			c.Ours++
			continue
		}
		c.Vendored++

		root, derived := VendorRootFor(f.Rel, pkg)
		if !roots[root] {
			roots[root] = true
			c.VendorRoots++
			if derived {
				c.RootsDerivedFromPath++
			} else {
				c.RootsFellBackToDir++
			}
		}

		hasNotice := HasCopyrightNotice(f.Source)
		if hasNotice {
			c.NoticesFound++
		} else {
			out = append(out, VendoredNoticeFinding{
				File: f.Rel, Package: pkg, VendorRoot: root, Kind: "notice-missing",
				Detail: "головной блок комментария не несёт строки правообладания: " +
					"уведомление первоисточника вырезано (Apache-2.0 §4(c) требует его сохранить)",
			})
		}

		licenseText := licenseAt(root)
		if licenseText == "" {
			if !rootReported[root] {
				rootReported[root] = true
				out = append(out, VendoredNoticeFinding{
					File: f.Rel, Package: pkg, VendorRoot: root, Kind: "license-copy-missing",
					Detail: "в корне вендоренного пространства нет копии лицензии " +
						"(Apache-2.0 §4(a) требует передавать её получателю)",
				})
			}
			continue
		}

		title := LicenseTitle(licenseText)
		if title == "" {
			c.LicenseTitleUnknown++
			continue
		}
		c.MismatchChecked++
		if hasNotice && !NoticeNamesLicense(f.Source, title) {
			out = append(out, VendoredNoticeFinding{
				File: f.Rel, Package: pkg, VendorRoot: root, Kind: "notice-license-mismatch",
				Detail: "уведомление не называет лицензию, лежащую копией рядом: " + title,
			})
		}
	}
	return out, c
}
