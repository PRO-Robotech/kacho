// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// anon_mail_bounds.go — ручки края из таблицы Р8 приёмки NTF-2 и их границы:
// ОДНА таблица на дерево (замысел issue-2917, З23; CX2-22).
//
// Таблицу читают три места, и второго объявления границ нет ни у одного:
//
//   - страж старта ResolveEdgeLimits — наличие и граница каждой ручки, затем
//     отношения между ручками; сообщение называет ключ и нарушенную границу;
//   - перепись отсутствия (вариант (т) сценария 71) — по строке на ключ;
//   - срок хранения моментов края (З26) — AnonMailWindowUpperBound берёт
//     верхнюю границу окон отсюда, а не из настроенного значения.
//
// Умолчаний в бинаре нет (Р8): незаданная ручка — отказ старта, а не «разумное
// значение». Ключей края — 19: источник 6, сложность 2, подсеть 8, общий поток
// 2, доверенные прыжки 1.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Границы Р8 — константы таблицы; каждая названа один раз.
const (
	// anonMailCountFloor — нижняя граница любого счёта: счёт пропущенных
	// запросов, начиная с которого действует ступень. Ноль и меньше — ступень
	// на первом же запросе, то есть не предел, а запрет.
	anonMailCountFloor = 1
	// anonMailWindowFloor и anonMailWindowCeiling — границы каждого окна
	// источника и подсети.
	anonMailWindowFloor   = time.Minute
	anonMailWindowCeiling = 24 * time.Hour
	// anonMailPoWBitsFloor и anonMailPoWBitsCeiling — границы сложности вызова.
	anonMailPoWBitsFloor   = 8
	anonMailPoWBitsCeiling = 24
	// anonMailSourceHardPerHour и anonMailSubnetHardPerHour — потолок темпа
	// «HARD за окно HARD», приведённого к часу.
	anonMailSourceHardPerHour = 1000
	anonMailSubnetHardPerHour = 10000
	// anonMailPoWKeyMinBytes — нижняя граница длины ключа подписи вызова (З9).
	anonMailPoWKeyMinBytes = 32
)

// Имена ручек края. Каждое совпадает с тегом своего поля Config; совпадение
// держит проба TestEdgeKnobRowReadsItsOwnField.
const (
	knobIPFreeLimit         = "KACHO_API_GATEWAY_ANON_MAIL_IP_FREE_LIMIT"
	knobIPPoWLimit          = "KACHO_API_GATEWAY_ANON_MAIL_IP_POW_LIMIT"
	knobIPHardLimit         = "KACHO_API_GATEWAY_ANON_MAIL_IP_HARD_LIMIT"
	knobIPFreeWindow        = "KACHO_API_GATEWAY_ANON_MAIL_IP_FREE_WINDOW"
	knobIPPoWWindow         = "KACHO_API_GATEWAY_ANON_MAIL_IP_POW_WINDOW"
	knobIPHardWindow        = "KACHO_API_GATEWAY_ANON_MAIL_IP_HARD_WINDOW"
	knobPoWBitsBase         = "KACHO_API_GATEWAY_ANON_MAIL_POW_BITS_BASE"
	knobPoWBitsHigh         = "KACHO_API_GATEWAY_ANON_MAIL_POW_BITS_HIGH"
	knobSubnetV4Len24PoW    = "KACHO_API_GATEWAY_ANON_MAIL_SUBNET_V4_24_POW_LIMIT"
	knobSubnetV4Len24Hard   = "KACHO_API_GATEWAY_ANON_MAIL_SUBNET_V4_24_HARD_LIMIT"
	knobSubnetV6Len56PoW    = "KACHO_API_GATEWAY_ANON_MAIL_SUBNET_V6_56_POW_LIMIT"
	knobSubnetV6Len56Hard   = "KACHO_API_GATEWAY_ANON_MAIL_SUBNET_V6_56_HARD_LIMIT"
	knobSubnetV6Len48PoW    = "KACHO_API_GATEWAY_ANON_MAIL_SUBNET_V6_48_POW_LIMIT"
	knobSubnetV6Len48Hard   = "KACHO_API_GATEWAY_ANON_MAIL_SUBNET_V6_48_HARD_LIMIT"
	knobSubnetPoWWindow     = "KACHO_API_GATEWAY_ANON_MAIL_SUBNET_POW_WINDOW"
	knobSubnetHardWindow    = "KACHO_API_GATEWAY_ANON_MAIL_SUBNET_HARD_WINDOW"
	knobGlobalRatePerSecond = "KACHO_API_GATEWAY_ANON_MAIL_GLOBAL_RATE_PER_SECOND"
	knobGlobalBurst         = "KACHO_API_GATEWAY_ANON_MAIL_GLOBAL_BURST"
)

