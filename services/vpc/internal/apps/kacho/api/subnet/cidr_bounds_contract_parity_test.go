// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subnet

// cidr_bounds_contract_parity_test.go — обещание контракта и константа валидатора
// сверяются машинно.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Границу размера подсети называют ДВА места: проза контракта («Minimum /28,
// maximum /16» у `ipv4_cidr_primary` — в проекции `Subnet` и в
// `CreateSubnetRequest`) и предикат валидатора. Два места об одном предмете
// расходятся молча, и расходятся они там, где расхождение не видно: обе стороны
// «работают», просто обещают разное. Ровно это и случилось — обещание называло
// две границы, предикат исполнял одну, и ни одна проба этого не видела.
//
// Проба читает ПРОЗУ КОНТРАКТА и требует, чтобы названные в ней числа совпали с
// константами. Правка одной стороны без другой краснеет с координатой.
//
// ЧТО ЭТА ПРОБА ДЕЛАЕТ САМОИСТЕКАЮЩИМ
//
// Отсутствие границы для IPv6 — послабление, у которого есть основание: контракт
// её не называет. Основание проверяется здесь же: обещание, появившееся у
// v6-поля, краснит пробу и требует исполнения. То есть послабление не переживёт
// своего основания, и снимать его руками никому не придётся.
//
// ПОЧЕМУ ОБЪЁМ ОСМОТРЕННОГО УТВЕРЖДАЕТСЯ ОТДЕЛЬНО
//
// Проба, которая ничего не нашла, зелена по построению. Поэтому она требует
// нижних границ переписи: сколько файлов прочитано, сколько CIDR-полей увидено,
// сколько обещаний разобрано. Переезд контракта, переименование поля или смена
// формулировки обнулят находки — и это обязано быть отличимо от «всё сходится».

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// promisedRangeRe — форма, которой контракт называет диапазон РАЗМЕРА сети.
// Первое число — самая маленькая сеть (самый длинный префикс), второе — самая
// большая (самый короткий).
var promisedRangeRe = regexp.MustCompile(`(?i)minimum\s*/(\d+)\s*,\s*maximum\s*/(\d+)`)

// cidrFieldRe — объявление строкового CIDR-поля в proto.
var cidrFieldRe = regexp.MustCompile(`^\s*(?:repeated\s+)?string\s+(ipv[46]_cidr_\w+)\s*=\s*\d+`)

// messageRe — начало объявления сообщения; нужно, чтобы поле относилось к своему
// РЕСУРСУ. Без этого CIDR-поля сети (её супернет — другой предмет с другими
// величинами) судились бы константами подсети.
var messageRe = regexp.MustCompile(`^message\s+(\w+)\s*\{`)

// subnetMessage — сообщение принадлежит ресурсу Subnet. Область гейта названа
// именем ресурса, а не именем файла: переезд контракта между файлами предмет
// сверки не меняет, а переименование ресурса обрушит перепись и будет замечено.
func subnetMessage(name string) bool { return strings.Contains(name, "Subnet") }

// promisedBound — обещание, привязанное к полю и координате.
type promisedBound struct {
	file           string
	line           int
	field          string
	smallestNetLen int // «Minimum /N» — самый ДЛИННЫЙ допустимый префикс
	largestNetLen  int // «maximum /N» — самый КОРОТКИЙ допустимый префикс
}

// protoCensus — перепись прочитанного, чтобы «ноль находок» было отличимо от
// «ноль прочитанного».
type protoCensus struct {
	files      int
	cidrFields int
	bounds     []promisedBound
}

// scanPromisedBounds разбирает содержимое одного proto-файла: комментарный блок,
// стоящий непосредственно перед объявлением CIDR-поля, и обещание в нём.
//
// Разбор идёт по СТРУКТУРЕ (блок комментария → объявление поля), а не поиском
// числа по всему файлу: иначе обещание у одного поля зачлось бы другому, и проба
// перестала бы отличать поля друг от друга — а именно этим v4 и отличается от v6.
func scanPromisedBounds(file, content string) (int, []promisedBound, error) {
	lines := strings.Split(content, "\n")
	var block []string
	var message string
	var fields int
	var out []promisedBound
	for i, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		if strings.HasPrefix(trimmed, "//") {
			block = append(block, strings.TrimPrefix(trimmed, "//"))
			continue
		}
		if mm := messageRe.FindStringSubmatch(ln); mm != nil {
			message = mm[1]
			block = nil
			continue
		}
		m := cidrFieldRe.FindStringSubmatch(ln)
		if m == nil {
			if trimmed != "" {
				block = nil // не комментарий и не поле — блок больше не «непосредственно перед»
			}
			continue
		}
		if !subnetMessage(message) {
			block = nil // CIDR-поле другого ресурса — не предмет этих констант
			continue
		}
		fields++
		if mm := promisedRangeRe.FindStringSubmatch(strings.Join(block, " ")); mm != nil {
			smallest, err := strconv.Atoi(mm[1])
			if err != nil {
				return fields, out, fmt.Errorf("%s:%d: «Minimum /%s» не число", file, i+1, mm[1])
			}
			largest, err := strconv.Atoi(mm[2])
			if err != nil {
				return fields, out, fmt.Errorf("%s:%d: «maximum /%s» не число", file, i+1, mm[2])
			}
			out = append(out, promisedBound{
				file: file, line: i + 1, field: m[1],
				smallestNetLen: smallest, largestNetLen: largest,
			})
		}
		block = nil
	}
	return fields, out, nil
}

