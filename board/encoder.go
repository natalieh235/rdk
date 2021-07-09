package board

import (
	"context"
	"sync"
	"sync/atomic"

	"go.viam.com/utils"

	pb "go.viam.com/core/proto/api/v1"
)

// Encoder keeps track of a motor position
type Encoder interface {
	// Position returns the current position in terms of ticks
	Position(ctx context.Context) (int64, error)

	// Start starts a background thread to run the encoder, if there is none needed this is a no-op
	Start(cancelCtx context.Context, activeBackgroundWorkers *sync.WaitGroup, onStart func())
}

// ---------

// HallEncoder keeps track of a motor position using a rotary hall encoder
type HallEncoder struct {
	a, b     DigitalInterrupt
	position int64
}

// NewHallEncoder creates a new HallEncoder
func NewHallEncoder(a, b DigitalInterrupt) *HallEncoder {
	return &HallEncoder{a, b, 0}
}

// Start starts the HallEncoder background thread
func (e *HallEncoder) Start(cancelCtx context.Context, activeBackgroundWorkers *sync.WaitGroup, onStart func()) {
	/**
	  a rotary encoder looks like

	  picture from https://github.com/joan2937/pigpio/blob/master/EXAMPLES/C/ROTARY_ENCODER/rotary_encoder.c
	    1   2     3    4    1    2    3    4     1

	            +---------+         +---------+      0
	            |         |         |         |
	  A         |         |         |         |
	            |         |         |         |
	  +---------+         +---------+         +----- 1

	      +---------+         +---------+            0
	      |         |         |         |
	  B   |         |         |         |
	      |         |         |         |
	  ----+         +---------+         +---------+  1

	*/

	chanA := make(chan bool)
	chanB := make(chan bool)

	e.a.AddCallback(chanA)
	e.b.AddCallback(chanB)

	activeBackgroundWorkers.Add(1)

	utils.ManagedGo(func() {
		onStart()
		aLevel := true
		bLevel := true

		lastWasA := true
		lastLevel := true

		for {

			select {
			case <-cancelCtx.Done():
				return
			default:
			}

			var level bool
			var isA bool

			select {
			case <-cancelCtx.Done():
				return
			case level = <-chanA:
				isA = true
				aLevel = level
			case level = <-chanB:
				isA = false
				bLevel = level
			}

			if isA == lastWasA && level == lastLevel {
				// this means we got the exact same message multiple times
				// this is probably some sort of hardware issue, so we ignore
				continue
			}
			lastWasA = isA
			lastLevel = level

			if !aLevel && !bLevel { // state 1
				if lastWasA {
					e.inc()
				} else {
					e.dec()
				}
			} else if !aLevel && bLevel { // state 2
				if lastWasA {
					e.dec()
				} else {
					e.inc()
				}
			} else if aLevel && bLevel { // state 3
				if lastWasA {
					e.inc()
				} else {
					e.dec()
				}
			} else if aLevel && !bLevel { // state 4
				if lastWasA {
					e.dec()
				} else {
					e.inc()
				}
			}

		}
	}, activeBackgroundWorkers.Done)
}

// Position returns the current position
func (e *HallEncoder) Position(ctx context.Context) (int64, error) {
	return atomic.LoadInt64(&e.position), nil
}

func (e *HallEncoder) rawPosition() int64 {
	return atomic.LoadInt64(&e.position)
}

func (e *HallEncoder) inc() {
	atomic.AddInt64(&e.position, 1)
}

func (e *HallEncoder) dec() {
	atomic.AddInt64(&e.position, -1)
}

// ---------

type singleEncoder struct {
	i        DigitalInterrupt
	position int64
	m        *encodedMotor // note: this is gross, but not sure anyone should use this, so....
}

func (e *singleEncoder) Start(cancelCtx context.Context, activeBackgroundWorkers *sync.WaitGroup, onStart func()) {
	encoderChannel := make(chan bool)
	e.i.AddCallback(encoderChannel)
	activeBackgroundWorkers.Add(1)
	utils.ManagedGo(func() {
		onStart()
		_, rpmDebug := getRPMSleepDebug()
		for {
			select {
			case <-cancelCtx.Done():
				return
			default:
			}

			select {
			case <-cancelCtx.Done():
				return
			case <-encoderChannel:
			}

			dir := e.m.rawDirection()
			if dir == pb.DirectionRelative_DIRECTION_RELATIVE_FORWARD {
				atomic.AddInt64(&e.position, 1)
				//stop = m.state.regulated && m.state.curPosition >= m.state.setPoint
			} else if dir == pb.DirectionRelative_DIRECTION_RELATIVE_BACKWARD {
				atomic.AddInt64(&e.position, -1)
				//stop = m.state.regulated && m.state.curPosition <= m.state.setPoint
			} else if rpmDebug {
				e.m.logger.Warn("got encoder tick but motor should be off")
			}
		}
	}, activeBackgroundWorkers.Done)
}

// Position returns the current position
func (e *singleEncoder) Position(ctx context.Context) (int64, error) {
	return atomic.LoadInt64(&e.position), nil
}