// AnonMailPoWKeyFileKnob — имя ручки файла ключа подписи вызовов (З9).
const AnonMailPoWKeyFileKnob = "KACHO_API_GATEWAY_ANON_MAIL_POW_KEY_FILE"

// anonMailPoWKeyRedactedTag — то, что печатается вместо ключа.
const anonMailPoWKeyRedactedTag = "[redacted]"

// EdgeLimits — 19 ключей Р8 края, разобранные и проверенные по границам.
// Строится только ResolveEdgeLimits.
type EdgeLimits struct {
	// TrustedHops — число доверенных прыжков: читают и модель прав, и
	// ограничитель (З8).
	TrustedHops TrustedHops
	// AnonMail — пределы ограничителя анонимной почты (Р5).
	AnonMail AnonMailLimits

	// effective — действующие значения ручек в порядке таблицы, для журнала
	// старта (близнец сценария 71).
	effective []slog.Attr
}

// AnonMailLimits — пределы ограничителя анонимной почты края (Р5, Р8).
type AnonMailLimits struct {
	Source           AnonMailSourceLimits
	PoWBits          AnonMailPoWBits
	SubnetV4Len24    AnonMailSubnetLimits
	SubnetV6Len56    AnonMailSubnetLimits
	SubnetV6Len48    AnonMailSubnetLimits
	SubnetPoWWindow  time.Duration
	SubnetHardWindow time.Duration
	Global           AnonMailGlobalFlow
}

// AnonMailSourceLimits — ось источника: три ступени и их окна.
type AnonMailSourceLimits struct {
	Free, PoW, Hard                   int
	FreeWindow, PoWWindow, HardWindow time.Duration
}

// AnonMailPoWBits — сложность вызова: базовая и повышенная.
type AnonMailPoWBits struct {
	Base, High int
}

// AnonMailSubnetLimits — ось подсети одной длины префикса.
type AnonMailSubnetLimits struct {
	PoW, Hard int
}

// AnonMailGlobalFlow — ведро общего потока: темп в секунду и всплеск.
type AnonMailGlobalFlow struct {
	RatePerSecond float64
	Burst         int
}

// LogValue печатает действующие значения всех ручек края под их именами.
func (l EdgeLimits) LogValue() slog.Value { return slog.GroupValue(l.effective...) }

// errNotSet — отсутствие ручки. Текст одинаков у каждой строки таблицы и у
// ParseTrustedHops: «<ключ> не задана».
var errNotSet = errors.New("не задана: умолчания нет, значение объявляет профиль установки")

// edgeKnob — строка таблицы: ручка, её поле и разбор с границей в
// распакованную величину.
type edgeKnob struct {
	name string
	raw  func(Config) string
	// put разбирает непустое значение, судит границу строки и кладёт величину
	// в пределы. Ошибка — без имени ручки: его добавляет страж.
	put func(raw string, l *EdgeLimits) error
	// windowCeiling — верхняя граница окна; ноль у строк, не задающих окно.
	windowCeiling time.Duration
}

