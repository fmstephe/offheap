// Copyright 2024 Francis Michael Stephens. All rights reserved.  Use of this
// source code is governed by an MIT license that can be found in the LICENSE
// file.

package intern

import (
	"math"
	"strconv"

	"github.com/fmstephe/memorymanager/pkg/intern/internbase"
)

type float64Interner struct {
	interner internbase.InternerWithUint64Id[float64Converter]
	fmt      byte
	prec     int
	bitSize  int
}

func NewFloat64Interner(config internbase.Config, fmt byte, prec, bitSize int) Interner[float64] {
	return &float64Interner{
		interner: internbase.NewInternerWithUint64Id[float64Converter](config),
		fmt:      fmt,
		prec:     prec,
		bitSize:  bitSize,
	}
}

func (i *float64Interner) Get(value float64) string {
	return i.interner.Get(newFloat64Converter(value, i.fmt, i.prec, i.bitSize))
}

func (i *float64Interner) GetStats() internbase.StatsSummary {
	return i.interner.GetStats()
}

var _ internbase.ConverterWithUint64Id = float64Converter{}

// A flexible converter for float64 values. Here the identity is generated by a
// call to math.Float64bits(...) and we convert the value into a string using
// strconv.FormatFloat(...)
type float64Converter struct {
	value   float64
	fmt     byte
	prec    int
	bitSize int
}

func newFloat64Converter(value float64, fmt byte, prec, bitSize int) float64Converter {
	return float64Converter{
		value:   value,
		fmt:     fmt,
		prec:    prec,
		bitSize: bitSize,
	}
}

func (c float64Converter) Identity() uint64 {
	return math.Float64bits(c.value)
}

func (c float64Converter) String() string {
	return strconv.FormatFloat(c.value, c.fmt, c.prec, c.bitSize)
}
