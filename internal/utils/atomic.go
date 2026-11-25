package utils

import (
	"fmt"
	"math"
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

func (c *Counter) IncValue() {
	c.val.Add(1)
}

func (c *Counter) DecValue() error {

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

type IntSize uint8

const (
	SIZE_8  IntSize = 8
	SIZE_16 IntSize = 16
	SIZE_32 IntSize = 32
	SIZE_64 IntSize = 64
)

func ReturnAndIncUint(val *uint64, size IntSize) uint64 {
	var maxVal uint64
	switch size {
	case SIZE_8:
		maxVal = uint64(math.MaxInt8)
	case SIZE_16:
		maxVal = uint64(math.MaxUint16)
	case SIZE_32:
		maxVal = uint64(math.MaxUint32)
	case SIZE_64:
		maxVal = math.MaxUint64
	default:
		panic("ReturnAndIncUint - Unreachable")
	}

	for {
		current := atomic.LoadUint64(val)
		if current >= maxVal {
			panic(fmt.Sprintf("Trying to increment %v when value is equal to: %v", size, current))
		}
		next := current + 1
		if atomic.CompareAndSwapUint64(val, current, next) {
			return current
		}
	}
}
