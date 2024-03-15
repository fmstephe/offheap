// The objectstore package allows us to alloc and free objects of a specific
// type.
//
// Each objectstore instance can alloc/free a single type of object. This is
// controlled by the generic type of an objectstore instance e.g.
//
//   store := objectstore.New[int]()
//
// will alloc/free only *int values.
//
// Each allocated object has a corresponding Reference which acts like a
// conventional pointer to retrieve the actual object from the objectstore using
// the Get() method e.g.
//
//  store := objectstore.New[int]()
//  reference, i1 := store.Alloc()
//  i2 := store.Get(reference)
//  if i1 == i2 {
//    println("This is correct, i1 and i2 will be a pointer to the same int")
//  }
//
// When you know that an allocated object will never be used again it's memory
// can be freed inside the objectstore using Free() e.g.
//
//  store := objectstore.New[int]()
//  reference, i1 := store.Alloc()
//  println(*i1)
//  store.Free(reference)
//  // You must never user i1 or reference again
//
// A best effort has been made to panic if an object is freed twice or if a
// freed object is accessed using Get(). However, it isn't guaranteed that
// these calls will panic. For example if an object is freed the next call to
// Alloc() will reuse the freed object and future calls to Get() and Free()
// using the Reference used in the Free will operate on the re-allocated object
// and not panic. So this behaviour cannot be relied on.
//
// References can be kept and stored in arbitrary datastructures, which can
// themselves be managed by an objectstore e.g.
//
//  type Node struct {
//    left  Reference[Node]
//    right Reference[Node]
//  }
//  store := objectstore.New[Node]()
//
// The Reference type contains no pointers. This means that we can retain as
// many of them as we like with no garbage collection cost. The objectstore
// itself contains some pointers, and must also be referenced via a pointer to
// work properly. So storing the objectstore itself in the Node struct above
// would introduce pointers to the managed object type and defeat the purpose
// of using an objectstore.
//
// The advantage of using an objectstore, over just allocating objects the
// normal way, is that we get pointer-like references to objects without those
// references (the objectstore's pointer is a type named Reference) being
// visible to the garbage collector. This means that we can build large
// datastructures, like trees or linked lists, which can live in memory
// indefinitely but incur almost zero garbage collection cost. This could be
// used to build a very large in memory cache with low GC impact.
//
// Memory Model Constraints:
//
// objectstore contains _no_ concurrency control inside. There are no atomics,
// mutexes or channels to protect against racing reads/writes.
//
// In a pure single threaded context the use of objectstore is fairly
// straightforward and should follow familiar behaviour patterns.  In order to
// get an object you are must first Alloc() it, the Reference you get from
// Alloc can get used to Get() a pointer to the object again in the future. You
// can Free() and object via its Reference, but it is only safe to do this
// once.  Calling Free() multiple times on the same Reference has unpredictable
// behaviour (although we make a best-effort to panic). Calling Get() on a
// Reference after Free() has been called on that Reference has similarly
// unpredictable behaviour (we try to panic here too, but this behaviour cannot
// be relied on).
//
// Supported Concurrent Designs
//
// The design of the objectstore supports single-threaded construction of a
// datastructure which then becomes read-only. This allows an unlimited number
// of concurrent readers but won't be useful for all systems, because the
// datastructure cannot be modified after construction.
//
// Alternatively, it is possible to use the objectstore to build single-reader
// multiple writer datastructures safely. The design must avoid calls to
// Alloc/Free in readers and take care to safely publish newly allocated
// objects to the readers using some kind of happens-before barrier, channel
// send/receive mutex lock/unlock or atomic write/read etc. This allows for
// MVCC style datastructures to be developed safely.  Tree style datastructures
// are ideal for this approach and it is likely that most datastructures
// developed using the objectstore will be tree based.
//
// 1: Independent Read Safety
//
// For a given set of live objects, previously allocated objects with a
// happens-before barrier between the allocators and readers, all objects
// can be read freely and calling Get() will work without data races.
//
// This guarantee continues to hold even if another goroutine is calling
// Alloc() and Free() to _independent_ objects/References concurrently with the
// reads.
//
// This seems like an unremarkable guarantee to make, but it does constrain the
// objectstore implementation in interesting ways. For example we cannot add a
// non-atomic read counter to Get() calls because this would be an uncontrolled
// concurrent read/write.
//
// 2: Independent Alloc Safety
//
// It is safe and possible for a writer to allocate new objects, using Alloc(),
// and then make those objects/references available to readers over a
// happens-before barrier. Preserving this guarantee requires us to ensure all
// data on the path of Get() for objects unrelated to the indepenent Alloc()
// calls are never written to during the call to Alloc().
//
// 3: Free Safety
//
// It is only safe for an object to be Freed once. It is up to the programmer
// to ensure that an object which has been Freed is never used again, and you
// must not call Get() with that object's Reference again. It is envisioned
// that a single writer will be responsible for both Alloc and Free calls, and
// a careful mechanism must be established to ensure Freed objects are never
// read again. The reason for mandating that the single writer be responsible
// for calling both Alloc() and Free() calls is that calls to Free() make the
// freed object available to the next call of Alloc(). Calling Alloc() after
// calling Free() from different goroutines without a happens-before barrier
// between them will always create a data-race, even when the Alloc() and
// Free() calls seem independent to the client program.

