package opsmonitor

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strconv"
	"time"
)

type Config struct {
	CaptchaMinFailures  int
	CaptchaFailureRatio float64
	LimitThreshold      int
	QueueWaitSeconds    float64
	QuotaWarningRatio   float64
	DailyCostBudget     *float64
	UnitPrices          map[string]float64
	CaptchaExpiresAt    *time.Time
}

func DefaultConfig() Config {
	return Config{CaptchaMinFailures: 3, CaptchaFailureRatio: .2, LimitThreshold: 20, QueueWaitSeconds: 1800, QuotaWarningRatio: .8, UnitPrices: map[string]float64{}}
}
func LoadConfig() (Config, error) {
	c := DefaultConfig()
	for name, p := range map[string]*float64{"CAPTCHA_FAILURE_RATIO": &c.CaptchaFailureRatio, "QUEUE_WAIT_SECONDS": &c.QueueWaitSeconds, "QUOTA_WARNING_RATIO": &c.QuotaWarningRatio} {
		if v := os.Getenv("OPS_MONITOR_" + name); v != "" {
			n, e := strconv.ParseFloat(v, 64)
			if e != nil || !validNumber(n) {
				return c, fmt.Errorf("invalid OPS_MONITOR_%s", name)
			}
			*p = n
		}
	}
	for name, p := range map[string]*int{"CAPTCHA_MIN_FAILURES": &c.CaptchaMinFailures, "LIMIT_THRESHOLD": &c.LimitThreshold} {
		if v := os.Getenv("OPS_MONITOR_" + name); v != "" {
			n, e := strconv.Atoi(v)
			if e != nil || n < 1 {
				return c, fmt.Errorf("invalid OPS_MONITOR_%s", name)
			}
			*p = n
		}
	}
	if c.CaptchaFailureRatio >= 1 || c.QuotaWarningRatio > 1 {
		return c, fmt.Errorf("monitor ratios must be at most one")
	}
	if v := os.Getenv("OPS_MONITOR_DAILY_COST_BUDGET"); v != "" {
		n, e := strconv.ParseFloat(v, 64)
		if e != nil || !validNumber(n) {
			return c, fmt.Errorf("invalid monitor cost budget")
		}
		c.DailyCostBudget = &n
	}
	if v := os.Getenv("OPS_MONITOR_UNIT_PRICES"); v != "" {
		if e := json.Unmarshal([]byte(v), &c.UnitPrices); e != nil {
			return c, fmt.Errorf("invalid monitor unit prices")
		}
		for k, n := range c.UnitPrices {
			if !validPriceTask(k) || !validNumber(n) {
				return c, fmt.Errorf("invalid monitor unit price")
			}
		}
	}
	if v := os.Getenv("OPS_MONITOR_CAPTCHA_EXPIRES_AT"); v != "" {
		n, e := time.Parse(time.RFC3339, v)
		if e != nil {
			return c, fmt.Errorf("invalid monitor captcha expiry")
		}
		c.CaptchaExpiresAt = &n
	}
	return c, nil
}
func validNumber(n float64) bool { return n > 0 && !math.IsNaN(n) && !math.IsInf(n, 0) }
func validTask(s string) bool {
	switch s {
	case "", "register", "login", "refresh", "logout", "init", "ai", "interactive", "fetch", "capture", "pdf", "subscribe", "background_fetch", "background_ocr":
		return true
	}
	return false
}

func validPriceTask(k string) bool {
	switch k {
	case "ai", "interactive", "fetch", "capture", "pdf", "subscribe", "background_fetch", "background_ocr":
		return true
	}
	return false
}
