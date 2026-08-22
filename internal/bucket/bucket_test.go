package bucket

import (
	"fmt"
	"math"
	"testing"
)

func TestOfIsDeterministic(t *testing.T) {
	first := Of("checkout-v2", "salt-1", "user-42")
	for range 100 {
		if got := Of("checkout-v2", "salt-1", "user-42"); got != first {
			t.Fatalf("bucket changed between calls: %d then %d", first, got)
		}
	}
}

func TestOfIsInRange(t *testing.T) {
	for i := range 10_000 {
		key := fmt.Sprintf("user-%d", i)
		if got := Of("flag", "salt", key); got >= Buckets {
			t.Fatalf("Of(%q) = %d, want < %d", key, got, Buckets)
		}
	}
}

func TestOfDecorrelatesFlags(t *testing.T) {
	same := 0
	const users = 5_000

	for i := range users {
		key := fmt.Sprintf("user-%d", i)
		if Of("flag-a", "salt", key) == Of("flag-b", "salt", key) {
			same++
		}
	}

	if same > users/100 {
		t.Errorf("%d of %d users share a bucket across two flags, expected near %d",
			same, users, users/Buckets)
	}
}

func TestOfDistributesEvenly(t *testing.T) {
	const users = 100_000
	const slots = 10

	counts := make([]int, slots)
	for i := range users {
		b := Of("flag", "salt", fmt.Sprintf("user-%d", i))
		counts[int(b)*slots/Buckets]++
	}

	expected := float64(users) / slots
	for slot, count := range counts {
		deviation := math.Abs(float64(count)-expected) / expected
		if deviation > 0.05 {
			t.Errorf("slot %d has %d users, expected ~%.0f (%.1f%% off)",
				slot, count, expected, deviation*100)
		}
	}
}

func TestWideningARolloutOnlyAdds(t *testing.T) {
	const users = 20_000
	percents := []float64{1, 5, 10, 25, 50, 75, 100}

	previous := make(map[string]bool, users)
	for _, percent := range percents {
		current := make(map[string]bool, users)
		for i := range users {
			key := fmt.Sprintf("user-%d", i)
			current[key] = InRollout(Of("flag", "salt", key), percent)
		}

		for key, wasIn := range previous {
			if wasIn && !current[key] {
				t.Fatalf("%s dropped out of the rollout when it widened to %.0f%%", key, percent)
			}
		}
		previous = current
	}
}

func TestInRolloutBounds(t *testing.T) {
	if InRollout(0, 0) {
		t.Error("a 0% rollout must include nobody")
	}
	if !InRollout(Buckets-1, 100) {
		t.Error("a 100% rollout must include everybody")
	}
	if InRollout(Buckets-1, 50) {
		t.Error("the last bucket must be outside a 50% rollout")
	}
	if !InRollout(0, 0.01) {
		t.Error("the first bucket must be inside a one basis point rollout")
	}
}

func TestInRolloutMatchesThePercentage(t *testing.T) {
	const users = 50_000

	for _, percent := range []float64{5, 20, 50, 80} {
		inside := 0
		for i := range users {
			if InRollout(Of("flag", "salt", fmt.Sprintf("user-%d", i)), percent) {
				inside++
			}
		}

		actual := float64(inside) / users * 100
		if math.Abs(actual-percent) > 1 {
			t.Errorf("rollout of %.0f%% reached %.2f%% of users", percent, actual)
		}
	}
}

func TestPickRespectsWeights(t *testing.T) {
	variations := []Weighted{
		{Key: "control", Weight: 50},
		{Key: "variant-a", Weight: 30},
		{Key: "variant-b", Weight: 20},
	}

	const users = 60_000
	counts := map[string]int{}
	for i := range users {
		key, ok := Pick(Of("exp", "salt", fmt.Sprintf("user-%d", i)), variations)
		if !ok {
			t.Fatal("Pick returned no variation")
		}
		counts[key]++
	}

	for _, variation := range variations {
		actual := float64(counts[variation.Key]) / users * 100
		if math.Abs(actual-variation.Weight) > 1.5 {
			t.Errorf("%s got %.2f%%, expected %.0f%%", variation.Key, actual, variation.Weight)
		}
	}
}

func TestPickIsStable(t *testing.T) {
	variations := []Weighted{{Key: "a", Weight: 1}, {Key: "b", Weight: 1}}
	b := Of("exp", "salt", "user-7")

	first, _ := Pick(b, variations)
	for range 50 {
		if got, _ := Pick(b, variations); got != first {
			t.Fatalf("Pick is not stable: %s then %s", first, got)
		}
	}
}

func TestPickHandlesDegenerateWeights(t *testing.T) {
	if _, ok := Pick(500, nil); ok {
		t.Error("no variations must yield no pick")
	}
	if _, ok := Pick(500, []Weighted{{Key: "a", Weight: 0}}); ok {
		t.Error("zero total weight must yield no pick")
	}

	key, ok := Pick(Buckets-1, []Weighted{{Key: "only", Weight: 100}})
	if !ok || key != "only" {
		t.Errorf("Pick = %q, %v; want \"only\", true", key, ok)
	}
}

func BenchmarkOf(b *testing.B) {
	b.ReportAllocs()
	for range b.N {
		Of("checkout-v2", "salt-1", "user-1234567890")
	}
}

func BenchmarkPick(b *testing.B) {
	variations := []Weighted{
		{Key: "control", Weight: 50},
		{Key: "variant-a", Weight: 30},
		{Key: "variant-b", Weight: 20},
	}
	b.ReportAllocs()
	for range b.N {
		Pick(4242, variations)
	}
}
