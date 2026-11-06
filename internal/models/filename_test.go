package models

import "testing"

func TestSafeFilename(t *testing.T) {
	cases := map[string]string{
		"report.pdf":                         "report.pdf",
		"../.bashrc":                         ".bashrc",
		"../../.config/autostart/x.desktop":  "x.desktop",
		`..\..\AppData\Roaming\evil.bat`:     "evil.bat",
		"/etc/passwd":                        "passwd",
		"..":                                 "download",
		".":                                  "download",
		"":                                   "download",
		"...":                                "download",
		"a\x1b]52;c;aGk=\x07b.txt":           "a]52;c;aGk=b.txt",
		"what?.txt":                          "what_.txt",
		"CON.txt":                            "_CON.txt",
		"trailing dots... ":                  "trailing dots",
		"日本語のファイル.png":                      "日本語のファイル.png",
	}
	for in, want := range cases {
		if got := SafeFilename(in); got != want {
			t.Errorf("SafeFilename(%q) = %q, want %q", in, got, want)
		}
	}
}