// vpcProtoDir — каталог контракта домена, найденный от корня модуля. Корень
// определяется по `go.mod`: относительная глубина пакета — величина, которая
// меняется при любом переезде каталога, и зашитая в пробу она сделала бы её
// красной по причине, не имеющей отношения к предмету.
func vpcProtoDir(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			protoDir := filepath.Join(dir, "proto", "kacho", "cloud", "vpc", "v1")
			st, serr := os.Stat(protoDir)
			require.NoError(t, serr, "каталог контракта домена не найден от корня модуля %s", dir)
			require.True(t, st.IsDir())
			return protoDir
		}
		parent := filepath.Dir(dir)
		require.NotEqual(t, parent, dir, "корень модуля (go.mod) не найден вверх от %s", dir)
		dir = parent
	}
}

// TestSubnetCidrBoundsMatchTheContract — обещание прозы контракта == константы
// валидатора; обещание у v6-поля — находка.
func TestSubnetCidrBoundsMatchTheContract(t *testing.T) {
	protoDir := vpcProtoDir(t)
	entries, err := treecorpus.Glob(filepath.Join(protoDir, "*.proto"))
	require.NoError(t, err)

	var census protoCensus
	for _, path := range entries {
		raw, rerr := os.ReadFile(path)
		require.NoError(t, rerr)
		census.files++
		fields, bounds, serr := scanPromisedBounds(filepath.Base(path), string(raw))
		require.NoError(t, serr)
		census.cidrFields += fields
		census.bounds = append(census.bounds, bounds...)
	}

	// Перепись — отдельное утверждение. Без него переезд контракта или
	// переименование поля дали бы ноль находок, и проба осталась бы зелёной,
	// ничего не прочитав.
	t.Logf("осмотрено: файлов контракта %d, CIDR-полей подсети %d, обещаний диапазона %d",
		census.files, census.cidrFields, len(census.bounds))
	require.GreaterOrEqual(t, census.files, 2,
		"прочитано меньше двух файлов контракта — предмет сверки не найден, а не «сходится»")
	require.GreaterOrEqual(t, census.cidrFields, 10,
		"CIDR-полей подсети найдено %d, а контракт несёт их десять (по две семьи × "+
			"primary/blocks в `Subnet`, пара якорей в `CreateSubnetRequest`, по паре в "+
			"`Add`/`RemoveSubnetCidrBlocksRequest`). Меньше — значит разбор объявлений или "+
			"распознавание ресурса сломано, а не контракт похудел", census.cidrFields)

	var v4 int
	for _, b := range census.bounds {
		switch {
		case strings.HasPrefix(b.field, "ipv4_"):
			v4++
			assert.Equal(t, subnetV4PrefixLenMax, b.smallestNetLen,
				"%s:%d (%s): контракт обещает «Minimum /%d» — самый длинный префикс, — "+
					"а валидатор держит /%d. Правка одной стороны без другой оставляет два "+
					"места об одном предмете, из которых верно одно",
				b.file, b.line, b.field, b.smallestNetLen, subnetV4PrefixLenMax)
			assert.Equal(t, subnetV4PrefixLenMin, b.largestNetLen,
				"%s:%d (%s): контракт обещает «maximum /%d» — самый короткий префикс, — "+
					"а валидатор держит /%d",
				b.file, b.line, b.field, b.largestNetLen, subnetV4PrefixLenMin)
		case strings.HasPrefix(b.field, "ipv6_"):
			t.Errorf("%s:%d (%s): контракт назвал диапазон для IPv6 («Minimum /%d, maximum /%d»), "+
				"а `validateSubnetV6CIDR` его не исполняет. Отсутствие v6-границы держалось "+
				"ровно тем, что контракт её не обещал — основание кончилось, значит границу "+
				"надо исполнить (и назвать константами рядом с v4)",
				b.file, b.line, b.field, b.smallestNetLen, b.largestNetLen)
		default:
			t.Errorf("%s:%d: поле %q не отнесено ни к одной семье — распознавание семьи "+
				"разошлось с именами контракта", b.file, b.line, b.field)
		}
	}
	require.GreaterOrEqual(t, v4, 2,
		"обещаний диапазона у v4-полей найдено %d: контракт называет его и в проекции "+
			"`Subnet`, и в `CreateSubnetRequest`. Ноль или одно означает, что проба читает "+
			"не то, а не что обещание сняли", v4)
}

