package tiny

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"time"

	"github.com/cxr29/log"
)

type A = []interface{}
type M = map[string]interface{}

func (ctx *Context) IsAJAX() bool {
	return ctx.Request.Header.Get("X-Requested-With") == "XMLHttpRequest"
}

func (ctx *Context) WriteString(s string) (int, error) {
	return ctx.Write([]byte(s))
}

func (ctx *Context) WriteJSON(v interface{}) (int, error) {
	ctx.ContentTypeJSON()
	p, err := json.Marshal(v)
	if err != nil {
		log.Warningln(err)
		return 0, err
	}
	return ctx.Write(p)
}

func (ctx *Context) WriteXML(v interface{}) (int, error) {
	ctx.ContentTypeXML()
	p, err := xml.Marshal(v)
	if err != nil {
		log.Warningln(err)
		return 0, err
	}
	return ctx.Write(p)
}

func (ctx *Context) WriteData(v interface{}) (int, error) {
	return ctx.WriteJSON(M{"Data": v})
}

func (ctx *Context) WriteError(v interface{}) (int, error) {
	if err, ok := v.(error); ok {
		v = err.Error()
	}
	return ctx.WriteJSON(M{"Error": v})
}

func (ctx *Context) WriteErrorf(format string, a ...interface{}) (int, error) {
	return ctx.WriteError(fmt.Sprintf(format, a...))
}

func (ctx *Context) DecodeJSON(v interface{}) error {
	defer ctx.Request.Body.Close()
	return json.NewDecoder(ctx.Request.Body).Decode(v)
}

func (ctx *Context) DecodeXML(v interface{}) error {
	defer ctx.Request.Body.Close()
	return xml.NewDecoder(ctx.Request.Body).Decode(v)
}

func (ctx *Context) ServeFile(name string) {
	http.ServeFile(ctx, ctx.Request, name)
}

func (ctx *Context) ContentLength(i int) {
	ctx.Header().Set("Content-Length", strconv.Itoa(i))
}

func (ctx *Context) ContentType(s string) {
	ctx.Header().Set(contentType, s)
}

func (ctx *Context) ContentTypePlain() {
	ctx.ContentType("text/plain; charset=utf-8")
}

func (ctx *Context) ContentTypeHTML() {
	ctx.ContentType("text/html; charset=utf-8")
}

func (ctx *Context) ContentTypeJSON() {
	ctx.ContentType("application/json; charset=utf-8")
}

func (ctx *Context) ContentTypeXML() {
	ctx.ContentType("application/xml; charset=utf-8")
}

func (ctx *Context) ContentTypeCSV() {
	ctx.ContentType("text/csv; charset=utf-8")
}

func (ctx *Context) ContentTypeXLSX() {
	ctx.ContentType("application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
}

const filenameLength = 255

var filenameRegexp = regexp.MustCompile(`^[-.0-9A-Z_a-z]+$`)

func (ctx *Context) ContentDisposition(filename, fallback string) {
	if filename == "" || len(filename) > filenameLength {
		panic("malformed filename")
	} else if filenameRegexp.MatchString(filename) {
		filename = "attachment; filename=" + filename
	} else {
		filename = "attachment; filename*=UTF-8''" + url.QueryEscape(filename)
		if fallback != "" {
			if len(fallback) > filenameLength || !filenameRegexp.MatchString(fallback) {
				panic("malformed fallback")
			}
			filename += "; filename=" + fallback
		}
	}
	ctx.Header().Set("Content-Disposition", filename)
}

func (ctx *Context) MaxAge(seconds int) {
	v := time.Now().Add(time.Duration(seconds) * time.Second).Format(http.TimeFormat)
	h := ctx.Header()
	h.Set("Expires", v)
	h.Set("Cache-Control", fmt.Sprintf("max-age=%d", seconds))
}

func (ctx *Context) NoCache() {
	h := ctx.Header()
	h.Set("Expires", "0")
	h.Set("Cache-Control", "no-cache, no-store, must-revalidate")
	h.Set("Pragma", "no-cache")
}

func (ctx *Context) LastModified(t time.Time) {
	ctx.Header().Set("Last-Modified", t.Format(http.TimeFormat))
}

func (ctx *Context) IfModifiedSince(t time.Time) bool {
	v, _ := http.ParseTime(ctx.Request.Header.Get("If-Modified-Since"))
	return v.IsZero() || v.Unix() != t.Unix()
}

func (ctx *Context) ETag(s string) {
	ctx.Header().Set("ETag", s)
}

func (ctx *Context) IfNoneMatch(s string) bool {
	v := ctx.Request.Header.Get("If-None-Match")
	return v == "" || v != s
}

func (ctx *Context) MovedPermanently(location string) {
	http.Redirect(ctx, ctx.Request, location, http.StatusMovedPermanently)
}

func (ctx *Context) Found(location string) {
	http.Redirect(ctx, ctx.Request, location, http.StatusFound)
}

func (ctx *Context) NotModified() {
	ctx.WriteHeader(http.StatusNotModified)
}

func (ctx *Context) Error(s string, code int) {
	ef := ctx.tree.ef
	for n := ctx.node; n != nil; n = n.above {
		if n.ef != nil {
			ef = n.ef
			break
		}
	}
	if ef != nil {
		ef(ctx, s, code)
	} else {
		http.Error(ctx, s, code)
	}
}

func (ctx *Context) BadRequest() {
	ctx.Error(http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
}

func (ctx *Context) Forbidden() {
	ctx.Error(http.StatusText(http.StatusForbidden), http.StatusForbidden)
}

func (ctx *Context) NotFound() {
	ctx.Error(http.StatusText(http.StatusNotFound), http.StatusNotFound)
}

func (ctx *Context) InternalServerError() {
	ctx.Error(http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
}

func (ctx *Context) ServiceUnavailable() {
	ctx.Error(http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
}