// edgeKnobTable — таблица границ края. Порядок — порядок Р8.
var edgeKnobTable = []edgeKnob{
	countKnob(knobIPFreeLimit, func(c Config) string { return c.AnonMailIPFreeLimit }, func(l *EdgeLimits) *int { return &l.AnonMail.Source.Free }),
	countKnob(knobIPPoWLimit, func(c Config) string { return c.AnonMailIPPoWLimit }, func(l *EdgeLimits) *int { return &l.AnonMail.Source.PoW }),
	countKnob(knobIPHardLimit, func(c Config) string { return c.AnonMailIPHardLimit }, func(l *EdgeLimits) *int { return &l.AnonMail.Source.Hard }),
	windowKnob(knobIPFreeWindow, func(c Config) string { return c.AnonMailIPFreeWindow }, func(l *EdgeLimits) *time.Duration { return &l.AnonMail.Source.FreeWindow }),
	windowKnob(knobIPPoWWindow, func(c Config) string { return c.AnonMailIPPoWWindow }, func(l *EdgeLimits) *time.Duration { return &l.AnonMail.Source.PoWWindow }),
	windowKnob(knobIPHardWindow, func(c Config) string { return c.AnonMailIPHardWindow }, func(l *EdgeLimits) *time.Duration { return &l.AnonMail.Source.HardWindow }),
	bitsKnob(knobPoWBitsBase, func(c Config) string { return c.AnonMailPoWBitsBase }, func(l *EdgeLimits) *int { return &l.AnonMail.PoWBits.Base }),
	bitsKnob(knobPoWBitsHigh, func(c Config) string { return c.AnonMailPoWBitsHigh }, func(l *EdgeLimits) *int { return &l.AnonMail.PoWBits.High }),
	countKnob(knobSubnetV4Len24PoW, func(c Config) string { return c.AnonMailSubnetV4Len24PoWLimit }, func(l *EdgeLimits) *int { return &l.AnonMail.SubnetV4Len24.PoW }),
	countKnob(knobSubnetV4Len24Hard, func(c Config) string { return c.AnonMailSubnetV4Len24HardLimit }, func(l *EdgeLimits) *int { return &l.AnonMail.SubnetV4Len24.Hard }),
	countKnob(knobSubnetV6Len56PoW, func(c Config) string { return c.AnonMailSubnetV6Len56PoWLimit }, func(l *EdgeLimits) *int { return &l.AnonMail.SubnetV6Len56.PoW }),
	countKnob(knobSubnetV6Len56Hard, func(c Config) string { return c.AnonMailSubnetV6Len56HardLimit }, func(l *EdgeLimits) *int { return &l.AnonMail.SubnetV6Len56.Hard }),
	countKnob(knobSubnetV6Len48PoW, func(c Config) string { return c.AnonMailSubnetV6Len48PoWLimit }, func(l *EdgeLimits) *int { return &l.AnonMail.SubnetV6Len48.PoW }),
	countKnob(knobSubnetV6Len48Hard, func(c Config) string { return c.AnonMailSubnetV6Len48HardLimit }, func(l *EdgeLimits) *int { return &l.AnonMail.SubnetV6Len48.Hard }),
	windowKnob(knobSubnetPoWWindow, func(c Config) string { return c.AnonMailSubnetPoWWindow }, func(l *EdgeLimits) *time.Duration { return &l.AnonMail.SubnetPoWWindow }),
	windowKnob(knobSubnetHardWindow, func(c Config) string { return c.AnonMailSubnetHardWindow }, func(l *EdgeLimits) *time.Duration { return &l.AnonMail.SubnetHardWindow }),
	{
		name: knobGlobalRatePerSecond,
		raw:  func(c Config) string { return c.AnonMailGlobalRatePerSecond },
		put: func(raw string, l *EdgeLimits) error {
			v, err := strconv.ParseFloat(raw, 64)
			if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
				return fmt.Errorf("= %q: не конечное число (граница: RATE > 0)", raw)
			}
			if v <= 0 {
				return fmt.Errorf("= %s: граница RATE > 0", raw)
			}
			l.AnonMail.Global.RatePerSecond = v
			return nil
		},
	},
	countKnob(knobGlobalBurst, func(c Config) string { return c.AnonMailGlobalBurst }, func(l *EdgeLimits) *int { return &l.AnonMail.Global.Burst }),
	{
		name: TrustedHopsKnob,
		raw:  func(c Config) string { return c.TrustedHops },
		put: func(raw string, l *EdgeLimits) error {
			h, err := parseTrustedHopsValue(raw)
			if err != nil {
				return err
			}
			l.TrustedHops = h
			return nil
		},
	},
}

func countKnob(name string, raw func(Config) string, dst func(*EdgeLimits) *int) edgeKnob {
	return edgeKnob{name: name, raw: raw, put: func(s string, l *EdgeLimits) error {
		n, err := strconv.Atoi(s)
		if err != nil {
			return fmt.Errorf("= %q: не целое число (граница: целое ≥ %d)", s, anonMailCountFloor)
		}
		if n < anonMailCountFloor {
			return fmt.Errorf("= %d: граница ≥ %d", n, anonMailCountFloor)
		}
		*dst(l) = n
		return nil
	}}
}

