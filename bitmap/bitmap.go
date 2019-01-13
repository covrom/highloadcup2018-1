package bitmap

import (
	"sync"
	"unsafe"

	"github.com/modern-go/reflect2"

	"github.com/funny-falcon/highloadcup2018/alloc"
)

const NoNext = -1
const SpanSize = 256
const SpanMask = SpanSize - 1

type Bitmap interface {
	Set(al alloc.Allocator, addr alloc.Ptr, i int32) alloc.Ptr
	Unset(al alloc.Allocator, addr alloc.Ptr, i int32) alloc.Ptr
	ApproxCapa() int32
	Iterator(al alloc.Allocator, max int32) Iterator
}

type Iterator interface {
	LastSpan() int32
	FetchAndNext(span int32) (Block, int32)
}

func LoopIter(it Iterator, f func(u []int32) bool) {
	var indx [256]int32
	last := it.LastSpan()
	for last >= 0 {
		block, next := it.FetchAndNext(last)
		if !f(block.Unroll(last, &indx)) {
			break
		}
		last = next
	}
}

func CountIter(it Iterator) uint32 {
	last := it.LastSpan()
	count := uint32(0)
	for last >= 0 {
		block, next := it.FetchAndNext(last)
		count += block.Count()
		last = next
	}
	return count
}

type Wrapper struct {
	sync.Mutex
	Alloc  alloc.Allocator
	Ptr    *alloc.Ptr
	Tpe    reflect2.Type
	Bitmap Bitmap
}

func Wrap(al alloc.Allocator, ptr *alloc.Ptr, pat interface{}) *Wrapper {
	tpe := reflect2.TypeOfPtr(pat).Elem()
	wr := &Wrapper{
		Alloc: al,
		Tpe:   tpe,
		Ptr:   ptr,
	}
	if wr.Ptr == nil {
		wr.Ptr = new(alloc.Ptr)
	}
	if *wr.Ptr == 0 {
		wr.Bitmap = pat.(Bitmap)
	} else {
		var p unsafe.Pointer
		al.Get(*wr.Ptr, &p)
		wr.Bitmap = tpe.PackEFace(p).(Bitmap)
	}
	return wr
}

func (w *Wrapper) IsEmpty() bool {
	return *w.Ptr == 0
}

func (w *Wrapper) Set(i int32) {
	w.Lock()
	w.remap(w.Bitmap.Set(w.Alloc, *w.Ptr, i))
	w.Unlock()
}

func (w *Wrapper) Unset(i int32) {
	w.Lock()
	w.remap(w.Bitmap.Unset(w.Alloc, *w.Ptr, i))
	w.Unlock()
}

func (w *Wrapper) GetIterator(max int32) Iterator {
	if w == nil {
		return EmptyIt
	}
	return w.Bitmap.Iterator(w.Alloc, max)
}

func (w *Wrapper) remap(ptr alloc.Ptr) {
	if ptr != *w.Ptr {
		var p unsafe.Pointer
		*w.Ptr = ptr
		w.Alloc.Get(*w.Ptr, &p)
		w.Bitmap = w.Tpe.PackEFace(p).(Bitmap)
	}
}

func (w *Wrapper) ApproxCapa() int32 {
	return w.Bitmap.ApproxCapa()
}

func (w *Wrapper) Iterator(max int32) Iterator {
	return w.Bitmap.Iterator(w.Alloc, max)
}
