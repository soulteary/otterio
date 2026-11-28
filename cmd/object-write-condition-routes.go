// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package cmd

import "net/http"

// rejectObjectWriteConditions prevents a route that has no destination
// precondition implementation from silently performing an unconditional write.
// Source-copy conditions use different headers and remain supported.
func rejectObjectWriteConditions(api string, next func(http.ResponseWriter, *http.Request)) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if len(r.Header.Values("If-Match")) != 0 || len(r.Header.Values("If-None-Match")) != 0 {
			ctx := newContext(r, w, api)
			writeErrorResponse(ctx, w, errorCodes.ToAPIErr(ErrNotImplemented), r.URL)
			return
		}
		next(w, r)
	}
}

func guardUnsupportedObjectWriteConditions(api string, next func(http.ResponseWriter, *http.Request)) func(http.ResponseWriter, *http.Request) {
	switch api {
	case "copyobjectpart", "putobjectpart", "newmultipartupload", "abortmultipartupload",
		"deletemultipleobjects", "postpolicybucket", "putobjectacl",
		"putobjecttagging", "deleteobjecttagging", "putobjectretention", "putobjectlegalhold", "restoreobject":
		return rejectObjectWriteConditions(api, next)
	default:
		return next
	}
}
