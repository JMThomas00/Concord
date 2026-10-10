package arcade

import "strings"

// font is the arcade pixel font: 5 x 7 pixels per letter, a column of
// space between letters. In half-blocks a letter is 5 cells wide and 3.5
// rows tall.
var font = map[rune][7]string{
	'A':  {".###.", "#...#", "#...#", "#####", "#...#", "#...#", "#...#"},
	'B':  {"####.", "#...#", "#...#", "####.", "#...#", "#...#", "####."},
	'C':  {".####", "#....", "#....", "#....", "#....", "#....", ".####"},
	'D':  {"####.", "#...#", "#...#", "#...#", "#...#", "#...#", "####."},
	'E':  {"#####", "#....", "#....", "####.", "#....", "#....", "#####"},
	'F':  {"#####", "#....", "#....", "####.", "#....", "#....", "#...."},
	'G':  {".####", "#....", "#....", "#.###", "#...#", "#...#", ".###."},
	'H':  {"#...#", "#...#", "#...#", "#####", "#...#", "#...#", "#...#"},
	'I':  {"#####", "..#..", "..#..", "..#..", "..#..", "..#..", "#####"},
	'J':  {"..###", "...#.", "...#.", "...#.", "#..#.", "#..#.", ".##.."},
	'K':  {"#...#", "#..#.", "#.#..", "##...", "#.#..", "#..#.", "#...#"},
	'L':  {"#....", "#....", "#....", "#....", "#....", "#....", "#####"},
	'M':  {"#...#", "##.##", "#.#.#", "#.#.#", "#...#", "#...#", "#...#"},
	'N':  {"#...#", "##..#", "#.#.#", "#..##", "#...#", "#...#", "#...#"},
	'O':  {".###.", "#...#", "#...#", "#...#", "#...#", "#...#", ".###."},
	'P':  {"####.", "#...#", "#...#", "####.", "#....", "#....", "#...."},
	'Q':  {".###.", "#...#", "#...#", "#...#", "#.#.#", "#..#.", ".##.#"},
	'R':  {"####.", "#...#", "#...#", "####.", "#.#..", "#..#.", "#...#"},
	'S':  {".####", "#....", "#....", ".###.", "....#", "....#", "####."},
	'T':  {"#####", "..#..", "..#..", "..#..", "..#..", "..#..", "..#.."},
	'U':  {"#...#", "#...#", "#...#", "#...#", "#...#", "#...#", ".###."},
	'V':  {"#...#", "#...#", "#...#", "#...#", "#...#", ".#.#.", "..#.."},
	'W':  {"#...#", "#...#", "#...#", "#.#.#", "#.#.#", "##.##", "#...#"},
	'X':  {"#...#", "#...#", ".#.#.", "..#..", ".#.#.", "#...#", "#...#"},
	'Y':  {"#...#", "#...#", ".#.#.", "..#..", "..#..", "..#..", "..#.."},
	'Z':  {"#####", "....#", "...#.", "..#..", ".#...", "#....", "#####"},
	'0':  {".###.", "#...#", "#..##", "#.#.#", "##..#", "#...#", ".###."},
	'1':  {"..#..", ".##..", "..#..", "..#..", "..#..", "..#..", ".###."},
	'2':  {".###.", "#...#", "....#", "...#.", "..#..", ".#...", "#####"},
	'3':  {"####.", "....#", "....#", ".###.", "....#", "....#", "####."},
	'4':  {"...#.", "..##.", ".#.#.", "#..#.", "#####", "...#.", "...#."},
	'5':  {"#####", "#....", "####.", "....#", "....#", "#...#", ".###."},
	'6':  {".###.", "#....", "#....", "####.", "#...#", "#...#", ".###."},
	'7':  {"#####", "....#", "...#.", "..#..", ".#...", ".#...", ".#..."},
	'8':  {".###.", "#...#", "#...#", ".###.", "#...#", "#...#", ".###."},
	'9':  {".###.", "#...#", "#...#", ".####", "....#", "....#", ".###."},
	'!':  {"..#..", "..#..", "..#..", "..#..", "..#..", ".....", "..#.."},
	'?':  {".###.", "#...#", "....#", "...#.", "..#..", ".....", "..#.."},
	'.':  {".....", ".....", ".....", ".....", ".....", ".....", "..#.."},
	'-':  {".....", ".....", ".....", ".###.", ".....", ".....", "....."},
	'\'': {"..#..", "..#..", ".....", ".....", ".....", ".....", "....."},
	' ':  {".....", ".....", ".....", ".....", ".....", ".....", "....."},
}

