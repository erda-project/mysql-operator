package tiny

import "net/http"

type Route struct {
	Name, method string
	handlers     []Handler
	tags         []Tag
	above, below *Router
	index        int
}

type Router struct {
	ErrorFunc
	routes   []*Route
	handlers []Handler
	above    *Route
}

func (r *Router) Fallback() {
	r.Use(
		HandleNotImplemented,
		NewRedirectTrailingSlash(true),
		NewRedirectCleanedPath(true),
		NewAllowedMethods(true),
		// HandleNotFound,
	)
}

func (r *Router) Use(handlers ...interface{}) {
	r.handlers = append(r.handlers, newHandlers(handlers)...)
}

func (r *Router) Group(pattern string, f func(*Router), handlers ...interface{}) {
	rr := &Route{
		tags:     mustSplitPath(pattern),
		handlers: newHandlers(handlers),
		above:    r,
		below:    new(Router),
	}
	rr.below.above = rr
	r.routes = append(r.routes, rr)
	rr.index = len(r.routes)
	f(rr.below)
}

func (r *Router) Handle(method, pattern string, handlers ...interface{}) *Route {
	rr := &Route{
		method:   method,
		tags:     mustSplitPath(pattern),
		handlers: newHandlers(handlers),
		above:    r,
	}
	r.routes = append(r.routes, rr)
	return rr
}

func (r *Router) Any(pattern string, handlers ...interface{}) *Route {
	return r.Handle("", pattern, handlers...)
}

func (r *Router) CONNECT(pattern string, handlers ...interface{}) *Route {
	return r.Handle("CONNECT", pattern, handlers...)
}

func (r *Router) DELETE(pattern string, handlers ...interface{}) *Route {
	return r.Handle("DELETE", pattern, handlers...)
}

func (r *Router) GET(pattern string, handlers ...interface{}) *Route {
	return r.Handle("GET", pattern, handlers...)
}

func (r *Router) HEAD(pattern string, handlers ...interface{}) *Route {
	return r.Handle("HEAD", pattern, handlers...)
}

func (r *Router) OPTIONS(pattern string, handlers ...interface{}) *Route {
	return r.Handle("OPTIONS", pattern, handlers...)
}

func (r *Router) POST(pattern string, handlers ...interface{}) *Route {
	return r.Handle("POST", pattern, handlers...)
}

func (r *Router) PUT(pattern string, handlers ...interface{}) *Route {
	return r.Handle("PUT", pattern, handlers...)
}

func (r *Router) TRACE(pattern string, handlers ...interface{}) *Route {
	return r.Handle("TRACE", pattern, handlers...)
}

func (r *Router) FileServer(pattern, root string) *Route {
	return r.Any(pattern, http.FileServer(http.Dir(root)))
}

var DefaultRouter = new(Router)

func Fallback() {
	DefaultRouter.Fallback()
}

func Use(handlers ...interface{}) {
	DefaultRouter.Use(handlers...)
}

func Group(pattern string, f func(*Router), handlers ...interface{}) {
	DefaultRouter.Group(pattern, f, handlers...)
}

func Handle(method, pattern string, handlers ...interface{}) *Route {
	return DefaultRouter.Handle(method, pattern, handlers...)
}

func Any(pattern string, handlers ...interface{}) *Route {
	return DefaultRouter.Any(pattern, handlers...)
}

func GET(pattern string, handlers ...interface{}) *Route {
	return DefaultRouter.GET(pattern, handlers...)
}

func HEAD(pattern string, handlers ...interface{}) *Route {
	return DefaultRouter.HEAD(pattern, handlers...)
}

func POST(pattern string, handlers ...interface{}) *Route {
	return DefaultRouter.POST(pattern, handlers...)
}

func PUT(pattern string, handlers ...interface{}) *Route {
	return DefaultRouter.PUT(pattern, handlers...)
}

func DELETE(pattern string, handlers ...interface{}) *Route {
	return DefaultRouter.DELETE(pattern, handlers...)
}

func CONNECT(pattern string, handlers ...interface{}) *Route {
	return DefaultRouter.CONNECT(pattern, handlers...)
}

func OPTIONS(pattern string, handlers ...interface{}) *Route {
	return DefaultRouter.OPTIONS(pattern, handlers...)
}

func TRACE(pattern string, handlers ...interface{}) *Route {
	return DefaultRouter.TRACE(pattern, handlers...)
}

func FileServer(pattern, root string) *Route {
	return DefaultRouter.FileServer(pattern, root)
}
