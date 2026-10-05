// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package dkimkey — чтение пары «закрытый ключ и селектор DKIM» из тома
// объекта, смонтированного без `subPath` (замысел issue-2915 §12а «Ключ и
// селектор DKIM», CX1-133, CX1-134, CX1-140).
//
// Функция чтения одна — [ReadPairFS] (корень зовёт [ReadPair] на порту [OS]):
// её зовут и загрузчик при старте, и перепроверка стража DNS. Второго пути
// чтения ключа или селектора в notify нет.
//
// Форма тома — kubelet: `<каталог>/..<поколение>/<имя>`, ссылка
// `<каталог>/..data` → `..<поколение>`, файлы `<каталог>/<имя>` → `..data/<имя>`.
// Пара читается из ОДНОГО поколения: `..data` разрешается один раз на попытку,
// оба файла читаются из пути поколения, а не через ссылки. kubelet после
// переключения `..data` удаляет прежний каталог поколения, поэтому файл,
// пропавший из разрешённого поколения, даёт повторное разрешение — не больше
// [generationRetries] раз.
//
// Тексты отказов фиксированы и содержимого ключа не несут: ошибка разборщика
// PEM/PKCS#1/PKCS#8 отбрасывается, а не оборачивается (класс CX1-125).
// Порт чтения удаления не несёт: читателю ключа право удаления не нужно.
package dkimkey

