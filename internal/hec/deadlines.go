package hec

import (
	"context"
	"errors"
	"math"
	"time"
)

const (
	MaxDirectCall            = 90 * time.Second
	MaxJobWait               = 15 * time.Second
	ResponseDeliveryReserve  = 10 * time.Second
	CallGateAcquireTimeout   = 10 * time.Second
	GenerationReadyTimeout   = 60 * time.Second
	TunnelStopTimeout        = 5 * time.Second
	GenerationCleanupTimeout = 10 * time.Second
)

var errInvalidDurationMilliseconds = errors.New("millisecond duration is out of range")

func durationFromMilliseconds(milliseconds int64) (time.Duration, error) {
	if milliseconds < 0 || milliseconds > math.MaxInt64/int64(time.Millisecond) {
		return 0, errInvalidDurationMilliseconds
	}
	return time.Duration(milliseconds) * time.Millisecond, nil
}

func directRunTimeout(milliseconds int64) (time.Duration, error) {
	duration, err := durationFromMilliseconds(milliseconds)
	if err != nil || duration > MaxDirectCall {
		return 0, errors.New("run timeout must be between 0 and 90000 milliseconds")
	}
	if duration == 0 {
		duration = MaxDirectCall
	}
	return duration, nil
}

func jobWaitTimeout(milliseconds int64) (time.Duration, error) {
	duration, err := durationFromMilliseconds(milliseconds)
	if err != nil || duration > MaxJobWait {
		return 0, errors.New("job wait timeout must be between 0 and 15000 milliseconds")
	}
	if duration == 0 {
		duration = MaxJobWait
	}
	return duration, nil
}

func minDuration(left, right time.Duration) time.Duration {
	if left <= 0 {
		return right
	}
	if right <= 0 || left < right {
		return left
	}
	return right
}

// directCallContext applies the fixed direct-call maximum and reserves time
// for tunnel response delivery when the incoming context already has a
// deadline. The tunnel client derives that deadline from poll receipt plus the
// command's response_timeout.
func directCallContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	deadline := time.Now().Add(MaxDirectCall)
	if callerDeadline, ok := parent.Deadline(); ok {
		reserved := callerDeadline.Add(-ResponseDeliveryReserve)
		if reserved.Before(deadline) {
			deadline = reserved
		}
	}
	return context.WithDeadline(parent, deadline)
}
