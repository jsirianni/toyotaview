package smartcar

import "time"

type accessToken struct {
	value     string
	expiresAt time.Time
}

func (t accessToken) valid(now time.Time) bool {
	if t.value == "" {
		return false
	}

	return now.Before(t.expiresAt.Add(-5 * time.Minute))
}
