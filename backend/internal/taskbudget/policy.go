package taskbudget

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Policies map[string]Policy

func LoadPolicies() (Policies, error) {
	out := Policies{}
	for key, limits := range map[string][4]int{
		"ai": {50, 1000, 2, 4}, "interactive": {300, 10000, 2, 20},
		"fetch": {100, 5000, 2, 10}, "capture": {100, 5000, 2, 10}, "pdf": {10, 300, 1, 3}, "subscribe": {30, 1000, 2, 10},
		"subscription_fetch": {500, 5000, 2, 5},
		"background_fetch":   {500, 5000, 2, 5}, "background_ocr": {10, 100, 1, 2},
	} {
		for i, suffix := range []string{"DAILY", "GLOBAL_DAILY", "CONCURRENT", "GLOBAL_CONCURRENT"} {
			name := "TASK_" + strings.ToUpper(key) + "_" + suffix
			if raw := os.Getenv(name); raw != "" {
				v, err := strconv.Atoi(raw)
				if err != nil || v < 1 || v > 1000000 {
					return nil, EnvError(name)
				}
				limits[i] = v
			}
		}
		out[key] = Policy{Daily: limits[0], GlobalDaily: limits[1], Concurrent: limits[2], GlobalConcurrent: limits[3], Lease: 15 * time.Minute}
	}
	for _, bucket := range []string{"background_fetch", "subscription_fetch"} {
		name := "TASK_" + strings.ToUpper(bucket) + "_ADMIN_DAILY"
		adminDaily := 2000
		if raw := os.Getenv(name); raw != "" {
			v, err := strconv.Atoi(raw)
			if err != nil || v < 1 || v > 1000000 {
				return nil, EnvError(name)
			}
			adminDaily = v
		}
		p := out[bucket]
		p.AdminDaily = adminDaily
		out[bucket] = p
	}
	return out, nil
}