// LogoShade is the standard logo shading, one role per pixel row: light at
// the top, the brand purple, then pink at the foot.
var LogoShade = []string{"hi", "hi", "purple", "purple", "purple", "pink", "pink"}

// LogoWidth is how many cells Logo takes for text at scale.
func LogoWidth(text string, scale int) int {
	n := len([]rune(text))
	if n == 0 {
		return 0
	}
	return n*6*max(1, scale) - max(1, scale)
}

// LogoHeight is how many rows Logo takes at scale, drop shadow included.
func LogoHeight(scale int) int { return (7*max(1, scale) + 2) / 2 }

// Logo draws text in the arcade pixel font from cell column x and
// half-row py, with a drop shadow one pixel down and right. shade gives
// each pixel row's role (one role for a single colour); nil uses LogoShade.
// Unknown characters draw as spaces.
func (c *Canvas) Logo(text string, x, py, scale int, shade []string) {
	scale = max(1, scale)
	if len(shade) == 0 {
		shade = LogoShade
	}
	text = strings.ToUpper(text)
	for _, pass := range []string{"shadow", "face"} {
		for n, ch := range []rune(text) {
			g, ok := font[ch]
			if !ok {
				g = font[' ']
			}
			for j, row := range g {
				role := shade[min(j, len(shade)-1)]
				for i, k := range row {
					if k != '#' {
						continue
					}
					for sy := 0; sy < scale; sy++ {
						for sx := 0; sx < scale; sx++ {
							X, Y := x+(n*6+i)*scale+sx, py+j*scale+sy
							if pass == "shadow" {
								c.Px(X+1, Y+1, "shadow")
							} else {
								c.Px(X, Y, role)
							}
						}
					}
				}
			}
		}
	}
}

// Seven-segment digits, 4 x 7 pixels, with the unlit segments drawn in
// "ghost" so the display's shape always shows.
var (
	segOn = map[int]string{0: "abcdef", 1: "bc", 2: "abged", 3: "abgcd", 4: "fgbc", 5: "afgcd", 6: "afgedc", 7: "abc", 8: "abcdefg", 9: "abcdfg"}
	segPx = map[byte][][2]int{
		'a': {{1, 0}, {2, 0}}, 'b': {{3, 1}, {3, 2}}, 'c': {{3, 4}, {3, 5}}, 'd': {{1, 6}, {2, 6}},
		'e': {{0, 4}, {0, 5}}, 'f': {{0, 1}, {0, 2}}, 'g': {{1, 3}, {2, 3}},
	}
	segCorners = []struct {
		at   [2]int
		segs string
	}{{[2]int{0, 0}, "af"}, {[2]int{3, 0}, "ab"}, {[2]int{0, 3}, "feg"}, {[2]int{3, 3}, "bcg"}, {[2]int{0, 6}, "ed"}, {[2]int{3, 6}, "cd"}}
)

// DigitWidth is a seven-segment digit's width in cells at scale, gap
// included.
func DigitWidth(scale int) int { return 5 * max(1, scale) }

// Digit draws one seven-segment digit (d < 0: all segments unlit) in role
// on, from column x and half-row py.
func (c *Canvas) Digit(d, x, py int, on string, scale int) {
	scale = max(1, scale)
	lit := ""
	if d >= 0 && d <= 9 {
		lit = segOn[d]
	}
	put := func(i, j int, role string) {
		for sy := 0; sy < scale; sy++ {
			for sx := 0; sx < scale; sx++ {
				c.Px(x+i*scale+sx, py+j*scale+sy, role)
			}
		}
	}
	for s, pts := range segPx {
		role := "ghost"
		if strings.IndexByte(lit, s) >= 0 {
			role = on
		}
		for _, p := range pts {
			put(p[0], p[1], role)
		}
	}
	for _, k := range segCorners {
		role := "ghost"
		if strings.ContainsAny(lit, k.segs) && lit != "" {
			role = on
		}
		put(k.at[0], k.at[1], role)
	}
}

// Number draws n as seven-segment digits, zero-padded to digits.
func (c *Canvas) Number(n, digits, x, py int, on string, scale int) {
	s := []rune(strings.Repeat("0", max(0, digits)) + itoa(max(0, n)))
	s = s[max(0, len(s)-max(digits, len(itoa(max(0, n))))):]
	for k, r := range s {
		c.Digit(int(r-'0'), x+k*DigitWidth(scale), py, on, scale)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
