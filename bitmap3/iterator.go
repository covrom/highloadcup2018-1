package bitmap3

import "sort"

type IBitmap interface {
	LoopBlock(func(int32, uint32) bool)
	GetL1() *[48]uint32
	GetL2(int32) uint32
	GetBlock(int32) uint32
	Has(int32) bool
}

type NullBitmap struct{}

func (NullBitmap) LoopBlock(func(int32, uint32) bool) {}
func (NullBitmap) GetL1() *[48]uint32                 { return &nullL1 }
func (NullBitmap) GetL2(int32) uint32                 { return 0 }
func (NullBitmap) GetBlock(int32) uint32              { return 0 }
func (NullBitmap) Has(int32) bool                     { return false }

var nullL1 [48]uint32

type RawUids []int32

func (r RawUids) Loop(f func([]int32) bool)        { f(r) }
func (r RawUids) Count() uint32                    { return uint32(len(r)) }
func (RawUids) LoopBlock(func(int32, uint32) bool) { panic("no") }
func (RawUids) GetL1() *[48]uint32                 { panic("no"); return &nullL1 }
func (RawUids) GetL2(int32) uint32                 { panic("no"); return 0 }
func (RawUids) GetBlock(int32) uint32              { panic("no"); return 0 }
func (RawUids) Has(int32) bool                     { panic("no"); return false }

type RawWithMap struct {
	R RawUids
	M IBitmap
}

func (RawWithMap) LoopBlock(func(int32, uint32) bool) { panic("no") }
func (RawWithMap) GetL1() *[48]uint32                 { panic("no"); return &nullL1 }
func (RawWithMap) GetL2(int32) uint32                 { panic("no"); return 0 }
func (RawWithMap) GetBlock(int32) uint32              { panic("no"); return 0 }
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
	Maps     []IBitmap
	L1       [48]uint32
	L2Filled [48]uint32
	L2       [1536]uint32

	LastSpan  int32
	LastBlock uint32
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
		}
	}
	sort.Slice(maps, func(i, j int) bool {
		return cnt(maps[i]) < cnt(maps[j])
	})
	bm := &AndBitmap{Maps: maps}
	for i := range bm.L1 {
		bm.L1[i] = ^uint32(0)
	}
	for _, m := range maps {
		ml1 := m.GetL1()
		for i := range bm.L1 {
			bm.L1[i] &= ml1[i]
		}
	}
	return bm
}