import (
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Переменные окружения пары — ручки загрузчика notify (§8). Здесь они нужны
// только тексту отказа «пути в разных каталогах»; совпадение с тегами
// перечня ручек конфигурации держит проба пакета config.
const (
	KeyFileEnv      = "KACHO_NOTIFY_DKIM_KEY_FILE"
	SelectorFileEnv = "KACHO_NOTIFY_DKIM_SELECTOR_FILE"
)

// generationRetries — сколько раз [ReadPairFS] заново разрешает `..data`, когда
// файла нет в разрешённом поколении (§8, CX1-140). Обновления тома kubelet
// приходят с периодом синхронизации (порядка минуты), а чтение пары — два
// малых файла: два переключения подряд за одно чтение — признак сбоя тома, и
// бесконечный цикл чтения на таком томе хуже отказа.
const generationRetries = 2

// MinKeyBits — нижняя граница длины ключа RSA (§8, NTF1-P14).
const MinKeyBits = 2048

// dataLink — имя ссылки тома kubelet на действующее поколение.
const dataLink = "..data"

// PairFS — порт чтения тома: разрешение ссылки и чтение файла. Корень передаёт
// [OS]; проба — обёртку над настоящим каталогом с крючком «между чтениями».
type PairFS interface {
	Readlink(name string) (string, error)
	ReadFile(name string) ([]byte, error)
}

type osFS struct{}

func (osFS) Readlink(name string) (string, error) { return os.Readlink(name) }
func (osFS) ReadFile(name string) ([]byte, error) { return os.ReadFile(name) }

// OS — порт чтения на файловой системе процесса.
var OS PairFS = osFS{}

// Pair — пара DKIM одного поколения тома.
type Pair struct {
	// Selector — селектор как он лежит в объекте. Форму имени DNS судит
	// вызывающий (`dnscheck.ValidSelector`).
	Selector string
	Key      *rsa.PrivateKey
	// Generation — разрешённое поколение `..data`, из которого прочитана пара.
	Generation string
	// Digest — SHA-256(len‖ключ‖len‖селектор) содержимого: смена пары
	// определяется по нему, а не по имени поколения — kubelet может выпустить
	// новое поколение с тем же содержимым.
	Digest [32]byte
}

// Part — какой ключ объекта назван отказом.
type Part int

const (
	// PartKey — закрытый ключ (`privateKeyKey`), ручка [KeyFileEnv].
	PartKey Part = iota
	// PartSelector — селектор (`selectorKey`), ручка [SelectorFileEnv].
	PartSelector
)

// Reason — закрытый перечень причин отказа (NTF1-P14).
type Reason int

const (
	// ReasonUnreadable — ссылки `..data` нет или она не читается, файл не
	// читается по причине иной, чем отсутствие, повторы исчерпаны, ключ не
	// разбирается.
	ReasonUnreadable Reason = iota
	// ReasonAbsent — файла нет в объекте при неизменном поколении.
	ReasonAbsent
	// ReasonNotRSA — ключ не RSA.
	ReasonNotRSA
	// ReasonShortKey — ключ RSA короче [MinKeyBits].
	ReasonShortKey
	// ReasonEmpty — селектор пуст.
	ReasonEmpty
	// ReasonSelectorForm — селектор вне формы имени DNS (судит вызывающий).
	ReasonSelectorForm
	// ReasonSplitDirs — пути ключа и селектора в разных каталогах тома.
	ReasonSplitDirs
)

// Error — отказ чтения пары: ключ объекта (имя файла тома) и причина.
// Содержимого файлов не несёт.
type Error struct {
	Part   Part
	Name   string
	Reason Reason
}

func (e *Error) Error() string {
	subject := "ключ DKIM"
	if e.Part == PartSelector {
		subject = "селектор DKIM"
	}
	why := ""
	switch e.Reason {
	case ReasonUnreadable:
		why = "не читается"
	case ReasonAbsent:
		why = "нет в объекте"
	case ReasonNotRSA:
		why = "не RSA"
	case ReasonShortKey:
		why = "короче 2048 бит"
	case ReasonEmpty:
		why = "пуст"
	case ReasonSelectorForm:
		why = "вне формы имени DNS"
	case ReasonSplitDirs:
		return subject + " `" + e.Name + "`: путь " + SelectorFileEnv + " не в каталоге " + KeyFileEnv +
			" — пару нельзя прочитать из одного поколения тома"
	}
	return subject + " `" + e.Name + "`: " + why
}

// ReadPair — [ReadPairFS] на порту [OS].
func ReadPair(keyFile, selectorFile string) (Pair, error) {
	return ReadPairFS(OS, keyFile, selectorFile)
}

// ReadPairFS читает пару из одного поколения тома (порядок — §12а):
//
//  1. оба пути в одном каталоге, иначе — отказ по селектору;
//  2. поколение разрешается один раз на попытку;
//  3. оба файла читаются из `<каталог>/<поколение>/<имя>`;
//  4. файла нет в разрешённом поколении — `..data` разрешается заново:
//     поколение сменилось — попытка с шага 3 целиком на новом; то же — «нет в
//     объекте»; повторов больше [generationRetries] — «не читается»;
//  5. ключ разбирается и судится (RSA, не короче [MinKeyBits]); селектор
//     непуст.
func ReadPairFS(fsys PairFS, keyFile, selectorFile string) (Pair, error) {
	keyName, selName := filepath.Base(keyFile), filepath.Base(selectorFile)
	unreadable := &Error{Part: PartKey, Name: keyName, Reason: ReasonUnreadable}
	dir := filepath.Dir(keyFile)
	if filepath.Dir(selectorFile) != dir {
		return Pair{}, &Error{Part: PartSelector, Name: selName, Reason: ReasonSplitDirs}
	}

	gen, ok := resolve(fsys, dir)
	if !ok {
		return Pair{}, unreadable
	}
	for retries := 0; ; retries++ {
		keyPEM, kerr := fsys.ReadFile(filepath.Join(dir, gen, keyName))
		sel, serr := fsys.ReadFile(filepath.Join(dir, gen, selName))
		if (kerr != nil && !errors.Is(kerr, fs.ErrNotExist)) || (serr != nil && !errors.Is(serr, fs.ErrNotExist)) {
			return Pair{}, unreadable
		}
		if kerr == nil && serr == nil {
			return assemble(keyName, selName, gen, keyPEM, sel)
		}
		next, ok := resolve(fsys, dir)
		if !ok {
			return Pair{}, unreadable
		}
		if next == gen {
			if kerr != nil {
				return Pair{}, &Error{Part: PartKey, Name: keyName, Reason: ReasonAbsent}
			}
			return Pair{}, &Error{Part: PartSelector, Name: selName, Reason: ReasonAbsent}
		}
		if retries == generationRetries {
			return Pair{}, unreadable
		}
		gen = next
	}
}

// resolve — поколение `..data`: одно имя каталога рядом со ссылкой. Иная
// форма цели (абсолютный путь, путь с разделителем) — не том kubelet.
func resolve(fsys PairFS, dir string) (string, bool) {
	gen, err := fsys.Readlink(filepath.Join(dir, dataLink))
	if err != nil || gen == "" || strings.ContainsRune(gen, filepath.Separator) || gen == "." || gen == ".." {
		return "", false
	}
	return gen, true
}

func assemble(keyName, selName, gen string, keyPEM, sel []byte) (Pair, error) {
	key, reason, ok := parseKey(keyPEM)
	if !ok {
		return Pair{}, &Error{Part: PartKey, Name: keyName, Reason: reason}
	}
	if len(sel) == 0 {
		return Pair{}, &Error{Part: PartSelector, Name: selName, Reason: ReasonEmpty}
	}
	return Pair{Selector: string(sel), Key: key, Generation: gen, Digest: digest(keyPEM, sel)}, nil
}

// parseKey — закрытый ключ RSA из PEM (PKCS#1 либо PKCS#8). Ошибка разборщика
// отбрасывается: её текст мог бы нести байты ключа.
func parseKey(b []byte) (*rsa.PrivateKey, Reason, bool) {
	block, _ := pem.Decode(b)
	if block == nil {
		return nil, ReasonUnreadable, false
	}
	var parsed any
	switch block.Type {
	case "RSA PRIVATE KEY":
		k, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, ReasonUnreadable, false
		}
		parsed = k
	case "PRIVATE KEY":
		k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, ReasonUnreadable, false
		}
		parsed = k
	case "EC PRIVATE KEY":
		return nil, ReasonNotRSA, false
	default:
		return nil, ReasonUnreadable, false
	}
	key, isRSA := parsed.(*rsa.PrivateKey)
	if !isRSA {
		return nil, ReasonNotRSA, false
	}
	if key.N.BitLen() < MinKeyBits {
		return nil, ReasonShortKey, false
	}
	return key, 0, true
}

// digest — SHA-256(len‖ключ‖len‖селектор), длины — 8 байт big-endian: границу
// между частями нельзя сдвинуть, не изменив дайджеста.
func digest(key, sel []byte) [32]byte {
	h := sha256.New()
	var n [8]byte
	binary.BigEndian.PutUint64(n[:], uint64(len(key)))
	h.Write(n[:])
	h.Write(key)
	binary.BigEndian.PutUint64(n[:], uint64(len(sel)))
	h.Write(n[:])
	h.Write(sel)
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}