func bitsKnob(name string, raw func(Config) string, dst func(*EdgeLimits) *int) edgeKnob {
	return edgeKnob{name: name, raw: raw, put: func(s string, l *EdgeLimits) error {
		n, err := strconv.Atoi(s)
		if err != nil {
			return fmt.Errorf("= %q: не целое число (граница: %d ≤ биты ≤ %d)", s, anonMailPoWBitsFloor, anonMailPoWBitsCeiling)
		}
		if n < anonMailPoWBitsFloor || n > anonMailPoWBitsCeiling {
			return fmt.Errorf("= %d: граница %d ≤ биты ≤ %d", n, anonMailPoWBitsFloor, anonMailPoWBitsCeiling)
		}
		*dst(l) = n
		return nil
	}}
}

func windowKnob(name string, raw func(Config) string, dst func(*EdgeLimits) *time.Duration) edgeKnob {
	return edgeKnob{name: name, raw: raw, windowCeiling: anonMailWindowCeiling, put: func(s string, l *EdgeLimits) error {
		d, err := time.ParseDuration(s)
		if err != nil {
			return fmt.Errorf("= %q: не длительность с единицей, например 15m или 1h (граница: %s ≤ окно ≤ %s)",
				s, anonMailWindowFloor, anonMailWindowCeiling)
		}
		if d < anonMailWindowFloor || d > anonMailWindowCeiling {
			return fmt.Errorf("= %s: граница %s ≤ окно ≤ %s", d, anonMailWindowFloor, anonMailWindowCeiling)
		}
		*dst(l) = d
		return nil
	}}
}

// edgeRelation — граница Р8 между ручками. Судится, только когда все её ручки
// разобраны: иначе отказ повторил бы уже названную причину.
type edgeRelation struct {
	knobs []string
	check func(l AnonMailLimits) error
}

var edgeRelationTable = []edgeRelation{
	orderedCounts(knobIPFreeLimit, knobIPPoWLimit, "FREE ≤ POW", func(l AnonMailLimits) (int, int) { return l.Source.Free, l.Source.PoW }),
	orderedCounts(knobIPPoWLimit, knobIPHardLimit, "POW ≤ HARD", func(l AnonMailLimits) (int, int) { return l.Source.PoW, l.Source.Hard }),
	orderedWindows(knobIPFreeWindow, knobIPPoWWindow, "W_F ≤ W_P", func(l AnonMailLimits) (time.Duration, time.Duration) {
		return l.Source.FreeWindow, l.Source.PoWWindow
	}),
	orderedWindows(knobIPPoWWindow, knobIPHardWindow, "W_P ≤ W_H", func(l AnonMailLimits) (time.Duration, time.Duration) {
		return l.Source.PoWWindow, l.Source.HardWindow
	}),
	hardRate(knobIPHardLimit, knobIPHardWindow, anonMailSourceHardPerHour, func(l AnonMailLimits) (int, time.Duration) {
		return l.Source.Hard, l.Source.HardWindow
	}),
	{
		knobs: []string{knobPoWBitsBase, knobPoWBitsHigh},
		check: func(l AnonMailLimits) error {
			if l.PoWBits.Base >= l.PoWBits.High {
				return fmt.Errorf("%s = %d, %s = %d: граница BASE < HIGH", knobPoWBitsBase, l.PoWBits.Base, knobPoWBitsHigh, l.PoWBits.High)
			}
			return nil
		},
	},
	orderedCounts(knobSubnetV4Len24PoW, knobSubnetV4Len24Hard, "POW ≤ HARD", func(l AnonMailLimits) (int, int) { return l.SubnetV4Len24.PoW, l.SubnetV4Len24.Hard }),
	orderedCounts(knobSubnetV6Len56PoW, knobSubnetV6Len56Hard, "POW ≤ HARD", func(l AnonMailLimits) (int, int) { return l.SubnetV6Len56.PoW, l.SubnetV6Len56.Hard }),
	orderedCounts(knobSubnetV6Len48PoW, knobSubnetV6Len48Hard, "POW ≤ HARD", func(l AnonMailLimits) (int, int) { return l.SubnetV6Len48.PoW, l.SubnetV6Len48.Hard }),
	orderedWindows(knobSubnetPoWWindow, knobSubnetHardWindow, "W_Ps ≤ W_Hs", func(l AnonMailLimits) (time.Duration, time.Duration) {
		return l.SubnetPoWWindow, l.SubnetHardWindow
	}),
	hardRate(knobSubnetV4Len24Hard, knobSubnetHardWindow, anonMailSubnetHardPerHour, func(l AnonMailLimits) (int, time.Duration) {
		return l.SubnetV4Len24.Hard, l.SubnetHardWindow
	}),
	hardRate(knobSubnetV6Len56Hard, knobSubnetHardWindow, anonMailSubnetHardPerHour, func(l AnonMailLimits) (int, time.Duration) {
		return l.SubnetV6Len56.Hard, l.SubnetHardWindow
	}),
	hardRate(knobSubnetV6Len48Hard, knobSubnetHardWindow, anonMailSubnetHardPerHour, func(l AnonMailLimits) (int, time.Duration) {
		return l.SubnetV6Len48.Hard, l.SubnetHardWindow
	}),
	{
		knobs: []string{knobGlobalRatePerSecond, knobGlobalBurst},
		check: func(l AnonMailLimits) error {
			if float64(l.Global.Burst) < l.Global.RatePerSecond {
				return fmt.Errorf("%s = %d, %s = %s: граница BURST ≥ RATE", knobGlobalBurst, l.Global.Burst,
					knobGlobalRatePerSecond, strconv.FormatFloat(l.Global.RatePerSecond, 'f', -1, 64))
			}
			return nil
		},
	},
}

