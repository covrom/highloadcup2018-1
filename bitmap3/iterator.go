package bitmap3

import "sort"

type IBitmap interface {
	LoopBlock(func(int32, uint64) bool)
	GetL2() *[384]uint64
	GetBlock(int32) uint64
	Has(int32) bool
}

type NullBitmap struct{}

func (NullBitmap) Loop(f func([]int32) bool)          {}
func (NullBitmap) LoopBlock(func(int32, uint64) bool) {}
func (NullBitmap) GetL2() *[384]uint64                { panic("no"); return nil }
func (NullBitmap) GetBlock(int32) uint64              { return 0 }
func (NullBitmap) Has(int32) bool                     { return false }

var nullL1 [48]uint32

type RawUids []int32

func (r RawUids) Loop(f func([]int32) bool)        { f(r) }
func (r RawUids) Count() uint32                    { return uint32(len(r)) }
func (RawUids) LoopBlock(func(int32, uint64) bool) { panic("no") }
func (RawUids) GetL2() *[384]uint64                { panic("no"); return nil }
func (RawUids) GetBlock(int32) uint64              { panic("no"); return 0 }
func (RawUids) Has(int32) bool                     { panic("no"); return false }

type RawWithMap struct {
	R RawUids
	M IBitmap
}

func (RawWithMap) LoopBlock(func(int32, uint64) bool) { panic("no") }
func (RawWithMap) GetL2() *[384]uint64                { panic("no"); return nil }
func (RawWithMap) GetBlock(int32) uint64              { panic("no"); return 0 }
func (RawWithMap) Has(int32) bool                     { panic("no"); return false }

func (r RawWithMap) Loop(f func([]int32) bool) {
	var s [1]int32
	for _, uid := range r.R {
		if r.M.Has(uid) {
			s[0] = uid
			if !f(s[:]) {
				break
			}
		}
	}
}

type AndBitmap struct {
	Maps []IBitmap
	L2   [384]uint64

	LastSpan  int32
	LastBlock uint64
}

func cnt(m IBitmap) uint32 {
	if c, ok := m.(Counter); ok {
		return c.Count()
	}
	return 1 << 30
}

func NewAndBitmap(maps []IBitmap) IBitmap {
	if len(maps) == 0 {
		return NullBitmap{}
	}
	if len(maps) == 1 {
		return maps[0]
	}
	maps = append([]IBitmap(nil), maps...)
	for i, m := range maps {
		if raw, ok := m.(RawUids); ok {
			return &RawWithMap{
				R: raw,
				M: NewAndBitmap(append(maps[:i], maps[i+1:]...)),
			}
		} else if _, ok := m.(NullBitmap); ok {
			return m
		}
	}
	sort.Slice(maps, func(i, j int) bool {
		return cnt(maps[i]) < cnt(maps[j])
	})
	bm := &AndBitmap{Maps: maps}
	for i := range bm.L2 {
		bm.L2[i] = ^uint64(0)
	}
	for _, m := range maps {
		for i, v := range m.GetL2() {
			bm.L2[i] &= v
		}
	}
	return bm
}

func (bm *AndBitmap) LoopBlock(f func(int32, uint64) bool) {
	var l2u Unrolled
	for l2ix := int32(len(bm.L2) - 1); l2ix >= 0; l2ix-- {
		l2v := bm.L2[l2ix]
		if l2v == 0 {
			continue
		}
		l2ixb := l2ix * 64
	l3loop:
		for _, l3ix := range Unroll(l2v, l2ixb, &l2u) {
			l3v := ^uint64(0)
			l3ixb := l3ix * 64
			for _, m := range bm.Maps {
				l3v &= m.GetBlock(l3ixb)
				if l3v == 0 {
					continue l3loop
				}
			}
			if l3v != 0 && !f(l3ixb, l3v) {
				return
			}
		}
	}
}

func (bm *AndBitmap) GetL2() *[384]uint64 {
	return &bm.L2
}

func (bm *AndBitmap) GetBlock(span int32) uint64 {
	if span == bm.LastSpan {
		return bm.LastBlock
	}
	if !Has(bm.L2[:], span/64) {
		return 0
	}
	l3v := ^uint64(0)
	for _, m := range bm.Maps {
		l3v &= m.GetBlock(span)
		if l3v == 0 {
			Unset(bm.L2[:], span/64)
			break
		}
	}
	bm.LastSpan = span
	bm.LastBlock = l3v
	return l3v
}

func (bm *AndBitmap) Has(ix int32) bool {
	bl := bm.GetBlock(ix &^ 63)
	b := uint64(1) << uint32(ix&63)
	return bl&b != 0
}

type OrBitmap struct {
	Maps []IBitmap
	L2   [384]uint64

	LastSpan  int32
	LastBlock uint64
}

func NewOrBitmap(maps []IBitmap) IBitmap {
	if len(maps) == 0 {
		return NullBitmap{}
	}
	if len(maps) == 1 {
		return maps[0]
	}
	bm := &OrBitmap{Maps: maps}
	for _, m := range maps {
		for i, v := range m.GetL2() {
			bm.L2[i] |= v
		}
	}
	return bm
}

func (bm *OrBitmap) LoopBlock(f func(int32, uint64) bool) {
	var l2u Unrolled
	for l2ix := int32(len(bm.L2) - 1); l2ix >= 0; l2ix-- {
		l2v := bm.L2[l2ix]
		if l2v == 0 {
			continue
		}
		l2ixb := l2ix * 64
		for _, l3ix := range Unroll(l2v, l2ixb, &l2u) {
			l3v := uint64(0)
			l3ixb := l3ix * 64
			for _, m := range bm.Maps {
				l3v |= m.GetBlock(l3ixb)
			}
			if l3v != 0 && !f(l3ixb, l3v) {
				return
			}
		}
	}
}

func (bm *OrBitmap) GetL2() *[384]uint64 {
	return &bm.L2
}

func (bm *OrBitmap) GetBlock(span int32) uint64 {
	if span == bm.LastSpan {
		return bm.LastBlock
	}
	if !Has(bm.L2[:], span/64) {
		return 0
	}
	l3v := uint64(0)
	for _, m := range bm.Maps {
		l3v |= m.GetBlock(span)
	}
	bm.LastSpan = span
	bm.LastBlock = l3v
	return l3v
}

func (bm *OrBitmap) Has(ix int32) bool {
	bl := bm.GetBlock(ix &^ 63)
	b := uint64(1) << uint32(ix&63)
	return bl&b != 0
}
