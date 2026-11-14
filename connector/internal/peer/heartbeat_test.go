package peer

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/k2dms/nexora/connector/internal/contract"
	"github.com/k2dms/nexora/connector/internal/envelope"
	"github.com/k2dms/nexora/connector/internal/frames"
)

// The sender half of framing.json#/liveness: a peer with BeatSeconds writes a heartbeat frame every
// interval while Serve runs, and the beat carries nothing but v, id, type and sentAt — the fifth frame
// type §9.8 named (VER-49's third clause).
func TestTheBeatSenderWritesHeartbeatsOnTheInterval(t *testing.T) {
	mine, theirs := newPipe(t)

	cfg := ToExtension()
	cfg.BeatSeconds = 1
	p := New(mine, cfg)
	done := make(chan error, 1)
	go func() { done <- p.Serve() }()

	// Two beats within a window that tolerates scheduling: the interval is one second, the contract's is
	// twenty-five, and the shape the frames carry is what this test is about, not the clock.
	deadline := time.Now().Add(3500 * time.Millisecond)
	beats := 0
	for beats < 2 && time.Now().Before(deadline) {
		data, err := frames.Read(theirs, contract.HostToExtensionBytes)
		if err != nil {
			t.Fatalf("reading a beat: %v", err)
		}
		f, err := envelope.Parse(data)
		if err != nil {
			t.Fatalf("the beat is not a valid envelope: %v", err)
		}
		if f.Type != envelope.TypeHeartbeat {
			t.Fatalf("the sender wrote a %s frame, want heartbeat", f.Type)
		}
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(data, &raw); err != nil {
			t.Fatalf("the beat does not re-read: %v", err)
		}
		for _, k := range []string{"method", "result", "ok", "error", "progress", "chunk"} {
			if _, present := raw[k]; present {
				t.Fatalf("the beat carries %s: a beat is v, id, type and sentAt and nothing else", k)
			}
		}
		beats++
	}
	_ = p.Close()
	if beats < 2 {
		t.Fatalf("only %d beats arrived in the window", beats)
	}
}

// The receiver half: an armed watchdog counts silence in contract units, and a beat arriving resets it —
// one beat per interval keeps the transport alive indefinitely, and the teardown happens only after the
// three-miss deadline passes with nothing arriving (VER-49's third clause).
func TestBeatsKeepTheWatchdogAliveAndSilenceTearsItDown(t *testing.T) {
	mine, theirs := newPipe(t)
	clk := &clock{t: time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC)}

	cfg := ToHost()
	cfg.HeartbeatInterval = 25 * time.Second
	cfg.MissedHeartbeats = 3
	cfg.Tick = ptrTick(time.Second)
	cfg.Now = clk.Now
	cfg.PipeFloor = 1
	p := New(mine, cfg)
	done := make(chan error, 1)
	go func() { done <- p.Serve() }()

	// Two beats arrive, one per 25-second interval, each resetting the count the watchdog reads on its own
	// tick: the peer must still be serving at the point a silent peer's 3×25s deadline has long passed.
	for i := 0; i < 2; i++ {
		clk.After(25 * time.Second)
		beat, err := envelope.NewHeartbeat()
		if err != nil {
			t.Fatalf("NewHeartbeat: %v", err)
		}
		data, err := beat.Marshal()
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if d, ok := theirs.(interface{ SetWriteDeadline(time.Time) error }); ok {
			_ = d.SetWriteDeadline(time.Now().Add(2 * time.Second))
		}
		if err := frames.Write(theirs, data, contract.HostToExtensionBytes); err != nil {
			t.Fatalf("writing beat %d: %v", i+1, err)
		}
		// Let the watchdog tick at least once with the beat counted: the real ticker runs on wall time, so
		// a short sleep is the window in which a missed-count bug would fire.
		time.Sleep(5 * time.Millisecond)
		select {
		case err := <-done:
			t.Fatalf("Serve returned while beats kept arriving: %v", err)
		default:
		}
	}
	// Then silence: three full intervals with nothing arriving is the verdict (VER-49's third clause).
	clk.After(3 * 25 * time.Second)
	err := <-done
	if err == nil || !errors.Is(err, ErrPeerLost) {
		t.Fatalf("Serve = %v, want ErrPeerLost", err)
	}
}

func ptrTick(d time.Duration) *time.Duration { return &d }
