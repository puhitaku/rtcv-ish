package logging

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestNew(t *testing.T) {
	for _, format := range []string{FormatAuto, FormatJSON} {
		var buf bytes.Buffer
		New(&buf, format).Info("hello", "k", 1)
		var m map[string]any
		if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
			t.Fatalf("%s: output is not JSON: %q", format, buf.String())
		}
		if m["msg"] != "hello" {
			t.Errorf("%s: msg = %v", format, m["msg"])
		}
	}

	var buf bytes.Buffer
	New(&buf, FormatText).Info("hello", "k", 1)
	if out := buf.String(); !strings.Contains(out, "hello") || !strings.Contains(out, "k=1") || strings.Contains(out, "\x1b[") {
		t.Errorf("text output = %q", out)
	}
}

func TestValidFormat(t *testing.T) {
	if err := ValidFormat("xml"); err == nil {
		t.Error("want error for xml")
	}
	if err := ValidFormat(FormatText); err != nil {
		t.Error(err)
	}
}
