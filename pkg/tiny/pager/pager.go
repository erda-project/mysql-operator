package pager

import (
	"bytes"
	"reflect"
	"strconv"

	"github.com/cxr29/tiny"
)

type Pager struct {
	Page, Count, Total, Number int
	Counts                     []int
}

func (p Pager) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('[')
	b.WriteString(strconv.Itoa(p.Page))
	b.WriteByte(',')
	b.WriteString(strconv.Itoa(p.Count))
	b.WriteByte(',')
	b.WriteString(strconv.Itoa(p.Total))
	b.WriteByte(',')
	b.WriteString(strconv.Itoa(p.Number))
	for _, i := range p.Counts {
		b.WriteByte(',')
		b.WriteString(strconv.Itoa(i))
	}
	b.WriteByte(']')
	return b.Bytes(), nil
}

func (p Pager) Start() int {
	return (p.Page - 1) * p.Count
}

func (p Pager) End() int {
	if p.Number >= 0 {
		return p.Start() + p.Number
	}
	n := p.Page * p.Count
	if p.Total >= 0 && n > p.Total {
		return p.Total
	}
	return n
}

func (p Pager) Empty() bool {
	if p.Total < 0 {
		panic("missing total")
	}
	return p.Total <= p.Start()
}

func (p Pager) Pages() int {
	if p.Total < 0 {
		panic("missing total")
	}
	n := p.Total / p.Count
	if p.Total%p.Count > 0 {
		n++
	}
	return n
}

func (p Pager) Pagination(n int) (a []int) {
	if n < 1 || n > 100 {
		panic("can't pagination")
	}
	var l, r, i, j, m int
	if p.Total < 0 {
		l = n
		r = 0
	} else {
		m = p.Total / p.Count
		if p.Total%p.Count > 0 {
			m++
		}
		i = n / 2
		if n%2 > 0 {
			i++
		}
		j = n - i
		l = p.Page
		if l > n {
			l = n
		}
		r = m - p.Page
		if r > n {
			r = n
		}
		if l > i && r > j {
			l = i
			r = j
		} else if l > i {
			l = n - r
		} else if r > j {
			r = n - l
		}
	}
	for i = 0; i < l; i++ {
		j = p.Page - i
		if j >= 1 {
			a = append(a, j)
		} else {
			break
		}
	}
	if len(a) > 0 {
		i = a[len(a)-1]
		if i > 2 {
			a = append(a, -(i - 1))
		}
		if i > 1 {
			a = append(a, 1)
		}
	} else {
		a = append(a, 1)
	}
	for i, j = 0, len(a)-1; i < j; i, j = i+1, j-1 {
		a[i], a[j] = a[j], a[i]
	}
	if p.Total < 0 {
		if p.Number < 0 || p.Count == p.Number {
			a = append(a, -(p.Page + 1))
		}
	} else {
		for i = 1; i <= r; i++ {
			j = p.Page + i
			if j <= m {
				a = append(a, j)
			} else {
				break
			}
		}
		if len(a) > 0 {
			i = a[len(a)-1]
			if i > 0 {
				if i < m-1 {
					a = append(a, -(i + 1))
				}
				if i < m {
					a = append(a, m)
				}
			}
		}
	}
	return
}

var key = reflect.TypeOf((*Pager)(nil)).Elem()

func Pull(ctx *tiny.Context) *Pager {
	return ctx.Value(key).(*Pager)
}

var Option = MaxCount(10).MinValue(1).MaxValue(10000).Distinct(-1)

func Push(counts ...int) tiny.HandlerFunc {
	if len(counts) == 0 {
		counts = []int{10, 50, 100}
	} else if _, es := Option.Clean(counts); es != "" {
		panic(es + " Counts")
	}
	return func(ctx *tiny.Context) {
		page, n := ctx.FirstInt("Page")
		if n == 0 {
			page = 1
		} else if n < 0 || page < 1 {
			ctx.WriteError("Illegal Page")
			return
		}
		count, n := ctx.FirstInt("Count")
		if n == 0 {
			count = counts[0]
		} else if n < 0 || count < 1 || !Contains(counts, count) {
			ctx.WriteError("Illegal Count")
			return
		}
		ctx.SetValue(key, &Pager{
			Page:   page,
			Count:  count,
			Counts: counts,
			Total:  -1,
			Number: -1,
		})
	}
}
