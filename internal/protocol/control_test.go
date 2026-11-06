package protocol

import (
	"encoding/json"
	"testing"
)

func TestStripControlJSON(t *testing.T) {
	type doc struct {
		Name    string   `json:"name"`
		Content string   `json:"content"`
		List    []string `json:"list"`
	}
	in := doc{
		Name:    "eve\x1b]52;c;cm0gLXJmIH4=\x07",
		Content: "look: `\x1b[2J` and \u009b31m and \x7f\r\nline two\tok",
		List:    []string{"a\x00b", `back\slash \u001b stays as text`, "\"quoted\""},
	}
	raw, _ := json.Marshal(in)
	var got doc
	if err := json.Unmarshal(StripControlJSON(raw), &got); err != nil {
		t.Fatalf("stripped JSON doesn't parse: %v\n%s", err, StripControlJSON(raw))
	}
	want := doc{
		Name:    "eve]52;c;cm0gLXJmIH4=",
		Content: "look: `[2J` and 31m and \nline two\tok",
		List:    []string{"ab", `back\slash \u001b stays as text`, "\"quoted\""},
	}
	if got.Name != want.Name || got.Content != want.Content || got.List[0] != want.List[0] || got.List[1] != want.List[1] || got.List[2] != want.List[2] {
		t.Fatalf("got %#v\nwant %#v", got, want)
	}
	// A hostile server could write the escapes itself.
	hand := []byte(`{"name":"x\u001B[31my\u0085z\u0007"}`)
	var h doc
	if err := json.Unmarshal(StripControlJSON(hand), &h); err != nil || h.Name != "x[31myz" {
		t.Fatalf("hand-written escapes: %q %v", h.Name, err)
	}
	plain := []byte(`{"name":"nothing to do"}`)
	if out := StripControlJSON(plain); string(out) != string(plain) {
		t.Fatalf("changed plain JSON: %s", out)
	}
}

func TestStripControl(t *testing.T) {
	if got := StripControl("a\x1b[1mb\u009bc\nd\te"); got != "a[1mbc\nd\te" {
		t.Fatalf("%q", got)
	}
	if !HasControl("x\x1by") || HasControl("plain\nand\ttab é 日本") {
		t.Fatal("HasControl wrong")
	}
}

func TestMessageStripControl(t *testing.T) {
	msg, _ := NewDispatch(EventMessageCreate, 1, map[string]string{"content": "`\x1b]52;c;aGk=\x07`"})
	msg.StripControl()
	if string(msg.Data) != `{"content":"` + "`]52;c;aGk=`" + `"}` {
		t.Fatalf("a message kept its escapes: %s", msg.Data)
	}
	sig, _ := NewDispatch(EventVoiceSignal, 2, map[string]string{"sdp": "v=0\r\no=- 1"})
	before := string(sig.Data)
	sig.StripControl()
	if string(sig.Data) != before {
		t.Fatalf("a voice signal lost its CRLF: %s", sig.Data)
	}
}
