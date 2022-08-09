package alog

import "strings"

const (
	X                = 'x'
	Slash            = '/'
	Backslash        = '\\'
	EscapeSequences  = "\x00\a\b\t\n\v\f\r\\"
	EscapedSequences = "0abtnvfr\\"
)

func mustHex(x byte) byte {
	if x < 10 {
		return x + '0'
	}
	if x < 16 {
		return x - 10 + 'a'
	}
	panic(x)
}

func Hex(v byte) []byte {
	return []byte{Backslash, X, mustHex(v / 16), mustHex(v % 16)}
}

func Escape(v byte) []byte {
	e := escape(v)
	switch e {
	case Slash:
		return []byte{v}
	case X:
		return Hex(v)
	default:
		return []byte{Backslash, e}
	}
}

func escape(b byte) byte {
	i := strings.IndexByte(EscapeSequences, b)
	if i == -1 {
		if b <= '\x1f' || b == '\x7f' {
			return X
		}
		return Slash
	}
	return EscapedSequences[i]
}
