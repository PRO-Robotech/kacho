// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// vendorednotice_injection_test.go — доказательство того, что держатель класса
// «вендоренный чужой файл без уведомления» СПОСОБЕН упасть И смолчать.
//
// Инъекция снимает НОВОЕ свойство у элемента, чьи СТАРЫЕ на месте: уведомление
// вырезается у уже существующего вендоренного контракта, а не заводится новый
// файл целиком. Форма «завести ещё один файл» здесь запрещена — новый файл
// нарушал бы всё, что требуется от контрактов вообще, и красное пришло бы от
// соседа, ничего не сказав о ЭТОМ гейте.
//
// Законный близнец стоит у КАЖДОЙ оси: без него отрицание зеленело бы на
// разборе, отвергающем всё подряд.
package repohygiene

import (
	"strings"
	"testing"
)

// apacheNotice — уведомление первоисточника, дословно как в дереве.
const apacheNotice = `// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

`

// vendoredBody — тело вендоренного контракта, каково оно есть.
const vendoredBody = `syntax = "proto3";

package google.rpc;

option go_package = "google.golang.org/genproto/googleapis/rpc/status;status";

message Status {
  int32 code = 1;
}
`

// ourBody — НАШ контракт. Уведомления первоисточника не несёт и нести не обязан:
// чужого текста в нём нет. Законный близнец оси 1.
const ourBody = `syntax = "proto3";

package kacho.cloud.vpc.v1;

option go_package = "github.com/PRO-Robotech/kacho/pkg/api/vpc/v1;vpcv1";

message Network {
  string id = 1;
}
`

// ourBodyMentioningAForeignPackage — НАШ контракт, чья проза называет чужой
// пакет. Законный близнец разбора объявления: гейт, судящий подстроку, признал
// бы этот файл вендоренным и потребовал бы от него чужого уведомления.
const ourBodyMentioningAForeignPackage = `// Здесь объяснено, почему поле совместимо с package google.rpc:
// форма ответа повторяет чужую намеренно.
/* package google.api — второй способ записи того же слова в комментарии. */
syntax = "proto3";

package kaname.cloud.iam.v1;

message Reply {
  int32 code = 1;
}
`

const (
	vendoredRel = "proto/google/rpc/status.proto"
	ourRel      = "proto/kacho/cloud/vpc/v1/network.proto"
	vendorRoot  = "proto/google"
)

// apacheCopy — заглавная часть копии Apache-2.0: гейт берёт название лицензии
// отсюда, а не выписывает его. Полный текст для разбора названия не нужен.
const apacheCopy = `
                                 Apache License
                           Version 2.0, January 2004
                        http://www.apache.org/licenses/

   TERMS AND CONDITIONS FOR USE, REPRODUCTION, AND DISTRIBUTION
`

// bsdCopy — копия лицензии БЕЗ заглавной строки. Ось сверки названия по такому
// корню НЕ выносится, и перепись обязана это назвать.
const bsdCopy = `Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are met:
`

// licenseThere — копия лицензии лежит в корне пространства.
func licenseThere(root string) string {
	if root == vendorRoot {
		return apacheCopy
	}
	return ""
}

// licenseNowhere — копии лицензии нет нигде.
func licenseNowhere(string) string { return "" }

func vendoredKindsOf(fs []VendoredNoticeFinding) []string {
	out := make([]string, 0, len(fs))
	for _, f := range fs {
		out = append(out, f.File+"["+f.Kind+"]")
	}
	return out
}

// TestVendoredNoticeControlIsSilent — контроль: всё цело, гейт молчит, и объём
// осмотренного НЕ нулевой. Без этой пробы молчание гейта неотличимо от молчания
// мёртвого гейта.
func TestVendoredNoticeControlIsSilent(t *testing.T) {
	files := []VendoredFile{
		{Rel: vendoredRel, Source: apacheNotice + vendoredBody},
		{Rel: ourRel, Source: ourBody},
	}
	got, c := ScanVendoredNotices(files, licenseThere)
	if len(got) != 0 {
		t.Fatalf("контроль обязан молчать, а гейт нашёл %v", vendoredKindsOf(got))
	}
	if c.Vendored != 1 || c.Ours != 1 || c.NoticesFound != 1 || c.MismatchChecked != 1 {
		t.Fatalf("перепись контроля разошлась: вендоренных %d, наших %d, уведомлений %d, "+
			"сверок названия %d", c.Vendored, c.Ours, c.NoticesFound, c.MismatchChecked)
	}
}