package objectstore

import (
	"fmt"
)

const objectChunkSize = 1024

type Stats struct {
	Allocs    int
	Frees     int
	RawAllocs int
	Live      int
	Reused    int
	Chunks    int
}

type Store[O any] struct {
	// Immutable fields
	chunkSize uint32

	// Accounting fields
	allocs int
	frees  int
	reused int

	// Data fields
	offset   uint32
	rootFree Reference[O]
	meta     [][]meta[O]
	objects  [][]O
}

// If the meta for an object has a non-nil nextFree pointer then the
// object is currently free.  Object's which have never been allocated are
// implicitly free, but have a nil nextFree point in their meta.
type meta[O any] struct {
	nextFree Reference[O]
}

func New[O any]() *Store[O] {
	chunkSize := uint32(objectChunkSize)
	// Initialise the first chunk
	meta := [][]meta[O]{make([]meta[O], chunkSize)}
	objects := [][]O{make([]O, chunkSize)}
	return &Store[O]{
		chunkSize: chunkSize,
		offset:    0,
		meta:      meta,
		objects:   objects,
	}
}

func (s *Store[O]) Alloc() (Reference[O], *O) {
	s.allocs++

	if s.rootFree.IsNil() {
		return s.allocFromOffset()
	}

	s.reused++
	return s.allocFromFree()
}

func (s *Store[O]) Get(r Reference[O]) *O {
	m := s.getMeta(r)
	if !m.nextFree.IsNil() {
		panic(fmt.Errorf("attempted to Get freed object %v", r))
	}
	return s.getObject(r)
}

func (s *Store[O]) Free(r Reference[O]) {
	meta := s.getMeta(r)

	if !meta.nextFree.IsNil() {
		panic(fmt.Errorf("attempted to Free freed object %v", r))
	}

	s.frees++

	if s.rootFree.IsNil() {
		meta.nextFree = r
	} else {
		meta.nextFree = s.rootFree
	}

	s.rootFree = r
}

func (s *Store[O]) GetStats() Stats {
	return Stats{
		Allocs:    s.allocs,
		Frees:     s.frees,
		RawAllocs: s.allocs - s.reused,
		Live:      s.allocs - s.frees,
		Reused:    s.reused,
		Chunks:    len(s.objects),
	}
}

func (s *Store[O]) allocFromFree() (Reference[O], *O) {
	// Get pointer to the next available freed slot
	alloc := s.rootFree

	// Grab the meta-data for the slot and nil out the, now
	// allocated, slot's nextFree pointer
	freeMeta := s.getMeta(alloc)
	nextFree := freeMeta.nextFree
	freeMeta.nextFree = Reference[O]{}

	// If the nextFree pointer points to the just allocated slot, then
	// there are no more freed slots available
	s.rootFree = nextFree
	if nextFree == alloc {
		s.rootFree = Reference[O]{}
	}

	return alloc, s.getObject(alloc)
}

func (s *Store[O]) allocFromOffset() (Reference[O], *O) {
	chunk := uint32(len(s.objects))
	s.offset++
	offset := s.offset
	if s.offset == s.chunkSize {
		// Create a new chunk
		s.meta = append(s.meta, make([]meta[O], s.chunkSize))
		s.objects = append(s.objects, make([]O, s.chunkSize))
		s.offset = 0
	}
	return Reference[O]{
		chunk:  chunk,
		offset: offset,
	}, &s.objects[chunk-1][offset-1]
}

func (s *Store[O]) getObject(r Reference[O]) *O {
	return &s.objects[r.chunk-1][r.offset-1]
}

func (s *Store[O]) getMeta(r Reference[O]) *meta[O] {
	return &s.meta[r.chunk-1][r.offset-1]
}
