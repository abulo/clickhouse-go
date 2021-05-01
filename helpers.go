package clickhouse

import (
	"bytes"
	"database/sql/driver"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"strings"
	"time"
)

func numInput(query string) int {

	var (
		count         int
		args          = make(map[string]struct{})
		reader        = bytes.NewReader([]byte(query))
		quote, gravis bool
		escape        bool
		keyword       bool
		inBetween     bool
		like          = newMatcher("like")
		limit         = newMatcher("limit")
		between       = newMatcher("between")
		in            = newMatcher("in")
		and           = newMatcher("and")
	)
	for {
		if char, _, err := reader.ReadRune(); err == nil {
			if escape {
				escape = false
				continue
			}
			switch char {
			case '\\':
				if gravis || quote {
					escape = true
				}
			case '\'':
				if !gravis {
					quote = !quote
				}
			case '`':
				if !quote {
					gravis = !gravis
				}
			}
			if quote || gravis {
				continue
			}
			switch {
			case char == '?' && keyword:
				count++
			case char == '@':
				if param := paramParser(reader); len(param) != 0 {
					if _, found := args[param]; !found {
						args[param] = struct{}{}
						count++
					}
				}
			case
				char == '=',
				char == '<',
				char == '>',
				char == '(',
				char == ',',
				char == '[',
				char == '%':
				keyword = true
			default:
				if limit.matchRune(char) || like.matchRune(char) || in.matchRune(char) {
					keyword = true
				} else if between.matchRune(char) {
					keyword = true
					inBetween = true
				} else if inBetween && and.matchRune(char) {
					keyword = true
					inBetween = false
				} else {
					keyword = keyword && (char == ' ' || char == '\t' || char == '\n')
				}
			}
		} else {
			break
		}
	}
	return count
}

func paramParser(reader *bytes.Reader) string {
	var name bytes.Buffer
	for {
		if char, _, err := reader.ReadRune(); err == nil {
			if char == '_' || char >= '0' && char <= '9' || 'a' <= char && char <= 'z' || 'A' <= char && char <= 'Z' {
				name.WriteRune(char)
			} else {
				reader.UnreadRune()
				break
			}
		} else {
			break
		}
	}
	return name.String()
}

var selectRe = regexp.MustCompile(`\s+SELECT\s+`)

func isInsert(query string) bool {
	if f := strings.Fields(query); len(f) > 2 {
		return strings.EqualFold("INSERT", f[0]) && strings.EqualFold("INTO", f[1]) && !selectRe.MatchString(strings.ToUpper(query))
	}
	return false
}

func quote(v driver.Value) string {
	switch v := reflect.ValueOf(v); v.Kind() {
	case reflect.Slice:
		values := make([]string, 0, v.Len())
		for i := 0; i < v.Len(); i++ {
			values = append(values, quote(v.Index(i).Interface()))
		}
		return strings.Join(values, ", ")
	}
	switch v := v.(type) {
	case string:
		return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(v) + "'"
	case time.Time:
		return formatTime(v)
	}
	return fmt.Sprint(v)
}

func formatTime(value time.Time) string {
	// toDate() overflows after 65535 days, but toDateTime() only overflows when time.Time overflows (after 9223372036854775807 seconds)
	if days := value.Unix() / 24 / 3600; days <= math.MaxUint16 && (value.Hour()+value.Minute()+value.Second()+value.Nanosecond()) == 0 {
		return fmt.Sprintf("toDate(%d)", days)
	}
	return fmt.Sprintf("toDateTime(%d)", value.Unix())
}
