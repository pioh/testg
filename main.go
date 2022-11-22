package main

import (
	"github.com/valyala/fasthttp"
	"go.uber.org/zap"
	"net/http"
	"sync/atomic"
)

func main() {
	counter := int64(0)
	cfg := zap.NewProductionConfig()
	cfg.Encoding = "console"
	logger, _ := cfg.Build()

	fasthttp.ListenAndServe(":8080", func(ctx *fasthttp.RequestCtx) {
		n := atomic.AddInt64(&counter, 1)
		logger.Info("enter check", zap.Int64("", n))
		ctx.WriteString("ok")
		logger.Info("exit check", zap.Int64("", n))
	})

	http.ListenAndServe(":8080", http.DefaultServeMux)
}
