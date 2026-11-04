package installer

import (
	"strconv"
	"strings"
)

// NewerVersion reports whether release tag b is newer than version a
// ("v0.1.2" style, with release candidates: 0.1.0-rc3 < 0.1.0-rc4 <
// 0.1.0). Anything unparsable compares as not newer, so a "dev" build never
// asks to be updated.
func NewerVersion(a, b string) bool {
	va, preA, okA := ParseVersion(a)
	vb, preB, okB := ParseVersion(b)
	if !okA || !okB {
		return false
	}
	for i := range va {
		if vb[i] != va[i] {
			return vb[i] > va[i]
		}
	}
	switch {
	case preA == preB:
		return false
	case preB == "": // the final release after its candidates
		return true
	case preA == "":
		return false
	}
	// rc3 vs rc10: by the number at the end when the words match.
	wordA, numA := splitPre(preA)
	wordB, numB := splitPre(preB)
	if wordA == wordB && numA >= 0 && numB >= 0 {
		return numB > numA
	}
	return preB > preA
}

// ParseVersion reads "v1.2.3" or "1.2.3-rc4": the three numbers and what
// follows the dash.
func ParseVersion(v string) ([3]int, string, bool) {
	var out [3]int
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	pre := ""
	if i := strings.IndexByte(v, '-'); i >= 0 {
		v, pre = v[:i], v[i+1:]
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return out, "", false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return out, "", false
		}
		out[i] = n
	}
	return out, pre, true
}

// splitPre splits "rc12" into "rc" and 12 (-1 when there's no number).
func splitPre(p string) (string, int) {
	i := len(p)
	for i > 0 && p[i-1] >= '0' && p[i-1] <= '9' {
		i--
	}
	n, err := strconv.Atoi(p[i:])
	if err != nil {
		return p, -1
	}
	return p[:i], n
}
