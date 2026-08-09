package ghdb

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// Typed IDs — never mix APK catalog ids with user request ids.
//
//   apk_12   → published app / APK listing
//   req_34   → user/company submission request
//   cat_5    → category
//   rev_7    → review
//

const (
	PrefixAPK     = "apk_"
	PrefixRequest = "req_"
	PrefixCat     = "cat_"
	PrefixReview  = "rev_"
)

func FormatID(prefix string, n int64) string {
	return fmt.Sprintf("%s%d", prefix, n)
}

func ParseID(id string) (prefix string, n int64, ok bool) {
	id = strings.TrimSpace(id)
	for _, p := range []string{PrefixAPK, PrefixRequest, PrefixCat, PrefixReview} {
		if strings.HasPrefix(id, p) {
			num, err := strconv.ParseInt(strings.TrimPrefix(id, p), 10, 64)
			if err != nil || num <= 0 {
				return p, 0, false
			}
			return p, num, true
		}
	}
	// legacy pure number → treat as unknown numeric
	if num, err := strconv.ParseInt(id, 10, 64); err == nil && num > 0 {
		return "", num, true
	}
	return "", 0, false
}

func MustBe(prefix, id string) (int64, error) {
	p, n, ok := ParseID(id)
	if !ok {
		return 0, fmt.Errorf("invalid id: %s", id)
	}
	if p != "" && p != prefix {
		return 0, fmt.Errorf("expected %sid, got %s", prefix, id)
	}
	// legacy numeric accepted only if caller allows — require prefix for new API
	if p == "" {
		return 0, fmt.Errorf("id must use prefix %s (got bare number %s)", prefix, id)
	}
	return n, nil
}

// NextTypedID allocates counter and returns prefixed string id.
func (s *Store) NextTypedID(ctx context.Context, counterName, prefix string) (string, int64, error) {
	n, err := s.NextID(ctx, counterName)
	if err != nil {
		return "", 0, err
	}
	return FormatID(prefix, n), n, nil
}

// Match row id field against path id (supports prefixed and legacy numeric).
func IDEquals(rowID any, want string) bool {
	s := ""
	switch t := rowID.(type) {
	case string:
		s = t
	case float64:
		s = FormatID("", int64(t)) // bare
		s = strconv.FormatInt(int64(t), 10)
	case int64:
		s = strconv.FormatInt(t, 10)
	case int:
		s = strconv.FormatInt(int64(t), 10)
	default:
		s = fmt.Sprint(rowID)
	}
	if s == want {
		return true
	}
	// compare numeric tails if both parse
	_, n1, ok1 := ParseID(s)
	_, n2, ok2 := ParseID(want)
	if ok1 && ok2 && n1 == n2 {
		// only equal across types if one is legacy bare and other prefixed — allow read compat
		p1, _, _ := ParseID(s)
		p2, _, _ := ParseID(want)
		if p1 == "" || p2 == "" || p1 == p2 {
			return p1 == p2 || p1 == "" || p2 == ""
		}
	}
	return false
}
