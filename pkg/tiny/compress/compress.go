package compress

import (
	"compress/flate"
	"compress/gzip"
	"io"
	"net/http"
	"reflect"
	"strings"

	"github.com/cxr29/log"
	"github.com/cxr29/tiny"
)

const (
	acceptEncoding   = "Accept-Encoding"
	acceptRanges     = "Accept-Ranges"
	contentEncoding  = "Content-Encoding"
	contentLength    = "Content-Length"
	contentRange     = "Content-Range"
	contentType      = "Content-Type"
	transferEncoding = "Transfer-Encoding"
	vary             = "Vary"
)

type Options struct {
	Level   int
	Gzip    bool
	Deflate bool
}

var DefaultOptions = &Options{flate.BestSpeed, true, true}

var key = reflect.TypeOf((*Compressor)(nil)).Elem()

func Pull(ctx *tiny.Context) *Compressor {
	v, ok := ctx.Values[key]
	if ok && v != nil {
		return v.(*Compressor)
	}
	return nil
}

func Off(ctx *tiny.Context) {
	v, ok := ctx.Values[key]
	if !ok {
		ctx.SetValue(key, nil)
	} else if v != nil {
		v.(*Compressor).Off = true
	}
}

func New(o *Options) tiny.HandlerFunc {
	if o == nil {
		o = DefaultOptions
	}
	return func(ctx *tiny.Context) {
		if ctx.HasValue(key) {
			return
		}
		s := strings.ToLower(ctx.Request.Header.Get(acceptEncoding))
		if o.Gzip && strings.Contains(s, "gzip") {
			s = "gzip"
		} else if o.Deflate && strings.Contains(s, "deflate") {
			s = "deflate"
		} else {
			return
		}
		c := &Compressor{o: o, ce: s}
		c.rw = ctx.ViaWriter(c)
		ctx.SetValue(key, c)
		defer func() {
			if c.flag == 1 {
				log.ErrError(c.wc.Close())
			}
		}()
		ctx.Next()
	}
}

type Compressor struct {
	Off  bool
	rw   http.ResponseWriter
	o    *Options
	ce   string
	flag int8
	wc   io.WriteCloser
}

func allowedStatusCode(sc int) bool {
	switch {
	case sc >= 100 && sc <= 199:
		return false
	case sc == 204 || sc == 304:
		return false
	}
	return true
}

func allowedContentType(ct string) bool {
	var s string
	if i := strings.Index(ct, "/"); i >= 0 {
		s = ct[i+1:]
		ct = ct[:i]
	}
	for _, i := range [...]string{"image", "audio", "video"} {
		if ct == i {
			return false
		}
	}
	if ct == "application" {
		for _, i := range [...]string{"ogg", "x-rar-compressed", "zip", "x-gzip"} {
			if s == i {
				return false
			}
		}
	}
	return true
}

func (c *Compressor) newWriter() bool {
	var err error
	switch c.ce {
	case "gzip":
		c.wc, err = gzip.NewWriterLevel(c.rw, c.o.Level)
	case "deflate":
		c.wc, err = flate.NewWriter(c.rw, c.o.Level)
	}
	if err == nil {
		return true
	}
	log.Errorln(err)
	return false
}

func (c *Compressor) initialize(data []byte) {
	if c.flag != 0 {
		return
	}
	if !c.Off {
		h := c.Header()
		if h.Get(contentEncoding) == "" && h.Get(contentRange) == "" {
			ct := h.Get(contentType)
			if h.Get(transferEncoding) == "" && ct == "" {
				ct = http.DetectContentType(data)
				h.Set(contentType, ct)
			}
			if allowedContentType(ct) && c.newWriter() {
				h.Del(contentLength)
				h.Del(acceptRanges)
				h.Set(contentEncoding, c.ce)
				h.Set(vary, acceptEncoding)
				c.flag = 1
				return
			}
		}
	}
	c.flag = -1
}

func (c *Compressor) Header() http.Header {
	return c.rw.Header()
}

func (c *Compressor) WriteHeader(code int) {
	if allowedStatusCode(code) {
		c.initialize(nil)
	} else if c.flag == 0 {
		c.flag = -1
	}
	c.rw.WriteHeader(code)
}

func (c *Compressor) Write(data []byte) (int, error) {
	c.initialize(data)
	if c.flag == 1 {
		return c.wc.Write(data)
	}
	return c.rw.Write(data)
}
