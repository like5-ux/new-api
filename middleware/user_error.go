package middleware

import (
	"bytes"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func UserErrorResponses() gin.HandlerFunc {
	return func(c *gin.Context) {
		writer := &userErrorResponseWriter{ResponseWriter: c.Writer, context: c}
		c.Writer = writer
		c.Next()
		writer.flushError()
	}
}

type userErrorResponseWriter struct {
	gin.ResponseWriter
	context   *gin.Context
	errorBody bytes.Buffer
}

func (w *userErrorResponseWriter) Write(data []byte) (int, error) {
	if w.context.GetString(RouteTagKey) != "relay" {
		return w.ResponseWriter.Write(data)
	}
	if w.Status() >= 400 && service.NewUserErrorPresentation(w.context) != nil {
		return w.errorBody.Write(data)
	}
	if !model.IsRelayErrorJSON(data) {
		return w.ResponseWriter.Write(data)
	}
	public := service.PresentUserErrorJSON(w.context, data, w.Status())
	if bytes.Equal(public, data) {
		return w.ResponseWriter.Write(data)
	}
	w.Header().Del("Content-Length")
	_, err := w.ResponseWriter.Write(public)
	if err != nil {
		return 0, err
	}
	return len(data), nil
}

func (w *userErrorResponseWriter) WriteString(data string) (int, error) { return w.Write([]byte(data)) }

func (w *userErrorResponseWriter) flushError() {
	if w.errorBody.Len() == 0 {
		return
	}
	public := service.PresentUserErrorJSON(w.context, w.errorBody.Bytes(), w.Status())
	w.Header().Del("Content-Length")
	_, _ = w.ResponseWriter.Write(public)
	w.errorBody.Reset()
}

func (w *userErrorResponseWriter) Flush() {
	w.flushError()
	w.ResponseWriter.Flush()
}
