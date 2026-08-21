// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// compiledartifact.go — предикат «этот файл есть СКОМПИЛИРОВАННЫЙ артефакт».
//
// Живёт в не-тестовом файле намеренно: его зовёт и гейт по дереву, и проба
// инъекции. Один предикат на двоих — иначе они разойдутся молча, и разойдутся
// там, где расхождение не видно.
package repohygiene

import "bytes"

// compiledArtifactKind — вид исполнимого артефакта, узнаваемый по первым байтам.
type compiledArtifactKind string

const (
	kindELF     compiledArtifactKind = "ELF"    // Linux/BSD
	kindMachO   compiledArtifactKind = "Mach-O" // macOS
	kindPE      compiledArtifactKind = "PE"     // Windows
	kindArchive compiledArtifactKind = "ar/.a"  // статическая библиотека
	kindNone    compiledArtifactKind = ""
)

// compiledArtifactMagicLen — сколько байт достаточно прочитать, чтобы ответить.
// Читается ИМЕННО столько: гейт обходит всё дерево, и чтение файлов целиком
// сделало бы его дорогим ровно там, где он и нужен, — на большом артефакте.
const compiledArtifactMagicLen = 8

// classifyCompiledArtifact отвечает, чем является файл по своим первым байтам.
//
// Разбор по МАГИИ, а не по имени и не по расширению. Имя здесь ничего не
// значит: артефакт сборки называется по каталогу пакета `main` и потому
// выглядит как обычный файл без расширения, а перечень таких имён — рукописный
// список, который разойдётся с деревом при первом новом сервисе. Именно так эта
// дыра и открылась во второй раз: перечень покрывал имена одного вида и не
// покрывал имя края.
func classifyCompiledArtifact(head []byte) compiledArtifactKind {
	switch {
	case bytes.HasPrefix(head, []byte{0x7f, 'E', 'L', 'F'}):
		return kindELF
	case bytes.HasPrefix(head, []byte{0xcf, 0xfa, 0xed, 0xfe}), // Mach-O 64 LE
		bytes.HasPrefix(head, []byte{0xce, 0xfa, 0xed, 0xfe}), // Mach-O 32 LE
		bytes.HasPrefix(head, []byte{0xca, 0xfe, 0xba, 0xbe}): // Mach-O universal
		return kindMachO
	case bytes.HasPrefix(head, []byte{'M', 'Z'}):
		return kindPE
	case bytes.HasPrefix(head, []byte("!<arch>\n")):
		return kindArchive
	}
	return kindNone
}
