package taskbudget

import (
	"fmt"
	"strings"
	"time"
)

// LimitError preserves the admission decision through wrapped AI errors.
// RetryAt is known for daily limits; concurrency release times are not known.
type LimitError struct {
	Reason  string
	Bucket  string
	Used    int
	Limit   int
	RetryAt *time.Time
}

func (e *LimitError) Unwrap() error { return ErrExceeded }

func (e *LimitError) Error() string {
	scope := "个人"
	if strings.HasPrefix(e.Reason, "global_") {
		scope = "全站"
	}
	task := "任务"
	if e.Bucket == "ai" {
		task = "AI 调用"
	}
	if strings.HasSuffix(e.Reason, "_concurrency") {
		return fmt.Sprintf("%s%s并发已达上限（%d/%d），请等待正在执行的任务结束后重试", scope, task, e.Used, e.Limit)
	}
	msg := fmt.Sprintf("%s每日%s额度不足（已使用 %d/%d）", scope, task, e.Used, e.Limit)
	if e.RetryAt != nil {
		msg += "，北京时间 " + e.RetryAt.In(time.FixedZone("CST", 8*60*60)).Format("2006-01-02 15:04") + " 重置"
	}
	if e.Bucket == "ai" {
		msg += "；后台自动任务与手动操作共用此额度"
	}
	return msg
}