// TestVendoredNoticeRedsWhenTheNoticeIsCutOut — ось 1: уведомление вырезано.
// Инъекция снимает ОДИН факт — головной блок комментария; всё остальное у файла
// на месте (пакет объявлен, копия лицензии лежит).
func TestVendoredNoticeRedsWhenTheNoticeIsCutOut(t *testing.T) {
	files := []VendoredFile{
		{Rel: vendoredRel, Source: vendoredBody}, // шапка снята
		{Rel: ourRel, Source: ourBody},
	}
	got, c := ScanVendoredNotices(files, licenseThere)
	if len(got) != 1 {
		t.Fatalf("инъекция обязана дать РОВНО одну находку, дала %d: %v", len(got), vendoredKindsOf(got))
	}
	if got[0].Kind != "notice-missing" {
		t.Fatalf("ось не та: %s", got[0].Kind)
	}
	// Находка обязана НАЗЫВАТЬ координату: перечень без имени файла посылает
	// читателя искать самому.
	if got[0].File != vendoredRel {
		t.Fatalf("находка не назвала координату: %q", got[0].File)
	}
	if got[0].VendorRoot != vendorRoot {
		t.Fatalf("корень пространства выведен неверно: %q", got[0].VendorRoot)
	}
	// Инъекция обязана ронять ТОЛЬКО проверяемое: наш файл рядом молчит.
	if c.Ours != 1 {
		t.Fatalf("наш файл перестал опознаваться нашим: %d", c.Ours)
	}
}

// TestVendoredNoticeStaysSilentOnOurOwnContract — законный близнец оси 1: НАШ
// контракт уведомления первоисточника не несёт и нести не обязан. Без этой
// пробы гейт был бы неотличим от требования чужой шапки от всего дерева.
func TestVendoredNoticeStaysSilentOnOurOwnContract(t *testing.T) {
	files := []VendoredFile{{Rel: ourRel, Source: ourBody}}
	got, c := ScanVendoredNotices(files, licenseNowhere)
	if len(got) != 0 {
		t.Fatalf("наш контракт обязан молчать, а гейт нашёл %v", vendoredKindsOf(got))
	}
	if c.Ours != 1 || c.Vendored != 0 {
		t.Fatalf("классификатор разошёлся: наших %d, вендоренных %d", c.Ours, c.Vendored)
	}
}

// TestVendoredNoticeReadsTheStatementNotTheProse — законный близнец разбора: наш
// контракт, чья ПРОЗА называет чужой пакет, вендоренным не становится. Гейт,
// судящий подстроку, краснел бы на собственном объяснении.
func TestVendoredNoticeReadsTheStatementNotTheProse(t *testing.T) {
	files := []VendoredFile{{Rel: ourRel, Source: ourBodyMentioningAForeignPackage}}
	got, c := ScanVendoredNotices(files, licenseNowhere)
	if len(got) != 0 {
		t.Fatalf("файл с чужим пакетом В КОММЕНТАРИИ обязан молчать, а гейт нашёл %v", vendoredKindsOf(got))
	}
	if c.Ours != 1 {
		t.Fatalf("разбор объявления сорвался на комментарии: наших %d, вендоренных %d",
			c.Ours, c.Vendored)
	}
}

// TestVendoredNoticeRedsWhenTheLicenseCopyIsAbsent — ось 2: §4(a). Инъекция
// снимает ОДИН факт — копию лицензии в корне; уведомление у файла на месте.
func TestVendoredNoticeRedsWhenTheLicenseCopyIsAbsent(t *testing.T) {
	files := []VendoredFile{{Rel: vendoredRel, Source: apacheNotice + vendoredBody}}
	got, _ := ScanVendoredNotices(files, licenseNowhere)
	if len(got) != 1 || got[0].Kind != "license-copy-missing" {
		t.Fatalf("ожидалась одна находка license-copy-missing, получено %v", vendoredKindsOf(got))
	}
	if got[0].VendorRoot != vendorRoot {
		t.Fatalf("находка не назвала корень: %q", got[0].VendorRoot)
	}
}

// TestVendoredMissingLicenseIsReportedOncePerRoot — отсутствие копии есть
// свойство КОРНЯ. Без этого перечень из четырёх строк читался бы как четыре
// разных предмета, и починка одного выглядела бы неполной.
func TestVendoredMissingLicenseIsReportedOncePerRoot(t *testing.T) {
	files := []VendoredFile{
		{Rel: "proto/google/rpc/status.proto", Source: apacheNotice + vendoredBody},
		{Rel: "proto/google/api/http.proto", Source: apacheNotice +
			strings.Replace(vendoredBody, "google.rpc", "google.api", 1)},
	}
	got, c := ScanVendoredNotices(files, licenseNowhere)
	if len(got) != 1 {
		t.Fatalf("на два файла одного корня ожидалась ОДНА находка, получено %d: %v",
			len(got), vendoredKindsOf(got))
	}
	if c.Vendored != 2 || c.VendorRoots != 1 {
		t.Fatalf("перепись разошлась: вендоренных %d, корней %d", c.Vendored, c.VendorRoots)
	}
}

