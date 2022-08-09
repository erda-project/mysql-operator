package tiny

import (
	"net/http"
	"reflect"

	"github.com/cxr29/log"
)

type Context struct {
	Writers       []http.ResponseWriter
	Request       *http.Request
	Values        map[interface{}]interface{}
	Params        []string
	tree          *Tree
	node          *Node
	wroteHeader   bool
	written       int64
	status, index int
}

func (ctx *Context) ViaWriter(w http.ResponseWriter) http.ResponseWriter {
	if w == nil || ctx.wroteHeader {
		return nil
	}
	ctx.Writers = append([]http.ResponseWriter{w}, ctx.Writers...)
	return ctx.Writers[1]
}

func (ctx *Context) RawWriter() http.ResponseWriter {
	return ctx.Writers[len(ctx.Writers)-1]
}

func (ctx *Context) Routed() bool {
	return ctx.node != nil
}

func (ctx *Context) call(i int) {
	var h Handler
	if j := i - len(ctx.tree.handlers); j < 0 {
		h = ctx.tree.handlers[i]
	} else if ctx.Routed() && j < len(ctx.node.handlers) {
		h = ctx.node.handlers[j]
	} else {
		return
	}
	h.ServeHTTP(ctx)
	if ctx.index == i && !ctx.wroteHeader {
		ctx.Next()
	}
}

func (ctx *Context) Next() {
	ctx.index++
	ctx.call(ctx.index)
}

func (ctx *Context) WriteHeader(code int) {
	if !ctx.wroteHeader {
		ctx.wroteHeader = true
		ctx.status = code
	}
	ctx.Writers[0].WriteHeader(code)
}

func (ctx *Context) Header() http.Header {
	return ctx.Writers[0].Header()
}

const contentType = "Content-Type"

func (ctx *Context) Write(data []byte) (int, error) {
	if !ctx.wroteHeader {
		h := ctx.Header()
		if h.Get("Transfer-Encoding") == "" && h.Get(contentType) == "" {
			h.Set(contentType, http.DetectContentType(data))
		}
		ctx.WriteHeader(http.StatusOK)
	}
	n, err := ctx.Writers[0].Write(data)
	log.ErrWarning(err)
	ctx.written += int64(n)
	return n, err
}

func (ctx *Context) WroteHeader() bool {
	return ctx.wroteHeader
}

func (ctx *Context) Written() int64 {
	return ctx.written
}

func (ctx *Context) Status() int {
	return ctx.status
}

func (ctx *Context) Param(name string) string {
	if ctx.Routed() {
		for i := len(ctx.node.params) - 1; i >= 0; i-- {
			if ctx.node.params[i] == name {
				return ctx.Params[i]
			}
		}
	}
	return ""
}

func (ctx *Context) HasValue(k interface{}) bool {
	_, ok := ctx.Values[k]
	return ok
}

func (ctx *Context) Value(k interface{}) interface{} {
	v, ok := ctx.Values[k]
	if !ok {
		panic("key not exist")
	}
	return v
}

func (ctx *Context) SetValue(k, v interface{}) {
	if k == nil {
		panic("nil key")
	} else if !reflect.TypeOf(k).Comparable() {
		panic("not comparable key")
	} else if ctx.HasValue(k) {
		panic("key already exists")
	}
	if ctx.Values == nil {
		ctx.Values = map[interface{}]interface{}{k: v}
	} else {
		ctx.Values[k] = v
	}
}
