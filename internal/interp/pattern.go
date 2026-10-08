package interp

import (
	"strings"
	"sync"
	"sync/atomic"
)

// patternToken is one element of a Java DateTimeFormatter pattern: either a
// run of one pattern letter or literal text.
type patternToken struct {
	letter byte
	count  int
	lit    string
}

// maxCachedPatterns bounds the pattern caches when scripts build patterns
// from data.
const maxCachedPatterns = 512

// patternCache memoizes tokenized patterns; scripts reuse a handful of
// constant patterns for every record.
var (
	patternCache sync.Map
	patternCount atomic.Int64
)

func tokenizePattern(p string) []patternToken {
	if cached, ok := patternCache.Load(p); ok {
		return cached.([]patternToken)
	}
	tokens := scanPattern(p)
	if patternCount.Add(1) <= maxCachedPatterns {
		patternCache.Store(p, tokens)
	}
	return tokens
}

func scanPattern(p string) []patternToken {
	var out []patternToken
	for i := 0; i < len(p); {
		c := p[i]
		switch {
		case c == '\'':
			lit, next := quotedLiteral(p, i)
			out = append(out, patternToken{lit: lit})
			i = next
		case isLetter(c):
			j := i
			for j < len(p) && p[j] == c {
				j++
			}
			out = append(out, patternToken{letter: c, count: j - i})
			i = j
		default:
			out = append(out, patternToken{lit: string(c)})
			i++
		}
	}
	return out
}

// quotedLiteral reads 'text' starting at the opening quote at i, where ”
// stands for a single quote, and returns the text and the next index.
func quotedLiteral(p string, i int) (string, int) {
	if i+1 < len(p) && p[i+1] == '\'' {
		return "'", i + 2
	}
	var lit strings.Builder
	j := i + 1
	for j < len(p) {
		if p[j] != '\'' {
			lit.WriteByte(p[j])
			j++
			continue
		}
		if j+1 < len(p) && p[j+1] == '\'' {
			lit.WriteByte('\'')
			j += 2
			continue
		}
		break
	}
	return lit.String(), j + 1
}
