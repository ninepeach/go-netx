package socket

import (
	"errors"
	"strings"
	"testing"
)

// fakeKernel caps the buffer at limit unless the force option is used and
// allowed, and optionally doubles what it reports, like Linux.
type fakeKernel struct {
	limit, value   int
	doubles        bool
	forceAllowed   bool
	forceTried     bool
	setErr, getErr error
}

const (
	fakeOpt   = 1
	fakeForce = 2
)

func (k *fakeKernel) ops() bufferOps {
	return bufferOps{
		set: func(opt, v int) error {
			if k.setErr != nil {
				return k.setErr
			}
			if opt == fakeForce {
				k.forceTried = true
				if !k.forceAllowed {
					return errors.New("EPERM")
				}
				k.value = v
				return nil
			}
			k.value = min(v, k.limit)
			return nil
		},
		get: func(int) (int, error) {
			if k.doubles {
				return 2 * k.value, k.getErr
			}
			return k.value, k.getErr
		},
	}
}

// Checks: SOCK-22
func TestEnsureBufferReportsShortSize(t *testing.T) {
	kind := bufferKind{name: "receive buffer", opt: fakeOpt}
	for _, doubles := range []bool{false, true} {
		k := &fakeKernel{limit: 1000, doubles: doubles}
		err := ensureBuffer(k.ops(), kind, 4000, doubles)
		var bse *BufferSizeError
		if !errors.As(err, &bse) {
			t.Fatalf("doubles=%v: err = %v, want *BufferSizeError", doubles, err)
		}
		if bse.Option != "receive buffer" || bse.Requested != 4000 || bse.Actual != 1000 {
			t.Fatalf("doubles=%v: got %+v, want receive buffer 4000/1000 (reading halved when doubled)", doubles, *bse)
		}
		if !strings.Contains(err.Error(), "requested 4000 bytes, kernel granted 1000") {
			t.Fatalf("message %q", err)
		}
		if IsUnsupported(err) {
			t.Fatal("a short buffer is not an unsupported option")
		}

		k = &fakeKernel{limit: 1000, doubles: doubles}
		if err := ensureBuffer(k.ops(), kind, 1000, doubles); err != nil {
			t.Fatalf("doubles=%v: size within the cap: %v", doubles, err)
		}
	}
}

// Checks: SOCK-23
func TestEnsureBufferTriesForceOnce(t *testing.T) {
	withForce := bufferKind{name: "send buffer", opt: fakeOpt, force: fakeForce}

	k := &fakeKernel{limit: 1000, doubles: true, forceAllowed: true}
	if err := ensureBuffer(k.ops(), withForce, 4000, true); err != nil || !k.forceTried {
		t.Fatalf("force allowed: err=%v tried=%v, want nil and tried", err, k.forceTried)
	}

	k = &fakeKernel{limit: 1000, doubles: true}
	var bse *BufferSizeError
	if err := ensureBuffer(k.ops(), withForce, 4000, true); !errors.As(err, &bse) || !k.forceTried || bse.Actual != 1000 {
		t.Fatalf("force refused: err=%v tried=%v, want *BufferSizeError with Actual 1000 after trying", err, k.forceTried)
	}

	k = &fakeKernel{limit: 1000}
	if err := ensureBuffer(k.ops(), withForce, 500, false); err != nil || k.forceTried {
		t.Fatalf("size granted: err=%v tried=%v, want no force", err, k.forceTried)
	}

	k = &fakeKernel{limit: 1000}
	noForce := bufferKind{name: "send buffer", opt: fakeOpt}
	if err := ensureBuffer(k.ops(), noForce, 4000, false); !errors.As(err, &bse) || k.forceTried {
		t.Fatalf("no force option: err=%v tried=%v", err, k.forceTried)
	}

	setErr := errors.New("set failed")
	k = &fakeKernel{limit: 1000, setErr: setErr}
	if err := ensureBuffer(k.ops(), withForce, 10, false); !errors.Is(err, setErr) {
		t.Fatalf("setsockopt error: got %v", err)
	}
	getErr := errors.New("get failed")
	k = &fakeKernel{limit: 1000, getErr: getErr}
	if err := ensureBuffer(k.ops(), withForce, 10, false); !errors.Is(err, getErr) {
		t.Fatalf("getsockopt error: got %v", err)
	}
}
