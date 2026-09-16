package registrationpolicy

import (
	"strconv"
	"strings"
)

// Policy is server-owned. Its zero value denies all share invitations.
type Policy struct {
	mode   string
	owners map[int]bool
}

func Parse(mode, owners string) Policy {
	p := Policy{mode: mode, owners: map[int]bool{}}
	if mode != "selected_owners" {
		return p
	}
	for _, value := range strings.Split(owners, ",") {
		if strings.TrimSpace(value) == "" {
			return Policy{}
		}
		id, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || id <= 0 {
			return Policy{}
		}
		p.owners[id] = true
	}
	return p
}
func (p Policy) Allows(owner int) bool {
	return owner > 0 && (p.mode == "all" || (p.mode == "selected_owners" && p.owners[owner]))
}
