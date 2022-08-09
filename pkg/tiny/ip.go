package tiny

import (
	"net"
	"reflect"
	"strings"
)

func (ctx *Context) Hostname() string {
	colon := strings.IndexByte(ctx.Request.Host, ':')
	if colon == -1 {
		return ctx.Request.Host
	}
	if i := strings.IndexByte(ctx.Request.Host, ']'); i != -1 {
		return strings.TrimPrefix(ctx.Request.Host[:i], "[")
	}
	return ctx.Request.Host[:colon]
}

func (ctx *Context) ParseRemoteIP(realIP, forwardedFor bool) net.IP {
	if realIP {
		s := ctx.Request.Header.Get("X-Real-IP")
		ip := net.ParseIP(strings.TrimSpace(s))
		if ip != nil {
			return ip
		}
	}
	if forwardedFor {
		s := ctx.Request.Header.Get("X-Forwarded-For")
		if i := strings.Index(s, ","); i != -1 {
			s = s[:i]
		}
		ip := net.ParseIP(strings.TrimSpace(s))
		if ip != nil {
			return ip
		}
	}
	s, _, _ := net.SplitHostPort(ctx.Request.RemoteAddr)
	return net.ParseIP(s)
}

var ipKey = reflect.TypeOf(net.IP(nil))

func (ctx *Context) SetRemoteIP(ip net.IP) {
	ctx.SetValue(ipKey, ip)
}

func (ctx *Context) RemoteIP() (ip net.IP) {
	if v, ok := ctx.Values[ipKey]; ok {
		ip = v.(net.IP)
	} else {
		ip = ctx.ParseRemoteIP(false, false)
		ctx.SetRemoteIP(ip)
	}
	return
}
