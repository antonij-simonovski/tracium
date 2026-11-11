package alert

import "time"

func SetClock(d *Dispatcher, now func() time.Time) { d.now = now }