// TestPromisedBoundExtractorReadsTheNumbersItIsGiven — положительный контроль
// разбора: без него сверка выше зеленела бы и на извлекателе, который всегда
// возвращает те же числа, что и константы, — то есть на тождестве вместо сверки.
//
// Вход синтетический и НАМЕРЕННО не совпадает с константами: если бы извлекатель
// подставлял их, обе пары ниже вышли бы равными им.
func TestPromisedBoundExtractorReadsTheNumbersItIsGiven(t *testing.T) {
	const synthetic = `
message CreateSubnetRequest {
  // Primary IPv4 CIDR anchor. Minimum /27, maximum /20.
  string ipv4_cidr_primary = 14;

  // Primary IPv6 CIDR anchor (immutable). Minimum /64, maximum /48.
  string ipv6_cidr_primary = 15;

  // Additional ranges, no promise here.
  repeated string ipv4_cidr_blocks = 17;
}
`
	fields, bounds, err := scanPromisedBounds("synthetic.proto", synthetic)
	require.NoError(t, err)
	require.Equal(t, 3, fields, "все три объявления обязаны быть увидены")
	require.Len(t, bounds, 2, "обещание разбирается ровно у тех полей, перед которыми стоит")

	assert.Equal(t, "ipv4_cidr_primary", bounds[0].field)
	assert.Equal(t, 27, bounds[0].smallestNetLen, "числа читаются из текста, а не подставляются")
	assert.Equal(t, 20, bounds[0].largestNetLen)

	assert.Equal(t, "ipv6_cidr_primary", bounds[1].field,
		"семья различается по имени поля — иначе v6-обещание зачлось бы v4")
	assert.Equal(t, 64, bounds[1].smallestNetLen)
	assert.Equal(t, 48, bounds[1].largestNetLen)
}

// TestPromisedBoundExtractorDoesNotBorrowAForeignComment — отрицание рядом с
// положительным: обещание, отделённое от объявления другим кодом, к этому
// объявлению НЕ прилипает. Иначе один комментарий в файле делал бы «сходится» по
// всем полям сразу, и проба перестала бы различать поля — а различение полей и
// есть то, чем она отличает v4 от v6.
func TestPromisedBoundExtractorDoesNotBorrowAForeignComment(t *testing.T) {
	const synthetic = `
message CreateSubnetRequest {
  // Minimum /27, maximum /20.
  string zone_id = 8;

  repeated string ipv4_cidr_blocks = 17;
}
`
	fields, bounds, err := scanPromisedBounds("synthetic.proto", synthetic)
	require.NoError(t, err)
	assert.Equal(t, 1, fields, "CIDR-поле здесь одно")
	assert.Empty(t, bounds, "обещание стоит перед ЧУЖИМ объявлением и не может зачесться CIDR-полю")
}

// TestPromisedBoundExtractorIgnoresAnotherResource — вторая половина области:
// CIDR-поле ДРУГОГО ресурса под эти константы не подпадает. Супернет сети —
// другой предмет с другими величинами, и обещание на нём не может краснить
// сверку подсети.
//
// Рядом — положительный близнец той же формы: то же поле в сообщении подсети
// разбирается. Без него «игнорируется» зеленело бы и на разборе, который не
// видит вообще ничего.
func TestPromisedBoundExtractorIgnoresAnotherResource(t *testing.T) {
	const foreign = `
message AddNetworkCidrBlocksRequest {
  // Supernet blocks. Minimum /27, maximum /20.
  repeated string ipv4_cidr_blocks = 2;
}
`
	fields, bounds, err := scanPromisedBounds("network_service.proto", foreign)
	require.NoError(t, err)
	assert.Equal(t, 0, fields, "поле сети в перепись подсети не входит")
	assert.Empty(t, bounds, "обещание сети не судится константами подсети")

	const own = `
message AddSubnetCidrBlocksRequest {
  // Blocks to add. Minimum /27, maximum /20.
  repeated string ipv4_cidr_blocks = 2;
}
`
	fields, bounds, err = scanPromisedBounds("subnet_service.proto", own)
	require.NoError(t, err)
	assert.Equal(t, 1, fields, "то же поле в сообщении ПОДСЕТИ обязано быть увидено")
	require.Len(t, bounds, 1)
	assert.Equal(t, 27, bounds[0].smallestNetLen)
	assert.Equal(t, 20, bounds[0].largestNetLen)
}
