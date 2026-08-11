package server

import "testing"

func TestMentionsTrigger(t *testing.T) {
	tests := []struct {
		name    string
		content string
		trigger string
		want    bool
	}{
		{"exact mention", "hey @burt what's up", "burt", true},
		{"case insensitive", "hey @BURT what's up", "burt", true},
		{"mention at start", "@burt hello", "burt", true},
		{"mention at end", "hello @burt", "burt", true},
		{"no mention", "just a normal message", "burt", false},
		{"similar name is not a match", "hey @burton how's it going", "burt", false},
		{"substring elsewhere in the word doesn't match", "concert @burty", "burt", false},
		{"different trigger word", "hey @alice", "burt", false},
		{"trigger word without the @ doesn't match", "burt is cool", "burt", false},
		{"punctuation immediately after the mention still matches", "@burt, can you help?", "burt", true},
		{"empty content", "", "burt", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mentionsTrigger(tt.content, tt.trigger)
			if got != tt.want {
				t.Errorf("mentionsTrigger(%q, %q) = %v, want %v", tt.content, tt.trigger, got, tt.want)
			}
		})
	}
}
