package main

import (
	"encoding/binary"
	"fmt"
)

// This file wraps the resources the executable carries — the icon sizes, the
// group directory that ties them together, and the version block — into a
// COFF object that go build links on its own. The resource tree is PE-shaped;
// in an object file the data entries hold section-relative offsets and one
// relocation per entry lets the linker turn them into real addresses.

// Resource types and the fixed identity of the tree.
const (
	rtIcon    = 3
	rtGroup   = 14
	rtVersion = 16

	langID = 0x0409 // en-US; the strings are Unicode, the language only sorts

	relAMD64Addr32 = 0x0002
	symClassStatic = 3

	subdirFlag = 0x8000_0000
)

// leaf is one resource: its type, its id, and which blob holds its bytes.
type leaf struct {
	id   uint32
	name uint32
}

// buildSYSO assembles the object for the icon sizes and the version strings.
// Strings are key/value pairs in the order the properties sheet reads them.
func buildSYSO(pngs [][]byte, version string, s map[string]string) []byte {
	var blobs [][]byte
	var leaves []leaf
	for i, p := range pngs {
		blobs = append(blobs, p)
		leaves = append(leaves, leaf{id: rtIcon, name: uint32(i + 1)})
	}
	blobs = append(blobs, groupIcon(sizes, pngs))
	leaves = append(leaves, leaf{id: rtGroup, name: 1})
	blobs = append(blobs, versionInfo(version, s))
	leaves = append(leaves, leaf{id: rtVersion, name: 1})
	return coff(blobs, leaves)
}

// groupIcon is the RT_GROUP_ICON: the ICO directory again, in grouped form,
// pointing at each RT_ICON by id.
func groupIcon(sizes []int, pngs [][]byte) []byte {
	out := []byte{0, 0, 1, 0} // reserved, type 1, then the count
	out = append(out, byte(len(pngs)), 0)
	for i, s := range sizes {
		b := byte(s)
		if s == 256 {
			b = 0
		}
		out = append(out, b, b) // width, height
		out = append(out, 0, 0) // palette, reserved
		out = append(out, 1, 0) // planes
		out = append(out, 32, 0)
		dw := uint32(len(pngs[i]))
		out = append(out, byte(dw), byte(dw>>8), byte(dw>>16), byte(dw>>24))
		out = append(out, byte(i+1), 0)
	}
	return out
}

// coff lays out the body the linker wants —
//
//	[root dir] [type dirs] [lang dirs] [data entries] [blobs]
//
// every offset relative to the body's own start — then wraps it in the file
// with relocations and the symbol table. Every leaf gets its own language
// directory; one belongs to exactly one resource id and cannot be shared.
func coff(blobs [][]byte, leaves []leaf) []byte {
	n := len(blobs)
	// Types in first-seen order; the icons share one type directory.
	var types []uint32
	for _, l := range leaves {
		if len(types) == 0 || types[len(types)-1] != l.id {
			types = append(types, l.id)
		}
	}
	at := 0
	rootAt := at
	at += 16 + 8*len(types) // root: one entry per resource type
	typeDirAt := map[uint32]int{}
	for _, t := range types {
		k := 0
		for _, l := range leaves {
			if l.id == t {
				k++
			}
		}
		typeDirAt[t] = at
		at += 16 + 8*k
	}
	langAt := make([]int, n)
	for i := range leaves {
		langAt[i] = at
		at += 16 + 8
	}
	dataAt := make([]int, n)
	for i := range leaves {
		dataAt[i] = at
		at += 16
	}
	blobAt := make([]int, n)
	for i, b := range blobs {
		at += len(b)
		if pad := (4 - at%4) % 4; pad != 0 { // blobs sit on 4-byte boundaries
			at += pad
		}
		blobAt[i] = at - len(b) - (4-at%4)%4
	}
	bodySize := at

	body := make([]byte, bodySize)
	put16 := func(o int, v uint16) { binary.LittleEndian.PutUint16(body[o:], v) }
	put32 := func(o int, v uint32) { binary.LittleEndian.PutUint32(body[o:], v) }

	// dir writes an IMAGE_RESOURCE_DIRECTORY and its id entries.
	dir := func(o, ided int, entry func(i int) (uint32, int, bool)) {
		put16(o+12, 0) // named entries
		put16(o+14, uint16(ided))
		for i := range ided {
			id, target, sub := entry(i)
			put32(o+16+8*i, id)
			off := uint32(target)
			if sub {
				off |= subdirFlag
			}
			put32(o+16+8*i+4, off)
		}
	}
	dir(rootAt, len(types), func(i int) (uint32, int, bool) {
		return types[i], typeDirAt[types[i]], true
	})
	for _, t := range types {
		members := []int{}
		for i, l := range leaves {
			if l.id == t {
				members = append(members, i)
			}
		}
		dir(typeDirAt[t], len(members), func(i int) (uint32, int, bool) {
			return leaves[members[i]].name, langAt[members[i]], true
		})
	}
	for i := range leaves {
		dir(langAt[i], 1, func(int) (uint32, int, bool) { return langID, dataAt[i], false })
		put32(dataAt[i], uint32(blobAt[i])) // OffsetToData, relocated at link time
		put32(dataAt[i]+4, uint32(len(blobs[i])))
		put32(dataAt[i]+8, 0) // code page
		put32(dataAt[i]+12, 0)
	}
	for i, b := range blobs {
		copy(body[blobAt[i]:], b)
	}

	// The file: header, section header, body, relocations, symbols.
	relocs := make([]byte, 10*n)
	for i := range leaves {
		binary.LittleEndian.PutUint32(relocs[10*i:], uint32(dataAt[i])) // field position in the section
		binary.LittleEndian.PutUint32(relocs[10*i+4:], 0)               // symbol: the .rsrc section symbol
		binary.LittleEndian.PutUint16(relocs[10*i+8:], relAMD64Addr32)
	}
	const hdr = 20 + 40
	symAt := hdr + bodySize + len(relocs)

	var syms []byte
	syms = append(syms, ".rsrc\x00\x00\x00"...) // 8-byte inline name
	append32 := func(v uint32) {
		var b [4]byte
		binary.LittleEndian.PutUint32(b[:], v)
		syms = append(syms, b[:]...)
	}
	append16 := func(v uint16) {
		var b [2]byte
		binary.LittleEndian.PutUint16(b[:], v)
		syms = append(syms, b[:]...)
	}
	append32(0) // value: the section's start
	append16(1) // section number
	append16(0) // type
	syms = append(syms, symClassStatic, 1)
	// Aux, format 1: section length, relocation count, then zeroes.
	append32(uint32(bodySize))
	append16(uint16(n))
	append16(0)
	append32(0) // checksum
	append16(0) // number
	syms = append(syms, 0, 0, 0, 0) // selection, reserved

	out := make([]byte, 0, symAt+18+4)
	add := func(b []byte) { out = append(out, b...) }
	h := make([]byte, 20)
	binary.LittleEndian.PutUint16(h[0:], 0x8664) // x64
	binary.LittleEndian.PutUint16(h[2:], 1)      // one section
	h[8], h[9], h[10], h[11] = 0, 0, 0, 0        // timestamp 0: reproducible
	binary.LittleEndian.PutUint32(h[8:], uint32(symAt))
	binary.LittleEndian.PutUint32(h[12:], 2) // two symbols
	binary.LittleEndian.PutUint16(h[18:], 0)
	add(h)

	s := make([]byte, 40)
	copy(s, ".rsrc")
	binary.LittleEndian.PutUint32(s[8:], 0)                      // VirtualSize
	binary.LittleEndian.PutUint32(s[12:], 0)                     // VirtualAddress
	binary.LittleEndian.PutUint32(s[16:], uint32(bodySize))      // SizeOfRawData
	binary.LittleEndian.PutUint32(s[20:], hdr)                   // PointerToRawData
	binary.LittleEndian.PutUint32(s[24:], uint32(hdr+bodySize))  // PointerToRelocations
	binary.LittleEndian.PutUint16(s[32:], uint16(n))             // NumberOfRelocations
	binary.LittleEndian.PutUint32(s[36:], 0x4000_0040)           // initialized, readable
	add(s)

	add(body)
	add(relocs)
	add(syms)
	add([]byte{0, 0, 0, 0}) // empty string table: length 0
	return out
}