// TestVendoredNoticeRedsWhenItNamesAnotherLicense — ось 3: уведомление есть,
// копия лежит, но названа в них РАЗНАЯ лицензия. Один снятый факт — название
// лицензии в уведомлении.
func TestVendoredNoticeRedsWhenItNamesAnotherLicense(t *testing.T) {
	wrong := strings.Replace(apacheNotice,
		"Licensed under the Apache License, Version 2.0",
		"Licensed under the Mozilla Public License, Version 2.0", 1)
	files := []VendoredFile{{Rel: vendoredRel, Source: wrong + vendoredBody}}
	got, c := ScanVendoredNotices(files, licenseThere)
	if len(got) != 1 || got[0].Kind != "notice-license-mismatch" {
		t.Fatalf("ожидалась одна находка notice-license-mismatch, получено %v", vendoredKindsOf(got))
	}
	if c.MismatchChecked != 1 {
		t.Fatalf("ось сверки не вынесена: %d", c.MismatchChecked)
	}
}

// TestVendoredNoticeNamesItsOwnBoundary — граница оси 3 НАЗВАНА, а не спрятана:
// копия лицензии без заглавной строки уводит пару в счётчик «вердикт не
// вынесен», и находка при этом НЕ выдумывается. Ноль находок здесь означает
// «сверять было нечем», и перепись обязана это отличать.
func TestVendoredNoticeNamesItsOwnBoundary(t *testing.T) {
	files := []VendoredFile{{Rel: vendoredRel, Source: apacheNotice + vendoredBody}}
	got, c := ScanVendoredNotices(files, func(string) string { return bsdCopy })
	if len(got) != 0 {
		t.Fatalf("по копии без заглавной строки ось не выносится, а гейт нашёл %v", vendoredKindsOf(got))
	}
	if c.LicenseTitleUnknown != 1 || c.MismatchChecked != 0 {
		t.Fatalf("граница не названа: не вынесено %d, вынесено %d",
			c.LicenseTitleUnknown, c.MismatchChecked)
	}
}

// TestVendoredNoticeBoundaryDoesNotMaskAFinding — проба антимаски: там, где
// рядом и находка, и невынесенная ось, находка объявляется. Граница обязана
// быть границей, а не способом снять проверку.
func TestVendoredNoticeBoundaryDoesNotMaskAFinding(t *testing.T) {
	files := []VendoredFile{
		{Rel: vendoredRel, Source: vendoredBody}, // уведомления нет — ось 1
		{Rel: "proto/google/api/http.proto", Source: apacheNotice +
			strings.Replace(vendoredBody, "google.rpc", "google.api", 1)},
	}
	got, c := ScanVendoredNotices(files, func(string) string { return bsdCopy })
	if len(got) != 1 || got[0].Kind != "notice-missing" {
		t.Fatalf("находка замаскирована границей: %v", vendoredKindsOf(got))
	}
	if c.LicenseTitleUnknown != 2 {
		t.Fatalf("перепись границы разошлась: %d", c.LicenseTitleUnknown)
	}
}

// TestVendorRootIsDerivedNotDefaulted — корень выводится ПО ПРАВИЛУ, и умолчание
// отличимо от вывода. Без этого «взято каталогом файла» читалось бы как «выведено».
func TestVendorRootIsDerivedNotDefaulted(t *testing.T) {
	root, derived := VendorRootFor("proto/google/rpc/status.proto", "google.rpc")
	if root != "proto/google" || !derived {
		t.Fatalf("вывод по сегменту пути сорвался: %q derived=%v", root, derived)
	}
	// Пакет, чей первый сегмент в пути не встречается: корень берётся каталогом
	// файла, и это ОБЪЯВЛЯЕТСЯ вторым значением, а не выдаётся за вывод.
	root, derived = VendorRootFor("proto/vendored/x/y.proto", "envoy.config")
	if root != "proto/vendored/x" || derived {
		t.Fatalf("умолчание не названо умолчанием: %q derived=%v", root, derived)
	}
}

// TestVendoredEmptyInputIsNotCleanliness — разбор пустого набора не выдаёт
// «чисто»: перепись остаётся нулевой, и держатель дерева на ней падает.
func TestVendoredEmptyInputIsNotCleanliness(t *testing.T) {
	got, c := ScanVendoredNotices(nil, licenseThere)
	if len(got) != 0 || c.FilesRead != 0 || c.Ours != 0 {
		t.Fatalf("пустой вход разобран неверно: находок %d, перепись %+v", len(got), c)
	}
}

// TestLicenseTitleComesFromTheCopy — название лицензии берётся ИЗ КОПИИ, а не
// выписано в гейте: зашитое слово «Apache» судило бы будущий чужой файл по
// чужой мерке.
func TestLicenseTitleComesFromTheCopy(t *testing.T) {
	if got := LicenseTitle(apacheCopy); got != "Apache License" {
		t.Fatalf("название лицензии прочитано неверно: %q", got)
	}
	if got := LicenseTitle(bsdCopy); got != "" {
		t.Fatalf("у копии без заглавной строки название обязано быть пустым, получено %q", got)
	}
}
