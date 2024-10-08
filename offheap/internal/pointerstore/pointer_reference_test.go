// Copyright 2024 Francis Michael Stephens. All rights reserved.  Use of this
// source code is governed by an MIT license that can be found in the LICENSE
// file.

package pointerstore

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Zero value of Reference returns true for IsNil()
func TestIsNil(t *testing.T) {
	r := RefPointer{}
	assert.True(t, r.IsNil())
}

// Calling newReference() with nil will panic
func TestNewReferenceWithNilPanics(t *testing.T) {
	assert.Panics(t, func() { NewReference(0, 0) })
}

// Demonstrate that a pointer with any non-0 field is not nil
func TestIsNotNil(t *testing.T) {
	allocConfig := NewAllocConfigBySize(8, 32*8)
	objects, metadata := MmapSlab(allocConfig)
	for i := range objects {
		r := NewReference(objects[i], metadata[i])
		// The object is not nil
		assert.False(t, r.IsNil())
		// Data pointer points to the correct location
		assert.Equal(t, objects[i], r.DataPtr())
		// Metadata pointer points to the correct location
		assert.Equal(t, metadata[i], r.metadataPtr())
		// Generation of a new Reference is always 0
		assert.Equal(t, uint8(0), r.Gen())
	}
}

// Demonstrate that the generation tag does not appear in either of the data or
// metadata pointers.
//
// It might be expected that if either of these pointer values did have the
// generation tag in them they wouldn't work, and all our conventional tests
// would reveal this. But our lived experience indicates that some platforms
// (anecdotally OSX M2 CPU running Ubunutu in a VM) work perfectly fine with
// garbage data in the high bits of the address. While other platforms
// (anecdotally Ubuntu running on whatever github uses) produce segfaults and
// seem to weirdly claim the address was 0x0.
//
// So now we have this test, which should catch us if we don't strip the
// generation tag out of wherever we've hidden it (at time of writing it's
// hidden in the top 8 bits of the object address pointer).
func TestGenerationDoesNotAppearInOtherFields(t *testing.T) {
	allocConfig := NewAllocConfigBySize(8, 32*8)
	objects, metadatas := MmapSlab(allocConfig)

	r := NewReference(objects[0], metadatas[0])
	dataPtr := r.DataPtr()
	metaPtr := r.metadataPtr()
	metadata := r.metadata()

	gen := uint8(255)
	metadata.gen = gen
	r.setGen(gen)

	assert.Equal(t, dataPtr, r.DataPtr())
	assert.Equal(t, metaPtr, r.metadataPtr())
	assert.Equal(t, gen, r.Gen())
}

func TestRealloc(t *testing.T) {
	allocConfig := NewAllocConfigBySize(8, 32*8)
	objects, metadatas := MmapSlab(allocConfig)

	r1 := NewReference(objects[0], metadatas[0])
	dataPtr := r1.DataPtr()
	metaPtr := r1.metadataPtr()
	gen := r1.Gen()

	r2 := r1.Realloc()

	// Assert that the data/metadata pointed to by r1 and r2 is the same
	assert.Equal(t, dataPtr, r2.DataPtr())
	assert.Equal(t, metaPtr, r2.metadataPtr())

	// Assert that r2 has a different generation than r1
	assert.NotEqual(t, gen, r2.Gen())

	// Assert that r1 is no longer valid, but r2 is valid
	assert.Panics(t, func() { r1.DataPtr() })
	assert.NotPanics(t, func() { r2.DataPtr() })
}
