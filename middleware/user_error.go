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
	if public, ok := presentSSEData(w.context, data); ok {
		w.Header().Del("Content-Length")
		_, err := w.ResponseWriter.Write(public)
		if err != nil {
			return 0, err
		}
		return len(data), nil
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

// presentSSEData masks error JSON carried in a complete SSE data line. Several
// adaptors render data frames directly instead of using relay/helper.
func presentSSEData(c *gin.Context, data []byte) ([]byte, bool) {
	payloadStart := 0
	for payloadStart < len(data) && (data[payloadStart] == ' ' || data[payloadStart] == '\t') {
		payloadStart++
	}
	if !bytes.HasPrefix(data[payloadStart:], []byte("data:")) {
		return data, false
	}
	payloadStart += len("data:")
	for payloadStart < len(data) && (data[payloadStart] == ' ' || data[payloadStart] == '\t') {
		payloadStart++
	}
	payloadEnd := len(data)
	for payloadEnd > payloadStart && (data[payloadEnd-1] == '\r' || data[payloadEnd-1] == '\n') {
		payloadEnd--
	}
	payload := data[payloadStart:payloadEnd]
	if !model.IsRelayErrorJSON(payload) {
		return data, false
	}
	public := service.PresentUserErrorJSON(c, payload, 0)
	if bytes.Equal(public, payload) {
		return data, false
	}
	result := make([]byte, 0, len(data)-len(payload)+len(public))
	result = append(result, data[:payloadStart]...)
	result = append(result, public...)
	result = append(result, data[payloadEnd:]...)
	return result, true
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
