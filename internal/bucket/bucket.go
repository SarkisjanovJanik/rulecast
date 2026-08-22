package bucket

const (
	Buckets = 10_000

	offset64 = 14695981039346656037
	prime64  = 1099511628211
)

func Of(flagKey, salt, userKey string) uint32 {
	hash := uint64(offset64)
	hash = write(hash, flagKey)
	hash = write(hash, salt)
	hash = write(hash, userKey)
	return uint32(hash % Buckets)
}

func write(hash uint64, s string) uint64 {
	for i := 0; i < len(s); i++ {
		hash ^= uint64(s[i])
		hash *= prime64
	}
	hash ^= ':'
	return hash * prime64
}

func InRollout(bucket uint32, percent float64) bool {
	if percent <= 0 {
		return false
	}
	if percent >= 100 {
		return true
	}
	return float64(bucket) < percent*Buckets/100
}

type Weighted struct {
	Key    string
	Weight float64
}

func Pick(bucket uint32, variations []Weighted) (string, bool) {
	var total float64
	for _, variation := range variations {
		if variation.Weight > 0 {
			total += variation.Weight
		}
	}
	if total <= 0 {
		return "", false
	}

	position := float64(bucket) / Buckets * total

	var cumulative float64
	for _, variation := range variations {
		if variation.Weight <= 0 {
			continue
		}
		cumulative += variation.Weight
		if position < cumulative {
			return variation.Key, true
		}
	}

	for i := len(variations) - 1; i >= 0; i-- {
		if variations[i].Weight > 0 {
			return variations[i].Key, true
		}
	}
	return "", false
}