func orderedCounts(lo, hi, bound string, get func(AnonMailLimits) (int, int)) edgeRelation {
	return edgeRelation{knobs: []string{lo, hi}, check: func(l AnonMailLimits) error {
		a, b := get(l)
		if a > b {
			return fmt.Errorf("%s = %d, %s = %d: граница %s", lo, a, hi, b, bound)
		}
		return nil
	}}
}

func orderedWindows(lo, hi, bound string, get func(AnonMailLimits) (time.Duration, time.Duration)) edgeRelation {
	return edgeRelation{knobs: []string{lo, hi}, check: func(l AnonMailLimits) error {
		a, b := get(l)
		if a > b {
			return fmt.Errorf("%s = %s, %s = %s: граница %s", lo, a, hi, b, bound)
		}
		return nil
	}}
}

// hardRate — «HARD / окно ≤ perHour в час». Сравнение точное в целых:
// HARD × 1 ч ≤ perHour × окно; произведение считается без переполнения.
func hardRate(limit, window string, perHour int64, get func(AnonMailLimits) (int, time.Duration)) edgeRelation {
	return edgeRelation{knobs: []string{limit, window}, check: func(l AnonMailLimits) error {
		n, w := get(l)
		lhs := new(big.Int).Mul(big.NewInt(int64(n)), big.NewInt(int64(time.Hour)))
		rhs := new(big.Int).Mul(big.NewInt(perHour), big.NewInt(int64(w)))
		if lhs.Cmp(rhs) > 0 {
			return fmt.Errorf("%s = %d, %s = %s: граница HARD / окно ≤ %d/ч", limit, n, window, w, perHour)
		}
		return nil
	}}
}

// ResolveEdgeLimits — страж старта ручек края (сценарий 71): разбирает все 19
// ключей по таблице и судит отношения между ними. Отказ называет ключ и
// нарушенную границу; нарушений может быть несколько — названы все, чтобы
// оператор не чинил профиль по одному ключу за перезапуск.
func ResolveEdgeLimits(cfg Config) (EdgeLimits, error) {
	var (
		out    EdgeLimits
		errs   []error
		parsed = make(map[string]bool, len(edgeKnobTable))
	)
	for _, row := range edgeKnobTable {
		raw := strings.TrimSpace(row.raw(cfg))
		if raw == "" {
			errs = append(errs, fmt.Errorf("%s %w", row.name, errNotSet))
			continue
		}
		if err := row.put(raw, &out); err != nil {
			errs = append(errs, fmt.Errorf("%s %w", row.name, err))
			continue
		}
		parsed[row.name] = true
		out.effective = append(out.effective, slog.String(row.name, raw))
	}
	for _, rel := range edgeRelationTable {
		if !allParsed(parsed, rel.knobs) {
			continue
		}
		if err := rel.check(out.AnonMail); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return EdgeLimits{}, fmt.Errorf("ручки края (приёмка NTF-2, Р8): %w", errors.Join(errs...))
	}
	return out, nil
}

