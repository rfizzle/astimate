package inspect

import "bytes"

// IsGenerated reports whether src, the bytes of a non-test TypeScript
// file, is generated code by SPEC.md 6.5: its leading comment block, the
// comments before the first token of code (a byte order mark and a
// shebang line may come first), carries one of
//
//   - "@generated", the Meta and Relay convention;
//   - a line that holds "generated" and, after it or on another line, "do
//     not edit" or "do not modify", compared case-insensitively. This
//     covers Go's form, "Code generated ... DO NOT EDIT", which ts-proto
//     writes, and the headers of openapi-generator, graphql-codegen and
//     Prisma.
//
// A marker after the first token of code does not count, and
// "/* eslint-disable */" alone is no marker.
func IsGenerated(src []byte) bool {
	src = bytes.TrimPrefix(src, []byte("\xEF\xBB\xBF"))
	if bytes.HasPrefix(src, []byte("#!")) {
		if nl := bytes.IndexByte(src, '\n'); nl >= 0 {
			src = src[nl+1:]
		} else {
			return false
		}
	}
	var generated, doNotEdit bool
	for {
		src = bytes.TrimLeft(src, " \t\r\n\v\f")
		var text []byte
		switch {
		case bytes.HasPrefix(src, []byte("//")):
			end := bytes.IndexByte(src, '\n')
			if end < 0 {
				end = len(src)
			}
			text, src = src[2:end], src[end:]
		case bytes.HasPrefix(src, []byte("/*")):
			end := bytes.Index(src[2:], []byte("*/"))
			if end < 0 {
				text, src = src[2:], nil
			} else {
				text, src = src[2:2+end], src[2+end+2:]
			}
		default:
			return false
		}
		if bytes.Contains(text, []byte("@generated")) {
			return true
		}
		for line := range bytes.SplitSeq(bytes.ToLower(text), []byte{'\n'}) {
			if bytes.Contains(line, []byte("generated")) {
				generated = true
			}
			if bytes.Contains(line, []byte("do not edit")) || bytes.Contains(line, []byte("do not modify")) {
				doNotEdit = true
			}
		}
		if generated && doNotEdit {
			return true
		}
	}
}