func (bm *AndBitmap) LoopBlock(f func(int32, uint32) bool) {
	var l1u, l2u Unrolled
	for l1ix := int32(len(bm.L1) - 1); l1ix >= 0; l1ix-- {
		l1v := bm.L1[l1ix]
		if l1v == 0 {
			continue
		}
		l1ixb := l1ix * 32
		for _, l2ix := range Unroll(l1v, l1ixb, &l1u) {
			if !Has(bm.L2Filled[:], l2ix) {
				bm.FillL2(l2ix)
			}
			l2v := bm.L2[l2ix]
			if l2v == 0 {
				continue
			}
			l2ixb := l2ix * 32
		l3loop:
			for _, l3ix := range Unroll(l2v, l2ixb, &l2u) {
				l3v := ^uint32(0)
				l3ixb := l3ix * 32
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
}

func (bm *AndBitmap) GetL1() *[48]uint32 {
	return &bm.L1
}

func (bm *AndBitmap) GetL2(l2ix int32) uint32 {
	if !Has(bm.L1[:], l2ix) {
		return 0
	}
	if !Has(bm.L2Filled[:], l2ix) {
		bm.FillL2(l2ix)
	}
	return bm.L2[l2ix]
}

func (bm *AndBitmap) GetBlock(span int32) uint32 {
	if span == bm.LastSpan {
		return bm.LastBlock
	}
	if !Has(bm.L1[:], span/(32*32)) {
		return 0
	}
	if !Has(bm.L2Filled[:], span/(32*32)) {
		bm.FillL2(span / (32 * 32))
	}
	if !Has(bm.L2[:], span/32) {
		return 0
	}
	l3v := ^uint32(0)
	for _, m := range bm.Maps {
		l3v &= m.GetBlock(span)
		if l3v == 0 {
			Unset(bm.L2[:], span/32)
			break
		}
	}
	bm.LastSpan = span
	bm.LastBlock = l3v
	return l3v
}

func (bm *AndBitmap) Has(ix int32) bool {
	bl := bm.GetBlock(ix &^ 31)
	b := uint32(1) << uint32(ix&31)
	return bl&b != 0
}

func (bm *AndBitmap) FillL2(l2ix int32) {
	l2v := ^uint32(0)
	for _, m := range bm.Maps {
		l2v &= m.GetL2(l2ix)
	}
	bm.L2[l2ix] = l2v
	if l2v == 0 {
		Unset(bm.L1[:], l2ix)
	}
	Set(bm.L2Filled[:], l2ix)
}

type OrBitmap struct {
	Maps     []IBitmap
	L1       [48]uint32
	L2Filled [48]uint32
	L2       [1536]uint32

	LastSpan  int32
	LastBlock uint32
}

func NewOrBitmap(maps []IBitmap) IBitmap {
	if len(maps) == 0 {
		return NullBitmap{}
	}
	if len(maps) == 1 {
		return maps[0]
	}
	bm := &OrBitmap{Maps: maps}
	for i := range bm.L1 {
		bm.L1[i] = 0
	}
	for _, m := range maps {
		ml1 := m.GetL1()
		for i := range bm.L1 {
			bm.L1[i] |= ml1[i]
		}
	}
	return bm
}

func (bm *OrBitmap) LoopBlock(f func(int32, uint32) bool) {
	var l1u, l2u Unrolled
	for l1ix := int32(len(bm.L1) - 1); l1ix >= 0; l1ix-- {
		l1v := bm.L1[l1ix]
		if l1v == 0 {
			continue
		}
		l1ixb := l1ix * 32
		for _, l2ix := range Unroll(l1v, l1ixb, &l1u) {
			if !Has(bm.L2Filled[:], l2ix) {
				bm.FillL2(l2ix)
			}
			l2v := bm.L2[l2ix]
			l2ixb := l2ix * 32
			for _, l3ix := range Unroll(l2v, l2ixb, &l2u) {
				l3v := uint32(0)
				l3ixb := l3ix * 32
				for _, m := range bm.Maps {
					l3v |= m.GetBlock(l3ixb)
				}
				if l3v != 0 && !f(l3ixb, l3v) {
					return
				}
			}
		}
	}
}

func (bm *OrBitmap) GetL1() *[48]uint32 {
	return &bm.L1
}

func (bm *OrBitmap) GetL2(l2ix int32) uint32 {
	if !Has(bm.L1[:], l2ix) {
		return 0
	}
	if !Has(bm.L2Filled[:], l2ix) {
		bm.FillL2(l2ix)
	}
	return bm.L2[l2ix]
}

func (bm *OrBitmap) GetBlock(span int32) uint32 {
	if span == bm.LastSpan {
		return bm.LastBlock
	}
	if !Has(bm.L1[:], span/(32*32)) {
		return 0
	}
	if !Has(bm.L2Filled[:], span/(32*32)) {
		bm.FillL2(span / (32 * 32))
	}
	if !Has(bm.L2[:], span/32) {
		return 0
	}
	l3v := uint32(0)
	for _, m := range bm.Maps {
		l3v |= m.GetBlock(span)
	}
	bm.LastSpan = span
	bm.LastBlock = l3v
	return l3v
}

func (bm *OrBitmap) Has(ix int32) bool {
	bl := bm.GetBlock(ix &^ 31)
	b := uint32(1) << uint32(ix&31)
	return bl&b != 0
}

func (bm *OrBitmap) FillL2(l2ix int32) {
	l2v := uint32(0)
	for _, m := range bm.Maps {
		l2v |= m.GetL2(l2ix)
	}
	bm.L2[l2ix] = l2v
	Set(bm.L2Filled[:], l2ix)
}