func allParsed(parsed map[string]bool, knobs []string) bool {
	for _, k := range knobs {
		if !parsed[k] {
			return false
		}
	}
	return true
}

// AnonMailWindowUpperBound — наибольшая верхняя граница окон края по таблице
// границ. Её читает срок хранения моментов (З26): он строго больше любого окна,
// которое установка вправе задать, а не только настроенного сегодня.
func AnonMailWindowUpperBound() time.Duration {
	var ceiling time.Duration
	for _, row := range edgeKnobTable {
		ceiling = max(ceiling, row.windowCeiling)
	}
	return ceiling
}

// AnonMailPoWKey — ключ подписи вызовов proof-of-work (З9). Ключ не печатается:
// String и LogValue отдают метку, а не байты.
type AnonMailPoWKey struct {
	b []byte
}

// Bytes возвращает копию ключа.
func (k AnonMailPoWKey) Bytes() []byte { return append([]byte(nil), k.b...) }

// String — метка вместо ключа.
func (k AnonMailPoWKey) String() string { return anonMailPoWKeyRedactedTag }

// LogValue — метка вместо ключа.
func (k AnonMailPoWKey) LogValue() slog.Value { return slog.StringValue(anonMailPoWKeyRedactedTag) }

// ReadAnonMailPoWKey читает ключ подписи вызовов из файла секрета. Ручка не
// задана, файл не читается, ключ короче 32 байт — отказ старта с именем ручки.
// Байты файла — ключ как есть: вывода из другого секрета и нормализации нет,
// и у всех реплик флота ключ один (CX2-13).
//
// Путь читается только из закрытого набора форм — иначе отказ старта, а не
// чтение «чего получится»:
//   - АБСОЛЮТНЫЙ: относительный разрешался бы от рабочего каталога процесса,
//     то есть читал бы то, что лежит рядом при запуске;
//   - КАНОНИЧЕСКИЙ (filepath.Clean не меняет ни буквы): путь, названный
//     оператором и напечатанный журналом старта, совпадает с прочитанным,
//     `..` и лишние разделители не проходят;
//   - ОБЫЧНЫЙ ФАЙЛ: FIFO при чтении без писателя ждёт вечно (страж висел бы
//     вместо отказа), устройство вида /dev/zero не кончается.
func ReadAnonMailPoWKey(cfg Config) (AnonMailPoWKey, error) {
	raw := strings.TrimSpace(cfg.AnonMailPoWKeyFile)
	if raw == "" {
		return AnonMailPoWKey{}, fmt.Errorf("%s %w", AnonMailPoWKeyFileKnob, errNotSet)
	}
	path := filepath.Clean(raw)
	if !filepath.IsAbs(path) {
		return AnonMailPoWKey{}, fmt.Errorf("%s = %q: путь не абсолютный", AnonMailPoWKeyFileKnob, raw)
	}
	if path != raw {
		return AnonMailPoWKey{}, fmt.Errorf("%s = %q: путь не в канонической форме, канон — %q",
			AnonMailPoWKeyFileKnob, raw, path)
	}
	// Stat идёт по ссылке, а не Lstat: смонтированный секрет — цепочка ссылок
	// внутри каталога тома, и судится её цель.
	fi, err := os.Stat(path)
	if err != nil {
		return AnonMailPoWKey{}, fmt.Errorf("%s = %q: файл секрета не читается: %w", AnonMailPoWKeyFileKnob, path, err)
	}
	if !fi.Mode().IsRegular() {
		return AnonMailPoWKey{}, fmt.Errorf("%s = %q: не обычный файл (%s)", AnonMailPoWKeyFileKnob, path, fi.Mode().Type())
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return AnonMailPoWKey{}, fmt.Errorf("%s = %q: файл секрета не читается: %w", AnonMailPoWKeyFileKnob, path, err)
	}
	if len(b) < anonMailPoWKeyMinBytes {
		return AnonMailPoWKey{}, fmt.Errorf("%s = %q: ключ %d байт, граница ≥ %d байт",
			AnonMailPoWKeyFileKnob, path, len(b), anonMailPoWKeyMinBytes)
	}
	return AnonMailPoWKey{b: b}, nil
}
