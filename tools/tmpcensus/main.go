// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Временный инструмент переписи (задача #1255). Удаляется тем же изменением.
package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

var family = map[protowire.Number]string{
	101501: "required", 101502: "pattern", 101503: "value", 101504: "size",
	101505: "length", 101506: "unique", 101510: "map_key", 101511: "bytes",
}

func optNums(opts proto.Message) []protowire.Number {
	if opts == nil {
		return nil
	}
	var out []protowire.Number
	b := opts.ProtoReflect().GetUnknown()
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			break
		}
		b = b[n:]
		n = protowire.ConsumeFieldValue(num, typ, b)
		if n < 0 {
			break
		}
		b = b[n:]
		out = append(out, num)
	}
	return out
}

func main() {
	raw, err := os.ReadFile(os.Args[1])
	if err != nil {
		panic(err)
	}
	var fds descriptorpb.FileDescriptorSet
	if err := proto.Unmarshal(raw, &fds); err != nil {
		panic(err)
	}
	mode := os.Args[2] // "declared" | "comments"

	type rec struct{ key, file string }
	var withDecl []rec
	noComment := map[string][]string{}
	files, msgs, fields := 0, 0, 0

	for _, fd := range fds.File {
		if !strings.HasPrefix(fd.GetName(), "kacho/") {
			continue
		}
		files++
		// комментарии по SourceCodeInfo: путь 4,<msgIdx>,2,<fieldIdx>
		comments := map[string]string{}
		for _, loc := range fd.GetSourceCodeInfo().GetLocation() {
			p := loc.GetPath()
			if len(p) == 4 && p[0] == 4 && p[2] == 2 {
				comments[fmt.Sprintf("%d/%d", p[1], p[3])] = loc.GetLeadingComments() + loc.GetTrailingComments()
			}
			if len(p) == 6 && p[0] == 4 && p[2] == 3 && p[4] == 2 {
				comments[fmt.Sprintf("%d.%d/%d", p[1], p[3], p[5])] = loc.GetLeadingComments() + loc.GetTrailingComments()
			}
		}
		var walk func(ms []*descriptorpb.DescriptorProto, prefix, cpath string)
		walk = func(ms []*descriptorpb.DescriptorProto, prefix, cpath string) {
			for mi, md := range ms {
				msgs++
				name := prefix + md.GetName()
				cp := cpath
				if cp == "" {
					cp = fmt.Sprintf("%d", mi)
				} else {
					cp = fmt.Sprintf("%s.%d", cpath, mi)
				}
				for fi, f := range md.Field {
					fields++
					key := name + "." + f.GetName()
					has := false
					for _, n := range optNums(f.Options) {
						if _, ok := family[n]; ok {
							has = true
						}
					}
					if has {
						withDecl = append(withDecl, rec{key, fd.GetName()})
					}
					if mode == "comments" {
						c := strings.TrimSpace(comments[fmt.Sprintf("%s/%d", cp, fi)])
						if c == "" {
							noComment[fd.GetName()] = append(noComment[fd.GetName()], key)
						}
					}
					if mode == "dump" {
						c := strings.TrimSpace(comments[fmt.Sprintf("%s/%d", cp, fi)])
						fmt.Printf("%s\t%s\n", key, strings.ReplaceAll(c, "\n", "\\n"))
					}
				}
				walk(md.NestedType, name+".", cp)
			}
		}
		walk(fd.MessageType, "", "")
	}

	if mode == "declared" {
		sort.Slice(withDecl, func(i, j int) bool { return withDecl[i].key < withDecl[j].key })
		for _, r := range withDecl {
			fmt.Printf("%s\t%s\n", r.key, r.file)
		}
		fmt.Fprintf(os.Stderr, "осмотрено: файлов %d, сообщений %d, полей %d; с объявлением семейства %d\n",
			files, msgs, fields, len(withDecl))
		return
	}
	keys := make([]string, 0, len(noComment))
	for k := range noComment {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	total := 0
	for _, k := range keys {
		sort.Strings(noComment[k])
		for _, f := range noComment[k] {
			fmt.Printf("%s\t%s\n", f, k)
			total++
		}
	}
	fmt.Fprintf(os.Stderr, "осмотрено: файлов %d, сообщений %d, полей %d; БЕЗ собственного комментария %d\n",
		files, msgs, fields, total)
}
