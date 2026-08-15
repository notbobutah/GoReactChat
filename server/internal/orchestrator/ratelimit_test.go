package orchestrator

import (
	"testing"
	"time"
)

func TestWindowRateLimiterBlocksAtLimit(t *testing.T) {
	r := NewWindowRateLimiter(2, time.Minute)
	key := RateKey{UserID: "u", WorkspaceID: "w"}

	for i := 0; i < 2; i++ {
		if d := r.Check(key); !d.Allowed {
			t.Fatalf("call %d denied while under the limit", i+1)
		}
	}
	d := r.Check(key)
	if d.Allowed {
		t.Error("third call allowed past a limit of 2")
	}
	if d.RetryAfter <= 0 {
		t.Error("a denial should carry a retry hint")
	}
}

func TestPerUserLimitDoesNotBlockOtherUsers(t *testing.T) {
	r := NewWindowRateLimiter(1, time.Minute)
	if d := r.Check(RateKey{UserID: "a"}); !d.Allowed {
		t.Fatal("first user denied")
	}
	if d := r.Check(RateKey{UserID: "b"}); !d.Allowed {
		t.Error("a second user was blocked by the first user's usage")
	}
}

func TestFixedKeyLimiterPoolsEveryCaller(t *testing.T) {
	// This is what makes the limit service-wide: two different visitors must
	// draw from the same bucket.
	r := NewFixedKeyRateLimiter(NewWindowRateLimiter(1, time.Minute))
	if d := r.Check(RateKey{UserID: "a"}); !d.Allowed {
		t.Fatal("first caller denied")
	}
	if d := r.Check(RateKey{UserID: "b"}); d.Allowed {
		t.Error("a second caller was allowed past a global limit of 1")
	}
}

func TestCompositeDeniesIfAnyLimiterDenies(t *testing.T) {
	// Generous per user, strict globally — the global one must still bind.
	c := NewCompositeRateLimiter(
		NewWindowRateLimiter(100, time.Minute),
		NewFixedKeyRateLimiter(NewWindowRateLimiter(2, time.Minute)),
	)
	if d := c.Check(RateKey{UserID: "a"}); !d.Allowed {
		t.Fatal("first call denied")
	}
	if d := c.Check(RateKey{UserID: "b"}); !d.Allowed {
		t.Fatal("second call denied")
	}
	if d := c.Check(RateKey{UserID: "c"}); d.Allowed {
		t.Error("third call allowed past the global limit of 2")
	}
}
