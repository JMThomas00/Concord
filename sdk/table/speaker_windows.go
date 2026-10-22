package table

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows plays a WAV from memory itself (winmm's PlaySound), so no file
// is needed and nothing is installed.
const soundNeedsFile = false

var procPlaySound = windows.NewLazySystemDLL("winmm.dll").NewProc("PlaySoundW")

func canPlaySounds() bool { return procPlaySound.Find() == nil }

// playSound starts data playing and returns. PlaySound plays one sound at a
// time, so a new one cuts off the last; data must stay alive while it
// plays (the speaker keeps every sound it loads).
func playSound(_ string, data []byte) error {
	const sndAsync, sndNoDefault, sndMemory = 0x1, 0x2, 0x4
	if len(data) == 0 {
		return nil
	}
	if r, _, err := procPlaySound.Call(uintptr(unsafe.Pointer(&data[0])), 0, sndAsync|sndNoDefault|sndMemory); r == 0 {
		return err
	}
	return nil
}
