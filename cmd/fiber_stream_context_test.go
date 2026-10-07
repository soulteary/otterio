package cmd

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/valyala/fasthttp"
)

func TestStreamContextCanceledOnCompletionAndConsumerClose(t *testing.T) {
	for _, disconnect := range []bool{false, true} {
		t.Run(map[bool]string{false: "completion", true: "consumer-close"}[disconnect], func(t *testing.T) {
			app := newFiberApp()
			var request fasthttp.Request
			request.SetRequestURI("/stream")
			var reqCtx fasthttp.RequestCtx
			reqCtx.Init(&request, nil, nil)
			c := app.AcquireCtx(&reqCtx)
			defer app.ReleaseCtx(c)
			defer reqCtx.Response.CloseBodyStream()
			requestContext := make(chan context.Context, 1)
			handlerDone := make(chan struct{})
			err := toOtterioStreamHandler(func(w http.ResponseWriter, r *http.Request) {
				defer close(handlerDone)
				requestContext <- r.Context()
				w.WriteHeader(http.StatusOK)
				if disconnect {
					<-r.Context().Done()
				}
			})(c)
			if err != nil {
				t.Fatal(err)
			}
			ctx := <-requestContext
			if disconnect {
				if err := reqCtx.Response.CloseBodyStream(); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case <-ctx.Done():
			case <-time.After(time.Second):
				t.Fatal("stream context was not canceled")
			}
			select {
			case <-handlerDone:
			case <-time.After(time.Second):
				t.Fatal("stream handler did not terminate")
			}
		})
	}
}

func TestFiberRequestContextValuesSurviveReuse(t *testing.T) {
	var request fasthttp.Request
	var reqCtx fasthttp.RequestCtx
	reqCtx.Init(&request, nil, nil)
	reqCtx.SetUserValue("name", "original")
	ctx := newFiberRequestCtx(&reqCtx)
	reqCtx.ResetUserValues()
	reqCtx.SetUserValue("name", "replacement")
	if got := ctx.Value("name"); got != "original" {
		t.Fatalf("request context value changed after reuse: %v", got)
	}
}
