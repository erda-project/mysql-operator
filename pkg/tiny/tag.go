package tiny

import (
	"regexp"
	"strings"
)

type Tag struct {
	Kind byte
	Name string
	Rule *regexp.Regexp
}

func (t Tag) Same(s Tag) bool {
	if t.Kind == s.Kind {
		switch t.Kind {
		case 'r':
			return t.Rule.String() == s.Rule.String()
		case 0:
			return t.Name == s.Name
		default:
			return true
		}
	}
	return false
}

func (t Tag) Boundary(s string) (i int) {
	switch t.Kind {
	case 'b':
		i = booleanBoundary(s)
	case 'i':
		i = integerBoundary(s)
	case 'n':
		i = numberBoundary(s)
	case 'p':
		i = partBoundary(s)
	case 'r':
		if a := t.Rule.FindStringIndex(s); a != nil && a[0] == 0 {
			i = a[1]
		}
	case 's':
		i = len(s)
	case 0:
		i = len(t.Name)
		if len(s) < i || s[:i] != t.Name {
			i = 0
		}
	}
	return
}

func (t Tag) String() (s string) {
	switch t.Kind {
	case 0:
		return t.Name
	case 'r':
		s = t.Name + t.Rule.String()
	case 'p':
		s = t.Name
	default:
		s = t.Name + string(meta[0]) + string(t.Kind)
	}
	return string(meta[1]) + s + string(meta[2])
}

func mustSplitPath(s string) (a []Tag) {
	a = splitPath(s)
	if len(a) == 0 && s != "" {
		panic(s)
	}
	return
}

const meta = ":<>^"

func splitPath(s string) (a []Tag) {
	if s != "" {
		for {
			i := strings.IndexByte(s, meta[1])
			j := strings.IndexByte(s, meta[2])
			if i == -1 && j == -1 {
				a = append(a, Tag{Name: s})
				return
			} else if i >= 0 && j >= 0 && i < j {
				if i > 0 {
					a = append(a, Tag{Name: s[:i]})
				}
				if t := splitTag(s[i+1 : j]); t.Kind > 0 {
					a = append(a, t)
					s = s[j+1:]
					if s != "" {
						continue
					}
					return
				}
			}
			break
		}
	}
	return nil
}

func splitTag(s string) (t Tag) {
	if strings.ContainsAny(s, meta[1:3]) {
		return
	}
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case meta[0]:
			switch s[i+1:] {
			case "b", "bool", "boolean":
				t.Kind = 'b'
			case "i", "int", "integer":
				t.Kind = 'i'
			case "n", "num", "number":
				t.Kind = 'n'
			case "s", "str", "string":
				t.Kind = 's'
			default:
				return
			}
			t.Name = s[:i]
			return
		case meta[3]:
			if r, err := regexp.Compile(s[i:]); err == nil {
				t.Kind = 'r'
				t.Name = s[:i]
				t.Rule = r
			}
			return
		}
	}
	t.Kind = 'p'
	t.Name = s
	return
}
