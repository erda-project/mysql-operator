package mylet

import (
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/cxr29/log"
	"github.com/cxr29/tiny"
	"github.com/cxr29/tiny/alog"
	"github.com/cxr29/tiny/compress"
)

var (
	HttpAddr   = ":80"
	HttpsAddr  = os.Getenv("HTTPS_ADDR")
	HttpsCert  = os.Getenv("HTTPS_CERT")
	HttpsKey   = os.Getenv("HTTPS_KEY")
	Http2Https = false
)

func init() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	tiny.SetEnv(os.Getenv("TINY_ENV"))

	if v := os.Getenv("HTTP_ADDR"); v != "" {
		HttpAddr = v
	}
	if v, err := strconv.ParseBool(os.Getenv("HTTP2HTTPS")); err == nil {
		Http2Https = v
	}
}

func Serve() {
	tiny.Use(
		alog.New(nil),
		compress.New(nil),
		func(ctx *tiny.Context) {
			if Http2Https && ctx.Request.URL.Scheme != "https" {
				u := *ctx.Request.URL
				u.Scheme = "https"
				code := http.StatusTemporaryRedirect
				if ctx.Request.Method == "GET" {
					code = http.StatusFound
				}
				http.Redirect(ctx, ctx.Request, u.String(), code)
			}
		},
	)
	tiny.Fallback()

	if HttpAddr != "" {
		go func() {
			log.Infoln("HTTP Serve", HttpAddr)
			err := tiny.ListenAndServe(HttpAddr, nil)
			if err != http.ErrServerClosed {
				log.ErrFatal(err)
			}
		}()
	}

	if HttpsAddr != "" {
		go func() {
			log.Infoln("HTTPS Serve", HttpsAddr)
			err := tiny.ListenAndServeTLS(HttpsAddr, HttpsCert, HttpsKey, nil)
			if err != http.ErrServerClosed {
				log.ErrFatal(err)
			}
		}()
	}

	notify := make(chan os.Signal, 1)
	signal.Notify(notify, syscall.SIGINT, syscall.SIGTERM)
	log.Infoln("Wait Signal")
	<-notify
	log.Infoln("Signal Notify")
}
