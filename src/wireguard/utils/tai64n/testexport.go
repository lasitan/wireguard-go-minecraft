package tai64n

import "time"

func Stamp(t time.Time) Timestamp {
	return stamp(t)
}
