package nickname

import (
	"strings"
	"testing"
)

func TestRestrictedNames(t *testing.T) {
	for _, name := range []string{
		"admin", " Admin ", "ADMIN123", "ＡＤＭＩＮ_01", "a.d.m.i.n", "a d m i n", "𝔸𝔻𝕄𝕀ℕ", "ádmin", "admіn", "adm1n",
		"administrator", "root", "r00t", "system", "support", "official", "moderator", "webmaster", "RSS Pal", "rss_pal_admin", "我是admin", "我的 Admin 账号",
		"管理员", "超级管理員", "管 理 員", "官方客服001", "系統管理員", "RSS Pal官方",
		"傻逼", "傻-逼-01", "操你妈", "裸聊加微信", "约炮小号", "f.u.c.k", "fuck you", "FUCKING", "motherfucker", "white power", "heil hitler",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Validate(name); err == nil {
				t.Errorf("restricted nickname accepted: %q", name)
			}
			if got := QQ(name); got != QQFallback {
				t.Errorf("unsafe QQ name retained: %q", got)
			}
		})
	}
}

func TestOrdinaryNamesAndFormat(t *testing.T) {
	for _, name := range []string{"Firen", "普通读者", "QQ读者", "badminton", "supporter", "administration笔记", "Scunthorpe", "theroots", "AI 读者", "小王🙂", "🙂", "张三", "草莓同学", "陈根生", "我爱读书"} {
		if got, err := Validate("  " + name + "  "); err != nil || got != name {
			t.Errorf("ordinary nickname rejected: %q %q %v", name, got, err)
		}
		if got := QQ(name); got != name {
			t.Errorf("ordinary QQ name changed: %q", got)
		}
	}
	for _, name := range []string{"", " ", "bad\nname", "ad\u200bmin", "name\u202eevil", strings.Repeat("名", 33), string([]byte{0xff})} {
		if _, err := Validate(name); err == nil {
			t.Errorf("bad format accepted: %q", name)
		}
	}
	for _, name := range []string{"👩‍💻读者", strings.Repeat("名", 100)} {
		if got := QQ(name); got != name {
			t.Errorf("valid provider profile changed: %q", got)
		}
	}
}
