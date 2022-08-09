package alog

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/cxr29/log"
	"github.com/cxr29/tiny"
)

type Options struct {
	Before, After  io.Writer
	Format, Layout string
	Comma          rune
	UseCRLF        bool
	Fields         []string
	mu             sync.Mutex // TODO: spinlock
	n              int
}

func (o *Options) add(i int) (n int) {
	o.mu.Lock()
	o.n += i
	n = o.n
	o.mu.Unlock()
	return
}

var DefaultOptions = &Options{
	After:  os.Stdout,
	Format: "text",
	Layout: "2006-01-02 15:04:05.000",
	Comma:  ',',
	Fields: []string{
		"time",
		"addr",
		"method",
		"host",
		"uri",
		"proto",
		"referer",
		"ua",
		"panic",
		"status",
		"size",
		"duration",
		"count",
		"r_x_forwarded_for",
	},
}

var key = reflect.TypeOf((*Alog)(nil)).Elem()

func Pull(ctx *tiny.Context) *Alog {
	v, ok := ctx.Values[key]
	if ok && v != nil {
		return v.(*Alog)
	}
	return nil
}

func Off(ctx *tiny.Context) {
	v, ok := ctx.Values[key]
	if !ok {
		ctx.SetValue(key, nil)
	} else if v != nil {
		v.(*Alog).Off = true
	}
}

func underscore2hyphen(s string) string {
	return http.CanonicalHeaderKey(strings.Replace(s[2:], "_", "-", -1))
}

func New(o *Options) tiny.HandlerFunc {
	if o == nil {
		o = DefaultOptions
	}
	switch o.Format {
	case "text", "csv", "json":
	default:
		panic("unsupported format")
	}
	var cookies []string
	var reqHeaders, resHeaders map[string]string
	for _, s := range o.Fields {
		if strings.HasPrefix(s, "c_") {
			cookies = append(cookies, s)
		} else if strings.HasPrefix(s, "r_") {
			if reqHeaders == nil {
				reqHeaders = map[string]string{s: underscore2hyphen(s)}
			} else {
				reqHeaders[s] = underscore2hyphen(s)
			}
		} else if strings.HasPrefix(s, "w_") {
			if resHeaders == nil {
				resHeaders = map[string]string{s: underscore2hyphen(s)}
			} else {
				resHeaders[s] = underscore2hyphen(s)
			}
		}
	}
	return func(ctx *tiny.Context) {
		if ctx.HasValue(key) {
			return
		}
		t := time.Now()
		a := &Alog{o: o, m: make(tiny.M, len(o.Fields))}
		a.Set("count", o.add(1))
		a.Set("time", t.Format(o.Layout))
		a.Set("addr", ctx.RemoteIP().String())
		a.Set("method", ctx.Request.Method)
		a.Set("host", ctx.Request.Host)
		a.Set("uri", ctx.Request.URL.RequestURI())
		a.Set("proto", ctx.Request.Proto)
		a.Set("referer", ctx.Request.Referer())
		a.Set("ua", ctx.Request.UserAgent())
		for _, i := range cookies {
			if c, err := ctx.Request.Cookie(i[2:]); err == nil {
				a.Set(i, c.Value)
			}
		}
		a.setHeaders(ctx.Request.Header, reqHeaders)
		ctx.SetValue(key, a)
		if o.Before != nil {
			a.writeTo(o.Before)
		}
		defer func() {
			err := recover()
			a.Set("panic", err != nil)
			a.Set("status", ctx.Status())
			a.Set("size", ctx.Written())
			a.Set("duration", time.Since(t))
			a.setHeaders(ctx.Header(), resHeaders)
			a.Set("count", o.add(-1))
			if !a.Off && o.After != nil {
				a.writeTo(o.After)
			}
			if err != nil {
				panic(err)
			}
		}()
		ctx.Next()
	}
}

type Alog struct {
	Off bool
	o   *Options
	m   tiny.M
}

func (a *Alog) Has(k string) bool {
	_, ok := a.m[k]
	return ok
}

func (a *Alog) Get(k string) interface{} {
	return a.m[k]
}

func (a *Alog) Set(k string, v interface{}) {
	a.m[k] = v
}

func (a *Alog) Del(k string) {
	delete(a.m, k)
}

func (a *Alog) setHeaders(h http.Header, m map[string]string) {
	for k, v := range m {
		if b, ok := h[v]; ok {
			a.Set(k, strings.Join(b, ", "))
		}
	}
}

func (a *Alog) writeTo(w io.Writer) {
	var err error
	switch a.o.Format {
	case "text":
		_, err = w.Write(a.Text())
	case "csv":
		c := csv.NewWriter(w)
		c.Comma = a.o.Comma
		c.UseCRLF = a.o.UseCRLF
		if err = c.Write(a.CSV()); err == nil {
			c.Flush()
			err = c.Error()
		}
	case "json":
		_, err = w.Write(a.JSON())
	}
	log.ErrError(err)
}

func (a *Alog) Text() []byte {
	var b [512]byte
	d := b[:0]
	for _, s := range a.o.Fields {
		d = AppendInterface(d, a.m[s])
	}
	d = LF(d)
	return d
}

func (a *Alog) CSV() []string {
	b := make([]string, len(a.o.Fields))
	for i, s := range a.o.Fields {
		if v := a.m[s]; v != nil {
			b[i] = fmt.Sprint(v)
		}
	}
	return b
}

func (a *Alog) JSON() []byte {
	p, err := json.Marshal(a.m)
	log.ErrError(err)
	return append(p, '\n')
}
