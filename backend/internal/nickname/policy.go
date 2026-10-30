// Package nickname checks new display-name writes, never historical reads or
// authentication identifiers. Its explicit local rules are not semantic moderation.
package nickname

import (
	"errors"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

var ErrFormat = errors.New("昵称须为 1–32 个字符，不能包含控制字符")
var ErrRestricted = errors.New("昵称包含保留名称或不当内容，请换一个")

const QQFallback = "QQ读者"

var englishTerms = []string{
	"admin", "administrator", "root", "system", "sysadmin", "superadmin", "superuser",
	"official", "support", "customerservice", "moderator", "webmaster", "rsspal",
	"fuck", "fucking", "fuckyou", "fuckoff", "motherfucker", "shit", "bullshit", "bitch", "asshole", "cunt",
	"nigger", "heilhitler", "whitepower",
}

var chineseTerms = []string{
	"管理员", "管理員", "官方", "客服", "超级管理", "超級管理", "系统管理", "系統管理",
	"傻逼", "傻屄", "傻屌", "操你妈", "操你媽", "草你妈", "草你媽", "操你娘", "去你妈", "去你媽",
	"死全家", "全家死光", "婊子", "狗娘养", "狗娘養",
	"裸聊", "约炮", "約炮", "招嫖", "援交", "卖淫", "賣淫", "色情", "黄片", "黃片", "成人视频", "成人視頻",
}

var englishPatterns = func() []*regexp.Regexp {
	patterns := make([]*regexp.Regexp, 0, len(englishTerms))
	for _, term := range englishTerms {
		// Word boundaries avoid false positives such as badminton and supporter;
		// separators between letters still catch a.d.m.i.n and f u c k.
		patterns = append(patterns, regexp.MustCompile(`(^|[^a-z])`+strings.Join(strings.Split(term, ""), `[\s\p{P}\p{S}\p{Cf}]*`)+`([^a-z]|$)`))
	}
	return patterns
}()

var leet = strings.NewReplacer("0", "o", "1", "i", "3", "e", "4", "a", "5", "s", "7", "t")

func comparisonText(name string) string {
	// Normalize only the comparison copy; accepted display spelling is retained.
	name = cases.Fold().String(norm.NFKD.String(name))
	return strings.Map(func(r rune) rune {
		if unicode.IsMark(r) {
			return -1
		}
		// Common Latin lookalikes used to impersonate authority names.
		switch r {
		case 'а', 'α':
			return 'a'
		case 'ԁ':
			return 'd'
		case 'і', 'ι':
			return 'i'
		case 'м':
			return 'm'
		case 'о', 'ο':
			return 'o'
		case 'ѕ':
			return 's'
		}
		return r
	}, name)
}

func restricted(name string) bool {
	text := comparisonText(name)
	for _, candidate := range []string{text, leet.Replace(text)} {
		for _, pattern := range englishPatterns {
			if pattern.MatchString(candidate) {
				return true
			}
		}
	}
	compact := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		return -1
	}, text)
	for _, term := range chineseTerms {
		if strings.Contains(compact, term) {
			return true
		}
	}
	return false
}

// Validate rejects a new custom name before its once-only choice is consumed.
func Validate(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" || !utf8.ValidString(name) || utf8.RuneCountInString(name) > 32 || strings.ContainsFunc(name, func(r rune) bool {
		return unicode.IsControl(r) || unicode.Is(unicode.Cf, r)
	}) {
		return "", ErrFormat
	}
	if restricted(name) {
		return "", ErrRestricted
	}
	return name, nil
}

// QQ never fills users.nickname: a safe fallback retains the one custom choice.
func QQ(raw string) string {
	name := strings.TrimSpace(raw)
	if name == "" || !utf8.ValidString(name) || utf8.RuneCountInString(name) > 128 || strings.ContainsFunc(name, func(r rune) bool {
		return unicode.IsControl(r) || (unicode.Is(unicode.Cf, r) && r != '\u200d' && r != '\u200c')
	}) || restricted(name) {
		return QQFallback
	}
	return name
}
