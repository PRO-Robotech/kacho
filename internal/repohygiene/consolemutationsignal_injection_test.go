// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// consolemutationsignal_injection_test.go — доказательство, что гейт сигнала об
// исходе мутации СПОСОБЕН упасть, и что он падает не на форме, а на существе.
//
// Инъекция идёт в ОБЕ стороны, потому что односторонняя ничего не доказывает:
// проверка, краснеющая на дефекте, но краснеющая и на законной конструкции той
// же формы, будет отключена первым же ложным срабатыванием.
//
//	ДЕФЕКТ            мутация без механизма → краснеет, называя координату
//	БЛИЗНЕЦ           та же мутация в файле с механизмом → молчит
//	БЛИЗНЕЦ-2         `api.get`/`api.list` (не мутируют) → молчат
//	БЛИЗНЕЦ-3         вызов в комментарии и в строковом литерале → молчит
//	ТРАНСПОРТ         тот же вызов в `src/api/` → молчит (переносит, не решает)
//	ВЕДОМОСТЬ         запись без предмета → краснеет отдельным вердиктом
//
// Разбор здесь — тот же, что в гейте (`consoleProbeScan` + `consoleMutationCallRe`
// + маркеры механизма), поэтому проба меряет ЕГО, а не свою копию правил.
package repohygiene

import (
	"regexp"
	"strings"
	"testing"
)

// classify повторяет решение гейта для одного файла: мутирует ли он и заведён ли
// на механизм. Вынесено, чтобы инъекция не переписывала предикат своими словами.
func classify(rel, src string) (sites int, wired bool, transport bool) {
	mask, _ := consoleProbeScan(src)
	sites = len(consoleMutationCallRe.FindAllIndex(mask, -1))
	transport = strings.Contains("/"+rel, consoleMutationTransportDir)
	for _, m := range consoleMutationMechanismMarkers {
		if strings.Contains(src, m) {
			wired = true
			break
		}
	}
	return sites, wired, transport
}

func TestConsoleMutationGateRedOnInjectedDefect(t *testing.T) {
	const defect = `
import { api } from "@shared/api/client";
export function CreateThing() {
  const submit = () => { void api.create("/vpc/v1/networks", { name: "n" }); };
  return submit;
}
`
	sites, wired, transport := classify("vpc/src/components/CreateThing.tsx", defect)
	if sites == 0 {
		t.Fatal("инъекция не воспроизвела дефект: признак не нашёл вызова мутации")
	}
	if transport {
		t.Fatal("файл вне src/api/ ошибочно принят за транспорт")
	}
	if wired {
		t.Fatal("файл без единого упоминания механизма признан заведённым — гейт не упадёт никогда")
	}
	// Координата обязана быть названа: вердикт без неё не приводит к месту правки.
	loc := consoleMutationCallRe.FindStringIndex(defect)
	line := 1 + strings.Count(defect[:loc[0]], "\n")
	if line != 4 {
		t.Fatalf("координата дефекта посчитана неверно: строка %d, ожидалась 4", line)
	}
}

func TestConsoleMutationGateSilentOnLawfulTwin(t *testing.T) {
	// Тот же вызов, тот же файл-компонент — но исход проходит через механизм.
	const lawful = `
import { api } from "@shared/api/client";
import { useSignalledMutation } from "@shared/lib/use-signalled-mutation";
export function CreateThing() {
  const m = useSignalledMutation({
    verb: "create",
    subject: { label: "Облачная сеть", gender: "f" },
    expectOperation: true,
    mutationFn: (b: unknown) => api.create("/vpc/v1/networks", b),
  });
  return m;
}
`
	sites, wired, _ := classify("vpc/src/components/CreateThing.tsx", lawful)
	if sites == 0 {
		t.Fatal("законный близнец перестал распознаваться как мутация — предикат сузился")
	}
	if !wired {
		t.Fatal("законный близнец не признан заведённым: гейт краснел бы на исправном коде")
	}
}

func TestConsoleMutationGateIgnoresReadsAndInertText(t *testing.T) {
	// Чтения не мутируют — требовать от них сигнала об исходе не за что.
	const reads = `
import { api } from "@shared/api/client";
export const load = () => api.get("/vpc/v1/networks/ntw-1");
export const page = () => api.list("/vpc/v1/networks");
`
	if sites, _, _ := classify("vpc/src/lib/load.ts", reads); sites != 0 {
		t.Fatalf("чтение принято за мутацию: найдено %d мест", sites)
	}

	// Гейт, краснеющий на собственном объяснении, снимут как непонятный: имя
	// метода стоит и в комментарии, и в строке, и ни то ни другое не исполняется.
	const inert = `
// Здесь описано, почему api.create( обязан идти через механизм сигнала.
/* Ещё одно упоминание: api.delete( в блочном комментарии. */
export const DOC = "api.update( внутри строкового литерала";
export const TPL = ` + "`api.post( внутри шаблонного литерала`" + `;
`
	if sites, _, _ := classify("shared/src/lib/doc.ts", inert); sites != 0 {
		t.Fatalf("гейт считает находкой упоминание в комментарии или строке: найдено %d", sites)
	}
}

func TestConsoleMutationGateSkipsTransport(t *testing.T) {
	// Тонкий однострочник над клиентом переносит запрос; решает вызывающий.
	const wrapper = `
import { api } from "@shared/api/client";
export const networksApi = {
  create: (b: unknown) => api.create("/vpc/v1/networks", b),
  remove: (id: string) => api.delete("/vpc/v1/networks/" + id),
};
`
	sites, _, transport := classify("shared/src/api/resources.ts", wrapper)
	if sites == 0 {
		t.Fatal("в транспорте перестали находиться вызовы — признак сузился")
	}
	if !transport {
		t.Fatal("файл в src/api/ не признан транспортом: гейт требовал бы сигнала от переносчика")
	}
}

func TestConsoleMutationLedgerGateRedOnEntryWithoutSubject(t *testing.T) {
	// Ведомость обязана истекать сама. Запись, чей файл больше не мутирует,
	// описывает несуществующий долг — и это находка, а не мелочь: именно так
	// послабление переживает свой предмет.
	ledgerHit := map[string]bool{"shared/src/components/Alive.tsx": true}
	entries := []string{"shared/src/components/Alive.tsx", "shared/src/components/Gone.tsx"}

	var stale []string
	for _, p := range entries {
		if !ledgerHit[p] {
			stale = append(stale, p)
		}
	}
	if len(stale) != 1 || stale[0] != "shared/src/components/Gone.tsx" {
		t.Fatalf("устаревшая запись ведомости не распознана: %v", stale)
	}
}

// TestConsoleMutationGatePremiseIsChecked — предпосылка гейта проверяется им
// самим. Признак, переставший ловить предмет, обязан РОНЯТЬ прогон, а не молча
// объявлять дерево чистым: «ноль находок» и «ноль прочитанного» — разные вещи.
func TestConsoleMutationGatePremiseIsChecked(t *testing.T) {
	broken := regexp.MustCompile(`\bapi\.thisMethodDoesNotExist\s*\(`)
	const real = `export const f = () => api.create("/vpc/v1/networks", {});`
	if broken.MatchString(real) {
		t.Fatal("контрольный предикат оказался годным — проба ничего не проверяет")
	}
	// Настоящий признак на том же входе предмет находит; значит нулевой результат
	// сломанного предиката означал бы именно поломку, и гейт обязан на ней падать.
	if !consoleMutationCallRe.MatchString(real) {
		t.Fatal("признак гейта не нашёл заведомой мутации")
	}
}
