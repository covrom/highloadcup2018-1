package main

import (
	"fmt"
	"sync"
	"unsafe"

	"github.com/funny-falcon/highloadcup2018/bitmap"

	"github.com/funny-falcon/highloadcup2018/alloc"
)

var StringAlloc alloc.Simple

const StringShards = 64
const ShardFind = (1 << 32) / StringShards

type StringsTable struct {
	Tbl []uint32
	Arr []StringHandle
}

type StringHandle struct {
	Hash   uint32
	Ptr    alloc.Ptr
	Handle int32
}

type String struct {
	Len  uint8
	Data [256]uint8
}

func (h *StringHandle) Str() string {
	var ustr *String
	StringAlloc.Get(h.Ptr, &ustr)
	return ustr.String()
}

func (h *StringHandle) HndlAsPtr() *alloc.Ptr {
	return (*alloc.Ptr)(unsafe.Pointer(&h.Handle))
}

func (us *String) String() string {
	sl := us.Data[:us.Len]
	str := *(*string)(unsafe.Pointer(&sl))
	return str
}

func hash(s string) uint32 {
	res := uint32(0x123456)
	for _, b := range []byte(s) {
		res ^= uint32(b)
		res *= 0x51235995
	}
	res ^= (res<<8 | res>>24) ^ (res<<19 | res>>13)
	res *= 0x62435345
	return res ^ res>>16
}

func (us *StringsTable) Insert(s string) (uint32, bool) {
	if s == "" {
		return 0, false
	}
	if len(s) > 255 {
		panic("String is too long " + s)
	}
	if len(us.Arr) >= len(us.Tbl)*5/8 {
		us.Rebalance()
	}
	h := hash(s)
	mask := uint32(len(us.Tbl) - 1)
	pos, d := h&mask, uint32(1)
	for us.Tbl[pos] != 0 {
		apos := us.Tbl[pos]
		hndl := us.Arr[apos-1]
		if hndl.Hash == h && hndl.Str() == s {
			return apos, false
		}
		pos = (pos + d) & mask
		d++
	}

	ptr := StringAlloc.Alloc(len(s) + 1)
	var ustr *String
	StringAlloc.Get(ptr, &ustr)
	ustr.Len = uint8(len(s))
	copy(ustr.Data[:], s)

	us.Arr = append(us.Arr, StringHandle{Hash: h, Ptr: ptr})
	apos := uint32(len(us.Arr))
	us.Tbl[pos] = apos
	return apos, true
}

func (us *StringsTable) Find(s string) uint32 {
	h := hash(s)
	mask := uint32(len(us.Tbl) - 1)
	pos, d := h&mask, uint32(1)
	for us.Tbl[pos] != 0 {
		apos := us.Tbl[pos]
		hndl := us.Arr[apos-1]
		if hndl.Hash == h && hndl.Str() == s {
			return apos
		}
		pos = (pos + d) & mask
		d++
	}
	return 0
}

func (us *StringsTable) GetHndl(i uint32) *StringHandle {
	return &us.Arr[i-1]
}

func (us *StringsTable) GetStr(i uint32) string {
	return us.Arr[i-1].Str()
}

func (ush *StringsTable) Rebalance() {
	newcapa := len(ush.Tbl) * 2
	if newcapa == 0 {
		newcapa = 256
	}
	mask := uint32(newcapa - 1)
	newTbl := make([]uint32, newcapa, newcapa)
	for i, hndl := range ush.Arr {
		pos, d := hndl.Hash&mask, uint32(1)
		for newTbl[pos] != 0 {
			pos = (pos + d) & mask
			d++
		}
		newTbl[pos] = uint32(i) + 1
	}
	ush.Tbl = newTbl
}

type UniqStrings struct {
	sync.Mutex
	StringsTable
}

func (us *UniqStrings) InsertUid(s string, uid int32) (uint32, bool) {
	if len(s) > 255 {
		panic("String is too long " + s)
	}
	us.Lock()
	defer us.Unlock()
	ix, isNew := us.Insert(s)
	hndl := us.GetHndl(ix)
	if isNew {
		hndl.Handle = -1
	}
	if hndl.Handle == -1 {
		hndl.Handle = uid
		return ix, true
	}
	return ix, false
}

func (us *UniqStrings) ResetUser(ix uint32, uid int32) {
	if ix == 0 {
		return
	}
	us.Lock()
	defer us.Unlock()
	hndl := us.GetHndl(ix)
	if hndl.Handle != uid {
		panic(fmt.Sprintf("User %d is not owner of string %s", uid, hndl.Str()))
	}
	hndl.Handle = -1
}

type SomeStrings struct {
	sync.Mutex
	StringsTable
	Null    *bitmap.Wrapper
	NotNull *bitmap.Wrapper
}

func NewSomeStrings() *SomeStrings {
	return &SomeStrings{
		Null:    bitmap.Wrap(&BitmapAlloc, nil, bitmap.LargeEmpty),
		NotNull: bitmap.Wrap(&BitmapAlloc, nil, bitmap.LargeEmpty),
	}
}

func (ss *SomeStrings) Add(str string, uid int32) uint32 {
	if str == "" {
		ss.Null.Set(uid)
		return 0
	}
	ss.NotNull.Set(uid)
	ss.Lock()
	defer ss.Unlock()
	ix, _ := ss.Insert(str)
	hndl := ss.GetHndl(ix)
	wr := bitmap.Wrap(&BitmapAlloc, hndl.HndlAsPtr(), bitmap.LargeEmpty)
	wr.Set(uid)
	return ix
}

func (ss *SomeStrings) GetIndex(ix uint32) *bitmap.Wrapper {
	if ix == 0 {
		return ss.Null
	}
	return bitmap.Wrap(&BitmapAlloc, ss.GetHndl(ix).HndlAsPtr(), bitmap.LargeEmpty)
}
