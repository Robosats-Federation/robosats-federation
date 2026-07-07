package server

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sort"
	"time"
)

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

type requestLogFieldsKey struct{}

type requestLogFields struct {
	values map[string]string
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(data []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(data)
	r.bytes += n
	return n, err
}

func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		recorder := &statusRecorder{ResponseWriter: w}
		fields := &requestLogFields{values: map[string]string{}}
		r = r.WithContext(context.WithValue(r.Context(), requestLogFieldsKey{}, fields))
		next.ServeHTTP(recorder, r)
		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}
		log.Printf(
			"request method=%s path=%s status=%d bytes=%d duration_ms=%d remote=%s%s",
			r.Method,
			r.URL.Path,
			status,
			recorder.bytes,
			time.Since(started).Milliseconds(),
			r.RemoteAddr,
			fields.render(),
		)
	})
}

func setLogField(r *http.Request, key string, value any) {
	if r == nil || key == "" || value == nil {
		return
	}
	fields, _ := r.Context().Value(requestLogFieldsKey{}).(*requestLogFields)
	if fields == nil {
		return
	}
	text := fmt.Sprint(value)
	if text == "" {
		return
	}
	fields.values[key] = text
}

func setLogFieldInt64(r *http.Request, key string, value int64) {
	if value == 0 {
		return
	}
	setLogField(r, key, value)
}

func setLogFieldInt64Ptr(r *http.Request, key string, value *int64) {
	if value == nil {
		return
	}
	setLogField(r, key, *value)
}

func (f *requestLogFields) render() string {
	if f == nil || len(f.values) == 0 {
		return ""
	}
	keys := make([]string, 0, len(f.values))
	for key := range f.values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := ""
	for _, key := range keys {
		out += " " + key + "=" + f.values[key]
	}
	return out
}
