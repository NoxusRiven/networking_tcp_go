package utils

import (
	"fmt"
	"sync/atomic"
)

type Counter struct {
	val atomic.Uint32
}

func (c *Counter) GetValue() uint32 {
	return c.val.Load()
}

func (c *Counter) UpdateValue(new uint32) {
	c.val.Swap(new)
}

func (c *Counter) IncLoad() {
	c.val.Add(1)
}

func (c *Counter) DecLoad() error {

	for {
		current := c.GetValue()
		if current == 0 {
			return fmt.Errorf("Node: Trying to decrement current load when it's value is 0")
		}
		next := current - 1
		if c.val.CompareAndSwap(current, next) {
			return nil
		}

		//Safe guard if it got decremented while executing this method, just repeat and check again
	}

}

type IntSize uint16

const (
	MaxUint8 = ^uint8(0)
	MinUint8 = 0
	MaxInt8  = int(MaxUint8 >> 1)
	MinInt8  = -MaxInt8 - 1

	MaxUint16 = ^uint16(0)
	MinUint16 = 0
	MaxInt16  = int(MaxUint16 >> 1)
	MinInt16  = -MaxInt16 - 1

	MaxUint32 = ^uint32(0)
	MinUint32 = 0
	MaxInt32  = int(MaxUint32 >> 1)
	MinInt32  = -MaxInt32 - 1

	MaxUint64 = ^uint64(0)
	MinUint64 = 0
	MaxInt64  = int(MaxUint64 >> 1)
	MinInt64  = -MaxInt64 - 1
)

const (
	SIZE_16 IntSize = 16
	SIZE_32 IntSize = 32
	SIZE_64 IntSize = 64
)

// TODO: fix this counter
func IncUint(val *uint64, size IntSize) uint64 {

	if size == SIZE_16 {
		if *val < uint64(MaxUint16) {
			cast := uint32(*val)
			return uint64(atomic.AddUint32(&cast, 1))
		} else {
			panic(fmt.Sprintf("Trying to increment uint16 when value is equal to: %v ", *val))
		}

	} else if size == SIZE_32 {
		cast := uint32(*val)
		return uint64(atomic.AndUint32(&cast, 1))

	} else if size == SIZE_64 {
		return atomic.AndUint64(val, 1)
	}

	panic("IncUint - Unreachable ")
}