// versionInfo builds the VS_VERSIONINFO block: fixed numbers, one string
// table, and the language translation the properties sheet reads.
func versionInfo(version string, s map[string]string) []byte {
	var maj, min, pat int
	fmt.Sscanf(version, "%d.%d.%d", &maj, &min, &pat)
	var out []byte
	u16 := func(v uint16) { out = append(out, byte(v), byte(v>>8)) }
	u32 := func(v uint32) {
		out = append(out, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
	}
	key := func(k string) {
		for _, r := range k {
			u16(uint16(r))
		}
		u16(0)
		for len(out)%4 != 0 {
			out = append(out, 0)
		}
	}
	patch16 := func(o int, v uint16) { binary.LittleEndian.PutUint16(out[o:], v) }

	u16(0) // wLength, patched at the end
	u16(52)
	u16(0) // a binary block
	key("VS_VERSION_INFO")
	u32(0xFEEF_04BD)
	u32(0x1_0000)
	u32(uint32(maj)<<16 | uint32(min))
	u32(uint32(pat) << 16)
	u32(uint32(maj)<<16 | uint32(min))
	u32(uint32(pat) << 16)
	u32(0x3F)     // valid flags
	u32(0)        // no flags set
	u32(0x4_0004) // VOS_NT_WINDOWS32
	u32(1)        // VFT_APP
	u32(0)        // subtype
	u32(0)        // file date
	u32(0)

	// StringFileInfo / <table> / String blocks, lengths patched when each
	// level is complete.
	strStart := len(out)
	u16(0)
	u16(0)
	u16(1)
	key("StringFileInfo")
	tableStart := len(out)
	u16(0)
	u16(0)
	u16(1)
	key("040904b0")
	for _, k := range []string{"CompanyName", "FileDescription", "FileVersion", "LegalCopyright", "ProductName", "OriginalFilename"} {
		v, ok := s[k]
		if !ok {
			continue
		}
		blockStart := len(out)
		u16(0) // block length, patched
		u16(0) // value length in words, patched
		u16(1)
		key(k)
		valueStart := len(out)
		for _, r := range v {
			u16(uint16(r))
		}
		u16(0)
		u16(0) // strings carry two terminators
		for len(out)%4 != 0 {
			out = append(out, 0)
		}
		patch16(blockStart, uint16(len(out)-blockStart))
		patch16(blockStart+2, uint16((len(out)-valueStart)/2))
	}
	patch16(tableStart, uint16(len(out)-tableStart))
	patch16(strStart, uint16(len(out)-strStart))

	varStart := len(out)
	u16(0)
	u16(4) // the value is one binary dword
	u16(0)
	key("VarFileInfo")
	u16(uint16(langID))
	u16(0x04B0) // Unicode
	patch16(varStart, uint16(len(out)-varStart))

	patch16(0, uint16(len(out)))
	return out
}
