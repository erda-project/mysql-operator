package pager

import (
	"sort"
	"strconv"

	"github.com/cxr29/tiny"
)

func Contains(a []int, i int) bool {
	return Index(a, i) >= 0
}

func Index(a []int, i int) int {
	for j := 0; j < len(a); j++ {
		if a[j] == i {
			return j
		}
	}
	return -1
}

func LastIndex(a []int, i int) int {
	for j := len(a); j >= 0; j-- {
		if a[j] == i {
			return j
		}
	}
	return -1
}

const (
	oAsc uint8 = 1 << iota
	oDesc
	oDistinct
	oDuplicate
	oMinValue
	oMaxValue
	oMinCount
	oMaxCount
)

type Options struct {
	flag               uint8
	minValue, maxValue int
	minCount, maxCount int
}

func (o *Options) hasFlag(f uint8) bool {
	return o.flag&f == f
}
func (o *Options) setFlag(f uint8) {
	o.flag |= f
}
func (o *Options) clearFlag(f uint8) {
	o.flag &^= f
}

func Order(i int) *Options {
	return new(Options).Order(i)
}

func (o *Options) Order(i int) *Options {
	if i > 0 {
		o.setFlag(oAsc)
	} else if i < 0 {
		o.setFlag(oDesc)
	} else {
		o.clearFlag(oAsc)
		o.clearFlag(oDesc)
	}
	return o
}

func Distinct(i int) *Options {
	return new(Options).Distinct(i)
}

func (o *Options) Distinct(i int) *Options {
	if i > 0 {
		o.setFlag(oDistinct)
	} else if i < 0 {
		o.setFlag(oDuplicate)
	} else {
		o.clearFlag(oDistinct)
		o.clearFlag(oDuplicate)
	}
	return o
}

func MinValue(i int) *Options {
	return new(Options).MinValue(i)
}

func (o *Options) MinValue(i int) *Options {
	o.setFlag(oMinValue)
	o.minValue = i
	return o
}

func MaxValue(i int) *Options {
	return new(Options).MaxValue(i)
}

func (o *Options) MaxValue(i int) *Options {
	o.setFlag(oMaxValue)
	o.maxValue = i
	return o
}

func MinCount(i int) *Options {
	return new(Options).MinCount(i)
}

func (o *Options) MinCount(i int) *Options {
	if i > 0 {
		o.setFlag(oMinCount)
	} else {
		o.clearFlag(oMinCount)
	}
	o.minCount = i
	return o
}

func MaxCount(i int) *Options {
	return new(Options).MaxCount(i)
}

func (o *Options) MaxCount(i int) *Options {
	if i > 0 {
		o.setFlag(oMaxCount)
	} else {
		o.clearFlag(oMaxCount)
	}
	o.maxCount = i
	return o
}

const (
	TooFew    = "Too Few"
	TooMany   = "Too Many"
	TooSmall  = "Too Small"
	TooBig    = "Too Big"
	Illegal   = "Illegal"
	Duplicate = "Duplicate"
)

func (o *Options) Push(key string) tiny.HandlerFunc {
	return func(ctx *tiny.Context) {
		a := o.ParseValues(ctx, key)
		if ctx.WroteHeader() {
			return
		}
		ctx.SetValue(key, a)
	}
}

func (o *Options) ParseValues(ctx *tiny.Context, key string) []int {
	ctx.Request.FormValue(key)
	a, es := o.Parse(ctx.Request.Form[key])
	if es != "" {
		ctx.WriteError(es + " " + key)
	}
	return a
}

func (o *Options) Parse(a []string) ([]int, string) {
	if o.hasFlag(oMinCount) && len(a) < o.minCount {
		return nil, TooFew
	} else if o.hasFlag(oMaxCount) && len(a) > o.maxCount {
		return nil, TooMany
	}
	b := make([]int, 0, len(a))
Loop:
	for _, s := range a {
		i, err := strconv.Atoi(s)
		if err != nil {
			return nil, Illegal
		}
		if o.hasFlag(oMinValue) && i < o.minValue {
			return nil, TooSmall
		} else if o.hasFlag(oMaxValue) && i > o.maxValue {
			return nil, TooBig
		}
		if o.hasFlag(oDistinct) {
			for _, j := range b {
				if j == i {
					continue Loop
				}
			}
		} else if o.hasFlag(oDuplicate) {
			for _, j := range b {
				if j == i {
					return nil, Duplicate
				}
			}
		}
		b = append(b, i)
	}
	if o.hasFlag(oAsc) {
		sort.Ints(b)
	} else if o.hasFlag(oDesc) {
		sort.Sort(sort.Reverse(sort.IntSlice(b)))
	}
	return b, ""
}

func (o *Options) Clean(a []int) ([]int, string) {
	n := len(a)
	if o.hasFlag(oMinCount) && n < o.minCount {
		return nil, TooFew
	} else if o.hasFlag(oMaxCount) && n > o.maxCount {
		return nil, TooMany
	}
Loop:
	for i := 0; i < n; i++ {
		if o.hasFlag(oMinValue) && a[i] < o.minValue {
			return nil, TooSmall
		} else if o.hasFlag(oMaxValue) && a[i] > o.maxValue {
			return nil, TooBig
		}
		if o.hasFlag(oDistinct) {
			for j := i - 1; j >= 0; j-- {
				if a[j] == a[i] {
					copy(a[i:n-1], a[i+1:n])
					n--
					i--
					continue Loop
				}
			}
		} else if o.hasFlag(oDuplicate) {
			for j := i - 1; j >= 0; j-- {
				if a[j] == a[i] {
					return nil, Duplicate
				}
			}
		}
	}
	a = a[:n]
	if o.hasFlag(oAsc) {
		sort.Ints(a)
	} else if o.hasFlag(oDesc) {
		sort.Sort(sort.Reverse(sort.IntSlice(a)))
	}
	return a, ""
}
