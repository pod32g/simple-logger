package log_test

import (
	"bytes"
	"testing"

	log "github.com/pod32g/simple-logger"
)

func FuzzJSONFormatterFormat(f *testing.F) {
	f.Add("hello", []byte("world"))
	f.Add("", []byte{})

	f.Fuzz(func(t *testing.T, msg string, blob []byte) {
		formatter := &log.JSONFormatter{}
		buf := bytes.Buffer{}
		formatter.FormatWithFieldsTo(log.INFO, msg, []log.Field{
			log.String("blob", string(blob)),
		}, &buf)

		_ = formatter.FormatWithFields(log.ERROR, msg, nil)
	})
}
